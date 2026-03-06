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
	"github.com/Havens-blog/e-cam-service/internal/cam/web"
	"github.com/Havens-blog/e-cam-service/pkg/ginx"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// CostHandler 成本分析 API 处理器
type CostHandler struct {
	costSvc      *analysis.CostService
	anomalySvc   *anomaly.AnomalyService
	optimizerSvc *optimizer.OptimizerService
}

// NewCostHandler 创建成本分析处理器
func NewCostHandler(
	costSvc *analysis.CostService,
	anomalySvc *anomaly.AnomalyService,
	optimizerSvc *optimizer.OptimizerService,
) *CostHandler {
	return &CostHandler{
		costSvc:      costSvc,
		anomalySvc:   anomalySvc,
		optimizerSvc: optimizerSvc,
	}
}

// PrivateRoutes 注册成本分析相关路由
func (h *CostHandler) PrivateRoutes(server *gin.Engine) {
	g := server.Group("/api/v1/cam")
	g.GET("/cost/summary", h.GetCostSummary)
	g.GET("/cost/trend", h.GetCostTrend)
	g.GET("/cost/distribution", h.GetCostDistribution)
	g.GET("/cost/comparison", h.GetYoYComparison)
	g.GET("/cost/anomalies", h.GetAnomalyEvents)
	g.POST("/cost/anomalies/detect", h.TriggerAnomalyDetection)
	g.GET("/cost/recommendations", h.ListRecommendations)
	g.POST("/cost/recommendations/:id/dismiss", ginx.Wrap(h.DismissRecommendation))
}

// GetCostSummary 成本概览
func (h *CostHandler) GetCostSummary(ctx *gin.Context) {
	tenantID := ctx.GetString("tenant_id")
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

	summary, err := h.costSvc.GetCostSummary(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(summary))
}

// GetCostTrend 成本趋势
func (h *CostHandler) GetCostTrend(ctx *gin.Context) {
	tenantID := ctx.GetString("tenant_id")
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

	points, err := h.costSvc.GetCostTrend(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(points))
}

// GetCostDistribution 成本分布
func (h *CostHandler) GetCostDistribution(ctx *gin.Context) {
	tenantID := ctx.GetString("tenant_id")
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

	items, err := h.costSvc.GetCostDistribution(ctx.Request.Context(), filter, dimension)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(items))
}

// GetYoYComparison 同比环比
func (h *CostHandler) GetYoYComparison(ctx *gin.Context) {
	tenantID := ctx.GetString("tenant_id")
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

	result, err := h.costSvc.GetYoYComparison(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(result))
}

// TriggerAnomalyDetection 手动触发异常检测
func (h *CostHandler) TriggerAnomalyDetection(ctx *gin.Context) {
	tenantID := ctx.GetString("tenant_id")
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
	tenantID := ctx.GetString("tenant_id")
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
	tenantID := ctx.GetString("tenant_id")
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
