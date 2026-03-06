package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/allocation"
	costdomain "github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"
	"github.com/Havens-blog/e-cam-service/pkg/ginx"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// CreateAllocationRuleReq 创建分摊规则请求
type CreateAllocationRuleReq struct {
	Name            string                      `json:"name"`
	RuleType        string                      `json:"rule_type"`
	DimensionCombos []costdomain.DimensionCombo `json:"dimension_combos"`
	TagKey          string                      `json:"tag_key"`
	TagValueMap     map[string]int64            `json:"tag_value_map"`
	SharedConfig    *costdomain.SharedConfig    `json:"shared_config"`
	Priority        int                         `json:"priority"`
}

// UpdateAllocationRuleReq 更新分摊规则请求
type UpdateAllocationRuleReq struct {
	Name            string                      `json:"name"`
	RuleType        string                      `json:"rule_type"`
	DimensionCombos []costdomain.DimensionCombo `json:"dimension_combos"`
	TagKey          string                      `json:"tag_key"`
	TagValueMap     map[string]int64            `json:"tag_value_map"`
	SharedConfig    *costdomain.SharedConfig    `json:"shared_config"`
	Priority        int                         `json:"priority"`
}

// SetDefaultPolicyReq 设置默认分摊策略请求
type SetDefaultPolicyReq struct {
	TargetID   string `json:"target_id"`
	TargetName string `json:"target_name"`
}

// ReAllocateReq 重新分摊请求
type ReAllocateReq struct {
	Period string `json:"period"`
}

// AllocationHandler 成本分摊 API 处理器
type AllocationHandler struct {
	allocationSvc *allocation.AllocationService
}

// NewAllocationHandler 创建成本分摊处理器
func NewAllocationHandler(allocationSvc *allocation.AllocationService) *AllocationHandler {
	return &AllocationHandler{allocationSvc: allocationSvc}
}

// PrivateRoutes 注册成本分摊相关路由
func (h *AllocationHandler) PrivateRoutes(server *gin.Engine) {
	g := server.Group("/api/v1/cam")
	g.POST("/allocation/rules", ginx.WrapBody(h.CreateAllocationRule))
	g.PUT("/allocation/rules/:id", ginx.WrapBody(h.UpdateAllocationRule))
	g.GET("/allocation/rules", h.ListAllocationRules)
	g.POST("/allocation/default-policy", ginx.WrapBody(h.SetDefaultPolicy))
	g.GET("/allocation/by-dimension", h.GetAllocationByDimension)
	g.GET("/allocation/by-node/:nodeId", h.GetAllocationByNode)
	g.GET("/allocation/tree", h.GetAllocationTree)
	g.POST("/allocation/reallocate", ginx.WrapBody(h.ReAllocateHistory))
}

// CreateAllocationRule 创建分摊规则
func (h *AllocationHandler) CreateAllocationRule(ctx *gin.Context, req CreateAllocationRuleReq) (ginx.Result, error) {
	tenantID := ctx.GetString("tenant_id")
	rule := costdomain.AllocationRule{
		Name:            req.Name,
		RuleType:        req.RuleType,
		DimensionCombos: req.DimensionCombos,
		TagKey:          req.TagKey,
		TagValueMap:     req.TagValueMap,
		SharedConfig:    req.SharedConfig,
		Priority:        req.Priority,
		TenantID:        tenantID,
	}

	id, err := h.allocationSvc.CreateAllocationRule(ctx.Request.Context(), rule)
	if err != nil {
		return web.ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}
	return web.Result(gin.H{"id": id}), nil
}

// UpdateAllocationRule 更新分摊规则
func (h *AllocationHandler) UpdateAllocationRule(ctx *gin.Context, req UpdateAllocationRuleReq) (ginx.Result, error) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return web.ErrorResult(errs.ParamsError), nil
	}

	tenantID := ctx.GetString("tenant_id")
	rule := costdomain.AllocationRule{
		ID:              id,
		Name:            req.Name,
		RuleType:        req.RuleType,
		DimensionCombos: req.DimensionCombos,
		TagKey:          req.TagKey,
		TagValueMap:     req.TagValueMap,
		SharedConfig:    req.SharedConfig,
		Priority:        req.Priority,
		TenantID:        tenantID,
	}

	if err := h.allocationSvc.UpdateAllocationRule(ctx.Request.Context(), rule); err != nil {
		return web.ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}
	return web.Result(nil), nil
}

