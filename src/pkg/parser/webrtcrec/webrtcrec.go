// Package webrtcrec 实现一个把 doppiocdn / Flashphoner-WCS 风格 WebRTC 直播流
// 录制为文件的 parser，注册名 "webrtc"。
//
// 工作流程（已通过抓包逆向并实测验证）：
//  1. WS 连接 wss://edge-webrtc.doppiocdn.com/，发送 connection；
//  2. 从 connected 取每会话下发的 TURN 凭据；
//  3. getStreamInfo -> playStream(携带本方 SDP offer) -> setRemoteSDP(服务端 answer)；
//  4. pion 以 relay/TURN 建连，OnTrack 收 H264 视频 + Opus 音频 RTP；
//  5. 把 RTP 转发到本地 UDP，交给 ffmpeg（读 SDP）混流为 MKV。
//
// 输入 URL 形如 webrtc://edge-webrtc.doppiocdn.com/{modelId}?quality={preset}，
// 由 boyfriend 平台模块的 GetStreamInfos 产生。
package webrtcrec

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
	uuid "github.com/satori/go.uuid"

	"github.com/bililive-go/bililive-go/src/live"
	"github.com/bililive-go/bililive-go/src/pkg/livelogger"
	"github.com/bililive-go/bililive-go/src/pkg/parser"
	bilisentry "github.com/bililive-go/bililive-go/src/pkg/sentry"
	"github.com/bililive-go/bililive-go/src/pkg/utils"
)

const (
	Name = "webrtc"

	defaultUA   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	wsURL       = "wss://edge-webrtc.doppiocdn.com/"
	wsOrigin    = "https://zh.boyfriend.show"
	mediaIdleTO = 20 * time.Second // 超过此时长无媒体则判定流结束
	handshakeTO = 25 * time.Second // 信令+建连超时
)

func init() {
	parser.Register(Name, new(builder))
}

type builder struct{}

func (b *builder) Build(cfg map[string]string, logger *livelogger.LiveLogger) (parser.Parser, error) {
	return &Parser{
		closeOnce:  new(sync.Once),
		stopCh:     make(chan struct{}),
		statusReq:  make(chan struct{}, 1),
		statusResp: make(chan map[string]interface{}, 1),
		audioOnly:  cfg["audio_only"] == "true",
		logger:     logger,
	}, nil
}

type Parser struct {
	logger    *livelogger.LiveLogger
	audioOnly bool

	closeOnce *sync.Once
	stopCh    chan struct{}

	mu        sync.Mutex
	cmd       *exec.Cmd
	cmdStdIn  io.WriteCloser
	cmdStdout io.ReadCloser
	pc        *webrtc.PeerConnection
	ws        *websocket.Conn
	sdpFile   string

	lastMediaUnixNano int64

	statusReq  chan struct{}
	statusResp chan map[string]interface{}
}

// ---- WS 信令报文 ----

