package dao

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Havens-blog/e-common-go/mongox"
	"go.mongodb.org/mongo-driver/bson"
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

// TestGetExpiringAutoRenewFilterLive 验证"包年包月+已开自动续费"的资源被过滤:
// 哨兵租户下插入三条 48h 后到期的实例(auto_renew=true/false/缺失),仅后两条应返回。
// 用负哨兵租户隔离真实数据,defer 清理,不触碰业务租户。
func TestGetExpiringAutoRenewFilterLive(t *testing.T) {
	dsn := os.Getenv("MONGO_DSN")
	if dsn == "" {
		t.Skip("set MONGO_DSN to run live check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(dsn))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())

	const sentinel int64 = -999909
	col := client.Database("ecam").Collection(InstanceCollection)
	// 先清残留(上次中断跑失败的兜底),再插入夹具,最后 defer 再清一次
	if _, err := col.DeleteMany(ctx, bson.M{"tenant_id": sentinel}); err != nil {
		t.Fatalf("预清理失败: %v", err)
	}
	defer col.DeleteMany(context.Background(), bson.M{"tenant_id": sentinel})

	soon := time.Now().Add(48 * time.Hour).UTC()
	docs := []interface{}{
		Instance{ID: 990000000001, ModelUID: "ecs", AssetID: "live-ar-true", AssetName: "自动续费已开(应隐藏)",
			TenantID: sentinel, AccountID: 1,
			Attributes: map[string]interface{}{"expired_time": soon, "auto_renew": true}},
		Instance{ID: 990000000002, ModelUID: "ecs", AssetID: "live-ar-false", AssetName: "自动续费未开(应显示)",
			TenantID: sentinel, AccountID: 1,
			Attributes: map[string]interface{}{"expired_time": soon, "auto_renew": false}},
		Instance{ID: 990000000003, ModelUID: "disk", AssetID: "live-ar-absent", AssetName: "无自动续费字段(应显示)",
			TenantID: sentinel, AccountID: 1,
			Attributes: map[string]interface{}{"expired_time": soon}},
		// 字符串 "true" 形态同样应被过滤(归一化覆盖)
		Instance{ID: 990000000004, ModelUID: "ecs", AssetID: "live-ar-strtrue", AssetName: "字符串true(应隐藏)",
			TenantID: sentinel, AccountID: 1,
			Attributes: map[string]interface{}{"expired_time": soon, "auto_renew": "true"}},
	}
	if _, err := col.InsertMany(ctx, docs); err != nil {
		t.Fatalf("插入夹具失败: %v", err)
	}

	d := NewDashboardDAO(mongox.NewMongo(client, "ecam"))
	items, total, err := d.GetExpiringInstances(ctx, sentinel, 30, 0, 50)
	if err != nil {
		t.Fatalf("GetExpiringInstances: %v", err)
	}
	got := map[string]bool{}
	for _, it := range items {
		got[it.AssetID] = true
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("预期只返回 2 条(false/缺失),实际 total=%d items=%d got=%v", total, len(items), got)
	}
	if !got["live-ar-false"] || !got["live-ar-absent"] {
		t.Fatalf("未开自动续费的夹具缺失: got=%v", got)
	}
	if got["live-ar-true"] || got["live-ar-strtrue"] {
		t.Fatalf("已开自动续费的夹具未被过滤: got=%v", got)
	}
}
