package dao

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-common-go/mongox"
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

// 回归:同一域名由多家 CDN 账号共同加速(多活 CDN),(account_id, domain, date)
// 才是唯一键——不同账号写同域名同日,必须各保留一行,不得互相覆盖。
// 旧实现唯一键为 (domain, date),后写的账号会顶掉先写账号的行,导致
// 成本归因(按流量占比)与流量统计丢失一方数据。
func TestCDNMetricPerAccountLive(t *testing.T) {
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
			"domain": map[string]any{"$regex": `^test-shared-`},
		})
	}()
	d := NewCDNMetricDAO(mongox.NewMongo(client, "ecam"))

	// 同一域名 test-shared-cdn.example.com,同一天,三个不同账号(阿里/腾讯/火山)各写一笔
	domain, date := "test-shared-cdn.example.com", "2026-09-14"
	seeds := []types.CDNMetric{
		{Domain: domain, Date: date, Bytes: 1000, Bandwidth: 10, HitRate: 0.9, AccountID: 1, Provider: "aliyun"},
		{Domain: domain, Date: date, Bytes: 2000, Bandwidth: 20, HitRate: 0.8, AccountID: 4, Provider: "tencent"},
		{Domain: domain, Date: date, Bytes: 3000, Bandwidth: 30, HitRate: 0.7, AccountID: 5, Provider: "volcengine"},
	}
	for _, m := range seeds {
		if err := d.UpsertMetric(ctx, m); err != nil {
			t.Fatalf("upsert acct %d: %v", m.AccountID, err)
		}
	}

	// 三行都必须保留,各账号值正确
	cnt, _ := coll.CountDocuments(ctx, map[string]any{"domain": domain, "date": date})
	if cnt != 3 {
		t.Fatalf("shared domain rows = %d, want 3 (跨账号互相覆盖 bug)", cnt)
	}
	for _, seed := range seeds {
		var got types.CDNMetric
		filter := map[string]any{"domain": domain, "date": date, "account_id": seed.AccountID}
		if err := coll.FindOne(ctx, filter).Decode(&got); err != nil {
			t.Fatalf("decode acct%d: %v", seed.AccountID, err)
		}
		if got.Bytes != seed.Bytes || got.Provider != seed.Provider {
			t.Fatalf("acct%d metric = %+v, want bytes %d (跨账号覆盖丢失)", seed.AccountID, got, seed.Bytes)
		}
	}

	// 账号内同日重采仍应幂等覆盖(同一账号同域名同日只留一条)
	update := seeds[1]
	update.Bytes = 2500
	if err := d.UpsertMetric(ctx, update); err != nil {
		t.Fatalf("re-upsert acct4: %v", err)
	}
	cnt, _ = coll.CountDocuments(ctx, map[string]any{"domain": domain, "date": date})
	if cnt != 3 {
		t.Fatalf("after re-upsert rows = %d, want 3", cnt)
	}
	var got types.CDNMetric
	if err := coll.FindOne(ctx, map[string]any{"domain": domain, "date": date, "account_id": 4}).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Bytes != 2500 {
		t.Fatalf("acct4 re-upsert bytes = %d, want 2500", got.Bytes)
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

	// TopByBytes 聚合(账号 889 视角,语料仅测试域 c,不受真实数据干扰):
	// c(9999) 应稳居该账号第一
	top, err := d.TopByBytes(ctx, 7, 10, 889)
	if err != nil {
		t.Fatalf("TopByBytes: %v", err)
	}
	if len(top) < 1 {
		t.Fatalf("TopByBytes(889) len = %d, want >= 1", len(top))
	}
	if top[0].Domain != "test-query-c.example.com" || top[0].Bytes != 9999 {
		t.Fatalf("TopByBytes(889)[0] = %+v, want c/9999", top[0])
	}
	// 账号 888 视角聚合:语料仅 a(100+200+400=700 / 3 天)与 b(800),无真实数据干扰。
	// 同时验证 a 的 3 天求和与 count,以及 b > a 的排序
	top, err = d.TopByBytes(ctx, 7, 10, 888)
	if err != nil {
		t.Fatalf("TopByBytes(888): %v", err)
	}
	if len(top) != 2 || top[0].Domain != "test-query-b.example.com" || top[0].Bytes != 800 {
		t.Fatalf("TopByBytes(888) = %+v, want [b/800 a/700]", top)
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

	// limit 生效
	top, err = d.TopByBytes(ctx, 7, 1, 888)
	if err != nil {
		t.Fatalf("TopByBytes(limit=1): %v", err)
	}
	if len(top) != 1 || top[0].Domain != "test-query-b.example.com" {
		t.Fatalf("TopByBytes(limit=1) = %+v, want only b", top)
	}
}
