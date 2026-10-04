package listeners

import (
	"context"
	"testing"

	"github.com/bililive-go/bililive-go/src/configs"
	"github.com/bililive-go/bililive-go/src/instance"
	"github.com/bililive-go/bililive-go/src/live"
	livemock "github.com/bililive-go/bililive-go/src/live/mock"
	"github.com/bililive-go/bililive-go/src/log"
	"github.com/bililive-go/bililive-go/src/pkg/events"
	evtmock "github.com/bililive-go/bililive-go/src/pkg/events/mock"
	"go.uber.org/mock/gomock"
)

func TestTitleChangeUsesEffectiveSplitConfig(t *testing.T) {
	for _, tc := range []struct {
		name     string
		global   bool
		platform bool
		room     *configs.VideoSplitStrategies
		want     bool
	}{
		{name: "平台启用", platform: true, want: true},
		{name: "平台覆盖全局关闭", global: true},
		{name: "房间关闭", platform: true, room: &configs.VideoSplitStrategies{}},
		{name: "房间启用", room: &configs.VideoSplitStrategies{OnRoomNameChanged: true}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := configs.GetCurrentConfig()
			t.Cleanup(func() { configs.SetCurrentConfig(previous) })
			cfg := configs.NewConfig()
			cfg.VideoSplitStrategies.OnRoomNameChanged = tc.global
			cfg.PlatformConfigs = map[string]configs.PlatformConfig{
				"bilibili": {OverridableConfig: configs.OverridableConfig{
					VideoSplitStrategies: &configs.VideoSplitStrategies{OnRoomNameChanged: tc.platform},
				}},
			}
			cfg.LiveRooms = []configs.LiveRoom{{Url: "https://live.bilibili.com/12345",
				OverridableConfig: configs.OverridableConfig{VideoSplitStrategies: tc.room}}}
			configs.SetCurrentConfig(cfg)
			ctrl := gomock.NewController(t)
			ed := evtmock.NewMockDispatcher(ctrl)
			room := livemock.NewMockLive(ctrl)
			room.EXPECT().GetRawUrl().Return(cfg.LiveRooms[0].Url).AnyTimes()
			if tc.want {
				ed.EXPECT().DispatchEvent(events.NewEvent(RoomNameChanged, room))
			}
			ctx := context.WithValue(context.Background(), instance.Key, &instance.Instance{})
			log.New(ctx)
			l := &listener{Live: room, ed: ed, status: status{roomStatus: true, roomName: "旧标题"}}
			l.processInfo(&live.Info{Status: true, HostName: "主播", RoomName: "新标题"})
		})
	}
}
