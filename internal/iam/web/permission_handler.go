package web

import (
	"fmt"
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/iam/service"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

type PermissionHandler struct {
	permissionService service.PermissionService
	logger            *elog.Component
}

func NewPermissionHandler(permissionService service.PermissionService, logger *elog.Component) *PermissionHandler {
	return &PermissionHandler{
		permissionService: permissionService,
		logger:            logger,
	}
}

// GetUserPermissions 获取用户权限
// @Summary 获取用户的所有权限
// @Tags 权限管理
// @Produce json
// @Param user_id path int true "用户ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/permissions/users/{user_id} [get]
func (h *PermissionHandler) GetUserPermissions(c *gin.Context) {
	userIDStr := c.Param("user_id")
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	permissions, err := h.permissionService.GetUserPermissions(c.Request.Context(), userID)
	if err != nil {
		h.logger.Error("获取用户权限失败", elog.Int64("user_id", userID), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(permissions))
}

// GetUserGroupPermissions 获取用户组权限
// @Summary 获取用户组的所有权限
// @Tags 权限管理
// @Produce json
// @Param group_id path int true "用户组ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/permissions/groups/{group_id} [get]
func (h *PermissionHandler) GetUserGroupPermissions(c *gin.Context) {
	groupIDStr := c.Param("group_id")
	groupID, err := strconv.ParseInt(groupIDStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	permissions, err := h.permissionService.GetUserGroupPermissions(c.Request.Context(), groupID)
	if err != nil {
		h.logger.Error("获取用户组权限失败", elog.Int64("group_id", groupID), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(permissions))
}

// GetUserEffectivePermissions 获取用户有效权限
// @Summary 获取用户的有效权限（包含用户组继承的权限）
// @Tags 权限管理
// @Produce json
// @Param user_id path int true "用户ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/permissions/users/{user_id}/effective [get]
func (h *PermissionHandler) GetUserEffectivePermissions(c *gin.Context) {
	userIDStr := c.Param("user_id")
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	permissions, err := h.permissionService.GetUserEffectivePermissions(c.Request.Context(), userID)
	if err != nil {
		h.logger.Error("获取用户有效权限失败", elog.Int64("user_id", userID), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(permissions))
}

// ListPoliciesByProvider 查询云平台权限策略
// @Summary 按云厂商查询可用的权限策略
// @Tags 权限管理
// @Produce json
// @Param cloud_account_id query int true "云账号ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/permissions/policies [get]
func (h *PermissionHandler) ListPoliciesByProvider(c *gin.Context) {
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

	policies, err := h.permissionService.ListPoliciesByProvider(c.Request.Context(), cloudAccountID)
	if err != nil {
		h.logger.Error("查询云平台权限策略失败",
			elog.Int64("cloud_account_id", cloudAccountID),
			elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(policies))
}

// RegisterRoutes 注册路由
func (h *PermissionHandler) RegisterRoutes(r *gin.RouterGroup) {
	permissions := r.Group("/permissions")
	{
		// 用户权限
		permissions.GET("/users/:user_id", h.GetUserPermissions)
		permissions.GET("/users/:user_id/effective", h.GetUserEffectivePermissions)

		// 用户组权限
		permissions.GET("/groups/:group_id", h.GetUserGroupPermissions)

		// 云平台权限策略
		permissions.GET("/policies", h.ListPoliciesByProvider)
	}
}
