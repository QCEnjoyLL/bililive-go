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
//
// 维护与自愈：MOUFLON 是混淆而非真加密，keystream 全局恒定故硬编码默认值。站点一旦轮换密钥，
// 表现为所有房间启动 startupTO 内取不到有效分段（能连上播放列表但 decFail>0）。此时
// ParseLiveStream 自动调 bootstrapKeystream（见 bootstrap.go）：启一次无头浏览器播该房间，
// 截获浏览器请求的真实分段地址，与播放列表的加密地址按 msn 配对反推出新 keystream，缓存到
// AppDataPath/hls_keystream.txt 后继续录制；引导有 5 分钟防抖。若站点改的是算法（非仅密钥），
// 自愈也会失败 —— 退回 browser 引擎，并参考 yt-dlp / StreaMonitor 的 stripchat 实现跟进。
// 手动救急：配置 feature.hls_keystream（hex）/ hls_pkey 直接覆盖。
package hlsmouflon

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bililive-go/bililive-go/src/configs"
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

	pollInterval = 500 * time.Millisecond
	startupTO    = 40 * time.Second // 启动期一直拿不到有效分段则判定未开播/密钥失效

	// 下播判定：区分"网络抖动暂时无新段"与"真下播"。只要 playlist 仍能拉到就认为在播，
	// 不因短暂无新段误判（避免误判下播 → recorder 5s 后重录、切出多个短文件）。
	offlineFetchFail    = 6               // playlist 连续拉取失败次数（~9s）→ 判定真下播（房间结束/变体消失）
	mediaStuckTO        = 3 * time.Minute // playlist 仍可拉但超此时长无任何新分段 → 兜底判定卡死下播
	targetDisableTO     = 1 * time.Minute // CDN 不支持 _HLS_msn、超时或长期不命中时，临时退回普通 playlist
	playlistRequestTO   = 4 * time.Second // playlist 探测不能长时间拖住短滑窗调度
	normalProbeInflight = 1
	targetProbeInterval = 300 * time.Millisecond
	targetProbeInflight = 0
	playlistLoopIdle    = 50 * time.Millisecond
)

// segRe 匹配分段文件名 {id}_{msn}_{hash}_{ts}(_partN).mp4，提取 hash。
// MOUFLON hash 是 base64 风格字符串，可能包含 +、-、_ 等非字母数字字符。
var segRe = regexp.MustCompile(`_\d+_(.+)_\d+(?:_part\d+)?\.mp4`)
var segPartRe = regexp.MustCompile(`_(\d+)_(.+)_\d+(?:_part(\d+))?\.mp4`)
var mapRe = regexp.MustCompile(`#EXT-X-MAP:URI="([^"]+)"`)

func init() { parser.Register(Name, new(builder)) }

type builder struct{}

