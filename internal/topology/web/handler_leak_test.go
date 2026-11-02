package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-cam-service/internal/topology/domain"
	"github.com/Havens-blog/e-cam-service/internal/topology/service"
)

// leakStubTopologyService 只实现 GetNodeDetail（其余由内嵌接口占位）。
type leakStubTopologyService struct {
	service.TopologyService
	err error
}

func (s *leakStubTopologyService) GetNodeDetail(_ context.Context, _ int64, _ string) (*domain.NodeDetail, error) {
	return nil, s.err
}

// 回归（2026-11-02）：topology 处理器曾把 err.Error() 直接写进响应体（7 处：
// 400 查询绑定失败、404 节点详情、5 处 500），现只用稳定文案，细节进日志。
func TestTopologyHandlerDoesNotLeakRawError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewTopologyHandler(&leakStubTopologyService{err: errors.New("mongo: no documents in result")}, nil)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/cam/topology/nodes/n1", http.NoBody)
	ctx.Params = gin.Params{{Key: "nodeID", Value: "n1"}, {Key: "id", Value: "n1"}}
	ctx.Set(middleware.TenantIDKey, int64(3))

	h.GetNodeDetail(ctx)

	if w.Code != http.StatusNotFound {
		t.Fatalf("节点详情失败应返回 404，实际 %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "mongo") || strings.Contains(body, "no documents") {
		t.Fatalf("响应体不得外泄原始错误文本，实际 %s", body)
	}
	if !strings.Contains(body, "topology node not found") {
		t.Fatalf("响应体应含稳定文案，实际 %s", body)
	}
}
