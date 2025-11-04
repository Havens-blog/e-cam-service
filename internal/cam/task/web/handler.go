package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/task"
	taskservice "github.com/Havens-blog/e-cam-service/internal/cam/task/service"
	"github.com/Havens-blog/e-cam-service/pkg/ginx"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/gin-gonic/gin"
)

// TaskHandler 任务处理器
type TaskHandler struct {
	taskSvc taskservice.TaskService
}

// NewTaskHandler 创建任务处理器
func NewTaskHandler(taskSvc taskservice.TaskService) *TaskHandler {
	return &TaskHandler{
		taskSvc: taskSvc,
	}
}

// RegisterRoutes 注册路由
func (h *TaskHandler) RegisterRoutes(r *gin.RouterGroup) {
	taskGroup := r.Group("/tasks")
	{
		// 提交任务
		taskGroup.POST("/sync-assets", ginx.WrapBody[SubmitSyncAssetsTaskReq](h.SubmitSyncAssetsTask))
		taskGroup.POST("/discover-assets", ginx.WrapBody[SubmitDiscoverAssetsTaskReq](h.SubmitDiscoverAssetsTask))

		// 查询任务
		taskGroup.GET("/:id", h.GetTask)
		taskGroup.GET("", h.ListTasks)

		// 操作任务
		taskGroup.POST("/:id/cancel", h.CancelTask)
		taskGroup.DELETE("/:id", h.DeleteTask)
	}
}

// SubmitSyncAssetsTask 提交同步资产任务
func (h *TaskHandler) SubmitSyncAssetsTask(ctx *gin.Context, req SubmitSyncAssetsTaskReq) (ginx.Result, error) {
	// TODO: 从上下文获取当前用户
	createdBy := "system"

	params := task.SyncAssetsParams{
		Provider:   req.Provider,
		AssetTypes: req.AssetTypes,
		Regions:    req.Regions,
		AccountID:  req.AccountID,
	}

	taskID, err := h.taskSvc.SubmitSyncAssetsTask(ctx.Request.Context(), params, createdBy)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(SubmitTaskResp{
		TaskID:  taskID,
		Message: "任务已提交，正在执行中",
	}), nil
}

// SubmitDiscoverAssetsTask 提交发现资产任务
func (h *TaskHandler) SubmitDiscoverAssetsTask(ctx *gin.Context, req SubmitDiscoverAssetsTaskReq) (ginx.Result, error) {
	// TODO: 从上下文获取当前用户
	createdBy := "system"

	params := task.DiscoverAssetsParams{
		Provider:   req.Provider,
		Region:     req.Region,
		AssetTypes: req.AssetTypes,
		AccountID:  req.AccountID,
	}

	taskID, err := h.taskSvc.SubmitDiscoverAssetsTask(ctx.Request.Context(), params, createdBy)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(SubmitTaskResp{
		TaskID:  taskID,
		Message: "任务已提交，正在执行中",
	}), nil
}

// GetTask 获取任务
func (h *TaskHandler) GetTask(ctx *gin.Context) {
	taskID := ctx.Param("id")

	t, err := h.taskSvc.GetTask(ctx.Request.Context(), taskID)
	if err != nil {
		ctx.JSON(404, ErrorResult(errs.SystemError))
		return
	}

	resp := h.toTaskResp(t)
	ctx.JSON(200, Result(resp))
}

// ListTasks 获取任务列表
func (h *TaskHandler) ListTasks(ctx *gin.Context) {
	// 解析查询参数
	taskType := ctx.Query("type")
	status := ctx.Query("status")
	createdBy := ctx.Query("created_by")

	offset, _ := strconv.ParseInt(ctx.DefaultQuery("offset", "0"), 10, 64)
	limit, _ := strconv.ParseInt(ctx.DefaultQuery("limit", "20"), 10, 64)

	filter := taskx.TaskFilter{
		Type:      taskx.TaskType(taskType),
		Status:    taskx.TaskStatus(status),
		CreatedBy: createdBy,
		Offset:    offset,
		Limit:     limit,
	}

	tasks, total, err := h.taskSvc.ListTasks(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	taskResps := make([]TaskResp, len(tasks))
	for i, t := range tasks {
		taskResps[i] = h.toTaskResp(&t)
	}

	resp := TaskListResp{
		Tasks: taskResps,
		Total: total,
	}

	ctx.JSON(200, Result(resp))
}

// CancelTask 取消任务
func (h *TaskHandler) CancelTask(ctx *gin.Context) {
	taskID := ctx.Param("id")

	err := h.taskSvc.CancelTask(ctx.Request.Context(), taskID)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(nil))
}

// DeleteTask 删除任务
func (h *TaskHandler) DeleteTask(ctx *gin.Context) {
	taskID := ctx.Param("id")

	err := h.taskSvc.DeleteTask(ctx.Request.Context(), taskID)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(nil))
}

// toTaskResp 转换为任务响应
func (h *TaskHandler) toTaskResp(t *taskx.Task) TaskResp {
	return TaskResp{
		ID:          t.ID,
		Type:        string(t.Type),
		Status:      string(t.Status),
		Params:      t.Params,
		Result:      t.Result,
		Error:       t.Error,
		Progress:    t.Progress,
		Message:     t.Message,
		CreatedBy:   t.CreatedBy,
		CreatedAt:   t.CreatedAt,
		StartedAt:   t.StartedAt,
		CompletedAt: t.CompletedAt,
		Duration:    t.Duration,
	}
}

// Result 成功响应
func Result(data interface{}) ginx.Result {
	return ginx.Result{
		Code: 0,
		Msg:  "success",
		Data: data,
	}
}

// ErrorResult 错误响应
func ErrorResult(code errs.ErrorCode) ginx.Result {
	return ginx.Result{
		Code: code.Code,
		Msg:  code.Msg,
	}
}

// ErrorResultWithMsg 带消息的错误响应
func ErrorResultWithMsg(code errs.ErrorCode, msg string) ginx.Result {
	return ginx.Result{
		Code: code.Code,
		Msg:  msg,
	}
}
