package configs

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveStreamPreferenceInheritsGlobalAndMergesOverrides(t *testing.T) {
	quality := "720p"
	attributes := map[string]string{"codec": "h264", "format": "hls"}
	cfg := NewConfig()
	cfg.StreamPreference = StreamPreference{Quality: &quality, Attributes: &attributes}
	room := &LiveRoom{Url: "https://live.bilibili.com/12345"}
	resolved := cfg.ResolveConfigForRoom(room, "bilibili")
	require.Equal(t, "720p", *resolved.StreamPreference.Quality)
	require.Equal(t, attributes, *resolved.StreamPreference.Attributes)

	platformAttributes := map[string]string{"cdn": "preferred"}
	cfg.PlatformConfigs = map[string]PlatformConfig{
		"bilibili": {OverridableConfig: OverridableConfig{
			StreamPreference: &StreamPreference{Attributes: &platformAttributes},
		}},
	}
	resolved = cfg.ResolveConfigForRoom(room, "bilibili")
	require.Equal(t, "720p", *resolved.StreamPreference.Quality)
	require.Equal(t, map[string]string{"codec": "h264", "format": "hls", "cdn": "preferred"}, *resolved.StreamPreference.Attributes)

	roomQuality := "1080p"
	roomAttributes := map[string]string{"codec": "", "format": "flv"}
	room.StreamPreference = &StreamPreference{Quality: &roomQuality, Attributes: &roomAttributes}
	resolved = cfg.ResolveConfigForRoom(room, "bilibili")
	require.Equal(t, "1080p", *resolved.StreamPreference.Quality)
	require.Equal(t, map[string]string{"format": "flv", "cdn": "preferred"}, *resolved.StreamPreference.Attributes)
	require.Equal(t, map[string]string{"codec": "h264", "format": "hls"}, attributes)
}
