// Package web 任务 HTTP 处理器
package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/shared/errs"
	"github.com/Havens-blog/e-cam-service/internal/task"
	taskservice "github.com/Havens-blog/e-cam-service/internal/task/service"
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
	return &TaskHandler{taskSvc: taskSvc}
}

// RegisterRoutes 注册路由
func (h *TaskHandler) RegisterRoutes(r *gin.RouterGroup) {
	taskGroup := r.Group("/tasks")
	{
		taskGroup.POST("/sync-assets", ginx.WrapBody[SubmitSyncAssetsTaskReq](h.SubmitSyncAssetsTask))
		taskGroup.POST("/discover-assets", ginx.WrapBody[SubmitDiscoverAssetsTaskReq](h.SubmitDiscoverAssetsTask))
		taskGroup.GET("/:id", h.GetTask)
		taskGroup.GET("", h.ListTasks)
		taskGroup.POST("/:id/cancel", h.CancelTask)
		taskGroup.DELETE("/:id", h.DeleteTask)
	}
}

// SubmitSyncAssetsTask 提交同步资产任务
// @Summary 提交同步资产任务
// @Description 提交异步同步资产任务，支持指定云厂商、资产类型和地域
// @Tags 异步任务
// @Accept json
// @Produce json
// @Param request body SubmitSyncAssetsTaskReq true "同步任务参数"
// @Success 200 {object} ginx.Result{data=SubmitTaskResp} "任务提交成功"
// @Failure 400 {object} ginx.Result "请求参数错误"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/tasks/sync-assets [post]
func (h *TaskHandler) SubmitSyncAssetsTask(ctx *gin.Context, req SubmitSyncAssetsTaskReq) (ginx.Result, error) {
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
// @Summary 提交发现资产任务
// @Description 提交异步发现资产任务，从指定云厂商和地域发现新资产
// @Tags 异步任务
// @Accept json
// @Produce json
// @Param request body SubmitDiscoverAssetsTaskReq true "发现任务参数"
// @Success 200 {object} ginx.Result{data=SubmitTaskResp} "任务提交成功"
// @Failure 400 {object} ginx.Result "请求参数错误"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/tasks/discover-assets [post]
func (h *TaskHandler) SubmitDiscoverAssetsTask(ctx *gin.Context, req SubmitDiscoverAssetsTaskReq) (ginx.Result, error) {
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
// @Summary 获取任务详情
// @Description 根据任务ID获取异步任务的详细信息和执行状态
// @Tags 异步任务
// @Accept json
// @Produce json
// @Param id path string true "任务ID"
// @Success 200 {object} ginx.Result{data=TaskResp} "成功"
// @Failure 404 {object} ginx.Result "任务不存在"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/tasks/{id} [get]
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
// @Summary 获取任务列表
// @Description 获取异步任务列表，支持按任务类型、状态、创建者等条件过滤
// @Tags 异步任务
// @Accept json
// @Produce json
// @Param type query string false "任务类型" Enums(sync_assets,discover_assets)
// @Param status query string false "任务状态" Enums(pending,running,completed,failed,cancelled)
// @Param created_by query string false "创建者"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} ginx.Result{data=TaskListResp} "成功"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/tasks [get]
func (h *TaskHandler) ListTasks(ctx *gin.Context) {
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

	resp := TaskListResp{Tasks: taskResps, Total: total}
	ctx.JSON(200, Result(resp))
}

// CancelTask 取消任务
// @Summary 取消任务
// @Description 取消正在执行或等待执行的异步任务
// @Tags 异步任务
// @Accept json
// @Produce json
// @Param id path string true "任务ID"
// @Success 200 {object} ginx.Result "取消成功"
// @Failure 400 {object} ginx.Result "任务无法取消"
// @Failure 404 {object} ginx.Result "任务不存在"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/tasks/{id}/cancel [post]
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
// @Summary 删除任务
// @Description 删除已完成或已取消的异步任务记录
// @Tags 异步任务
// @Accept json
// @Produce json
// @Param id path string true "任务ID"
// @Success 200 {object} ginx.Result "删除成功"
// @Failure 400 {object} ginx.Result "任务无法删除"
// @Failure 404 {object} ginx.Result "任务不存在"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/tasks/{id} [delete]
func (h *TaskHandler) DeleteTask(ctx *gin.Context) {
	taskID := ctx.Param("id")

	err := h.taskSvc.DeleteTask(ctx.Request.Context(), taskID)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(nil))
}

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
	return ginx.Result{Code: 0, Msg: "success", Data: data}
}

// ErrorResult 错误响应
func ErrorResult(code errs.ErrorCode) ginx.Result {
	return ginx.Result{Code: code.Code, Msg: code.Msg}
}

// ErrorResultWithMsg 带消息的错误响应
func ErrorResultWithMsg(code errs.ErrorCode, msg string) ginx.Result {
	return ginx.Result{Code: code.Code, Msg: msg}
}
