package servers

import (
	"context"
	"errors"
	"testing"

	"github.com/bililive-go/bililive-go/src/configs"
	"github.com/bililive-go/bililive-go/src/instance"
	"github.com/bililive-go/bililive-go/src/listeners"
	livemock "github.com/bililive-go/bililive-go/src/live/mock"
	"github.com/bililive-go/bililive-go/src/recorders"
	"github.com/bililive-go/bililive-go/src/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type removalRecorderManager struct {
	recorders.Manager
	removed types.LiveID
	err     error
}

func (m *removalRecorderManager) RemoveRecorder(ctx context.Context, id types.LiveID) error {
	m.removed = id
	return m.err
}

func TestRemoveRoomStopsRecordingWithoutListener(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "停止手动录制"},
		{name: "没有录制器也可删除", err: recorders.ErrRecorderNotExist},
		{name: "停止失败保留房间", err: errors.New("停止失败")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			roomURL := "https://live.bilibili.com/12345"
			room := livemock.NewMockLive(ctrl)
			room.EXPECT().GetLiveId().Return(types.LiveID("room")).AnyTimes()
			room.EXPECT().GetRawUrl().Return(roomURL).AnyTimes()
			setWebAuthTestConfig(t, configs.RPCAuth{})
			_, err := configs.AppendLiveRoom(configs.LiveRoom{Url: roomURL})
			require.NoError(t, err)
			inst := &instance.Instance{}
			ctx := context.WithValue(context.Background(), instance.Key, inst)
			inst.Ctx = ctx
			listeners.NewManager(ctx)
			rm := &removalRecorderManager{err: tc.err}
			inst.RecorderManager = rm
			inst.Lives.Set("room", room)

			err = removeLiveImpl(ctx, room)
			require.Equal(t, types.LiveID("room"), rm.removed)
			if tc.err != nil && !errors.Is(tc.err, recorders.ErrRecorderNotExist) {
				require.ErrorIs(t, err, tc.err)
				require.True(t, inst.Lives.Has("room"))
				require.Len(t, configs.GetCurrentConfig().LiveRooms, 1)
			} else {
				require.NoError(t, err)
				require.False(t, inst.Lives.Has("room"))
				require.Empty(t, configs.GetCurrentConfig().LiveRooms)
			}
		})
	}
}
