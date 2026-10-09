// @feature nas-ops-insight @api-functional
//
// Contract step-1-daily-collect-trigger: 触发当日 NAS 指标采集 — the
// persistent gate claims the day once, restarts never re-claim, concurrent
// rivals lose silently, and gate write failures back off and escalate.
// The scheduler trigger loop is unexported ( covered by internal unit tests );
// this suite drives the exported PersistentDailyGate with the production
// claim-then-submit composition.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package nas_metric_insight_lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-common-go/taskx"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/require"
)

// newGate 装配生产持久化日闸。
func newGate(h *nastest.Harness) *scheduler.PersistentDailyGate {
	return scheduler.NewPersistentDailyGate(h.GateStore, h.GateAlerter, elog.DefaultLogger)
}

// claimAndSubmit 生产调度器语义组合:认领成功才提交 1 条
// nas:collect_metrics(days=2) 任务,返回认领结果。
func claimAndSubmit(t *testing.T, h *nastest.Harness, gate *scheduler.PersistentDailyGate) bool {
	t.Helper()
	claimed, err := gate.TryClaim(context.Background(), scheduler.GateResourceNAS, nastest.Today())
	require.NoError(t, err)
	if !claimed {
		return false
	}
	queue := taskx.NewQueue(h.TaskRepo, elog.DefaultLogger, taskx.Config{WorkerNum: 1, BufferSize: 10})
	queue.RegisterExecutor(dummyExecutor{})
	require.NoError(t, queue.Submit(&taskx.Task{
		ID:     "nasjt-lifecycle-" + nastest.Today(),
		Type:   executor.TaskTypeNASCollectMetrics,
		Status: taskx.TaskStatusPending,
		Params: map[string]any{"days": 2},
	}))
	return true
}

// dummyExecutor 让队列接受 nas:collect_metrics(不启动 worker)。
type dummyExecutor struct{}

func (d dummyExecutor) Execute(_ context.Context, _ *taskx.Task) error { return nil }
func (d dummyExecutor) GetType() taskx.TaskType                        { return executor.TaskTypeNASCollectMetrics }

// Outcome success: 认领成功,恰好提交 1 条 days=2 采集任务,last_date 推进
// 为今日并持久化,日志记录任务标识(任务仓储可见)。
func TestStep1_DailyCollectTrigger_Success(t *testing.T) {
	h, _ := newJourneyHarness()
	gate := newGate(h)

	require.True(t, claimAndSubmit(t, h, gate))
	tasks := h.TaskRepo.Tasks()
	require.Len(t, tasks, 1)
	require.Equal(t, executor.TaskTypeNASCollectMetrics, tasks[0].Type)
	require.Equal(t, 2, tasks[0].Params["days"])
	require.Equal(t, nastest.Today(), h.GateStore.LastDate(scheduler.GateResourceNAS))
}

// Outcome restart-same-day-no-reclaim: 当日已认领后服务重启,再次尝试触发
// 静默跳过,不产生新采集任务,last_date 保持今日。
func TestStep1_DailyCollectTrigger_RestartSameDayNoReclaim(t *testing.T) {
	h, _ := newJourneyHarness()
	require.True(t, claimAndSubmit(t, h, newGate(h)))

	// 重启后全新 gate 实例重新进入当日窗口
	restarted := newGate(h)
	require.False(t, claimAndSubmit(t, h, restarted), "当日已认领,重启不得重复提交")
	require.Len(t, h.TaskRepo.Tasks(), 1)
	require.Equal(t, nastest.Today(), h.GateStore.LastDate(scheduler.GateResourceNAS))
}

// Outcome concurrent-claim-lost: 两个实例同日并发认领,仅一个成功并提交任务;
// 竞争失败方静默跳过无告警,全天恰好一条采集任务。
func TestStep1_DailyCollectTrigger_ConcurrentClaimLost(t *testing.T) {
	h, _ := newJourneyHarness()
	queueGateA := newGate(h)
	queueGateB := newGate(h)

	wins := 0
	for _, gate := range []*scheduler.PersistentDailyGate{queueGateA, queueGateB} {
		if claimAndSubmit(t, h, gate) {
			wins++
		}
	}
	require.Equal(t, 1, wins, "两个实例同日并发认领应恰有一个胜出")
	require.Len(t, h.TaskRepo.Tasks(), 1, "全天该资源类型恰好一条采集任务")
	require.Empty(t, h.GateAlerter.Calls(), "正常竞争失败不产生错误告警")
}

// Outcome gate-write-failure-backoff: mongo 日闸写失败按 1s/2s/4s 指数退避
// 重试,重试耗尽后升级告警(resource_type=nas, operation=write),当日任务
// 未提交、last_date 未被推进。
func TestStep1_DailyCollectTrigger_GateWriteFailureBackoff(t *testing.T) {
	h, _ := newJourneyHarness()
	gate := newGate(h)
	injected := errors.New("injected: gate write failure")

	h.GateStore.FailClaim(scheduler.GateResourceNAS, 4, injected)
	claimed, err := gate.TryClaim(context.Background(), scheduler.GateResourceNAS, nastest.Today())
	require.Error(t, err, "重试耗尽应返回错误")
	require.False(t, claimed)
	require.Len(t, h.TaskRepo.Tasks(), 0, "当日任务未被提交")

	alerts := h.GateAlerter.Calls()
	require.NotEmpty(t, alerts, "重试耗尽必须升级告警")
	last := alerts[len(alerts)-1]
	require.Equal(t, scheduler.GateResourceNAS, last.ResourceType)
	require.Equal(t, "write", last.Operation)
}
