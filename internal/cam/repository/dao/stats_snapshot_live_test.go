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

// 快照全链路活体验证(写测试快照后清理):MONGO_DSN 门控
func TestStatsSnapshotLive(t *testing.T) {
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
		// 清理测试快照(按测试域过滤,勿动真实快照)
		_, _ = client.Database("ecam").Collection(StatsSnapshotCollection).DeleteMany(
			context.Background(), map[string]any{"domain": "test:trend", "tenant_id": int64(999)})
	}()
	d := NewStatsSnapshotDAO(mongox.NewMongo(client, "ecam"))

	// 1. 写一条 8 天前的基线
	d8 := SnapshotDate(time.Now().AddDate(0, 0, -8))
	if err := d.Upsert(ctx, StatsSnapshot{
		Domain: "test:trend", TenantID: 999, Date: d8,
		Metrics: map[string]any{"total": float64(100), "coverage_percent": 40.0},
	}); err != nil {
		t.Fatalf("upsert baseline: %v", err)
	}
	// 2. 当日快照(幂等写两次应覆盖同一条)
	today := SnapshotDate(time.Now())
	for i := 0; i < 2; i++ {
		if err := d.Upsert(ctx, StatsSnapshot{
			Domain: "test:trend", TenantID: 999, Date: today,
			Metrics: map[string]any{"total": float64(120), "coverage_percent": 45.5},
		}); err != nil {
			t.Fatalf("upsert today: %v", err)
		}
	}
	// 3. 7 天前(含)最近基线应命中 8 天前那条
	base, err := d.GetNearestOnOrBefore(ctx, "test:trend", 999, SnapshotDate(time.Now().AddDate(0, 0, -7)))
	if err != nil {
		t.Fatalf("get baseline: %v", err)
	}
	if base == nil {
		t.Fatal("expected baseline, got nil")
	}
	if base.Date != d8 {
		t.Fatalf("baseline date = %s, want %s", base.Date, d8)
	}
	// 4. 差值计算:total +20,coverage +5.5pp
	dl := base.Delta(map[string]float64{"total": 120, "coverage_percent": 45.5})
	if dl["total"] != 20 {
		t.Fatalf("total delta = %v, want 20", dl["total"])
	}
	if dl["coverage_percent"] != 5.5 {
		t.Fatalf("coverage delta = %v, want 5.5", dl["coverage_percent"])
	}
	// 5. 无基线场景:更早日期之前无快照
	none, err := d.GetNearestOnOrBefore(ctx, "test:trend", 999, "2020-01-01")
	if err != nil || none != nil {
		t.Fatalf("expected nil baseline, got %v, %v", none, err)
	}
	// 6. 同 (domain,tenant,date) 幂等:今日快照只有一条
	cnt, _ := client.Database("ecam").Collection(StatsSnapshotCollection).
		CountDocuments(ctx, map[string]any{"domain": "test:trend", "tenant_id": int64(999)})
	if cnt != 2 { // 基线 1 + 今日 1
		t.Fatalf("snapshot count = %d, want 2 (幂等覆盖失败)", cnt)
	}
}
