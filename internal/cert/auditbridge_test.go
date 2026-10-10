package cert

import (
	"context"
	"testing"
	"time"

	certservice "github.com/Havens-blog/e-cam-service/internal/cert/service"
)

// fakeChangeAuditStore 内存 ChangeAuditStore（追加即存；去重键命中即
// inserted=false）。复刻 internal/audit 单集合仅追加语义：RecordDedup 按
// (OrderID, Action, DedupKey) 去重；ListUnmetDomains 倒序取最近一条非空；
// ListOrphanCleanupResults 按 action 过滤。桥仅依赖 cert 中性端口，故测试亦
// 不 import internal/audit。
type fakeChangeAuditStore struct {
	entries []certservice.ChangeAuditEntry
}

func (f *fakeChangeAuditStore) Record(_ context.Context, e certservice.ChangeAuditEntry) error {
	f.entries = append(f.entries, e)
	return nil
}

func (f *fakeChangeAuditStore) RecordDedup(_ context.Context, e certservice.ChangeAuditEntry) (bool, error) {
	if e.DedupKey != "" {
		for _, ex := range f.entries {
			if ex.OrderID == e.OrderID && ex.Action == e.Action && ex.DedupKey == e.DedupKey {
				return false, nil
			}
		}
	}
	f.entries = append(f.entries, e)
	return true, nil
}

