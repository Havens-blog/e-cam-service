// @feature disk-ops-insight @api-functional
//
// Journey fixtures for disk-metrics-daily-collection contract tests: a metric
// -capable test vendor, seeded disk assets, the persistent daily gate disk key
// and a real taskx.Queue wired to in-memory fakes.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package disk_metrics_daily_collection

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/require"
)

// newJourneyHarness builds the journey world with one metric-capable vendor.
func newJourneyHarness() (*disktest.Harness, *disktest.Provider) {
	h := disktest.NewHarness()
	provider := h.NewProvider(true)
	return h, provider
}

// seedDiskAccount 注册一个活跃账号并挂 n 个 disk 资产(region 缺省 cn-test-1)。
func seedDiskAccount(t *testing.T, h *disktest.Harness, provider *disktest.Provider, accountID int64, diskIDs ...string) {
	t.Helper()
	h.SeedAccount(accountID, provider)
	for _, id := range diskIDs {
		h.SeedDisk(accountID, provider.Name, id, "cn-test-1")
	}
}

// newTaskQueue builds a real taskx.Queue over the harness task repo
// ( BufferSize=1: second Submit deterministically hits "queue full" ).
func newTaskQueue(h *disktest.Harness) *taskx.Queue {
	queue := taskx.NewQueue(h.TaskRepo, elog.DefaultLogger, taskx.Config{WorkerNum: 1, BufferSize: 1})
	queue.RegisterExecutor(dummyDiskExecutor{})
	return queue
}

// dummyDiskExecutor lets the queue accept disk:collect_metrics submissions
// without running workers.
type dummyDiskExecutor struct{}

func (d dummyDiskExecutor) Execute(_ context.Context, _ *taskx.Task) error { return nil }
func (d dummyDiskExecutor) GetType() taskx.TaskType                        { return executor.TaskTypeDiskCollectMetrics }

// newGate 装配生产持久化日闸(harness 存储 + 告警通道替身)。
func newGate(h *disktest.Harness) *scheduler.PersistentDailyGate {
	return scheduler.NewPersistentDailyGate(h.GateStore, h.GateAlerter, elog.DefaultLogger)
}

// submitDiskDailyCollect mirrors the production scheduler semantics: claim
// today via the disk key, and only on a successful claim submit exactly one
// disk:collect_metrics task with days=2.
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

// requireSubmittedDiskDailyTasks asserts the repo holds exactly n daily
// collect tasks, all typed disk:collect_metrics with days=2.
func requireSubmittedDiskDailyTasks(t *testing.T, h *disktest.Harness, n int, msgAndArgs ...any) {
	t.Helper()
	tasks := h.TaskRepo.Tasks()
	require.Len(t, tasks, n, msgAndArgs...)
	for _, task := range tasks {
		require.Equal(t, executor.TaskTypeDiskCollectMetrics, task.Type, msgAndArgs...)
		require.Equal(t, 2, task.Params["days"], "每日采集语义固定 days=2(补昨日完整行+今日初态)")
	}
}
