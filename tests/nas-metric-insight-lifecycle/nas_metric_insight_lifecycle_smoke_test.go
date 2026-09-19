// @feature nas-ops-insight @api-functional
//
// Journey smoke: nas-metric-insight-lifecycle happy path — gate claim submits
// the daily collect, vendor metrics land as GB rows, the operator reads the
// per-fs trend, then consumes the ops-card aggregation and the Top ranking to
// identify the high-watermark instance. Journey invariants verified across
// steps: fs-level dedup in aggregation, per-account isolation in trend reads,
// derived utilization never persisted.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package nas_metric_insight_lifecycle

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

func TestNASMetricInsightLifecycle_FullJourneySmoke(t *testing.T) {
	h, provider := newJourneyHarness()

	// Step 1: 触发当日采集(持久化日闸认领 + 提交 days=2 任务)
	require.True(t, claimAndSubmit(t, h, newGate(h)))
	require.Len(t, h.TaskRepo.Tasks(), 1)

	// Step 2: 厂商指标按 GB 口径落库
	h.SeedAccount(1, provider)
	h.SeedInstance(1, provider, "fs-a", "共享盘A", "cn-hangzhou")
	provider.Querier.SetMetrics(
		nastest.Metric("fs-a", "共享盘A", nastest.DayOffset(-1), 90, 45),
		nastest.Metric("fs-a", "共享盘A", nastest.Today(), 200, 50),
	)
	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Equal(t, 2, nastest.ResultField(t, task, "metrics_total").(int))

	router := h.NewNASRouter(h.TenantID)
	query := h.NewQueryService()

	// Step 3: 运营查看单实例近 30 天趋势
	status, env := nastest.GetJSON(t, router, "/assets/nas/metrics?fs_id=fs-a&account_id=1&days=30")
	require.Equal(t, 200, status)
	var trend service.NASFsMetricsResp
	require.NoError(t, json.Unmarshal(env.Data, &trend))
	require.Equal(t, "fs-a", trend.FsID)
	require.InDelta(t, 0.25, *trend.Days[len(trend.Days)-1].Utilization, 1e-9)

	// Step 4: 运营卡聚合(多账号共享 fs 场景由 Top 承载)
	acc2 := h.SeedAccount(2, provider)
	h.SeedInstance(2, provider, "fs-a", "共享盘A", "cn-hangzhou")
	h.SeedMetric(t, acc2.ID, provider.Name,
		nastest.Metric("fs-a", "共享盘A", nastest.Today(), 400, 120))

	// Step 5: Top 排行识别高水位实例(共享 fs 不双计)
	top, err := query.GetTop(context.Background(), h.TenantID, 0, 30, service.NASSortUtilization, 10, 1, 10)
	require.NoError(t, err)
	require.Equal(t, 1, top.Total, "两账号同 fs 聚合去重后 total=1")
	item := top.Items[0]
	require.Equal(t, "fs-a", item.FsID)
	require.Equal(t, []int64{1, 2}, item.AccountIDs)
	require.NotNil(t, item.Latest.Capacity)
	require.Equal(t, float64(400), *item.Latest.Capacity, "代表行取同日容量最大行")

	// 下钻:Top 识别的高水位 fs 可凭 fs_id 趋势下钻(账号视角各自序列)
	trend2, err := query.GetFsMetrics(context.Background(), h.TenantID, acc2.ID, "fs-a", 30)
	require.NoError(t, err)
	require.InDelta(t, 0.3, *trend2.Days[len(trend2.Days)-1].Utilization, 1e-9)
}
