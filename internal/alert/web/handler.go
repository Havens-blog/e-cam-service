// Package web 告警通知 HTTP 处理器
package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/alert/domain"
	"github.com/Havens-blog/e-cam-service/internal/alert/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/middleware"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// AlertHandler 告警管理处理器
type AlertHandler struct {
	alertService *service.AlertService
	logger       *elog.Component
}

// NewAlertHandler 创建告警处理器
func NewAlertHandler(alertService *service.AlertService, logger *elog.Component) *AlertHandler {
	return &AlertHandler{alertService: alertService, logger: logger}
}

// RegisterRoutes 注册路由
func (h *AlertHandler) RegisterRoutes(r *gin.RouterGroup) {
	alert := r.Group("/alert")
	{
		// 告警规则
		rules := alert.Group("/rules")
		rules.POST("", h.CreateRule)
		rules.GET("", h.ListRules)
		rules.GET("/:id", h.GetRule)
		rules.PUT("/:id", h.UpdateRule)
		rules.DELETE("/:id", h.DeleteRule)
		rules.PUT("/:id/toggle", h.ToggleRule)

		// 告警事件
		events := alert.Group("/events")
		events.GET("", h.ListEvents)

		// 通知渠道
		channels := alert.Group("/channels")
		channels.POST("", h.CreateChannel)
		channels.GET("", h.ListChannels)
		channels.GET("/:id", h.GetChannel)
		channels.PUT("/:id", h.UpdateChannel)
		channels.DELETE("/:id", h.DeleteChannel)
		channels.POST("/:id/test", h.TestChannel)
	}
}

// ========== 告警规则 ==========

// CreateRule 创建告警规则
// @Summary 创建告警规则
// @Tags 告警管理
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param body body CreateRuleReq true "创建告警规则"
// @Success 200 {object} Result
// @Router /api/v1/cam/alert/rules [post]
func (h *AlertHandler) CreateRule(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var req CreateRuleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"code": 400, "msg": err.Error()})
		return
	}

	rule := domain.AlertRule{
		Name:             req.Name,
		Type:             domain.AlertType(req.Type),
		Condition:        req.Condition,
		ChannelIDs:       req.ChannelIDs,
		AccountIDs:       req.AccountIDs,
		ResourceTypes:    req.ResourceTypes,
		Regions:          req.Regions,
		SilenceDuration:  req.SilenceDuration,
		EscalateAfter:    req.EscalateAfter,
		EscalateChannels: req.EscalateChannels,
		TenantID:         tenantID,
	}

	id, err := h.alertService.CreateRule(c.Request.Context(), rule)
	if err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	c.JSON(200, gin.H{"code": 0, "msg": "success", "data": gin.H{"id": id}})
}

// ListRules 查询告警规则列表
// @Summary 查询告警规则列表
// @Tags 告警管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param type query string false "告警类型"
// @Param offset query int false "偏移量"
// @Param limit query int false "限制数量"
// @Success 200 {object} Result
// @Router /api/v1/cam/alert/rules [get]
func (h *AlertHandler) ListRules(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	filter := domain.AlertRuleFilter{
		TenantID: tenantID,
		Type:     domain.AlertType(c.Query("type")),
		Offset:   parseIntDefault(c.Query("offset"), 0),
		Limit:    parseIntDefault(c.Query("limit"), 20),
	}

	rules, total, err := h.alertService.ListRules(c.Request.Context(), filter)
	if err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	c.JSON(200, gin.H{"code": 0, "msg": "success", "data": gin.H{"items": rules, "total": total}})
}

// GetRule 获取告警规则详情
// @Summary 获取告警规则详情
// @Tags 告警管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "规则ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/alert/rules/{id} [get]
func (h *AlertHandler) GetRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"code": 400, "msg": "invalid id"})
		return
	}

	rule, err := h.alertService.GetRule(c.Request.Context(), id)
	if err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	c.JSON(200, gin.H{"code": 0, "msg": "success", "data": rule})
}

// UpdateRule 更新告警规则
// @Summary 更新告警规则
// @Tags 告警管理
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "规则ID"
// @Param body body CreateRuleReq true "更新告警规则"
// @Success 200 {object} Result
// @Router /api/v1/cam/alert/rules/{id} [put]
func (h *AlertHandler) UpdateRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"code": 400, "msg": "invalid id"})
		return
	}

	var req CreateRuleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"code": 400, "msg": err.Error()})
		return
	}

	rule := domain.AlertRule{
		ID:               id,
		Name:             req.Name,
		Type:             domain.AlertType(req.Type),
		Condition:        req.Condition,
		ChannelIDs:       req.ChannelIDs,
		AccountIDs:       req.AccountIDs,
		ResourceTypes:    req.ResourceTypes,
		Regions:          req.Regions,
		SilenceDuration:  req.SilenceDuration,
		EscalateAfter:    req.EscalateAfter,
		EscalateChannels: req.EscalateChannels,
	}

	if err := h.alertService.UpdateRule(c.Request.Context(), rule); err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	c.JSON(200, gin.H{"code": 0, "msg": "success"})
}

// DeleteRule 删除告警规则
// @Summary 删除告警规则
// @Tags 告警管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "规则ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/alert/rules/{id} [delete]
func (h *AlertHandler) DeleteRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"code": 400, "msg": "invalid id"})
		return
	}

	if err := h.alertService.DeleteRule(c.Request.Context(), id); err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	c.JSON(200, gin.H{"code": 0, "msg": "success"})
}

