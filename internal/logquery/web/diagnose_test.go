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

// diagTestCloud 诊断测试专用云(独立于 handler_test.go 的 CDN 桩,防互覆)。
const diagTestCloud domain.CloudProvider = "webdiagcloud"

// diagAggStub WAF 聚合桩:固定返回桶 500 + Top IP 400(前窗同构,突增 1 倍)。
type diagAggStub struct{}

func (p *diagAggStub) Cloud() domain.CloudProvider { return diagTestCloud }
func (p *diagAggStub) LogType() logquery.LogType   { return logquery.LogTypeWAF }
func (p *diagAggStub) ListLogSources(context.Context, *domain.CloudAccount) ([]logquery.LogSource, error) {
	return nil, nil
}
func (p *diagAggStub) Search(context.Context, *domain.CloudAccount, logquery.SearchParams) ([]logquery.LogEntry, error) {
	return nil, nil
}
func (p *diagAggStub) Aggregate(context.Context, *domain.CloudAccount, logquery.AggregateParams) (*logquery.AggregateResult, error) {
	return &logquery.AggregateResult{
		Buckets: []logquery.AggregateBucket{{Timestamp: time.Now().UnixMilli() / 1000, Count: 500}},
		TopN:    []logquery.TopNItem{{Name: "1.2.3.4", Count: 400, Value: 400}},
	}, nil
}

type diagAccountSource struct{}

func (diagAccountSource) List(_ context.Context, f domain.CloudAccountFilter) ([]domain.CloudAccount, int64, error) {
	if f.TenantID != 3 {
		return nil, 0, nil
	}
	return []domain.CloudAccount{{ID: 7, Name: "diag-acc", Provider: diagTestCloud,
		Status: domain.CloudAccountStatusActive, TenantID: 3}}, 1, nil
}

func newDiagRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	logquery.RegisterProvider(diagTestCloud, logquery.LogTypeWAF, func(*domain.CloudAccount) (logquery.LogProvider, error) {
		return &diagAggStub{}, nil
	})
	svc := service.NewFederationService(diagAccountSource{}, nil)
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

// TestDiagnose POST /diagnose:WAF 判定结构完整(风险等级/类型/Top 攻击源/
// 措施/summary 空串/per-source 状态);租户边界与既有 /logs/* 一致;
// SLB 明确拒绝。AC1/AC4 + Hard Rules
func TestDiagnose(t *testing.T) {
	r := newDiagRouter(t)
	now := time.Now().UnixMilli()
	body, _ := json.Marshal(map[string]any{
		"log_type":   "waf",
		"start_time": now - 600_000,
		"end_time":   now,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cam/logs/diagnose", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data service.DiagnoseResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	d := resp.Data
	if d.Result == nil || d.Result.RiskLevel == "" || d.Result.AttackType == "" {
		t.Fatalf("result = %+v", d.Result)
	}
	if len(d.Result.Measures) < 3 {
		t.Errorf("measures = %d, want >=3", len(d.Result.Measures))
	}
	if len(d.Result.TopSources) == 0 || d.Result.TopSources[0].IP != "1.2.3.4" ||
		d.Result.TopSources[0].Count != 400 || d.Result.TopSources[0].Share != 0.8 {
		t.Errorf("top_sources = %+v, want 1.2.3.4/400/0.8", d.Result.TopSources)
	}
	if d.Summary != "" {
		t.Errorf("summary = %q, want 空串(AI 解读后置)", d.Summary)
	}
	if d.AggregateFrames != 2 {
		t.Errorf("aggregate_frames = %d, want 2", d.AggregateFrames)
	}
	if len(d.Sources) != 1 || d.Sources[0].Error != "" {
		t.Errorf("sources = %+v", d.Sources)
	}
	if d.Total != 500 || len(d.Buckets) == 0 || len(d.TopIPs) == 0 {
		t.Errorf("current window details = total %d buckets %d topIPs %d", d.Total, len(d.Buckets), len(d.TopIPs))
	}
	// 前窗同构(500/400):突增倍数 1,不降级。
	if d.Prev == nil || d.Result.SurgeMultiplier != 1 || d.Result.Degraded {
		t.Errorf("prev/surge/degraded = %+v/%v/%v, want prev 非空/1/false", d.Prev, d.Result.SurgeMultiplier, d.Result.Degraded)
	}

	// 无租户上下文:403(与 search/aggregate 同边界)。
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/cam/logs/diagnose?anon=1", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Errorf("anon: code = %d, want 403", w2.Code)
	}

	// SLB 不开放:400 明确错误(Hard Rules:仅 WAF)。
	bad, _ := json.Marshal(map[string]any{"log_type": "slb", "start_time": now - 1, "end_time": now})
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/cam/logs/diagnose", bytes.NewReader(bad))
	req3.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusBadRequest || !bytes.Contains(w3.Body.Bytes(), []byte("only supports waf")) {
		t.Errorf("slb: code = %d body=%s, want 400 waf-only", w3.Code, w3.Body.String())
	}

	// 非法请求体:400。
	w4 := httptest.NewRecorder()
	req4 := httptest.NewRequest(http.MethodPost, "/api/v1/cam/logs/diagnose", bytes.NewReader([]byte("{broken")))
	req4.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusBadRequest {
		t.Errorf("broken body: code = %d, want 400", w4.Code)
	}
}
