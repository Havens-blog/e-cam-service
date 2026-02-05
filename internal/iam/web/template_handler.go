package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/iam/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

type TemplateHandler struct {
	templateService service.PolicyTemplateService
	logger          *elog.Component
}

func NewTemplateHandler(templateService service.PolicyTemplateService, logger *elog.Component) *TemplateHandler {
	return &TemplateHandler{
		templateService: templateService,
		logger:          logger,
	}
}

// CreateTemplate 创建策略模板
// @Summary 创建策略模板
// @Tags 策略模板管理
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param body body CreateTemplateVO true "创建策略模板请求"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/templates [post]
func (h *TemplateHandler) CreateTemplate(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var req CreateTemplateVO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, Error(err))
		return
	}

	template, err := h.templateService.CreateTemplate(c.Request.Context(), &domain.CreateTemplateRequest{
		Name:           req.Name,
		Description:    req.Description,
		Category:       req.Category,
		Policies:       req.Policies,
		CloudPlatforms: req.CloudPlatforms,
		TenantID:       tenantID,
	})

	if err != nil {
		h.logger.Error("创建策略模板失败", elog.String("tenant_id", tenantID), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(template))
}

// GetTemplate 获取策略模板详情
// @Summary 获取策略模板详情
// @Tags 策略模板管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "模板ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/templates/{id} [get]
func (h *TemplateHandler) GetTemplate(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	template, err := h.templateService.GetTemplate(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("获取策略模板失败", elog.String("tenant_id", tenantID), elog.Int64("template_id", id), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(template))
}

// ListTemplates 查询策略模板列表
// @Summary 查询策略模板列表
// @Tags 策略模板管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param category query string false "模板分类"
// @Param is_built_in query bool false "是否内置"
// @Param keyword query string false "关键词"
// @Param page query int false "页码"
// @Param size query int false "每页数量"
// @Success 200 {object} PageResult
// @Router /api/v1/cam/iam/templates [get]
func (h *TemplateHandler) ListTemplates(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var req ListTemplatesVO
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(400, Error(err))
		return
	}

	if req.Page == 0 {
		req.Page = 1
	}
	if req.Size == 0 {
		req.Size = 20
	}

	offset := (req.Page - 1) * req.Size

	templates, total, err := h.templateService.ListTemplates(c.Request.Context(), domain.TemplateFilter{
		Category:  req.Category,
		IsBuiltIn: req.IsBuiltIn,
		TenantID:  tenantID,
		Keyword:   req.Keyword,
		Offset:    offset,
		Limit:     req.Size,
	})

	if err != nil {
		h.logger.Error("查询策略模板列表失败", elog.String("tenant_id", tenantID), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, PageSuccess(templates, total, req.Page, req.Size))
}

// UpdateTemplate 更新策略模板
// @Summary 更新策略模板信息
// @Tags 策略模板管理
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "模板ID"
// @Param body body UpdateTemplateVO true "更新策略模板请求"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/templates/{id} [put]
func (h *TemplateHandler) UpdateTemplate(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	var req UpdateTemplateVO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, Error(err))
		return
	}

	err = h.templateService.UpdateTemplate(c.Request.Context(), id, &domain.UpdateTemplateRequest{
		Name:           req.Name,
		Description:    req.Description,
		Policies:       req.Policies,
		CloudPlatforms: req.CloudPlatforms,
	})

	if err != nil {
		h.logger.Error("更新策略模板失败", elog.String("tenant_id", tenantID), elog.Int64("template_id", id), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, SuccessWithMsg("更新成功", nil))
}

// DeleteTemplate 删除策略模板
// @Summary 删除策略模板
// @Tags 策略模板管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "模板ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/templates/{id} [delete]
func (h *TemplateHandler) DeleteTemplate(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	err = h.templateService.DeleteTemplate(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("删除策略模板失败", elog.String("tenant_id", tenantID), elog.Int64("template_id", id), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, SuccessWithMsg("删除成功", nil))
}

// CreateFromGroup 从用户组创建模板
// @Summary 从用户组创建策略模板
// @Tags 策略模板管理
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param body body CreateFromGroupVO true "从用户组创建模板请求"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/templates/from-group [post]
func (h *TemplateHandler) CreateFromGroup(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var req CreateFromGroupVO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, Error(err))
		return
	}

	template, err := h.templateService.CreateFromGroup(c.Request.Context(), req.GroupID, req.Name, req.Description)
	if err != nil {
		h.logger.Error("从用户组创建模板失败", elog.String("tenant_id", tenantID), elog.Int64("group_id", req.GroupID), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(template))
}

// RegisterRoutes 注册路由
func (h *TemplateHandler) RegisterRoutes(r *gin.RouterGroup) {
	templates := r.Group("/templates")
	{
		templates.POST("", h.CreateTemplate)
		templates.GET("/:id", h.GetTemplate)
		templates.GET("", h.ListTemplates)
		templates.PUT("/:id", h.UpdateTemplate)
		templates.DELETE("/:id", h.DeleteTemplate)
		templates.POST("/from-group", h.CreateFromGroup)
	}
}
