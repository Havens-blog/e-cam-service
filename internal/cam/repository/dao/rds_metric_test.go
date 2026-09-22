package dao

import (
	"context"
	"strings"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
)

// RDS 指标写入路径数据质量门禁(T2 AC-4,proposal「单位归一化」+ probe-report
// §2 归一定案「三使用率百分比 0~100」):
//   - cpu/memory/disk_percent 各限 0~100:越界(负值/>100)拒绝——厂商口径异常/
//     未归一形态在门禁处拦下,门禁保证落库每行可反向验证口径正确;
//   - 四指标全 0(CPU/内存/磁盘/连接):例外放行,强制打 qc_status=zero_exception
//     (不拦截不跳过,T5/T8 契约:全 0 是异常行,落库可见,读取侧甄别——停用/
//     重启中实例由 T8 结合实例状态打 data_status 而非 zero_exception);
//   - connections 为负:拒绝写入并报错(连接数无负值语义);
//   - 其余:原样放行(保留适配器已打的 qc_status 标注)。
func TestRDSMetricQC(t *testing.T) {
	cases := []struct {
		name      string
		in        types.RDSMetric
		wantErr   bool
		errSubstr string
		wantQc    string
	}{
		{
			name:   "正常行放行保留适配器标注(huawei rds001/rds002 实盘样本)",
			in:     types.RDSMetric{RdsID: "r-ok", Date: "2026-09-21", CPUPercent: 15.7, MemoryPercent: 82.06, DiskPercent: 33.5, Connections: 128, QcStatus: "ok_pre"},
			wantQc: "ok_pre",
		},
		{
			name:   "CPU 0 其余非零放行(低负载属正常业务事实,probe-report 零值 13 行)",
			in:     types.RDSMetric{RdsID: "r-idle-cpu", Date: "2026-09-21", CPUPercent: 0, MemoryPercent: 27.41, DiskPercent: 10, Connections: 3},
			wantQc: "",
		},
		{
			name:   "connections 0 其余非零放行(空闲库正常,不触发 zero_exception)",
			in:     types.RDSMetric{RdsID: "r-no-conn", Date: "2026-09-21", CPUPercent: 1.2, MemoryPercent: 30, DiskPercent: 20, Connections: 0},
			wantQc: "",
		},
		{
			name:   "四指标全 0 例外放行并强制打 zero_exception(停用实例/采集全缺形态)",
			in:     types.RDSMetric{RdsID: "r-zero", Date: "2026-09-21", CPUPercent: 0, MemoryPercent: 0, DiskPercent: 0, Connections: 0},
			wantQc: types.RDSMetricQcZeroException,
		},
		{
			name:   "四指标全 0 覆盖调用方误打的正常标注",
			in:     types.RDSMetric{RdsID: "r-zero2", Date: "2026-09-21", QcStatus: "mistake_ok"},
			wantQc: types.RDSMetricQcZeroException,
		},
		{
			name:      "CPU 越上界拒绝(厂商原始口径未归一形态)",
			in:        types.RDSMetric{RdsID: "r-cpu-high", Date: "2026-09-21", CPUPercent: 100.5, MemoryPercent: 50, DiskPercent: 50, Connections: 1},
			wantErr:   true,
			errSubstr: "r-cpu-high",
		},
		{
			name:      "内存负值拒绝",
			in:        types.RDSMetric{RdsID: "r-mem-neg", Date: "2026-09-21", CPUPercent: 50, MemoryPercent: -0.1, DiskPercent: 50, Connections: 1},
			wantErr:   true,
			errSubstr: "r-mem-neg",
		},
		{
			name:      "磁盘高于 100 拒绝",
			in:        types.RDSMetric{RdsID: "r-disk-high", Date: "2026-09-21", CPUPercent: 50, MemoryPercent: 50, DiskPercent: 101, Connections: 1},
			wantErr:   true,
			errSubstr: "r-disk-high",
		},
		{
			name:   "CPU 恰好 100 放行(上界闭区间)",
			in:     types.RDSMetric{RdsID: "r-cpu-max", Date: "2026-09-21", CPUPercent: 100, MemoryPercent: 90, DiskPercent: 80, Connections: 7},
			wantQc: "",
		},
		{
			name:      "负 connections 拒绝(连接数无负值语义)",
			in:        types.RDSMetric{RdsID: "r-conn-neg", Date: "2026-09-21", CPUPercent: 50, MemoryPercent: 50, DiskPercent: 50, Connections: -1},
			wantErr:   true,
			errSubstr: "r-conn-neg",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rdsMetricQC(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil (metric=%+v)", tc.in)
				}
				if !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("error 应携带 rds_id 便于执行器失败归因: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("want pass, got error: %v", err)
			}
			if got.QcStatus != tc.wantQc {
				t.Fatalf("qc_status = %q, want %q", got.QcStatus, tc.wantQc)
			}
		})
	}
}

