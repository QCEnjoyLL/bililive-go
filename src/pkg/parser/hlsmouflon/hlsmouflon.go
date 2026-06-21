// Package hlsmouflon 实现 doppiocdn / stripchat 系（boyfriend.show）的 HLS 录制 parser，
// 注册名 "hls"。相比 webrtc（花屏）和 browser（重、慢、易中断），这是最优路径：
//
//	纯 HTTP 拉 LL-HLS 播放列表 → 解出被 MOUFLON 混淆的真实分段地址 → 下载 fMP4 分段
//	喂给 ffmpeg -c copy 封装为 .mkv。无损、无花屏、无广告、TCP 稳定、无需浏览器/WebRTC。
//
// MOUFLON 解码（已逆向并实测）：播放列表每段给一个诱饵 #EXT-X-PART(media.mp4) 和一个
// 真实但被加密的 #EXT-X-MOUFLON:URI:。真实分段文件名里的 hash 是
//
//	realHash = XOR( base64decode(reverse(encHash)), keystream )
//
// keystream = sha256(pdkey)[:16]，对固定 pkey 全局恒定，可硬编码（失效时支持配置覆盖）。
package hlsmouflon

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bililive-go/bililive-go/src/live"
	"github.com/bililive-go/bililive-go/src/pkg/livelogger"
	"github.com/bililive-go/bililive-go/src/pkg/parser"
	"github.com/bililive-go/bililive-go/src/pkg/utils"
)

const (
	Name = "hls"

	defaultUA  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	referer    = "https://zh.boyfriend.show/"
	masterHost = "edge-hls.doppiocdn.com"

	// 默认 MOUFLON 解码参数（已实测：该 pkey 全局恒定、跨房间通用）。
	// 若站点轮换导致失效，可用配置 hls_pkey / hls_keystream 覆盖，无需改代码。
	defaultPkey         = "1Dzcc6OjP73LKbtI"
	defaultKeystreamHex = "334eff75462ef6a610704bc3d3e7445f"

	pollInterval = 1500 * time.Millisecond
	mediaIdleTO  = 30 * time.Second // 超过此时长无新分段则判定下播
	startupTO    = 40 * time.Second // 启动期一直拿不到有效分段则判定未开播/密钥失效
)

// segRe 匹配分段文件名 {id}_{msn}_{hash}_{ts}(_partN).mp4，提取 hash。
var segRe = regexp.MustCompile(`_\d+_([A-Za-z0-9]+)_\d+(?:_part\d+)?\.mp4`)
var mapRe = regexp.MustCompile(`#EXT-X-MAP:URI="([^"]+)"`)

func init() { parser.Register(Name, new(builder)) }

type builder struct{}

func (b *builder) Build(cfg map[string]string, logger *livelogger.LiveLogger) (parser.Parser, error) {
	pkey := defaultPkey
	if v := cfg["hls_pkey"]; v != "" {
		pkey = v
	}
	ksHex := defaultKeystreamHex
	if v := cfg["hls_keystream"]; v != "" {
		ksHex = v
	}
	ks, err := hex.DecodeString(ksHex)
	if err != nil || len(ks) == 0 {
		return nil, fmt.Errorf("hlsmouflon: 非法 keystream: %v", err)
	}
	return &Parser{
		closeOnce:   new(sync.Once),
		cleanupOnce: new(sync.Once),
		stopCh:      make(chan struct{}),
		pkey:        pkey,
		keystream:   ks,
		logger:      logger,
	}, nil
}

type Parser struct {
	logger    *livelogger.LiveLogger
	pkey      string
	keystream []byte

	closeOnce   *sync.Once
	cleanupOnce *sync.Once
	stopCh      chan struct{}

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	outFile string
	written int64

	hc *http.Client
}

