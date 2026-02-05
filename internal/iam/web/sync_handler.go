package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/iam/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

type SyncHandler struct {
	syncService service.SyncService
	logger      *elog.Component
}

func NewSyncHandler(syncService service.SyncService, logger *elog.Component) *SyncHandler {
	return &SyncHandler{
		syncService: syncService,
		logger:      logger,
	}
}

// CreateSyncTask 创建同步任务
// @Summary 创建同步任务
// @Tags 同步任务管理
// @Accept json
// @Produce json
// @Param body body CreateSyncTaskVO true "创建同步任务请求"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/sync/tasks [post]
func (h *SyncHandler) CreateSyncTask(c *gin.Context) {
	var req CreateSyncTaskVO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, Error(err))
		return
	}

	task, err := h.syncService.CreateSyncTask(c.Request.Context(), &service.CreateSyncTaskRequest{
		TaskType:       req.TaskType,
		TargetType:     req.TargetType,
		TargetID:       req.TargetID,
		CloudAccountID: req.CloudAccountID,
		Provider:       req.Provider,
	})

	if err != nil {
		h.logger.Error("创建同步任务失败", elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(task))
}

// GetSyncTaskStatus 获取同步任务状态
// @Summary 获取同步任务状态
// @Tags 同步任务管理
// @Produce json
// @Param id path int true "任务ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/sync/tasks/{id} [get]
func (h *SyncHandler) GetSyncTaskStatus(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	task, err := h.syncService.GetSyncTaskStatus(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("获取同步任务状态失败", elog.Int64("task_id", id), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(task))
}

// ListSyncTasks 查询同步任务列表
// @Summary 查询同步任务列表
// @Tags 同步任务管理
// @Produce json
// @Param task_type query string false "任务类型"
// @Param status query string false "状态"
// @Param cloud_account_id query int false "云账号ID"
// @Param provider query string false "云厂商"
// @Param page query int false "页码"
// @Param size query int false "每页数量"
// @Success 200 {object} PageResult
// @Router /api/v1/cam/iam/sync/tasks [get]
func (h *SyncHandler) ListSyncTasks(c *gin.Context) {
	var req ListSyncTasksVO
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

	tasks, total, err := h.syncService.ListSyncTasks(c.Request.Context(), domain.SyncTaskFilter{
		TaskType:       req.TaskType,
		Status:         req.Status,
		CloudAccountID: req.CloudAccountID,
		Provider:       req.Provider,
		Offset:         offset,
		Limit:          req.Size,
	})

	if err != nil {
		h.logger.Error("查询同步任务列表失败", elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, PageSuccess(tasks, total, req.Page, req.Size))
}

// RetrySyncTask 重试同步任务
// @Summary 重试失败的同步任务
// @Tags 同步任务管理
// @Produce json
// @Param id path int true "任务ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/sync/tasks/{id}/retry [post]
func (h *SyncHandler) RetrySyncTask(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(400, Error(err))
		return
	}

	err = h.syncService.RetrySyncTask(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("重试同步任务失败", elog.Int64("task_id", id), elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, SuccessWithMsg("重试成功", nil))
}

// RegisterRoutes 注册路由
func (h *SyncHandler) RegisterRoutes(r *gin.RouterGroup) {
	sync := r.Group("/sync")
	{
		tasks := sync.Group("/tasks")
		{
			tasks.POST("", h.CreateSyncTask)
			tasks.GET("/:id", h.GetSyncTaskStatus)
			tasks.GET("", h.ListSyncTasks)
			tasks.POST("/:id/retry", h.RetrySyncTask)
		}
	}
}
