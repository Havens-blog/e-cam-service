package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/analysis"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
)

// fakeSummaryReconciler 记录被请求刷新的租户与调用次数
type fakeSummaryReconciler struct {
	rebuilt   bool
	err       error
	calls     int
	gotTenant int64
}

func (f *fakeSummaryReconciler) ReconcileTenant(_ context.Context, tenantID int64) (bool, error) {
	f.calls++
	f.gotTenant = tenantID
	return f.rebuilt, f.err
}

// newRefreshTestHandler 构造仅注入对账能力的 CostHandler（其余服务不被该端点触碰）。
func newRefreshTestHandler(r analysis.SummaryReconciler) *CostHandler {
	svc := analysis.NewCostService(nil, nil, elog.DefaultLogger)
	if r != nil {
		svc.SetSummaryReconciler(r)
	}
	return &CostHandler{costSvc: svc}
}

func doRefreshRequest(t *testing.T, h *CostHandler, tenantID int64) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/cam/cost/summary/refresh", nil)
	c.Set(middleware.TenantIDKey, tenantID)
	h.RefreshCostSummary(c)
	return rec
}

// TestRefreshCostSummary 租户级成本汇总刷新入口（表驱动）。
func TestRefreshCostSummary(t *testing.T) {
	tests := []struct {
		name       string
		tenantID   int64
		rebuilt    bool
		reconErr   error
		wantStatus int
		wantCalls  int
		wantTenant int64
	}{
		{
			name:       "发生重建 - 200",
			tenantID:   3,
			rebuilt:    true,
			wantStatus: http.StatusOK,
			wantCalls:  1,
			wantTenant: 3,
		},
		{
			name:       "账期已一致 - 仍 200 且 rebuilt=false",
			tenantID:   3,
			rebuilt:    false,
			wantStatus: http.StatusOK,
			wantCalls:  1,
			wantTenant: 3,
		},
		{
			name:       "对账失败 - 500",
			tenantID:   3,
			reconErr:   errors.New("mongo unavailable"),
			wantStatus: http.StatusInternalServerError,
			wantCalls:  1,
			wantTenant: 3,
		},
		{
			name:       "会话未选定租户 - 403 且不触库",
			tenantID:   0,
			rebuilt:    true,
			wantStatus: http.StatusForbidden,
			wantCalls:  0,
			wantTenant: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeSummaryReconciler{rebuilt: tc.rebuilt, err: tc.reconErr}

			rec := doRefreshRequest(t, newRefreshTestHandler(f), tc.tenantID)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Equal(t, tc.wantCalls, f.calls, "tenant_id=0 必须在触库前被拒绝")
			if tc.wantCalls > 0 {
				assert.Equal(t, tc.wantTenant, f.gotTenant, "必须只刷新调用方租户")
			}
		})
	}
}

// TestRefreshCostSummary_NotInjected 未注入对账能力时返回 500，而不是 panic。
func TestRefreshCostSummary_NotInjected(t *testing.T) {
	rec := doRefreshRequest(t, newRefreshTestHandler(nil), 3)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
