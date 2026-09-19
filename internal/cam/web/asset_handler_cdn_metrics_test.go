package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

// fakeCDNQuery 全能力 CDN 查询服务 mock(仅指标方法生效)
type fakeCDNQuery struct {
	gotMetricsTenant int64
	gotMetricsDomain string
	gotMetricsDays   int
	gotTopTenant     int64
	gotTopDays       int
	gotTopLimit      int
	metrics          []types.CDNMetric
	top              []types.CDNMetricTopRow
}

func (f *fakeCDNQuery) GetCacheConfig(context.Context, int64, int64, string, string) ([]types.CDNCacheRule, error) {
	return nil, nil
}
func (f *fakeCDNQuery) GetDomainSettings(context.Context, int64, int64, string, string) (*types.CDNDomainSettings, error) {
	return nil, nil
}
func (f *fakeCDNQuery) GetDomainMetrics(_ context.Context, tenantID int64, domainName string, days int) ([]types.CDNMetric, error) {
	f.gotMetricsTenant = tenantID
	f.gotMetricsDomain = domainName
	f.gotMetricsDays = days
	return f.metrics, nil
}
func (f *fakeCDNQuery) GetTopDomains(_ context.Context, tenantID int64, days, limit int) ([]types.CDNMetricTopRow, error) {
	f.gotTopTenant = tenantID
	f.gotTopDays = days
	f.gotTopLimit = limit
	return f.top, nil
}

// newCDNMetricsRouter 组装仅含 metrics/top 路由的测试路由,并注入租户 7
func newCDNMetricsRouter(t *testing.T, fake *fakeCDNQuery) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewAssetHandler(nil, nil, fake, nil)
	r.Use(func(c *gin.Context) {
		c.Set(middleware.TenantIDKey, int64(7))
		c.Next()
	})
	grp := r.Group("/api/v1/cam/assets")
	grp.GET("/cdn/metrics", h.GetCDNDomainMetrics)
	grp.GET("/cdn/top", h.GetCDNTopDomains)
	return r
}

func doJSON(t *testing.T, r *gin.Engine, path string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	r.ServeHTTP(w, req)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
	return w.Code, body
}

// metrics 正常路径:透传域名/days,租户来自上下文,items 结构对齐
func TestGetCDNDomainMetrics_OK(t *testing.T) {
	fake := &fakeCDNQuery{metrics: []types.CDNMetric{
		{Domain: "a.example.com", Date: "2026-09-14", Bytes: 100, Bandwidth: 50, HitRate: -1},
		{Domain: "a.example.com", Date: "2026-09-13", Bytes: 80, Bandwidth: 40, HitRate: 0.9},
	}}
	r := newCDNMetricsRouter(t, fake)

	code, body := doJSON(t, r, "/api/v1/cam/assets/cdn/metrics?domain_name=a.example.com&days=14")
	if code != 200 {
		t.Fatalf("status = %d, body=%s", code, body)
	}
	if fake.gotMetricsTenant != 7 || fake.gotMetricsDomain != "a.example.com" || fake.gotMetricsDays != 14 {
		t.Fatalf("service args = tenant %d, domain %q, days %d", fake.gotMetricsTenant, fake.gotMetricsDomain, fake.gotMetricsDays)
	}
	data := body["data"].(map[string]any)
	if data["domain"] != "a.example.com" {
		t.Fatalf("domain = %v", data["domain"])
	}
	items := data["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2", len(items))
	}
	first := items[0].(map[string]any)
	if first["date"] != "2026-09-14" || first["bytes"].(float64) != 100 || first["bandwidth"].(float64) != 50 || first["hit_rate"].(float64) != -1 {
		t.Fatalf("items[0] = %v", first)
	}
}

// metrics 缺参 → 400
func TestGetCDNDomainMetrics_MissingDomain(t *testing.T) {
	r := newCDNMetricsRouter(t, &fakeCDNQuery{})
	code, body := doJSON(t, r, "/api/v1/cam/assets/cdn/metrics")
	if code != 400 {
		t.Fatalf("status = %d, want 400", code)
	}
	if body["code"].(float64) == 0 {
		t.Fatalf("expected error code, got %v", body)
	}
}

// metrics days 非法 → 400
func TestGetCDNDomainMetrics_InvalidDays(t *testing.T) {
	r := newCDNMetricsRouter(t, &fakeCDNQuery{})
	for _, q := range []string{"?domain_name=a.example.com&days=abc", "?domain_name=a.example.com&days=0", "?domain_name=a.example.com&days=-1"} {
		code, _ := doJSON(t, r, "/api/v1/cam/assets/cdn/metrics"+q)
		if code != 400 {
			t.Fatalf("query %q status = %d, want 400", q, code)
		}
	}
}

// top 正常路径:metric 缺省按 bytes,items 仅含 domain/bytes
func TestGetCDNTopDomains_OK(t *testing.T) {
	fake := &fakeCDNQuery{top: []types.CDNMetricTopRow{
		{Domain: "big.example.com", Bytes: 900, Days: 7, Count: 7},
		{Domain: "mid.example.com", Bytes: 500, Days: 7, Count: 7},
	}}
	r := newCDNMetricsRouter(t, fake)

	code, body := doJSON(t, r, "/api/v1/cam/assets/cdn/top?metric=bytes&days=7&limit=10")
	if code != 200 {
		t.Fatalf("status = %d, body=%s", code, body)
	}
	if fake.gotTopTenant != 7 || fake.gotTopDays != 7 || fake.gotTopLimit != 10 {
		t.Fatalf("service args = tenant %d, days %d, limit %d", fake.gotTopTenant, fake.gotTopDays, fake.gotTopLimit)
	}
	items := body["data"].(map[string]any)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2", len(items))
	}
	first := items[0].(map[string]any)
	if first["domain"] != "big.example.com" || first["bytes"].(float64) != 900 {
		t.Fatalf("items[0] = %v", first)
	}
	if _, has := first["count"]; has {
		t.Fatalf("top 响应不应透出 count/days 内部字段: %v", first)
	}
}

// top metric 参数仅支持 bytes
func TestGetCDNTopDomains_InvalidMetric(t *testing.T) {
	r := newCDNMetricsRouter(t, &fakeCDNQuery{})
	code, _ := doJSON(t, r, "/api/v1/cam/assets/cdn/top?metric=bandwidth")
	if code != 400 {
		t.Fatalf("status = %d, want 400", code)
	}
}

// top days/limit 非法 → 400
func TestGetCDNTopDomains_InvalidPaging(t *testing.T) {
	r := newCDNMetricsRouter(t, &fakeCDNQuery{})
	for _, q := range []string{"?days=0", "?limit=-5", "?days=x"} {
		code, _ := doJSON(t, r, "/api/v1/cam/assets/cdn/top"+q)
		if code != 400 {
			t.Fatalf("query %q status = %d, want 400", q, code)
		}
	}
}
