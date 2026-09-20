// @feature oss-ops-insight @api-functional
//
// Contract step-5-view-bucket-trend: 用户在抽屉「监控」tab 查看双轴趋势 —
// the trend endpoint returns the capacity(GB) and object-count series plus
// latest/average summaries, all sourced from the metric table; missing days
// are annotated, never faked. unauthorized-401 is exempt ( see doc.go ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package oss_metric_insight_lifecycle

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// Outcome dual-axis-trend-rendered: 返回容量(GB)与对象数两条序列(逐日升序),
// 同时含「最新一天」与「近 N 天均值」两类值;数值全部来自指标表。
func TestStep5_DualAxisTrendRendered(t *testing.T) {
	h, provider := newJourneyHarness()
	seedBucketAccount(t, h, provider, 1, "web-assets")
	seedMetric(t, h, 1, provider.Name, osstest.Metric("web-assets", osstest.DayOffset(-1), 590, 4100))
	seedMetric(t, h, 1, provider.Name, osstest.Metric("web-assets", osstest.Today(), 600, 4200))

	resp, err := h.NewQueryService().GetBucketMetrics(context.Background(), 1, 1, "web-assets", 7)
	require.NoError(t, err)
	require.Equal(t, "web-assets", resp.BucketName)
	require.Len(t, resp.Days, 7, "days 窗口逐日展开(升序)")

	// 两条序列(容量 + 对象数)按日期升序且来自指标表
	require.Equal(t, osstest.DayOffset(-6), resp.Days[0].Date)
	last := resp.Days[len(resp.Days)-1]
	require.Equal(t, osstest.Today(), last.Date)
	require.NotNil(t, last.StorageSize)
	require.Equal(t, float64(600), *last.StorageSize)
	require.NotNil(t, last.ObjectCount)
	require.Equal(t, int64(4200), *last.ObjectCount)
	require.Equal(t, "ok", last.DataStatus)

	// 「最新一天」与「近 N 天均值」两类值齐备
	require.NotNil(t, resp.Latest)
	require.Equal(t, osstest.Today(), resp.Latest.Date)
	require.Equal(t, float64(600), *resp.Latest.StorageSize)
	require.NotNil(t, resp.Average)
	require.InDelta(t, 595, *resp.Average.StorageSize, 1e-9)
}

// Outcome unauthorized: 豁免(见 doc.go)。
func TestStep5_BucketTrend_UnauthorizedExempt(t *testing.T) {
	t.Log("exempt: 401 enforced by global auth middleware outside the OSS surface")
}
