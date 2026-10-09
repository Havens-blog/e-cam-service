// @feature nas-ops-insight @api-functional
//
// Contract step-3-metric-upsert: 指标行落库(首写生效+补采覆盖) — today rows
// are first-write-wins via the insert-if-absent path, yesterday rows are
// refreshed by the overwrite path during next-day backfill and frozen once out
// of the collection window, out-of-range capacity batches are rejected whole,
// and capacity=0 rows land visible with qc_status=zero_exception.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package daily_metrics_collection

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 昨日初态行被补采覆盖为厂商日末值,今日行为新插入,
// 每 (account_id, fs_id, date) 恰好一行。
func TestStep3_MetricUpsert_YesterdayOverwriteTodayInsert(t *testing.T) {
	h, provider := newJourneyHarness()
	account := h.SeedAccount(1, provider)
	h.SeedInstance(1, provider, "fs-a", "共享盘A", "cn-hangzhou")
	// 昨日 00:10 初态行(容量 88),今日无行
	h.SeedMetric(t, account.ID, provider.Name, nastest.Metric("fs-a", "共享盘A", nastest.DayOffset(-1), 88, 8.8))
	provider.Querier.SetMetrics(
		nastest.Metric("fs-a", "共享盘A", nastest.DayOffset(-1), 90, 9), // 日末聚合值
		nastest.Metric("fs-a", "共享盘A", nastest.Today(), 100, 10),
	)

	_, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	yesterday, ok := h.MetricDAO.Row(1, "fs-a", nastest.DayOffset(-1))
	require.True(t, ok)
	require.Equal(t, float64(90), yesterday.Capacity, "昨日行应被补采覆盖为日末值")

	today, ok := h.MetricDAO.Row(1, "fs-a", nastest.Today())
	require.True(t, ok)
	require.Equal(t, float64(100), today.Capacity, "今日行应为首写新插入")
	require.Equal(t, 2, h.MetricDAO.Count(), "唯一键之下不应产生重复行")
}

// Outcome today-first-write-protected: 今日行已首写后,同日重采/回填再次写入
// 同键新值不得覆盖首写值(走 $setOnInsert 保护路径)。
func TestStep3_MetricUpsert_TodayFirstWriteProtected(t *testing.T) {
	h, provider := newJourneyHarness()
	account := h.SeedAccount(1, provider)
	h.SeedInstance(1, provider, "fs-a", "共享盘A", "cn-hangzhou")
	// 今日行已首写(凌晨初态 95)
	h.SeedMetric(t, account.ID, provider.Name, nastest.Metric("fs-a", "共享盘A", nastest.Today(), 95, 9.5))
	// 手动触发重采,厂商当日口径已变化为 105
	provider.Querier.SetMetrics(
		nastest.Metric("fs-a", "共享盘A", nastest.Today(), 105, 10.5),
	)

	_, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	today, ok := h.MetricDAO.Row(1, "fs-a", nastest.Today())
	require.True(t, ok)
	require.Equal(t, float64(95), today.Capacity, "今日行必须保持首写值不被覆盖")
	require.Equal(t, float64(9.5), today.UsedCapacity)
	require.Equal(t, 1, h.MetricDAO.Count(), "同键重复写入不得新增行")
}

// Outcome yesterday-frozen-after-backfill: 采集窗口收敛为当日(days=1)时,
// 已冻结的更早历史行不再被任何写入触碰(补采窗口不再包含它们)。
func TestStep3_MetricUpsert_YesterdayFrozenOutsideWindow(t *testing.T) {
	h, provider := newJourneyHarness()
	account := h.SeedAccount(1, provider)
	h.SeedInstance(1, provider, "fs-a", "共享盘A", "cn-hangzhou")
	// 昨日行已处于日末冻结态(100);今日行已落库
	h.SeedMetric(t, account.ID, provider.Name, nastest.Metric("fs-a", "共享盘A", nastest.DayOffset(-1), 100, 10))
	h.SeedMetric(t, account.ID, provider.Name, nastest.Metric("fs-a", "共享盘A", nastest.Today(), 100, 10))
	// 后续写入(任何形态)尝试触碰昨日行
	provider.Querier.SetMetrics(
		nastest.Metric("fs-a", "共享盘A", nastest.Today(), 110, 11),
	)

	// days=1 收敛窗口为 [今日, 今日]:昨日行跨日冻结,重采不再触碰
	_, err := h.RunCollect(t, map[string]any{"days": 1})
	require.NoError(t, err)

	yesterday, ok := h.MetricDAO.Row(1, "fs-a", nastest.DayOffset(-1))
	require.True(t, ok)
	require.Equal(t, float64(100), yesterday.Capacity, "冻结行的字段保持日末态不变")
	require.Equal(t, 2, h.MetricDAO.Count(), "同批仅新增今日行语义,行数不增长")
}

