package main

// cert ChangeAuditStore 远程适配器的往返测试。
//
// 立一个实现内部契约的 httptest 假生产方（内存审计存储），逐方法验证：
//   - Record 写路径（POST + body 形状）
//   - RecordDedup 幂等语义（同 dedupKey 第二次 inserted=false）
//   - List* 读路径（GET + orderId query + entries 解码）
//   - 嵌套载荷 *bool（Success）/ []string（UnmetDomains）跨 JSON 往返不失真
//
// 这把 audit 远程 adapter（含写路径与幂等，cert 变更执行最重的端口）从死代码
// 变成可执行验证的真实代码：真实生产方（audit 域）上线后匹配同一契约即可工作。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	certservice "github.com/Havens-blog/e-cam-service/internal/cert/service"
)

// fakeAuditUpstream 内存实现 audit 内部契约的假生产方。
type fakeAuditUpstream struct {
	entries   []auditEntryDTO
	dedupKeys map[string]bool
}

func newFakeAuditUpstream() *fakeAuditUpstream {
	return &fakeAuditUpstream{dedupKeys: map[string]bool{}}
}

func (f *fakeAuditUpstream) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/internal/v1/change-audit/dedup", func(w http.ResponseWriter, r *http.Request) {
		var e auditEntryDTO
		_ = json.NewDecoder(r.Body).Decode(&e)
		inserted := true
		if e.DedupKey != "" && f.dedupKeys[e.DedupKey] {
			inserted = false
		} else {
			if e.DedupKey != "" {
				f.dedupKeys[e.DedupKey] = true
			}
			f.entries = append(f.entries, e)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"inserted": inserted})
	})

	mux.HandleFunc("/internal/v1/change-audit/orphan-cleanup", func(w http.ResponseWriter, r *http.Request) {
		order := r.URL.Query().Get("orderId")
		var out []auditEntryDTO
		for _, e := range f.entries {
			if e.OrderID == order && e.Action == "orphan_cleanup" {
				out = append(out, e)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": out})
	})

	mux.HandleFunc("/internal/v1/change-audit/unmet-domains", func(w http.ResponseWriter, r *http.Request) {
		order := r.URL.Query().Get("orderId")
		var domains []string
		for _, e := range f.entries {
			if e.OrderID == order && len(e.UnmetDomains) > 0 {
				domains = e.UnmetDomains
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"domains": domains})
	})

	// 注意：/change-audit 既是 Record(POST) 也是 ListByOrder(GET)。
	mux.HandleFunc("/internal/v1/change-audit", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var e auditEntryDTO
			if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.entries = append(f.entries, e)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			order := r.URL.Query().Get("orderId")
			var out []auditEntryDTO
			for _, e := range f.entries {
				if e.OrderID == order {
					out = append(out, e)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"entries": out})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	return mux
}

func boolPtr(b bool) *bool { return &b }

func TestRemoteAuditStore_RoundTrip(t *testing.T) {
	up := newFakeAuditUpstream()
	srv := httptest.NewServer(up.handler())
	defer srv.Close()

	store := newRemoteAuditStore(srv.URL)
	ctx := context.Background()

	// 1) Record 写路径。
	base := certservice.ChangeAuditEntry{
		OrderID: "ord-1", ItemID: "it-1", Actor: "alice", Action: "execute",
		Detail: "signed", At: 1700000000000,
	}
	if err := store.Record(ctx, base); err != nil {
		t.Fatalf("Record 失败: %v", err)
	}

	// 2) ListByOrder 读回。
	got, err := store.ListByOrder(ctx, "ord-1")
	if err != nil {
		t.Fatalf("ListByOrder 失败: %v", err)
	}
	if len(got) != 1 || got[0].Actor != "alice" || got[0].At != 1700000000000 {
		t.Fatalf("ListByOrder 往返失真: %+v", got)
	}

	// 3) 嵌套载荷 *bool + []string 往返（orphan_cleanup + verify）。
	orphan := certservice.ChangeAuditEntry{
		OrderID: "ord-1", Action: "orphan_cleanup", Cloud: "aliyun",
		CloudCertID: "c-9", OrphanAction: "delete", Success: boolPtr(true),
		At: 1700000001000, DedupKey: "orphan:c-9:delete:true",
	}
	if _, err := store.RecordDedup(ctx, orphan); err != nil {
		t.Fatalf("RecordDedup(orphan) 失败: %v", err)
	}
	verify := certservice.ChangeAuditEntry{
		OrderID: "ord-1", Action: "verify", UnmetDomains: []string{"a.example.com", "b.example.com"},
		At: 1700000002000, DedupKey: "verify:1700000002000",
	}
	if _, err := store.RecordDedup(ctx, verify); err != nil {
		t.Fatalf("RecordDedup(verify) 失败: %v", err)
	}

	// *bool 往返：orphan 清理结果里 Success 必须仍是 true（非 nil）。
	orphans, err := store.ListOrphanCleanupResults(ctx, "ord-1")
	if err != nil {
		t.Fatalf("ListOrphanCleanupResults 失败: %v", err)
	}
	if len(orphans) != 1 {
		t.Fatalf("期望 1 条孤儿清理结果，得 %d", len(orphans))
	}
	if orphans[0].Success == nil || *orphans[0].Success != true {
		t.Errorf("*bool Success 往返失真: %v", orphans[0].Success)
	}

	// []string 往返。
	domains, err := store.ListUnmetDomains(ctx, "ord-1")
	if err != nil {
		t.Fatalf("ListUnmetDomains 失败: %v", err)
	}
	if len(domains) != 2 || domains[0] != "a.example.com" || domains[1] != "b.example.com" {
		t.Errorf("[]string UnmetDomains 往返失真: %v", domains)
	}
}

func TestRemoteAuditStore_DedupIdempotency(t *testing.T) {
	up := newFakeAuditUpstream()
	srv := httptest.NewServer(up.handler())
	defer srv.Close()

	store := newRemoteAuditStore(srv.URL)
	ctx := context.Background()

	entry := certservice.ChangeAuditEntry{
		OrderID: "ord-2", Action: "orphan_cleanup", Success: boolPtr(false),
		DedupKey: "orphan:c-1:delete:false", At: 1700000000000,
	}

	// 首次插入 inserted=true。
	ins, err := store.RecordDedup(ctx, entry)
	if err != nil {
		t.Fatalf("首次 RecordDedup 失败: %v", err)
	}
	if !ins {
		t.Fatal("首次 RecordDedup 期望 inserted=true")
	}

	// 同 dedupKey 第二次 inserted=false（幂等）。
	ins, err = store.RecordDedup(ctx, entry)
	if err != nil {
		t.Fatalf("二次 RecordDedup 失败: %v", err)
	}
	if ins {
		t.Fatal("二次 RecordDedup 期望 inserted=false（幂等去重）")
	}
}
