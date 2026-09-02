package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/logquery/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

const (
	testCloud domain.CloudProvider = "webtestcloud"
	tenantKey                      = "tenant_id"
)

type webEntry struct {
	Ts int64 `json:"timestamp"`
}

func (e *webEntry) GetTimestamp() int64       { return e.Ts }
func (e *webEntry) GetMeta() logquery.LogMeta { return logquery.LogMeta{Cloud: testCloud} }

type webAccountSource struct{}

func (webAccountSource) List(_ context.Context, f domain.CloudAccountFilter) ([]domain.CloudAccount, int64, error) {
	if f.TenantID != 3 {
		return nil, 0, nil
	}
	return []domain.CloudAccount{{ID: 1, Name: "acc", Provider: testCloud,
		Status: domain.CloudAccountStatusActive, TenantID: 3}}, 1, nil
}

func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	now := time.Now().UnixMilli()
	logquery.RegisterProvider(testCloud, logquery.LogTypeCDN, func(*domain.CloudAccount) (logquery.LogProvider, error) {
		return &stubProvider{entries: []logquery.LogEntry{&webEntry{Ts: now - 1000}}}, nil
	})
	svc := service.NewFederationService(webAccountSource{}, nil)
	r := gin.New()
	g := r.Group("/api/v1/cam/logs")
	g.Use(func(c *gin.Context) {
		if c.Query("anon") == "1" {
			c.Next()
			return
		}
		c.Set(middleware.TenantIDKey, int64(3))
		c.Next()
	})
	NewLogQueryHandler(svc).RegisterRoutes(g)
	return r
}

type stubProvider struct {
	entries []logquery.LogEntry
}

func (p *stubProvider) Cloud() domain.CloudProvider { return testCloud }
func (p *stubProvider) LogType() logquery.LogType   { return logquery.LogTypeCDN }
func (p *stubProvider) ListLogSources(context.Context, *domain.CloudAccount) ([]logquery.LogSource, error) {
	return []logquery.LogSource{{Cloud: testCloud, ResourceID: "api.example.com", Enabled: true}}, nil
}
func (p *stubProvider) Search(context.Context, *domain.CloudAccount, logquery.SearchParams) ([]logquery.LogEntry, error) {
	return p.entries, nil
}

func TestTypes(t *testing.T) {
	r := newTestRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/cam/logs/types", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	var resp struct {
		Data []struct {
			Type   string `json:"type"`
			Fields []struct {
				Key string `json:"key"`
			} `json:"fields"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 3 {
		t.Fatalf("types = %d, want 3", len(resp.Data))
	}
	for _, m := range resp.Data {
		if len(m.Fields) == 0 {
			t.Errorf("type %s has no fields", m.Type)
		}
	}
}

func TestSources(t *testing.T) {
	r := newTestRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/cam/logs/sources?log_type=cdn", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []logquery.LogSource `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || resp.Data[0].ResourceID != "api.example.com" {
		t.Fatalf("sources = %+v", resp.Data)
	}
	// 缺 log_type:400
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/v1/cam/logs/sources", nil))
	if w2.Code != http.StatusBadRequest {
		t.Errorf("missing log_type: code = %d, want 400", w2.Code)
	}
}

func TestSearch(t *testing.T) {
	r := newTestRouter(t)
	now := time.Now().UnixMilli()
	body, _ := json.Marshal(map[string]any{
		"log_type":   "cdn",
		"start_time": now - 3600_000,
		"end_time":   now,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cam/logs/search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Total   int                     `json:"total"`
			Entries []json.RawMessage       `json:"entries"`
			Sources []service.SourceOutcome `json:"sources"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Total != 1 || len(resp.Data.Entries) != 1 {
		t.Fatalf("data = %+v", resp.Data)
	}
	var entry map[string]any
	if err := json.Unmarshal(resp.Data.Entries[0], &entry); err != nil {
		t.Fatal(err)
	}
	if entry["timestamp"] == nil {
		t.Fatalf("entry missing timestamp: %v", entry)
	}
	// 无租户上下文:403
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/cam/logs/search?anon=1", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Errorf("anon: code = %d, want 403", w2.Code)
	}
	// 非法 log_type:400
	bad, _ := json.Marshal(map[string]any{"log_type": "nope", "start_time": now, "end_time": now + 1})
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/cam/logs/search", bytes.NewReader(bad))
	req3.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusBadRequest {
		t.Errorf("bad type: code = %d, want 400", w3.Code)
	}
}