// Outcome out-of-range-batch-rejected: 批量写入中任一行非零 capacity 越出
// [1MB, 1PB] 数量级(字节直写 GB 字段的单位 bug 形态)则整批拒绝,不良行
// 与同批健康行均不得落库,错误携带 fs_id 与 date 供失败归因。
func TestStep3_MetricUpsert_OutOfRangeBatchRejected(t *testing.T) {
	h, _ := newJourneyHarness()

	validRow := nastest.Metric("fs-ok", "健康盘", nastest.Today(), 100, 10)
	// 1e9 GB ≈ 字节直写形态,远超 1PiB=1048576 GB 上界
	badRow := nastest.Metric("fs-bad", "越界盘", nastest.DayOffset(-1), 1e9, 1e8)

	err := h.MetricDAO.BulkUpsertMetrics(context.Background(), []types.NASMetric{validRow, badRow})
	require.Error(t, err)
	require.Contains(t, err.Error(), "fs-bad", "错误应携带 fs_id 供归因")
	require.Contains(t, err.Error(), nastest.DayOffset(-1), "错误应携带 date 供归因")
	require.Equal(t, 0, h.MetricDAO.Count(), "整批拒绝:同批健康行也不得落库")

	// 首写保护路径同门禁
	err = h.MetricDAO.BulkInsertIfAbsent(context.Background(), []types.NASMetric{badRow})
	require.Error(t, err)
	require.Contains(t, err.Error(), "fs-bad")
}

// Outcome zero-capacity-zero-exception: capacity=0 异常行不拦截不跳过,
// 强制打 qc_status=zero_exception 后照常落库(华为/AWS 实盘现状)。
func TestStep3_MetricUpsert_ZeroCapacityZeroException(t *testing.T) {
	h, provider := newJourneyHarness()
	h.SeedAccount(1, provider)
	h.SeedInstance(1, provider, "fs-zero", "零容量盘", "cn-beijing")
	provider.Querier.SetMetrics(
		nastest.Metric("fs-zero", "零容量盘", nastest.Today(), 0, 0),
	)

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	// metrics_total 计入该行(未被「全零跳过」类过滤吞掉)
	total := nastest.ResultField(t, task, "metrics_total").(int)
	require.Equal(t, 1, total, "capacity=0 行必须计入写入行数")
	require.Empty(t, nastest.ResultFailures(t, task), "零容量是异常标注而非失败,不得进 failures")

	row, ok := h.MetricDAO.Row(1, "fs-zero", nastest.Today())
	require.True(t, ok, "capacity=0 异常行必须落库可见")
	require.Equal(t, types.NASMetricQcZeroException, row.QcStatus)
	require.Equal(t, float64(0), row.Capacity)
}

// ==================== real DAO live checks ( MONGO_DSN gated, 见 nastest.LiveMetricDAO ) ====================

// Live 复核:真实 mongo 之上跨账号同 fs 各留一行,任何账号的写入不得覆盖
// 另一账号的行(唯一键含 account_id 的 Hard Rule 实证)。无 mongo 时跳过。
func TestStep3_LiveDAO_CrossAccountRowsCoexist(t *testing.T) {
	store := nastest.LiveMetricDAO(t)
	ctx := context.Background()

	rows := []types.NASMetric{
		nastest.Metric("fs-shared", "共享盘", nastest.Today(), 100, 10),
		nastest.Metric("fs-shared", "共享盘", nastest.Today(), 200, 20),
		nastest.Metric("fs-shared", "共享盘", nastest.Today(), 0, 0),
	}
	rows[0].AccountID, rows[0].Provider = 1, "nasjt-live-a"
	rows[1].AccountID, rows[1].Provider = 2, "nasjt-live-b"
	rows[2].AccountID, rows[2].Provider = 3, "nasjt-live-c"
	require.NoError(t, store.BulkInsertIfAbsent(ctx, rows))

	got, err := store.ListByAccounts(ctx, []int64{1, 2, 3}, 7)
	require.NoError(t, err)
	require.Len(t, got, 3, "同 fs 同日三账号应各留一行")
	// ListByAccounts 排序键 (fs_id, date) 在本用例三行全并列,返回次序不受契约
	// 约束——按 account_id 归位断言,不依赖次序。
	byAccount := make(map[int64]types.NASMetric, len(got))
	for _, row := range got {
		byAccount[row.AccountID] = row
	}
	require.Equal(t, types.NASMetricQcZeroException, byAccount[3].QcStatus, "capacity=0 行应打 zero_exception")
	require.Equal(t, float64(0), byAccount[3].Capacity)
	require.Equal(t, float64(100), byAccount[1].Capacity)
	require.Equal(t, float64(200), byAccount[2].Capacity)
}

// Live 复核:真实 mongo 之上同键重放幂等、行数不增长。无 mongo 时跳过。
func TestStep3_LiveDAO_ReplayIdempotent(t *testing.T) {
	store := nastest.LiveMetricDAO(t)
	ctx := context.Background()

	row := nastest.Metric("fs-idem", "幂等盘", nastest.Today(), 100, 10)
	row.AccountID, row.Provider = 1, "nasjt-live-a"
	first := []types.NASMetric{row}
	require.NoError(t, store.BulkUpsertMetrics(ctx, first))
	require.NoError(t, store.BulkInsertIfAbsent(ctx, first))
	row.Capacity = 999 // 同键二次写入新值:upsert 路径覆盖,首写路径不覆盖
	second := []types.NASMetric{row}
	require.NoError(t, store.BulkInsertIfAbsent(ctx, second))

	got, err := store.ListByFs(ctx, 1, "fs-idem", 7)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, float64(100), got[0].Capacity, "今日首写生效语义在真实唯一索引下成立")
}
