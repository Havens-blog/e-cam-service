// @feature oss-ops-insight @api-functional
//
// Journey smoke: the OSS daily-metrics golden path in sequence — gate claim
// on the oss key → task submit ( days=2 ) → collect → two-window persist →
// same-day re-claim rejected. Only happy-path outcomes; per-outcome edge
// cases live in the oss_stepN files.
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

func TestOSSDailyMetricsCollection_FullJourneySmoke(t *testing.T) {
	h, provider := ossNewHarness()
	ossSeedBucketAccount(t, h, provider, 1, "web-assets")
	gate := ossNewGate(h)
	queue := ossNewTaskQueue(h)

	// Step 1: oss 日闸认领并提交当日采集(days=2)
	require.True(t, ossSubmitDailyCollect(t, h, gate, queue), "当日认领应成功")
	ossRequireSubmittedDailyTasks(t, h, 1)

	// Step 2+3: 执行器采集并落库(今日首写 + 昨日补采覆盖)
	provider.Querier.SetMetrics(
		osstest.Metric("web-assets", osstest.DayOffset(-1), 530, 4100),
		osstest.Metric("web-assets", osstest.Today(), 600, 4200),
	)
	task, err := h.RunCollect(t, nil)
	require.NoError(t, err, "Step2: 采集全流程不中断")
	require.Equal(t, 2, nastest.ResultField(t, task, "metrics_total").(int), "Step3: 两窗口落库 2 条")
	today, ok := h.MetricDAO.Row(1, "web-assets", osstest.Today())
	require.True(t, ok)
	require.Equal(t, float64(600), today.StorageSize, "今日行首写生效")
	yesterday, ok := h.MetricDAO.Row(1, "web-assets", osstest.DayOffset(-1))
	require.True(t, ok)
	require.Equal(t, float64(530), yesterday.StorageSize, "昨日行补采覆盖")

	// Step 4(不变量): 当日重复认领一律拒绝,重启不重复提交
	require.False(t, ossSubmitDailyCollect(t, h, ossNewGate(h), queue))
	ossRequireSubmittedDailyTasks(t, h, 1)
}
