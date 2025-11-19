package web

import (
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/iam/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

type TenantHandler struct {
	tenantService service.TenantService
	logger        *elog.Component
}

func NewTenantHandler(tenantService service.TenantService, logger *elog.Component) *TenantHandler {
	return &TenantHandler{
		tenantService: tenantService,
		logger:        logger,
	}
}

// CreateTenant 创建租户
// @Summary 创建租户
// @Tags 租户管理
// @Accept json
// @Produce json
// @Param body body CreateTenantVO true "创建租户请求"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/tenants [post]
func (h *TenantHandler) CreateTenant(c *gin.Context) {
	var req CreateTenantVO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, Error(err))
		return
	}

	tenant, err := h.tenantService.CreateTenant(c.Request.Context(), &domain.CreateTenantRequest{
		ID:          req.ID,
		Name:        req.Name,
		DisplayName: req.DisplayName,
		Description: req.Description,
		Settings:    req.Settings,
		Metadata:    req.Metadata,
	})

	if err != nil {
		h.logger.Error("创建租户失败", elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(tenant))
}

// GetTenant 获取租户详情
// @Summary 获取租户详情
// @Tags 租户管理
// @Produce json
// @Param tenant_id path string true "租户ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/tenants/{tenant_id} [get]
func (h *TenantHandler) GetTenant(c *gin.Context) {
	tenantID := c.Param("tenant_id")
	if tenantID == "" {
		c.JSON(400, Error(fmt.Errorf("tenant_id is required")))
		return
	}

	tenant, err := h.tenantService.GetTenant(c.Request.Context(), tenantID)
	if err != nil {
		h.logger.Error("获取租户失败", elog.String("tenant_id", tenantID), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(tenant))
}

// ListTenants 查询租户列表
// @Summary 查询租户列表
// @Tags 租户管理
// @Produce json
// @Param keyword query string false "关键词"
// @Param status query string false "状态"
// @Param industry query string false "行业"
// @Param region query string false "地区"
// @Param page query int false "页码"
// @Param size query int false "每页数量"
// @Success 200 {object} PageResult
// @Router /api/v1/cam/iam/tenants [get]
func (h *TenantHandler) ListTenants(c *gin.Context) {
	var req ListTenantsVO
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

	tenants, total, err := h.tenantService.ListTenants(c.Request.Context(), domain.TenantFilter{
		Keyword:  req.Keyword,
		Status:   domain.TenantStatus(req.Status),
		Industry: req.Industry,
		Region:   req.Region,
		Offset:   offset,
		Limit:    req.Size,
	})

	if err != nil {
		h.logger.Error("查询租户列表失败", elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, PageSuccess(tenants, total, req.Page, req.Size))
}

// UpdateTenant 更新租户
// @Summary 更新租户信息
// @Tags 租户管理
// @Accept json
// @Produce json
// @Param tenant_id path string true "租户ID"
// @Param body body UpdateTenantVO true "更新租户请求"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/tenants/{tenant_id} [put]
func (h *TenantHandler) UpdateTenant(c *gin.Context) {
	tenantID := c.Param("tenant_id")
	if tenantID == "" {
		c.JSON(400, Error(fmt.Errorf("tenant_id is required")))
		return
	}

	var req UpdateTenantVO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, Error(err))
		return
	}

	updateReq := &domain.UpdateTenantRequest{
		Name:        req.Name,
		DisplayName: req.DisplayName,
		Description: req.Description,
		Status:      req.Status,
		Settings:    req.Settings,
		Metadata:    req.Metadata,
	}

	err := h.tenantService.UpdateTenant(c.Request.Context(), tenantID, updateReq)
	if err != nil {
		h.logger.Error("更新租户失败", elog.String("tenant_id", tenantID), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, SuccessWithMsg("更新成功", nil))
}

// DeleteTenant 删除租户
// @Summary 删除租户
// @Tags 租户管理
// @Produce json
// @Param tenant_id path string true "租户ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/tenants/{tenant_id} [delete]
func (h *TenantHandler) DeleteTenant(c *gin.Context) {
	tenantID := c.Param("tenant_id")
	if tenantID == "" {
		c.JSON(400, Error(fmt.Errorf("tenant_id is required")))
		return
	}

	err := h.tenantService.DeleteTenant(c.Request.Context(), tenantID)
	if err != nil {
		h.logger.Error("删除租户失败", elog.String("tenant_id", tenantID), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, SuccessWithMsg("删除成功", nil))
}

// GetTenantStats 获取租户统计信息
// @Summary 获取租户统计信息
// @Tags 租户管理
// @Produce json
// @Param tenant_id path string true "租户ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/tenants/{tenant_id}/stats [get]
func (h *TenantHandler) GetTenantStats(c *gin.Context) {
	tenantID := c.Param("tenant_id")
	if tenantID == "" {
		c.JSON(400, Error(fmt.Errorf("tenant_id is required")))
		return
	}

	stats, err := h.tenantService.GetTenantStats(c.Request.Context(), tenantID)
	if err != nil {
		h.logger.Error("获取租户统计信息失败", elog.String("tenant_id", tenantID), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(stats))
}

// RegisterRoutes 注册路由
func (h *TenantHandler) RegisterRoutes(r *gin.RouterGroup) {
	tenants := r.Group("/tenants")
	{
		tenants.POST("", h.CreateTenant)
		tenants.GET("/:tenant_id", h.GetTenant)
		tenants.GET("", h.ListTenants)
		tenants.PUT("/:tenant_id", h.UpdateTenant)
		tenants.DELETE("/:tenant_id", h.DeleteTenant)
		tenants.GET("/:tenant_id/stats", h.GetTenantStats)
	}
}
