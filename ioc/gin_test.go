package ioc

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Havens-blog/e-cam-service/internal/metrics"
)

func TestOriginAllowed(t *testing.T) {
	allowed := []string{"http://localhost:8888", "*.jlcops.com"}

	tests := []struct {
		name     string
		origin   string
		list     []string
		allowAll bool
		want     bool
	}{
		{name: "精确匹配", origin: "http://localhost:8888", list: allowed, want: true},
		{name: "大小写不敏感", origin: "HTTP://LOCALHOST:8888", list: allowed, want: true},
		{name: "不在白名单", origin: "http://evil.example.com", list: allowed, want: false},
		{name: "通配子域命中", origin: "https://cam.jlcops.com", list: allowed, want: true},
		{name: "通配主域本身不命中", origin: "https://jlcops.com", list: allowed, want: false},
		{name: "仿冒后缀不命中", origin: "https://eviljlcops.com", list: allowed, want: false},
		{name: "allow_all 放行任意来源", origin: "http://anything.example", list: allowed, allowAll: true, want: true},
		{name: "空白名单拒绝", origin: "http://localhost:5173", list: []string{}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := originAllowed(tt.origin, tt.list, tt.allowAll); got != tt.want {
				t.Errorf("originAllowed(%q) = %v, want %v", tt.origin, got, tt.want)
			}
		})
	}
}

func TestOriginAllowedDefaults(t *testing.T) {
	// 默认白名单必须覆盖本地联调入口，否则未配置 cors 的部署会静默拒绝跨域
	for _, origin := range []string{
		"http://localhost:8888", "http://127.0.0.1:5173", "http://localhost:3333",
	} {
		if !originAllowed(origin, corsDefaultOrigins, false) {
			t.Errorf("默认白名单应放行 %q", origin)
		}
	}
}

// TestRegisterPublicRoutesServesMetrics 验证 /metrics 与健康检查同为免认证
// 基础路由，且 /metrics 实际暴露 ecam_* Prometheus 指标族。
func TestRegisterPublicRoutesServesMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(metrics.PrometheusMiddleware())
	registerPublicRoutes(r)

	// 先打一发真实请求：Prometheus *Vec 无子序列时不输出指标族，
	// 需让中间件先记录到样本，/metrics 才能暴露对应族
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/cam/health", nil))
	// 同步任务指标不经 HTTP，直接制造子序列（值 0 不影响断言语义）
	metrics.SyncTaskTotal.WithLabelValues("cam:__probe__", "success").Add(0)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /metrics = %d, want 200（免认证可达）", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ecam_http_requests_total") {
		t.Errorf("/metrics 响应未包含 ecam_http_requests_total 指标族")
	}
	if !strings.Contains(w.Body.String(), "ecam_sync_task_total") {
		t.Errorf("/metrics 响应未包含 ecam_sync_task_total 指标族")
	}

	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/v1/cam/health", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/cam/health = %d, want 200", w2.Code)
	}
}