// ListAllocationRules 分摊规则列表
func (h *AllocationHandler) ListAllocationRules(ctx *gin.Context) {
	tenantID := ctx.GetString("tenant_id")
	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	filter := repository.AllocationRuleFilter{
		TenantID: tenantID,
		RuleType: ctx.Query("rule_type"),
		Status:   ctx.Query("status"),
		Offset:   int64(offset),
		Limit:    int64(limit),
	}

	rules, err := h.allocationSvc.ListRules(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(gin.H{
		"items": rules,
	}))
}

// SetDefaultPolicy 设置默认分摊策略
func (h *AllocationHandler) SetDefaultPolicy(ctx *gin.Context, req SetDefaultPolicyReq) (ginx.Result, error) {
	tenantID := ctx.GetString("tenant_id")
	policy := costdomain.DefaultAllocationPolicy{
		TargetID:   req.TargetID,
		TargetName: req.TargetName,
		TenantID:   tenantID,
	}

	if err := h.allocationSvc.SetDefaultAllocationPolicy(ctx.Request.Context(), policy); err != nil {
		return web.ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}
	return web.Result(nil), nil
}

// GetAllocationByDimension 按维度查询分摊
func (h *AllocationHandler) GetAllocationByDimension(ctx *gin.Context) {
	tenantID := ctx.GetString("tenant_id")
	dimType := ctx.Query("dim_type")
	dimValue := ctx.Query("dim_value")
	period := ctx.Query("period")

	if dimType == "" || period == "" {
		ctx.JSON(http.StatusBadRequest, web.ErrorResult(errs.ParamsError))
		return
	}

	result, err := h.allocationSvc.GetAllocationByDimension(ctx.Request.Context(), tenantID, dimType, dimValue, period)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(result))
}

// GetAllocationByNode 按服务树节点查询分摊
func (h *AllocationHandler) GetAllocationByNode(ctx *gin.Context) {
	tenantID := ctx.GetString("tenant_id")
	nodeIDStr := ctx.Param("nodeId")
	nodeID, err := strconv.ParseInt(nodeIDStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, web.ErrorResult(errs.ParamsError))
		return
	}
	period := ctx.Query("period")
	if period == "" {
		ctx.JSON(http.StatusBadRequest, web.ErrorResult(errs.ParamsError))
		return
	}

	result, err := h.allocationSvc.GetAllocationByNode(ctx.Request.Context(), tenantID, nodeID, period)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(result))
}

// GetAllocationTree 维度层级分摊树形视图
func (h *AllocationHandler) GetAllocationTree(ctx *gin.Context) {
	tenantID := ctx.GetString("tenant_id")
	dimType := ctx.Query("dim_type")
	rootID := ctx.Query("root_id")
	period := ctx.Query("period")

	if dimType == "" || period == "" {
		ctx.JSON(http.StatusBadRequest, web.ErrorResult(errs.ParamsError))
		return
	}

	tree, err := h.allocationSvc.GetAllocationTree(ctx.Request.Context(), tenantID, dimType, rootID, period)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(tree))
}

// ReAllocateHistory 重新分摊历史数据
func (h *AllocationHandler) ReAllocateHistory(ctx *gin.Context, req ReAllocateReq) (ginx.Result, error) {
	tenantID := ctx.GetString("tenant_id")
	if req.Period == "" {
		return web.ErrorResult(errs.ParamsError), nil
	}

	// 异步执行分摊计算，避免 HTTP 超时
	go func() {
		bgCtx := context.Background()
		if err := h.allocationSvc.ReAllocateHistory(bgCtx, tenantID, req.Period); err != nil {
			h.allocationSvc.Logger().Warn("async ReAllocateHistory failed",
				elog.String("tenant_id", tenantID),
				elog.String("period", req.Period),
				elog.FieldErr(err))
		}
	}()

	return web.Result(gin.H{"message": "分摊计算已提交"}), nil
}
