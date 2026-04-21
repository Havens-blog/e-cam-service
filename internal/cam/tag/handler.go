package tag

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/middleware"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"
	"github.com/gin-gonic/gin"
)

// TagHandler 标签管理 HTTP 处理器
type TagHandler struct {
	svc TagService
}

// NewTagHandler 创建标签处理器
func NewTagHandler(svc TagService) *TagHandler {
	return &TagHandler{svc: svc}
}

// RegisterRoutes 注册标签路由
func (h *TagHandler) RegisterRoutes(g *gin.RouterGroup) {
	tags := g.Group("/tags")
	// 标签列表与统计
	tags.GET("", h.ListTags)
	tags.GET("/stats", h.GetTagStats)
	tags.GET("/resources", h.ListTagResources)
	// 标签操作
	tags.POST("/bindResource", h.BindResource)
	tags.POST("/unbindResource", h.UnbindResource)
	// 标签策略
	tags.POST("/policies", h.CreatePolicy)
	tags.GET("/policies", h.ListPolicies)
	tags.PUT("/policies/:id", h.UpdatePolicy)
	tags.DELETE("/policies/:id", h.DeletePolicy)
	tags.GET("/compliance", h.CheckCompliance)
	// 自动打标规则
	tags.POST("/rules", h.CreateRule)
	tags.GET("/rules", h.ListRules)
	tags.PUT("/rules/:id", h.UpdateRule)
	tags.DELETE("/rules/:id", h.DeleteRule)
	tags.POST("/rules/preview", h.PreviewRules)
	tags.POST("/rules/execute", h.ExecuteRules)
}

// ==================== 标签列表与统计 ====================

// ListTags 查询标签列表
func (h *TagHandler) ListTags(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	offset, _ := strconv.ParseInt(ctx.DefaultQuery("offset", "0"), 10, 64)
	limit, _ := strconv.ParseInt(ctx.DefaultQuery("limit", "20"), 10, 64)
	accountID, _ := strconv.ParseInt(ctx.DefaultQuery("account_id", "0"), 10, 64)

	filter := TagFilter{
		Key:          ctx.Query("key"),
		Value:        ctx.Query("value"),
		Provider:     ctx.Query("provider"),
		AccountID:    accountID,
		ResourceType: ctx.Query("resource_type"),
		Offset:       offset,
		Limit:        limit,
	}

	items, total, err := h.svc.ListTags(ctx.Request.Context(), tenantID, filter)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(gin.H{
		"items": items,
		"total": total,
	}))
}

// GetTagStats 查询标签统计
func (h *TagHandler) GetTagStats(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	stats, err := h.svc.GetTagStats(ctx.Request.Context(), tenantID)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(stats))
}

// ListTagResources 查询标签关联资源
func (h *TagHandler) ListTagResources(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	key := ctx.Query("key")
	if key == "" {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "key is required"))
		return
	}

	offset, _ := strconv.ParseInt(ctx.DefaultQuery("offset", "0"), 10, 64)
	limit, _ := strconv.ParseInt(ctx.DefaultQuery("limit", "20"), 10, 64)

	filter := TagResourceFilter{
		Key:          key,
		Value:        ctx.Query("value"),
		Provider:     ctx.Query("provider"),
		ResourceType: ctx.Query("resource_type"),
		Offset:       offset,
		Limit:        limit,
	}

	items, total, err := h.svc.ListTagResources(ctx.Request.Context(), tenantID, filter)
	if err != nil {
		if errors.Is(err, ErrTagKeyEmpty) {
			ctx.JSON(http.StatusOK, web.ErrorResult(ErrTagKeyEmpty))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(gin.H{
		"items": items,
		"total": total,
	}))
}

// ==================== 标签操作 ====================

// BindResource 绑定标签
func (h *TagHandler) BindResource(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	var req BindTagsReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}

	if len(req.Resources) == 0 {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "resources list cannot be empty"))
		return
	}
	if len(req.Tags) == 0 {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "tags cannot be empty"))
		return
	}

	result, err := h.svc.BindTags(ctx.Request.Context(), tenantID, req)
	if err != nil {
		if errors.Is(err, ErrTagKeyEmpty) {
			ctx.JSON(http.StatusOK, web.ErrorResult(ErrTagKeyEmpty))
			return
		}
		if errors.Is(err, ErrTagValueEmpty) {
			ctx.JSON(http.StatusOK, web.ErrorResult(ErrTagValueEmpty))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(result))
}

// UnbindResource 解绑标签
func (h *TagHandler) UnbindResource(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	var req UnbindTagsReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}

	if len(req.Resources) == 0 {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "resources list cannot be empty"))
		return
	}
	if len(req.TagKeys) == 0 {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "tag_keys cannot be empty"))
		return
	}

	result, err := h.svc.UnbindTags(ctx.Request.Context(), tenantID, req)
	if err != nil {
		if errors.Is(err, ErrTagKeyEmpty) {
			ctx.JSON(http.StatusOK, web.ErrorResult(ErrTagKeyEmpty))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(result))
}

// ==================== 标签策略 ====================

