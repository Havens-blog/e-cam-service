package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

// dry-run handler 单测：条件为空 → 400；正常条件 → 200 返回命中清单。

type stubRuleEngineForDryRun struct {
	service.RuleEngineService
	dryRunFn func(ctx context.Context, tenantID int64, req domain.DryRunRequest) (*domain.DryRunResult, error)
}

func (s *stubRuleEngineForDryRun) DryRunRules(ctx context.Context, tenantID int64, req domain.DryRunRequest) (*domain.DryRunResult, error) {
	return s.dryRunFn(ctx, tenantID, req)
}

func newDryRunTestRouter(t *testing.T, stub *stubRuleEngineForDryRun) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.TenantIDKey, int64(1))
		c.Next()
	})
	h := NewHandler(nil, nil, stub, nil)
	h.RegisterRuleRoutes(router.Group("/service-tree"))
	return router
}

func doDryRunRequest(t *testing.T, router *gin.Engine, body string) (int, ginxCodeMsgData) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/service-tree/rules/dry-run", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var resp ginxCodeMsgData
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v, body=%s", err, w.Body.String())
	}
	return w.Code, resp
}

type ginxCodeMsgData struct {
	Code int                  `json:"code"`
	Msg  string               `json:"msg"`
	Data *domain.DryRunResult `json:"data"`
}

// TestDryRunRulesHandlerEmptyConditions 条件为空返回 400（不触发服务层）
func TestDryRunRulesHandlerEmptyConditions(t *testing.T) {
	called := false
	stub := &stubRuleEngineForDryRun{
		dryRunFn: func(ctx context.Context, tenantID int64, req domain.DryRunRequest) (*domain.DryRunResult, error) {
			called = true
			return &domain.DryRunResult{}, nil
		},
	}
	router := newDryRunTestRouter(t, stub)

	httpCode, resp := doDryRunRequest(t, router, `{}`)

	if called {
		t.Error("条件为空不应进入服务层")
	}
	if resp.Code != 400 {
		t.Errorf("in-body code = %d, want 400, msg=%q", resp.Code, resp.Msg)
	}
	_ = httpCode
}

// TestDryRunRulesHandlerHit 正常条件走服务层并返回命中清单
func TestDryRunRulesHandlerHit(t *testing.T) {
	var gotTenant int64
	var gotReq domain.DryRunRequest
	stub := &stubRuleEngineForDryRun{
		dryRunFn: func(ctx context.Context, tenantID int64, req domain.DryRunRequest) (*domain.DryRunResult, error) {
			gotTenant = tenantID
			gotReq = req
			return &domain.DryRunResult{
				Items: []domain.DryRunMatchItem{{
					ResourceID: 7, AssetID: "i-web-01", AssetName: "web-01",
					Provider: "aliyun", Region: "cn-hangzhou", BindStatus: domain.BindStatusUnbound,
				}},
				Total: 1,
			}, nil
		},
	}
	router := newDryRunTestRouter(t, stub)

	_, resp := doDryRunRequest(t, router, `{"env_id":5,"conditions":[{"field":"name","operator":"contains","value":"web"}]}`)

	if gotTenant != 1 {
		t.Errorf("tenantID = %d, want 1", gotTenant)
	}
	if gotReq.EnvID != 5 || len(gotReq.Conditions) != 1 {
		t.Errorf("请求透传不符: %+v", gotReq)
	}
	if resp.Code != 0 {
		t.Errorf("in-body code = %d, want 0, msg=%q", resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.Total != 1 || len(resp.Data.Items) != 1 {
		t.Fatalf("data 不符: %+v", resp.Data)
	}
	if resp.Data.Items[0].BindStatus != domain.BindStatusUnbound {
		t.Errorf("bind_status = %q, want %q", resp.Data.Items[0].BindStatus, domain.BindStatusUnbound)
	}
}
