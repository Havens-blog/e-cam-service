package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/iam/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

type GroupHandler struct {
	groupService service.PermissionGroupService
	logger       *elog.Component
}

func NewGroupHandler(groupService service.PermissionGroupService, logger *elog.Component) *GroupHandler {
	return &GroupHandler{
		groupService: groupService,
		logger:       logger,
	}
}

// CreateGroup 创建权限组
// @Summary 创建权限组
// @Tags 权限组管理
// @Accept json
// @Produce json
// @Param body body CreateGroupVO true "创建权限组请求"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/groups [post]
func (h *GroupHandler) CreateGroup(c *gin.Context) {
	var req CreateGroupVO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, Error(err))
		return
	}

	group, err := h.groupService.CreateGroup(c.Request.Context(), &domain.CreatePermissionGroupRequest{
		Name:           req.Name,
		Description:    req.Description,
		Policies:       req.Policies,
		CloudPlatforms: req.CloudPlatforms,
		TenantID:       req.TenantID,
	})

	if err != nil {
		h.logger.Error("创建权限组失败", elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(group))
}

// GetGroup 获取权限组详情
// @Summary 获取权限组详情
// @Tags 权限组管理
// @Produce json
// @Param id path int true "权限组ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/groups/{id} [get]
func (h *GroupHandler) GetGroup(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	group, err := h.groupService.GetGroup(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("获取权限组失败", elog.Int64("group_id", id), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(group))
}

// ListGroups 查询权限组列表
// @Summary 查询权限组列表
// @Tags 权限组管理
// @Produce json
// @Param tenant_id query string false "租户ID"
// @Param keyword query string false "关键词"
// @Param page query int false "页码"
// @Param size query int false "每页数量"
// @Success 200 {object} PageResult
// @Router /api/v1/cam/iam/groups [get]
func (h *GroupHandler) ListGroups(c *gin.Context) {
	var req ListGroupsVO
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

	groups, total, err := h.groupService.ListGroups(c.Request.Context(), domain.PermissionGroupFilter{
		TenantID: req.TenantID,
		Keyword:  req.Keyword,
		Offset:   offset,
		Limit:    req.Size,
	})

	if err != nil {
		h.logger.Error("查询权限组列表失败", elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, PageSuccess(groups, total, req.Page, req.Size))
}

// UpdateGroup 更新权限组
// @Summary 更新权限组信息
// @Tags 权限组管理
// @Accept json
// @Produce json
// @Param id path int true "权限组ID"
// @Param body body UpdateGroupVO true "更新权限组请求"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/groups/{id} [put]
func (h *GroupHandler) UpdateGroup(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	var req UpdateGroupVO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, Error(err))
		return
	}

	err = h.groupService.UpdateGroup(c.Request.Context(), id, &domain.UpdatePermissionGroupRequest{
		Name:           req.Name,
		Description:    req.Description,
		CloudPlatforms: req.CloudPlatforms,
	})

	if err != nil {
		h.logger.Error("更新权限组失败", elog.Int64("group_id", id), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, SuccessWithMsg("更新成功", nil))
}

// DeleteGroup 删除权限组
// @Summary 删除权限组
// @Tags 权限组管理
// @Produce json
// @Param id path int true "权限组ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/groups/{id} [delete]
func (h *GroupHandler) DeleteGroup(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	err = h.groupService.DeleteGroup(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("删除权限组失败", elog.Int64("group_id", id), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, SuccessWithMsg("删除成功", nil))
}

// UpdatePolicies 更新权限策略
// @Summary 更新权限组的权限策略
// @Tags 权限组管理
// @Accept json
// @Produce json
// @Param id path int true "权限组ID"
// @Param body body UpdatePoliciesVO true "更新权限策略请求"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/groups/{id}/policies [put]
func (h *GroupHandler) UpdatePolicies(c *gin.Context) {
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
		h.logger.Error("更新权限策略失败", elog.Int64("group_id", id), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, SuccessWithMsg("更新成功", nil))
}

// RegisterRoutes 注册路由
func (h *GroupHandler) RegisterRoutes(r *gin.RouterGroup) {
	groups := r.Group("/groups")
	{
		groups.POST("", h.CreateGroup)
		groups.GET("/:id", h.GetGroup)
		groups.GET("", h.ListGroups)
		groups.PUT("/:id", h.UpdateGroup)
		groups.DELETE("/:id", h.DeleteGroup)
		groups.PUT("/:id/policies", h.UpdatePolicies)
	}
}