type wsMsg struct {
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type connectedData struct {
	PeerConfig struct {
		ICEServers []struct {
			URLs       []string `json:"urls"`
			Username   string   `json:"username"`
			Credential string   `json:"credential"`
		} `json:"iceServers"`
		ICETransportPolicy string `json:"iceTransportPolicy"`
	} `json:"peerConfig"`
}

func (p *Parser) ParseLiveStream(ctx context.Context, streamUrlInfo *live.StreamUrlInfo, liveObj live.Live, file string) error {
	modelID, quality, err := parseWebRTCURL(streamUrlInfo.Url)
	if err != nil {
		return err
	}
	headers := streamUrlInfo.HeadersForDownloader

	ffmpegPath, err := utils.GetFFmpegPathForLive(ctx, liveObj)
	if err != nil {
		return err
	}

	// 选两个空闲 UDP 端口给 ffmpeg 监听（视频/音频各一）。
	videoPort, err := freeUDPPort()
	if err != nil {
		return fmt.Errorf("分配视频UDP端口失败: %w", err)
	}
	audioPort, err := freeUDPPort()
	if err != nil {
		return fmt.Errorf("分配音频UDP端口失败: %w", err)
	}

	// 1) WS 握手
	reqHeader := http.Header{}
	reqHeader.Set("Origin", wsOrigin)
	reqHeader.Set("User-Agent", headerOr(headers, "User-Agent", defaultUA))
	if ck := headers["Cookie"]; ck != "" {
		reqHeader.Set("Cookie", ck)
	}
	ws, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, reqHeader)
	if err != nil {
		return fmt.Errorf("WebRTC 信令连接失败: %w", err)
	}
	p.mu.Lock()
	p.ws = ws
	p.mu.Unlock()
	defer p.closeWS()
	defer func() {
		p.mu.Lock()
		f := p.sdpFile
		p.mu.Unlock()
		if f != "" {
			os.Remove(f)
		}
	}()

	sid, _ := uuid.NewV4()
	sessionID := sid.String()
	send := func(message string, data any) error {
		b, _ := json.Marshal(map[string]any{"message": message, "data": data})
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.ws == nil {
			return fmt.Errorf("ws closed")
		}
		return p.ws.WriteMessage(websocket.TextMessage, b)
	}

	if err := send("connection", []any{map[string]any{
		"appKey":               "callbackApp",
		"clientBrowserVersion": headerOr(headers, "User-Agent", defaultUA),
		"clientOSVersion":      "5.0 (Windows NT 10.0; Win64; x64)",
		"mediaProviders":       []string{"WebRTC"},
		"custom":               map[string]any{"name": modelID, "aclAuth": ""},
	}}); err != nil {
		return err
	}

	// WS 读循环
	msgCh := make(chan wsMsg, 64)
	wsErrCh := make(chan error, 1)
	go func() {
		for {
			_, raw, rerr := ws.ReadMessage()
			if rerr != nil {
				wsErrCh <- rerr
				return
			}
			var m wsMsg
			if json.Unmarshal(raw, &m) == nil {
				msgCh <- m
			}
		}
	}()

	// 2~4) 信令状态机 + pion 建连，直到媒体到达后启动 ffmpeg
	var (
		pc           *webrtc.PeerConnection
		pendingOffer string
		tracksReady  int32
		videoPT      = int32(102)
		audioPT      = int32(111)
		audioClock   = int32(48000)
		audioCh      = int32(2)
	)
	ffmpegStarted := make(chan error, 1)
	var startOnce sync.Once
	pcFailed := make(chan struct{}, 1)

	tryStartFFmpeg := func() {
		startOnce.Do(func() {
			sdp := buildSDP(videoPort, audioPort, int(atomic.LoadInt32(&videoPT)),
				int(atomic.LoadInt32(&audioPT)), int(atomic.LoadInt32(&audioClock)), int(atomic.LoadInt32(&audioCh)))
			ffmpegStarted <- p.startFFmpeg(ctx, ffmpegPath, sdp, file)
		})
	}

	handshakeTimer := time.NewTimer(handshakeTO)
	defer handshakeTimer.Stop()

signalingLoop:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.stopCh:
			return nil
		case <-handshakeTimer.C:
			return fmt.Errorf("WebRTC 建连超时（未在 %s 内收到媒体）", handshakeTO)
		case rerr := <-wsErrCh:
			return fmt.Errorf("WebRTC 信令中断: %w", rerr)
		case startErr := <-ffmpegStarted:
			if startErr != nil {
				return startErr
			}
			break signalingLoop // ffmpeg 已启动，转入录制阶段
		case m := <-msgCh:
			switch m.Message {
			case "connected":
				var cd connectedData
				_ = json.Unmarshal(m.Data, &cd)
				pc, err = p.newPeerConnection(cd, &tracksReady, &videoPT, &audioPT, &audioClock, &audioCh, videoPort, audioPort, tryStartFFmpeg, pcFailed)
				if err != nil {
					return fmt.Errorf("创建 PeerConnection 失败: %w", err)
				}
				p.mu.Lock()
				p.pc = pc
				p.mu.Unlock()
				offer, oerr := pc.CreateOffer(nil)
				if oerr != nil {
					return oerr
				}
				if oerr = pc.SetLocalDescription(offer); oerr != nil {
					return oerr
				}
				pendingOffer = offer.SDP
				if serr := send("getStreamInfo", map[string]any{"mediaSessionId": sessionID, "modelName": modelID}); serr != nil {
					return serr
				}
			case "getStreamInfoReply":
				// 始终请求音视频两路轨道；纯音频模式由 ffmpeg -vn 在混流阶段丢弃视频，
				// 这样轨道数恒为 2，便于统一触发 ffmpeg 启动。
				if serr := send("playStream", []any{map[string]any{
					"hasAudio": true, "hasVideo": true, "mediaProvider": "WebRTC",
					"mediaSessionId": sessionID, "name": modelID, "quality": quality,
					"published": false, "record": false, "sdp": pendingOffer,
				}}); serr != nil {
					return serr
				}
			case "setRemoteSDP":
				var arr []json.RawMessage
				if json.Unmarshal(m.Data, &arr) == nil && len(arr) >= 2 && pc != nil {
					var answer string
					_ = json.Unmarshal(arr[1], &answer)
					if derr := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer}); derr != nil {
						return fmt.Errorf("SetRemoteDescription 失败: %w", derr)
					}
				}
			}
		}
	}

	// 录制阶段：等待 ffmpeg 退出 / 停止 / 流结束
	p.markMedia()
	bilisentry.Go(p.scheduler)
	idle := time.NewTicker(5 * time.Second)
	defer idle.Stop()
	waitCh := make(chan error, 1)
	go func() {
		p.mu.Lock()
		cmd := p.cmd
		p.mu.Unlock()
		if cmd != nil {
			waitCh <- cmd.Wait()
		} else {
			waitCh <- fmt.Errorf("ffmpeg 未启动")
		}
	}()

	for {
		select {
		case <-ctx.Done():
			p.Stop()
			<-waitCh
			return ctx.Err()
		case <-p.stopCh:
			<-waitCh
			return nil
		case err := <-waitCh:
			// ffmpeg 退出（正常停止返回 nil，或被 Stop 杀掉）
			return err
		case rerr := <-wsErrCh:
			p.logger.Debugf("WebRTC 信令在录制中断开: %v", rerr)
			p.Stop()
			<-waitCh
			return nil
		case <-pcFailed:
			p.logger.Info("WebRTC 连接断开，结束本次录制")
			p.Stop()
			<-waitCh
			return nil
		case <-idle.C:
			if time.Since(time.Unix(0, atomic.LoadInt64(&p.lastMediaUnixNano))) > mediaIdleTO {
				p.logger.Info("WebRTC 长时间无媒体数据，判定流结束")
				p.Stop()
				<-waitCh
				return nil
			}
		}
	}
}

