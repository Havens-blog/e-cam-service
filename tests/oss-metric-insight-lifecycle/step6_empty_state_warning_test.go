// @feature oss-ops-insight @api-functional
//
// Contract step-6-empty-state-warning: 验证采集异常时的警示空态 — the empty
// state distinguishes "no data yet" from "collect failing": the read path
// returns missing-day annotations without fabricated values, while the
// collect task Result carries the failure signal the console can surface as
// a warning instead of a plain empty state.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package oss_metric_insight_lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// Outcome no-data-empty-state: bucket 正常纳管但从未采集 — 空态返回(非报错),
// days 序列为逐日 missing 标注,latest/average 无可用值为 null;不构造空趋势
// 假图、不用 0 冒充数据。
func TestStep6_NoDataEmptyState(t *testing.T) {
	h, provider := newJourneyHarness()
	seedBucketAccount(t, h, provider, 1, "brand-new-bucket")

	resp, err := h.NewQueryService().GetBucketMetrics(context.Background(), 1, 1, "brand-new-bucket", 7)
	require.NoError(t, err, "无数据是空态而非错误")
	require.Len(t, resp.Days, 7)
	for _, point := range resp.Days {
		require.Equal(t, "missing", point.DataStatus)
		require.Nil(t, point.StorageSize, "缺失日不填假值")
		require.Nil(t, point.ObjectCount, "缺失日不填假值")
	}
	require.Nil(t, resp.Latest, "无可用行时 latest 为 null")
	require.Nil(t, resp.Average.StorageSize, "无可用行时均值为 null")

	// 对照:无失败状态时 Result.failures 为空(纯「无数据」语义)
	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Empty(t, nastest.ResultFailures(t, task))
}

// Outcome failure-warning-empty-state: 采集持续失败时指标表无数据 — 空态
// 保持「不构造假值」的同时,失败信号在采集侧可辨(Result.failures +
// failed_buckets),前端据此渲染警示而非纯空。
func TestStep6_FailureWarningEmptyState(t *testing.T) {
	h, provider := newJourneyHarness()
	seedBucketAccount(t, h, provider, 1, "failing-bucket")
	provider.Querier.Fail(errors.New("injected: vendor API unauthorized"))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	// 采集失败信号可辨:failures 明细 + failed_buckets 计数(警示来源)
	failures := nastest.ResultFailures(t, task)
	require.Len(t, failures, 1)
	require.Equal(t, 1, nastest.ResultField(t, task, "failed_buckets").(int))

	// 指标表无数据:趋势空态不显示 0 冒充数据
	resp, err := h.NewQueryService().GetBucketMetrics(context.Background(), 1, 1, "failing-bucket", 7)
	require.NoError(t, err)
	for _, point := range resp.Days {
		require.Equal(t, "missing", point.DataStatus)
		require.Nil(t, point.StorageSize, "采集失败空态不得用 0 冒充数据")
	}
	require.Nil(t, resp.Latest)

	// 聚合侧同样不虚报:该 bucket 不进 Top(无数据 bucket 自然跳过)
	top, err := h.NewQueryService().GetTop(context.Background(), 1, 1, 7, service.OSSSortStorageSize, 10, 1, 10)
	require.NoError(t, err)
	require.Empty(t, top.Items)
}