// 门禁错误须携带 date 供回补窗口失败归因(与 rds_id 同一条错误消息)。
func TestRDSMetricQcErrorCarriesDate(t *testing.T) {
	_, err := rdsMetricQC(types.RDSMetric{RdsID: "r-x", Date: "2026-09-20", CPUPercent: 200})
	if err == nil || !strings.Contains(err.Error(), "2026-09-20") {
		t.Fatalf("gate error 应携带 date: %v", err)
	}
}

// 未过门禁的行必须在触碰数据库前拒绝:db 句柄为 nil 时若门禁未先行拦截,
// Collection 调用会 panic——返回 error 即为「写入前校验」的证明。
func TestRDSMetricUpsertGateBeforeWrite(t *testing.T) {
	d := &rdsMetricDAO{}
	err := d.UpsertMetric(context.Background(), types.RDSMetric{
		RdsID: "r-high", Date: "2026-09-21", CPUPercent: 101,
	})
	if err == nil {
		t.Fatal("want usage gate error before any db touch, got nil")
	}
}

func TestRDSMetricBulkUpsertGateBeforeWrite(t *testing.T) {
	d := &rdsMetricDAO{}
	batch := []types.RDSMetric{
		{RdsID: "r-ok", Date: "2026-09-21", CPUPercent: 50, MemoryPercent: 50, DiskPercent: 50, Connections: 10, AccountID: 1, Provider: "aliyun"},
		{RdsID: "r-bad", Date: "2026-09-21", CPUPercent: -1}, // 单行越界 → 整批拒绝
	}
	if err := d.BulkUpsertMetrics(context.Background(), batch); err == nil {
		t.Fatal("batch with out-of-range row must be rejected before db write, got nil")
	}
}

func TestRDSMetricBulkInsertIfAbsentGateBeforeWrite(t *testing.T) {
	d := &rdsMetricDAO{}
	batch := []types.RDSMetric{
		{RdsID: "r-ok", Date: "2026-09-21", CPUPercent: 50, MemoryPercent: 50, DiskPercent: 50, Connections: 10, AccountID: 1, Provider: "aliyun"},
		{RdsID: "r-bad", Date: "2026-09-21", MemoryPercent: 200}, // 单行越界 → 整批拒绝
	}
	if err := d.BulkInsertIfAbsent(context.Background(), batch); err == nil {
		t.Fatal("insert-if-absent batch with out-of-range row must be rejected before db write, got nil")
	}
}

// 空批直接返回 nil,不触碰数据库(连 db 句柄都未注入也不得 panic)
func TestRDSMetricBulkUpsertMetrics_EmptyBatch(t *testing.T) {
	d := &rdsMetricDAO{}
	if err := d.BulkUpsertMetrics(context.Background(), nil); err != nil {
		t.Fatalf("nil batch: %v", err)
	}
	if err := d.BulkUpsertMetrics(context.Background(), []types.RDSMetric{}); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
}

func TestRDSMetricBulkInsertIfAbsent_EmptyBatch(t *testing.T) {
	d := &rdsMetricDAO{}
	if err := d.BulkInsertIfAbsent(context.Background(), nil); err != nil {
		t.Fatalf("nil batch: %v", err)
	}
	if err := d.BulkInsertIfAbsent(context.Background(), []types.RDSMetric{}); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
}

// ListByAccounts 空账号集返回空切片不触库(与 Disk DAO 同口径)
func TestRDSMetricListByAccounts_EmptyAccounts(t *testing.T) {
	d := &rdsMetricDAO{}
	got, err := d.ListByAccounts(context.Background(), nil, 30)
	if err != nil {
		t.Fatalf("nil accounts: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty slice, got %d rows", len(got))
	}
}

// 读取窗口收敛(T2 AC-5,rds_metric_query.go):缺省 30、上限 90、原值透传
func TestNormalizeRDSReadDays(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 30}, {-5, 30}, {1, 1}, {45, 45}, {90, 90}, {91, 90}, {365, 90},
	}
	for _, tc := range cases {
		if got := normalizeRDSReadDays(tc.in); got != tc.want {
			t.Fatalf("normalizeRDSReadDays(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