func (p *Parser) ParseLiveStream(ctx context.Context, streamUrlInfo *live.StreamUrlInfo, liveObj live.Live, file string) error {
	// modelId 来自 webrtc:// 伪 URL 的路径段
	modelID := strings.Trim(streamUrlInfo.Url.Path, "/")
	if modelID == "" {
		return fmt.Errorf("hlsmouflon: 无法从 %s 解析 modelId", streamUrlInfo.Url)
	}
	p.hc = &http.Client{Timeout: 15 * time.Second}

	// 1) master → 变体地址
	master := fmt.Sprintf("https://%s/hls/%s/master/%s.m3u8", masterHost, modelID, modelID)
	mbody, err := p.get(master)
	if err != nil {
		return fmt.Errorf("hlsmouflon: 拉取 master 失败（可能未开播）: %w", err)
	}
	variant := ""
	for _, l := range strings.Split(string(mbody), "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "https") {
			variant = l
			break
		}
	}
	if variant == "" {
		return fmt.Errorf("hlsmouflon: master 中找不到变体地址")
	}
	playlist := fmt.Sprintf("%s?psch=v2&pkey=%s", variant, p.pkey)

	// 2) ffmpeg：从 stdin 读 fMP4(init+分段)，-c copy 封装 .mkv
	ffmpegPath, err := utils.GetFFmpegPathForLive(ctx, liveObj)
	if err != nil {
		return fmt.Errorf("hlsmouflon: 找不到 ffmpeg: %w", err)
	}
	cmd := exec.Command(ffmpegPath, "-hide_banner", "-nostats", "-fflags", "+genpts", "-i", "pipe:0", "-c", "copy", "-y", file)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("hlsmouflon: 启动 ffmpeg 失败: %w", err)
	}
	if stderr != nil {
		go drainToLog(stderr, p.logger)
	}
	p.mu.Lock()
	p.cmd, p.stdin, p.outFile = cmd, stdin, file
	p.mu.Unlock()
	defer p.cleanup()

	p.logger.Infof("hlsmouflon 开始录制 model=%s -> %s", modelID, file)

	// 3) 轮询：解码真实分段 → 写入 ffmpeg
	seen := make(map[string]bool)
	initDone := false
	lastData := time.Now()
	started := false
	start := time.Now()
	var decFail int

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-p.stopCh:
			return nil
		default:
		}

		body, err := p.get(playlist)
		if err == nil {
			lines := strings.Split(string(body), "\n")
			if !initDone {
				if m := mapRe.FindStringSubmatch(string(body)); m != nil {
					if b, e := p.get(m[1]); e == nil {
						p.writeSeg(b)
						initDone = true
					}
				}
			}
			for _, l := range lines {
				l = strings.TrimSpace(l)
				if !strings.HasPrefix(l, "#EXT-X-MOUFLON:URI:") {
					continue
				}
				encURL := strings.TrimPrefix(l, "#EXT-X-MOUFLON:URI:")
				m := segRe.FindStringSubmatch(encURL)
				if m == nil {
					continue
				}
				realHash, ok := p.decode(m[1])
				if !ok {
					decFail++
					continue
				}
				realURL := strings.Replace(encURL, m[1], realHash, 1)
				if seen[realURL] {
					continue
				}
				seen[realURL] = true
				seg, e := p.get(realURL)
				if e != nil {
					decFail++
					continue
				}
				p.writeSeg(seg)
				started = true
				decFail = 0
				lastData = time.Now()
			}
		}

		// 启动期一直失败 → 密钥失效/未开播
		if !started && time.Since(start) > startupTO {
			return fmt.Errorf("hlsmouflon: %s 启动 %s 内未取到有效分段（未开播或 MOUFLON 密钥已轮换，可用 hls_keystream 覆盖）", modelID, startupTO)
		}
		// 已开录后长时间无新数据 → 下播
		if started && time.Since(lastData) > mediaIdleTO {
			p.logger.Infof("hlsmouflon: %s 已无新分段，判定下播", modelID)
			return nil
		}

		select {
		case <-ctx.Done():
			return nil
		case <-p.stopCh:
			return nil
		case <-time.After(pollInterval):
		}
	}
}

// decode 把 MOUFLON 加密的分段 hash 还原为真实 hash：
// realHash = XOR( base64decode(reverse(enc)), keystream(cycled) )
func (p *Parser) decode(enc string) (string, bool) {
	rev := reverseStr(enc)
	if pad := (4 - len(rev)%4) % 4; pad > 0 {
		rev += strings.Repeat("=", pad)
	}
	data, err := base64.StdEncoding.DecodeString(rev)
	if err != nil {
		if data, err = base64.URLEncoding.DecodeString(rev); err != nil {
			return "", false
		}
	}
	out := make([]byte, len(data))
	for i := range data {
		out[i] = data[i] ^ p.keystream[i%len(p.keystream)]
	}
	for _, c := range out {
		if c < 0x20 || c > 0x7e { // 真实 hash 应为可打印 ASCII
			return "", false
		}
	}
	return string(out), true
}

func (p *Parser) writeSeg(b []byte) {
	p.mu.Lock()
	if p.stdin != nil {
		n, _ := p.stdin.Write(b)
		p.written += int64(n)
	}
	p.mu.Unlock()
}

func (p *Parser) get(url string) ([]byte, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", defaultUA)
	req.Header.Set("Referer", referer)
	resp, err := p.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (p *Parser) Stop() error {
	p.closeOnce.Do(func() { close(p.stopCh) })
	p.cleanup()
	return nil
}

// cleanup 统一收尾（幂等）：关 stdin、回收 ffmpeg；几乎无数据则删碎片文件。
func (p *Parser) cleanup() {
	p.cleanupOnce.Do(func() {
		p.mu.Lock()
		cmd, stdin, out, written := p.cmd, p.stdin, p.outFile, p.written
		if p.stdin != nil {
			_ = p.stdin.Close()
			p.stdin = nil
		}
		_ = stdin
		p.mu.Unlock()
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
		if cmd != nil && out != "" && written < 64*1024 {
			if _, e := os.Stat(out); e == nil {
				_ = os.Remove(out)
				p.logger.Infof("hlsmouflon: 本次几乎无数据，已删除碎片文件 %s", out)
			}
		}
	})
}

func reverseStr(s string) string {
	r := []byte(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

func drainToLog(r io.Reader, logger *livelogger.LiveLogger) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			logger.Debugf("hlsmouflon ffmpeg: %s", string(buf[:n]))
		}
		if err != nil {
			return
		}
	}
}
