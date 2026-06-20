// Package boyfriend 支持 zh.boyfriend.show（StripChat 白标平台）的直播录制。
//
// 该平台的视频通过 WebRTC（doppiocdn / Flashphoner-WCS 风格信令）分发，
// 其 HLS 接口对匿名/登录用户均返回无法解码的 "URI 混淆" 分段，连官方播放器
// 自身也不解码该格式而是走 WebRTC。因此本平台的 GetStreamInfos 返回
// webrtc:// 伪 URL，由 webrtcrec parser 负责实际拉流录制（见 pkg/parser/webrtcrec）。
package boyfriend

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/hr3lxphr6j/requests"
	"github.com/tidwall/gjson"

	"github.com/bililive-go/bililive-go/src/live"
	"github.com/bililive-go/bililive-go/src/live/internal"
)

const (
	domain = "zh.boyfriend.show"
	cnName = "BoyFriend"

	apiBase    = "https://zh.boyfriend.show/api/front"
	webrtcHost = "edge-webrtc.doppiocdn.com"
	referer    = "https://zh.boyfriend.show/"
	userAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

func init() {
	live.Register(domain, new(builder))
}

type builder struct{}

func (b *builder) Build(url *url.URL) (live.Live, error) {
	return &Live{
		BaseLive: internal.NewBaseLive(url),
	}, nil
}

type Live struct {
	internal.BaseLive
}

func (l *Live) GetPlatformCNName() string {
	return cnName
}

// roomID 从 URL 路径解析房间名（即主播 username），如 https://zh.boyfriend.show/MS-818 -> MS-818
func (l *Live) roomID() string {
	return strings.Trim(l.Url.Path, "/")
}

func (l *Live) requestHeaders() map[string]any {
	return map[string]any{
		"User-Agent":      userAgent,
		"Referer":         referer,
		"Accept":          "application/json, text/plain, */*",
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
	}
}

// cookieKVs 返回当前直播间适用的 Cookie（来自配置注入的 CookieJar）。
// 用于解除未登录观看 1 小时的限制。
func (l *Live) cookieKVs() map[string]string {
	kv := make(map[string]string)
	if l.Options != nil && l.Options.Cookies != nil {
		for _, c := range l.Options.Cookies.Cookies(l.Url) {
			kv[c.Name] = c.Value
		}
	}
	return kv
}

// fetchBroadcast 调用 /api/front/v1/broadcasts/{房间名}，返回 item 节点。
// 该接口一次性给出开播状态、modelId、清晰度预设和源分辨率，匿名即可访问。
func (l *Live) fetchBroadcast() (gjson.Result, error) {
	roomID := l.roomID()
	if roomID == "" {
		return gjson.Result{}, live.ErrRoomUrlIncorrect
	}
	resp, err := l.RequestSession.Get(
		fmt.Sprintf("%s/v1/broadcasts/%s", apiBase, url.PathEscape(roomID)),
		requests.Headers(l.requestHeaders()),
		requests.Cookies(l.cookieKVs()),
	)
	if err != nil {
		return gjson.Result{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return gjson.Result{}, live.ErrRoomNotExist
	}
	body, err := resp.Bytes()
	if err != nil {
		return gjson.Result{}, err
	}
	item := gjson.GetBytes(body, "item")
	if !item.Exists() || item.Get("isDeleted").Bool() {
		return gjson.Result{}, live.ErrRoomNotExist
	}
	return item, nil
}

func (l *Live) GetInfo() (*live.Info, error) {
	item, err := l.fetchBroadcast()
	if err != nil {
		return nil, err
	}
	hostName := item.Get("username").String()
	if hostName == "" {
		hostName = l.roomID()
	}
	isLive := item.Get("isLive").Bool()
	status := item.Get("status").String()
	// 仅 public 视为可录；isLive 但非 public（如 groupShow/privateShow 群秀/私密秀）
	// 公开观众拿不到画面流，记录原因便于排查（避免误以为程序坏了）。
	if isLive && status != "public" {
		l.GetLogger().Debugf("房间在播但非公开(status=%s)，公开观众无画面，暂不录制", status)
	}
	return &live.Info{
		Live:      l,
		HostName:  hostName,
		RoomName:  hostName,
		Status:    isLive && status == "public",
		AudioOnly: l.Options.AudioOnly,
	}, nil
}

func (l *Live) GetStreamInfos() ([]*live.StreamUrlInfo, error) {
	item, err := l.fetchBroadcast()
	if err != nil {
		return nil, err
	}
	if !(item.Get("isLive").Bool() && item.Get("status").String() == "public") {
		return nil, fmt.Errorf("%w: 房间未开播或不可观看 (status=%s)", live.ErrLiveOffline, item.Get("status").String())
	}

	modelID := item.Get("modelId").String()
	if modelID == "" {
		modelID = item.Get("streamName").String()
	}
	if modelID == "" {
		return nil, fmt.Errorf("boyfriend: 未能解析 modelId")
	}

	settings := item.Get("settings")
	srcW := int(settings.Get("width").Int())
	srcH := int(settings.Get("height").Int())
	srcFPS := settings.Get("fps").Float()
	codec := settings.Get("video.codec").String()
	if codec == "" {
		codec = "h264"
	}
	srcBitrate := int(settings.Get("video.bitrate").Int() / 1000)

	// 内置 WebRTC 引擎基于 H264：遇到 H265/HEVC 或非 webrtc 推流(如 rtmp)的房间无法建连录制。
	// 给出可操作提示——这类房间需改用浏览器引擎(recording_engine: browser，由站点播放器解码)。
	mediaTransport := settings.Get("mediaTransport").String()
	lc := strings.ToLower(codec)
	if (mediaTransport != "" && mediaTransport != "webrtc") || lc == "h265" || lc == "hevc" {
		l.GetLogger().Warnf("房间为 %s 推流、编码 %s：内置 WebRTC 引擎(仅支持 H264)无法录制，请将该房间改用浏览器引擎(recording_engine: browser)",
			mediaTransport, codec)
	}

	headers := map[string]string{
		"User-Agent": userAgent,
		"Referer":    referer,
		"Origin":     strings.TrimSuffix(referer, "/"),
	}
	// 把 Cookie 透传给录制器（WebRTC 信令握手会带上，用于解除 1 小时限制）
	if cookieStr := buildCookieHeader(l.cookieKVs()); cookieStr != "" {
		headers["Cookie"] = cookieStr
	}

	// 收集清晰度：source（最高）置顶，其余按平台 presets 顺序（已是高→低），过滤模糊档。
	qualities := make([]string, 0, 6)
	qualities = append(qualities, "source")
	for _, p := range settings.Get("presets").Array() {
		name := p.String()
		if name == "" || strings.Contains(name, "blurred") || name == "source" {
			continue
		}
		qualities = append(qualities, name)
	}

	streams := make([]*live.StreamUrlInfo, 0, len(qualities))
	for _, q := range qualities {
		w, h := presetDimensions(q, srcW, srcH)
		fps := srcFPS
		if strings.HasSuffix(q, "60") {
			fps = 60
		} else if q != "source" {
			fps = 30
		}
		streamURL, perr := url.Parse(fmt.Sprintf("webrtc://%s/%s?quality=%s", webrtcHost, modelID, url.QueryEscape(q)))
		if perr != nil {
			continue
		}
		s := &live.StreamUrlInfo{
			Url:         streamURL,
			Name:        q,
			Description: q,
			Quality:     q,
			Format:      "webrtc",
			Width:       w,
			Height:      h,
			FrameRate:   fps,
			Codec:       codec,
			AudioCodec:  "opus",
			AttributesForStreamSelect: map[string]string{
				"format":      "webrtc",
				"quality_key": q,
			},
			HeadersForDownloader: headers,
		}
		if q == "source" {
			s.Bitrate = srcBitrate
		}
		streams = append(streams, s)
	}
	if len(streams) == 0 {
		return nil, fmt.Errorf("boyfriend: 未能构造任何可用清晰度")
	}
	return streams, nil
}

// presetDimensions 把清晰度预设名映射到分辨率。source 用平台给出的源分辨率。
func presetDimensions(preset string, srcW, srcH int) (int, int) {
	switch {
	case preset == "source":
		return srcW, srcH
	case strings.HasPrefix(preset, "1080"):
		return 1920, 1080
	case strings.HasPrefix(preset, "720"):
		return 1280, 720
	case strings.HasPrefix(preset, "480"):
		return 854, 480
	case strings.HasPrefix(preset, "360"):
		return 640, 360
	case strings.HasPrefix(preset, "240"):
		return 426, 240
	case strings.HasPrefix(preset, "160"):
		return 284, 160
	}
	return 0, 0
}

func buildCookieHeader(kv map[string]string) string {
	if len(kv) == 0 {
		return ""
	}
	parts := make([]string, 0, len(kv))
	for k, v := range kv {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "; ")
}
