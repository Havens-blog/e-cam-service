// @feature disk-ops-insight @api-functional
//
// Contract disk-day-gate-resilience step-3-restart-consistency: after today's
// claim/commit, three simulated restarts each observe exactly one same-day
// execution ( the persisted state prevents duplicates and never loses the
// claim ).
//
// Exemptions ( see doc.go ): feature-flag-rollback and no-gate-assembly-
// safe-skip live in unexported scheduler methods covered by the internal
// suite internal/cam/scheduler/auto_sync_disk_metrics_test.go.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package disk_day_gate_resilience

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 当日已认领/已提交后服务重启 ×3 — 每次重启(全新 gate 实例
// 加载同一持久化存储)当日采集各仅触发 1 条(共 1 次成功执行);持久化状态
// 防止重复采集,也不丢当日认领。
func TestStep3_RestartConsistency_Success(t *testing.T) {
	h := newJourneyHarness()
	gate := newGate(h)
	queue := newTaskQueue(h)

	// 一个最小采集世界:认领后的当日执行真实写 1 行今日指标
	provider := h.NewProvider(true)
	account := h.SeedAccount(1, provider)
	h.SeedDisk(account.ID, provider.Name, "dsk-restart", "cn-test-1")
	provider.Querier.SetMetrics(disktest.Metric("dsk-restart", disktest.Today(), 55.5, 120, 8.5))

	// 重启前:当日认领 + 提交,一次成功执行
	require.True(t, submitDiskDailyCollect(t, h, gate, queue))
	_, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Equal(t, 1, h.MetricDAO.Count(), "首次执行应恰好写入 1 行今日指标")

	// 模拟重启 ×3:每次全新 gate 实例 + 全新队列视图,同一持久化存储
	for i := 1; i <= 3; i++ {
		restartedGate := newGate(h)
		require.False(t, submitDiskDailyCollect(t, h, restartedGate, queue),
			"第 %d 次重启后当日不应重复采集", i)
	}
	requireSubmittedDiskDailyTasks(t, h, 1, "重启 ×3 后任务仓储仍应只有当日 1 条")
	require.Equal(t, disktest.Today(), h.GateStore.LastDate(scheduler.GateResourceDisk),
		"重启不丢当日认领")
	require.Equal(t, 1, h.MetricDAO.Count(), "重启 ×3 不产生重复指标写入")
}

// Outcome feature-flag-rollback: 豁免(见 doc.go)——内存闸回滚分支在未导出的
// 调度器方法 checkDiskMetricsCollectionMemory 内,由 internal 套件
// auto_sync_disk_metrics_test.go 覆盖;导出面(持久化日闸)无法观测特性开关。
func TestStep3_FeatureFlagRollback_Exempt(t *testing.T) {
	t.Log("exempt: memory-gate rollback branch covered by internal/cam/scheduler/auto_sync_disk_metrics_test.go")
}

// Outcome no-gate-assembly-safe-skip: 豁免(见 doc.go)——dailyGate=nil 安全跳过
// 分支同属未导出的调度器方法,由 internal 套件覆盖。
func TestStep3_NoGateAssemblySafeSkip_Exempt(t *testing.T) {
	t.Log("exempt: nil-gate safe-skip branch covered by internal/cam/scheduler/auto_sync_disk_metrics_test.go")
}
