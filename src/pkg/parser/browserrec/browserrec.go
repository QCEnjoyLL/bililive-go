// Package browserrec 实现一个"无头浏览器抓取"录制 parser，注册名 "browser"。
//
// 思路：让 Chromium 自己用其完整 WebRTC 栈（抖动缓冲/NACK/错误隐藏）解码直播，
// 再用页面内的 MediaRecorder 把"已解码"的 MediaStream 录成 WebM，按块经 CDP binding
// 取出，喂给 ffmpeg `-c copy` 封装为 .mkv。相比进程内直转（webrtcrec），画面无绿幕/
// 花屏，代价是重编码吃 CPU、需要本机 Chrome/Edge。
//
// 仅用于 webrtc:// 流；通过配置 recording_engine: browser 切换，默认仍是 webrtcrec。
package browserrec

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/bililive-go/bililive-go/src/live"
	"github.com/bililive-go/bililive-go/src/pkg/livelogger"
	"github.com/bililive-go/bililive-go/src/pkg/parser"
	"github.com/bililive-go/bililive-go/src/pkg/utils"
	"github.com/bililive-go/bililive-go/src/tools"
)

const (
	Name = "browser"

	videoReadyTO = 45 * time.Second // 等 video 就绪的上限，超时即返回错误（绝不无限卡"准备中"）
	mediaIdleTO  = 20 * time.Second // 超过此时长无新数据块则判定流结束
)

// 开录：抓 video 的原生 MediaStream（srcObject，原始分辨率），MediaRecorder 录 WebM，
// 用 FileReader 转 base64 后切片经 binding 回传（避开 CDP binding 大 payload 丢弃；
// 按 4 的倍数切片，每片可独立 base64 解码）。
const recordJS = `(() => {
  const v = document.querySelector('video');
  if (!v) return 'NO_VIDEO';
  try { v.muted = true; v.play(); } catch (e) {}
  const s = v.srcObject || (v.captureStream && v.captureStream());
  if (!s) return 'NO_STREAM';
  let mime = 'video/webm;codecs=vp8,opus';
  if (!MediaRecorder.isTypeSupported(mime)) mime = 'video/webm';
  const rec = new MediaRecorder(s, {mimeType: mime});
  rec.ondataavailable = e => {
    if (!e.data || !e.data.size) return;
    const fr = new FileReader();
    fr.onload = () => {
      const r = fr.result, idx = r.indexOf('base64,');
      const b64 = idx >= 0 ? r.slice(idx + 7) : '';
      const N = 60000;
      for (let i = 0; i < b64.length; i += N) {
        try { window.__chunk(b64.slice(i, i + N)); } catch (er) {}
      }
    };
    fr.readAsDataURL(e.data);
  };
  window.__rec = rec;
  rec.start(1000);
  return s.getVideoTracks().length + 'v' + s.getAudioTracks().length + 'a';
})()`

// 过年龄门：置常见 localStorage 标记 + 只点对话框内的确认按钮（不碰页脚链接，
// 避免误点 "18 U.S.C. 2257" 之类链接导致跳转）。
const dismissJS = `(() => {
  try {
    for (const k of ['ageVerified','age_verified','isAdult','ageConfirmed','agree','ageGatePassed']) {
      localStorage.setItem(k, 'true');
    }
  } catch (e) {}
  const scope = document.querySelector('[role=dialog], .modal, [class*=age], [class*=Age], [class*=consent], [class*=Consent]') || document;
  for (const b of scope.querySelectorAll('button')) {
    const t = (b.textContent || '').trim();
    if (/^(enter|i am over|i'm over|yes,? i am|agree|continue|我已满|我已年满|同意|确认进入|进入)/i.test(t)) b.click();
  }
  return 0;
})()`

func init() {
	parser.Register(Name, new(builder))
}

type builder struct{}

func (b *builder) Build(cfg map[string]string, logger *livelogger.LiveLogger) (parser.Parser, error) {
	w, h := qualityToWindow(cfg["recording_quality"])
	return &Parser{
		closeOnce:   new(sync.Once),
		cleanupOnce: new(sync.Once),
		stopCh:      make(chan struct{}),
		browserPath: cfg["browser_path"], // 可选：指定 Chrome/Edge/Chromium 路径；留空则自动探测
		winW:        w,
		winH:        h,
		quality:     cfg["recording_quality"],
		logger:      logger,
	}, nil
}

