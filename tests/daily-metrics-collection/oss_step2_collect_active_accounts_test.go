// @feature oss-ops-insight @api-functional
//
// Contract oss step-2-collect-active-accounts: 按活跃账号遍历 bucket 采集 —
// the executor collects the [yesterday, today] window per active account
// with per-bucket bounded concurrency, and a single failing bucket counts
// into failed_buckets/failures without blocking the rest.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package daily_metrics_collection

import (
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// Outcome two-day-window-collected: 执行 oss:collect_metrics(days=2) — 每个
// bucket 产出昨日与今日两组指标;任务 Result 含 date_range、metrics_total、
// accounts;账号级互斥 + bucket 有界并发复用生产模式。
func TestOSSStep2_TwoDayWindowCollected(t *testing.T) {
	h, provider := ossNewHarness()
	ossSeedBucketAccount(t, h, provider, 1, "web-assets", "log-archive")

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
	require.Equal(t, 4, nastest.ResultField(t, task, "metrics_total").(int), "两 bucket × 两日 = 4 条")
	require.Equal(t, 1, nastest.ResultField(t, task, "accounts").(int))

	// 厂商查询区间透传 [昨日, 今日](bucket 有界并发下逐 bucket 一次查询)
	calls := provider.Querier.Calls()
	require.Len(t, calls, 2)
	for _, call := range calls {
		require.Equal(t, osstest.DayOffset(-1), call.Start)
		require.Equal(t, osstest.Today(), call.End)
	}

	require.Equal(t, 4, h.MetricDAO.Count())
}

// Outcome single-failure-isolated: 遍历中某个 bucket 的厂商查询失败 — 失败
// bucket 计入 failed_buckets 与 failures 明细;其余 bucket 照常产出两组指标;
// 全流程不中断。
func TestOSSStep2_SingleFailureIsolated(t *testing.T) {
	h := osstest.NewHarness()
	p := h.NewPerAccountProvider()
	h.SeedAccountNamed(1, p.Name)
	h.SeedAccountNamed(2, p.Name)
	// 账号 1:好 bucket;账号 2:一个必然失败的 bucket + 一个好 bucket
	h.SeedBucket(1, p.Name, "good-bucket-a")
	h.SeedBucket(2, p.Name, "failing-bucket")
	h.SeedBucket(2, p.Name, "good-bucket-b")

	p.QuerierFor(1).SetMetrics(
		osstest.Metric("good-bucket-a", osstest.DayOffset(-1), 100, 10),
		osstest.Metric("good-bucket-a", osstest.Today(), 110, 11),
	)
	p.QuerierFor(2).FailBucket("failing-bucket", errors.New("injected: bucket-level vendor failure"))
	p.QuerierFor(2).SetMetrics(
		osstest.Metric("good-bucket-b", osstest.DayOffset(-1), 200, 20),
		osstest.Metric("good-bucket-b", osstest.Today(), 210, 21),
	)

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err, "单 bucket 失败不得中断全流程")

	// 失败 bucket 计入 failed_buckets 与 failures 明细
	require.Equal(t, 1, nastest.ResultField(t, task, "failed_buckets").(int))
	failures := nastest.ResultFailures(t, task)
	require.Len(t, failures, 1)
	require.Equal(t, int64(2), failures[0].AccountID)
	require.Contains(t, failures[0].LastError, "bucket-level vendor failure")

	// 失败 bucket 不产生待写行;同账号其余 bucket 及其他账号照常产出两组指标
	_, ok := h.MetricDAO.Row(2, "failing-bucket", osstest.Today())
	require.False(t, ok, "失败 bucket 不得落库")
	require.Equal(t, 4, h.MetricDAO.Count(), "两个好 bucket × 两日 = 4 行")
	_, ok = h.MetricDAO.Row(1, "good-bucket-a", osstest.Today())
	require.True(t, ok)
	_, ok = h.MetricDAO.Row(2, "good-bucket-b", osstest.Today())
	require.True(t, ok)
}
