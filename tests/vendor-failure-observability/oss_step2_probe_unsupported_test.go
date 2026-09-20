// @feature oss-ops-insight @api-functional
//
// Contract oss step-2-probe-unsupported-classification: 尽力而为厂商探测不
// supported 归类为非失败 — probe-unsupported is an INFO-level empty result
// ( no failure counter, no alert ), recovery resets the per-round failure
// counter, and the probe attribution is recorded ( no_metric_support ) rather
// than silently swallowed.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package vendor_failure_observability

import (
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// Outcome probe-unsupported-info-not-failure: 尽力而为厂商(tencent)实盘无
// OSS 容量指标 — INFO 日志+空返回,不算失败、不进 failures,与「调用失败」
// 明确区分。
func TestOSSStep2_ProbeUnsupportedInfoNotFailure(t *testing.T) {
	h := osstest.NewHarness()
	plain := h.NewNamedProvider("tencent", false) // 未实现 OSSMetricQuerier(探测不支持)
	mandatory := h.NewNamedProvider("aliyun", true)
	plainAcc := h.SeedAccount(1, plain)
	mandatoryAcc := h.SeedAccount(2, mandatory)
	h.SeedBucket(1, plain.Name, "plain-bucket")
	h.SeedBucket(2, mandatory.Name, "ok-bucket")
	mandatory.Querier.SetMetrics(osstest.Metric("ok-bucket", osstest.Today(), 150, 15))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	// 探测不支持的失败口径:不入 failures、不计 failed_buckets、不触发告警
	require.Equal(t, []string{"tencent"}, nastest.ResultStrings(t, task, "no_metric_support"))
	require.Empty(t, nastest.ResultFailures(t, task))
	require.Equal(t, 0, nastest.ResultField(t, task, "failed_buckets").(int))
	require.Empty(t, nastest.ResultAlerts(t, task))
	require.Empty(t, h.HealthAlerter.Calls())

	// 其余厂商采集不受影响;探测不支持厂商无指标行
	_, ok := h.MetricDAO.Row(mandatoryAcc.ID, "ok-bucket", osstest.Today())
	require.True(t, ok)
	_, ok = h.MetricDAO.Row(plainAcc.ID, "plain-bucket", osstest.Today())
	require.False(t, ok)
}

// Outcome recovery-success-counter-reset: 厂商 API 短暂故障后恢复 — 恢复后
// 正常落库,failures 计数按轮次归位(不无限累计历史失败),缺失日由补采
// (days=2 两日窗口)覆盖。
func TestOSSStep2_RecoverySuccessCounterReset(t *testing.T) {
	h, provider := ossNewHarness()
	ossSeedBucketAccount(t, h, provider, 1, "ok-bucket")

	// 第一轮:故障,failures 非空
	provider.Querier.Fail(errors.New("injected: transient vendor outage"))
	round1, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.NotEmpty(t, nastest.ResultFailures(t, round1))

	// 第二轮:恢复,计数按轮次归位,两日窗口补采覆盖缺失日
	provider.Querier.Recover()
	provider.Querier.SetMetrics(
		osstest.Metric("ok-bucket", osstest.DayOffset(-1), 210, 21), // 昨日缺失日补齐
		osstest.Metric("ok-bucket", osstest.Today(), 220, 22),
	)
	round2, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Empty(t, nastest.ResultFailures(t, round2), "恢复轮 failures 必须归位为空")
	require.Equal(t, 2, nastest.ResultField(t, round2, "metrics_total").(int), "指标不丢日:两日窗口补齐")

	// 缺失日被补齐落库
	_, ok := h.MetricDAO.Row(1, "ok-bucket", osstest.DayOffset(-1))
	require.True(t, ok, "上一轮缺失的昨日行由补采覆盖")
	_, ok = h.MetricDAO.Row(1, "ok-bucket", osstest.Today())
	require.True(t, ok)
}

// Outcome probe-attribution-recorded: 探测归因显式记录 — no_metric_support
// 携带厂商清单(归因可查询),不伪装数据、不静默吞掉原因。
func TestOSSStep2_ProbeAttributionRecorded(t *testing.T) {
	h := osstest.NewHarness()
	plain := h.NewNamedProvider("volcengine", false)
	ossSeedBucketAccount(t, h, plain, 1, "plain-bucket")

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	// 归因显式记录在 Result(订阅未开通/指标不存在 → 二期补+开通路径的
	// 运营线索),不产生伪装指标行
	support := nastest.ResultStrings(t, task, "no_metric_support")
	require.Contains(t, support, "volcengine", "探测归因必须显式记录厂商")
	require.Equal(t, 0, h.MetricDAO.Count(), "探测不支持绝不产生伪装指标行")
}
