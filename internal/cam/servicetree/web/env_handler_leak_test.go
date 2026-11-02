package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/service"
)

// leakStubEnvironmentService 只实现 GetByID（其余由内嵌接口占位）。
// 该方法只收 int64 ⇒ 不会触发 gocritic 的 hugeParam，无需 nolint。
type leakStubEnvironmentService struct {
	service.EnvironmentService
	err error
}

func (s *leakStubEnvironmentService) GetByID(_ context.Context, _ int64) (domain.Environment, error) {
	return domain.Environment{}, s.err
}

// 回归（2026-11-02）：servicetree 的 34 处错误分支此前写作
// `return ginx.Result{Code: 500, Msg: err.Error()}, nil` —— 既回原文，又因**返回 nil**
// 被 ginx 包装器判为"无错误" ⇒ **HTTP 200 + code 500**（失败被当成成功）。
// 现改为返回**非 nil error**（框架据此给 500）+ 稳定文案。
func TestEnvHandlerReturnsErrorOnServiceFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewEnvHandler(&leakStubEnvironmentService{err: errors.New("mongo: no documents in result")})

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/cam/service-tree/environments/1", http.NoBody)
	ctx.Params = gin.Params{{Key: "id", Value: "1"}}

	res, err := h.GetEnv(ctx)

	if err == nil {
		t.Fatal("查询失败必须返回非 nil error（此前返回 nil ⇒ 框架会回 HTTP 200 + code 500）")
	}
	if strings.Contains(res.Msg, "mongo") || strings.Contains(res.Msg, "no documents") {
		t.Fatalf("响应体不得外泄原始错误文本，实际 %q", res.Msg)
	}
	if res.Code != 500 {
		t.Fatalf("响应体业务码应为 500，实际 %d", res.Code)
	}
}
