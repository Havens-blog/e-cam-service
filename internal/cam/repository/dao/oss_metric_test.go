package dao

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Havens-blog/e-cloudx-sdk"
	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-common-go/mongox"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 接口形状冻结(T2 AC-2):GetOSSMetrics 签名无 region(OSS 是全局服务,
// 与 CDNMetricQuerier 同型;proposal「Proposed Solution」第 1 条)。
// fake 适配器编译期断言:签名一旦漂移(加 region/改参数序)此处即编译失败。
type fakeOSSMetricQuerier struct{}

func (fakeOSSMetricQuerier) GetOSSMetrics(ctx context.Context, bucketName, startDate, endDate string) ([]types.OSSMetric, error) {
	return nil, nil
}

var _ cloudx.OSSMetricQuerier = fakeOSSMetricQuerier{}

// 写入路径数量级自检(T2 AC-4,规格「单位归一化」):storage_size=0 例外放行并强制
// 打 qc_status=zero_exception(不拦截不跳过);非零行须落在 [1MB, 1PB](GB 二进制
// GiB 计),越界拒绝——字节直写 GB 字段的单位 bug 在门禁处拦下。OSS 只有一个容量
// 维度(storage_size),无 NAS 的 used_capacity。
func TestOSSMetricQC(t *testing.T) {
	minGB := 1.0 / 1024    // 1MiB(规格记 1MB,二进制口径)
	maxGB := 1024.0 * 1024 // 1PiB(规格记 1PB)
	cases := []struct {
		name    string
		in      types.OSSMetric
		wantErr bool
		wantQc  string
		wantCap float64
	}{
		{
			name:    "零容量例外放行并打标(华为/AWS 实盘 capacity=0 同型)",
			in:      types.OSSMetric{BucketName: "b-zero", Date: "2026-09-19", StorageSize: 0},
			wantQc:  types.OSSMetricQcZeroException,
			wantCap: 0,
		},
		{
			name:    "零容量覆盖调用方误打的正常标注",
			in:      types.OSSMetric{BucketName: "b-zero2", Date: "2026-09-19", StorageSize: 0, QcStatus: "mistake_ok"},
			wantQc:  types.OSSMetricQcZeroException,
			wantCap: 0,
		},
		{
			name:    "正常值放行(适配器经 types.BytesToGB 换算后的 GB 口径)",
			in:      types.OSSMetric{BucketName: "b-ok", Date: "2026-09-19", StorageSize: types.BytesToGB(1479796900730), ObjectCount: 42},
			wantQc:  "",
			wantCap: types.BytesToGB(1479796900730),
		},
		{
			name:    "下界 1MB 恰好放行",
			in:      types.OSSMetric{BucketName: "b-min", Date: "2026-09-19", StorageSize: minGB},
			wantQc:  "",
			wantCap: minGB,
		},
		{
			name:    "上界 1PB 恰好放行",
			in:      types.OSSMetric{BucketName: "b-max", Date: "2026-09-19", StorageSize: maxGB},
			wantQc:  "",
			wantCap: maxGB,
		},
		{
			name:    "低于下界的非零值拒绝(单位换算缺失形态)",
			in:      types.OSSMetric{BucketName: "b-low", Date: "2026-09-19", StorageSize: minGB / 2},
			wantErr: true,
		},
		{
			name:    "高于上界拒绝(字节直写 GB 字段形态)",
			in:      types.OSSMetric{BucketName: "b-high", Date: "2026-09-19", StorageSize: maxGB * 10},
			wantErr: true,
		},
		{
			name:    "负容量拒绝",
			in:      types.OSSMetric{BucketName: "b-neg", Date: "2026-09-19", StorageSize: -1},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ossMetricQC(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil (metric=%+v)", tc.in)
				}
				if !strings.Contains(err.Error(), tc.in.BucketName) {
					t.Fatalf("error 应携带 bucket_name 便于执行器失败归因: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("want pass, got error: %v", err)
			}
			if got.QcStatus != tc.wantQc {
				t.Fatalf("qc_status = %q, want %q", got.QcStatus, tc.wantQc)
			}
			if got.StorageSize != tc.wantCap {
				t.Fatalf("storage_size = %v, want %v", got.StorageSize, tc.wantCap)
			}
		})
	}
}

