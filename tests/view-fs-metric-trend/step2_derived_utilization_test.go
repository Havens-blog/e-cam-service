// @feature nas-ops-insight @api-functional
//
// Contract step-2-derived-utilization: 解读派生使用率与数据状态 — utilization
// is derived at read time in [0,1] ( 0 when used=0, clamped via
// min(used,capacity) when used>capacity with raw used still returned ), gap
// days are annotated data_status=missing with nulls, and zero_exception rows
// map to data_status=zero_exception with null utilization.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package view_fs_metric_trend

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// seedDerivedWorld 账号 1 名下:fs-ok 正常非零(used=0),fs-gap 缺失日,
// fs-zero zero_exception 行,fs-over used>capacity 行。
func seedDerivedWorld(t *testing.T) (*nastest.Harness, *nastest.Provider) {
	t.Helper()
	h, provider := newJourneyHarness()
	acc := h.SeedAccount(1, provider)
	h.SeedMetric(t, acc.ID, provider.Name,
		nastest.Metric("fs-ok", "空载盘", nastest.Today(), 100, 0))
	h.SeedMetric(t, acc.ID, provider.Name,
		nastest.Metric("fs-gap", "缺口盘", nastest.DayOffset(-5), 100, 50))
	h.SeedMetric(t, acc.ID, provider.Name,
		nastest.Metric("fs-zero", "零容量盘", nastest.Today(), 0, 0))
	h.SeedMetric(t, acc.ID, provider.Name,
		nastest.Metric("fs-over", "超用盘", nastest.Today(), 100, 150))
	return h, provider
}

// Outcome success: utilization 由 capacity/used 读取时派生(非独立落库字段),
// 取值 0~1;used=0 且 capacity>0 时 utilization=0;data_status=ok 且
// qc_status 原样透出。
func TestStep2_DerivedUtilization_NormalRows(t *testing.T) {
	h, _ := seedDerivedWorld(t)
	ctx := context.Background()
	query := h.NewQueryService()

	trend, err := query.GetFsMetrics(ctx, h.TenantID, 1, "fs-ok", 7)
	require.NoError(t, err)
	today := trend.Days[len(trend.Days)-1]
	require.Equal(t, service.NASDataStatusOK, today.DataStatus)
	require.Equal(t, "", today.QcStatus, "qc_status 原样透出(空=正常)")
	require.NotNil(t, today.Utilization)
	require.InDelta(t, 0.0, *today.Utilization, 1e-9, "used=0 且 capacity>0 → utilization=0")
	require.GreaterOrEqual(t, *today.Utilization, 0.0)
	require.LessOrEqual(t, *today.Utilization, 1.0)
}

// Outcome success(超用收敛): used>capacity 时按 min(used, capacity) 收敛
// 参与计算且原始 used 仍返回,响应不出现 NaN。
func TestStep2_DerivedUtilization_OverUsedClamped(t *testing.T) {
	h, _ := seedDerivedWorld(t)
	query := h.NewQueryService()

	trend, err := query.GetFsMetrics(context.Background(), h.TenantID, 1, "fs-over", 7)
	require.NoError(t, err)
	today := trend.Days[len(trend.Days)-1]
	require.NotNil(t, today.Used)
	require.Equal(t, float64(150), *today.Used, "原始 used 仍返回")
	require.NotNil(t, today.Utilization)
	require.InDelta(t, 1.0, *today.Utilization, 1e-9, "utilization 按 min(used,capacity) 收敛到 1")
	require.False(t, today.Utilization != nil && (*today.Utilization != *today.Utilization), "不出现 NaN")
}

// Outcome missing-day-annotated: 窗口内无行日期以 data_status=missing 标注,
// capacity/used/utilization 均为 null,不填充 0 或假值;有数据日返回真实值。
func TestStep2_DerivedUtilization_MissingDayAnnotated(t *testing.T) {
	h, _ := seedDerivedWorld(t)
	query := h.NewQueryService()

	trend, err := query.GetFsMetrics(context.Background(), h.TenantID, 1, "fs-gap", 7)
	require.NoError(t, err)

	foundDataDay := false
	for _, day := range trend.Days {
		if day.Date == nastest.DayOffset(-5) {
			require.Equal(t, service.NASDataStatusOK, day.DataStatus)
			require.NotNil(t, day.Capacity)
			require.InDelta(t, 0.5, *day.Utilization, 1e-9, "有数据日返回真实值")
			foundDataDay = true
			continue
		}
		require.Equal(t, service.NASDataStatusMissing, day.DataStatus, "缺失日标注 missing")
		require.Nil(t, day.Capacity)
		require.Nil(t, day.Used)
		require.Nil(t, day.Utilization, "缺失日绝不以 0/假值填充")
	}
	require.True(t, foundDataDay)
}

// Outcome zero-exception-null-utilization: qc_status=zero_exception 行原样
// 暴露并映射 data_status=zero_exception,utilization=null。
func TestStep2_DerivedUtilization_ZeroExceptionNullUtilization(t *testing.T) {
	h, _ := seedDerivedWorld(t)
	query := h.NewQueryService()

	trend, err := query.GetFsMetrics(context.Background(), h.TenantID, 1, "fs-zero", 7)
	require.NoError(t, err)
	today := trend.Days[len(trend.Days)-1]
	require.Equal(t, service.NASDataStatusZeroException, today.DataStatus)
	require.Equal(t, "zero_exception", today.QcStatus, "qc_status 原样暴露")
	require.Nil(t, today.Utilization, "零容量异常行 utilization 为 null")
	require.NotNil(t, today.Capacity)
	require.Equal(t, float64(0), *today.Capacity)
}
