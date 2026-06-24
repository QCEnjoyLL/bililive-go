// Package browser 提供 webrtc(browser) 与 hls 两个录制引擎共用的无头浏览器定位逻辑：
// 解析可执行文件路径（显式指定 > 已下载内置 Chromium > 系统 Chrome/Edge > 现下载）。
//
// 抽出为独立包，避免 browserrec 与 hlsmouflon(keystream 自愈引导) 各写一份探测/下载逻辑。
package browser

import (
	"os"
	"os/exec"
	goruntime "runtime"

	"github.com/bililive-go/bililive-go/src/pkg/livelogger"
	"github.com/bililive-go/bililive-go/src/tools"
)

// ResolveExecPath 解析浏览器可执行文件路径，优先级：
//  1. 显式指定 browserPath
//  2. 已下载的内置 Chromium
//  3. 系统 Chrome/Edge（返回 "" 交给 chromedp 自动探测，避免无谓下载 150MB）
//  4. 都没有 → 现下载内置 Chromium（仅首次；mac/linux-arm 无内置构建时下载会失败并返回 ""）
func ResolveExecPath(browserPath string, logger *livelogger.LiveLogger) string {
	if browserPath != "" {
		return browserPath
	}
	if path := DownloadedChromium(); path != "" {
		return path
	}
	if FindSystem() != "" {
		return "" // 有系统浏览器，交给 chromedp 自动探测同一个
	}
	if logger != nil {
		logger.Infof("browser: 未检测到 Chrome/Edge，开始下载内置 Chromium（约 150MB，仅首次）…")
	}
	if err := tools.DownloadIfNecessary("chromium"); err != nil {
		if logger != nil {
			logger.Warnf("browser: 下载内置 Chromium 失败（mac/ARM 无内置构建，请安装系统 Chrome/Edge）: %v", err)
		}
		return ""
	}
	return DownloadedChromium()
}

// DownloadedChromium 返回已下载的内置 Chromium 路径（不存在则 ""）。
func DownloadedChromium() string {
	api := tools.Get()
	if api == nil {
		return ""
	}
	t, err := api.GetTool("chromium")
	if err != nil || !t.DoesToolExist() {
		return ""
	}
	return t.GetToolPath()
}

// FindSystem 探测系统已安装的 Chrome/Edge/Chromium（找到返回其路径，否则 ""）。
// 先查 PATH，再按平台查常见安装位置——mac/linux 的 Chrome 常不在 PATH，
// 补默认路径让这些平台无需内置 Chromium 即可使用 browser 引擎与 keystream 自愈。
func FindSystem() string {
	for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome", "microsoft-edge", "msedge"} {
		if pth, err := exec.LookPath(n); err == nil {
			return pth
		}
	}
	var candidates []string
	switch goruntime.GOOS {
	case "windows":
		candidates = []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		}
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	case "linux":
		candidates = []string{
			"/usr/bin/google-chrome", "/usr/bin/google-chrome-stable",
			"/usr/bin/chromium", "/usr/bin/chromium-browser", "/snap/bin/chromium",
			"/usr/bin/microsoft-edge",
		}
	}
	for _, pth := range candidates {
		if _, err := os.Stat(pth); err == nil {
			return pth
		}
	}
	return ""
}
