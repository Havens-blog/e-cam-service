package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/logquery/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

type auditCaptureSink struct {
	mu      sync.Mutex
	entries []LogQueryAudit
}

func (s *auditCaptureSink) WriteLogQuery(_ context.Context, e LogQueryAudit) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, e)
	return nil
}

func (s *auditCaptureSink) wait(t *testing.T, n int) []LogQueryAudit {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		got := len(s.entries)
		s.mu.Unlock()
		if got >= n {
			s.mu.Lock()
			defer s.mu.Unlock()
			return append([]LogQueryAudit(nil), s.entries...)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("audit not written within deadline: got %d want %d", len(s.entries), n)
	return nil
}

func newAuditTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/cam/logs/search", nil)
	c.Request.Header.Set("User-Agent", "test-agent")
	c.Request.RemoteAddr = "1.2.3.4:5678"
	c.Set(middleware.CtxUidKey, int64(7))
	c.Set(middleware.CtxUsernameKey, "ops")
	c.Set(middleware.TenantIDKey, int64(3))
	c.Set(middleware.CtxRequestIDKey, "req-123")
	return c
}

func TestHasPartialFailure(t *testing.T) {
	cases := []struct {
		name    string
		sources []service.SourceOutcome
		want    bool
	}{
		{"无源(空结果)", nil, false},
		{"全部成功", []service.SourceOutcome{{Error: ""}, {Error: ""}}, false},
		{"全部失败", []service.SourceOutcome{{Error: "a"}, {Error: "b"}}, false},
		{"部分成功", []service.SourceOutcome{{Error: ""}, {Error: "timeout"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasPartialFailure(tc.sources); got != tc.want {
				t.Fatalf("hasPartialFailure = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestLogQueryAuditSummaryExcludesSensitive 审计摘要只含查询范围,不含关键字
// query 与字段筛选 filters(可能含敏感检索词/凭据)。
func TestLogQueryAuditSummaryExcludesSensitive(t *testing.T) {
	e := LogQueryAudit{
		LogType:     "cdn",
		Clouds:      []string{"aliyun"},
		AccountIDs:  []int64{1, 2},
		Resources:   []string{"a.example.com"},
		StartTime:   1700000000000,
		EndTime:     1700003600000,
		ResultCount: 42,
	}
	raw := e.Summary()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("summary is not valid json: %v, raw=%s", err, raw)
	}
	for _, forbidden := range []string{"query", "filters", "keyword"} {
		if _, ok := m[forbidden]; ok {
			t.Fatalf("summary must not contain %q: %s", forbidden, raw)
		}
	}
	if m["log_type"] != "cdn" || m["result_count"] != float64(42) {
		t.Fatalf("summary fields wrong: %s", raw)
	}
}

func TestRecordSearchAuditSuccess(t *testing.T) {
	sink := &auditCaptureSink{}
	h := &LogQueryHandler{audit: sink}
	c := newAuditTestContext()
	req := searchRequest{LogType: "cdn", StartTime: 1000, EndTime: 2000, Clouds: []string{"aliyun"}, Query: "super-secret-keyword"}
	resp := &service.SearchResponse{
		Total:   5,
		Sources: []service.SourceOutcome{{Cloud: "aliyun", Count: 5}},
	}
	h.recordSearchAudit(c, req, resp, nil, time.Now().Add(-10*time.Millisecond))

	e := sink.wait(t, 1)[0]
	if !e.Success || e.Partial {
		t.Fatalf("success audit flags wrong: success=%v partial=%v", e.Success, e.Partial)
	}
	if e.ResultCount != 5 || e.LogType != "cdn" || e.TenantID != 3 || e.OperatorID != "7" || e.OperatorName != "ops" {
		t.Fatalf("audit entry fields wrong: %+v", e)
	}
	if strings.Contains(e.Summary(), "super-secret-keyword") {
		t.Fatalf("audit summary leaked query keyword: %s", e.Summary())
	}
}

func TestRecordSearchAuditPartial(t *testing.T) {
	sink := &auditCaptureSink{}
	h := &LogQueryHandler{audit: sink}
	c := newAuditTestContext()
	req := searchRequest{LogType: "waf", StartTime: 1000, EndTime: 2000}
	resp := &service.SearchResponse{
		Total: 3,
		Sources: []service.SourceOutcome{
			{Cloud: "aliyun", Count: 2},
			{Cloud: "aws", Error: "timeout"},
		},
	}
	h.recordSearchAudit(c, req, resp, nil, time.Now().Add(-10*time.Millisecond))

	e := sink.wait(t, 1)[0]
	if !e.Success || !e.Partial {
		t.Fatalf("partial audit flags wrong: success=%v partial=%v", e.Success, e.Partial)
	}
	if e.ResultCount != 3 {
		t.Fatalf("result_count = %d, want 3", e.ResultCount)
	}
}

func TestRecordSearchAuditFailed(t *testing.T) {
	sink := &auditCaptureSink{}
	h := &LogQueryHandler{audit: sink}
	c := newAuditTestContext()
	req := searchRequest{LogType: "cdn", StartTime: 1000, EndTime: 2000}
	h.recordSearchAudit(c, req, nil, errors.New("all sources failed"), time.Now().Add(-10*time.Millisecond))

	e := sink.wait(t, 1)[0]
	if e.Success || e.Partial {
		t.Fatalf("failed audit flags wrong: success=%v partial=%v", e.Success, e.Partial)
	}
	if e.ResultCount != 0 {
		t.Fatalf("result_count = %d, want 0", e.ResultCount)
	}
}

func TestRecordExportAudit(t *testing.T) {
	sink := &auditCaptureSink{}
	h := &LogQueryHandler{audit: sink}
	c := newAuditTestContext()
	req := exportAuditRequest{
		LogType:    "cdn",
		StartTime:  1000,
		EndTime:    2000,
		Clouds:     []string{"aliyun"},
		AccountIDs: []int64{1},
		Resources:  []string{"a.example.com"},
		Count:      42,
	}
	h.recordExportAudit(c, req)

	e := sink.wait(t, 1)[0]
	if e.Operation != "export" || !e.Exported || !e.Success {
		t.Fatalf("export audit flags wrong: op=%s exported=%v success=%v", e.Operation, e.Exported, e.Success)
	}
	if e.ResultCount != 42 || e.LogType != "cdn" || e.TenantID != 3 || e.OperatorID != "7" {
		t.Fatalf("export audit fields wrong: %+v", e)
	}
	if strings.Contains(e.Summary(), "query") || strings.Contains(e.Summary(), "filters") {
		t.Fatalf("export audit summary leaked sensitive keys: %s", e.Summary())
	}
}

// TestExportAuditHandler 端点契约:POST /export-audit 落一条 log_export 审计,
// 返回 200;无租户上下文仍被组级 RequireTenant 语义外的 tenantID 防御拒绝。
func TestExportAuditHandler(t *testing.T) {
	sink := &auditCaptureSink{}
	h := NewLogQueryHandler(service.NewFederationService(webAccountSource{}, nil))
	h.SetAuditSink(sink)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1/cam/logs")
	g.Use(func(c *gin.Context) { c.Set(middleware.TenantIDKey, int64(3)); c.Next() })
	h.RegisterRoutes(g)

	body := `{"log_type":"cdn","start_time":1000,"end_time":2000,"clouds":["aliyun"],"account_ids":[1],"resources":["a.example.com"],"count":42}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cam/logs/export-audit", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}

	e := sink.wait(t, 1)[0]
	if e.Operation != "export" || e.ResultCount != 42 {
		t.Fatalf("handler audit wrong: %+v", e)
	}
}
