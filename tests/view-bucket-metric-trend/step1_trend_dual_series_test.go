// @feature oss-ops-insight @api-functional
//
// Contract step-1-trend-dual-series: 查看单 bucket 容量/对象数趋势 — the
// trend endpoint returns both series with latest/average summaries sourced
// from the metric table, answers empty states without fabricating values
// ( no-data and collect-failure distinguishable ), and unauthorized-401 is
// exempt ( see doc.go ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package view_bucket_metric_trend

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// Outcome dual-series-with-latest-and-average: 返回容量(GB)与对象数两条序列
// (逐日升序),同时含「最新一天」与「近 N 天均值」两类值;数值全部来自指标表。
func TestStep1_DualSeriesWithLatestAndAverage(t *testing.T) {
	h, provider := newJourneyHarness()
	seedTrendAccount(t, h, provider, 1)
	seedTrendWindow(t, h, provider, 1, 7, 100)

	resp, err := h.NewQueryService().GetBucketMetrics(context.Background(), 1, 1, trendBucket, 7)
	require.NoError(t, err)
	require.Equal(t, trendBucket, resp.BucketName)
	require.Len(t, resp.Days, 7)

	// 容量 + 对象数两条序列逐日升序
	require.Equal(t, osstest.DayOffset(-6), resp.Days[0].Date)
	require.Equal(t, osstest.Today(), resp.Days[len(resp.Days)-1].Date)
	last := resp.Days[len(resp.Days)-1]
	require.NotNil(t, last.StorageSize)
	require.NotNil(t, last.ObjectCount)
	require.Equal(t, float64(106), *last.StorageSize, "容量序列来自指标表(最新日 106)")
	require.Equal(t, int64(16), *last.ObjectCount, "对象数序列来自指标表")
	require.Equal(t, "ok", last.DataStatus)

	// 「最新一天」与「近 N 天均值」两类值
	require.NotNil(t, resp.Latest)
	require.Equal(t, osstest.Today(), resp.Latest.Date)
	require.InDelta(t, 106, *resp.Latest.StorageSize, 1e-9)
	require.NotNil(t, resp.Average)
	require.InDelta(t, 103, *resp.Average.StorageSize, 1e-9, "均值为近 7 天窗口聚合")

	// 响应形状锚:双轴图所需字段齐备(date/storage_size/object_count/data_status)
	raw, err := json.Marshal(resp.Days[len(resp.Days)-1])
	require.NoError(t, err)
	for _, field := range []string{"date", "storage_size", "object_count", "data_status", "qc_status"} {
		require.Contains(t, string(raw), field, "趋势点缺少双轴图字段 %s", field)
	}
}

// Outcome no-data-empty-state: 新纳管 bucket 尚无采集行 — 空态返回(非报错),
// days 序列为逐日 missing 标注;不构造空趋势假图。
func TestStep1_NoDataEmptyState(t *testing.T) {
	h, provider := newJourneyHarness()
	seedTrendAccount(t, h, provider, 1)

	resp, err := h.NewQueryService().GetBucketMetrics(context.Background(), 1, 1, trendBucket, 7)
	require.NoError(t, err, "无数据是空态而非错误")
	require.Len(t, resp.Days, 7)
	for _, point := range resp.Days {
		require.Equal(t, "missing", point.DataStatus)
		require.Nil(t, point.StorageSize)
		require.Nil(t, point.ObjectCount)
	}
	require.Nil(t, resp.Latest, "无可用行时 latest 为 null(不构造假图)")
	require.Nil(t, resp.Average.StorageSize)
}

// Outcome collect-failure-warning-empty-state: 采集持续失败/未启用 — 趋势
// 空态保持不填假值,同时失败信号在采集侧可辨(failures/failed_buckets),
// 空态可区分「采集失败」与「无数据」。
func TestStep1_CollectFailureWarningEmptyState(t *testing.T) {
	h, provider := newJourneyHarness()
	seedTrendAccount(t, h, provider, 1)
	provider.Querier.Fail(errors.New("injected: vendor API down"))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	// 失败信号可辨:failures 明细 + failed_buckets 计数(警示来源,区别于纯无数据)
	require.NotEmpty(t, nastest.ResultFailures(t, task))
	require.Equal(t, 1, nastest.ResultField(t, task, "failed_buckets").(int))

	// 趋势空态:不显示 0 冒充数据
	resp, err := h.NewQueryService().GetBucketMetrics(context.Background(), 1, 1, trendBucket, 7)
	require.NoError(t, err)
	for _, point := range resp.Days {
		require.Equal(t, "missing", point.DataStatus)
		require.Nil(t, point.StorageSize)
	}
	require.Nil(t, resp.Latest)
}

// Outcome unauthorized: 豁免(见 doc.go)。
func TestStep1_TrendDualSeries_UnauthorizedExempt(t *testing.T) {
	t.Log("exempt: 401 enforced by global auth middleware outside the OSS surface")
}

// service 形状锚:响应结构与 handler 层一致(接口契约)。
func TestStep1_TrendResponseShape_HandlerConsistent(t *testing.T) {
	h, provider := newJourneyHarness()
	seedTrendAccount(t, h, provider, 1)
	seedTrendWindow(t, h, provider, 1, 3, 50)

	router := h.NewOSSRouter(1)
	status, env := osstest.GetJSON(t, router, trendPath(1, 3))
	require.Equal(t, 200, status)

	var resp service.OSSBucketMetricsResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.Len(t, resp.Days, 3)
	require.NotNil(t, resp.Latest)
	require.NotNil(t, resp.Average)
}
