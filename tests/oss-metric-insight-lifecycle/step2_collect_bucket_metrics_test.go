// @feature oss-ops-insight @api-functional
//
// Contract step-2-collect-bucket-metrics: 按活跃账号遍历 bucket 采集容量/对象数
// — the executor collects a [yesterday, today] window per active account
// ( active = >=1 OSS bucket in the local asset enumeration, no EnableAutoSync
// dependency ), vendor call failures land in Result.failures without blocking
// the rest, and probe-unsupported vendors are INFO-skipped, never failures.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package oss_metric_insight_lifecycle

import (
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// Outcome collected-all-active-accounts: days=2 全量采集,每个 bucket 产出
// 昨日与今日两组指标;Result 含 date_range/metrics_total/accounts;容量落在
// [1MB, 1PB] 数量级门禁内;活跃账号口径不依赖 EnableAutoSync。
func TestStep2_CollectedAllActiveAccounts(t *testing.T) {
	h, provider := newJourneyHarness()
	seedBucketAccount(t, h, provider, 1, "web-assets", "log-archive")

	provider.Querier.SetMetrics(
		osstest.Metric("web-assets", osstest.DayOffset(-1), 512, 4096),
		osstest.Metric("web-assets", osstest.Today(), 600, 4200),
		osstest.Metric("log-archive", osstest.DayOffset(-1), 1024, 100),
		osstest.Metric("log-archive", osstest.Today(), 1100, 120),
	)

	task, err := h.RunCollect(t, map[string]any{"days": 2})
	require.NoError(t, err)

	require.Equal(t, osstest.DayOffset(-1)+" ~ "+osstest.Today(),
		nastest.ResultField(t, task, "date_range"), "采集区间应为 [昨日, 今日]")
	require.Equal(t, 4, nastest.ResultField(t, task, "metrics_total"), "两 bucket × 两日 = 4 条")
	require.Equal(t, 1, nastest.ResultField(t, task, "accounts"))

	// 厂商查询区间透传 [昨日, 今日]
	calls := provider.Querier.Calls()
	require.Len(t, calls, 2, "两个 bucket 各一次厂商查询")
	for _, call := range calls {
		require.Equal(t, osstest.DayOffset(-1), call.Start)
		require.Equal(t, osstest.Today(), call.End)
	}

	// 数量级门禁内:行按唯一键逐项落库
	row, ok := h.MetricDAO.Row(1, "web-assets", osstest.Today())
	require.True(t, ok)
	require.Equal(t, float64(600), row.StorageSize)
	require.Equal(t, int64(4200), row.ObjectCount)
	require.Equal(t, 4, h.MetricDAO.Count())
}

// Outcome vendor-call-failure: 必达厂商调用失败只影响自身 — 失败明细入
// Result.failures(provider/account_id/error_count/last_error),其他厂商账号
// 照常产出指标,全流程不中断。
func TestStep2_VendorCallFailure_Isolated(t *testing.T) {
	h := osstest.NewHarness()
	bad := h.NewNamedProvider("aliyun", true)  // 故障必达厂商(字面量命中厂商语义)
	good := h.NewNamedProvider("huawei", true) // 正常必达厂商
	badAcc := h.SeedAccount(1, bad)
	goodAcc := h.SeedAccount(2, good)
	h.SeedBucket(1, bad.Name, "broken-bucket")
	h.SeedBucket(2, good.Name, "healthy-bucket")

	bad.Querier.Fail(errors.New("injected: vendor monitoring API down"))
	good.Querier.SetMetrics(osstest.Metric("healthy-bucket", osstest.Today(), 200, 20))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err, "单厂商失败不得反噬整轮采集任务")

	failures := nastest.ResultFailures(t, task)
	require.Len(t, failures, 1, "仅故障厂商账号计入失败明细")
	f := failures[0]
	require.Equal(t, "aliyun", f.Provider)
	require.Equal(t, badAcc.ID, f.AccountID)
	require.Equal(t, 1, f.ErrorCount)
	require.Contains(t, f.LastError, "vendor monitoring API down")

	// 正常厂商照常落库;故障厂商不落库
	require.Equal(t, 1, h.MetricDAO.Count())
	_, ok := h.MetricDAO.Row(goodAcc.ID, "healthy-bucket", osstest.Today())
	require.True(t, ok)
	_, ok = h.MetricDAO.Row(badAcc.ID, "broken-bucket", osstest.Today())
	require.False(t, ok)
}

// Outcome vendor-no-metric-support: 尽力而为厂商未实现指标查询 — INFO 语义
// 跳过,空返回不计失败、不进 failures;Result 的 no_metric_support 含该厂商;
// 全流程不中断。
func TestStep2_VendorNoMetricSupport_InfoNotFailure(t *testing.T) {
	h := osstest.NewHarness()
	plain := h.NewNamedProvider("tencent", false) // 未实现 OSSMetricQuerier
	good := h.NewNamedProvider("aws", true)
	plainAcc := h.SeedAccount(1, plain)
	goodAcc := h.SeedAccount(2, good)
	h.SeedBucket(1, plain.Name, "plain-bucket")
	h.SeedBucket(2, good.Name, "supported-bucket")
	good.Querier.SetMetrics(osstest.Metric("supported-bucket", osstest.Today(), 300, 30))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	require.Equal(t, []string{"tencent"}, nastest.ResultStrings(t, task, "no_metric_support"),
		"探测不支持厂商应入 no_metric_support 清单")
	require.Empty(t, nastest.ResultFailures(t, task), "探测不支持不计失败")
	require.Equal(t, 0, nastest.ResultField(t, task, "failed_buckets"))

	// 探测不支持厂商不产生指标行;其余账号不受影响
	_, ok := h.MetricDAO.Row(plainAcc.ID, "plain-bucket", osstest.Today())
	require.False(t, ok)
	_, ok = h.MetricDAO.Row(goodAcc.ID, "supported-bucket", osstest.Today())
	require.True(t, ok)
}
