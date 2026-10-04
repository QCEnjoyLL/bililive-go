package recorders

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bililive-go/bililive-go/src/configs"
	"github.com/bililive-go/bililive-go/src/instance"
	"github.com/bililive-go/bililive-go/src/listeners"
	"github.com/bililive-go/bililive-go/src/live"
	livemock "github.com/bililive-go/bililive-go/src/live/mock"
	"github.com/bililive-go/bililive-go/src/notify"
	"github.com/bililive-go/bililive-go/src/pkg/livelogger"
	"github.com/bililive-go/bililive-go/src/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type sessionTestRecorder struct {
	Recorder
	startedAt      time.Time
	closed         bool
	restartStarted chan struct{}
	restartRelease chan struct{}
}

func (r *sessionTestRecorder) Start(ctx context.Context) error { return nil }
func (r *sessionTestRecorder) StartTime() time.Time            { return r.startedAt }
func (r *sessionTestRecorder) Close()                          { r.closed = true }
func (r *sessionTestRecorder) CloseForRestart() []notify.RecordingFileDetail {
	r.Close()
	if r.restartStarted != nil {
		close(r.restartStarted)
		<-r.restartRelease
	}
	return nil
}

func setupSessionTest(t *testing.T, cfg *configs.Config) (context.Context, *manager, live.Live, *[]*sessionTestRecorder) {
	t.Helper()
	previous := configs.GetCurrentConfig()
	t.Cleanup(func() { configs.SetCurrentConfig(previous) })
	configs.SetCurrentConfig(cfg)
	inst := &instance.Instance{}
	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, instance.Key, inst)
	listeners.NewManager(ctx)
	m := NewManager(ctx).(*manager)
	ctrl := gomock.NewController(t)
	room := livemock.NewMockLive(ctrl)
	room.EXPECT().GetLiveId().Return(types.LiveID("room")).AnyTimes()
	room.EXPECT().GetRawUrl().Return("https://live.bilibili.com/12345").AnyTimes()
	room.EXPECT().GetLogger().Return(livelogger.New(0, nil)).AnyTimes()
	var created []*sessionTestRecorder
	previousFactory := newRecorder
	newRecorder = func(ctx context.Context, l live.Live) (Recorder, error) {
		r := &sessionTestRecorder{startedAt: time.Now()}
		created = append(created, r)
		return r, nil
	}
	t.Cleanup(func() { newRecorder = previousFactory })
	t.Cleanup(func() {
		cancel()
		if m.HasRecorder(ctx, "room") {
			require.NoError(t, m.RemoveRecorder(ctx, "room"))
		}
	})
	return ctx, m, room, &created
}

func TestRecordingDeadlineSurvivesSegmentRestart(t *testing.T) {
	cfg := configs.NewConfig()
	cfg.VideoSplitStrategies = configs.VideoSplitStrategies{
		MaxDuration: 30 * time.Minute, MaxRecordDuration: 2 * time.Hour,
	}
	cfg.LiveRooms = []configs.LiveRoom{{Url: "https://live.bilibili.com/12345", IsListening: true}}
	ctx, m, room, created := setupSessionTest(t, cfg)
	require.NoError(t, m.AddRecorder(ctx, room))
	session := m.sessions["room"]

	// 分段重启只替换录制器，整场截止时间必须保留。
	require.NoError(t, m.RestartRecorder(ctx, room))
	require.Same(t, session, m.sessions["room"])
	require.Len(t, *created, 2)
	require.True(t, (*created)[0].closed)
	// 模拟断流重试后刚开始的新分段，仍应按整场起点停止。
	(*created)[1].startedAt = session.startedAt.Add(2*time.Hour - time.Second)
	require.False(t, m.checkSession(ctx, room, session, session.startedAt.Add(2*time.Hour), true))
	require.True(t, (*created)[1].closed)
	require.False(t, m.HasRecorder(ctx, "room"))
	require.ErrorIs(t, session.ctx.Err(), context.Canceled)
	updatedRoom, err := configs.GetCurrentConfig().GetLiveRoomByUrl(room.GetRawUrl())
	require.NoError(t, err)
	require.False(t, updatedRoom.IsListening)
}