func (f *fakeChangeAuditStore) ListByOrder(_ context.Context, orderID string) ([]certservice.ChangeAuditEntry, error) {
	var out []certservice.ChangeAuditEntry
	for _, e := range f.entries {
		if e.OrderID == orderID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeChangeAuditStore) ListOrphanCleanupResults(_ context.Context, orderID string) ([]certservice.ChangeAuditEntry, error) {
	var out []certservice.ChangeAuditEntry
	for _, e := range f.entries {
		if e.OrderID == orderID && e.Action == certservice.AuditActionOrphanCleanup {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeChangeAuditStore) ListUnmetDomains(_ context.Context, orderID string) ([]string, error) {
	// at 升序（插入序等价）；自尾向前取最近一条非空清单。
	for i := len(f.entries) - 1; i >= 0; i-- {
		e := f.entries[i]
		if e.OrderID == orderID && e.Action == certservice.AuditActionVerify && len(e.UnmetDomains) > 0 {
			return e.UnmetDomains, nil
		}
	}
	return []string{}, nil
}

func newBridgeForTest() (*changeAuditBridge, *fakeChangeAuditStore) {
	fake := &fakeChangeAuditStore{}
	return &changeAuditBridge{store: fake}, fake
}

// TestAuditBridge_WriteAndListByOrder 写入→按单读取往返：ChangeAuditEvent
// 全字段（orderID/itemID/actor/action/detail/at）不丢失，at 毫秒往返一致。
func TestAuditBridge_WriteAndListByOrder(t *testing.T) {
	bridge, fake := newBridgeForTest()
	ctx := context.Background()
	at := time.Now().Truncate(time.Millisecond)

	events := []certservice.ChangeAuditEvent{
		{OrderID: "o1", Actor: "ops@example.com", Action: certservice.AuditActionCreate, Detail: "create change order", At: at},
		{OrderID: "o1", Actor: "ops@example.com", Action: certservice.AuditActionConfirm, Detail: "confirm", At: at.Add(time.Minute)},
		{OrderID: "o1", ItemID: "i1", Actor: certservice.ActorExecutor, Action: certservice.AuditActionItemResult, Detail: "item finished status=success", At: at.Add(2 * time.Minute)},
	}
	for _, e := range events {
		if err := bridge.WriteChangeAudit(ctx, e); err != nil {
			t.Fatalf("write %s: %v", e.Action, err)
		}
	}

	logs, err := bridge.ListByOrder(ctx, "o1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("expect 3 logs, got %d", len(logs))
	}
	if logs[0].Action != certservice.AuditActionCreate || logs[2].Action != certservice.AuditActionItemResult {
		t.Fatalf("order mismatch: %+v", logs)
	}
	if logs[0].Actor != "ops@example.com" || logs[2].ItemID != "i1" {
		t.Fatalf("fields lost: %+v", logs)
	}
	if !logs[0].At.Equal(at) {
		t.Fatalf("at roundtrip mismatch: %v vs %v", logs[0].At, at)
	}
	if len(fake.entries) != 3 {
		t.Fatalf("fake should hold 3 entries, got %d", len(fake.entries))
	}
}

// TestAuditBridge_RecordRollback 5.8 事件映射：Outcome 并入 detail、ItemID
// 透传、actor 取 ctx 操作者（HTTP 触发回滚归因）。
func TestAuditBridge_RecordRollback(t *testing.T) {
	bridge, _ := newBridgeForTest()
	ctx := certservice.WithOperator(context.Background(), "sup@example.com")
	at := time.Now().Truncate(time.Millisecond)

	err := bridge.RecordRollback(ctx, certservice.RollbackAuditEvent{
		OrderID: "o1", ItemID: "i9",
		Outcome: certservice.RollbackAuditItemRolledBack,
		Detail:  "resource=cdn/x cloudCertId=cc-1 orphanCleaned=true",
		At:      at,
	})
	if err != nil {
		t.Fatalf("record rollback: %v", err)
	}

	logs, err := bridge.ListByOrder(ctx, "o1")
	if err != nil || len(logs) != 1 {
		t.Fatalf("list: %+v err=%v", logs, err)
	}
	l := logs[0]
	if l.Action != certservice.AuditActionRollback || l.ItemID != "i9" || l.Actor != "sup@example.com" {
		t.Fatalf("rollback mapping wrong: %+v", l)
	}
	if l.Detail != certservice.RollbackAuditItemRolledBack+": resource=cdn/x cloudCertId=cc-1 orphanCleaned=true" {
		t.Fatalf("detail mapping wrong: %q", l.Detail)
	}
	if !l.At.Equal(at) {
		t.Fatalf("at mismatch: %v", l.At)
	}
}

// TestAuditBridge_OrphanAndVerify 5.9/5.10 端口：去重键幂等（重复 false）+
// 报告存档读侧投影（OrphanCleanupResult / UnmetDomains 往返）。
func TestAuditBridge_OrphanAndVerify(t *testing.T) {
	bridge, _ := newBridgeForTest()
	ctx := context.Background()
	at := time.Now().Truncate(time.Millisecond)

	// 孤儿清理：首写 true，同键重写 false（抑制重复告警口径）
	inserted, err := bridge.RecordOrphanCleanup(ctx, "o1", certservice.OrphanCleanupResult{
		Cloud: "aliyun", CloudCertID: "cc-1", Action: certservice.OrphanActionCleanup,
		Success: true, At: at,
	})
	if err != nil || !inserted {
		t.Fatalf("first orphan record: inserted=%v err=%v", inserted, err)
	}
	inserted, err = bridge.RecordOrphanCleanup(ctx, "o1", certservice.OrphanCleanupResult{
		Cloud: "aliyun", CloudCertID: "cc-1", Action: certservice.OrphanActionCleanup,
		Success: true, At: at.Add(time.Hour), // 时间不同但去重键相同
	})
	if err != nil || inserted {
		t.Fatalf("duplicate orphan record should be inserted=false: %v err=%v", inserted, err)
	}

	// 验证窗口未达标存档
	inserted, err = bridge.RecordUnmetDomains(ctx, "o1", []string{"a.example.com", "b.example.com"}, at)
	if err != nil || !inserted {
		t.Fatalf("verify record: inserted=%v err=%v", inserted, err)
	}

	// 读侧投影
	results, err := bridge.ListOrphanCleanup(ctx, "o1")
	if err != nil || len(results) != 1 {
		t.Fatalf("orphan read: %+v err=%v", results, err)
	}
	r := results[0]
	if r.Cloud != "aliyun" || r.CloudCertID != "cc-1" || r.Action != certservice.OrphanActionCleanup || !r.Success || !r.At.Equal(at) {
		t.Fatalf("orphan projection wrong: %+v", r)
	}

	unmet, err := bridge.ListUnmetDomains(ctx, "o1")
	if err != nil || len(unmet) != 2 || unmet[0] != "a.example.com" {
		t.Fatalf("unmet read: %+v err=%v", unmet, err)
	}
}

// TestAuditBridge_UnmetLatestWins 多次窗口存档取最近一条（终局判定固化，
// 查询期不重算防漂移）。
func TestAuditBridge_UnmetLatestWins(t *testing.T) {
	bridge, _ := newBridgeForTest()
	ctx := context.Background()
	base := time.Now().Truncate(time.Millisecond)

	if _, err := bridge.RecordUnmetDomains(ctx, "o1", []string{"old.example.com"}, base); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := bridge.RecordUnmetDomains(ctx, "o1", []string{"new1.example.com", "new2.example.com"}, base.Add(time.Hour)); err != nil {
		t.Fatalf("second: %v", err)
	}
	unmet, err := bridge.ListUnmetDomains(ctx, "o1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// fake DAO 保插入序（at 升序等价），服务层取最近一条非空。
	if len(unmet) != 2 || unmet[0] != "new1.example.com" {
		t.Fatalf("expect latest archive, got %+v", unmet)
	}
}

// TestAuditBridge_SystemActorFallback 系统事件 actor 回退：无操作者 ctx 时
// verify/orphan/rollback 写入侧用 scheduler 标识（item_result 的 executor
// 回退在执行引擎 auditItemResult，见 execute_service_test）。
func TestAuditBridge_SystemActorFallback(t *testing.T) {
	bridge, _ := newBridgeForTest()
	ctx := context.Background()

	if _, err := bridge.RecordUnmetDomains(ctx, "o1", []string{"a.example.com"}, time.Now()); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if _, err := bridge.RecordOrphanCleanup(ctx, "o1", certservice.OrphanCleanupResult{
		Cloud: "tencent", CloudCertID: "cc-2", Action: certservice.OrphanActionCleanup, Success: true, At: time.Now(),
	}); err != nil {
		t.Fatalf("orphan: %v", err)
	}
	if err := bridge.RecordRollback(ctx, certservice.RollbackAuditEvent{
		OrderID: "o1", Outcome: certservice.RollbackAuditOrderHeld, At: time.Now(),
	}); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	logs, err := bridge.ListByOrder(ctx, "o1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, l := range logs {
		if l.Actor != certservice.ActorScheduler {
			t.Fatalf("%s actor should fall back to scheduler, got %q", l.Action, l.Actor)
		}
	}
	if len(logs) != 3 {
		t.Fatalf("expect 3 system events, got %d", len(logs))
	}
}
