// @feature disk-ops-insight @api-functional
//
// Contract disk-metrics-daily-collection step-5-result-summary: the task
// Result carries the full observability shape ( metrics_total / accounts /
// date_range / skipped_accounts / no_metric_support / accounts_without_disk /
// failed_disks / failures / health_alerts, progress 100 ), the mandatory-
// provider zero-success health check upgrades to AlertDiskZeroSuccess only on
// full runs with >=1 real disk instance, and partial runs ( single
// account/provider ) skip the health judgment.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package disk_metrics_daily_collection

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 全量采集完成 — 任务 Result 含全部汇总键,progress 达 100;
// 成功/失败分布可查询。
func TestStep5_ResultSummary_Success(t *testing.T) {
	h, provider := newJourneyHarness()
	acc := h.SeedAccount(1, provider)
	h.SeedDisk(acc.ID, provider.Name, "dsk-s1", "cn-test-1")
	provider.Querier.SetMetrics(disktest.Metric("dsk-s1", disktest.Today(), 42.0, 80, 6.0))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	for _, key := range []string{
		"metrics_total", "accounts", "date_range", "skipped_accounts",
		"no_metric_support", "accounts_without_disk", "failed_disks", "failures", "health_alerts",
	} {
		require.Contains(t, task.Result, key, "Result 应含汇总键 %s", key)
	}
	require.Equal(t, 1, task.Result["metrics_total"])
	require.Equal(t, 1, task.Result["accounts"])
	require.Equal(t, 100, task.Progress, "progress 应达 100")
	require.NotEmpty(t, task.Result["date_range"], "date_range 应描述采集区间")
}

// Outcome zero-success-alert: 必达厂商(aliyun)近 3 天零成功写库行且实盘存在
// ≥1 个 Disk 实例,本次为全量运行 — 触发 AlertDiskZeroSuccess 升级告警(携带
// provider/窗口天数/实例数),厂商名入 Result.health_alerts。
func TestStep5_ResultSummary_ZeroSuccessAlert(t *testing.T) {
	h := disktest.NewHarness()
	// 命中必达厂商字面量 aliyun:有实盘实例但窗口内零成功写库行
	aliyun := h.NewNamedProvider("aliyun", true)
	acc := h.SeedAccount(1, aliyun)
	h.SeedDisk(acc.ID, aliyun.Name, "dsk-ali-1", "cn-hangzhou")
	// 故意不 SetMetrics:本轮采集查不到任何行,窗口内零写库证据

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	alerts := h.HealthAlerter.Calls()
	require.NotEmpty(t, alerts, "必达厂商零成功且实盘有实例应升级告警")
	found := false
	for _, a := range alerts {
		if a.Provider == "aliyun" {
			found = true
			require.Equal(t, 3, a.WindowDays, "健康监控窗口应为近 3 天")
			require.GreaterOrEqual(t, a.InstanceCount, int64(1), "告警应携带实例数前置")
		}
	}
	require.True(t, found, "告警应指向 aliyun")
	require.Contains(t, nastest.ResultStrings(t, task, "health_alerts"), "aliyun",
		"厂商名应入 Result.health_alerts")
}

// Outcome partial-run-no-health-judgment: 手动触发且任务参数限定单账号 —
// 跳过必达厂商零成功健康判定,不产生健康告警;其余汇总字段照常输出。
func TestStep5_ResultSummary_PartialRunNoHealthJudgment(t *testing.T) {
	h := disktest.NewHarness()
	aliyun := h.NewNamedProvider("aliyun", true)
	acc := h.SeedAccount(1, aliyun)
	h.SeedDisk(acc.ID, aliyun.Name, "dsk-ali-2", "cn-hangzhou")
	// 零写库行 + 必达厂商,但本次为手动单账号局部运行

	task, err := h.RunCollect(t, map[string]any{"account_id": 1})
	require.NoError(t, err)

	require.Empty(t, h.HealthAlerter.Calls(), "局部运行不得触发健康判定")
	require.Equal(t, 0, len(nastest.ResultStrings(t, task, "health_alerts")),
		"Result 不含 health_alerts 告警项")
	// 其余汇总字段照常输出
	require.Contains(t, task.Result, "metrics_total")
	require.Contains(t, task.Result, "accounts")
}
