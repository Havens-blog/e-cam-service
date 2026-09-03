package dao

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 一次性集成验证(只读聚合):DASHBOARD_LIVE=1 时启用
func TestGetExpiringLive(t *testing.T) {
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
	defer client.Disconnect(context.Background())
	d := NewDashboardDAO(mongox.NewMongo(client, "ecam"))

	start := time.Now()
	items, total, err := d.GetExpiringInstances(ctx, 3, 30, 0, 20)
	if err != nil {
		t.Fatalf("GetExpiringInstances: %v", err)
	}
	t.Logf("elapsed: %dms  total: %d  items: %d", time.Since(start).Milliseconds(), total, len(items))
	if total == 0 {
		t.Fatal("total 为 0,预期 ~3923")
	}
	if len(items) == 0 {
		t.Fatal("items 为空")
	}
	if items[0].AssetName == "" {
		t.Fatal("首页数据字段为空,投影异常")
	}
}