func TestSessionTimersUsePlatformAndRoomOverrides(t *testing.T) {
	for _, tc := range []struct {
		name        string
		platform    configs.VideoSplitStrategies
		room        *configs.VideoSplitStrategies
		wantRestart bool
		wantStop    bool
	}{
		{name: "平台定时分段", platform: configs.VideoSplitStrategies{MaxDuration: time.Minute}, wantRestart: true},
		{name: "房间禁用平台分段", platform: configs.VideoSplitStrategies{MaxDuration: time.Minute}, room: &configs.VideoSplitStrategies{}},
		{name: "房间定时停止手动录制", room: &configs.VideoSplitStrategies{MaxRecordDuration: time.Minute}, wantStop: true},
		{name: "房间禁用平台定时停止", platform: configs.VideoSplitStrategies{MaxRecordDuration: time.Minute}, room: &configs.VideoSplitStrategies{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := configs.NewConfig()
			cfg.PlatformConfigs = map[string]configs.PlatformConfig{
				"bilibili": {OverridableConfig: configs.OverridableConfig{VideoSplitStrategies: &tc.platform}},
			}
			cfg.LiveRooms = []configs.LiveRoom{{
				Url:               "https://live.bilibili.com/12345",
				OverridableConfig: configs.OverridableConfig{VideoSplitStrategies: tc.room},
			}}
			ctx, m, room, created := setupSessionTest(t, cfg)
			require.NoError(t, m.AddRecorder(ctx, room))
			session := m.sessions["room"]
			m.checkSession(ctx, room, session, session.startedAt.Add(2*time.Minute), true)
			require.Equal(t, !tc.wantStop, m.HasRecorder(ctx, "room"))
			if tc.wantRestart {
				require.Len(t, *created, 2)
				require.True(t, (*created)[0].closed)
				require.Same(t, session, m.sessions["room"])
			} else {
				require.Len(t, *created, 1)
				require.Equal(t, tc.wantStop, (*created)[0].closed)
			}
		})
	}
}

func TestOldSessionTimerCannotStopNewRecording(t *testing.T) {
	cfg := configs.NewConfig()
	cfg.VideoSplitStrategies.MaxRecordDuration = time.Minute
	ctx, m, room, created := setupSessionTest(t, cfg)
	require.NoError(t, m.AddRecorder(ctx, room))
	oldSession := m.sessions["room"]
	require.NoError(t, m.RemoveRecorder(ctx, "room"))
	require.NoError(t, m.AddRecorder(ctx, room))
	newSession := m.sessions["room"]
	require.NotSame(t, oldSession, newSession)
	require.False(t, m.checkSession(ctx, room, oldSession, oldSession.startedAt.Add(time.Hour), true))
	require.True(t, m.HasRecorder(ctx, "room"))
	require.False(t, (*created)[1].closed)
	require.ErrorIs(t, m.restartRecorder(ctx, room, oldSession), ErrRecorderNotExist)
}

func TestSessionDeadlineWhileSegmentIsFinalizing(t *testing.T) {
	cfg := configs.NewConfig()
	cfg.VideoSplitStrategies.MaxRecordDuration = time.Hour
	ctx, m, room, created := setupSessionTest(t, cfg)
	require.NoError(t, m.AddRecorder(ctx, room))
	session := m.sessions["room"]
	oldRecorder := (*created)[0]
	oldRecorder.restartStarted = make(chan struct{})
	oldRecorder.restartRelease = make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(oldRecorder.restartRelease) }) }
	defer release()
	finished := make(chan error, 1)
	go func() { finished <- m.RestartRecorder(ctx, room) }()
	select {
	case <-oldRecorder.restartStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("分段收尾未开始")
	}
	// 整场定时器必须能在旧分段仍收尾时停止新分段。
	require.False(t, m.checkSession(ctx, room, session, session.startedAt.Add(time.Hour), false))
	require.False(t, m.HasRecorder(ctx, "room"))
	require.True(t, (*created)[1].closed)
	require.Equal(t, 1, m.GetActiveRecordingsCount())
	release()
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("分段收尾未结束")
	}
	require.Zero(t, m.GetActiveRecordingsCount())
}

func TestFailedSegmentRestartPreservesRecordingSession(t *testing.T) {
	cfg := configs.NewConfig()
	cfg.VideoSplitStrategies.MaxRecordDuration = time.Hour
	ctx, m, room, created := setupSessionTest(t, cfg)
	require.NoError(t, m.AddRecorder(ctx, room))
	session := m.sessions["room"]
	failure := errors.New("创建分段失败")
	newRecorder = func(ctx context.Context, l live.Live) (Recorder, error) { return nil, failure }
	require.ErrorIs(t, m.RestartRecorder(ctx, room), failure)
	require.Same(t, session, m.sessions["room"])
	require.False(t, (*created)[0].closed)
	require.NoError(t, session.ctx.Err())
	require.False(t, m.checkSession(ctx, room, session, session.startedAt.Add(time.Hour), false))
	require.True(t, (*created)[0].closed)
}

func TestSelectStreamUsesGlobalPreference(t *testing.T) {
	cfg := configs.NewConfig()
	quality := "720p"
	cfg.StreamPreference.Quality = &quality
	_, _, room, _ := setupSessionTest(t, cfg)
	r := &recorder{Live: room}
	streams := []*live.StreamUrlInfo{{Quality: "1080p"}, {Quality: "720p"}}
	require.Same(t, streams[1], r.selectPreferredStream(streams))
}