func (p *Parser) newPeerConnection(cd connectedData, tracksReady, videoPT, audioPT, audioClock, audioCh *int32,
	videoPort, audioPort int, tryStartFFmpeg func(), pcFailed chan struct{}) (*webrtc.PeerConnection, error) {

	cfg := webrtc.Configuration{ICETransportPolicy: webrtc.ICETransportPolicyRelay}
	for _, s := range cd.PeerConfig.ICEServers {
		cfg.ICEServers = append(cfg.ICEServers, webrtc.ICEServer{
			URLs: s.URLs, Username: s.Username, Credential: s.Credential,
		})
	}

	pc, err := webrtc.NewPeerConnection(cfg)
	if err != nil {
		return nil, err
	}
	if _, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		pc.Close()
		return nil, err
	}
	if _, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		pc.Close()
		return nil, err
	}

	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		p.logger.Debugf("WebRTC PC 状态: %s", s)
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed || s == webrtc.PeerConnectionStateDisconnected {
			select {
			case pcFailed <- struct{}{}:
			default:
			}
		}
	})

	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		codec := track.Codec()
		p.logger.Infof("WebRTC 收到轨道: kind=%s codec=%s pt=%d", track.Kind(), codec.MimeType, codec.PayloadType)

		port := audioPort
		if track.Kind() == webrtc.RTPCodecTypeVideo {
			atomic.StoreInt32(videoPT, int32(codec.PayloadType))
			port = videoPort
			// 周期性 PLI 请求关键帧，确保 ffmpeg 尽快拿到 SPS/PPS+IDR
			go func() {
				// 立即请求一次关键帧，缩短拿到 SPS（分辨率）的时间
				_ = pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}})
				t := time.NewTicker(2 * time.Second)
				defer t.Stop()
				for {
					select {
					case <-p.stopCh:
						return
					case <-t.C:
						_ = pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}})
					}
				}
			}()
		} else {
			atomic.StoreInt32(audioPT, int32(codec.PayloadType))
			atomic.StoreInt32(audioClock, int32(codec.ClockRate))
			if codec.Channels > 0 {
				atomic.StoreInt32(audioCh, int32(codec.Channels))
			}
		}

		// 两条轨道都到齐后再启动 ffmpeg（此时 PT 已确定）
		if atomic.AddInt32(tracksReady, 1) >= 2 {
			tryStartFFmpeg()
		}

		conn, derr := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", port))
		if derr != nil {
			p.logger.Warnf("连接本地 UDP 失败: %v", derr)
			return
		}
		defer conn.Close()
		// 增大本地 UDP 发送缓冲
		if uc, ok := conn.(*net.UDPConn); ok {
			_ = uc.SetWriteBuffer(8 << 20)
		}
		// 解耦：track 读取与 UDP 写入分离，避免写入抖动反压导致 pion 侧丢包。
		// 单写协程保证 FIFO 顺序，通道足够大，localhost 写入极快，几乎不会丢。
		pktCh := make(chan []byte, 4096)
		go func() {
			for b := range pktCh {
				_, _ = conn.Write(b)
			}
		}()
		buf := make([]byte, 1600)
		for {
			n, _, rerr := track.Read(buf)
			if rerr != nil {
				close(pktCh)
				return
			}
			p.markMedia()
			pkt := make([]byte, n)
			copy(pkt, buf[:n])
			select {
			case pktCh <- pkt:
			default: // 通道满（极罕见）时丢弃，优先保证 track 读取不被阻塞
			}
		}
	})

	return pc, nil
}

