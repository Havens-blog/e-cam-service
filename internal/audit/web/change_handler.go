package web

import (
	"github.com/Havens-blog/e-cam-service/internal/audit/domain"
	"github.com/Havens-blog/e-cam-service/internal/audit/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// ChangeHandler 变更历史查询处理器
type ChangeHandler struct {
	tracker *service.ChangeTracker
	logger  *elog.Component
}

// NewChangeHandler 创建变更历史查询处理器
func NewChangeHandler(tracker *service.ChangeTracker, logger *elog.Component) *ChangeHandler {
	return &ChangeHandler{tracker: tracker, logger: logger}
}

// ListAssetChanges 查询资产变更历史
// @Summary 查询资产变更历史
// @Tags 审计变更
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param asset_id query string true "资产ID"
// @Param field_name query string false "变更字段名"
// @Param start_time query int false "开始时间(Unix毫秒)"
// @Param end_time query int false "结束时间(Unix毫秒)"
// @Param offset query int false "偏移量"
// @Param limit query int false "限制数量"
// @Success 200 {object} gin.H
// @Router /api/v1/cam/audit/changes [get]
func (h *ChangeHandler) ListAssetChanges(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	assetID := c.Query("asset_id")

	filter := domain.ChangeFilter{
		AssetID:   assetID,
		TenantID:  tenantID,
		FieldName: c.Query("field_name"),
		Offset:    parseIntDefault(c.Query("offset"), 0),
		Limit:     parseIntDefault(c.Query("limit"), 20),
	}
	if st := c.Query("start_time"); st != "" {
		v := parseIntDefault(st, 0)
		filter.StartTime = &v
	}
	if et := c.Query("end_time"); et != "" {
		v := parseIntDefault(et, 0)
		filter.EndTime = &v
	}

	records, total, err := h.tracker.ListByAssetID(c.Request.Context(), filter)
	if err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 0, "msg": "success", "data": gin.H{"items": records, "total": total}})
}

// GetChangeSummary 获取变更统计汇总
// @Summary 获取变更统计汇总
// @Tags 资产变更
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param model_uid query string false "模型UID"
// @Param provider query string false "云厂商"
// @Param start_time query int false "开始时间(Unix毫秒)"
// @Param end_time query int false "结束时间(Unix毫秒)"
// @Success 200 {object} gin.H
// @Router /api/v1/cam/audit/changes/summary [get]
func (h *ChangeHandler) GetChangeSummary(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	filter := domain.ChangeFilter{
		TenantID: tenantID,
		ModelUID: c.Query("model_uid"),
		Provider: c.Query("provider"),
	}
	if st := c.Query("start_time"); st != "" {
		v := parseIntDefault(st, 0)
		filter.StartTime = &v
	}
	if et := c.Query("end_time"); et != "" {
		v := parseIntDefault(et, 0)
		filter.EndTime = &v
	}

	summary, err := h.tracker.GetSummary(c.Request.Context(), filter)
	if err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 0, "msg": "success", "data": summary})
}
