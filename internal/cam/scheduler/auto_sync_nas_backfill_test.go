package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-common-go/taskx"
)

// NAS 历史回填调度触发单测(spec AC「错峰:回填在 01:30~06:00 窗口执行」):
// 窗口内经持久化日闸认领后提交 nas:backfill_metrics(days=30);窗口外不提交;
// 同一窗口日闸保证只提交一次;未装配日闸安全跳过。

// backfillQueueExecutor 让队列允许提交 nas:backfill_metrics(测试不启动 worker)
type backfillQueueExecutor struct{}

func (d *backfillQueueExecutor) Execute(_ context.Context, _ *taskx.Task) error { return nil }
func (d *backfillQueueExecutor) GetType() taskx.TaskType {
	return executor.TaskTypeNASBackfillMetrics
}

func newBackfillTestScheduler(t *testing.T, gate *PersistentDailyGate, now time.Time) (*AutoSyncScheduler, *mockTaskRepo) {
	t.Helper()
	s, repo := newTestScheduler(t, gate)
	// 队列允许提交回填任务类型
	s.taskQueue.RegisterExecutor(&backfillQueueExecutor{})
	s.nowFn = func() time.Time { return now }
	return s, repo
}

// 回填窗口内(02:00 CST)认领成功 → 恰好提交 1 条 nas:backfill_metrics(days=30)
func TestCheckNASMetricsBackfill_SubmitsInWindow(t *testing.T) {
	g := newTestGate(newFakeGateStore(), &recordingAlerter{})
	inWindow := time.Date(2026, 9, 19, 2, 0, 0, 0, time.FixedZone("CST", 8*3600))
	s, repo := newBackfillTestScheduler(t, g, inWindow)

	s.checkNASMetricsBackfill()

	tasks, err := repo.List(context.Background(), taskx.TaskFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("窗口内认领成功应恰好提交 1 条任务,实际 %d", len(tasks))
	}
	task := tasks[0]
	if task.Type != executor.TaskTypeNASBackfillMetrics {
		t.Fatalf("任务类型应为 nas:backfill_metrics,实际 %s", task.Type)
	}
	if days, _ := task.Params["days"].(int); days != 30 {
		t.Fatalf("days 默认应为 30,实际 %v", task.Params["days"])
	}

	// 同窗口第二轮:日闸已认领,不重复提交
	s.checkNASMetricsBackfill()
	tasks, _ = repo.List(context.Background(), taskx.TaskFilter{})
	if len(tasks) != 1 {
		t.Fatalf("同窗口不应重复提交,实际 %d 条", len(tasks))
	}
}

// 窗口外(00:10 与 06:00 后)不提交、不认领
func TestCheckNASMetricsBackfill_SkipsOutsideWindow(t *testing.T) {
	g := newTestGate(newFakeGateStore(), &recordingAlerter{})
	cst := time.FixedZone("CST", 8*3600)
	for _, hhmm := range []struct {
		h, m int
	}{
		{0, 10}, // 每日采集窗口,不得碰撞
		{1, 29},
		{6, 0},
		{12, 0},
	} {
		outside := time.Date(2026, 9, 19, hhmm.h, hhmm.m, 0, 0, cst)
		s, repo := newBackfillTestScheduler(t, g, outside)
		s.checkNASMetricsBackfill()
		if n, _ := repo.Count(context.Background(), taskx.TaskFilter{}); n != 0 {
			t.Fatalf("%02d:%02d 窗口外不应提交任务,实际 %d 条", hhmm.h, hhmm.m, n)
		}
	}
}

// 未装配日闸(nil):安全跳过不 panic
func TestCheckNASMetricsBackfill_NilGateSafe(t *testing.T) {
	inWindow := time.Date(2026, 9, 19, 2, 0, 0, 0, time.FixedZone("CST", 8*3600))
	s, repo := newTestScheduler(t, nil)
	s.nowFn = func() time.Time { return inWindow }
	s.checkNASMetricsBackfill()
	if n, _ := repo.Count(context.Background(), taskx.TaskFilter{}); n != 0 {
		t.Fatalf("未装配日闸不应提交任务,实际 %d 条", n)
	}
}
