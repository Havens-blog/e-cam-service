// Package web 审计模块 HTTP 处理器
package web

import (
	"github.com/Havens-blog/e-cam-service/internal/audit/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/audit/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// AuditEventHandler 统一审计事件查询处理器
type AuditEventHandler struct {
	eventSvc *service.AuditEventService
	logger   *elog.Component
}

// NewAuditEventHandler 创建统一审计事件查询处理器
func NewAuditEventHandler(eventSvc *service.AuditEventService, logger *elog.Component) *AuditEventHandler {
	return &AuditEventHandler{eventSvc: eventSvc, logger: logger}
}

// RegisterRoutes 注册统一审计事件路由
func (h *AuditEventHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/events", h.ListEvents)
}

// ListEvents 跨域查询统一审计事件
//
// @Summary 跨域查询统一审计事件
// @Tags 审计管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param actor_id query string false "操作者ID"
// @Param service query string false "所属服务(e-cam-service|e-iam)" Enums(e-cam-service, e-iam)
// @Param domain query string false "业务域(audit|operation|change)" Enums(audit, operation, change)
// @Param action query string false "动作(create/update/delete/sync/execute…)"
// @Param result query string false "结果(success|failed|partial)" Enums(success, failed, partial)
// @Param resource_type query string false "资源类型(api/asset/user/role…)"
// @Param resource_id query string false "资源ID"
// @Param start_time query int false "开始时间(Unix毫秒)"
// @Param end_time query int false "结束时间(Unix毫秒)"
// @Param offset query int false "偏移量"
// @Param limit query int false "限制数量(≤1000)"
// @Param order query string false "排序方向(asc|desc，默认 desc)" Enums(asc, desc)
// @Success 200 {object} gin.H
// @Router /api/v1/cam/audit/events [get]
func (h *AuditEventHandler) ListEvents(c *gin.Context) {
	// 租户恒从登录上下文注入：统一审计跨域聚合，绝不允许调用方自报租户
	// 拉取他人审计（与 ListAuditLogs 的 GetTenantID 口径一致）。
	tenantID := middleware.GetTenantID(c)
	filter := dao.AuditEventFilter{
		TenantID:     tenantID,
		ActorID:      c.Query("actor_id"),
		Service:      c.Query("service"),
		Domain:       c.Query("domain"),
		Action:       c.Query("action"),
		Result:       c.Query("result"),
		ResourceType: c.Query("resource_type"),
		ResourceID:   c.Query("resource_id"),
		Offset:       parseIntDefault(c.Query("offset"), 0),
		Limit:        min(parseIntDefault(c.Query("limit"), 20), int64(maxPageLimit)),
		Order:        c.Query("order"),
	}
	if st := c.Query("start_time"); st != "" {
		v := parseIntDefault(st, 0)
		filter.StartTime = &v
	}
	if et := c.Query("end_time"); et != "" {
		v := parseIntDefault(et, 0)
		filter.EndTime = &v
	}

	events, total, err := h.eventSvc.ListEvents(c.Request.Context(), &filter)
	if err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 0, "msg": "success", "data": gin.H{"items": events, "total": total}})
}
