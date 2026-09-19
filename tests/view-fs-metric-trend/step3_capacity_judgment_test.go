// @feature nas-ops-insight @api-functional
//
// Contract step-3-capacity-judgment: 判断扩容时机 — a pure decision step: the
// trend data must be sufficient to express "the day with the highest watermark
// in the near-N-day window" ( peak = max of the day-value series; the day-end
// snapshot architecture does not promise intra-day spikes ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package view_fs_metric_trend

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 趋势数据足以表达「近 N 天里水位最高的那一天」— 7 日窗口内
// 水位逐日抬升、峰值日在 -2 日,日值序列最大值即近 N 天峰值口径。
func TestStep3_CapacityJudgment_PeakDayIdentifiable(t *testing.T) {
	h, provider := newJourneyHarness()
	acc := h.SeedAccount(1, provider)

	// 7 日窗口:used 逐日抬升,-2 日达峰(90/100),昨日回落
	peaks := map[int]float64{-6: 40, -5: 50, -4: 60, -3: 75, -2: 90, -1: 70}
	for offset, used := range peaks {
		h.SeedMetric(t, acc.ID, provider.Name,
			nastest.Metric("fs-scale", "扩容盘", nastest.DayOffset(offset), 100, used))
	}
	h.SeedMetric(t, acc.ID, provider.Name,
		nastest.Metric("fs-scale", "扩容盘", nastest.Today(), 100, 55))

	trend, err := h.NewQueryService().GetFsMetrics(context.Background(), h.TenantID, acc.ID, "fs-scale", 7)
	require.NoError(t, err)

	// 日值序列最大值即近 N 天峰值口径:峰值为 -2 日的 0.9
	peakUtil, peakDate := 0.0, ""
	for _, day := range trend.Days {
		if day.Utilization != nil && *day.Utilization > peakUtil {
			peakUtil, peakDate = *day.Utilization, day.Date
		}
	}
	require.InDelta(t, 0.9, peakUtil, 1e-9, "日值序列最大值表达近 N 天峰值")
	require.Equal(t, nastest.DayOffset(-2), peakDate, "可识别水位最高的那一天")

	// 日末态快照口径:每日恰一个值,不承诺日内尖峰
	byDate := map[string]int{}
	for _, day := range trend.Days {
		byDate[day.Date]++
		require.LessOrEqual(t, byDate[day.Date], 1, "每日至多一个快照值")
	}
}
