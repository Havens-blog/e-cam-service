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

// leakStubInstanceService 只实现 GetByID（其余由内嵌接口占位）。
// 该方法只收 int64 ⇒ 不会触发 gocritic 的 hugeParam，无需 nolint。
type leakStubInstanceService struct {
	service.InstanceService
	err error
}

func (s *leakStubInstanceService) GetByID(_ context.Context, _ int64) (domain.Instance, error) {
	return domain.Instance{}, s.err
}

// 回归（2026-11-02）：账号/实例/数据库三个处理器共 20 处把 err.Error() 直接写进响应体
// （11 处 500 直写、9 处"返回 nil 的包装器形态"⇒ 后者此前还会回 HTTP 200）。现统一为稳定文案。
func TestInstanceHandlerDoesNotLeakRawError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewInstanceHandler(&leakStubInstanceService{err: errors.New("mongo: no documents in result")})

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/cam/instances/1", http.NoBody)
	ctx.Params = gin.Params{{Key: "id", Value: "1"}}

	h.GetByID(ctx)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("详情失败应返回 500，实际 %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "mongo") || strings.Contains(body, "no documents") {
		t.Fatalf("响应体不得外泄原始错误文本，实际 %s", body)
	}
	if !strings.Contains(body, `"code":500`) {
		t.Fatalf("响应体应保留业务码 500，实际 %s", body)
	}
}
