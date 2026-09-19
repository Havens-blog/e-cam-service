// @feature nas-ops-insight @api-functional
//
// Journey smoke: view-fs-metric-trend happy path — request the trend, read
// the derived utilization with data_status annotations, and identify the peak
// watermark day for the expansion-timing decision.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package view_fs_metric_trend

import (
	"encoding/json"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

func TestViewFsMetricTrend_FullJourneySmoke(t *testing.T) {
	h, provider := newJourneyHarness()
	acc := h.SeedAccount(1, provider)
	h.SeedMetric(t, acc.ID, provider.Name,
		nastest.Metric("fs-a", "共享盘A", nastest.DayOffset(-1), 100, 25))
	h.SeedMetric(t, acc.ID, provider.Name,
		nastest.Metric("fs-a", "共享盘A", nastest.Today(), 200, 50))

	// Step 1: 请求单实例趋势(200,升序)
	router := h.NewNASRouter(1)
	status, env := nastest.GetJSON(t, router, "/assets/nas/metrics?fs_id=fs-a&account_id=1&days=30")
	require.Equal(t, 200, status)
	var trend service.NASFsMetricsResp
	require.NoError(t, json.Unmarshal(env.Data, &trend))
	require.Len(t, trend.Days, 30)

	// Step 2: 解读派生使用率与数据状态
	today := trend.Days[len(trend.Days)-1]
	require.Equal(t, service.NASDataStatusOK, today.DataStatus)
	require.InDelta(t, 0.25, *today.Utilization, 1e-9)
	missing := 0
	for _, day := range trend.Days {
		if day.DataStatus == service.NASDataStatusMissing {
			missing++
		}
	}
	require.Equal(t, 28, missing)

	// Step 3: 判断扩容时机(峰值日 = 日值序列最大值)
	peakUtil := 0.0
	for _, day := range trend.Days {
		if day.Utilization != nil && *day.Utilization > peakUtil {
			peakUtil = *day.Utilization
		}
	}
	require.InDelta(t, 0.25, peakUtil, 1e-9)
}
