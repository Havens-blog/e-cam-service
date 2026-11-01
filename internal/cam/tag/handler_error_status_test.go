package tag

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"

	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
)

// stubTagService 只实现本用例需要的方法；其余方法由内嵌接口占位。
type stubTagService struct {
	TagService
	listTagsErr     error
	createPolicyErr error
	deletePolicyErr error
}

//nolint:gocritic // hugeParam: 必须与 TagService.ListTags 的签名一致（值传递）
func (s *stubTagService) ListTags(_ context.Context, _ int64, _ TagFilter) ([]TagSummary, int64, error) {
	if s.listTagsErr != nil {
		return nil, 0, s.listTagsErr
	}
	return nil, 0, nil
}

//nolint:gocritic // hugeParam: 必须与 TagService.CreatePolicy 的签名一致（值传递）
func (s *stubTagService) CreatePolicy(_ context.Context, _ int64, _ CreatePolicyReq) (TagPolicy, error) {
	return TagPolicy{}, s.createPolicyErr
}

func (s *stubTagService) DeletePolicy(_ context.Context, _, _ int64) error {
	return s.deletePolicyErr
}

func newTagTestHandler(svc TagService) *TagHandler {
	gin.SetMode(gin.TestMode)
	return &TagHandler{svc: svc, logger: elog.DefaultLogger}
}

func tagTestCtx(method, target, body string, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	ctx.Request = httptest.NewRequest(method, target, reader)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set(middleware.TenantIDKey, int64(3))
	if params != nil {
		ctx.Params = params
	}
	return ctx, w
}

// 回归（2026-11-01）：本处理器此前 47 处错误响应全部写成 HTTP 200（含参数错误与"不存在"）
// ⇒ 只按 HTTP 判成功的客户端会把失败当成功。以下钉住状态码与业务码一致。
func TestTagHandlerParamErrorReturns400(t *testing.T) {
	h := newTagTestHandler(&stubTagService{})

	ctx, w := tagTestCtx(http.MethodPost, "/api/v1/cam/tags/policies", "not-json", nil)
	h.CreatePolicy(ctx)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应返回 400，实际 %d（此前为 200 + code 400）", w.Code)
	}

	ctx2, w2 := tagTestCtx(http.MethodDelete, "/api/v1/cam/tags/policies/abc", "", gin.Params{{Key: "id", Value: "abc"}})
	h.DeletePolicy(ctx2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("非数字 id 应返回 400，实际 %d（此前为 200 + code 400）", w2.Code)
	}
}

// 领域校验错误（400030 ErrPolicyNameEmpty）⇒ 400。
func TestTagHandlerDomainValidationReturns400(t *testing.T) {
	h := newTagTestHandler(&stubTagService{createPolicyErr: ErrPolicyNameEmpty})

	ctx, w := tagTestCtx(http.MethodPost, "/api/v1/cam/tags/policies", `{"name":""}`, nil)
	h.CreatePolicy(ctx)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("领域 400032 应映射 400，实际 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"code":400032`) {
		t.Fatalf("响应体应含 400032，实际 %s", w.Body.String())
	}
}

// 领域 not-found（404030 ErrPolicyNotFound）⇒ 404。
func TestTagHandlerDomainNotFoundReturns404(t *testing.T) {
	h := newTagTestHandler(&stubTagService{deletePolicyErr: ErrPolicyNotFound})

	ctx, w := tagTestCtx(http.MethodDelete, "/api/v1/cam/tags/policies/9", "", gin.Params{{Key: "id", Value: "9"}})
	h.DeletePolicy(ctx)

	if w.Code != http.StatusNotFound {
		t.Fatalf("领域 404030 应映射 404，实际 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"code":404030`) {
		t.Fatalf("响应体应含 404030，实际 %s", w.Body.String())
	}
}

// 非领域错误：500 + **不得外泄 err.Error()**。
func TestTagHandlerGenericErrorLeakFree(t *testing.T) {
	h := newTagTestHandler(&stubTagService{listTagsErr: errors.New("mongo: no documents in result")})

	ctx, w := tagTestCtx(http.MethodGet, "/api/v1/cam/tags", "", nil)
	h.ListTags(ctx)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("非领域错误应返回 500，实际 %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "mongo") || strings.Contains(body, "no documents") {
		t.Fatalf("响应体不得外泄原始错误文本，实际 %s", body)
	}
}

// 领域 not-found 经由 service 返回时同样映射 404（证明段位映射对任意标签领域码生效）。
func TestTagHandlerServiceDomainNotFoundReturns404(t *testing.T) {
	h := newTagTestHandler(&stubTagService{listTagsErr: ErrRuleNotFound})

	ctx, w := tagTestCtx(http.MethodGet, "/api/v1/cam/tags", "", nil)
	h.ListTags(ctx)

	if w.Code != http.StatusNotFound {
		t.Fatalf("领域 404040 应映射 404，实际 %d", w.Code)
	}
}
