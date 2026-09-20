// @feature oss-ops-insight @api-functional
//
// Contract oss step-1-claim-oss-daily-gate: oss 日闸原子认领当日 — the oss
// key claims each day exactly once ( nas/cdn keys untouched ), a multi-replica
// race has a single winner with silent losers, write failures back off with
// an escalation alert, and a first deploy claims as initial without
// backfilling history.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package daily_metrics_collection

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// Outcome claimed-once-and-submitted: oss 键认领成功返回当日认领权,恰好提交
// 一条 oss:collect_metrics(days=2);oss/nas/cdn 按资源类型分键互不干扰。
func TestOSSStep1_GateClaimOnceAndSubmitted(t *testing.T) {
	h, _ := ossNewHarness()
	// 既有 nas/cdn 日闸键已认领今日(fixture_spec: 既有记录)
	for _, res := range []string{scheduler.GateResourceNAS, scheduler.GateResourceCDN} {
		_, err := h.GateStore.TryClaimDaily(context.Background(), res, osstest.Today())
		require.NoError(t, err)
	}

	gate := ossNewGate(h)
	queue := ossNewTaskQueue(h)

	require.True(t, ossSubmitDailyCollect(t, h, gate, queue), "当日 oss 键应认领成功")
	ossRequireSubmittedDailyTasks(t, h, 1)
	require.Equal(t, osstest.Today(), h.GateStore.LastDate(scheduler.GateResourceOSS),
		"认领成功后 scheduler_state 的 oss 键 last_date 应推进为今日")

	// nas/cdn 键记录不变(分键互不干扰)
	require.Equal(t, osstest.Today(), h.GateStore.LastDate(scheduler.GateResourceNAS))
	require.Equal(t, osstest.Today(), h.GateStore.LastDate(scheduler.GateResourceCDN))

	// 同日第二轮认领失败:当日至多提交一次
	require.False(t, ossSubmitDailyCollect(t, h, gate, queue))
	ossRequireSubmittedDailyTasks(t, h, 1)
}

// Outcome multi-replica-race-single-winner: 两个实例同日同时认领同一 oss 键 —
// 未认领方静默跳过不提交,当日仍只有胜出方的一条任务,无告警。
func TestOSSStep1_MultiReplicaRaceSingleWinner(t *testing.T) {
	h, _ := ossNewHarness()
	gate := ossNewGate(h)
	queue := ossNewTaskQueue(h)

	require.True(t, ossSubmitDailyCollect(t, h, gate, queue))

	// 竞争实例(全新 gate,同一持久化存储)同日认领
	rivalGate := ossNewGate(h)
	require.False(t, ossSubmitDailyCollect(t, h, rivalGate, queue), "当日已被认领,竞争方应认领失败")
	ossRequireSubmittedDailyTasks(t, h, 1)
	require.Empty(t, h.GateAlerter.Calls(), "正常竞争语义不触发告警")
}

// Outcome gate-write-failure-backoff-alert: 认领写连续失败 — 指数退避重试并
// 升级告警(resource_type=oss, operation=write);退避期间任务队列不被洪泛;
// 恢复(新实例加载同一持久化存储)后当日正常认领一次并提交。
func TestOSSStep1_GateWriteFailureBackoffAlert(t *testing.T) {
	h, _ := ossNewHarness()
	gate := ossNewGate(h)
	queue := ossNewTaskQueue(h)
	injected := errors.New("injected: mongo write failure")

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

	// 退避期间任务队列不被洪泛;恢复(新实例)后当日正常认领一次
	ossRequireSubmittedDailyTasks(t, h, 0)
	require.False(t, ossSubmitDailyCollect(t, h, gate, queue), "退避窗口内同实例应静默跳过")
	ossRequireSubmittedDailyTasks(t, h, 0)
	require.True(t, ossSubmitDailyCollect(t, h, ossNewGate(h), queue), "恢复后应正常认领一次")
	ossRequireSubmittedDailyTasks(t, h, 1)
}

// Outcome first-deploy-no-record: 首部署 scheduler_state 尚无 oss 记录 —
// 视为首次认领,认领后触发一次当日提交;不回溯补采历史日期。
func TestOSSStep1_FirstDeployNoRecord(t *testing.T) {
	h, _ := ossNewHarness()
	gate := ossNewGate(h)
	queue := ossNewTaskQueue(h)

	require.True(t, ossSubmitDailyCollect(t, h, gate, queue), "首部署应视为首次认领成功")
	ossRequireSubmittedDailyTasks(t, h, 1)
	require.Equal(t, osstest.Today(), h.GateStore.LastDate(scheduler.GateResourceOSS))
	require.Equal(t, 0, h.MetricDAO.Count(), "首次认领不回溯补采历史指标")
}
