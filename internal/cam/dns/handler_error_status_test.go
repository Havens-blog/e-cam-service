package dns

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
)

// stubDNSService 只实现本用例需要的 BatchDeleteRecords；其余方法由内嵌接口占位。
type stubDNSService struct {
	DNSService
	err error
}

func (s *stubDNSService) BatchDeleteRecords(_ context.Context, _ int64, _ string, _ []string) (*BatchDeleteResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &BatchDeleteResult{}, nil
}

func batchDeleteCtx(body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost,
		"/api/v1/cam/dns/domains/example.com/records/batch-delete", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set(middleware.TenantIDKey, int64(3))
	ctx.Params = gin.Params{{Key: "domain", Value: "example.com"}}
	return ctx, w
}

// 回归（2026-11-01）：本处理器此前把所有错误响应都写成 HTTP 200（含参数错误）
// ⇒ 只按 HTTP 判成功的客户端会把"参数错"当成成功。此处钉住状态码与业务码一致。
func TestBatchDeleteRecordsParamErrorReturns400(t *testing.T) {
	h := NewDNSHandler(&stubDNSService{})

	ctx, w := batchDeleteCtx(`{"record_ids":[]}`)
	h.BatchDeleteRecords(ctx)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("空 record_ids 应返回 400，实际 %d（此前为 200 + code 400）", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"code":400`) {
		t.Fatalf("响应体应保留业务码 400，实际 %s", w.Body.String())
	}
}

func TestBatchDeleteRecordsInvalidBodyReturns400(t *testing.T) {
	h := NewDNSHandler(&stubDNSService{})

	ctx, w := batchDeleteCtx(`not-json`)
	h.BatchDeleteRecords(ctx)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应返回 400，实际 %d", w.Code)
	}
}

// 领域 not-found（404050）应由 handleError 映射为 HTTP 404。
func TestBatchDeleteRecordsDomainNotFoundReturns404(t *testing.T) {
	h := NewDNSHandler(&stubDNSService{err: ErrDNSAccountNotFound})

	ctx, w := batchDeleteCtx(`{"record_ids":["1"]}`)
	h.BatchDeleteRecords(ctx)

	if w.Code != http.StatusNotFound {
		t.Fatalf("领域 404050 应映射为 404，实际 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"code":404050`) {
		t.Fatalf("响应体应含 404050，实际 %s", w.Body.String())
	}
}

// 非领域错误：500 + **不得外泄 err.Error()**（可能含 "mongo: no documents in result" 等存储细节）。
func TestBatchDeleteRecordsGenericErrorLeakFree(t *testing.T) {
	h := NewDNSHandler(&stubDNSService{err: errors.New("mongo: no documents in result")})

	ctx, w := batchDeleteCtx(`{"record_ids":["1"]}`)
	h.BatchDeleteRecords(ctx)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("非领域错误应返回 500，实际 %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "mongo") || strings.Contains(body, "no documents") {
		t.Fatalf("响应体不得外泄原始错误文本，实际 %s", body)
	}
}