// CreatePolicy 创建标签策略
func (h *TagHandler) CreatePolicy(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	var req CreatePolicyReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}

	policy, err := h.svc.CreatePolicy(ctx.Request.Context(), tenantID, req)
	if err != nil {
		if errors.Is(err, ErrPolicyNameEmpty) {
			ctx.JSON(http.StatusOK, web.ErrorResult(ErrPolicyNameEmpty))
			return
		}
		if errors.Is(err, ErrPolicyKeysEmpty) {
			ctx.JSON(http.StatusOK, web.ErrorResult(ErrPolicyKeysEmpty))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(policy))
}

// ListPolicies 查询标签策略列表
func (h *TagHandler) ListPolicies(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	offset, _ := strconv.ParseInt(ctx.DefaultQuery("offset", "0"), 10, 64)
	limit, _ := strconv.ParseInt(ctx.DefaultQuery("limit", "20"), 10, 64)

	filter := PolicyFilter{
		Offset: offset,
		Limit:  limit,
	}

	items, total, err := h.svc.ListPolicies(ctx.Request.Context(), tenantID, filter)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(gin.H{
		"items": items,
		"total": total,
	}))
}

// UpdatePolicy 更新标签策略
func (h *TagHandler) UpdatePolicy(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid id"))
		return
	}

	var req UpdatePolicyReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}

	if err := h.svc.UpdatePolicy(ctx.Request.Context(), tenantID, id, req); err != nil {
		if errors.Is(err, ErrPolicyNotFound) {
			ctx.JSON(http.StatusOK, web.ErrorResult(ErrPolicyNotFound))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(nil))
}

// DeletePolicy 删除标签策略
func (h *TagHandler) DeletePolicy(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid id"))
		return
	}

	if err := h.svc.DeletePolicy(ctx.Request.Context(), tenantID, id); err != nil {
		if errors.Is(err, ErrPolicyNotFound) {
			ctx.JSON(http.StatusOK, web.ErrorResult(ErrPolicyNotFound))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(nil))
}

// CheckCompliance 合规检查
func (h *TagHandler) CheckCompliance(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	policyIDStr := ctx.Query("policy_id")
	if policyIDStr == "" {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "policy_id is required"))
		return
	}
	policyID, err := strconv.ParseInt(policyIDStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid policy_id"))
		return
	}

	offset, _ := strconv.ParseInt(ctx.DefaultQuery("offset", "0"), 10, 64)
	limit, _ := strconv.ParseInt(ctx.DefaultQuery("limit", "100"), 10, 64)

	filter := ComplianceFilter{
		PolicyID:     policyID,
		ResourceType: ctx.Query("resource_type"),
		Provider:     ctx.Query("provider"),
		Offset:       offset,
		Limit:        limit,
	}

	results, total, err := h.svc.CheckCompliance(ctx.Request.Context(), tenantID, filter)
	if err != nil {
		if errors.Is(err, ErrPolicyNotFound) {
			ctx.JSON(http.StatusOK, web.ErrorResult(ErrPolicyNotFound))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, web.Result(gin.H{
		"items":               results,
		"total":               total,
		"non_compliant_count": total,
	}))
}

// ==================== 自动打标规则 ====================

// CreateRule 创建自动打标规则
func (h *TagHandler) CreateRule(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	var req CreateRuleReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}
	rule, err := h.svc.CreateRule(ctx.Request.Context(), tenantID, req)
	if err != nil {
		if errors.Is(err, ErrRuleNameEmpty) || errors.Is(err, ErrRuleNoCondition) || errors.Is(err, ErrRuleNoTags) {
			ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, err.Error()))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(rule))
}

// ListRules 查询自动打标规则列表
func (h *TagHandler) ListRules(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	offset, _ := strconv.ParseInt(ctx.DefaultQuery("offset", "0"), 10, 64)
	limit, _ := strconv.ParseInt(ctx.DefaultQuery("limit", "20"), 10, 64)
	items, total, err := h.svc.ListRules(ctx.Request.Context(), tenantID, RuleFilter{Offset: offset, Limit: limit})
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(gin.H{"items": items, "total": total}))
}

// UpdateRule 更新自动打标规则
func (h *TagHandler) UpdateRule(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid id"))
		return
	}
	var req UpdateRuleReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}
	if err := h.svc.UpdateRule(ctx.Request.Context(), tenantID, id, req); err != nil {
		if errors.Is(err, ErrRuleNotFound) {
			ctx.JSON(http.StatusOK, web.ErrorResult(ErrRuleNotFound))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(nil))
}

// DeleteRule 删除自动打标规则
func (h *TagHandler) DeleteRule(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid id"))
		return
	}
	if err := h.svc.DeleteRule(ctx.Request.Context(), tenantID, id); err != nil {
		if errors.Is(err, ErrRuleNotFound) {
			ctx.JSON(http.StatusOK, web.ErrorResult(ErrRuleNotFound))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(nil))
}

// PreviewRules 预览规则匹配结果
func (h *TagHandler) PreviewRules(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	var body struct {
		RuleIDs []int64 `json:"rule_ids"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}
	results, err := h.svc.PreviewRules(ctx.Request.Context(), tenantID, body.RuleIDs)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(gin.H{"items": results}))
}

// ExecuteRules 执行自动打标规则
func (h *TagHandler) ExecuteRules(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	var body struct {
		RuleIDs []int64 `json:"rule_ids"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}
	results, err := h.svc.ExecuteRules(ctx.Request.Context(), tenantID, body.RuleIDs)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(gin.H{"items": results}))
}
