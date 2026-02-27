package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/middleware"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/gin-gonic/gin"
)

// DashboardHandler 仪表盘HTTP处理器
type DashboardHandler struct {
	dashboardSvc service.DashboardService
}

// NewDashboardHandler 创建仪表盘处理器
func NewDashboardHandler(dashboardSvc service.DashboardService) *DashboardHandler {
	return &DashboardHandler{dashboardSvc: dashboardSvc}
}

// RegisterRoutesWithGroup 注册仪表盘路由到指定路由组
func (h *DashboardHandler) RegisterRoutesWithGroup(rg *gin.RouterGroup) {
	rg.GET("/overview", h.Overview)
	rg.GET("/by-provider", h.ByProvider)
	rg.GET("/by-region", h.ByRegion)
	rg.GET("/by-asset-type", h.ByAssetType)
	rg.GET("/by-account", h.ByAccount)
	rg.GET("/expiring", h.Expiring)
}

// ==================== 响应结构体 ====================

// DashboardOverviewResp 仪表盘总览响应
type DashboardOverviewResp struct {
	Total      int64          `json:"total"`
	ByProvider []GroupCountVO `json:"by_provider"`
	ByType     []GroupCountVO `json:"by_type"`
	ByStatus   []GroupCountVO `json:"by_status"`
}

// GroupCountVO 分组统计视图对象
type GroupCountVO struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// ExpiringListResp 即将过期资源列表响应
type ExpiringListResp struct {
	Items []UnifiedAssetVO `json:"items"`
	Total int64            `json:"total"`
}

// DashboardGroupResp 分组统计响应
type DashboardGroupResp struct {
	Items []GroupCountVO `json:"items"`
}

// ==================== Swagger 响应类型 ====================

// DashboardOverviewResult 仪表盘总览响应（用于 Swagger）
type DashboardOverviewResult struct {
	Code int                   `json:"code" example:"200"`
	Msg  string                `json:"msg" example:"success"`
	Data DashboardOverviewResp `json:"data"`
}

// DashboardGroupResult 分组统计响应（用于 Swagger）
type DashboardGroupResult struct {
	Code int                `json:"code" example:"200"`
	Msg  string             `json:"msg" example:"success"`
	Data DashboardGroupResp `json:"data"`
}

// DashboardExpiringResult 即将过期资源响应（用于 Swagger）
type DashboardExpiringResult struct {
	Code int              `json:"code" example:"200"`
	Msg  string           `json:"msg" example:"success"`
	Data ExpiringListResp `json:"data"`
}

// ==================== Handler 方法 ====================

// Overview 获取资产总览
// @Summary 获取资产总览
// @Description 获取资产总数、按云厂商/类型/状态的分布统计
// @Tags 仪表盘
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Success 200 {object} DashboardOverviewResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/dashboard/overview [get]
func (h *DashboardHandler) Overview(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	overview, err := h.dashboardSvc.GetOverview(ctx.Request.Context(), tenantID)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(DashboardOverviewResp{
		Total:      overview.Total,
		ByProvider: toGroupCountVOs(overview.ByProvider),
		ByType:     toGroupCountVOs(overview.ByType),
		ByStatus:   toGroupCountVOs(overview.ByStatus),
	}))
}

// ByProvider 按云厂商统计
// @Summary 按云厂商统计资产数量
// @Description 返回各云厂商的资产数量分布
// @Tags 仪表盘
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Success 200 {object} DashboardGroupResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/dashboard/by-provider [get]
func (h *DashboardHandler) ByProvider(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	counts, err := h.dashboardSvc.CountByProvider(ctx.Request.Context(), tenantID)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(DashboardGroupResp{Items: toGroupCountVOs(counts)}))
}

// ByRegion 按地域统计
// @Summary 按地域统计资产数量
// @Description 返回各地域的资产数量分布
// @Tags 仪表盘
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Success 200 {object} DashboardGroupResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/dashboard/by-region [get]
func (h *DashboardHandler) ByRegion(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	counts, err := h.dashboardSvc.CountByRegion(ctx.Request.Context(), tenantID)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(DashboardGroupResp{Items: toGroupCountVOs(counts)}))
}

// ByAssetType 按资产类型统计
// @Summary 按资产类型统计数量
// @Description 返回各资产类型的数量分布
// @Tags 仪表盘
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Success 200 {object} DashboardGroupResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/dashboard/by-asset-type [get]
func (h *DashboardHandler) ByAssetType(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	counts, err := h.dashboardSvc.CountByAssetType(ctx.Request.Context(), tenantID)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(DashboardGroupResp{Items: toGroupCountVOs(counts)}))
}

// ByAccount 按云账号统计
// @Summary 按云账号统计资产数量
// @Description 返回各云账号的资产数量分布
// @Tags 仪表盘
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Success 200 {object} DashboardGroupResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/dashboard/by-account [get]
func (h *DashboardHandler) ByAccount(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	counts, err := h.dashboardSvc.CountByAccount(ctx.Request.Context(), tenantID)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(DashboardGroupResp{Items: toGroupCountVOs(counts)}))
}

// Expiring 获取即将过期的资源
// @Summary 获取即将过期的资源列表
// @Description 查询指定天数内即将过期的云资源
// @Tags 仪表盘
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param days query int false "过期天数(默认30)" default(30)
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} DashboardExpiringResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/dashboard/expiring [get]
func (h *DashboardHandler) Expiring(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	days, _ := strconv.Atoi(ctx.DefaultQuery("days", "30"))
	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	if days <= 0 {
		days = 30
	}

	instances, total, err := h.dashboardSvc.GetExpiringResources(
		ctx.Request.Context(), tenantID, days, int64(offset), int64(limit),
	)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(ExpiringListResp{
		Items: daoInstancesToVOs(instances),
		Total: total,
	}))
}

// ==================== 辅助函数 ====================

// toGroupCountVOs 转换分组统计结果
func toGroupCountVOs(counts []dao.GroupCount) []GroupCountVO {
	vos := make([]GroupCountVO, len(counts))
	for i, c := range counts {
		vos[i] = GroupCountVO{Key: c.Key, Count: c.Count}
	}
	return vos
}

// daoInstancesToVOs 将 DAO 层实例转换为统一资产视图
func daoInstancesToVOs(instances []dao.Instance) []UnifiedAssetVO {
	vos := make([]UnifiedAssetVO, len(instances))
	for i, inst := range instances {
		provider := ""
		region := ""
		status := ""
		if inst.Attributes != nil {
			if p, ok := inst.Attributes["provider"].(string); ok {
				provider = p
			}
			if r, ok := inst.Attributes["region"].(string); ok {
				region = r
			}
			if s, ok := inst.Attributes["status"].(string); ok {
				status = s
			}
		}
		vos[i] = UnifiedAssetVO{
			ID:         inst.ID,
			AssetID:    inst.AssetID,
			AssetName:  inst.AssetName,
			AssetType:  extractAssetType(inst.ModelUID),
			TenantID:   inst.TenantID,
			AccountID:  inst.AccountID,
			Provider:   provider,
			Region:     region,
			Status:     status,
			Attributes: inst.Attributes,
			CreateTime: inst.Ctime,
			UpdateTime: inst.Utime,
		}
	}
	return vos
}
