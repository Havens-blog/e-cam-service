// @feature nas-ops-insight @api-functional
//
// Contract step-3-high-watermark-drilldown + Journey smoke: identify the
// high-watermark instance from the ranking and drill down into the trend
// drawer by fs_id — ranking reflects real watermark ( no shared-fs double
// counting ), zero_exception rows stay separately visible, and the drilled fs
// yields a full trend series.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package watermark_overview_top

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 排行反映真实水位;共享 fs 不因多账号并存而双计失真;
// 高水位实例可凭 fs_id 下钻到抽屉趋势。
func TestStep3_HighWatermarkDrilldown_Success(t *testing.T) {
	h, _ := seedDrilldownWorld(t)
	query := h.NewQueryService()
	ctx := context.Background()

	// Top 排行(均值使用率口径):fs-hot 0.95 居首,共享 fs 只计一次
	top, err := query.GetTop(ctx, h.TenantID, 0, 30, service.NASSortUtilization, 10, 1, 10)
	require.NoError(t, err)
	require.Equal(t, 3, top.Total, "fs-hot/fs-shared/fs-zero 各计一次(共享 fs 不双计)")
	hottest := top.Items[0]
	require.Equal(t, "fs-hot", hottest.FsID, "排行首位为真实高水位实例")
	require.InDelta(t, 0.95, *hottest.Average.Utilization, 1e-9)

	// 下钻:凭 fs_id + Top 条目携带的 account_id 请求趋势抽屉
	drilldown, err := query.GetFsMetrics(ctx, h.TenantID, hottest.AccountIDs[0], hottest.FsID, 30)
	require.NoError(t, err)
	today := drilldown.Days[len(drilldown.Days)-1]
	require.Equal(t, service.NASDataStatusOK, today.DataStatus)
	require.NotNil(t, today.Utilization)
	require.InDelta(t, 0.95, *today.Utilization, 1e-9, "下钻趋势与排行口径一致")

	// zero_exception 行单独可见,不混入正常排行语义(均值分母剔除后垫底)
	zeroItem := top.Items[2]
	require.Equal(t, "fs-zero", zeroItem.FsID)
	require.Equal(t, service.NASDataStatusZeroException, zeroItem.DataStatus)
}

// seedDrilldownWorld:账号 1 的高水位盘(fs-hot 0.95)+ 零容量异常盘;
// 账号 2 与账号 1 共享 fs-shared。
func seedDrilldownWorld(t *testing.T) (*nastest.Harness, *nastest.Provider) {
	t.Helper()
	h, provider := newJourneyHarness()
	acc1 := h.SeedAccount(1, provider)
	acc2 := h.SeedAccount(2, provider)
	h.SeedMetric(t, acc1.ID, provider.Name,
		nastest.Metric("fs-hot", "高水位盘", nastest.Today(), 400, 380))
	h.SeedMetric(t, acc1.ID, provider.Name,
		nastest.Metric("fs-zero", "零容量盘", nastest.Today(), 0, 0))
	h.SeedMetric(t, acc1.ID, provider.Name,
		nastest.Metric("fs-shared", "共享盘", nastest.Today(), 100, 50))
	h.SeedMetric(t, acc2.ID, provider.Name,
		nastest.Metric("fs-shared", "共享盘", nastest.Today(), 300, 90))
	return h, provider
}

// Journey smoke: watermark-overview-top happy path — ops-card dedup, Top
// ranking, high-watermark drilldown.
func TestWatermarkOverviewTop_FullJourneySmoke(t *testing.T) {
	h, _ := seedDrilldownWorld(t)
	router := h.NewNASRouter(1)
	ctx := context.Background()

	// Step 1: 运营卡聚合(fs 去重)
	top, err := h.NewQueryService().GetTop(ctx, h.TenantID, 0, 30,
		service.NASSortCapacity, 10, 1, 10)
	require.NoError(t, err)
	require.Equal(t, 3, top.Total)

	// Step 2: Top 排行(200 + 元数据)
	status, env := nastest.GetJSON(t, router, "/assets/nas/top?days=30&sort=utilization")
	require.Equal(t, 200, status)
	var resp service.NASTopResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.Len(t, resp.Items, 3)

	// Step 3: 高水位下钻
	hottest := resp.Items[0]
	drilldown, err := h.NewQueryService().GetFsMetrics(ctx, h.TenantID, hottest.AccountIDs[0], hottest.FsID, 30)
	require.NoError(t, err)
	require.Equal(t, "fs-hot", drilldown.FsID)
}