// resolveBrowserPath 解析浏览器可执行文件路径，优先级：
//  1. 显式指定 (cfg browser_path)
//  2. 已下载的内置 Chromium
//  3. 系统 Chrome/Edge（返回 "" 交给 chromedp 自动探测，避免无谓下载 150MB）
//  4. 都没有 → 现下载内置 Chromium（仅首次）
func (p *Parser) resolveBrowserPath() string {
	if p.browserPath != "" {
		return p.browserPath
	}
	if path := downloadedChromium(); path != "" {
		return path
	}
	if findSystemBrowser() != "" {
		return "" // 有系统浏览器，交给 chromedp 自动探测同一个
	}
	p.logger.Infof("browserrec: 未检测到 Chrome/Edge，开始下载内置 Chromium（约 150MB，仅首次）…")
	if err := tools.DownloadIfNecessary("chromium"); err != nil {
		p.logger.Warnf("browserrec: 下载内置 Chromium 失败: %v", err)
		return ""
	}
	return downloadedChromium()
}

// downloadedChromium 返回已下载的内置 Chromium 路径（不存在则 ""）。
func downloadedChromium() string {
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

// findSystemBrowser 探测系统已安装的 Chrome/Edge/Chromium（找到返回其路径）。
func findSystemBrowser() string {
	for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome", "microsoft-edge", "msedge"} {
		if pth, err := exec.LookPath(n); err == nil {
			return pth
		}
	}
	if goruntime.GOOS == "windows" {
		for _, pth := range []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		} {
			if _, err := os.Stat(pth); err == nil {
				return pth
			}
		}
	}
	return ""
}

// qualityLabel 返回用于日志的画质档位名（空/source/1080p 等）。
func qualityLabel(q string) string {
	if q == "" {
		return "source(默认最高)"
	}
	return q
}

// qualityToWindow 把清晰度档位映射为无头窗口尺寸。
// 该站为自适应 WebRTC 播放器，画质跟随播放器/视口大小：窗口越大画质越高（至源画质封顶）。
// 这是比驱动隐藏的画质菜单更稳健的选择方式。
func qualityToWindow(q string) (int, int) {
	switch q {
	case "720p":
		return 1280, 720
	case "480p":
		return 854, 480
	case "360p":
		return 640, 360
	case "1080p", "source", "":
		return 1920, 1080 // 默认取最高（源画质上限通常为 1080p）
	default:
		return 1920, 1080
	}
}

type Parser struct {
	logger      *livelogger.LiveLogger
	browserPath string
	winW, winH  int
	quality     string

	closeOnce   *sync.Once
	cleanupOnce *sync.Once
	stopCh      chan struct{}

	mu       sync.Mutex
	stdin    io.WriteCloser
	cmd      *exec.Cmd
	bctx     context.Context
	cancelFn context.CancelFunc
	outFile  string

	lastChunkNano int64
	bytesWritten  int64
}

