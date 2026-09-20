// @feature oss-ops-insight @api-functional
//
// Journey smoke: the trend-view golden path — collected rows over a multi-day
// window render as an ascending dual series with latest/average summaries, a
// gap is annotated missing without fake values, and a zero_exception day is
// exposed verbatim. Only happy-path outcomes.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package view_bucket_metric_trend

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

func TestViewBucketMetricTrend_FullJourneySmoke(t *testing.T) {
	h, provider := newJourneyHarness()
	seedTrendAccount(t, h, provider, 1)
	// 4 天窗口:前日、断档、今日零异常行(采集异常语义齐备)
	seedSingleDay(t, h, provider, 1, -3, 400, 40)
	seedSingleDay(t, h, provider, 1, -1, 420, 42)
	seedSingleDay(t, h, provider, 1, 0, 0, 3) // 今日:storage_size=0 异常行

	resp, err := h.NewQueryService().GetBucketMetrics(context.Background(), 1, 1, trendBucket, 4)
	require.NoError(t, err)
	require.Len(t, resp.Days, 4)

	// 双序列升序:缺失日标注 missing 不填假值;zero_exception 日原样为 0
	byDate := make(map[string]string, len(resp.Days))
	for _, point := range resp.Days {
		byDate[point.Date] = point.DataStatus
	}
	require.Equal(t, "ok", byDate[osstest.DayOffset(-3)])
	require.Equal(t, "missing", byDate[osstest.DayOffset(-2)], "断档日标注 missing")
	require.Equal(t, "ok", byDate[osstest.DayOffset(-1)])
	require.Equal(t, "zero_exception", byDate[osstest.Today()])

	// 最新一天 = 今日异常零行,原样暴露
	require.NotNil(t, resp.Latest)
	require.Equal(t, float64(0), *resp.Latest.StorageSize)

	// 均值跳过异常零行与缺失日:仅 400/420 参与 → 410
	require.InDelta(t, 410, *resp.Average.StorageSize, 1e-9)

	// qc_status 闭环:异常行原样透出
	today := resp.Days[len(resp.Days)-1]
	require.Equal(t, types.OSSMetricQcZeroException, today.QcStatus)
}