// ToggleRule 启用/禁用告警规则
// @Summary 启用/禁用告警规则
// @Tags 告警管理
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "规则ID"
// @Param body body ToggleRuleReq true "启用/禁用"
// @Success 200 {object} Result
// @Router /api/v1/cam/alert/rules/{id}/toggle [put]
func (h *AlertHandler) ToggleRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"code": 400, "msg": "invalid id"})
		return
	}

	var req ToggleRuleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"code": 400, "msg": err.Error()})
		return
	}

	if err := h.alertService.ToggleRule(c.Request.Context(), id, req.Enabled); err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	c.JSON(200, gin.H{"code": 0, "msg": "success"})
}

// ========== 告警事件 ==========

// ListEvents 查询告警事件列表
// @Summary 查询告警事件列表
// @Tags 告警管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param type query string false "告警类型"
// @Param severity query string false "告警级别"
// @Param status query string false "事件状态"
// @Param offset query int false "偏移量"
// @Param limit query int false "限制数量"
// @Success 200 {object} Result
// @Router /api/v1/cam/alert/events [get]
func (h *AlertHandler) ListEvents(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	filter := domain.AlertEventFilter{
		TenantID: tenantID,
		Type:     domain.AlertType(c.Query("type")),
		Severity: domain.Severity(c.Query("severity")),
		Status:   domain.EventStatus(c.Query("status")),
		Offset:   parseIntDefault(c.Query("offset"), 0),
		Limit:    parseIntDefault(c.Query("limit"), 20),
	}

	events, total, err := h.alertService.ListEvents(c.Request.Context(), filter)
	if err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	c.JSON(200, gin.H{"code": 0, "msg": "success", "data": gin.H{"items": events, "total": total}})
}

// ========== 通知渠道 ==========

// CreateChannel 创建通知渠道
// @Summary 创建通知渠道
// @Tags 告警管理
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param body body CreateChannelReq true "创建通知渠道"
// @Success 200 {object} Result
// @Router /api/v1/cam/alert/channels [post]
func (h *AlertHandler) CreateChannel(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var req CreateChannelReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"code": 400, "msg": err.Error()})
		return
	}

	ch := domain.NotificationChannel{
		Name:     req.Name,
		Type:     domain.ChannelType(req.Type),
		Config:   req.Config,
		TenantID: tenantID,
	}

	id, err := h.alertService.CreateChannel(c.Request.Context(), ch)
	if err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	c.JSON(200, gin.H{"code": 0, "msg": "success", "data": gin.H{"id": id}})
}

// ListChannels 查询通知渠道列表
// @Summary 查询通知渠道列表
// @Tags 告警管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param type query string false "渠道类型"
// @Param offset query int false "偏移量"
// @Param limit query int false "限制数量"
// @Success 200 {object} Result
// @Router /api/v1/cam/alert/channels [get]
func (h *AlertHandler) ListChannels(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	filter := domain.ChannelFilter{
		TenantID: tenantID,
		Type:     domain.ChannelType(c.Query("type")),
		Offset:   parseIntDefault(c.Query("offset"), 0),
		Limit:    parseIntDefault(c.Query("limit"), 20),
	}

	channels, total, err := h.alertService.ListChannels(c.Request.Context(), filter)
	if err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	c.JSON(200, gin.H{"code": 0, "msg": "success", "data": gin.H{"items": channels, "total": total}})
}

// GetChannel 获取通知渠道详情
// @Summary 获取通知渠道详情
// @Tags 告警管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "渠道ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/alert/channels/{id} [get]
func (h *AlertHandler) GetChannel(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"code": 400, "msg": "invalid id"})
		return
	}

	ch, err := h.alertService.GetChannel(c.Request.Context(), id)
	if err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	c.JSON(200, gin.H{"code": 0, "msg": "success", "data": ch})
}

// UpdateChannel 更新通知渠道
// @Summary 更新通知渠道
// @Tags 告警管理
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "渠道ID"
// @Param body body CreateChannelReq true "更新通知渠道"
// @Success 200 {object} Result
// @Router /api/v1/cam/alert/channels/{id} [put]
func (h *AlertHandler) UpdateChannel(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"code": 400, "msg": "invalid id"})
		return
	}

	var req CreateChannelReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"code": 400, "msg": err.Error()})
		return
	}

	ch := domain.NotificationChannel{
		ID:     id,
		Name:   req.Name,
		Type:   domain.ChannelType(req.Type),
		Config: req.Config,
	}

	if err := h.alertService.UpdateChannel(c.Request.Context(), ch); err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	c.JSON(200, gin.H{"code": 0, "msg": "success"})
}

// DeleteChannel 删除通知渠道
// @Summary 删除通知渠道
// @Tags 告警管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "渠道ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/alert/channels/{id} [delete]
func (h *AlertHandler) DeleteChannel(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"code": 400, "msg": "invalid id"})
		return
	}

	if err := h.alertService.DeleteChannel(c.Request.Context(), id); err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	c.JSON(200, gin.H{"code": 0, "msg": "success"})
}

// TestChannel 测试通知渠道
// @Summary 测试通知渠道
// @Tags 告警管理
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "渠道ID"
// @Success 200 {object} Result
// @Router /api/v1/cam/alert/channels/{id}/test [post]
func (h *AlertHandler) TestChannel(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"code": 400, "msg": "invalid id"})
		return
	}

	if err := h.alertService.TestChannel(c.Request.Context(), id); err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	c.JSON(200, gin.H{"code": 0, "msg": "success"})
}

// ========== 辅助函数 ==========

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
