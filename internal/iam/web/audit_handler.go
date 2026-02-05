package web

import (
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/iam/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

type AuditHandler struct {
	auditService service.AuditService
	logger       *elog.Component
}

func NewAuditHandler(auditService service.AuditService, logger *elog.Component) *AuditHandler {
	return &AuditHandler{
		auditService: auditService,
		logger:       logger,
	}
}

// ListAuditLogs 查询审计日志列表
// @Summary 查询审计日志列表
// @Tags 审计日志管理
// @Produce json
// @Param operation_type query string false "操作类型"
// @Param operator_id query string false "操作人ID"
// @Param target_type query string false "目标类型"
// @Param cloud_platform query string false "云平台"
// @Param tenant_id query string false "租户ID"
// @Param start_time query string false "开始时间"
// @Param end_time query string false "结束时间"
// @Param page query int false "页码"
// @Param size query int false "每页数量"
// @Success 200 {object} PageResult
// @Router /api/v1/cam/iam/audit/logs [get]
func (h *AuditHandler) ListAuditLogs(c *gin.Context) {
	var req ListAuditLogsVO
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

	filter := domain.AuditLogFilter{
		OperationType: req.OperationType,
		OperatorID:    req.OperatorID,
		TargetType:    req.TargetType,
		CloudPlatform: req.CloudPlatform,
		TenantID:      req.TenantID,
		Offset:        offset,
		Limit:         req.Size,
	}

	// 解析时间参数
	if req.StartTime != "" {
		startTime, err := time.Parse("2006-01-02 15:04:05", req.StartTime)
		if err != nil {
			c.JSON(400, Error(fmt.Errorf("开始时间格式错误: %w", err)))
			return
		}
		filter.StartTime = &startTime
	}

	if req.EndTime != "" {
		endTime, err := time.Parse("2006-01-02 15:04:05", req.EndTime)
		if err != nil {
			c.JSON(400, Error(fmt.Errorf("结束时间格式错误: %w", err)))
			return
		}
		filter.EndTime = &endTime
	}

	logs, total, err := h.auditService.ListAuditLogs(c.Request.Context(), filter)
	if err != nil {
		h.logger.Error("查询审计日志列表失败", elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, PageSuccess(logs, total, req.Page, req.Size))
}

// ExportAuditLogs 导出审计日志
// @Summary 导出审计日志
// @Tags 审计日志管理
// @Produce json
// @Param operation_type query string false "操作类型"
// @Param operator_id query string false "操作人ID"
// @Param target_type query string false "目标类型"
// @Param cloud_platform query string false "云平台"
// @Param tenant_id query string false "租户ID"
// @Param start_time query string false "开始时间"
// @Param end_time query string false "结束时间"
// @Param format query string true "导出格式(csv/json)"
// @Success 200 {file} file
// @Router /api/v1/cam/iam/audit/logs/export [get]
func (h *AuditHandler) ExportAuditLogs(c *gin.Context) {
	var req ExportAuditLogsVO
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(400, Error(err))
		return
	}

	filter := domain.AuditLogFilter{
		OperationType: req.OperationType,
		OperatorID:    req.OperatorID,
		TargetType:    req.TargetType,
		CloudPlatform: req.CloudPlatform,
		TenantID:      req.TenantID,
	}

	// 解析时间参数
	if req.StartTime != "" {
		startTime, err := time.Parse("2006-01-02 15:04:05", req.StartTime)
		if err != nil {
			c.JSON(400, Error(fmt.Errorf("开始时间格式错误: %w", err)))
			return
		}
		filter.StartTime = &startTime
	}

	if req.EndTime != "" {
		endTime, err := time.Parse("2006-01-02 15:04:05", req.EndTime)
		if err != nil {
			c.JSON(400, Error(fmt.Errorf("结束时间格式错误: %w", err)))
			return
		}
		filter.EndTime = &endTime
	}

	data, err := h.auditService.ExportAuditLogs(c.Request.Context(), filter, req.Format)
	if err != nil {
		h.logger.Error("导出审计日志失败", elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	// 设置响应头
	filename := fmt.Sprintf("audit_logs_%s.%s", time.Now().Format("20060102150405"), req.Format)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	if req.Format == domain.ExportFormatCSV {
		c.Header("Content-Type", "text/csv")
	} else {
		c.Header("Content-Type", "application/json")
	}

	c.Data(200, c.GetHeader("Content-Type"), data)
}

// GenerateAuditReport 生成审计报告
// @Summary 生成审计报告
// @Tags 审计日志管理
// @Accept json
// @Produce json
// @Param body body GenerateAuditReportVO true "生成审计报告请求"
// @Success 200 {object} Result
// @Router /api/v1/cam/iam/audit/reports [post]
func (h *AuditHandler) GenerateAuditReport(c *gin.Context) {
	var req GenerateAuditReportVO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, Error(err))
		return
	}

	// 解析时间参数
	startTime, err := time.Parse("2006-01-02 15:04:05", req.StartTime)
	if err != nil {
		c.JSON(400, Error(fmt.Errorf("开始时间格式错误: %w", err)))
		return
	}

	endTime, err := time.Parse("2006-01-02 15:04:05", req.EndTime)
	if err != nil {
		c.JSON(400, Error(fmt.Errorf("结束时间格式错误: %w", err)))
		return
	}

	report, err := h.auditService.GenerateAuditReport(c.Request.Context(), &domain.AuditReportRequest{
		StartTime: &startTime,
		EndTime:   &endTime,
		TenantID:  req.TenantID,
	})

	if err != nil {
		h.logger.Error("生成审计报告失败", elog.FieldErr(err))
		c.JSON(500, Error(err))
		return
	}

	c.JSON(200, Success(report))
}

// RegisterRoutes 注册路由
func (h *AuditHandler) RegisterRoutes(r *gin.RouterGroup) {
	audit := r.Group("/audit")
	{
		audit.GET("/logs", h.ListAuditLogs)
		audit.GET("/logs/export", h.ExportAuditLogs)
		audit.POST("/reports", h.GenerateAuditReport)
	}
}
