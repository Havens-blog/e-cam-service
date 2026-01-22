package web

import (
	"fmt"
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/iam/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/middleware"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

type UserGroupHandler struct {
	groupService service.UserGroupService
	logger       *elog.Component
}

func NewUserGroupHandler(groupService service.UserGroupService, logger *elog.Component) *UserGroupHandler {
	return &UserGroupHandler{
		groupService: groupService,
		logger:       logger,
	}
}

// CreateGroup 创建用户组
// @Summary 创建用户组
// @Tags 用户组管理
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param body body CreateUserGroupVO true "创建用户组请求"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/groups [post]
func (h *UserGroupHandler) CreateGroup(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var req CreateUserGroupVO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, Error(err))
		return
	}

	group, err := h.groupService.CreateGroup(c.Request.Context(), &domain.CreateUserGroupRequest{
		Name:           req.Name,
		Description:    req.Description,
		Policies:       req.Policies,
		CloudPlatforms: req.CloudPlatforms,
		TenantID:       tenantID, // 使用中间件提取的租户ID
	})

	if err != nil {
		h.logger.Error("创建用户组失败", elog.String("tenant_id", tenantID), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(group))
}

// GetGroup 获取用户组详情
// @Summary 获取用户组详情
// @Tags 用户组管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "用户组ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/groups/{id} [get]
func (h *UserGroupHandler) GetGroup(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	group, err := h.groupService.GetGroup(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("获取用户组失败", elog.String("tenant_id", tenantID), elog.Int64("group_id", id), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(group))
}

// ListGroups 查询用户组列表
// @Summary 查询用户组列表
// @Tags 用户组管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param keyword query string false "关键词"
// @Param page query int false "页码"
// @Param size query int false "每页数量"
// @Success 200 {object} PageResult
// @Router /api/v1/cam/iam/groups [get]
func (h *UserGroupHandler) ListGroups(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var req ListUserGroupsVO
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

	groups, total, err := h.groupService.ListGroups(c.Request.Context(), domain.UserGroupFilter{
		TenantID: tenantID, // 使用中间件提取的租户ID
		Keyword:  req.Keyword,
		Offset:   offset,
		Limit:    req.Size,
	})

	if err != nil {
		h.logger.Error("查询用户组列表失败", elog.String("tenant_id", tenantID), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, PageSuccess(groups, total, req.Page, req.Size))
}

// UpdateGroup 更新用户组
// @Summary 更新用户组信息
// @Tags 用户组管理
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "用户组ID"
// @Param body body UpdateUserGroupVO true "更新用户组请求"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/groups/{id} [put]
func (h *UserGroupHandler) UpdateGroup(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	var req UpdateUserGroupVO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, Error(err))
		return
	}

	err = h.groupService.UpdateGroup(c.Request.Context(), id, &domain.UpdateUserGroupRequest{
		Name:           req.Name,
		Description:    req.Description,
		CloudPlatforms: req.CloudPlatforms,
	})

	if err != nil {
		h.logger.Error("更新用户组失败", elog.String("tenant_id", tenantID), elog.Int64("group_id", id), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, SuccessWithMsg("更新成功", nil))
}

// DeleteGroup 删除用户组
// @Summary 删除用户组
// @Tags 用户组管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "用户组ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/groups/{id} [delete]
func (h *UserGroupHandler) DeleteGroup(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	err = h.groupService.DeleteGroup(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("删除用户组失败", elog.String("tenant_id", tenantID), elog.Int64("group_id", id), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, SuccessWithMsg("删除成功", nil))
}

// UpdatePolicies 更新用户组权限
// @Summary 更新用户组的权限策略
// @Tags 用户组管理
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "用户组ID"
// @Param body body UpdatePoliciesVO true "更新权限策略请求"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/groups/{id}/policies [put]
func (h *UserGroupHandler) UpdatePolicies(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	var req UpdatePoliciesVO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, Error(err))
		return
	}

	err = h.groupService.UpdatePolicies(c.Request.Context(), id, req.Policies)
	if err != nil {
		h.logger.Error("更新权限策略失败", elog.String("tenant_id", tenantID), elog.Int64("group_id", id), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, SuccessWithMsg("更新成功", nil))
}

// GetGroupMembers 获取用户组成员列表
// @Summary 获取用户组成员列表
// @Tags 用户组管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "用户组ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/groups/{id}/members [get]
func (h *UserGroupHandler) GetGroupMembers(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	members, err := h.groupService.GetGroupMembers(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("获取用户组成员失败", elog.String("tenant_id", tenantID), elog.Int64("group_id", id), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(members))
}

// RegisterRoutes 注册路由
func (h *UserGroupHandler) RegisterRoutes(r *gin.RouterGroup) {
	groups := r.Group("/groups")
	{
		groups.POST("", h.CreateGroup)
		groups.GET("/:id", h.GetGroup)
		groups.GET("", h.ListGroups)
		groups.PUT("/:id", h.UpdateGroup)
		groups.DELETE("/:id", h.DeleteGroup)
		groups.PUT("/:id/policies", h.UpdatePolicies)
		groups.POST("/sync", h.SyncGroups)
		groups.GET("/:id/members", h.GetGroupMembers)
	}
}

// SyncGroups 同步用户组及成员
// @Summary 同步云平台用户组及其成员
// @Description 从云平台同步用户组信息，包括用户组基本信息、权限策略和用户组成员。支持阿里云、腾讯云等多云平台。
// @Description 同步结果包含：用户组统计（总数、新建、更新、失败）和成员统计（总数、同步成功、失败）
// @Tags 用户组管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param cloud_account_id query int true "云账号ID"
// @Success 200 {object} Result{data=service.GroupSyncResult} "同步结果统计"
// @Failure 400 {object} Result "参数错误"
// @Failure 500 {object} Result "同步失败"
// @Router /api/v1/cam/iam/groups/sync [post]
func (h *UserGroupHandler) SyncGroups(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	cloudAccountIDStr := c.Query("cloud_account_id")
	if cloudAccountIDStr == "" {
		c.JSON(400, Error(fmt.Errorf("cloud_account_id is required")))
		return
	}

	cloudAccountID, err := strconv.ParseInt(cloudAccountIDStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(fmt.Errorf("invalid cloud_account_id")))
		return
	}

	result, err := h.groupService.SyncGroups(c.Request.Context(), cloudAccountID)
	if err != nil {
		h.logger.Error("同步用户组失败",
			elog.String("tenant_id", tenantID),
			elog.Int64("cloud_account_id", cloudAccountID),
			elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(result))
}
