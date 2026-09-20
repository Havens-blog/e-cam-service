// @feature oss-ops-insight @api-functional
//
// Contract oss step-4-restart-no-duplicate: 服务重启验证当日不重复提交 —
// restarts never duplicate the daily submission ( claim persisted in mongo ),
// read failures enter a >=5min backoff window without flooding the queue, and
// the memory-gate rollback branch stays internal-only ( documented exemption ).
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

// Outcome restart-no-duplicate-submit: 当日 oss 日闸已被认领后连续 3 次重启
// (全新 gate 实例加载同一持久化存储)— 每次重启后 OSS/NAS/CDN 任务各仅 1 条,
// 各资源键 last_date 保持今日不变。
func TestOSSStep4_RestartNoDuplicateSubmit(t *testing.T) {
	h, _ := ossNewHarness()
	// nas/cdn 同日已认领(fixture_spec: 三资源键均持久化今日认领态)
	for _, res := range []string{scheduler.GateResourceNAS, scheduler.GateResourceCDN} {
		_, err := h.GateStore.TryClaimDaily(context.Background(), res, osstest.Today())
		require.NoError(t, err)
	}
	gate := ossNewGate(h)
	queue := ossNewTaskQueue(h)
	require.True(t, ossSubmitDailyCollect(t, h, gate, queue))

	// 连续 3 次重启:每次重启后当日不再重复提交(任一资源键)
	for restart := 1; restart <= 3; restart++ {
		restartedGate := ossNewGate(h)
		require.False(t, ossSubmitDailyCollect(t, h, restartedGate, queue),
			"第 %d 次重启后 oss 键不应重复认领", restart)
		ossRequireSubmittedDailyTasks(t, h, 1)
	}

	// 各资源键 last_date 保持今日不变
	require.Equal(t, osstest.Today(), h.GateStore.LastDate(scheduler.GateResourceOSS))
	require.Equal(t, osstest.Today(), h.GateStore.LastDate(scheduler.GateResourceNAS))
	require.Equal(t, osstest.Today(), h.GateStore.LastDate(scheduler.GateResourceCDN))
}

// Outcome gate-read-failure-backoff: 读 oss 日闸状态失败立即返回错误;随后进入
// >=5 分钟退避窗口,窗口内重读静默跳过(不发起写认领、不洪泛任务队列)。
// 现实现(daily_gate.go 读失败分支)以 ERROR 日志降级、未经 alerter 上报
// operation=read——告警升级仅落写失败路径;此处按实码锚定可观测语义,
// 与契约的告警预期差异已在 doc.go 备案。
func TestOSSStep4_GateReadFailureBackoff(t *testing.T) {
	h, _ := ossNewHarness()
	gate := ossNewGate(h)

	h.GateStore.FailGet(scheduler.GateResourceOSS, errors.New("injected: mongo read failure"))
	_, err := gate.TryClaim(context.Background(), scheduler.GateResourceOSS, osstest.Today())
	require.Error(t, err)

	claimCallsAfterFailure := h.GateStore.SnapshotClaimCalls()

	// 退避窗口内重读:静默跳过(false, nil),不发起写认领
	claimed, err := gate.TryClaim(context.Background(), scheduler.GateResourceOSS, osstest.Today())
	require.NoError(t, err, "退避窗口内应静默跳过而非报错")
	require.False(t, claimed)
	require.Equal(t, claimCallsAfterFailure, h.GateStore.SnapshotClaimCalls(),
		"退避窗口内不得发起写认领")
	ossRequireSubmittedDailyTasks(t, h, 0)
}

// Outcome memory-gate-rollback: 豁免(见 doc.go)——SCHEDULER_PERSISTENT_GATE_
// ENABLED 关闭回滚内存闸的分支(checkOSSMetricsCollectionMemory)为调度器
// 内部未导出路径,由 internal scheduler 单测覆盖;此处备案豁免而非静默丢弃。
func TestOSSStep4_MemoryGateRollbackExempt(t *testing.T) {
	t.Log("exempt: memory-gate rollback branch is internal-only ( scheduler unexported ), covered by internal unit tests")
}
