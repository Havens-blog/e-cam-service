// @feature oss-ops-insight @api-functional
//
// Contract step-1-claim-daily-gate: 调度器认领 oss 日闸并触发当日采集 — the
// oss key is claimed atomically once per day, a first deploy ( no oss record )
// claims as initial, gate write failures back off with alerting and never
// submit, and the oss key is independent from the nas/cdn keys.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package oss_metric_insight_lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// Outcome claimed-and-task-submitted: 认领成功(oss 键 last_date→今日),队列
// 新增恰好一条 oss:collect_metrics(days=2);oss 与 nas/cdn 分键互不干扰。
func TestStep1_ClaimedAndTaskSubmitted(t *testing.T) {
	h, _ := newJourneyHarness()
	// 既有 nas/cdn 键已认领今日(分键互不干扰的对照背景)
	_, err := h.GateStore.TryClaimDaily(context.Background(), scheduler.GateResourceNAS, osstest.Today())
	require.NoError(t, err)
	_, err = h.GateStore.TryClaimDaily(context.Background(), scheduler.GateResourceCDN, osstest.Today())
	require.NoError(t, err)

	gate := newGate(h)
	queue := newTaskQueue(h)

	require.True(t, submitOSSDailyCollect(t, h, gate, queue), "当日 oss 键应认领成功")
	requireSubmittedOSSDailyTasks(t, h, 1)
	require.Equal(t, osstest.Today(), h.GateStore.LastDate(scheduler.GateResourceOSS),
		"认领成功后 scheduler_state 的 oss 键 last_date 应推进为今日")

	// nas/cdn 键记录不受 oss 认领影响
	require.Equal(t, osstest.Today(), h.GateStore.LastDate(scheduler.GateResourceNAS))
	require.Equal(t, osstest.Today(), h.GateStore.LastDate(scheduler.GateResourceCDN))

	// 同日第二轮认领失败:至多提交一次
	require.False(t, submitOSSDailyCollect(t, h, gate, queue))
	requireSubmittedOSSDailyTasks(t, h, 1)
}

// Outcome first-deploy-claim: 首部署无 oss 键记录视为首次认领成功,触发一次
// 当日提交,不回溯补采历史指标行。
func TestStep1_FirstDeployClaim_NoBackfill(t *testing.T) {
	h, _ := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h)

	require.True(t, submitOSSDailyCollect(t, h, gate, queue), "首部署应视为首次认领成功")
	requireSubmittedOSSDailyTasks(t, h, 1)
	require.Equal(t, osstest.Today(), h.GateStore.LastDate(scheduler.GateResourceOSS))
	require.Equal(t, 0, h.MetricDAO.Count(), "首次认领不回溯补采历史指标")
}

// Outcome gate-write-failure: 认领写失败按指数退避重试,耗尽后返回错误并经
// SchedulerGateAlerter 升级告警(resource_type=oss, operation=write);
// 不提交采集任务,oss 键保持未认领,恢复后当日可正常认领一次。
func TestStep1_GateWriteFailure_BackoffAlertNoSubmit(t *testing.T) {
	h, _ := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h)
	injected := errors.New("injected: mongo write failure")

	// 注入连续 4 次写失败(首次尝试 + 3 次重试全部失败)
	h.GateStore.FailClaim(scheduler.GateResourceOSS, 4, injected)
	claimed, err := gate.TryClaim(context.Background(), scheduler.GateResourceOSS, osstest.Today())
	require.Error(t, err, "重试耗尽应返回错误而非静默跳过")
	require.False(t, claimed)
	require.Equal(t, "", h.GateStore.LastDate(scheduler.GateResourceOSS), "写失败期间 last_date 不得推进")

	alerts := h.GateAlerter.Calls()
	require.NotEmpty(t, alerts, "重试耗尽必须升级告警,不得仅记日志")
	last := alerts[len(alerts)-1]
	require.Equal(t, scheduler.GateResourceOSS, last.ResourceType)
	require.Equal(t, "write", last.Operation)
	require.ErrorIs(t, last.Err, injected)

	// 任务未被提交。恢复语义:退避窗口内同实例静默跳过(防洪泛),
	// 新实例(重启/重部署形态)加载同一持久化存储后可正常认领一次并提交。
	requireSubmittedOSSDailyTasks(t, h, 0)
	require.False(t, submitOSSDailyCollect(t, h, gate, queue), "退避窗口内同实例应静默跳过不提交")
	requireSubmittedOSSDailyTasks(t, h, 0)

	recoveredGate := newGate(h)
	require.True(t, submitOSSDailyCollect(t, h, recoveredGate, queue), "恢复(新实例)后应正常认领一次")
	requireSubmittedOSSDailyTasks(t, h, 1)
}