func (p *Parser) ParseLiveStream(ctx context.Context, _ *live.StreamUrlInfo, liveObj live.Live, file string) error {
	pageURL := liveObj.GetRawUrl()

	ffmpegPath, err := utils.GetFFmpegPathForLive(ctx, liveObj)
	if err != nil {
		return fmt.Errorf("browserrec: 找不到 ffmpeg: %w", err)
	}

	// 1) 启动无头浏览器（ffmpeg 推迟到画面就绪后再起，避免启动失败时泄漏 ffmpeg/留碎片）
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", "new"),
		chromedp.Flag("autoplay-policy", "no-user-gesture-required"),
		chromedp.Flag("use-fake-ui-for-media-stream", true),
		chromedp.Flag("mute-audio", false),
		chromedp.WindowSize(p.winW, p.winH),
	)
	if execPath := p.resolveBrowserPath(); execPath != "" {
		opts = append(opts, chromedp.ExecPath(execPath))
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	bctx, cancelB := chromedp.NewContext(allocCtx)
	p.mu.Lock()
	p.bctx = bctx
	p.cancelFn = func() { cancelB(); cancelAlloc() }
	p.outFile = file
	p.mu.Unlock()
	defer p.cleanup() // 统一收尾：停录、关 stdin、杀 ffmpeg、cancel 浏览器、删空文件

	// 2) 收块：解码后写入 ffmpeg stdin（stdin 在画面就绪、ffmpeg 启动后才非空）
	chromedp.ListenTarget(bctx, func(ev interface{}) {
		be, ok := ev.(*runtime.EventBindingCalled)
		if !ok || be.Name != "__chunk" {
			return
		}
		data, derr := base64.StdEncoding.DecodeString(be.Payload)
		if derr != nil || len(data) == 0 {
			return
		}
		p.mu.Lock()
		if p.stdin != nil {
			_, _ = p.stdin.Write(data)
			atomic.AddInt64(&p.bytesWritten, int64(len(data)))
		}
		p.mu.Unlock()
		atomic.StoreInt64(&p.lastChunkNano, time.Now().UnixNano())
	})

	// 注入 Cookie（导航前）→ 解除 1 小时未登录限制
	if err := chromedp.Run(bctx,
		network.Enable(),
		runtime.Enable(),
		chromedp.ActionFunc(func(c context.Context) error { return setCookies(c, liveObj, pageURL) }),
		runtime.AddBinding("__chunk"),
	); err != nil {
		return fmt.Errorf("browserrec: 初始化浏览器失败: %w", err)
	}

	// 导航（站点广告多、偶发超时 → 重试，不致命）
	for i := 0; i < 3; i++ {
		if e := chromedp.Run(bctx, chromedp.Navigate(pageURL)); e == nil {
			break
		} else {
			p.logger.Warnf("browserrec: 导航失败(%d/3): %v", i+1, e)
			time.Sleep(2 * time.Second)
		}
	}
	_ = chromedp.Run(bctx, runtime.AddBinding("__chunk")) // 新页面上下文重加 binding

	// 等 video 就绪（超时即返回错误，交由上层重试，绝不无限卡住；此时 ffmpeg 尚未启动，不留文件）
	if !p.waitVideo(ctx, bctx) {
		return fmt.Errorf("browserrec: %s 未在 %s 内就绪（未开播/年龄门/headless 不播放）", pageURL, videoReadyTO)
	}

	// 自适应画质会从低分辨率逐步爬升到匹配窗口的档位；等其稳定后再开录，
	// 否则会录到初始的低分辨率（MediaRecorder 以首帧分辨率为准）。最多等约 12s。
	var lastH, stable, finalH int
	for i := 0; i < 12; i++ {
		var h int
		_ = chromedp.Run(bctx, chromedp.Evaluate(`(document.querySelector('video')||{}).videoHeight||0`, &h))
		p.logger.Debugf("browserrec 画质爬升[%d] videoHeight=%d", i, h)
		finalH = h
		if h > 0 && h >= p.winH {
			break // 已达目标档位
		}
		if h > 0 && h == lastH {
			stable++
		} else {
			stable = 0
		}
		if stable >= 3 {
			break // 连续 3 次不变 → 已到源画质上限
		}
		lastH = h
		select {
		case <-ctx.Done():
			return nil
		case <-p.stopCh:
			return nil
		case <-time.After(1 * time.Second):
		}
	}

	// 3) 画面就绪后才启动 ffmpeg：从 stdin 读 WebM，-c copy 封装 .mkv（WebM ⊂ Matroska，不重编码）
	cmd := exec.Command(ffmpegPath,
		"-hide_banner", "-nostats",
		"-fflags", "+genpts",
		"-i", "pipe:0",
		"-c", "copy",
		"-y", file,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("browserrec: 创建 ffmpeg stdin 失败: %w", err)
	}
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("browserrec: 启动 ffmpeg 失败: %w", err)
	}
	if stderr != nil {
		go drainToLog(stderr, p.logger)
	}
	p.mu.Lock()
	p.cmd, p.stdin = cmd, stdin
	p.mu.Unlock()

	// 4) 开录
	var info string
	if e := chromedp.Run(bctx, chromedp.Evaluate(recordJS, &info)); e != nil {
		return fmt.Errorf("browserrec: 开录失败: %w", e)
	}
	atomic.StoreInt64(&p.lastChunkNano, time.Now().UnixNano())
	p.logger.Infof("browserrec 开始录制 %s（%s，画质档=%s，实际高=%dp）-> %s", pageURL, info, qualityLabel(p.quality), finalH, file)

	// 5) 阻塞直到停止/取消/浏览器退出/长时间无数据；收尾由 defer cleanup 完成
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-p.stopCh:
			return nil
		case <-bctx.Done():
			p.logger.Warnf("browserrec: 浏览器已退出")
			return nil
		case <-ticker.C:
			if time.Since(time.Unix(0, atomic.LoadInt64(&p.lastChunkNano))) > mediaIdleTO {
				p.logger.Infof("browserrec: %s 已无数据，判定下播", pageURL)
				return nil
			}
		}
	}
}