func (b *builder) Build(cfg map[string]string, logger *livelogger.LiveLogger) (parser.Parser, error) {
	pkey := defaultPkey
	if v := cfg["hls_pkey"]; v != "" {
		pkey = v
	}
	// keystream 读取优先级：显式配置 > 自愈引导缓存 > 硬编码默认
	ksHex := cfg["hls_keystream"]
	if ksHex == "" {
		ksHex = loadKeystreamCache()
	}
	if ksHex == "" {
		ksHex = defaultKeystreamHex
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
	variant := firstHTTPSLine(mbody)
	if variant == "" {
		return fmt.Errorf("hlsmouflon: master 中找不到变体地址")
	}
	playlist := buildMouflonPlaylistURL(variant, p.pkey, 0)

	// 2) ffmpeg：从 stdin 读 fMP4(init+分段)，-c copy 封装 .mkv
	ffmpegPath, err := utils.EnsureFFmpegPathForLive(ctx, liveObj)
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

	p.logger.Infof("hlsmouflon 开始录制 model=%s -> %s", modelID, file)

	// 3) 轮询 playlist 发现分段；下载、重试和顺序写入交给调度器。
	sched := newHLSSegmentScheduler(ctx, p.get)
	defer sched.stop()
	writeCh := make(chan []byte, 128)
	writeDone := make(chan struct{})
	var writeMu sync.Mutex
	var writeBlocked int
	var maxWriteMs int
	go func() {
		defer close(writeDone)
		for b := range writeCh {
			t0 := time.Now()
			p.writeSeg(b)
			elapsed := int(time.Since(t0).Milliseconds())
			writeMu.Lock()
			if elapsed > maxWriteMs {
				maxWriteMs = elapsed
			}
			writeMu.Unlock()
		}
	}()
	enqueueWrite := func(b []byte) {
		select {
		case writeCh <- b:
		default:
			writeMu.Lock()
			writeBlocked++
			writeMu.Unlock()
			writeCh <- b
		}
	}
	snapshotWrites := func(reset bool) (int, int, int) {
		writeMu.Lock()
		blocked, maxMs := writeBlocked, maxWriteMs
		if reset {
			writeBlocked, maxWriteMs = 0, 0
		}
		writeMu.Unlock()
		return len(writeCh), blocked, maxMs
	}
	probeCtx, cancelProbes := context.WithCancel(ctx)
	defer cancelProbes()
	normalResults := make(chan hlsPlaylistResult, 64)
	targetResults := make(chan hlsPlaylistResult, 64)
	var normalInflight int
	targetInflight := map[int]bool{}
	var lastNormalProbe time.Time
	var lastTargetProbe time.Time
	initDone := false
	lastData := time.Now()
	started := false
	start := time.Now()
	var decFail int
	triedBootstrap := false
	var fetchFail int // 普通 playlist 连续拉取失败计数（区分真下播 vs 网络抖动）
	var lastDiagGaps int
	var targetFailures int
	var targetRequests int
	var targetFallbacks int
	var targetHits int
	var targetMisses int
	var targetPending int
	var targetMissStreak int
	var targetDisabledUntil time.Time
	var playlistFullRefs int
	var playlistPartRefs int
	var normalRequests int
	var normalFailures int
	var normalEmpty int
	var normalMaxMs int
	var lastDiagWritten int
	var lastDiagPlaylistMSN int
	var lastDiagWriteWaits int
	diagAt := time.Now()
	flushWritable := func(force bool) {
		var writable []hlsWritableSegment
		if force {
			writable = sched.takeWritableFinal(time.Now())
		} else {
			writable = sched.takeWritable(time.Now())
		}
		for _, seg := range writable {
			enqueueWrite(seg.body)
			started = true
			decFail = 0
			lastData = time.Now()
		}
	}
	defer func() {
		flushWritable(true)
		close(writeCh)
		select {
		case <-writeDone:
		case <-time.After(15 * time.Second):
			p.logger.Warnf("hlsmouflon: 等待 ffmpeg 写入队列排空超时，强制收尾")
		}
		p.cleanup()
	}()

	for {
		select {
		case <-ctx.Done():
			flushWritable(true)
			return nil
		case <-p.stopCh:
			flushWritable(true)
			return nil
		default:
		}

		now := time.Now()
		processPlaylistResult := func(result hlsPlaylistResult) {
			if result.request.targetMSN > 0 {
				delete(targetInflight, result.request.targetMSN)
			} else if normalInflight > 0 {
				normalInflight--
			}
			if result.err != nil {
				if result.request.targetMSN > 0 {
					targetFailures++
					if shouldDisableTargetPlaylist(result.err) || targetFailures >= 3 {
						targetDisabledUntil = time.Now().Add(targetDisableTO)
						p.logger.Warnf("hlsmouflon: 定向 playlist 请求 _HLS_msn=%d 失败（%v），临时退回普通 playlist %s", result.request.targetMSN, result.err, targetDisableTO)
						targetFailures = 0
						targetFallbacks++
					}
				} else {
					normalFailures++
					fetchFail++
				}
				return
			}

			if result.request.targetMSN > 0 {
				targetFailures = 0
			} else {
				fetchFail = 0
				if result.elapsedMs > normalMaxMs {
					normalMaxMs = result.elapsedMs
				}
			}
			if !initDone {
				if m := mapRe.FindStringSubmatch(string(result.body)); m != nil {
					if b, e := p.get(m[1]); e == nil {
						enqueueWrite(b)
						initDone = true
					}
				}
			}
			segs, failedDecode := parseMouflonSegments(result.body, p.decode)
			segs, fullRefs, partRefs := preferCompleteMouflonSegments(segs)
			if result.request.targetMSN == 0 && len(segs) == 0 {
				normalEmpty++
			}
			feedSegments := true
			if result.request.targetMSN > 0 {
				if len(segs) == 0 {
					targetPending++
					targetMissStreak = 0
					feedSegments = false
				} else {
					switch classifyTargetPlaylist(segs, result.request.targetMSN) {
					case targetPlaylistHit:
						targetHits++
						targetMissStreak = 0
						segs = hlsSegmentsForMSN(segs, result.request.targetMSN)
						fullRefs, partRefs = countMouflonSegmentTypes(segs)
					case targetPlaylistPending:
						targetPending++
						targetMissStreak = 0
						feedSegments = false
					case targetPlaylistMiss:
						targetMisses++
						targetMissStreak++
						feedSegments = false
					}
				}
			}
			if feedSegments {
				playlistFullRefs += fullRefs
				playlistPartRefs += partRefs
			}
			decFail += failedDecode
			if feedSegments {
				if result.request.targetMSN > 0 {
					sched.addTarget(segs)
				} else {
					sched.add(segs)
				}
				flushWritable(false)
			}
		}

		drainPlaylistResults := func() {
			for {
				select {
				case result := <-normalResults:
					processPlaylistResult(result)
				case result := <-targetResults:
					processPlaylistResult(result)
				default:
					return
				}
			}
		}

		drainPlaylistResults()
		startNormalProbe := func() {
			if normalInflight >= normalProbeInflight {
				return
			}
			if !lastNormalProbe.IsZero() && now.Sub(lastNormalProbe) < pollInterval {
				return
			}
			req := hlsPlaylistRequest{url: playlist}
			normalInflight++
			lastNormalProbe = now
			normalRequests++
			go func() {
				reqCtx, cancel := context.WithTimeout(probeCtx, playlistRequestTO)
				defer cancel()
				t0 := time.Now()
				body, err := p.getPlaylistWithContext(reqCtx, req.url)
				result := hlsPlaylistResult{
					request:   req,
					body:      body,
					err:       err,
					elapsedMs: int(time.Since(t0).Milliseconds()),
				}
				select {
				case normalResults <- result:
				case <-probeCtx.Done():
				}
			}()
		}
		startTargetProbe := func(msn int) {
			if msn <= 0 || targetInflight[msn] || len(targetInflight) >= targetProbeInflight {
				return
			}
			if !lastTargetProbe.IsZero() && now.Sub(lastTargetProbe) < targetProbeInterval {
				return
			}
			req := hlsPlaylistRequest{
				url:       buildMouflonPlaylistURL(variant, p.pkey, msn),
				targetMSN: msn,
			}
			targetInflight[msn] = true
			lastTargetProbe = now
			targetRequests++
			go func() {
				reqCtx, cancel := context.WithTimeout(probeCtx, playlistRequestTO)
				defer cancel()
				t0 := time.Now()
				body, err := p.getTargetPlaylistWithContext(reqCtx, req.url)
				result := hlsPlaylistResult{
					request:   req,
					body:      body,
					err:       err,
					elapsedMs: int(time.Since(t0).Milliseconds()),
				}
				select {
				case targetResults <- result:
				case <-probeCtx.Done():
				}
			}()
		}
		startNormalProbe()
		if started && now.After(targetDisabledUntil) {
			for _, msn := range sched.targetProbeMSNs(targetProbeInflight) {
				startTargetProbe(msn)
			}
		}
		drainPlaylistResults()
		if targetMissStreak >= 20 {
			p.logger.Warnf("hlsmouflon: 定向 playlist 连续 %d 次返回已越过目标 msn 但未包含，继续探测补洞", targetMissStreak)
			targetMissStreak = 0
		}
		flushWritable(false)

		// 启动期一直失败：若能连上播放列表却解不出分段(decFail>0)，多半是 keystream 失效，
		// 用无头浏览器引导一次新密钥并继续；纯未开播(decFail==0)或引导失败则返回。
		if !started && time.Since(start) > startupTO {
			if decFail > 0 && !triedBootstrap {
				triedBootstrap = true
				if newKs, err := bootstrapKeystream(ctx, liveObj, modelID, p.pkey, p.logger); err == nil {
					p.keystream = newKs
					saveKeystreamCache(newKs, p.logger)
					sched.reset()
					decFail, start = 0, time.Now()
					p.logger.Infof("hlsmouflon: 已应用引导出的新 keystream，继续录制 %s", modelID)
					continue
				} else {
					p.logger.Warnf("hlsmouflon: keystream 引导失败: %v", err)
				}
			}
			return fmt.Errorf("hlsmouflon: %s 启动 %s 内未取到有效分段（未开播，或 MOUFLON 密钥已轮换且自动引导失败；可手动配置 hls_keystream 覆盖）", modelID, startupTO)
		}
		// 下播判定（区分真下播 vs 网络抖动）：
		//  - playlist 连续拉取失败 → 房间结束/变体消失，判定真下播
		//  - playlist 仍可拉但超久无任何新分段 → 兜底判定卡死
		// 只要 playlist 还能拉到、只是暂时没有新段，就继续轮询、不误判下播（不触发重录）。
		if started && fetchFail >= offlineFetchFail {
			p.logger.Infof("hlsmouflon: %s playlist 连续 %d 次拉取失败，判定下播", modelID, fetchFail)
			flushWritable(true)
			return nil
		}
		if started && time.Since(lastData) > mediaStuckTO {
			p.logger.Infof("hlsmouflon: %s 超过 %s 无新分段（playlist 仍在但卡死），判定下播", modelID, mediaStuckTO)
			flushWritable(true)
			return nil
		}
		// 诊断输出（每 ~30s）：丢段统计帮助判断"跟不上丢段"还是"网络波动"
		if started && time.Since(diagAt) > 30*time.Second {
			st := sched.snapshot(true)
			gapDelta := st.gaps - lastDiagGaps
			lastDiagGaps = st.gaps
			targetState := "启用"
			if targetProbeInflight <= 0 {
				targetState = "禁用"
			} else if time.Now().Before(targetDisabledUntil) {
				targetState = "回退"
			}
			writeQueue, writeBlocked, writeMaxMs := snapshotWrites(true)
			p.logger.Infof("hlsmouflon 诊断 %s：累计写入 %d 段，新发现 %d 段，确认丢段本周期 %d/累计 %d，疑似漏看本周期 %d/累计 %d，下载失败 %d，重试成功 %d，下载队列 %d，写入队列 %d，写入等待 %d，写入阻塞 %d，当前 msn=%d，playlist最新msn=%d，liveLag=%d，窗口=%d-%d/%d 段，playlist完整/part %d/%d，普通playlist 请求/失败/空/最大耗时 %d/%d/%d/%dms，定向playlist=%s 请求/命中/待出/未中/回退 %d/%d/%d/%d/%d，本周期最大单段下载 %dms，最大单段写入 %dms",
				modelID, st.written, st.discovered, gapDelta, st.gaps, st.suspectedMissed, st.suspectedTotal, st.downloadFailures, st.retrySuccess, st.queued, writeQueue, st.writeWaits, writeBlocked, st.currentMSN, st.lastSeenMSN, st.liveLagMSN, st.windowMinMSN, st.windowMaxMSN, st.windowSegments, playlistFullRefs, playlistPartRefs, normalRequests, normalFailures, normalEmpty, normalMaxMs, targetState, targetRequests, targetHits, targetPending, targetMisses, targetFallbacks, st.maxDownloadMs, writeMaxMs)
			periodMSNGrowth := 0
			if lastDiagPlaylistMSN > 0 && st.lastSeenMSN >= lastDiagPlaylistMSN {
				periodMSNGrowth = st.lastSeenMSN - lastDiagPlaylistMSN
			}
			periodWritten := 0
			if st.written >= lastDiagWritten {
				periodWritten = st.written - lastDiagWritten
			}
			writeWaitDelta := 0
			if st.writeWaits >= lastDiagWriteWaits {
				writeWaitDelta = st.writeWaits - lastDiagWriteWaits
			}
			p.logger.Infof("hlsmouflon 监视 %s：%s",
				modelID, hlsMonitorSummary(st, periodMSNGrowth, periodWritten, writeWaitDelta, targetState))
			lastDiagWritten = st.written
			lastDiagPlaylistMSN = st.lastSeenMSN
			lastDiagWriteWaits = st.writeWaits
			targetRequests, targetHits, targetPending, targetMisses, targetFallbacks = 0, 0, 0, 0, 0
			normalRequests, normalFailures, normalEmpty, normalMaxMs = 0, 0, 0, 0
			playlistFullRefs, playlistPartRefs = 0, 0
			diagAt = time.Now()
		}

		select {
		case <-ctx.Done():
			flushWritable(true)
			return nil
		case <-p.stopCh:
			flushWritable(true)
			return nil
		case <-time.After(playlistLoopIdle):
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

func (p *Parser) get(url string) ([]byte, error) { return fetch(p.hc, url) }

func (p *Parser) getPlaylist(rawURL string) ([]byte, error) {
	return p.getPlaylistWithContext(context.Background(), rawURL)
}

func (p *Parser) getPlaylistWithContext(ctx context.Context, rawURL string) ([]byte, error) {
	return fetchWithCacheMode(ctx, p.hc, rawURL, false)
}

func (p *Parser) getTargetPlaylistWithContext(ctx context.Context, rawURL string) ([]byte, error) {
	return fetchWithCacheMode(ctx, p.hc, rawURL, false)
}

type hlsPlaylistRequest struct {
	url       string
	targetMSN int
}

type hlsPlaylistResult struct {
	request   hlsPlaylistRequest
	body      []byte
	err       error
	elapsedMs int
}

func (p *Parser) fetchPlaylistBatch(ctx context.Context, requests []hlsPlaylistRequest) []hlsPlaylistResult {
	if len(requests) == 0 {
		return nil
	}
	batchCtx, cancel := context.WithTimeout(ctx, playlistRequestTO)
	defer cancel()

	if len(requests) == 1 {
		body, err := p.getPlaylistWithContext(batchCtx, requests[0].url)
		return []hlsPlaylistResult{{request: requests[0], body: body, err: err}}
	}

	ch := make(chan hlsPlaylistResult, len(requests))
	for _, req := range requests {
		req := req
		go func() {
			body, err := p.getPlaylistWithContext(batchCtx, req.url)
			ch <- hlsPlaylistResult{request: req, body: body, err: err}
		}()
	}

	results := make([]hlsPlaylistResult, 0, len(requests))
	for range requests {
		results = append(results, <-ch)
	}
	return results
}

type httpStatusError struct {
	code int
}

func (e *httpStatusError) Error() string { return fmt.Sprintf("HTTP %d", e.code) }

func buildMouflonPlaylistURL(variant, pkey string, targetMSN int) string {
	u, err := url.Parse(variant)
	if err != nil {
		if targetMSN > 0 {
			return fmt.Sprintf("%s?psch=v2&pkey=%s&_HLS_msn=%d", variant, url.QueryEscape(pkey), targetMSN)
		}
		return fmt.Sprintf("%s?psch=v2&pkey=%s", variant, url.QueryEscape(pkey))
	}
	q := u.Query()
	q.Set("psch", "v2")
	q.Set("pkey", pkey)
	if targetMSN > 0 {
		q.Set("_HLS_msn", strconv.Itoa(targetMSN))
	} else {
		q.Del("_HLS_msn")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func withPlaylistCacheBuster(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		sep := "?"
		if strings.Contains(rawURL, "?") {
			sep = "&"
		}
		return fmt.Sprintf("%s%s_bgo_reload=%d", rawURL, sep, time.Now().UnixNano())
	}
	q := u.Query()
	q.Set("_bgo_reload", strconv.FormatInt(time.Now().UnixNano(), 10))
	u.RawQuery = q.Encode()
	return u.String()
}

func shouldDisableTargetPlaylist(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var statusErr *httpStatusError
	if errors.As(err, &statusErr) {
		switch statusErr.code {
		case http.StatusBadRequest, http.StatusNotFound, http.StatusPreconditionFailed, http.StatusRequestedRangeNotSatisfiable:
			return true
		}
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

type targetPlaylistOutcome int

const (
	targetPlaylistHit targetPlaylistOutcome = iota
	targetPlaylistPending
	targetPlaylistMiss
)

func classifyTargetPlaylist(segs []hlsSegmentRef, targetMSN int) targetPlaylistOutcome {
	if targetMSN <= 0 || len(segs) == 0 {
		return targetPlaylistPending
	}
	_, maxMSN := hlsSegmentMSNRange(segs)
	if hlsSegmentsContainMSN(segs, targetMSN) {
		return targetPlaylistHit
	}
	if maxMSN < targetMSN {
		return targetPlaylistPending
	}
	return targetPlaylistMiss
}

func hlsSegmentMSNRange(segs []hlsSegmentRef) (int, int) {
	if len(segs) == 0 {
		return 0, 0
	}
	minMSN, maxMSN := segs[0].key.msn, segs[0].key.msn
	for _, seg := range segs[1:] {
		if seg.key.msn < minMSN {
			minMSN = seg.key.msn
		}
		if seg.key.msn > maxMSN {
			maxMSN = seg.key.msn
		}
	}
	return minMSN, maxMSN
}

func hlsSegmentsContainMSN(segs []hlsSegmentRef, msn int) bool {
	for _, seg := range segs {
		if seg.key.msn == msn {
			return true
		}
	}
	return false
}

func hlsSegmentsForMSN(segs []hlsSegmentRef, msn int) []hlsSegmentRef {
	filtered := make([]hlsSegmentRef, 0, len(segs))
	for _, seg := range segs {
		if seg.key.msn == msn {
			filtered = append(filtered, seg)
		}
	}
	return filtered
}

func countMouflonSegmentTypes(segs []hlsSegmentRef) (int, int) {
	fullCount, partCount := 0, 0
	for _, seg := range segs {
		if seg.partial {
			partCount++
		} else {
			fullCount++
		}
	}
	return fullCount, partCount
}

func hlsMonitorSummary(st hlsSegmentStats, periodMSNGrowth, periodWritten, writeWaitDelta int, targetState string) string {
	expected := periodMSNGrowth
	if expected <= 0 {
		expected = st.discovered + st.suspectedMissed
	}
	coverageText := "覆盖率=未知"
	if expected > 0 {
		coverage := float64(st.discovered) * 100 / float64(expected)
		coverageText = fmt.Sprintf("覆盖率=%.0f%%（新发现 %d/预估新增 %d，疑似漏看 %d）", coverage, st.discovered, expected, st.suspectedMissed)
	}

	risks := make([]string, 0, 4)
	if st.suspectedMissed > 0 {
		risks = append(risks, "playlist滑窗漏段")
	}
	if st.windowSegments <= 2 && st.liveLagMSN <= 3 {
		risks = append(risks, "窗口过短且贴近直播边缘")
	}
	if targetState == "回退" && st.suspectedMissed > 0 {
		risks = append(risks, "定向playlist回退中")
	}
	if st.maxDownloadMs >= 3000 {
		risks = append(risks, "单段下载慢")
	}
	if writeWaitDelta >= 20 {
		risks = append(risks, "写入等待偏多")
	}
	if len(risks) == 0 {
		risks = append(risks, "未见明显异常")
	}

	return fmt.Sprintf("%s，写入本周期 %d 段，liveLag=%d，窗口=%d-%d/%d 段，下载最大=%dms，写入等待+%d，风险=%s",
		coverageText, periodWritten, st.liveLagMSN, st.windowMinMSN, st.windowMaxMSN, st.windowSegments, st.maxDownloadMs, writeWaitDelta, strings.Join(risks, "、"))
}

// fetch 带站点 UA/Referer 拉取 url，非 200 视为错误。包级以便引导逻辑(bootstrap.go)复用。
func fetch(hc *http.Client, url string) ([]byte, error) {
	return fetchWithCacheMode(context.Background(), hc, url, false)
}

func fetchWithNoCache(hc *http.Client, url string) ([]byte, error) {
	return fetchWithCacheMode(context.Background(), hc, url, true)
}

func fetchWithNoCacheContext(ctx context.Context, hc *http.Client, url string) ([]byte, error) {
	return fetchWithCacheMode(ctx, hc, url, true)
}

func fetchWithCacheMode(ctx context.Context, hc *http.Client, url string, noCache bool) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", defaultUA)
	req.Header.Set("Referer", referer)
	if noCache {
		req.Header.Set("Cache-Control", "no-cache")
		req.Header.Set("Pragma", "no-cache")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, &httpStatusError{code: resp.StatusCode}
	}
	return io.ReadAll(resp.Body)
}

// firstHTTPSLine 返回 m3u8 中第一条 https 开头的行（master 里即变体地址）。
func firstHTTPSLine(body []byte) string {
	for _, l := range strings.Split(string(body), "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "https") {
			return l
		}
	}
	return ""
}

// keystream 自愈缓存：引导出的新 keystream 持久化到 app 数据目录，下次启动直接复用，
// 免去再次引导。无法确定数据目录（如测试环境）则静默跳过，回退默认/配置值。
func keystreamCachePath() string {
	cfg := configs.GetCurrentConfig()
	if cfg == nil || cfg.AppDataPath == "" {
		return ""
	}
	return filepath.Join(cfg.AppDataPath, "hls_keystream.txt")
}

func loadKeystreamCache() string {
	p := keystreamCachePath()
	if p == "" {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func saveKeystreamCache(ks []byte, logger *livelogger.LiveLogger) {
	p := keystreamCachePath()
	if p == "" {
		return
	}
	if err := os.WriteFile(p, []byte(hex.EncodeToString(ks)), 0o644); err != nil {
		logger.Warnf("hlsmouflon: 写 keystream 缓存失败: %v", err)
		return
	}
	logger.Infof("hlsmouflon: 新 keystream 已缓存到 %s", p)
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
