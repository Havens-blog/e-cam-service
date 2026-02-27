// Package web 审计模块 HTTP 处理器
package web

import (
	"fmt"
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/audit/domain"
	"github.com/Havens-blog/e-cam-service/internal/audit/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// AuditHandler 审计日志查询处理器
type AuditHandler struct {
	auditSvc *service.AuditService
	logger   *elog.Component
}

// NewAuditHandler 创建审计日志查询处理器
func NewAuditHandler(auditSvc *service.AuditService, logger *elog.Component) *AuditHandler {
	return &AuditHandler{auditSvc: auditSvc, logger: logger}
}

// RegisterRoutes 注册审计日志路由
func (h *AuditHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/logs", h.ListAuditLogs)
	r.GET("/logs/export", h.ExportAuditLogs)
	r.POST("/reports", h.GenerateReport)
}

// ListAuditLogs 查询审计日志列表
// @Summary 查询审计日志列表
// @Tags 审计管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param operation_type query string false "操作类型"
// @Param operator_id query string false "操作人ID"
// @Param http_method query string false "HTTP方法"
// @Param api_path query string false "API路径(前缀匹配)"
// @Param request_id query string false "请求ID"
// @Param status_code query int false "响应状态码"
// @Param start_time query int false "开始时间(Unix毫秒)"
// @Param end_time query int false "结束时间(Unix毫秒)"
// @Param offset query int false "偏移量"
// @Param limit query int false "限制数量"
// @Success 200 {object} gin.H
// @Router /api/v1/cam/audit/logs [get]
func (h *AuditHandler) ListAuditLogs(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	filter := domain.AuditLogFilter{
		TenantID:      tenantID,
		OperationType: domain.AuditOperationType(c.Query("operation_type")),
		OperatorID:    c.Query("operator_id"),
		HTTPMethod:    c.Query("http_method"),
		APIPath:       c.Query("api_path"),
		RequestID:     c.Query("request_id"),
		StatusCode:    int(parseIntDefault(c.Query("status_code"), 0)),
		Offset:        parseIntDefault(c.Query("offset"), 0),
		Limit:         parseIntDefault(c.Query("limit"), 20),
	}
	if st := c.Query("start_time"); st != "" {
		v := parseIntDefault(st, 0)
		filter.StartTime = &v
	}
	if et := c.Query("end_time"); et != "" {
		v := parseIntDefault(et, 0)
		filter.EndTime = &v
	}

	logs, total, err := h.auditSvc.ListAuditLogs(c.Request.Context(), filter)
	if err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 0, "msg": "success", "data": gin.H{"items": logs, "total": total}})
}

// ExportAuditLogs 导出审计日志
// @Summary 导出审计日志
// @Tags 审计管理
// @Produce octet-stream
// @Param X-Tenant-ID header string true "租户ID"
// @Param format query string false "导出格式(csv/json)" default(json)
// @Param operation_type query string false "操作类型"
// @Param start_time query int false "开始时间(Unix毫秒)"
// @Param end_time query int false "结束时间(Unix毫秒)"
// @Success 200 {file} file
// @Router /api/v1/cam/audit/logs/export [get]
func (h *AuditHandler) ExportAuditLogs(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	format := c.DefaultQuery("format", "json")

	filter := domain.AuditLogFilter{
		TenantID:      tenantID,
		OperationType: domain.AuditOperationType(c.Query("operation_type")),
		OperatorID:    c.Query("operator_id"),
		HTTPMethod:    c.Query("http_method"),
		APIPath:       c.Query("api_path"),
	}
	if st := c.Query("start_time"); st != "" {
		v := parseIntDefault(st, 0)
		filter.StartTime = &v
	}
	if et := c.Query("end_time"); et != "" {
		v := parseIntDefault(et, 0)
		filter.EndTime = &v
	}

	data, err := h.auditSvc.ExportAuditLogs(c.Request.Context(), filter, format)
	if err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	var contentType, ext string
	if format == "csv" {
		contentType = "text/csv; charset=utf-8"
		ext = "csv"
	} else {
		contentType = "application/json; charset=utf-8"
		ext = "json"
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=audit_logs.%s", ext))
	c.Data(200, contentType, data)
}

// GenerateReport 生成审计报告
// @Summary 生成审计报告
// @Tags 审计管理
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param body body AuditReportReq true "报告参数"
// @Success 200 {object} gin.H
// @Router /api/v1/cam/audit/reports [post]
func (h *AuditHandler) GenerateReport(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var req AuditReportReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"code": 400, "msg": err.Error()})
		return
	}

	report, err := h.auditSvc.GenerateReport(c.Request.Context(), tenantID, req.StartTime, req.EndTime)
	if err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 0, "msg": "success", "data": report})
}

func parseIntDefault(s string, defaultVal int64) int64 {
	if s == "" {
		return defaultVal
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return defaultVal
	}
	return v
}