// cleanup 统一收尾（幂等）：尽力停掉页面内 MediaRecorder 并等最后一块落盘，
// 关闭 stdin、回收 ffmpeg 与浏览器进程；若全程几乎没写入数据则删除空/碎片文件。
func (p *Parser) cleanup() {
	p.cleanupOnce.Do(func() {
		p.mu.Lock()
		cmd, stdin, bctx, cancel, out := p.cmd, p.stdin, p.bctx, p.cancelFn, p.outFile
		p.mu.Unlock()

		// 浏览器若仍在，停 MediaRecorder 并给最后一块时间 flush
		if bctx != nil && cmd != nil {
			_ = chromedp.Run(bctx, chromedp.Evaluate(
				`window.__rec && window.__rec.state!=='inactive' && window.__rec.stop()`, nil))
			time.Sleep(800 * time.Millisecond)
		}
		// 关 stdin，让 ffmpeg 正常收尾
		p.mu.Lock()
		if p.stdin != nil {
			_ = p.stdin.Close()
			p.stdin = nil
		}
		p.mu.Unlock()
		// 回收 ffmpeg：最多等 8s，超时强杀，杜绝泄漏
		if cmd != nil && cmd.Process != nil {
			done := make(chan struct{})
			go func() { _, _ = cmd.Process.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(8 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
		}
		// 回收浏览器进程
		if cancel != nil {
			cancel()
		}
		// 启动失败/无数据：删除空或极小（<64KB）的碎片文件
		if cmd != nil && out != "" && atomic.LoadInt64(&p.bytesWritten) < 64*1024 {
			if _, statErr := os.Stat(out); statErr == nil {
				_ = os.Remove(out)
				p.logger.Infof("browserrec: 本次几乎无数据，已删除碎片文件 %s", out)
			}
		}
		_ = stdin // 已在上面关闭
	})
}

// waitVideo 轮询：每秒尝试过年龄门并检查 video 是否拿到 MediaStream。
func (p *Parser) waitVideo(ctx context.Context, bctx context.Context) bool {
	deadline := time.Now().Add(videoReadyTO)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		case <-p.stopCh:
			return false
		case <-bctx.Done():
			return false
		default:
		}
		_ = chromedp.Run(bctx, chromedp.Evaluate(dismissJS, nil))
		var ready bool
		_ = chromedp.Run(bctx, chromedp.Evaluate(
			`(()=>{const v=document.querySelector('video');return !!(v&&(v.srcObject||v.readyState>=2));})()`, &ready))
		if ready {
			return true
		}
		time.Sleep(1 * time.Second)
	}
	return false
}

func (p *Parser) Stop() error {
	p.closeOnce.Do(func() { close(p.stopCh) })
	// 走统一收尾：停录、关 stdin、回收 ffmpeg 与浏览器（幂等，与 defer cleanup 二者其一生效）
	p.cleanup()
	return nil
}

// setCookies 把用户 Cookie 注入浏览器（与站点同域），解除未登录限制。
func setCookies(ctx context.Context, liveObj live.Live, pageURL string) error {
	opts := liveObj.GetOptions()
	if opts == nil || opts.Cookies == nil {
		return nil
	}
	u, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}
	for _, c := range opts.Cookies.Cookies(u) {
		if e := network.SetCookie(c.Name, c.Value).
			WithDomain(u.Hostname()).WithPath("/").Do(ctx); e != nil {
			return e
		}
	}
	return nil
}

func drainToLog(r io.Reader, logger *livelogger.LiveLogger) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			logger.Debugf("browserrec ffmpeg: %s", string(buf[:n]))
		}
		if err != nil {
			return
		}
	}
}
