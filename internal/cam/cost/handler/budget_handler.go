package handler

import (
	"net/http"
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/budget"
	costdomain "github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"
	"github.com/Havens-blog/e-cam-service/pkg/ginx"
	"github.com/gin-gonic/gin"
)

// CreateBudgetReq 创建预算请求
type CreateBudgetReq struct {
	Name        string    `json:"name"`
	AmountLimit float64   `json:"amount_limit"`
	ScopeType   string    `json:"scope_type"`
	ScopeValue  string    `json:"scope_value"`
	Thresholds  []float64 `json:"thresholds"`
}

// UpdateBudgetReq 更新预算请求
type UpdateBudgetReq struct {
	Name        string    `json:"name"`
	AmountLimit float64   `json:"amount_limit"`
	ScopeType   string    `json:"scope_type"`
	ScopeValue  string    `json:"scope_value"`
	Thresholds  []float64 `json:"thresholds"`
}

// BudgetHandler 预算管理 API 处理器
type BudgetHandler struct {
	budgetSvc *budget.BudgetService
}

// NewBudgetHandler 创建预算管理处理器
func NewBudgetHandler(budgetSvc *budget.BudgetService) *BudgetHandler {
	return &BudgetHandler{budgetSvc: budgetSvc}
}

// PrivateRoutes 注册预算管理相关路由
func (h *BudgetHandler) PrivateRoutes(server *gin.Engine) {
	g := server.Group("/api/v1/cam")
	g.POST("/budget", ginx.WrapBody(h.CreateBudget))
	g.GET("/budget", h.ListBudgets)
	g.GET("/budget/:id/progress", h.GetBudgetProgress)
	g.PUT("/budget/:id", ginx.WrapBody(h.UpdateBudget))
	g.DELETE("/budget/:id", h.DeleteBudget)
}

// CreateBudget 创建预算规则
func (h *BudgetHandler) CreateBudget(ctx *gin.Context, req CreateBudgetReq) (ginx.Result, error) {
	tenantID := ctx.GetString("tenant_id")
	rule := costdomain.BudgetRule{
		Name:        req.Name,
		AmountLimit: req.AmountLimit,
		ScopeType:   req.ScopeType,
		ScopeValue:  req.ScopeValue,
		Thresholds:  req.Thresholds,
		TenantID:    tenantID,
	}

	id, err := h.budgetSvc.CreateBudget(ctx.Request.Context(), rule)
	if err != nil {
		return web.ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}
	return web.Result(gin.H{"id": id}), nil
}

// ListBudgets 预算规则列表
func (h *BudgetHandler) ListBudgets(ctx *gin.Context) {
	tenantID := ctx.GetString("tenant_id")
	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	filter := repository.BudgetFilter{
		TenantID:  tenantID,
		ScopeType: ctx.Query("scope_type"),
		Status:    ctx.Query("status"),
		Offset:    int64(offset),
		Limit:     int64(limit),
	}

	budgets, total, err := h.budgetSvc.ListBudgets(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(gin.H{
		"items": budgets,
		"total": total,
	}))
}

// GetBudgetProgress 预算消耗进度
func (h *BudgetHandler) GetBudgetProgress(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, web.ErrorResult(errs.ParamsError))
		return
	}

	progress, err := h.budgetSvc.GetBudgetProgress(ctx.Request.Context(), id)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(progress))
}

// UpdateBudget 更新预算规则
func (h *BudgetHandler) UpdateBudget(ctx *gin.Context, req UpdateBudgetReq) (ginx.Result, error) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return web.ErrorResult(errs.ParamsError), nil
	}

	tenantID := ctx.GetString("tenant_id")
	rule := costdomain.BudgetRule{
		ID:          id,
		Name:        req.Name,
		AmountLimit: req.AmountLimit,
		ScopeType:   req.ScopeType,
		ScopeValue:  req.ScopeValue,
		Thresholds:  req.Thresholds,
		TenantID:    tenantID,
	}

	if err := h.budgetSvc.UpdateBudget(ctx.Request.Context(), rule); err != nil {
		return web.ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}
	return web.Result(nil), nil
}

// DeleteBudget 删除预算规则
func (h *BudgetHandler) DeleteBudget(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, web.ErrorResult(errs.ParamsError))
		return
	}

	if err := h.budgetSvc.DeleteBudget(ctx.Request.Context(), id); err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(nil))
}
