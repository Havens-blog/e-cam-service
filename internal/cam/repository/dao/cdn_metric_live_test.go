package dao

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CDN 指标 upsert 活体验证(MONGO_DSN 门控,写测试数据后清理)
func TestCDNMetricLive(t *testing.T) {
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
			context.Background(), map[string]any{"domain": "test-live.example.com"})
	}()
	d := NewCDNMetricDAO(mongox.NewMongo(client, "ecam"))

	base := types.CDNMetric{
		Domain: "test-live.example.com", Date: "2026-09-14",
		Bytes: 1024, Bandwidth: 88, HitRate: 0.95,
		AccountID: 999, Provider: "aliyun",
	}
	// 1. 首写
	if err := d.UpsertMetric(ctx, base); err != nil {
		t.Fatalf("upsert first: %v", err)
	}
	// 2. 同 (domain,date) 重采覆盖
	updated := base
	updated.Bytes = 2048
	updated.HitRate = 0.97
	if err := d.UpsertMetric(ctx, updated); err != nil {
		t.Fatalf("upsert second: %v", err)
	}
	// 3. 只有唯一一条且值被覆盖
	coll := client.Database("ecam").Collection(CDNMetricCollection)
	cnt, _ := coll.CountDocuments(ctx, map[string]any{"domain": "test-live.example.com"})
	if cnt != 1 {
		t.Fatalf("metric count = %d, want 1 (幂等覆盖失败)", cnt)
	}
	var got types.CDNMetric
	if err := coll.FindOne(ctx, map[string]any{"domain": "test-live.example.com"}).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Bytes != 2048 || got.HitRate != 0.97 || got.Date != "2026-09-14" {
		t.Fatalf("unexpected metric: %+v", got)
	}
}
