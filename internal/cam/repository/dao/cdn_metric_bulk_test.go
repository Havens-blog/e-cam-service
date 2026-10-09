package dao

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-common-go/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 空批直接返回 nil,不触碰数据库(连 db 句柄都未注入也不得 panic)
func TestBulkUpsertMetrics_EmptyBatch(t *testing.T) {
	d := &cdnMetricDAO{}
	if err := d.BulkUpsertMetrics(context.Background(), nil); err != nil {
		t.Fatalf("nil batch: %v", err)
	}
	if err := d.BulkUpsertMetrics(context.Background(), []types.CDNMetric{}); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
}

// 错误注入:超时/不可达的 context 必须把错误原样回传,不得吞错
func TestBulkUpsertMetrics_ErrorPassthrough(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	client, err := mongo.Connect(context.Background(),
		options.Client().ApplyURI("mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=100&connectTimeoutMS=100"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()
	d := &cdnMetricDAO{db: mongox.NewMongo(client, "ecam")}

	err = d.BulkUpsertMetrics(ctx, []types.CDNMetric{{
		Domain: "test-bulk-err.example.com", Date: "2026-09-18",
		Bytes: 1, AccountID: 1, Provider: "aliyun",
	}})
	if err == nil {
		t.Fatal("want error from canceled ctx / unreachable server, got nil")
	}
}

// BulkUpsertMetrics 活体验证(MONGO_DSN 门控,写测试数据后清理):
// 单批多条一次落库、同键二次批量写入幂等不重复(值覆盖)、跨账号同域名行互不覆盖
func TestCDNMetricBulkUpsertLive(t *testing.T) {
	dsn := os.Getenv("MONGO_DSN")
	if dsn == "" {
		t.Skip("set MONGO_DSN to run live check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(dsn))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		// 清理测试域名指标(勿动真实数据)
		_, _ = client.Database("ecam").Collection(CDNMetricCollection).DeleteMany(
			context.Background(), map[string]any{"domain": map[string]any{"$regex": `^test-bulk-`}})
	}()
	d := NewCDNMetricDAO(mongox.NewMongo(client, "ecam"))
	coll := client.Database("ecam").Collection(CDNMetricCollection)

	// Arrange: 同一账号 3 天 + 跨账号同日 1 条 = 4 行
	domain := "test-bulk-a.example.com"
	metrics := []types.CDNMetric{
		{Domain: domain, Date: "2026-09-14", Bytes: 100, Bandwidth: 10, HitRate: 0.9, AccountID: 901, Provider: "aliyun"},
		{Domain: domain, Date: "2026-09-15", Bytes: 200, Bandwidth: 20, HitRate: 0.8, AccountID: 901, Provider: "aliyun"},
		{Domain: domain, Date: "2026-09-16", Bytes: 300, Bandwidth: 30, HitRate: 0.7, AccountID: 901, Provider: "aliyun"},
		{Domain: domain, Date: "2026-09-16", Bytes: 400, Bandwidth: 40, HitRate: 0.6, AccountID: 902, Provider: "tencent"},
	}

	// Act: 单批一次 BulkWrite
	if err := d.BulkUpsertMetrics(ctx, metrics); err != nil {
		t.Fatalf("bulk upsert: %v", err)
	}

	// Assert: 4 行全部落库
	cnt, err := coll.CountDocuments(ctx, bson.M{"domain": domain})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 4 {
		t.Fatalf("rows = %d, want 4 (单批多条落库失败)", cnt)
	}

	// 幂等:同键二次批量写入不新增行,值覆盖为最新
	replay := append([]types.CDNMetric(nil), metrics...)
	replay[0].Bytes = 111
	replay[0].HitRate = 0.99
	if err := d.BulkUpsertMetrics(ctx, replay); err != nil {
		t.Fatalf("bulk replay: %v", err)
	}
	cnt, err = coll.CountDocuments(ctx, bson.M{"domain": domain})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 4 {
		t.Fatalf("rows after replay = %d, want 4 (幂等失败)", cnt)
	}
	var got types.CDNMetric
	if err := coll.FindOne(ctx, bson.M{"domain": domain, "date": "2026-09-14", "account_id": int64(901)}).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Bytes != 111 || got.HitRate != 0.99 {
		t.Fatalf("replayed metric = %+v, want bytes 111 hit_rate 0.99 (覆盖失败)", got)
	}
}
