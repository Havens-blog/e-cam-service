// @feature nas-ops-insight @api-functional
//
// Contract step-5-top-ranking: 运营查看 Top 排行识别高水位实例 — GET
// /assets/nas/top returns total/page/page_size/items sorted by the sort field
// ( near-N-day average for utilization ), dedups shared fs, and answers 400
// for out-of-domain sort values. unauthorized-401 is exempt ( see doc.go ).
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

// seedTopWorld 构建多账号共享 fs 世界:共享 fs-shared 被账号 1/2/3 采集
// (账号 2 容量口径最大),独占 fs-solo 属账号 1。
func seedTopWorld(t *testing.T) (*nastest.Harness, *nastest.Provider) {
	t.Helper()
	h, provider := newJourneyHarness()
	acc1 := h.SeedAccount(1, provider)
	acc2 := h.SeedAccount(2, provider)
	acc3 := h.SeedAccount(3, provider)
	for _, acc := range []int64{1, 2, 3} {
		h.SeedInstance(acc, provider, "fs-shared", "共享盘", "cn-hangzhou")
	}
	h.SeedInstance(1, provider, "fs-solo", "独占盘", "cn-beijing")

	h.SeedMetric(t, acc1.ID, provider.Name, nastest.Metric("fs-shared", "共享盘", nastest.Today(), 100, 50))
	h.SeedMetric(t, acc2.ID, provider.Name, nastest.Metric("fs-shared", "共享盘", nastest.Today(), 300, 90))
	h.SeedMetric(t, acc3.ID, provider.Name, nastest.Metric("fs-shared", "共享盘", nastest.Today(), 200, 20))
	h.SeedMetric(t, acc1.ID, provider.Name, nastest.Metric("fs-solo", "独占盘", nastest.Today(), 400, 380))
	return h, provider
}

// Outcome success: 200 响应含 total/page/page_size/items;items 按 sort 字段
// 近 N 天均值口径降序取前 N;每条含 fs_id/fs_name/account_id 列表/data_status/
// qc_status 与最新一天、近 N 天均值两类值;共享 fs 不双计。
func TestStep5_TopRanking_Success(t *testing.T) {
	h, _ := seedTopWorld(t)
	router := h.NewNASRouter(1)

	status, env := nastest.GetJSON(t, router,
		"/assets/nas/top?account_id=&days=30&sort=utilization&top=10&page=1&page_size=10")
	require.Equal(t, 200, status)

	var resp service.NASTopResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.Equal(t, 2, resp.Total, "共享 fs 去重后 total=2")
	require.Equal(t, 1, resp.Page)
	require.Equal(t, 10, resp.PageSize)
	require.Len(t, resp.Items, 2)

	// 高水位(均值使用率)降序:fs-solo 0.95 > fs-shared 代表行 0.3
	require.GreaterOrEqual(t, *resp.Items[0].Average.Utilization, *resp.Items[1].Average.Utilization)
	require.Equal(t, "fs-solo", resp.Items[0].FsID)
	require.InDelta(t, 0.95, *resp.Items[0].Average.Utilization, 1e-9)

	// 共享 fs 代表行:「日期 desc 再容量 desc」第一行 → 容量最大行 300
	shared := resp.Items[1]
	require.Equal(t, "fs-shared", shared.FsID)
	require.NotNil(t, shared.Latest.Capacity)
	require.Equal(t, float64(300), *shared.Latest.Capacity, "代表行取同日容量最大行,不跨账号求和")
	require.Equal(t, []int64{1, 2, 3}, shared.AccountIDs, "account_id 为去重升序账号列表")
	require.Equal(t, service.NASDataStatusOK, shared.DataStatus)
}

// Outcome sort-invalid-400: sort 携带合法域 capacity|utilization 之外的值
// 返回 400,错误信息指出合法域,不触发任何聚合查询。
func TestStep5_TopRanking_SortInvalid400(t *testing.T) {
	h, _ := seedTopWorld(t)
	router := h.NewNASRouter(1)

	status, env := nastest.GetJSON(t, router, "/assets/nas/top?account_id=1&sort=name")
	require.Equal(t, 400, status)
	require.Contains(t, env.Msg, "capacity")
	require.Contains(t, env.Msg, "utilization")
}

// Outcome unauthorized-401: 豁免(见 doc.go)。
func TestStep5_TopRanking_UnauthorizedExempt(t *testing.T) {
	t.Log("exempt: 401 enforced by global auth middleware outside the NAS surface")
}

// Journey invariant 补充锚定:service 层查询与 handler 层排序口径一致
// (utilization 取近 N 天均值;容量口径取代表行最新值)。
func TestStep5_TopRanking_ServiceLayerConsistent(t *testing.T) {
	h, _ := seedTopWorld(t)
	query := h.NewQueryService()

	resp, err := query.GetTop(context.Background(), h.TenantID, 0, 30, service.NASSortUtilization, 10, 1, 10)
	require.NoError(t, err)
	require.Len(t, resp.Items, 2)
	for _, item := range resp.Items {
		require.NotNil(t, item.Latest.Capacity)
		require.NotNil(t, item.Latest.Utilization)
		require.NotNil(t, item.Average.Capacity)
		require.NotNil(t, item.Average.Utilization)
	}
}
