// @feature nas-ops-insight @api-functional
//
// Contract step-4-dedup-aggregation: 聚合层去重消费 — aggregation takes the
// "latest date then largest capacity" first row as the physical fs's capacity
// basis ( no cross-account sum / average ), dedups shared fs into one item,
// and Top item account_id is the deduped ascending account list.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multi_account_shared_fs

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 聚合按 fs_id 去重,取「日期 desc 再容量 desc」第一行代表
// 物理 fs,不双计、不跨账号求和/平均;Top 单条目 account_id 为去重升序列表。
func TestStep4_DedupAggregation_RepresentativeAndAccountList(t *testing.T) {
	h, provider, _ := seedThreeAccountAgg(t, map[int64]float64{1: 100, 2: 300, 3: 200})

	top, err := h.NewQueryService().GetTop(context.Background(), h.TenantID, 0, 30,
		service.NASSortCapacity, 10, 1, 10)
	require.NoError(t, err)
	require.Equal(t, 1, top.Total, "共享 fs 去重后只计一次")
	require.Len(t, top.Items, 1)

	item := top.Items[0]
	require.Equal(t, SharedFS, item.FsID)
	require.NotNil(t, item.Latest.Capacity)
	require.Equal(t, float64(300), *item.Latest.Capacity, "同日内取容量最大行为代表行,不求和(600)/平均(200)")
	require.Equal(t, []int64{1, 2, 3}, item.AccountIDs, "恰好覆盖全部采集账号的去重升序列表")
	_ = provider
}

// Outcome representative-row-capacity-basis: 共享 fs 不同账号容量口径不同、
// 且跨日期分布时,以最新日期行为口径(同日内再取容量最大行)。
func TestStep4_DedupAggregation_RepresentativeRowCapacityBasis(t *testing.T) {
	h, provider, _ := seedThreeAccountAgg(t, map[int64]float64{1: 100, 2: 200, 3: 300})

	// 账号 1 另有昨日行(容量 900,大于今日任何口径)——不参与今日代表行
	yesterday := nastest.DayOffset(-1)
	provider.QuerierFor(1).SetMetrics(
		nastest.Metric(SharedFS, "共享盘", yesterday, 900, 90),
		nastest.Metric(SharedFS, "共享盘", nastest.Today(), 100, 30))
	_, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	top, err := h.NewQueryService().GetTop(context.Background(), h.TenantID, 0, 30,
		service.NASSortCapacity, 10, 1, 10)
	require.NoError(t, err)
	require.Len(t, top.Items, 1)
	item := top.Items[0]
	require.NotNil(t, item.Latest.Capacity)
	require.Equal(t, float64(300), *item.Latest.Capacity,
		"以最新日期行(今日)的容量最大行 300 为口径,旧日期 900 不抬高今日口径")
}

// Outcome account-id-list-dedup-asc: 共享 fs 被 ≥3 账号采集且窗口内各有行时,
// Top 条目的 account_id 为去重升序列表,无重复项。
func TestStep4_DedupAggregation_AccountIDListDedupAsc(t *testing.T) {
	h, _, _ := seedThreeAccountAgg(t, map[int64]float64{1: 100, 2: 200, 3: 300})

	top, err := h.NewQueryService().GetTop(context.Background(), h.TenantID, 0, 30,
		service.NASSortUtilization, 10, 1, 10)
	require.NoError(t, err)
	require.Len(t, top.Items, 1)
	ids := top.Items[0].AccountIDs
	require.Equal(t, []int64{1, 2, 3}, ids)
	require.Len(t, ids, len(map[int64]struct{}{1: {}, 2: {}, 3: {}}), "无重复项")
}

// seedThreeAccountAgg 三账号各自采集共享 fs(当日行)。
func seedThreeAccountAgg(t *testing.T, capacities map[int64]float64) (*nastest.Harness, *nastest.PerAccountProvider, []int64) {
	t.Helper()
	h, provider := newJourneyHarness()
	ids := seedSharedFSCollect(t, h, provider, capacities)
	_, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	return h, provider, ids
}
