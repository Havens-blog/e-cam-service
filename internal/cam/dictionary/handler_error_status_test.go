package dictionary

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
)

// stubDictService 只实现本用例需要的方法；其余方法由内嵌接口占位。
type stubDictService struct {
	DictService
	createTypeErr error
	deleteTypeErr error
}

//nolint:gocritic // hugeParam: 必须与 DictService.CreateType 的签名一致（值传递）
func (s *stubDictService) CreateType(_ context.Context, _ int64, _ CreateTypeReq) (DictType, error) {
	return DictType{}, s.createTypeErr
}

func (s *stubDictService) DeleteType(_ context.Context, _, _ int64) error {
	return s.deleteTypeErr
}

func newDictTestHandler(svc DictService) *DictHandler {
	gin.SetMode(gin.TestMode)
	return &DictHandler{svc: svc}
}

func dictTestCtx(method, target, body string, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set(middleware.TenantIDKey, int64(3))
	if params != nil {
		ctx.Params = params
	}
	return ctx, w
}

// 回归（2026-11-01）：本处理器此前 38 处错误响应全部写成 HTTP 200（含参数错误与 404/409 语义）
// ⇒ 只按 HTTP 判成功的客户端会把失败当成功。以下钉住状态码与业务码一致。
func TestDictHandlerParamErrorReturns400(t *testing.T) {
	h := newDictTestHandler(&stubDictService{})

	ctx, w := dictTestCtx(http.MethodPost, "/api/v1/cam/dict/types", "not-json", nil)
	h.CreateType(ctx)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应返回 400，实际 %d（此前为 200 + code 400）", w.Code)
	}
}

// 领域 not-found（404020）⇒ 404。
func TestDictHandlerDomainNotFoundReturns404(t *testing.T) {
	h := newDictTestHandler(&stubDictService{deleteTypeErr: errs.DictTypeNotFound})

	ctx, w := dictTestCtx(http.MethodDelete, "/api/v1/cam/dict/types/9", "", gin.Params{{Key: "id", Value: "9"}})
	h.DeleteType(ctx)

	if w.Code != http.StatusNotFound {
		t.Fatalf("领域 404020 应映射 404，实际 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"code":404020`) {
		t.Fatalf("响应体应含 404020，实际 %s", w.Body.String())
	}
}

// 领域冲突（409020）⇒ 409 —— 本 PR 为共享映射补的 409 段。
func TestDictHandlerDomainConflictReturns409(t *testing.T) {
	h := newDictTestHandler(&stubDictService{deleteTypeErr: errs.DictTypeCodeExists})

	ctx, w := dictTestCtx(http.MethodDelete, "/api/v1/cam/dict/types/9", "", gin.Params{{Key: "id", Value: "9"}})
	h.DeleteType(ctx)

	if w.Code != http.StatusConflict {
		t.Fatalf("领域 409020 应映射 409，实际 %d（修复前共享映射会把 409xxx 当 500）", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"code":409020`) {
		t.Fatalf("响应体应含 409020，实际 %s", w.Body.String())
	}
}

// 非领域错误：500 + **不得外泄 err.Error()**。
func TestDictHandlerGenericErrorLeakFree(t *testing.T) {
	h := newDictTestHandler(&stubDictService{createTypeErr: errors.New("mongo: no documents in result")})

	ctx, w := dictTestCtx(http.MethodPost, "/api/v1/cam/dict/types", `{"code":"x","name":"y"}`, nil)
	h.CreateType(ctx)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("非领域错误应返回 500，实际 %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "mongo") || strings.Contains(body, "no documents") {
		t.Fatalf("响应体不得外泄原始错误文本，实际 %s", body)
	}
}
