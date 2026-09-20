// @feature oss-ops-insight @api-functional
//
// Contract oss step-3-persist-two-window-rows: 指标行落库(首写生效 + 补采覆盖)
// — today rows are first-write-wins via the insert-if-absent path, yesterday
// rows are refreshed by the overwrite path during next-day backfill, and the
// two window semantics never bleed into each other.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package daily_metrics_collection

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// Outcome today-first-write-yesterday-overwrite: 今日行首写生效新增,昨日行
// 从凌晨初态被补采覆盖为厂商日末聚合值;metrics_total 相应增加。
func TestOSSStep3_TodayFirstWriteYesterdayOverwrite(t *testing.T) {
	h, provider := ossNewHarness()
	ossSeedBucketAccount(t, h, provider, 1, "web-assets")
	// 昨日 00:10 初态行(容量 512),今日无行
	h.SeedMetric(t, 1, provider.Name, osstest.Metric("web-assets", osstest.DayOffset(-1), 512, 4000))
	provider.Querier.SetMetrics(
		osstest.Metric("web-assets", osstest.DayOffset(-1), 530, 4100), // 日末聚合值
		osstest.Metric("web-assets", osstest.Today(), 600, 4200),
	)

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Equal(t, 2, nastest.ResultField(t, task, "metrics_total").(int), "今日首写 1 条 + 昨日覆盖 1 条")

	yesterday, ok := h.MetricDAO.Row(1, "web-assets", osstest.DayOffset(-1))
	require.True(t, ok)
	require.Equal(t, float64(530), yesterday.StorageSize, "昨日行应被补采覆盖为日末值")
	today, ok := h.MetricDAO.Row(1, "web-assets", osstest.Today())
	require.True(t, ok)
	require.Equal(t, float64(600), today.StorageSize, "今日行应为首写新插入")
	require.Equal(t, 2, h.MetricDAO.Count(), "唯一键之下不应产生重复行")
}

// Outcome today-row-never-overwritten: 今日行已首写后,同日重复写入同键新值
// 不得覆盖首写值($setOnInsert 保护路径),仅补当日缺失行。
func TestOSSStep3_TodayRowNeverOverwritten(t *testing.T) {
	h, provider := ossNewHarness()
	ossSeedBucketAccount(t, h, provider, 1, "web-assets")
	// 今日行已首写(凌晨初态 595)
	h.SeedMetric(t, 1, provider.Name, osstest.Metric("web-assets", osstest.Today(), 595, 4050))
	// 同日重采,厂商当日口径已变化为 605
	provider.Querier.SetMetrics(osstest.Metric("web-assets", osstest.Today(), 605, 4150))

	_, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	today, ok := h.MetricDAO.Row(1, "web-assets", osstest.Today())
	require.True(t, ok)
	require.Equal(t, float64(595), today.StorageSize, "今日行必须保持首写值不被覆盖")
	require.Equal(t, int64(4050), today.ObjectCount)
	require.Equal(t, 1, h.MetricDAO.Count(), "同键重复写入不得新增行")
}

// Outcome yesterday-row-frozen-after-backfill: 采集窗口收敛为当日(days=1)时,
// 昨日行不再被任何今日写入触碰(跨日冻结,两窗口语义不互相渗透)。
func TestOSSStep3_YesterdayRowFrozenAfterBackfill(t *testing.T) {
	h, provider := ossNewHarness()
	ossSeedBucketAccount(t, h, provider, 1, "web-assets")
	// 昨日行已处于日末冻结态;今日行已落库
	h.SeedMetric(t, 1, provider.Name, osstest.Metric("web-assets", osstest.DayOffset(-1), 530, 4100))
	h.SeedMetric(t, 1, provider.Name, osstest.Metric("web-assets", osstest.Today(), 600, 4200))
	// 后续写入尝试触碰昨日行(厂商返回昨日新口径)
	provider.Querier.SetMetrics(
		osstest.Metric("web-assets", osstest.DayOffset(-1), 999, 999),
		osstest.Metric("web-assets", osstest.Today(), 610, 4210),
	)

	// days=1 收敛窗口为 [今日, 今日]:昨日行跨日冻结,重采不再触碰
	_, err := h.RunCollect(t, map[string]any{"days": 1})
	require.NoError(t, err)

	yesterday, ok := h.MetricDAO.Row(1, "web-assets", osstest.DayOffset(-1))
	require.True(t, ok)
	require.Equal(t, float64(530), yesterday.StorageSize, "冻结行的字段保持日末态不变")
	today, ok := h.MetricDAO.Row(1, "web-assets", osstest.Today())
	require.True(t, ok)
	require.Equal(t, float64(600), today.StorageSize, "今日行独立首写保护")
	require.Equal(t, 2, h.MetricDAO.Count(), "行数不增长")
}
