package pipeline

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type blockingTestStage struct {
	started chan<- struct{}
	release <-chan struct{}
}

func (s *blockingTestStage) Name() string { return "blocking" }

func (s *blockingTestStage) Execute(ctx *PipelineContext, input []FileInfo) ([]FileInfo, error) {
	s.started <- struct{}{}
	select {
	case <-s.release:
		return input, nil
	case <-ctx.Ctx.Done():
		return nil, ctx.Ctx.Err()
	}
}

func TestConcurrentTaskStartsRespectLimitAndReuseSlots(t *testing.T) {
	const limit = 2
	const total = 8
	store := NewMemoryStore()
	m := NewManager(context.Background(), store, &ManagerConfig{MaxConcurrent: limit}, nil)
	t.Cleanup(func() { m.Close(context.Background()) })
	started := make(chan struct{}, total)
	release := make(chan struct{}, total)
	m.RegisterStage("blocking", func(config StageConfig) (Stage, error) {
		return &blockingTestStage{started: started, release: release}, nil
	})
	var tasks []*PipelineTask
	for i := 0; i < total; i++ {
		task := NewPipelineTask(RecordInfo{}, &PipelineConfig{Stages: []StageConfig{{Name: "blocking"}}}, nil)
		require.NoError(t, store.CreateTask(context.Background(), task))
		tasks = append(tasks, task)
	}
	var callers sync.WaitGroup
	for _, task := range tasks {
		callers.Go(func() { m.startTask(task) })
	}
	callers.Wait()
	stats, err := m.GetStats()
	require.NoError(t, err)
	require.Equal(t, limit, stats.RunningCount)
	require.Equal(t, total-limit, stats.PendingCount)

	// 让首批任务完成，再验证空出的槽位可用于后续任务。
	for i := 0; i < limit; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("任务未开始执行")
		}
		release <- struct{}{}
	}
	require.Eventually(t, func() bool {
		stats, err := m.GetStats()
		return err == nil && stats.RunningCount == 0 && stats.CompletedCount == limit
	}, 5*time.Second, time.Millisecond)

	// 同时触发多个调度入口，仍必须遵守同一个并发上限。
	for i := 0; i < total; i++ {
		callers.Go(m.scheduleNextTasks)
	}
	callers.Wait()
	stats, err = m.GetStats()
	require.NoError(t, err)
	require.Equal(t, limit, stats.RunningCount)
	require.Equal(t, total-2*limit, stats.PendingCount)
}

func TestCancelledManagerDoesNotStartPendingTasks(t *testing.T) {
	store := NewMemoryStore()
	m := NewManager(context.Background(), store, nil, nil)
	task := NewPipelineTask(RecordInfo{}, &PipelineConfig{}, nil)
	require.NoError(t, store.CreateTask(context.Background(), task))
	m.Close(context.Background())
	m.startTask(task)
	m.scheduleNextTasks()
	saved, err := store.GetTask(context.Background(), task.ID)
	require.NoError(t, err)
	require.Equal(t, PipelineStatusPending, saved.Status)
}
