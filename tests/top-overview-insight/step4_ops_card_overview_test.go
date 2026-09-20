// @feature oss-ops-insight @api-functional
//
// Contract step-4-ops-card-overview: 查看运营卡总览 — the ops card numbers
// derive from metric-table aggregation over the near-7-day window, and an
// account with no rows answers an empty aggregation ( the collect task Result
// carries the failure/no-data signal the console needs to distinguish the two
// empty states, never faking 0 as data ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package top_overview_insight

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// errInjected 故障注入错误(采集失败形态)。
var errInjected = errors.New("injected: vendor API down")

// Outcome ops-card-7day-growth: 指标数据覆盖近 7 天窗口 — 运营卡数值来自
// 指标表聚合(近 7 天均值/最新值),增速底座数据齐备时正常展示。
func TestStep4_OpsCard7DayGrowth(t *testing.T) {
	h, provider := newJourneyHarness()
	h.SeedAccount(1, provider)
	// 近 7 天窗口逐日行:容量 100→106 线性增长(增速可计算)
	for d := 7; d >= 1; d-- {
		storage := float64(100 + (7 - d))
		h.SeedMetric(t, 1, provider.Name, osstest.Metric("growth-bucket", osstest.DayOffset(-d+1), storage, int64(10+(7-d))))
	}

	top, err := h.NewQueryService().GetTop(context.Background(), 1, 0, 7, service.OSSSortStorageSize, 10, 1, 10)
	require.NoError(t, err)
	require.Len(t, top.Items, 1)
	item := top.Items[0]
	require.Equal(t, "growth-bucket", item.BucketName)

	// 近 7 天均值(103)与最新一天(106)齐备:增速由前端按窗口差值派生
	require.NotNil(t, item.Average.StorageSize)
	require.InDelta(t, 103, *item.Average.StorageSize, 1e-9, "均值口径覆盖近 7 天窗口")
	require.NotNil(t, item.Latest.StorageSize)
	require.InDelta(t, 106, *item.Latest.StorageSize, 1e-9, "最新值为窗口末日指标表值")
	require.Equal(t, osstest.Today(), item.Latest.Date)
}

// Outcome no-data-vs-failure-empty-state: 账号下尚无任何指标行 — 聚合空态
// 返回空而非 0 冒充;「无数据」与「采集失败」由采集侧信号区分(失败明细
// vs 空汇总)。
func TestStep4_NoDataVsFailureEmptyState(t *testing.T) {
	h, provider := newJourneyHarness()
	h.SeedAccount(1, provider)
	h.SeedBucket(1, provider.Name, "not-yet-collected-bucket") // 有资产但无任何指标行

	// 聚合空态:空 items,非错误、不显示 0 冒充数据
	top, err := h.NewQueryService().GetTop(context.Background(), 1, 1, 7, service.OSSSortStorageSize, 10, 1, 10)
	require.NoError(t, err)
	require.Empty(t, top.Items)

	// 对照 A(无数据):无失败明细 + accounts_without_oss 如实反映
	taskClean, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Empty(t, nastest.ResultFailures(t, taskClean), "无数据场景不虚报失败")

	// 对照 B(采集失败):failures + failed_buckets 可辨 → 前端渲染警示而非纯空
	provider.Querier.Fail(errInjected)
	taskFailed, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.NotEmpty(t, nastest.ResultFailures(t, taskFailed), "采集失败必须可辨")
	require.Equal(t, 1, nastest.ResultField(t, taskFailed, "failed_buckets").(int))
}
