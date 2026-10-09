// @feature nas-ops-insight @api-functional
//
// Contract step-2-unique-key-write: 唯一键各行独立落库 — the (account_id,
// fs_id, date) unique key keeps one row per account with no overwrite/merge
// across accounts, today rows are first-write-wins, next-day backfill updates
// each account's own row only, and interleaved writes keep both rows intact.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multi_account_shared_fs

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 三账号同日各留一行,互不覆盖、互不合并;跨账号同 fs
// 并存语义成立。
func TestStep2_UniqueKeyWrite_ThreeRowsCoexist(t *testing.T) {
	h, _ := newJourneyHarness()
	dao := h.MetricDAO
	ctx := context.Background()
	rows := sharedRows(map[int64]float64{1: 100, 2: 200, 3: 300})
	require.NoError(t, dao.BulkInsertIfAbsent(ctx, rows))

	got, err := dao.ListByFs(ctx, 1, SharedFS, 7)
	require.NoError(t, err)
	require.Len(t, got, 1, "每账号同日恰好一行")
	require.Equal(t, 3, dao.Count(), "同 fs 同日恰好三行,每行 account_id 各异")
}

// Outcome today-first-write-protected: 同一账号同日被重复采集两次,今日行
// 首写生效,第二次写入不覆盖,仅补缺失行。
func TestStep2_UniqueKeyWrite_TodayFirstWriteProtected(t *testing.T) {
	h, _ := newJourneyHarness()
	dao := h.MetricDAO
	ctx := context.Background()

	// 首写(凌晨初态 95)
	first := sharedRows(map[int64]float64{1: 95})
	require.NoError(t, dao.BulkInsertIfAbsent(ctx, first))
	// 同日重采(修正值 105)不得覆盖首写
	second := sharedRows(map[int64]float64{1: 105})
	require.NoError(t, dao.BulkInsertIfAbsent(ctx, second))

	row, ok := dao.Row(1, SharedFS, nastest.Today())
	require.True(t, ok)
	require.Equal(t, float64(95), row.Capacity, "今日行保持首写值")
	require.Equal(t, 1, dao.Count())
}

// Outcome next-day-backfill-account-isolated: 次日补采对三账号昨日行执行
// 覆盖更新,每账号仅覆盖自己的行,补采完成后跨账号互不触碰。
func TestStep2_UniqueKeyWrite_NextDayBackfillAccountIsolated(t *testing.T) {
	h, provider := newJourneyHarness()
	yesterday := nastest.DayOffset(-1)

	// 昨日初态行:三账号各留一行
	seedSharedFSCollect(t, h, provider, map[int64]float64{1: 100, 2: 100, 3: 100})
	_, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	// 次日补采:厂商日末值各账号不同
	for accountID, finalCapacity := range map[int64]float64{1: 110, 2: 220, 3: 330} {
		provider.QuerierFor(accountID).SetMetrics(
			nastest.Metric(SharedFS, "共享盘", yesterday, finalCapacity, finalCapacity*0.3))
	}
	_, err = h.RunCollect(t, nil)
	require.NoError(t, err)

	// 每账号仅覆盖自己的昨日行
	for accountID, want := range map[int64]float64{1: 110, 2: 220, 3: 330} {
		row, ok := h.MetricDAO.Row(accountID, SharedFS, yesterday)
		require.True(t, ok)
		require.Equal(t, want, row.Capacity, "账号 %d 的昨日行被自己的补采值更新", accountID)
	}
}

// Outcome cross-account-no-overwrite: 两账号对同 fs 同日交错写入(任意先后
// 顺序),每账号的行始终保持本账号最新写入值,行数恒为账号数。
func TestStep2_UniqueKeyWrite_CrossAccountNoOverwrite(t *testing.T) {
	h, _ := newJourneyHarness()
	dao := h.MetricDAO
	ctx := context.Background()

	// 交错序列:A 写 → B 写 → A 再写 → B 再写,行数不随写入次数增长
	batchA1 := sharedRows(map[int64]float64{1: 100})
	batchB1 := sharedRows(map[int64]float64{2: 200})
	batchA2 := sharedRows(map[int64]float64{1: 120}) // 同账号重放(覆盖自身)
	batchB2 := sharedRows(map[int64]float64{2: 240})
	require.NoError(t, dao.BulkUpsertMetrics(ctx, batchA1))
	require.NoError(t, dao.BulkUpsertMetrics(ctx, batchB1))
	require.NoError(t, dao.BulkUpsertMetrics(ctx, batchA2))
	require.NoError(t, dao.BulkUpsertMetrics(ctx, batchB2))

	require.Equal(t, 2, dao.Count(), "行数恒为账号数,不随写入次数增长")
	nastest.RequireNoCrossAccountLoss(t, dao, SharedFS, nastest.Today(),
		map[int64]float64{1: 120, 2: 240})
}

// sharedRows 构造三形态共享 fs 行集合(每行缺省字段在 DAO 写入路径补齐)。
func sharedRows(capacities map[int64]float64) []types.NASMetric {
	rows := make([]types.NASMetric, 0, len(capacities))
	for accountID, capacity := range capacities {
		row := nastest.Metric(SharedFS, "共享盘", nastest.Today(), capacity, capacity*0.3)
		row.AccountID = accountID
		row.Provider = "nasjt-shared"
		rows = append(rows, row)
	}
	return rows
}
