package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
)

// leakStubDashboardService 只实现 GetOverview（其余由内嵌接口占位）；
// 该方法只收 int64 参数，不会触发 gocritic 的 hugeParam。
type leakStubDashboardService struct {
	service.DashboardService
	err error
}

func (s *leakStubDashboardService) GetOverview(_ context.Context, _ int64) (*service.DashboardOverview, error) {
	return nil, s.err
}

// 回归（2026-11-02）：dashboard 处理器曾把 err.Error() 直接写进 500 响应体（6 处），
// 现改为稳定文案 + 细节进日志。仪表盘是首屏，其统计接口的报错文案对用户可见。
func TestDashboardHandlerDoesNotLeakRawError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewDashboardHandler(&leakStubDashboardService{err: errors.New("mongo: no documents in result")})

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/cam/dashboard/overview", http.NoBody)
	ctx.Set(middleware.TenantIDKey, int64(3))

	h.Overview(ctx)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("统计失败应返回 500，实际 %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "mongo") || strings.Contains(body, "no documents") {
		t.Fatalf("响应体不得外泄原始错误文本，实际 %s", body)
	}
	if !strings.Contains(body, `"code":500`) {
		t.Fatalf("响应体应保留业务码 500，实际 %s", body)
	}
}
