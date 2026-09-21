package dao

import (
	"context"
	"strings"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
)

// 写入路径数据质量门禁(T2 AC-4,规格「使用率口径归一」+「单位归一化」):
//   - usage_percent 限 0~100:越界(负值/>100)拒绝——厂商口径异常/未归一形态
//     (如阿里 Burst 系列实盘 -1 哨兵值,probe-report §1.1)在门禁处拦下;
//   - usage_percent=0 例外放行并强制打 qc_status=zero_exception(不拦截不跳过,
//     异常行落库可见,读取侧结合 usage_scope 甄别);
//   - iops/throughput 非负校验,负值拒绝。
func TestDiskMetricQC(t *testing.T) {
	cases := []struct {
		name      string
		in        types.DiskMetric
		wantErr   bool
		errSubstr string
		wantQc    string
		wantUsage float64
	}{
		{
			name:      "使用率零值例外放行并打标(口径缺失/合法闲盘统一打标)",
			in:        types.DiskMetric{DiskID: "d-zero", Date: "2026-09-19", UsagePercent: 0, IOPS: 5, Throughput: 1.2},
			wantQc:    types.DiskMetricQcZeroException,
			wantUsage: 0,
		},
		{
			name:      "零值覆盖调用方误打的正常标注",
			in:        types.DiskMetric{DiskID: "d-zero2", Date: "2026-09-19", UsagePercent: 0, QcStatus: "mistake_ok"},
			wantQc:    types.DiskMetricQcZeroException,
			wantUsage: 0,
		},
		{
			name:      "正常值放行(探测实测实例级使用率样本)",
			in:        types.DiskMetric{DiskID: "d-ok", Date: "2026-09-19", UsagePercent: 60.693, UsageScope: types.DiskUsageScopeInstanceLevel, IOPS: 8.256, Throughput: 0.117, QcStatus: "ok_pre"},
			wantQc:    "ok_pre",
			wantUsage: 60.693,
		},
		{
			name:      "下界 0 恰好放行",
			in:        types.DiskMetric{DiskID: "d-min", Date: "2026-09-19", UsagePercent: 0},
			wantQc:    types.DiskMetricQcZeroException,
			wantUsage: 0,
		},
		{
			name:      "上界 100 恰好放行",
			in:        types.DiskMetric{DiskID: "d-max", Date: "2026-09-19", UsagePercent: 100},
			wantQc:    "",
			wantUsage: 100,
		},
		{
			name:      "AWS busy_share 全闲盘派生 0.00 合法放行(probe-report §1.3 实盘样本)",
			in:        types.DiskMetric{DiskID: "d-idle", Date: "2026-09-19", UsagePercent: 0.001, UsageScope: types.DiskUsageScopeBusyShare},
			wantQc:    "",
			wantUsage: 0.001,
		},
		{
			name:      "负使用率拒绝(aliyun Burst 系列 -1 哨兵直写形态)",
			in:        types.DiskMetric{DiskID: "d-neg", Date: "2026-09-19", UsagePercent: -1},
			wantErr:   true,
			errSubstr: "d-neg",
		},
		{
			name:      "高于 100 拒绝(厂商原始口径未归一形态)",
			in:        types.DiskMetric{DiskID: "d-high", Date: "2026-09-19", UsagePercent: 100.5},
			wantErr:   true,
			errSubstr: "d-high",
		},
		{
			name:      "负 IOPS 拒绝",
			in:        types.DiskMetric{DiskID: "d-iops", Date: "2026-09-19", UsagePercent: 50, IOPS: -0.1},
			wantErr:   true,
			errSubstr: "d-iops",
		},
		{
			name:      "负吞吐拒绝",
			in:        types.DiskMetric{DiskID: "d-bps", Date: "2026-09-19", UsagePercent: 50, Throughput: -1},
			wantErr:   true,
			errSubstr: "d-bps",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := diskMetricQC(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil (metric=%+v)", tc.in)
				}
				if !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("error 应携带 disk_id 便于执行器失败归因: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("want pass, got error: %v", err)
			}
			if got.QcStatus != tc.wantQc {
				t.Fatalf("qc_status = %q, want %q", got.QcStatus, tc.wantQc)
			}
			if got.UsagePercent != tc.wantUsage {
				t.Fatalf("usage_percent = %v, want %v", got.UsagePercent, tc.wantUsage)
			}
		})
	}
}

// 门禁错误须携带 date 供回补窗口失败归因(与 disk_id 同一条错误消息)。
func TestDiskMetricQcErrorCarriesDate(t *testing.T) {
	_, err := diskMetricQC(types.DiskMetric{DiskID: "d-x", Date: "2026-09-18", UsagePercent: 200})
	if err == nil || !strings.Contains(err.Error(), "2026-09-18") {
		t.Fatalf("gate error 应携带 date: %v", err)
	}
}

// 未过门禁的行必须在触碰数据库前拒绝:db 句柄为 nil 时若门禁未先行拦截,
// Collection 调用会 panic——返回 error 即为「写入前校验」的证明。
func TestDiskMetricUpsertGateBeforeWrite(t *testing.T) {
	d := &diskMetricDAO{}
	err := d.UpsertMetric(context.Background(), types.DiskMetric{
		DiskID: "d-high", Date: "2026-09-19", UsagePercent: 101,
	})
	if err == nil {
		t.Fatal("want usage_percent gate error before any db touch, got nil")
	}
}

func TestDiskMetricBulkUpsertGateBeforeWrite(t *testing.T) {
	d := &diskMetricDAO{}
	batch := []types.DiskMetric{
		{DiskID: "d-ok", Date: "2026-09-19", UsagePercent: 50, AccountID: 1, Provider: "aliyun"},
		{DiskID: "d-bad", Date: "2026-09-19", UsagePercent: -1}, // 单行越界 → 整批拒绝
	}
	if err := d.BulkUpsertMetrics(context.Background(), batch); err == nil {
		t.Fatal("batch with out-of-range row must be rejected before db write, got nil")
	}
}

func TestDiskMetricBulkInsertIfAbsentGateBeforeWrite(t *testing.T) {
	d := &diskMetricDAO{}
	batch := []types.DiskMetric{
		{DiskID: "d-ok", Date: "2026-09-19", UsagePercent: 50, AccountID: 1, Provider: "aliyun"},
		{DiskID: "d-bad", Date: "2026-09-19", UsagePercent: 200}, // 单行越界 → 整批拒绝
	}
	if err := d.BulkInsertIfAbsent(context.Background(), batch); err == nil {
		t.Fatal("insert-if-absent batch with out-of-range row must be rejected before db write, got nil")
	}
}

// 空批直接返回 nil,不触碰数据库(连 db 句柄都未注入也不得 panic)
func TestDiskMetricBulkUpsertMetrics_EmptyBatch(t *testing.T) {
	d := &diskMetricDAO{}
	if err := d.BulkUpsertMetrics(context.Background(), nil); err != nil {
		t.Fatalf("nil batch: %v", err)
	}
	if err := d.BulkUpsertMetrics(context.Background(), []types.DiskMetric{}); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
}

func TestDiskMetricBulkInsertIfAbsent_EmptyBatch(t *testing.T) {
	d := &diskMetricDAO{}
	if err := d.BulkInsertIfAbsent(context.Background(), nil); err != nil {
		t.Fatalf("nil batch: %v", err)
	}
	if err := d.BulkInsertIfAbsent(context.Background(), []types.DiskMetric{}); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
}
