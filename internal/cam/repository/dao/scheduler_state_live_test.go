package dao

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-common-go/mongox"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// scheduler_state DAO 活体验证(MONGO_DSN 门控):原子认领语义、首次认领过渡、
// 资源类型分键、并发认领只一个成功。独立测试库 ecam_dao_test + Cleanup 整库 Drop,
// 不接触真实业务集合。
const schedulerStateTestDB = "ecam_dao_test"

func liveSchedulerStateDAO(t *testing.T) (SchedulerStateDAO, *mongo.Collection) {
	t.Helper()
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
	db := client.Database(schedulerStateTestDB)
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer dropCancel()
		_ = db.Drop(dropCtx)
		_ = client.Disconnect(dropCtx)
	})
	return NewSchedulerStateDAO(mongox.NewMongo(client, schedulerStateTestDB)), db.Collection(SchedulerStateCollection)
}

// 首次无记录视为首次认领;认领后当日不重复;跨日重新认领;nas/cdn 分键互不覆盖
func TestSchedulerStateDAO_ClaimSemantics_Live(t *testing.T) {
	d, col := liveSchedulerStateDAO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	claimed, err := d.TryClaimDaily(ctx, "nas", "2026-09-19")
	if err != nil || !claimed {
		t.Fatalf("首次认领应成功: claimed=%v err=%v", claimed, err)
	}

	claimed, err = d.TryClaimDaily(ctx, "nas", "2026-09-19")
	if err != nil || claimed {
		t.Fatalf("同日二次认领应失败: claimed=%v err=%v", claimed, err)
	}

	claimed, err = d.TryClaimDaily(ctx, "nas", "2026-09-20")
	if err != nil || !claimed {
		t.Fatalf("跨日认领应成功: claimed=%v err=%v", claimed, err)
	}

	// 分键:nas 已认领不影响 cdn 首次认领
	claimed, err = d.TryClaimDaily(ctx, "cdn", "2026-09-19")
	if err != nil || !claimed {
		t.Fatalf("cdn 键首次认领应成功: claimed=%v err=%v", claimed, err)
	}

	last, err := d.GetLastDate(ctx, "nas")
	if err != nil || last != "2026-09-20" {
		t.Fatalf("nas last_date 应为 2026-09-20: %q err=%v", last, err)
	}
	if n, err := col.CountDocuments(ctx, map[string]any{}); err != nil || n != 2 {
		t.Fatalf("应恰有 nas/cdn 两行: n=%d err=%v", n, err)
	}
}

// 并发认领:多 goroutine 同触发只一个成功(findOneAndUpdate 原子性 + upsert)
func TestSchedulerStateDAO_ConcurrentClaim_Live(t *testing.T) {
	d, _ := liveSchedulerStateDAO(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const n = 16
	var winCount int64
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, err := d.TryClaimDaily(ctx, "nas", "2026-09-19")
			if err != nil {
				t.Errorf("并发认领不应报错: %v", err)
				return
			}
			if claimed {
				mu.Lock()
				winCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if winCount != 1 {
		t.Fatalf("并发 %d 次认领应只有 1 个成功,实际 %d", n, winCount)
	}
}