// 未过数量级自检的行必须在触碰数据库前拒绝:db 句柄为 nil 时若门禁未先行拦截,
// Collection 调用会 panic——返回 error 即为「写入前校验」的证明。
func TestOSSMetricUpsertGateBeforeWrite(t *testing.T) {
	d := &ossMetricDAO{}
	err := d.UpsertMetric(context.Background(), types.OSSMetric{
		BucketName: "b-high", Date: "2026-09-19", StorageSize: 10485760, // 10PiB 直写形态
	})
	if err == nil {
		t.Fatal("want magnitude gate error before any db touch, got nil")
	}
}

func TestOSSMetricBulkUpsertGateBeforeWrite(t *testing.T) {
	d := &ossMetricDAO{}
	batch := []types.OSSMetric{
		{BucketName: "b-ok", Date: "2026-09-19", StorageSize: 100, AccountID: 1, Provider: "aliyun"},
		{BucketName: "b-bad", Date: "2026-09-19", StorageSize: 10485760}, // 单行越界 → 整批拒绝
	}
	if err := d.BulkUpsertMetrics(context.Background(), batch); err == nil {
		t.Fatal("batch with out-of-range row must be rejected before db write, got nil")
	}
}

// 空批直接返回 nil,不触碰数据库(连 db 句柄都未注入也不得 panic)
func TestOSSMetricBulkUpsertMetrics_EmptyBatch(t *testing.T) {
	d := &ossMetricDAO{}
	if err := d.BulkUpsertMetrics(context.Background(), nil); err != nil {
		t.Fatalf("nil batch: %v", err)
	}
	if err := d.BulkUpsertMetrics(context.Background(), []types.OSSMetric{}); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
}

// 首写生效写入(AC-3 今日行不覆盖)同样必须先过数量级自检:
// 越界行在触碰数据库前整批拒绝(零容量行照常放行)。
func TestOSSMetricBulkInsertIfAbsentGateBeforeWrite(t *testing.T) {
	d := &ossMetricDAO{}
	batch := []types.OSSMetric{
		{BucketName: "b-ok", Date: "2026-09-19", StorageSize: 100, AccountID: 1, Provider: "aliyun"},
		{BucketName: "b-bad", Date: "2026-09-19", StorageSize: 10485760}, // 单行越界 → 整批拒绝
	}
	if err := d.BulkInsertIfAbsent(context.Background(), batch); err == nil {
		t.Fatal("insert-if-absent batch with out-of-range row must be rejected before db write, got nil")
	}
}

func TestOSSMetricBulkInsertIfAbsent_EmptyBatch(t *testing.T) {
	d := &ossMetricDAO{}
	if err := d.BulkInsertIfAbsent(context.Background(), nil); err != nil {
		t.Fatalf("nil batch: %v", err)
	}
	if err := d.BulkInsertIfAbsent(context.Background(), []types.OSSMetric{}); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
}

// 错误注入:超时/不可达的 context 必须把错误原样回传,不得吞错
func TestOSSMetricUpsertMetrics_ErrorPassthrough(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	client, err := mongo.Connect(context.Background(),
		options.Client().ApplyURI("mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=100&connectTimeoutMS=100"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()
	d := &ossMetricDAO{db: mongox.NewMongo(client, ossMetricTestDB)}

	row := types.OSSMetric{BucketName: "test-err-bucket", Date: "2026-09-18", StorageSize: 100, AccountID: 1, Provider: "aliyun"}
	if err := d.UpsertMetric(ctx, row); err == nil {
		t.Fatal("want error from canceled ctx / unreachable server, got nil")
	}
	if err := d.BulkUpsertMetrics(ctx, []types.OSSMetric{row}); err == nil {
		t.Fatal("want bulk error from unreachable server, got nil")
	}
}
