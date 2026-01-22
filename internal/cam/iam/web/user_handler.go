package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/iam/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/middleware"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

type UserHandler struct {
	userService service.CloudUserService
	logger      *elog.Component
}

func NewUserHandler(userService service.CloudUserService, logger *elog.Component) *UserHandler {
	return &UserHandler{
		userService: userService,
		logger:      logger,
	}
}

// CreateUser 创建用户
// @Summary 创建云用户
// @Tags 用户管理
// @Accept json
// @Produce json
// @Param body body CreateUserVO true "创建用户请求"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/users [post]
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req CreateUserVO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, Error(err))
		return
	}

	// 从上下文获取租户ID
	tenantID := middleware.GetTenantID(c)

	user, err := h.userService.CreateUser(c.Request.Context(), &domain.CreateCloudUserRequest{
		Username:       req.Username,
		UserType:       req.UserType,
		CloudAccountID: req.CloudAccountID,
		DisplayName:    req.DisplayName,
		Email:          req.Email,
		UserGroups:     req.UserGroups,
		TenantID:       tenantID, // 使用中间件提取的租户ID
	})

	if err != nil {
		h.logger.Error("创建用户失败", elog.String("tenant_id", tenantID), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(user))
}

// GetUser 获取用户详情
// @Summary 获取用户详情
// @Tags 用户管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "用户ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/users/{id} [get]
func (h *UserHandler) GetUser(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	user, err := h.userService.GetUser(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("获取用户失败", elog.String("tenant_id", tenantID), elog.Int64("user_id", id), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(user))
}

// ListUsers 查询用户列表
// @Summary 查询用户列表
// @Tags 用户管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param provider query string false "云厂商"
// @Param user_type query string false "用户类型"
// @Param status query string false "状态"
// @Param page query int false "页码"
// @Param size query int false "每页数量"
// @Success 200 {object} PageResult
// @Router /api/v1/cam/iam/users [get]
func (h *UserHandler) ListUsers(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var req ListUsersVO
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

	users, total, err := h.userService.ListUsers(c.Request.Context(), domain.CloudUserFilter{
		Provider:       req.Provider,
		UserType:       req.UserType,
		Status:         req.Status,
		CloudAccountID: req.CloudAccountID,
		TenantID:       tenantID, // 使用中间件提取的租户ID
		Keyword:        req.Keyword,
		Offset:         offset,
		Limit:          req.Size,
	})

	if err != nil {
		h.logger.Error("查询用户列表失败", elog.String("tenant_id", tenantID), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, PageSuccess(users, total, req.Page, req.Size))
}

// UpdateUser 更新用户
// @Summary 更新用户信息
// @Tags 用户管理
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "用户ID"
// @Param body body UpdateUserVO true "更新用户请求"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/users/{id} [put]
func (h *UserHandler) UpdateUser(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	var req UpdateUserVO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, Error(err))
		return
	}

	err = h.userService.UpdateUser(c.Request.Context(), id, &domain.UpdateCloudUserRequest{
		DisplayName: req.DisplayName,
		Email:       req.Email,
		UserGroups:  req.UserGroups,
		Status:      req.Status,
	})

	if err != nil {
		h.logger.Error("更新用户失败", elog.String("tenant_id", tenantID), elog.Int64("user_id", id), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, SuccessWithMsg("更新成功", nil))
}

// DeleteUser 删除用户
// @Summary 删除用户
// @Tags 用户管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "用户ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/users/{id} [delete]
func (h *UserHandler) DeleteUser(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	err = h.userService.DeleteUser(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("删除用户失败", elog.String("tenant_id", tenantID), elog.Int64("user_id", id), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, SuccessWithMsg("删除成功", nil))
}

// SyncUsers 同步用户
// @Summary 同步云平台用户（异步）
// @Description 创建异步同步任务，立即返回任务ID，可通过任务ID查询同步进度
// @Tags 用户管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param cloud_account_id query int true "云账号ID"
// @Success 200 {object} Result{data=domain.SyncTask}
// @Router /api/v1/cam/iam/users/sync [post]
func (h *UserHandler) SyncUsers(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	cloudAccountIDStr := c.Query("cloud_account_id")
	cloudAccountID, err := strconv.ParseInt(cloudAccountIDStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	// 使用异步同步方法
	task, err := h.userService.SyncUsersAsync(c.Request.Context(), cloudAccountID)
	if err != nil {
		h.logger.Error("创建同步任务失败", elog.String("tenant_id", tenantID), elog.Int64("cloud_account_id", cloudAccountID), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, SuccessWithMsg("同步任务已创建，请通过任务ID查询进度", task))
}

// AssignPermissionGroups 批量分配用户组
// @Summary 批量分配用户组
// @Tags 用户管理
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param body body AssignUserGroupsVO true "批量分配用户组请求"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/users/batch-assign [post]
func (h *UserHandler) AssignPermissionGroups(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var req AssignUserGroupsVO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, Error(err))
		return
	}

	err := h.userService.AssignPermissionGroups(c.Request.Context(), req.UserIDs, req.GroupIDs)
	if err != nil {
		h.logger.Error("批量分配用户组失败", elog.String("tenant_id", tenantID), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, SuccessWithMsg("分配成功", nil))
}

// RegisterRoutes 注册路由
func (h *UserHandler) RegisterRoutes(r *gin.RouterGroup) {
	users := r.Group("/users")
	{
		users.POST("", h.CreateUser)
		users.GET("/:id", h.GetUser)
		users.GET("", h.ListUsers)
		users.PUT("/:id", h.UpdateUser)
		users.DELETE("/:id", h.DeleteUser)
		users.POST("/sync", h.SyncUsers)
		users.POST("/batch-assign", h.AssignPermissionGroups)
	}
}
