// @feature disk-ops-insight @api-functional
//
// Journey fixtures for disk-day-gate-resilience contract tests: the persistent
// daily gate ( resource_type=disk key ) wired to in-memory fakes, plus a real
// taskx.Queue so claim-to-submit semantics stay observable.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package disk_day_gate_resilience

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-common-go/taskx"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/require"
)

// newJourneyHarness builds the journey world ( fakes only, no vendor needed
// on the gate resilience surface ).
func newJourneyHarness() *disktest.Harness {
	return disktest.NewHarness()
}

// newTestLogger 生产日志组件形态(与 elog.DefaultLogger 同源,helper 内聚)。
func newTestLogger() *elog.Component { return elog.DefaultLogger }

// newGate 装配生产持久化日闸(harness 存储 + 告警通道替身)。
func newGate(h *disktest.Harness) *scheduler.PersistentDailyGate {
	return scheduler.NewPersistentDailyGate(h.GateStore, h.GateAlerter, elog.DefaultLogger)
}

// newTaskQueue builds a real taskx.Queue over the harness task repo.
// BufferSize=1 so a second Submit deterministically hits "queue full"
// ( no workers are started, nothing drains the buffered slot ).
func newTaskQueue(h *disktest.Harness) *taskx.Queue {
	queue := taskx.NewQueue(h.TaskRepo, elog.DefaultLogger, taskx.Config{WorkerNum: 1, BufferSize: 1})
	queue.RegisterExecutor(dummyDiskExecutor{})
	return queue
}

// dummyDiskExecutor lets the queue accept disk:collect_metrics submissions
// without running workers ( the executor itself is driven directly ).
type dummyDiskExecutor struct{}

func (d dummyDiskExecutor) Execute(_ context.Context, _ *taskx.Task) error { return nil }
func (d dummyDiskExecutor) GetType() taskx.TaskType                        { return executor.TaskTypeDiskCollectMetrics }

// submitDiskDailyCollect mirrors the production scheduler semantics
// ( auto_sync_disk_metrics.go ): claim today via the persistent gate disk key,
// and only on a successful claim submit exactly one disk:collect_metrics task
// with days=2. Returns whether the claim (and thus the submission) happened.
func submitDiskDailyCollect(t *testing.T, h *disktest.Harness, gate *scheduler.PersistentDailyGate, queue *taskx.Queue) bool {
	t.Helper()
	claimed, err := gate.TryClaim(context.Background(), scheduler.GateResourceDisk, disktest.Today())
	require.NoError(t, err)
	if !claimed {
		return false
	}
	task := &taskx.Task{
		ID:        "diskjt-daily-" + disktest.Today(),
		Type:      executor.TaskTypeDiskCollectMetrics,
		Status:    taskx.TaskStatusPending,
		Params:    map[string]any{"days": 2},
		CreatedBy: "diskjt-daily",
	}
	require.NoError(t, queue.Submit(task))
	return true
}

// requireSubmittedDiskDailyTasks asserts the repo holds exactly n daily collect
// tasks, all typed disk:collect_metrics with days=2.
func requireSubmittedDiskDailyTasks(t *testing.T, h *disktest.Harness, n int, msgAndArgs ...any) {
	t.Helper()
	tasks := h.TaskRepo.Tasks()
	require.Len(t, tasks, n, msgAndArgs...)
	for _, task := range tasks {
		require.Equal(t, executor.TaskTypeDiskCollectMetrics, task.Type, msgAndArgs...)
		require.Equal(t, 2, task.Params["days"], "每日采集语义固定 days=2(补昨日完整行+今日初态)")
	}
}