// startFFmpeg 启动 ffmpeg 读取本地 RTP（经 SDP 描述）并混流写入 file。
func (p *Parser) startFFmpeg(ctx context.Context, ffmpegPath, sdp, file string) error {
	sdpFile, err := os.CreateTemp("", "boyfriend-*.sdp")
	if err != nil {
		return fmt.Errorf("创建临时 SDP 失败: %w", err)
	}
	if _, err = sdpFile.WriteString(sdp); err != nil {
		sdpFile.Close()
		return err
	}
	sdpFile.Close()

	args := []string{
		"-hide_banner", "-nostats",
		"-progress", "-",
		"-protocol_whitelist", "file,udp,rtp",
		// 让 ffmpeg 在写 Matroska 头前充分分析输入，确保已从 H264 关键帧解析出
		// SPS（分辨率），否则会报 "dimensions not set / Could not write header"。
		"-analyzeduration", "10000000",
		"-probesize", "10000000",
		// 抗丢包/乱序：增大 UDP 接收缓冲（关键帧大量分片时不溢出）、增大 RTP 重排
		// 队列与最大等待时延，给乱序/NACK 重传到达的包留出被使用的时间窗，显著减少花屏。
		"-buffer_size", "33554432",
		"-reorder_queue_size", "4096",
		"-max_delay", "5000000",
		"-fflags", "+genpts",
		"-i", sdpFile.Name(),
	}
	if p.audioOnly {
		args = append(args, "-vn")
	}
	args = append(args, "-c", "copy", "-y", file)

	p.mu.Lock()
	defer p.mu.Unlock()
	cmd := exec.Command(ffmpegPath, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.MultiWriter(utils.NewLogFilterWriter(os.Stderr), utils.NewLoggerWriter(p.logger))
	if err = cmd.Start(); err != nil {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		os.Remove(sdpFile.Name())
		return err
	}
	p.cmd = cmd
	p.cmdStdIn = stdin
	p.cmdStdout = stdout
	p.sdpFile = sdpFile.Name()
	p.logger.Infof("ffmpeg 已启动（WebRTC -> %s）", file)
	return nil
}

func (p *Parser) markMedia() {
	atomic.StoreInt64(&p.lastMediaUnixNano, time.Now().UnixNano())
}

func (p *Parser) Stop() error {
	p.closeOnce.Do(func() {
		close(p.stopCh)
		p.mu.Lock()
		cmd, stdin := p.cmd, p.cmdStdIn
		pc, ws := p.pc, p.ws
		p.mu.Unlock()

		// 优雅停止 ffmpeg
		if cmd != nil && cmd.Process != nil {
			if stdin != nil {
				_, _ = stdin.Write([]byte("q"))
			}
			proc := cmd.Process
			go func() {
				time.Sleep(3 * time.Second)
				_ = proc.Kill()
			}()
		}
		if pc != nil {
			_ = pc.Close()
		}
		if ws != nil {
			_ = ws.Close()
		}
	})
	return nil
}

func (p *Parser) closeWS() {
	p.mu.Lock()
	ws := p.ws
	p.ws = nil
	p.mu.Unlock()
	if ws != nil {
		_ = ws.Close()
	}
}

// GetPID 返回 ffmpeg 进程 PID。
func (p *Parser) GetPID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Pid
	}
	return 0
}

