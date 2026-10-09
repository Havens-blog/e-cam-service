// @feature oss-ops-insight @api-functional
//
// Contract step-4-zero-exception-exposure: zero_exception 行原样暴露 — the
// storage_size=0 row surfaces with data_status=zero_exception and qc_status
// verbatim ( an anomalous zero, not a normal empty bucket ), and capacity-0
// derivation answers null ( never divide-by-zero panic / NaN ) with no
// utilization field fabricated into the response.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package view_bucket_metric_trend

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// mustJSON 序列化断言输入(失败即终止用例)。
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}

// Outcome zero-exception-mapped-to-data-status: 窗口内存在 qc_status=
// zero_exception(storage_size=0 异常)的行 — 读取响应原样暴露 qc_status 并
// 映射 data_status=zero_exception,该日 storage_size 原样为 0(异常零与正常
// 空桶可区分)。
func TestStep4_ZeroExceptionMappedToDataStatus(t *testing.T) {
	h, provider := newJourneyHarness()
	seedTrendAccount(t, h, provider, 1)
	// 今日行为 storage_size=0 异常行(写入门禁强制打标,见 step3 lifecycle)
	seedSingleDay(t, h, provider, 1, 0, 0, 5)

	resp, err := h.NewQueryService().GetBucketMetrics(context.Background(), 1, 1, trendBucket, 3)
	require.NoError(t, err)
	today := resp.Days[len(resp.Days)-1]
	require.Equal(t, osstest.Today(), today.Date)
	require.NotNil(t, today.StorageSize)
	require.Equal(t, float64(0), *today.StorageSize, "异常零原样暴露为 0(不吞不补)")
	require.Equal(t, "zero_exception", today.DataStatus, "qc_status 应映射 data_status=zero_exception")
	require.Equal(t, types.OSSMetricQcZeroException, today.QcStatus, "qc_status 原样透出")
}

// Outcome zero-capacity-derivation-null: 容量为 0 时使用率类派生为 null 而非
// 除零 panic/NaN,接口正常返回;响应中不出现 utilization 字段(Hard Rule:
// utilization 不落库、读取侧不在 OSS 响应伪造)。
func TestStep4_ZeroCapacityDerivationNull(t *testing.T) {
	h, provider := newJourneyHarness()
	seedTrendAccount(t, h, provider, 1)
	seedSingleDay(t, h, provider, 1, 0, 0, 0) // 全零窗口

	resp, err := h.NewQueryService().GetBucketMetrics(context.Background(), 1, 1, trendBucket, 3)
	require.NoError(t, err, "容量为 0 不得引发除零 panic")

	// 全零窗口:均值跳过异常行后无可用值 → null(非 0、非 NaN)
	require.NotNil(t, resp.Average)
	require.Nil(t, resp.Average.StorageSize, "全零窗口均值应为 null 而非 0/NaN")
	require.Nil(t, resp.Average.ObjectCount)

	// 响应不含 NaN / utilization 伪造字段
	router := h.NewOSSRouter(1)
	status, env := osstest.GetJSON(t, router, trendPath(1, 3))
	require.Equal(t, 200, status)
	raw := strings.ToLower(string(mustJSON(t, env.Data)))
	require.NotContains(t, raw, "nan", "响应不得携带 NaN")
	require.NotContains(t, raw, "utilization", "OSS 响应不得伪造 utilization 字段")
}
