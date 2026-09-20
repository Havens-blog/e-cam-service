// @feature oss-ops-insight @api-functional
//
// Journey smoke: full golden path in sequence — gate claim → task submit →
// collect → persist → bucket trend read → Top read. Only happy-path outcomes;
// per-outcome edge cases live in the step files.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package oss_metric_insight_lifecycle

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// TestOSSMetricInsightLifecycle_FullJourneySmoke 走通 Step1~Step5 happy path:
// 日闸认领提交 → 执行器采集 → 指标行落库 → 趋势/Top 读取,全程以指标表为
// 唯一数据来源,同日重跑不重复提交。
func TestOSSMetricInsightLifecycle_FullJourneySmoke(t *testing.T) {
	h, provider := newJourneyHarness()
	seedBucketAccount(t, h, provider, 1, "web-assets")
	gate := newGate(h)
	queue := newTaskQueue(h)

	// Step 1: 调度器认领 oss 日闸并提交当日采集(days=2)
	require.True(t, submitOSSDailyCollect(t, h, gate, queue), "Step1: 当日认领应成功")
	requireSubmittedOSSDailyTasks(t, h, 1)

	// Step 2+3: 执行器采集并落库(今日首写 + 昨日补采覆盖)
	provider.Querier.SetMetrics(
		osstest.Metric("web-assets", osstest.DayOffset(-1), 530, 4100),
		osstest.Metric("web-assets", osstest.Today(), 600, 4200),
	)
	task, err := h.RunCollect(t, nil)
	require.NoError(t, err, "Step2: 采集全流程不中断")
	require.Equal(t, 2, nastest.ResultField(t, task, "metrics_total").(int), "Step3: 指标行落库条数")
	today, ok := h.MetricDAO.Row(1, "web-assets", osstest.Today())
	require.True(t, ok)
	require.Equal(t, types.OSSMetricQcOK, today.QcStatus, "正常行 qc_status 为空(OK)")

	// Step 4+5: 读取侧一律来自指标表(运营卡聚合 Top + 抽屉趋势)
	resp, err := h.NewQueryService().GetBucketMetrics(context.Background(), 1, 1, "web-assets", 7)
	require.NoError(t, err, "Step5: 趋势读取")
	require.NotNil(t, resp.Latest)
	require.Equal(t, float64(600), *resp.Latest.StorageSize)

	top, err := h.NewQueryService().GetTop(context.Background(), 1, 0, 7, service.OSSSortStorageSize, 10, 1, 10)
	require.NoError(t, err, "Step4: Top 读取")
	require.Len(t, top.Items, 1)
	require.Equal(t, "web-assets", top.Items[0].BucketName)

	// Journey invariant: 当日 oss 采集任务至多提交一次(重启/手动+自动重叠形态)
	require.False(t, submitOSSDailyCollect(t, h, gate, queue), "同日第二轮认领必须失败")
	requireSubmittedOSSDailyTasks(t, h, 1)
}
