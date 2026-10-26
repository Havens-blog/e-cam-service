// Package handler HTTP API 处理器
package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/analysis"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/anomaly"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/optimizer"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	camservice "github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-common-go/ginx"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// CostHandler 成本分析 API 处理器
type CostHandler struct {
	costSvc      *analysis.CostService
	anomalySvc   *anomaly.AnomalyService
	optimizerSvc *optimizer.OptimizerService
	cdnCostSvc   *camservice.CDNCostService
}

// NewCostHandler 创建成本分析处理器
func NewCostHandler(
	costSvc *analysis.CostService,
	anomalySvc *anomaly.AnomalyService,
	optimizerSvc *optimizer.OptimizerService,
	cdnCostSvc *camservice.CDNCostService,
) *CostHandler {
	return &CostHandler{
		costSvc:      costSvc,
		anomalySvc:   anomalySvc,
		optimizerSvc: optimizerSvc,
		cdnCostSvc:   cdnCostSvc,
	}
}

// PrivateRoutes 注册成本分析相关路由
func (h *CostHandler) PrivateRoutes(server *gin.Engine) {
	g := server.Group("/api/v1/cam")
	// 本组全部端点都以 middleware.GetTenantID(ctx) 构造 filter，且组内**没有**任何
	// handler 级的 tenantID==0 判空，故此处的 RequireTenant 不是重复校验而是唯一防线。
	// 若放行 tenant_id=0（eiam 的「等待选择租户」临时凭证），DAO 的
	// `if filter.TenantID != 0` 会丢弃租户谓词，返回全部租户的成本数据。
	g.Use(middleware.RequireTenant(elog.DefaultLogger))
	g.GET("/cost/summary", h.GetCostSummary)
	g.GET("/cost/cdn", h.GetCDNCost)
	g.GET("/cost/trend", h.GetCostTrend)
	g.GET("/cost/distribution", h.GetCostDistribution)
	g.GET("/cost/comparison", h.GetYoYComparison)
	g.GET("/cost/anomalies", h.GetAnomalyEvents)
	g.POST("/cost/anomalies/detect", h.TriggerAnomalyDetection)
	g.GET("/cost/recommendations", h.ListRecommendations)
	g.POST("/cost/recommendations/:id/dismiss", ginx.Wrap(h.DismissRecommendation))
	// 运维入口：刷新本租户的成本汇总（手工补采后免重启）。租户级，见 handler 注释。
	g.POST("/cost/summary/refresh", h.RefreshCostSummary)
}

// GetCostSummary 成本概览
func (h *CostHandler) GetCostSummary(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	filter := analysis.CostFilter{
		TenantID:    tenantID,
		Provider:    ctx.Query("provider"),
		ServiceType: ctx.Query("service_type"),
		Region:      ctx.Query("region"),
		StartDate:   ctx.Query("start_date"),
		EndDate:     ctx.Query("end_date"),
	}
	if aid := ctx.Query("account_id"); aid != "" {
		filter.AccountID, _ = strconv.ParseInt(aid, 10, 64)
	}

	reqCtx, cancel := context.WithTimeout(ctx.Request.Context(), 15*time.Second)
	defer cancel()

	summary, err := h.costSvc.GetCostSummary(reqCtx, filter)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(summary))
}

// GetCostTrend 成本趋势
func (h *CostHandler) GetCostTrend(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	filter := analysis.CostTrendFilter{
		CostFilter: analysis.CostFilter{
			TenantID:    tenantID,
			Provider:    ctx.Query("provider"),
			ServiceType: ctx.Query("service_type"),
			Region:      ctx.Query("region"),
			StartDate:   ctx.Query("start_date"),
			EndDate:     ctx.Query("end_date"),
		},
		Granularity: ctx.DefaultQuery("granularity", "daily"),
	}
	if aid := ctx.Query("account_id"); aid != "" {
		filter.AccountID, _ = strconv.ParseInt(aid, 10, 64)
	}

	reqCtx, cancel := context.WithTimeout(ctx.Request.Context(), 15*time.Second)
	defer cancel()

	points, err := h.costSvc.GetCostTrend(reqCtx, filter)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(points))
}

