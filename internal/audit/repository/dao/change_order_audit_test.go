package dao

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/audit/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// mongox test 实例（与 internal/cert/repository 同口径）：CERT_TEST_MONGODB_DSN
// 优先，缺省本地 27017；不可达跳过集成测试。每用例独立随机库 + Cleanup Drop。
var (
	auditTestMongoOnce   sync.Once
	auditTestMongoClient *mongo.Client
	auditTestMongoErr    error
)

func connectAuditTestMongo() {
	dsn := "mongodb://127.0.0.1:27017"
	if v := os.Getenv("CERT_TEST_MONGODB_DSN"); v != "" {
		dsn = v
	}
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(dsn))
	if err != nil {
		auditTestMongoClient, auditTestMongoErr = nil, err
		return
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, readpref.PrimaryPreferred()); err != nil {
		_ = client.Disconnect(context.Background())
		auditTestMongoClient, auditTestMongoErr = nil, err
		return
	}
	auditTestMongoClient, auditTestMongoErr = client, nil
}

func newAuditTestMongo(t *testing.T) *mongox.Mongo {
	t.Helper()
	auditTestMongoOnce.Do(connectAuditTestMongo)
	if auditTestMongoErr != nil {
		t.Skipf("mongox test 实例不可用（可设置 CERT_TEST_MONGODB_DSN）: %v", auditTestMongoErr)
	}
	dbName := fmt.Sprintf("ecam_audit_test_%d", rand.Int63())
	db := mongox.NewMongo(auditTestMongoClient, dbName)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = db.Database().Drop(ctx)
	})
	return db
}

// TestChangeOrderAuditDAO_AppendAndList 仅追加 + at 升序稳定返回（5.11
// 按单查询契约：与 ChangeReport 逐条比对要求顺序稳定）。
func TestChangeOrderAuditDAO_AppendAndList(t *testing.T) {
	db := newAuditTestMongo(t)
	d := NewChangeOrderAuditDAO(db)
	requireNoErr(t, d.InitIndexes(context.Background()))

	base := time.Now().UnixMilli()
	// 乱序写入（毫秒间可能有同值：显式 at 控制）
	entries := []domain.ChangeOrderAuditEntry{
		{OrderID: "o1", Actor: "ops", Action: "create", Detail: "d1", At: base + 3000},
		{OrderID: "o1", Actor: "ops", Action: "confirm", Detail: "d2", At: base + 1000},
		{OrderID: "o1", Actor: "executor", Action: "item_result", Detail: "d3", ItemID: "i1", At: base + 1000}, // 同 at 不同 id
		{OrderID: "o2", Actor: "ops", Action: "create", Detail: "other order", At: base + 500},
	}
	for _, e := range entries {
		_, inserted, err := d.Append(context.Background(), e)
		if err != nil || !inserted {
			t.Fatalf("append %s/%s: inserted=%v err=%v", e.Action, e.Detail, inserted, err)
		}
	}

	got, err := d.ListByOrder(context.Background(), "o1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expect 3 entries for o1, got %d", len(got))
	}
	// at 升序；同 at 依 id 升序（confirm 与 item_result 的相对序按写入序/id）。
	if got[0].At > got[1].At || got[1].At > got[2].At {
		t.Fatalf("entries not at-ascending: %+v", got)
	}
	if got[0].Action != "confirm" {
		t.Fatalf("first entry should be confirm (at=base+1000), got %s", got[0].Action)
	}

	// 按单隔离
	got2, err := d.ListByOrder(context.Background(), "o2")
	if err != nil || len(got2) != 1 || got2[0].Action != "create" {
		t.Fatalf("o2 isolation failed: %+v err=%v", got2, err)
	}
}

// TestChangeOrderAuditDAO_DedupKey 幂等去重（5.9/5.10 端口契约）：同
// (orderID, action, dedupKey) 二次写入 inserted=false 且不产生重复文档；
// 无 DedupKey 的普通事件不受唯一索引影响。
func TestChangeOrderAuditDAO_DedupKey(t *testing.T) {
	db := newAuditTestMongo(t)
	d := NewChangeOrderAuditDAO(db)
	requireNoErr(t, d.InitIndexes(context.Background()))

	orphan := domain.ChangeOrderAuditEntry{
		OrderID: "o1", Actor: "scheduler", Action: "orphan_cleanup",
		Cloud: "aliyun", CloudCertID: "cc-1", OrphanAction: "cleanup",
		At: time.Now().UnixMilli(), DedupKey: "cc-1|cleanup|true",
	}
	success := true
	orphan.Success = &success

	if _, inserted, err := d.Append(context.Background(), orphan); err != nil || !inserted {
		t.Fatalf("first append: inserted=%v err=%v", inserted, err)
	}
	if _, inserted, err := d.Append(context.Background(), orphan); err != nil || inserted {
		t.Fatalf("duplicate append should return inserted=false: inserted=%v err=%v", inserted, err)
	}
	got, err := d.ListByOrderAction(context.Background(), "o1", "orphan_cleanup")
	if err != nil || len(got) != 1 {
		t.Fatalf("expect 1 orphan entry after duplicate append, got %d err=%v", len(got), err)
	}

	// verify 去重键独立于 orphan（action 维度隔离）
	verify := domain.ChangeOrderAuditEntry{
		OrderID: "o1", Actor: "scheduler", Action: "verify",
		At: time.Now().UnixMilli(), UnmetDomains: []string{"a.example.com"}, DedupKey: "42",
	}
	if _, inserted, err := d.Append(context.Background(), verify); err != nil || !inserted {
		t.Fatalf("verify append: inserted=%v err=%v", inserted, err)
	}

	// 普通事件（无 DedupKey）可任意重复追加（仅追加口径）
	plain := domain.ChangeOrderAuditEntry{OrderID: "o1", Actor: "ops", Action: "create", Detail: "again", At: time.Now().UnixMilli()}
	for i := 0; i < 2; i++ {
		if _, inserted, err := d.Append(context.Background(), plain); err != nil || !inserted {
			t.Fatalf("plain append #%d: inserted=%v err=%v", i, inserted, err)
		}
	}
}

// TestChangeOrderAuditDAO_FieldsComplete 审计字段完整性（AC：actor/时间/
// 操作对象/结果）——孤儿清理结构化载荷往返。
func TestChangeOrderAuditDAO_FieldsComplete(t *testing.T) {
	db := newAuditTestMongo(t)
	d := NewChangeOrderAuditDAO(db)
	requireNoErr(t, d.InitIndexes(context.Background()))

	success := false
	e := domain.ChangeOrderAuditEntry{
		OrderID: "o9", Actor: "scheduler", Action: "orphan_cleanup",
		Detail: "cloud=tencent cloudCertId=cc-9 action=skip_keep success=false",
		At:     time.Now().UnixMilli(),
		Cloud:  "tencent", CloudCertID: "cc-9", OrphanAction: "skip_keep",
		Success: &success, DedupKey: "cc-9|skip_keep|false",
	}
	if _, _, err := d.Append(context.Background(), e); err != nil {
		t.Fatalf("append: %v", err)
	}
	got, err := d.ListByOrder(context.Background(), "o9")
	if err != nil || len(got) != 1 {
		t.Fatalf("list: %+v err=%v", got, err)
	}
	g := got[0]
	if g.ID == 0 || g.Actor != "scheduler" || g.Cloud != "tencent" ||
		g.CloudCertID != "cc-9" || g.OrphanAction != "skip_keep" ||
		g.Success == nil || *g.Success != false || g.At == 0 {
		t.Fatalf("entry fields incomplete: %+v", g)
	}
}

func requireNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
