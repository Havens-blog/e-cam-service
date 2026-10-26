package dao

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Havens-blog/e-common-go/mongox"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TestDistinctTenantIDsLive 预算/账单两个 Distinct 租户枚举方法的活体验证
// (只读,不写任何集合,不校验具体数据,仅断言管道可执行且返回类型正确):
// MONGO_DSN 门控,未设置则跳过——与 TestAggregateCDNLive 同一模式。
func TestDistinctTenantIDsLive(t *testing.T) {
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
	m := mongox.NewMongo(client, "ecam")

	budgetTenants, err := NewBudgetDAO(m).DistinctTenantIDs(ctx)
	if err != nil {
		t.Fatalf("budget DistinctTenantIDs: %v", err)
	}
	for _, id := range budgetTenants {
		if id <= 0 {
			t.Errorf("budget 集合枚举出非正 tenant_id: %d", id)
		}
	}

	billTenants, err := NewBillDAO(m).DistinctTenantIDs(ctx)
	if err != nil {
		t.Fatalf("bill DistinctTenantIDs: %v", err)
	}
	for _, id := range billTenants {
		if id <= 0 {
			t.Errorf("bill 集合枚举出非正 tenant_id: %d", id)
		}
	}
}
