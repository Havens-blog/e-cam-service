package dao

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-common-go/mongox"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 写入路径数量级自检(T2 AC-4,规格「单位归一化与字段语义」):
//   - capacity=0 例外放行并强制打 qc_status=zero_exception(华为/AWS 实盘现状,
//     不拦截不跳过,异常行落库可见);
//   - 非零行 capacity 须落在 [1MB, 1PB](GB 二进制 GiB 计),越界拒绝——
//     字节直写 GB 字段的单位 bug(sync_nas.go 数据质量 bug 根源)在门禁处拦下。
func TestNASMetricQC(t *testing.T) {
	minGB := 1.0 / 1024    // 1MiB(规格记 1MB,二进制口径)
	maxGB := 1024.0 * 1024 // 1PiB(规格记 1PB)
	cases := []struct {
		name    string
		in      types.NASMetric
		wantErr bool
		wantQc  string
		wantCap float64
	}{
		{
			name:    "零容量例外放行并打标(华为/AWS 实盘现状)",
			in:      types.NASMetric{FsID: "fs-zero", Date: "2026-09-19", Capacity: 0, UsedCapacity: 0},
			wantQc:  types.NASMetricQcZeroException,
			wantCap: 0,
		},
		{
			name:    "零容量覆盖调用方误打的正常标注",
			in:      types.NASMetric{FsID: "fs-zero2", Date: "2026-09-19", Capacity: 0, QcStatus: "mistake_ok"},
			wantQc:  types.NASMetricQcZeroException,
			wantCap: 0,
		},
		{
			name:    "正常值放行(探测实测 jlc-fat 口径)",
			in:      types.NASMetric{FsID: "fs-ok", Date: "2026-09-19", Capacity: 2457.32, UsedCapacity: 1378.14},
			wantQc:  "",
			wantCap: 2457.32,
		},
		{
			name:    "下界 1MB 恰好放行",
			in:      types.NASMetric{FsID: "fs-min", Date: "2026-09-19", Capacity: minGB},
			wantQc:  "",
			wantCap: minGB,
		},
		{
			name:    "上界 1PB 恰好放行",
			in:      types.NASMetric{FsID: "fs-max", Date: "2026-09-19", Capacity: maxGB},
			wantQc:  "",
			wantCap: maxGB,
		},
		{
			name:    "低于下界的非零值拒绝(单位换算缺失形态)",
			in:      types.NASMetric{FsID: "fs-low", Date: "2026-09-19", Capacity: minGB / 2},
			wantErr: true,
		},
		{
			name:    "高于上界拒绝(aliyun 10PiB 名义容量直写形态)",
			in:      types.NASMetric{FsID: "fs-high", Date: "2026-09-19", Capacity: maxGB * 10},
			wantErr: true,
		},
		{
			name:    "负容量拒绝",
			in:      types.NASMetric{FsID: "fs-neg", Date: "2026-09-19", Capacity: -1},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := nasMetricQC(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil (metric=%+v)", tc.in)
				}
				if !strings.Contains(err.Error(), tc.in.FsID) {
					t.Fatalf("error 应携带 fs_id 便于执行器失败归因: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("want pass, got error: %v", err)
			}
			if got.QcStatus != tc.wantQc {
				t.Fatalf("qc_status = %q, want %q", got.QcStatus, tc.wantQc)
			}
			if got.Capacity != tc.wantCap {
				t.Fatalf("capacity = %v, want %v", got.Capacity, tc.wantCap)
			}
		})
	}
}

// 未过数量级自检的行必须在触碰数据库前拒绝:db 句柄为 nil 时若门禁未先行拦截,
// Collection 调用会 panic——返回 error 即为「写入前校验」的证明。
func TestNASMetricUpsertGateBeforeWrite(t *testing.T) {
	d := &nasMetricDAO{}
	err := d.UpsertMetric(context.Background(), types.NASMetric{
		FsID: "fs-high", Date: "2026-09-19", Capacity: 10485760, // 10PiB 名义容量直写形态
	})
	if err == nil {
		t.Fatal("want magnitude gate error before any db touch, got nil")
	}
}

func TestNASMetricBulkUpsertGateBeforeWrite(t *testing.T) {
	d := &nasMetricDAO{}
	batch := []types.NASMetric{
		{FsID: "fs-ok", Date: "2026-09-19", Capacity: 100, AccountID: 1, Provider: "aliyun"},
		{FsID: "fs-bad", Date: "2026-09-19", Capacity: 10485760}, // 单行越界 → 整批拒绝
	}
	if err := d.BulkUpsertMetrics(context.Background(), batch); err == nil {
		t.Fatal("batch with out-of-range row must be rejected before db write, got nil")
	}
}

// 空批直接返回 nil,不触碰数据库(连 db 句柄都未注入也不得 panic)
func TestNASMetricBulkUpsertMetrics_EmptyBatch(t *testing.T) {
	d := &nasMetricDAO{}
	if err := d.BulkUpsertMetrics(context.Background(), nil); err != nil {
		t.Fatalf("nil batch: %v", err)
	}
	if err := d.BulkUpsertMetrics(context.Background(), []types.NASMetric{}); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
}

// 首写生效写入(T5 AC:今日行首写生效)同样必须先过数量级自检:
// 越界行在触碰数据库前整批拒绝(零容量行照常放行)。
func TestNASMetricBulkInsertIfAbsentGateBeforeWrite(t *testing.T) {
	d := &nasMetricDAO{}
	batch := []types.NASMetric{
		{FsID: "fs-ok", Date: "2026-09-19", Capacity: 100, AccountID: 1, Provider: "aliyun"},
		{FsID: "fs-bad", Date: "2026-09-19", Capacity: 10485760}, // 单行越界 → 整批拒绝
	}
	if err := d.BulkInsertIfAbsent(context.Background(), batch); err == nil {
		t.Fatal("insert-if-absent batch with out-of-range row must be rejected before db write, got nil")
	}
}

func TestNASMetricBulkInsertIfAbsent_EmptyBatch(t *testing.T) {
	d := &nasMetricDAO{}
	if err := d.BulkInsertIfAbsent(context.Background(), nil); err != nil {
		t.Fatalf("nil batch: %v", err)
	}
	if err := d.BulkInsertIfAbsent(context.Background(), []types.NASMetric{}); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
}

// 错误注入:超时/不可达的 context 必须把错误原样回传,不得吞错
func TestNASMetricUpsertMetrics_ErrorPassthrough(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	client, err := mongo.Connect(context.Background(),
		options.Client().ApplyURI("mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=100&connectTimeoutMS=100"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()
	d := &nasMetricDAO{db: mongox.NewMongo(client, nasMetricTestDB)}

	row := types.NASMetric{FsID: "test-err-fs", Date: "2026-09-18", Capacity: 100, AccountID: 1, Provider: "aliyun"}
	if err := d.UpsertMetric(ctx, row); err == nil {
		t.Fatal("want error from canceled ctx / unreachable server, got nil")
	}
	if err := d.BulkUpsertMetrics(ctx, []types.NASMetric{row}); err == nil {
		t.Fatal("want bulk error from unreachable server, got nil")
	}
}
