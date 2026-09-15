package dao

import (
	"context"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TestCDNServiceTypeNameRegex 验证 CDN service_type_name 过滤正则:
// 匹配 cdn / p_cdn / dcdn,排除 cdnbilling / other 等非 CDN 值。
// 背景: CDN 账单中约 64% 金额 service_type=other,只能靠 service_type_name 圈定,
// 且 ^cdn 裸前缀会误伤 cdnbilling,故正则必须带词边界。
func TestCDNServiceTypeNameRegex(t *testing.T) {
	re, err := regexp.Compile(CDNServiceTypeNameRegex)
	if err != nil {
		t.Fatalf("CDNServiceTypeNameRegex %q 编译失败: %v", CDNServiceTypeNameRegex, err)
	}
	matching := []string{"cdn", "p_cdn", "dcdn"}
	for _, v := range matching {
		if !re.MatchString(v) {
			t.Errorf("正则 %q 应匹配 %q", CDNServiceTypeNameRegex, v)
		}
	}
	excluded := []string{"cdnbilling", "other", "dccdn"}
	for _, v := range excluded {
		if re.MatchString(v) {
			t.Errorf("正则 %q 不应匹配 %q", CDNServiceTypeNameRegex, v)
		}
	}
}

// TestAggregateCDNLive CDN 聚合管道活体验证(只读,不写任何集合):MONGO_DSN 门控
func TestAggregateCDNLive(t *testing.T) {
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
	defer func() { _ = client.Disconnect(context.Background()) }()
	d := NewCDNBillDAO(mongox.NewMongo(client, "ecam"))

	// 租户恒定过滤:0 不作通配,用不存在的租户号验证管道可执行且返回空集
	const ghostTenant = int64(999999999)
	const start, end = "2020-01-01", "2030-12-31"

	accts, err := d.AggregateByServiceTypeName(ctx, ghostTenant, "account_id", start, end)
	if err != nil {
		t.Fatalf("AggregateByServiceTypeName: %v", err)
	}
	if len(accts) != 0 {
		t.Errorf("ghost tenant 应返回空集, got %d rows", len(accts))
	}

	monthly, err := d.AggregateCDNMonthly(ctx, ghostTenant, start, end)
	if err != nil {
		t.Fatalf("AggregateCDNMonthly: %v", err)
	}
	if len(monthly) != 0 {
		t.Errorf("ghost tenant 应返回空集, got %d rows", len(monthly))
	}
}
