// @feature oss-ops-insight @api-functional
//
// Journey fixtures for oss-metric-insight-lifecycle contract tests: the
// persistent daily gate ( resource_type=oss key ), the collect executor and a
// real taskx.Queue wired to in-memory fakes.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package oss_metric_insight_lifecycle

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/require"
)

// newJourneyHarness builds the journey world with one metric-capable vendor.
func newJourneyHarness() (*osstest.Harness, *osstest.Provider) {
	h := osstest.NewHarness()
	provider := h.NewProvider(true)
	return h, provider
}

// seedBucketAccount 注册一个活跃账号并挂 n 个 OSS bucket 资产。
func seedBucketAccount(t *testing.T, h *osstest.Harness, provider *osstest.Provider, accountID int64, bucketNames ...string) {
	t.Helper()
	h.SeedAccount(accountID, provider)
	for _, name := range bucketNames {
		h.SeedBucket(accountID, provider.Name, name)
	}
}

// newTaskQueue builds a real taskx.Queue over the harness task repo.
// BufferSize=1 so a second Submit deterministically hits "queue full"
// ( no workers are started, nothing drains the buffered slot ).
func newTaskQueue(h *osstest.Harness) *taskx.Queue {
	queue := taskx.NewQueue(h.TaskRepo, elog.DefaultLogger, taskx.Config{WorkerNum: 1, BufferSize: 1})
	queue.RegisterExecutor(dummyOSSExecutor{})
	return queue
}

// dummyOSSExecutor lets the queue accept oss:collect_metrics submissions
// without running workers ( the executor itself is driven directly ).
type dummyOSSExecutor struct{}

func (d dummyOSSExecutor) Execute(_ context.Context, _ *taskx.Task) error { return nil }
func (d dummyOSSExecutor) GetType() taskx.TaskType                        { return executor.TaskTypeOSSCollectMetrics }

// newGate 装配生产持久化日闸(harness 存储 + 告警通道替身)。
func newGate(h *osstest.Harness) *scheduler.PersistentDailyGate {
	return scheduler.NewPersistentDailyGate(h.GateStore, h.GateAlerter, elog.DefaultLogger)
}

// submitOSSDailyCollect mirrors the production scheduler semantics
// ( auto_sync_oss_metrics.go ): claim today via the persistent gate oss key,
// and only on a successful claim submit exactly one oss:collect_metrics task
// with days=2. Returns whether the claim (and thus the submission) happened.
func submitOSSDailyCollect(t *testing.T, h *osstest.Harness, gate *scheduler.PersistentDailyGate, queue *taskx.Queue) bool {
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

// requireSubmittedOSSDailyTasks asserts the repo holds exactly n daily collect
// tasks, all typed oss:collect_metrics with days=2.
func requireSubmittedOSSDailyTasks(t *testing.T, h *osstest.Harness, n int) {
	t.Helper()
	tasks := h.TaskRepo.Tasks()
	require.Len(t, tasks, n)
	for _, task := range tasks {
		require.Equal(t, executor.TaskTypeOSSCollectMetrics, task.Type)
		require.Equal(t, 2, task.Params["days"], "每日采集语义固定 days=2(补昨日完整行+今日初态)")
	}
}

// seedMetric 直写一行指标(经写入门禁,零容量行自动打 zero_exception)。
func seedMetric(t *testing.T, h *osstest.Harness, accountID int64, providerName string, m types.OSSMetric) {
	t.Helper()
	h.SeedMetric(t, accountID, providerName, m)
}
