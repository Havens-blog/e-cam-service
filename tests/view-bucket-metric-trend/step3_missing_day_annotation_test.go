// @feature oss-ops-insight @api-functional
//
// Contract step-3-missing-day-annotation: 缺失日以 data_status 标注 — days
// with no metric row are annotated data_status=missing with null
// storage_size/object_count ( never 0 or neighbor values ), while collected
// days return their real values.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package view_bucket_metric_trend

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// Outcome missing-day-annotated-no-fake-value: 窗口内存在采集断档(昨日行缺失,
// 前日与今日有行)— 缺失日 data_status=missing 且字段为 null,不填假值;
// 已有日正常返回。
func TestStep3_MissingDayAnnotatedNoFakeValue(t *testing.T) {
	h, provider := newJourneyHarness()
	seedTrendAccount(t, h, provider, 1)
	// 窗口 3 天:前日有行、昨日断档、今日有行
	seedSingleDay(t, h, provider, 1, -2, 300, 30)
	seedSingleDay(t, h, provider, 1, 0, 320, 32)

	resp, err := h.NewQueryService().GetBucketMetrics(context.Background(), 1, 1, trendBucket, 3)
	require.NoError(t, err)
	require.Len(t, resp.Days, 3)

	byDate := map[string]dayPoint{}
	for _, point := range resp.Days {
		byDate[point.Date] = dayPoint{status: point.DataStatus, storage: point.StorageSize, objects: point.ObjectCount}
	}

	// 已有日:真实值正常返回
	dMinus2 := byDate[osstest.DayOffset(-2)]
	require.Equal(t, "ok", dMinus2.status)
	require.NotNil(t, dMinus2.storage)
	require.Equal(t, float64(300), *dMinus2.storage)

	// 缺失日:只标注,绝不填假值(0/邻近值冒充)
	missing := byDate[osstest.DayOffset(-1)]
	require.Equal(t, "missing", missing.status, "断档日必须显式标注 missing")
	require.Nil(t, missing.storage, "缺失日 storage_size 为 null,不得填 0 或邻近值 300/320")
	require.Nil(t, missing.objects, "缺失日 object_count 为 null")

	// 均值跳过缺失日:300 与 320 的均值 310,而非被缺失日拉低
	require.NotNil(t, resp.Average.StorageSize)
	require.InDelta(t, 310, *resp.Average.StorageSize, 1e-9)
}

// dayPoint 趋势点断言形状(指针保留 null 语义)。
type dayPoint struct {
	status  string
	storage *float64
	objects *int64
}
