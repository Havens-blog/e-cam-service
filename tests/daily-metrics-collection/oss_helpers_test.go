// @feature oss-ops-insight @api-functional
//
// Journey fixtures for the oss-ops-insight daily-metrics-collection contract
// tests ( co-located with the nas-ops-insight suite of the same journey —
// OSS symbols carry the oss prefix to coexist in this package ): the
// persistent daily gate oss key, the collect executor and a real taskx.Queue
// wired to in-memory fakes.
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
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/require"
)

// ossNewHarness builds the OSS journey world with one metric-capable vendor.
func ossNewHarness() (*osstest.Harness, *osstest.Provider) {
	h := osstest.NewHarness()
	provider := h.NewProvider(true)
	return h, provider
}

// ossNewGate 装配生产持久化日闸(harness 存储 + 告警通道替身)。
func ossNewGate(h *osstest.Harness) *scheduler.PersistentDailyGate {
	return scheduler.NewPersistentDailyGate(h.GateStore, h.GateAlerter, elog.DefaultLogger)
}

// ossNewTaskQueue builds a real taskx.Queue over the harness task repo.
// BufferSize=1 so a second Submit deterministically hits "queue full"
// ( no workers are started, nothing drains the buffered slot ).
func ossNewTaskQueue(h *osstest.Harness) *taskx.Queue {
	queue := taskx.NewQueue(h.TaskRepo, elog.DefaultLogger, taskx.Config{WorkerNum: 1, BufferSize: 1})
	queue.RegisterExecutor(ossDummyExecutor{})
	return queue
}

// ossDummyExecutor lets the queue accept oss:collect_metrics submissions
// without running workers ( the executor itself is driven directly ).
type ossDummyExecutor struct{}

func (d ossDummyExecutor) Execute(_ context.Context, _ *taskx.Task) error { return nil }
func (d ossDummyExecutor) GetType() taskx.TaskType                        { return executor.TaskTypeOSSCollectMetrics }

// ossSubmitDailyCollect mirrors the production scheduler semantics
// ( auto_sync_oss_metrics.go ): claim today via the persistent gate oss key,
// and only on a successful claim submit exactly one oss:collect_metrics task
// with days=2. Returns whether the claim (and thus the submission) happened.
func ossSubmitDailyCollect(t *testing.T, h *osstest.Harness, gate *scheduler.PersistentDailyGate, queue *taskx.Queue) bool {
	t.Helper()
	claimed, err := gate.TryClaim(context.Background(), scheduler.GateResourceOSS, osstest.Today())
	require.NoError(t, err)
	if !claimed {
		return false
	}
	task := &taskx.Task{
		ID:        "ossjt-daily-" + osstest.Today(),
		Type:      executor.TaskTypeOSSCollectMetrics,
		Status:    taskx.TaskStatusPending,
		Params:    map[string]any{"days": 2},
		CreatedBy: "ossjt-daily",
	}
	require.NoError(t, queue.Submit(task))
	return true
}

// ossRequireSubmittedDailyTasks asserts the repo holds exactly n daily collect
// tasks, all typed oss:collect_metrics with days=2.
func ossRequireSubmittedDailyTasks(t *testing.T, h *osstest.Harness, n int) {
	t.Helper()
	tasks := h.TaskRepo.Tasks()
	require.Len(t, tasks, n)
	for _, task := range tasks {
		require.Equal(t, executor.TaskTypeOSSCollectMetrics, task.Type)
		require.Equal(t, 2, task.Params["days"], "每日采集语义固定 days=2(补昨日完整行+今日初态)")
	}
}

// ossSeedBucketAccount 注册活跃账号并挂 n 个 OSS bucket 资产。
func ossSeedBucketAccount(t *testing.T, h *osstest.Harness, provider *osstest.Provider, accountID int64, bucketNames ...string) {
	t.Helper()
	h.SeedAccount(accountID, provider)
	for _, name := range bucketNames {
		h.SeedBucket(accountID, provider.Name, name)
	}
}
