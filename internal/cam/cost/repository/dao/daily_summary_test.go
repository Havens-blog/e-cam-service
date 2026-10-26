package dao

import (
	"context"
	"testing"

	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShouldRebuildSummary 汇总表重建判据（表驱动）。
//
// 判据是「明细表账期范围 vs 汇总表账期范围」是否一致：不一致即重建；
// 明细表为空时不动作（无源可依，也不清空既有汇总数据）。
func TestShouldRebuildSummary(t *testing.T) {
	tests := []struct {
		name           string
		srcMin, srcMax string
		sumMin, sumMax string
		want           bool
	}{
		{
			name:   "范围一致 - 跳过重建",
			srcMin: "2025-12-01", srcMax: "2026-10-01",
			sumMin: "2025-12-01", sumMax: "2026-10-01",
			want: false,
		},
		{
			name:   "汇总表为空 - 需要重建",
			srcMin: "2025-12-01", srcMax: "2026-10-01",
			sumMin: "", sumMax: "",
			want: true,
		},
		{
			name:   "汇总表账期滞后（线上实测：汇总停在 2026-03、明细已到 2026-10）- 需要重建",
			srcMin: "2025-12-01", srcMax: "2026-10-01",
			sumMin: "2025-12-01", sumMax: "2026-03-01",
			want: true,
		},
		{
			name:   "汇总表上界超出明细（范围外残留行）- 需要重建以清理",
			srcMin: "2025-12-01", srcMax: "2026-10-01",
			sumMin: "2025-12-01", sumMax: "2026-12-01",
			want: true,
		},
		{
			name:   "汇总表下界早于明细 - 需要重建",
			srcMin: "2026-01-01", srcMax: "2026-10-01",
			sumMin: "2025-11-01", sumMax: "2026-10-01",
			want: true,
		},
		{
			name:   "明细表为空 - 无源可依，不动作",
			srcMin: "", srcMax: "",
			sumMin: "", sumMax: "",
			want: false,
		},
		{
			name:   "明细表为空但汇总表有陈旧数据 - 仍不动作（不清空既有数据）",
			srcMin: "", srcMax: "",
			sumMin: "2025-12-01", sumMax: "2026-03-01",
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, shouldRebuildSummary(tc.srcMin, tc.srcMax, tc.sumMin, tc.sumMax))
		})
	}
}

// TestReconcileTenant_RejectsZeroTenant tenant_id=0 在 DAO 里语义是「不过滤租户」，
// 与「只刷新该租户」相反。必须在触库前拒绝，否则租户级刷新会退化成全库重建。
// db 故意留空：只要守卫生效，就不会有任何数据库访问。
func TestReconcileTenant_RejectsZeroTenant(t *testing.T) {
	d := &DailySummaryDAO{logger: elog.DefaultLogger}

	rebuilt, err := d.ReconcileTenant(context.Background(), 0)

	require.Error(t, err, "tenantID=0 必须报错，不能退化为全库重建")
	assert.False(t, rebuilt)
	assert.Contains(t, err.Error(), "tenantID")
}
