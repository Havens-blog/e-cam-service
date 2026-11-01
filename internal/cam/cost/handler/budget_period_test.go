package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/budget"
	costdomain "github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
)

// stubBudgetDAO 只实现本用例需要的方法；其余方法由内嵌接口占位（被调用即 panic，能暴露误用）。
type stubBudgetDAO struct {
	repository.BudgetDAO
	created costdomain.BudgetRule
	updated costdomain.BudgetRule
}

//nolint:gocritic // hugeParam: 必须与 repository.BudgetDAO.Create 的签名一致（值传递）
func (s *stubBudgetDAO) Create(_ context.Context, b costdomain.BudgetRule) (int64, error) {
	s.created = b
	return 7, nil
}

//nolint:gocritic // hugeParam: 必须与 repository.BudgetDAO.Update 的签名一致（值传递）
func (s *stubBudgetDAO) Update(_ context.Context, b costdomain.BudgetRule) error {
	s.updated = b
	return nil
}

func newBudgetTestHandler() (*BudgetHandler, *stubBudgetDAO) {
	gin.SetMode(gin.TestMode)
	dao := &stubBudgetDAO{}
	svc := budget.NewBudgetService(dao, nil, nil, elog.DefaultLogger)
	return NewBudgetHandler(svc), dao
}

func budgetTestCtx(id string) *gin.Context {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/cam/budget", http.NoBody)
	ctx.Set(middleware.TenantIDKey, int64(3))
	if id != "" {
		ctx.Params = gin.Params{{Key: "id", Value: id}}
	}
	return ctx
}

// 回归（2026-11-01）：前端预算表单有「周期」选择、域模型 BudgetRule 与库表都有 period，
// 但请求结构曾漏掉该字段 ⇒ 用户选的周期被**静默丢弃**。此处钉住 handler 的透传。
func TestCreateBudgetTransparesPeriod(t *testing.T) {
	h, dao := newBudgetTestHandler()
	if _, err := h.CreateBudget(budgetTestCtx(""), CreateBudgetReq{
		Name:        "月预算",
		AmountLimit: 1000,
		Period:      "quarter",
		ScopeType:   "provider",
		ScopeValue:  "aliyun",
		Thresholds:  []float64{80, 100},
	}); err != nil {
		t.Fatalf("CreateBudget 返回错误: %v", err)
	}
	if dao.created.Period != "quarter" {
		t.Fatalf("Period 未透传到 BudgetRule：得到 %q，期望 %q", dao.created.Period, "quarter")
	}
	if dao.created.TenantID != 3 {
		t.Fatalf("TenantID 应来自会话：得到 %d", dao.created.TenantID)
	}
}

func TestUpdateBudgetTransparesPeriod(t *testing.T) {
	h, dao := newBudgetTestHandler()
	if _, err := h.UpdateBudget(budgetTestCtx("7"), UpdateBudgetReq{
		Name:        "月预算",
		AmountLimit: 2000,
		Period:      "year",
		ScopeType:   "account",
		ScopeValue:  "1",
		Thresholds:  []float64{90},
	}); err != nil {
		t.Fatalf("UpdateBudget 返回错误: %v", err)
	}
	if dao.updated.Period != "year" {
		t.Fatalf("Period 未透传到 BudgetRule：得到 %q，期望 %q", dao.updated.Period, "year")
	}
	if dao.updated.ID != 7 {
		t.Fatalf("ID 应为路径参数：得到 %d", dao.updated.ID)
	}
}
