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

// ListByDomain / TopByBytes 活体验证(MONGO_DSN 门控;测试数据带 test-query- 前缀域名,
// 结束后清理,勿动真实数据)
func TestCDNMetricQueryLive(t *testing.T) {
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
	coll := client.Database("ecam").Collection(CDNMetricCollection)
	defer func() {
		_, _ = coll.DeleteMany(context.Background(), map[string]any{
			"domain": map[string]any{"$regex": `^test-query-`},
		})
	}()
	d := NewCDNMetricDAO(mongox.NewMongo(client, "ecam"))

	// Arrange: 账号 888 两个域名 3 天数据 + 账号 889 一个域名(隔离验证)
	// 今天按运营时区(UTC+8)计算,避免 UTC 凌晨跨日抖动
	today := time.Now().In(time.FixedZone("CST", 8*60*60))
	dateOf := func(offsetDays int) string {
		return today.AddDate(0, 0, -offsetDays).Format("2006-01-02")
	}
	rows := []types.CDNMetric{
		{Domain: "test-query-a.example.com", Date: dateOf(0), Bytes: 100, Bandwidth: 10, HitRate: 0.9, AccountID: 888, Provider: "aliyun"},
		{Domain: "test-query-a.example.com", Date: dateOf(1), Bytes: 200, Bandwidth: 20, HitRate: 0.8, AccountID: 888, Provider: "aliyun"},
		{Domain: "test-query-a.example.com", Date: dateOf(2), Bytes: 400, Bandwidth: 40, HitRate: 0.7, AccountID: 888, Provider: "aliyun"},
		{Domain: "test-query-b.example.com", Date: dateOf(0), Bytes: 800, Bandwidth: 80, HitRate: 0.6, AccountID: 888, Provider: "aliyun"},
		{Domain: "test-query-c.example.com", Date: dateOf(0), Bytes: 9999, Bandwidth: 99, HitRate: 0.5, AccountID: 889, Provider: "aliyun"},
	}
	for _, m := range rows {
		if err := d.UpsertMetric(ctx, m); err != nil {
			t.Fatalf("seed upsert: %v", err)
		}
	}

	// Act + Assert: ListByDomain 按 date 降序
	items, err := d.ListByDomain(ctx, "test-query-a.example.com", 7, 0)
	if err != nil {
		t.Fatalf("ListByDomain: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("ListByDomain len = %d, want 3", len(items))
	}
	if items[0].Date != dateOf(0) || items[2].Date != dateOf(2) {
		t.Fatalf("ListByDomain not date-desc: %s ~ %s", items[0].Date, items[2].Date)
	}

	// accountID 过滤:889 只能看到自己账号的指标
	items, err = d.ListByDomain(ctx, "test-query-a.example.com", 7, 889)
	if err != nil {
		t.Fatalf("ListByDomain(889): %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("ListByDomain(889) len = %d, want 0 (账号隔离失败)", len(items))
	}

	// days 窗口:近 2 天只剩 2 条
	items, err = d.ListByDomain(ctx, "test-query-a.example.com", 2, 0)
	if err != nil {
		t.Fatalf("ListByDomain(days=2): %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("ListByDomain(days=2) len = %d, want 2", len(items))
	}

	// TopByBytes 聚合:b(800) > c(9999 按账号 889 才见)> a(700);全量下 c 第一
	top, err := d.TopByBytes(ctx, 7, 10, 0)
	if err != nil {
		t.Fatalf("TopByBytes: %v", err)
	}
	if len(top) < 3 {
		t.Fatalf("TopByBytes len = %d, want >= 3", len(top))
	}
	if top[0].Domain != "test-query-c.example.com" || top[0].Bytes != 9999 {
		t.Fatalf("TopByBytes[0] = %+v, want c/9999", top[0])
	}
	var aRow *types.CDNMetricTopRow
	for i := range top {
		if top[i].Domain == "test-query-a.example.com" {
			aRow = &top[i]
		}
	}
	if aRow == nil || aRow.Bytes != 700 || aRow.Count != 3 {
		t.Fatalf("top row for a = %+v, want bytes 700 count 3", aRow)
	}
	if aRow.Days != 7 {
		t.Fatalf("top row days = %d, want 7", aRow.Days)
	}

	// TopByBytes 账号隔离:888 看不到 c
	top, err = d.TopByBytes(ctx, 7, 10, 888)
	if err != nil {
		t.Fatalf("TopByBytes(888): %v", err)
	}
	if len(top) != 2 || top[0].Domain != "test-query-b.example.com" || top[0].Bytes != 800 {
		t.Fatalf("TopByBytes(888) = %+v, want [b/800 a/700]", top)
	}

	// limit 生效
	top, err = d.TopByBytes(ctx, 7, 1, 888)
	if err != nil {
		t.Fatalf("TopByBytes(limit=1): %v", err)
	}
	if len(top) != 1 || top[0].Domain != "test-query-b.example.com" {
		t.Fatalf("TopByBytes(limit=1) = %+v, want only b", top)
	}
}
