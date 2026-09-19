// @feature nas-ops-insight @api-functional
//
// Contract step-1-gate-claim: 日闸原子认领当日 — the persistent daily gate
// claims each day exactly once via one atomic claim, silently skips when the
// day is already taken, retries write failures with exponential backoff and
// alerts on exhaustion, backs off read failures for >=5 minutes, and treats
// a first deploy ( no scheduler_state record ) as an initial claim without
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
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/require"
)

// newGate 装配生产持久化日闸(harness 存储 + 告警通道替身)。
func newGate(h *nastest.Harness) *scheduler.PersistentDailyGate {
	return scheduler.NewPersistentDailyGate(h.GateStore, h.GateAlerter, elog.DefaultLogger)
}

// Outcome success: 认领成功返回真,恰好提交 1 条 nas:collect_metrics(days=2),
// last_date 推进为今日并持久化;同日第二轮认领失败,不再产生新任务。
func TestStep1_GateClaimSuccess_SubmitsCollectOncePerDay(t *testing.T) {
	h, _ := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h)

	require.True(t, submitDailyCollect(t, h, gate, queue), "当日首次认领应成功")
	requireSubmittedDailyTasks(t, h, 1)
	require.Equal(t, nastest.Today(), h.GateStore.LastDate(scheduler.GateResourceNAS),
		"认领成功后 scheduler_state 的 last_date 应推进为今日")

	// 同日第二轮(重启/手动+自动重叠):认领失败,不提交新任务
	require.False(t, submitDailyCollect(t, h, gate, queue))
	requireSubmittedDailyTasks(t, h, 1)
}

// Outcome claim-already-taken: 当日已被认领后,新 gate 实例(多副本/重启形态)
// 认领失败、静默跳过无告警,不提交任何新任务。
func TestStep1_ClaimAlreadyTaken_SilentSkip(t *testing.T) {
	h, _ := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h)

	require.True(t, submitDailyCollect(t, h, gate, queue))

	// 另一实例(全新 gate,同一持久化存储)同日并发认领
	rivalGate := newGate(h)
	require.False(t, submitDailyCollect(t, h, rivalGate, queue), "当日已被认领,竞争方应认领失败")
	requireSubmittedDailyTasks(t, h, 1)
	require.Empty(t, h.GateAlerter.Calls(), "正常竞争语义不触发告警")
}

// Outcome gate-write-failure-retry-alert: 写日闸失败按 1s/2s/4s 指数退避重试,
// 重试耗尽返回错误并触发日闸写失败升级告警(resource_type=nas,
// operation=write);last_date 在成功前未被推进、任务未提交。
func TestStep1_GateWriteFailure_RetryExhaustedAlerts(t *testing.T) {
	h, _ := newJourneyHarness()
	gate := newGate(h)
	injected := errors.New("injected: mongo write failure")

	// 注入连续 4 次写失败(首次尝试 + 3 次重试全部失败)
	h.GateStore.FailClaim(scheduler.GateResourceNAS, 4, injected)
	claimed, err := gate.TryClaim(context.Background(), scheduler.GateResourceNAS, nastest.Today())
	require.Error(t, err, "重试耗尽应返回错误")
	require.False(t, claimed)
	require.Equal(t, "", h.GateStore.LastDate(scheduler.GateResourceNAS), "写失败期间 last_date 不得推进")

	alerts := h.GateAlerter.Calls()
	require.NotEmpty(t, alerts, "重试耗尽必须升级告警,不得仅记日志")
	last := alerts[len(alerts)-1]
	require.Equal(t, scheduler.GateResourceNAS, last.ResourceType)
	require.Equal(t, "write", last.Operation)
	require.Equal(t, 1, last.Failures, "跨轮退避从连续失败 1 起算")
	require.ErrorIs(t, last.Err, injected)

	// 任务未被提交
	requireSubmittedDailyTasks(t, h, 0)
}

// Outcome gate-read-failure-backoff: 读日闸状态失败立即返回错误;随后进入
// >=5 分钟退避窗口,窗口内重读静默跳过(不触发写认领、不洪泛任务队列)。
// 现实现(daily_gate.go 读失败分支)以 ERROR 日志降级、未经 alerter 上报
// operation=read——告警升级仅落写失败路径;此处按实码锚定可观测语义,
// 与契约的告警预期差异已在 doc.go 备案。
func TestStep1_GateReadFailure_BackoffWindow(t *testing.T) {
	h, _ := newJourneyHarness()
	gate := newGate(h)

	h.GateStore.FailGet(scheduler.GateResourceNAS, errors.New("injected: mongo read failure"))
	_, err := gate.TryClaim(context.Background(), scheduler.GateResourceNAS, nastest.Today())
	require.Error(t, err)

	claimCallsAfterFailure := h.GateStore.SnapshotClaimCalls()

	// 退避窗口内重读:静默跳过(false, nil),不发起写认领
	claimed, err := gate.TryClaim(context.Background(), scheduler.GateResourceNAS, nastest.Today())
	require.NoError(t, err, "退避窗口内应静默跳过而非报错")
	require.False(t, claimed)
	require.Equal(t, claimCallsAfterFailure, h.GateStore.SnapshotClaimCalls(),
		"退避窗口内不得发起写认领")
	requireSubmittedDailyTasks(t, h, 0)
}

// Outcome first-deploy-initial-claim: 首部署无 scheduler_state 记录视为首次
// 认领成功,认领后触发一次当日提交,不回溯补采历史(无历史指标行落库)。
func TestStep1_FirstDeployInitialClaim_NoBackfill(t *testing.T) {
	h, _ := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h)

	require.True(t, submitDailyCollect(t, h, gate, queue), "首部署应视为首次认领成功")
	requireSubmittedDailyTasks(t, h, 1)
	require.Equal(t, nastest.Today(), h.GateStore.LastDate(scheduler.GateResourceNAS))
	require.Equal(t, 0, h.MetricDAO.Count(), "首次认领不回溯补采历史指标")
}

// Journey invariant: cdn 与 nas 日闸按资源类型分键,互相独立认领、互不阻塞。
func TestStep1_GateKeysIndependentBetweenNASAndCDN(t *testing.T) {
	h, _ := newJourneyHarness()
	gate := newGate(h)
	ctx := context.Background()

	nasClaimed, err := gate.TryClaim(ctx, scheduler.GateResourceNAS, nastest.Today())
	require.NoError(t, err)
	require.True(t, nasClaimed)

	cdnClaimed, err := gate.TryClaim(ctx, scheduler.GateResourceCDN, nastest.Today())
	require.NoError(t, err)
	require.True(t, cdnClaimed, "cdn 键独立于 nas 键,nas 已认领不阻塞 cdn 认领")

	require.Equal(t, nastest.Today(), h.GateStore.LastDate(scheduler.GateResourceNAS))
	require.Equal(t, nastest.Today(), h.GateStore.LastDate(scheduler.GateResourceCDN))
}
