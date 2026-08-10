package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/collector"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/task"
	taskservice "github.com/Havens-blog/e-cam-service/internal/cam/task/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-cam-service/pkg/ginx"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// ManualCollectReq 手动触发采集请求
type ManualCollectReq struct {
	AccountID int64  `json:"account_id"`
	StartTime string `json:"start_time"` // RFC3339 格式
	EndTime   string `json:"end_time"`   // RFC3339 格式
}

// CollectorHandler 采集管理 API 处理器
type CollectorHandler struct {
	collectorSvc *collector.CollectorService
	taskSvc      taskservice.TaskService
}

// NewCollectorHandler 创建采集管理处理器
func NewCollectorHandler(collectorSvc *collector.CollectorService, taskSvc taskservice.TaskService) *CollectorHandler {
	return &CollectorHandler{collectorSvc: collectorSvc, taskSvc: taskSvc}
}

// PrivateRoutes 注册采集管理相关路由
func (h *CollectorHandler) PrivateRoutes(server *gin.Engine) {
	g := server.Group("/api/v1/cam")
	// 本组全部端点都以 middleware.GetTenantID(ctx) 构造 filter，且组内**没有**任何
	// handler 级的 tenantID==0 判空，故此处的 RequireTenant 不是重复校验而是唯一防线。
	// 若放行 tenant_id=0（eiam 的「等待选择租户」临时凭证），DAO 的
	// `if filter.TenantID != 0` 会丢弃租户谓词，返回全部租户的成本数据。
	g.Use(middleware.RequireTenant(elog.DefaultLogger))
	g.POST("/cost/collect", ginx.WrapBody(h.TriggerManualCollection))
	g.GET("/cost/collect/logs", h.ListCollectLogs)
}

// TriggerManualCollection 手动触发采集（通过异步任务队列）
func (h *CollectorHandler) TriggerManualCollection(ctx *gin.Context, req ManualCollectReq) (ginx.Result, error) {
	tenantID := middleware.GetTenantID(ctx)

	// 验证时间格式
	if _, err := time.Parse(time.RFC3339, req.StartTime); err != nil {
		return web.ErrorResultWithMsg(errs.ParamsError, "invalid start_time format, use RFC3339"), nil
	}
	if _, err := time.Parse(time.RFC3339, req.EndTime); err != nil {
		return web.ErrorResultWithMsg(errs.ParamsError, "invalid end_time format, use RFC3339"), nil
	}

	params := task.SyncBillingParams{
		AccountID: req.AccountID,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
		TenantID:  tenantID,
	}

	taskID, err := h.taskSvc.SubmitBillingCollectTask(ctx.Request.Context(), params, "user")
	if err != nil {
		return web.ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return web.Result(gin.H{
		"task_id": taskID,
		"message": "采集任务已提交，可在任务列表查看进度",
	}), nil
}

// ListCollectLogs 采集日志列表
func (h *CollectorHandler) ListCollectLogs(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	var accountID int64
	if aid := ctx.Query("account_id"); aid != "" {
		accountID, _ = strconv.ParseInt(aid, 10, 64)
	}

	filter := repository.CollectLogFilter{
		TenantID:  tenantID,
		AccountID: accountID,
		Status:    ctx.Query("status"),
		Offset:    int64(offset),
		Limit:     int64(limit),
	}

	logs, total, err := h.collectorSvc.ListCollectLogs(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(gin.H{
		"items": logs,
		"total": total,
	}))
}
