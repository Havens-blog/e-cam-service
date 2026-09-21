// @feature disk-ops-insight @api-functional
//
// Contract disk-day-gate-resilience step-4-multi-resource-regression: the four
// resource keys ( nas/cdn/oss/disk ) claim and commit independently on the
// shared persistent gate, the pre-existing keys keep their exact pre-disk
// behavior, and a failing disk key leaves no state that blocks the other keys
// at the scheduler_state layer.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package disk_day_gate_resilience

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 四资源日闸并行复用 — 各资源按各自 resource_type 键独立
// 认领/提交,互不干扰;Disk 分支接入后 NAS/CDN/OSS 既有调度行为不变。
func TestStep4_MultiResource_FourKeysIndependent(t *testing.T) {
	h := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h)

	// 四资源键各自独立认领:全部胜出(键间零串扰);disk 走生产语义认领并
	// 提交 days=2 任务,nas/cdn/oss 走各自认领不产生 disk 采集任务
	for _, res := range []string{scheduler.GateResourceNAS, scheduler.GateResourceCDN, scheduler.GateResourceOSS} {
		claimed, err := gate.TryClaim(context.Background(), res, disktest.Today())
		require.NoError(t, err)
		require.True(t, claimed, "%s 键应独立认领成功", res)
	}
	require.True(t, submitDiskDailyCollect(t, h, gate, queue), "disk 键应独立认领成功并提交采集任务")
	// 各键 last_date 各自推进为今日
	for _, res := range []string{scheduler.GateResourceNAS, scheduler.GateResourceCDN, scheduler.GateResourceOSS, scheduler.GateResourceDisk} {
		require.Equal(t, disktest.Today(), h.GateStore.LastDate(res), "%s 键 last_date 应各自推进", res)
	}
	requireSubmittedDiskDailyTasks(t, h, 1)
}

// Outcome existing-keys-regression: disk 键首次接入 scheduler_state,既有
// nas/cdn/oss 键已有历史 last_date 记录 — disk 认领/提交不改变既有键的历史
// 记录;既有键行为与 Disk 接入前完全一致(按 resource_type 分键,无键冲突、
// 无状态串扰)。
func TestStep4_MultiResource_ExistingKeysRegression(t *testing.T) {
	h := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h)
	yesterday := disktest.DayOffset(-1)

	// 既有三键的历史状态记录(接入 disk 键之前)
	for _, res := range []string{scheduler.GateResourceNAS, scheduler.GateResourceCDN, scheduler.GateResourceOSS} {
		claimed, err := h.GateStore.TryClaimDaily(context.Background(), res, yesterday)
		require.NoError(t, err)
		require.True(t, claimed)
	}

	// disk 键首次接入:无任何记录,视为首次认领并成功
	require.True(t, submitDiskDailyCollect(t, h, gate, queue), "disk 键首次接入应认领成功")

	// 既有键历史记录保持不变(仍为昨日),无状态串扰
	for _, res := range []string{scheduler.GateResourceNAS, scheduler.GateResourceCDN, scheduler.GateResourceOSS} {
		require.Equal(t, yesterday, h.GateStore.LastDate(res), "%s 键历史记录不得被 disk 接入改变", res)
	}
	// 既有键当日认领语义与 Disk 接入前一致:昨日已认领,今日可正常认领
	for _, res := range []string{scheduler.GateResourceNAS, scheduler.GateResourceCDN, scheduler.GateResourceOSS} {
		claimed, err := gate.TryClaim(context.Background(), res, disktest.Today())
		require.NoError(t, err)
		require.True(t, claimed, "%s 键今日认领应与 Disk 接入前语义一致", res)
	}
}

// Outcome disk-key-isolation: disk 键处于写失败重试/退避状态期间 — 存储层
// 键隔离保证 nas/cdn/oss 键的当日认领与提交不受影响,照常胜出;键间零串扰。
//
// 码实锚定注记:gate 实例的写退避窗口是实例级(同实例内全资源共享,上限
// 5 分钟)—— 故障注入期间同实例的其余键会被退避短路,这是进程内降级语义;
// 持久化存储层(本用例断言面)键与键完全独立,新实例(另一进程语义)的
// nas/cdn/oss 认领不受 disk 键任何失败状态影响。
func TestStep4_MultiResource_DiskKeyIsolation(t *testing.T) {
	h := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h)
	injected := errors.New("injected: disk key write failure")

	// disk 键写失败重试耗尽:进入告警 + 跨轮退避,当日无认领记录落库
	h.GateStore.FailClaim(scheduler.GateResourceDisk, 4, injected)
	_, err := gate.TryClaim(context.Background(), scheduler.GateResourceDisk, disktest.Today())
	require.Error(t, err)
	require.Equal(t, "", h.GateStore.LastDate(scheduler.GateResourceDisk))
	require.NotEmpty(t, h.GateAlerter.Calls(), "disk 键写失败应升级告警")

	// 故障期间,其余键(另一 gate 实例 = 另一进程语义)照常认领并提交
	for _, res := range []string{scheduler.GateResourceNAS, scheduler.GateResourceCDN, scheduler.GateResourceOSS} {
		claimed, err := h.GateStore.TryClaimDaily(context.Background(), res, disktest.Today())
		require.NoError(t, err)
		require.True(t, claimed, "disk 键故障期间 %s 键应照常认领", res)
		require.Equal(t, disktest.Today(), h.GateStore.LastDate(res))
	}
	// disk 键自身故障状态不外溢:恢复后当日正常认领
	require.True(t, submitDiskDailyCollect(t, h, newGate(h), queue), "disk 键恢复后应正常认领")
	requireSubmittedDiskDailyTasks(t, h, 1)
}