// ---- ffmpeg -progress 解析（与 ffmpeg parser 一致）----

func (p *Parser) scheduler() {
	defer close(p.statusResp)
	statusCh := p.scanFFmpegStatus()
	for {
		select {
		case <-p.statusReq:
			select {
			case b, ok := <-statusCh:
				if !ok {
					return
				}
				p.statusResp <- p.decodeFFmpegStatus(b)
			case <-time.After(3 * time.Second):
				p.statusResp <- nil
			}
		default:
			if _, ok := <-statusCh; !ok {
				return
			}
		}
	}
}

func (p *Parser) scanFFmpegStatus() <-chan []byte {
	ch := make(chan []byte)
	p.mu.Lock()
	stdout := p.cmdStdout
	p.mu.Unlock()
	if stdout == nil {
		close(ch)
		return ch
	}
	br := bufio.NewScanner(stdout)
	br.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if idx := bytes.Index(data, []byte("progress=continue\n")); idx >= 0 {
			return idx + 1, data[0:idx], nil
		}
		return 0, nil, nil
	})
	bilisentry.Go(func() {
		defer close(ch)
		for br.Scan() {
			ch <- br.Bytes()
		}
	})
	return ch
}

func (p *Parser) decodeFFmpegStatus(b []byte) map[string]interface{} {
	status := map[string]interface{}{"parser": Name}
	s := bufio.NewScanner(bytes.NewReader(b))
	s.Split(bufio.ScanLines)
	for s.Scan() {
		split := bytes.SplitN(s.Bytes(), []byte("="), 2)
		if len(split) != 2 {
			continue
		}
		status[string(bytes.TrimSpace(split[0]))] = string(bytes.TrimSpace(split[1]))
	}
	return status
}

func (p *Parser) Status() (map[string]interface{}, error) {
	select {
	case p.statusReq <- struct{}{}:
	default:
		return nil, nil
	}
	select {
	case resp, ok := <-p.statusResp:
		if !ok {
			return nil, nil
		}
		return resp, nil
	case <-time.After(3 * time.Second):
		return nil, nil
	}
}

// ---- helpers ----

// parseWebRTCURL 解析 webrtc://host/{modelId}?quality=X
func parseWebRTCURL(u *url.URL) (modelID, quality string, err error) {
	if u == nil || u.Scheme != "webrtc" {
		return "", "", fmt.Errorf("不是 webrtc URL: %v", u)
	}
	modelID = strings.Trim(u.Path, "/")
	if modelID == "" {
		return "", "", fmt.Errorf("webrtc URL 缺少 modelId: %v", u)
	}
	quality = u.Query().Get("quality")
	if quality == "" {
		quality = "source"
	}
	return modelID, quality, nil
}

func buildSDP(videoPort, audioPort, videoPT, audioPT, audioClock, audioCh int) string {
	if audioClock <= 0 {
		audioClock = 48000
	}
	if audioCh <= 0 {
		audioCh = 2
	}
	return fmt.Sprintf(`v=0
o=- 0 0 IN IP4 127.0.0.1
s=bililive-webrtc
c=IN IP4 127.0.0.1
t=0 0
m=video %d RTP/AVP %d
a=rtpmap:%d H264/90000
a=fmtp:%d packetization-mode=1
m=audio %d RTP/AVP %d
a=rtpmap:%d opus/%d/%d
`, videoPort, videoPT, videoPT, videoPT, audioPort, audioPT, audioPT, audioClock, audioCh)
}

// freeUDPPort 绑定一个临时 UDP 端口并立即释放，返回该端口号供 ffmpeg 监听。
func freeUDPPort() (int, error) {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return 0, err
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	conn.Close()
	return port, nil
}

func headerOr(headers map[string]string, key, def string) string {
	if headers != nil {
		if v, ok := headers[key]; ok && v != "" {
			return v
		}
	}
	return def
}
