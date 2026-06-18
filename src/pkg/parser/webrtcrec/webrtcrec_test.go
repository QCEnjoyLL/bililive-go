package webrtcrec

import (
	"net/url"
	"strings"
	"testing"
)

func TestParseWebRTCURL(t *testing.T) {
	u, _ := url.Parse("webrtc://edge-webrtc.doppiocdn.com/251286276?quality=720p")
	model, quality, err := parseWebRTCURL(u)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != "251286276" {
		t.Errorf("model = %q, want 251286276", model)
	}
	if quality != "720p" {
		t.Errorf("quality = %q, want 720p", quality)
	}

	// 缺省 quality
	u2, _ := url.Parse("webrtc://host/123")
	_, q2, err := parseWebRTCURL(u2)
	if err != nil || q2 != "source" {
		t.Errorf("default quality should be source, got %q err=%v", q2, err)
	}

	// 非 webrtc scheme
	u3, _ := url.Parse("https://host/123")
	if _, _, err := parseWebRTCURL(u3); err == nil {
		t.Error("non-webrtc scheme should error")
	}
}

func TestBuildSDP(t *testing.T) {
	sdp := buildSDP(5004, 5006, 102, 111, 48000, 2)
	for _, want := range []string{
		"m=video 5004 RTP/AVP 102",
		"a=rtpmap:102 H264/90000",
		"a=fmtp:102 packetization-mode=1",
		"m=audio 5006 RTP/AVP 111",
		"a=rtpmap:111 opus/48000/2",
	} {
		if !strings.Contains(sdp, want) {
			t.Errorf("SDP missing %q:\n%s", want, sdp)
		}
	}

	// 音频参数缺省回退
	sdp2 := buildSDP(1, 2, 102, 111, 0, 0)
	if !strings.Contains(sdp2, "opus/48000/2") {
		t.Errorf("default audio clock/ch fallback failed:\n%s", sdp2)
	}
}
