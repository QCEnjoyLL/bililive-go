package boyfriend

import "testing"

func TestPresetDimensions(t *testing.T) {
	cases := []struct {
		preset       string
		srcW, srcH   int
		wantW, wantH int
	}{
		{"source", 1920, 1080, 1920, 1080},
		{"source", 640, 480, 640, 480},
		{"1080p", 0, 0, 1920, 1080},
		{"720p60", 0, 0, 1280, 720},
		{"720p", 0, 0, 1280, 720},
		{"480p", 0, 0, 854, 480},
		{"240p", 0, 0, 426, 240},
		{"unknown", 0, 0, 0, 0},
	}
	for _, c := range cases {
		w, h := presetDimensions(c.preset, c.srcW, c.srcH)
		if w != c.wantW || h != c.wantH {
			t.Errorf("presetDimensions(%q,%d,%d) = %dx%d, want %dx%d", c.preset, c.srcW, c.srcH, w, h, c.wantW, c.wantH)
		}
	}
}

func TestBuildCookieHeader(t *testing.T) {
	if got := buildCookieHeader(nil); got != "" {
		t.Errorf("empty map should give empty string, got %q", got)
	}
	got := buildCookieHeader(map[string]string{"a": "1"})
	if got != "a=1" {
		t.Errorf("buildCookieHeader = %q, want a=1", got)
	}
}
