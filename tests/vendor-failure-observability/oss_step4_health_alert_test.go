// @feature oss-ops-insight @api-functional
//
// Contract oss step-4-health-alert: 必达厂商持续失败触发健康告警 — a
// mandatory vendor with zero successful writes over the 3-day window and
// >=1 real OSS bucket fires AlertOSSZeroSuccess exactly once per full run
// ( throttled, not flooded per bucket/minute ), best-effort vendors never
// alert, and recovery flips the alert state off.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package vendor_failure_observability

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// Outcome health-alert-fired: 必达厂商(aliyun)连续 3 天零成功写库且实盘存在
// OSS bucket,全量采集运行 — AlertOSSZeroSuccess 触发并明确指出厂商与时间窗。
func TestOSSStep4_HealthAlertFired(t *testing.T) {
	h, provider := ossNewMandatoryHarness(t, "aliyun")
	// 必达厂商实盘存在 bucket,但指标表近 3 天无任何成功写入行
	ossSeedBucketAccount(t, h, provider, 1, "silent-bucket")

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	// 告警触发:厂商 + 零成功时间窗(近 3 天)+ bucket 数前置
	alerts := h.HealthAlerter.Calls()
	require.Len(t, alerts, 1, "零成功必达厂商应触发一次升级告警")
	require.Equal(t, "aliyun", alerts[0].Provider)
	require.Equal(t, 3, alerts[0].WindowDays, "零成功时间窗为近 3 天")
	require.Equal(t, int64(1), alerts[0].BucketCount)
	// 告警随任务 Result 的 health_alerts 可见
	require.Equal(t, []string{"aliyun"}, nastestResultAlerts(t, task))
}

// Outcome best-effort-no-alert: 尽力而为厂商持续无指标能力 — 不触发健康告警;
// 必达厂商窗口内有成功行亦不告警(无误报)。
func TestOSSStep4_BestEffortNoAlert(t *testing.T) {
	h := osstest.NewHarness()
	bestEffort := h.NewNamedProvider("tencent", false) // 尽力而为:探测不支持
	mandatory := h.NewNamedProvider("aliyun", true)
	ossSeedBucketAccount(t, h, bestEffort, 1, "plain-bucket")
	ossSeedBucketAccount(t, h, mandatory, 2, "healthy-bucket")
	// 必达厂商今日有成功写入行(健康)
	mandatory.Querier.SetMetrics(osstest.Metric("healthy-bucket", osstest.Today(), 100, 10))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	require.Empty(t, nastestResultAlerts(t, task), "尽力而为厂商的已知限制不告警")
	require.Empty(t, h.HealthAlerter.Calls(), "告警通道零调用")
}

// Outcome alert-throttled-not-flooding: 告警条件多轮持续满足 — 每轮全量采集
// 每厂商至多一次告警(不逐 bucket/逐分钟洪泛);恢复成功后告警状态解除。
func TestOSSStep4_AlertThrottledNotFlooding(t *testing.T) {
	h, provider := ossNewMandatoryHarness(t, "huawei")
	// 同厂商 3 个 bucket 持续零成功:洪泛形态是「每 bucket × 每轮」告警
	ossSeedBucketAccount(t, h, provider, 1, "silent-1", "silent-2", "silent-3")

	// 连续两轮采集,条件持续满足
	for round := 0; round < 2; round++ {
		_, err := h.RunCollect(t, nil)
		require.NoError(t, err)
	}
	alerts := h.HealthAlerter.Calls()
	require.Len(t, alerts, 2, "每轮每厂商至多一次告警(3 bucket × 2 轮 ≠ 6 次)")

	// 恢复成功后告警状态解除:窗口内出现成功写入行 → 不再告警
	provider.Querier.SetMetrics(
		osstest.Metric("silent-1", osstest.Today(), 100, 10),
		osstest.Metric("silent-2", osstest.Today(), 100, 10),
		osstest.Metric("silent-3", osstest.Today(), 100, 10),
	)
	_, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Len(t, h.HealthAlerter.Calls(), 2, "恢复后告警状态解除,不新增告警")
}
