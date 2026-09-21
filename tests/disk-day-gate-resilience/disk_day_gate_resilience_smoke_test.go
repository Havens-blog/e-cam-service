// @feature disk-ops-insight @api-functional
//
// Journey smoke for disk-day-gate-resilience: the happy path end-to-end —
// step 1 atomic claim submits one disk:collect_metrics task, step 2 commit
// stops same-day repeats, step 3 restarts preserve the persisted fact, step 4
// the four resource keys share the gate without interference.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package disk_day_gate_resilience

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/stretchr/testify/require"
)

func TestDiskDayGateResilience_FullJourneySmoke(t *testing.T) {
	h := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h)

	// Step 1: 持久化日闸原子认领 → 恰好提交一条 disk:collect_metrics(days=2)
	require.True(t, submitDiskDailyCollect(t, h, gate, queue), "step1: 当日认领应胜出")
	requireSubmittedDiskDailyTasks(t, h, 1, "step1: 应恰好一条采集任务")

	// Step 2: 采集成功后提交日闸 → 当日后续调度检查全部跳过
	_, err := h.RunCollect(t, nil)
	require.NoError(t, err, "step2: 采集应成功完成")
	require.False(t, submitDiskDailyCollect(t, h, gate, queue), "step2: 当日后续调度应跳过")

	// Step 3: 服务重启 ×3 → 各仅 1 条,不丢当日认领
	for i := 1; i <= 3; i++ {
		require.False(t, submitDiskDailyCollect(t, h, newGate(h), queue), "step3: 第 %d 次重启后不得重复采集", i)
	}
	requireSubmittedDiskDailyTasks(t, h, 1, "step3: 重启 ×3 仍只有当日 1 条")

	// Step 4: 四资源日闸并行复用 → nas/cdn/oss/disk 独立认领,互不干扰
	for _, res := range []string{scheduler.GateResourceNAS, scheduler.GateResourceCDN, scheduler.GateResourceOSS} {
		claimed, err := h.GateStore.TryClaimDaily(context.Background(), res, disktest.Today())
		require.NoError(t, err)
		require.True(t, claimed, "step4: %s 键应独立认领", res)
	}
	// Journey Invariants: 既有键零回归 + 故障不静默(全程无告警)+ 状态一致
	require.Empty(t, h.GateAlerter.Calls(), "正常旅程不触发任何升级告警")
	require.Equal(t, disktest.Today(), h.GateStore.LastDate(scheduler.GateResourceDisk))
}
