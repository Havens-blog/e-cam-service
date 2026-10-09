// @feature nas-ops-insight @api-functional
//
// Journey fixtures for daily-metrics-collection contract tests: the
// persistent daily gate, the collect executor and a real taskx.Queue wired
// to in-memory fakes.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package daily_metrics_collection

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-common-go/taskx"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/require"
)

// newJourneyHarness builds the journey world with one metric-capable vendor.
func newJourneyHarness() (*nastest.Harness, *nastest.Provider) {
	h := nastest.NewHarness()
	provider := h.NewProvider(true)
	return h, provider
}

// newTaskQueue builds a real taskx.Queue over the harness task repo.
// BufferSize=1 so a second Submit deterministically hits "queue full"
// ( no workers are started, nothing drains the buffered slot ).
func newTaskQueue(h *nastest.Harness) *taskx.Queue {
	queue := taskx.NewQueue(h.TaskRepo, elog.DefaultLogger, taskx.Config{WorkerNum: 1, BufferSize: 1})
	queue.RegisterExecutor(dummyNASExecutor{})
	return queue
}

// dummyNASExecutor lets the queue accept nas:collect_metrics submissions
// without running workers ( the executor itself is driven directly ).
type dummyNASExecutor struct{}

func (d dummyNASExecutor) Execute(_ context.Context, _ *taskx.Task) error { return nil }
func (d dummyNASExecutor) GetType() taskx.TaskType                        { return executor.TaskTypeNASCollectMetrics }

// submitDailyCollect mirrors the production scheduler semantics
// ( auto_sync_nas_metrics.go ): claim today via the persistent gate, and only
// on a successful claim submit exactly one nas:collect_metrics task with
// days=2. Returns whether the claim (and thus the submission) happened.
func submitDailyCollect(t *testing.T, h *nastest.Harness, gate *scheduler.PersistentDailyGate, queue *taskx.Queue) bool {
	t.Helper()
	claimed, err := gate.TryClaim(context.Background(), scheduler.GateResourceNAS, nastest.Today())
	require.NoError(t, err)
	if !claimed {
		return false
	}
	task := &taskx.Task{
		ID:        "nasjt-daily-" + nastest.Today(),
		Type:      executor.TaskTypeNASCollectMetrics,
		Status:    taskx.TaskStatusPending,
		Params:    map[string]any{"days": 2},
		CreatedBy: "nasjt-daily",
	}
	require.NoError(t, queue.Submit(task))
	return true
}

// requireSubmittedDailyTasks asserts the repo holds exactly n daily collect
// tasks, all typed nas:collect_metrics with days=2.
func requireSubmittedDailyTasks(t *testing.T, h *nastest.Harness, n int) {
	t.Helper()
	tasks := h.TaskRepo.Tasks()
	require.Len(t, tasks, n)
	for _, task := range tasks {
		require.Equal(t, executor.TaskTypeNASCollectMetrics, task.Type)
		require.Equal(t, 2, task.Params["days"], "每日采集语义固定 days=2(补昨日完整行+今日初态)")
	}
}
