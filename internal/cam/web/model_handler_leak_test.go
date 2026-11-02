package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
)

// leakStubModelService 只实现 GetModel（其余由内嵌接口占位）。
// 该方法只收 string ⇒ 不会触发 gocritic 的 hugeParam，无需 nolint。
type leakStubModelService struct {
	service.ModelService
	err error
}

func (s *leakStubModelService) GetModel(_ context.Context, _ string) (*domain.ModelDetail, error) {
	return nil, s.err
}

// 回归（2026-11-02）：model_handler/handler_assets/asset_handler_edge 三文件共 29 处
// 曾把 err.Error() 直接写进响应体（含 404/域码分支与"返回 nil 的包装器形态"）。
// 现统一为稳定文案，细节进日志。
func TestModelHandlerDoesNotLeakRawError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{modelSvc: &leakStubModelService{err: errors.New("mongo: no documents in result")}}

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/cam/models/ecs", http.NoBody)
	ctx.Params = gin.Params{{Key: "uid", Value: "ecs"}}

	h.GetModel(ctx)

	if w.Code != http.StatusNotFound {
		t.Fatalf("模型不存在分支应返回 404，实际 %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "mongo") || strings.Contains(body, "no documents") {
		t.Fatalf("响应体不得外泄原始错误文本，实际 %s", body)
	}
	if !strings.Contains(body, `"code":404`) {
		t.Fatalf("响应体应保留领域业务码 404 段，实际 %s", body)
	}
}
