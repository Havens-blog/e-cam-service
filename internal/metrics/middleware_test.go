package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(PrometheusMiddleware())
	return r
}

// TestPrometheusMiddlewareRecordsRouteTemplate 验证 route 标签取路由模板
// （c.FullPath()）而非原始路径，避免租户/资源 ID 撑爆指标基数。
func TestPrometheusMiddlewareRecordsRouteTemplate(t *testing.T) {
	HTTPRequestsTotal.Reset()
	HTTPRequestDuration.Reset()

	r := newTestRouter(t)
	r.GET("/api/v1/cam/things/:id", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/cam/things/12345", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET 命中路由 = %d, want 200", w.Code)
	}

	if got := testutil.ToFloat64(HTTPRequestsTotal.WithLabelValues("GET", "/api/v1/cam/things/:id", "200")); got != 1 {
		t.Errorf("ecam_http_requests_total{route=模板} = %v, want 1", got)
	}
	// 原始路径不得成为标签：路由模板维度下应有且仅有 1 个子序列
	if got := testutil.CollectAndCount(HTTPRequestsTotal); got != 1 {
		t.Errorf("ecam_http_requests_total 子序列数 = %d, want 1（原始路径泄漏为标签）", got)
	}
	if got := testutil.CollectAndCount(HTTPRequestDuration); got < 1 {
		t.Errorf("ecam_http_request_duration_seconds 未采集到样本")
	}
}

// TestPrometheusMiddlewareRecordsStatus 验证状态码标签按真实响应记录。
func TestPrometheusMiddlewareRecordsStatus(t *testing.T) {
	HTTPRequestsTotal.Reset()

	r := newTestRouter(t)
	r.GET("/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/boom", func(c *gin.Context) { c.String(http.StatusInternalServerError, "boom") })

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ok", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/boom", nil))

	if got := testutil.ToFloat64(HTTPRequestsTotal.WithLabelValues("GET", "/ok", "200")); got != 1 {
		t.Errorf("status=200 计数 = %v, want 1", got)
	}
	if got := testutil.ToFloat64(HTTPRequestsTotal.WithLabelValues("GET", "/boom", "500")); got != 1 {
		t.Errorf("status=500 计数 = %v, want 1", got)
	}
}

// TestPrometheusMiddlewareUnmatchedRoute 验证未匹配路由（404）以固定
// "unmatched" 兜底，而非空串或原始路径。
func TestPrometheusMiddlewareUnmatchedRoute(t *testing.T) {
	HTTPRequestsTotal.Reset()

	r := newTestRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/nope/xyz", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("未匹配路由 = %d, want 404", w.Code)
	}

	if got := testutil.ToFloat64(HTTPRequestsTotal.WithLabelValues("GET", "unmatched", "404")); got != 1 {
		t.Errorf("ecam_http_requests_total{route=unmatched,status=404} = %v, want 1", got)
	}
	if got := testutil.CollectAndCount(HTTPRequestsTotal); got != 1 {
		t.Errorf("ecam_http_requests_total 子序列数 = %d, want 1（未匹配路由未归并）", got)
	}
}