// GetCostDistribution 成本分布
func (h *CostHandler) GetCostDistribution(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	dimension := ctx.DefaultQuery("dimension", "provider")
	filter := analysis.CostFilter{
		TenantID:    tenantID,
		Provider:    ctx.Query("provider"),
		ServiceType: ctx.Query("service_type"),
		Region:      ctx.Query("region"),
		StartDate:   ctx.Query("start_date"),
		EndDate:     ctx.Query("end_date"),
	}
	if aid := ctx.Query("account_id"); aid != "" {
		filter.AccountID, _ = strconv.ParseInt(aid, 10, 64)
	}

	reqCtx, cancel := context.WithTimeout(ctx.Request.Context(), 15*time.Second)
	defer cancel()

	items, err := h.costSvc.GetCostDistribution(reqCtx, filter, dimension)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(items))
}

// GetYoYComparison 同比环比
func (h *CostHandler) GetYoYComparison(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	filter := analysis.CostFilter{
		TenantID:    tenantID,
		Provider:    ctx.Query("provider"),
		ServiceType: ctx.Query("service_type"),
		Region:      ctx.Query("region"),
		StartDate:   ctx.Query("start_date"),
		EndDate:     ctx.Query("end_date"),
	}
	if aid := ctx.Query("account_id"); aid != "" {
		filter.AccountID, _ = strconv.ParseInt(aid, 10, 64)
	}

	reqCtx, cancel := context.WithTimeout(ctx.Request.Context(), 15*time.Second)
	defer cancel()

	result, err := h.costSvc.GetYoYComparison(reqCtx, filter)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(result))
}

// TriggerAnomalyDetection 手动触发异常检测
func (h *CostHandler) TriggerAnomalyDetection(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	go func() {
		bgCtx := context.Background()
		if err := h.anomalySvc.DetectAnomalies(bgCtx, tenantID, yesterday); err != nil {
			elog.Error("manual anomaly detection failed", elog.FieldErr(err))
		}
	}()

	ctx.JSON(http.StatusOK, web.Result(gin.H{"message": "异常检测已提交，请稍后刷新查看结果"}))
}

// GetAnomalyEvents 异常事件列表
func (h *CostHandler) GetAnomalyEvents(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	filter := repository.AnomalyFilter{
		Dimension: ctx.Query("dimension"),
		Severity:  ctx.Query("severity"),
		StartDate: ctx.Query("start_date"),
		EndDate:   ctx.Query("end_date"),
		SortBy:    ctx.DefaultQuery("sort_by", "severity"),
		Offset:    int64(offset),
		Limit:     int64(limit),
	}

	anomalies, total, err := h.anomalySvc.GetAnomalyEvents(ctx.Request.Context(), tenantID, filter)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(gin.H{
		"items": anomalies,
		"total": total,
	}))
}

// ListRecommendations 优化建议列表
func (h *CostHandler) ListRecommendations(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	filter := repository.RecommendationFilter{
		Type:     ctx.Query("type"),
		Provider: ctx.Query("provider"),
		Status:   ctx.Query("status"),
		Offset:   int64(offset),
		Limit:    int64(limit),
	}

	recs, total, err := h.optimizerSvc.ListRecommendations(ctx.Request.Context(), tenantID, filter)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(gin.H{
		"items": recs,
		"total": total,
	}))
}

// DismissRecommendation 忽略建议
func (h *CostHandler) DismissRecommendation(ctx *gin.Context) (ginx.Result, error) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return web.ErrorResult(errs.ParamsError), nil
	}

	if err := h.optimizerSvc.DismissRecommendation(ctx.Request.Context(), id); err != nil {
		return web.ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}
	return web.Result(nil), nil
}

// RefreshCostSummary 刷新当前租户的成本汇总（运维入口）。
//
// 用途：手工补采/回补账单后立即让预聚合口径与明细表对齐，无需重启服务。
// 只作用于调用方租户（DAO 侧 ReconcileTenant 显式拒绝 tenant_id=0），无跨租户影响；
// 组上的 RequireTenant 已保证 tenantID 非 0，故这里不会退化成全库重建。
func (h *CostHandler) RefreshCostSummary(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	if tenantID == 0 {
		// 双重保险：组上的 RequireTenant 已拦截，但本端点会触发全表聚合，不能让
		// 「tenant_id=0 即不过滤租户」的 DAO 语义在这里退化成全库重建。
		ctx.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "当前会话未选定租户空间",
		})
		return
	}

	// 全量对账要扫明细表并可能重建，超时给得比常规查询宽。
	reqCtx, cancel := context.WithTimeout(ctx.Request.Context(), 5*time.Minute)
	defer cancel()

	rebuilt, err := h.costSvc.ReconcileSummary(reqCtx, tenantID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(gin.H{
		"tenant_id": tenantID,
		"rebuilt":   rebuilt, // false = 账期已一致、无需重建
	}))
}
