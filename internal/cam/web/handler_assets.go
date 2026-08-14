package web

import (
	"strconv"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/pkg/ginx"
	"github.com/gin-gonic/gin"
)

// CreateAsset 创建资产
// @Summary 创建资产
// @Description 创建新的云资产记录
// @Tags 资产管理
// @Accept json
// @Produce json
// @Param request body CreateAssetReq true "资产信息"
// @Success 200 {object} ginx.Result{data=CloudAsset} "成功"
// @Failure 400 {object} ginx.Result "请求参数错误"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/assets [post]
func (h *Handler) CreateAsset(ctx *gin.Context, req CreateAssetReq) (ginx.Result, error) {
	asset := h.toDomain(req)

	id, err := h.svc.CreateAsset(ctx.Request.Context(), asset)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(map[string]interface{}{
		"id": id,
	}), nil
}

// CreateMultiAssets 批量创建资产
// @Summary 批量创建资产
// @Description 批量创建多个云资产记录
// @Tags 资产管理
// @Accept json
// @Produce json
// @Param request body CreateMultiAssetsReq true "批量资产信息"
// @Success 200 {object} ginx.Result{data=[]CloudAsset} "成功"
// @Failure 400 {object} ginx.Result "请求参数错误"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/assets/batch [post]
func (h *Handler) CreateMultiAssets(ctx *gin.Context, req CreateMultiAssetsReq) (ginx.Result, error) {
	if len(req.Assets) == 0 {
		return ErrorResult(errs.ParamsError), nil
	}

	assets := make([]camdomain.CloudAsset, len(req.Assets))
	for i, assetReq := range req.Assets {
		assets[i] = h.toDomain(assetReq)
	}

	count, err := h.svc.CreateMultiAssets(ctx.Request.Context(), assets)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(map[string]interface{}{
		"count": count,
	}), nil
}

// UpdateAsset 更新资产
// @Summary 更新资产
// @Description 更新指定ID的云资产信息
// @Tags 资产管理
// @Accept json
// @Produce json
// @Param id path int true "资产ID"
// @Param request body UpdateAssetReq true "更新的资产信息"
// @Success 200 {object} ginx.Result{data=CloudAsset} "成功"
// @Failure 400 {object} ginx.Result "请求参数错误"
// @Failure 404 {object} ginx.Result "资产不存在"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/assets/{id} [put]
func (h *Handler) UpdateAsset(ctx *gin.Context, req UpdateAssetReq) (ginx.Result, error) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return ErrorResult(errs.ParamsError), nil
	}

	// 先获取现有资产
	existingAsset, err := h.svc.GetAssetById(ctx.Request.Context(), id)
	if err != nil {
		return ErrorResult(errs.AssetNotFound), nil
	}

	// 更新字段
	if req.AssetName != "" {
		existingAsset.AssetName = req.AssetName
	}
	if req.Status != "" {
		existingAsset.Status = req.Status
	}
	if req.Tags != nil {
		tags := make([]camdomain.Tag, len(req.Tags))
		for i, tag := range req.Tags {
			tags[i] = camdomain.Tag{Key: tag.Key, Value: tag.Value}
		}
		existingAsset.Tags = tags
	}
	if req.Metadata != "" {
		existingAsset.Metadata = req.Metadata
	}
	if req.Cost > 0 {
		existingAsset.Cost = req.Cost
	}

	err = h.svc.UpdateAsset(ctx.Request.Context(), existingAsset)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(nil), nil
}

// GetAssetById 根据ID获取资产
// @Summary 获取资产详情
// @Description 根据资产ID获取详细信息
// @Tags 资产管理
// @Accept json
// @Produce json
// @Param id path int true "资产ID"
// @Success 200 {object} ginx.Result{data=CloudAsset} "成功"
// @Failure 400 {object} ginx.Result "请求参数错误"
// @Failure 404 {object} ginx.Result "资产不存在"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/assets/{id} [get]
func (h *Handler) GetAssetById(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		ctx.JSON(400, ErrorResult(errs.ParamsError))
		return
	}

	asset, err := h.svc.GetAssetById(ctx.Request.Context(), id)
	if err != nil {
		ctx.JSON(404, ErrorResult(errs.AssetNotFound))
		return
	}

	ctx.JSON(200, Result(h.toAssetVO(asset)))
}

// ListAssets 获取资产列表
// @Summary 获取资产列表
// @Description 获取云资产列表，支持按云厂商、资产类型、状态等条件过滤
// @Tags 资产管理
// @Accept json
// @Produce json
// @Param provider query string false "云厂商" Enums(aliyun,aws,azure)
// @Param asset_type query string false "资产类型" Enums(ecs,rds,oss,vpc)
// @Param status query string false "资产状态" Enums(running,stopped,deleted)
// @Param region query string false "地域"
// @Param tenant_id query string false "租户ID"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} ginx.Result{data=[]CloudAsset} "成功"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/assets [get]
func (h *Handler) ListAssets(ctx *gin.Context) {
	// 从 query 参数获取过滤条件
	provider := ctx.Query("provider")
	assetType := ctx.Query("asset_type")
	region := ctx.Query("region")
	status := ctx.Query("status")
	assetName := ctx.Query("asset_name")

	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	filter := camdomain.AssetFilter{
		Provider:  provider,
		AssetType: assetType,
		Region:    region,
		Status:    status,
		AssetName: assetName,
		Offset:    int64(offset),
		Limit:     int64(limit),
	}

	assets, total, err := h.svc.ListAssets(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	assetVOs := make([]CloudAsset, len(assets))
	for i, asset := range assets {
		assetVOs[i] = h.toAssetVO(asset)
	}

	resp := AssetListResp{
		Assets: assetVOs,
		Total:  total,
	}

	ctx.JSON(200, Result(resp))
}

// DeleteAsset 删除资产
// @Summary 删除资产
// @Description 删除指定ID的云资产记录
// @Tags 资产管理
// @Accept json
// @Produce json
// @Param id path int true "资产ID"
// @Success 200 {object} ginx.Result "成功"
// @Failure 400 {object} ginx.Result "请求参数错误"
// @Failure 404 {object} ginx.Result "资产不存在"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/assets/{id} [delete]
func (h *Handler) DeleteAsset(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		ctx.JSON(400, ErrorResult(errs.ParamsError))
		return
	}

	err = h.svc.DeleteAsset(ctx.Request.Context(), id)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(nil))
}

// DiscoverAssets 发现资产
// @Summary 发现云资产
// @Description 从指定云厂商和地域发现新的云资产
// @Tags 资产发现
// @Accept json
// @Produce json
// @Param request body DiscoverAssetsReq true "发现资产请求"
// @Success 200 {object} ginx.Result{data=[]CloudAsset} "成功"
// @Failure 400 {object} ginx.Result "请求参数错误"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/assets/discover [post]
func (h *Handler) DiscoverAssets(ctx *gin.Context, req DiscoverAssetsReq) (ginx.Result, error) {
	assets, err := h.svc.DiscoverAssets(ctx.Request.Context(), req.Provider, req.Region, req.AssetTypes)
	if err != nil {
		return ErrorResultWithMsg(errs.DiscoveryFailed, err.Error()), nil
	}

	assetVOs := make([]CloudAsset, len(assets))
	for i, asset := range assets {
		assetVOs[i] = h.toAssetVO(asset)
	}

	return Result(map[string]any{
		"assets":      assetVOs,
		"count":       len(assetVOs),
		"asset_types": req.AssetTypes,
	}), nil
}

// SyncAssets 同步资产
// @Summary 同步云资产（已废弃）
// @Description 同步指定云账号的资产状态和信息。此接口已废弃，请使用 POST /api/v1/cam/cloud-accounts/{id}/sync
// @Tags 资产发现
// @Accept json
// @Produce json
// @Param request body SyncAssetsReq true "同步资产请求"
// @Success 200 {object} ginx.Result "成功"
// @Failure 400 {object} ginx.Result "请求参数错误"
// @Failure 404 {object} ginx.Result "云账号不存在"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Deprecated
// @Router /cam/assets/sync [post]
func (h *Handler) SyncAssets(ctx *gin.Context, req SyncAssetsReq) (ginx.Result, error) {
	synced, err := h.svc.SyncAssets(ctx.Request.Context(), req.AccountID, req.AssetTypes)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(map[string]any{
		"account_id":  req.AccountID,
		"asset_types": req.AssetTypes,
		"synced":      synced,
		"message":     "此接口已废弃，请使用 POST /api/v1/cam/cloud-accounts/{id}/sync",
	}), nil
}

// GetAssetStatistics 获取资产统计
// @Summary 获取资产统计
// @Description 获取云资产的统计信息，包括数量、类型分布等
// @Tags 统计分析
// @Accept json
// @Produce json
// @Success 200 {object} ginx.Result{data=object} "成功"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/assets/statistics [get]
func (h *Handler) GetAssetStatistics(ctx *gin.Context) {
	stats, err := h.svc.GetAssetStatistics(ctx.Request.Context())
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	resp := AssetStatisticsResp{
		TotalAssets:      stats.TotalAssets,
		ProviderStats:    stats.ProviderStats,
		AssetTypeStats:   stats.AssetTypeStats,
		RegionStats:      stats.RegionStats,
		StatusStats:      stats.StatusStats,
		TotalCost:        stats.TotalCost,
		LastDiscoverTime: stats.LastDiscoverTime,
	}

	ctx.JSON(200, Result(resp))
}

// GetCostAnalysis 获取成本分析
// @Summary 获取成本分析
// @Description 获取云资产的成本分析报告
// @Tags 统计分析
// @Accept json
// @Produce json
// @Param provider query string false "云厂商" Enums(aliyun,aws,azure)
// @Param days query int false "分析天数" default(30)
// @Success 200 {object} ginx.Result{data=object} "成功"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/assets/cost-analysis [get]
func (h *Handler) GetCostAnalysis(ctx *gin.Context) {
	provider := ctx.Query("provider")
	days, _ := strconv.Atoi(ctx.DefaultQuery("days", "30"))

	analysis, err := h.svc.GetCostAnalysis(ctx.Request.Context(), provider, days)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	resp := CostAnalysisResp{
		Provider:    analysis.Provider,
		TotalCost:   analysis.TotalCost,
		DailyCosts:  make([]DailyCost, len(analysis.DailyCosts)),
		AssetCosts:  make([]AssetCost, len(analysis.AssetCosts)),
		RegionCosts: analysis.RegionCosts,
	}

	for i, dc := range analysis.DailyCosts {
		resp.DailyCosts[i] = DailyCost{Date: dc.Date, Cost: dc.Cost}
	}

	for i, ac := range analysis.AssetCosts {
		resp.AssetCosts[i] = AssetCost{
			AssetId:   ac.AssetId,
			AssetName: ac.AssetName,
			AssetType: ac.AssetType,
			Cost:      ac.Cost,
		}
	}

	ctx.JSON(200, Result(resp))
}

// toDomain 将请求转换为领域模型
func (h *Handler) toDomain(req CreateAssetReq) camdomain.CloudAsset {
	tags := make([]camdomain.Tag, len(req.Tags))
	for i, tag := range req.Tags {
		tags[i] = camdomain.Tag{Key: tag.Key, Value: tag.Value}
	}

	return camdomain.CloudAsset{
		AssetId:      req.AssetId,
		AssetName:    req.AssetName,
		AssetType:    req.AssetType,
		Provider:     req.Provider,
		Region:       req.Region,
		Zone:         req.Zone,
		Status:       req.Status,
		Tags:         tags,
		Metadata:     req.Metadata,
		Cost:         req.Cost,
		CreateTime:   req.CreateTime,
		UpdateTime:   req.UpdateTime,
		DiscoverTime: req.DiscoverTime,
	}
}

// toAssetVO 将领域模型转换为VO
func (h *Handler) toAssetVO(asset camdomain.CloudAsset) CloudAsset {
	tags := make([]Tag, len(asset.Tags))
	for i, tag := range asset.Tags {
		tags[i] = Tag{Key: tag.Key, Value: tag.Value}
	}

	return CloudAsset{
		Id:           asset.Id,
		AssetId:      asset.AssetId,
		AssetName:    asset.AssetName,
		AssetType:    asset.AssetType,
		Provider:     asset.Provider,
		Region:       asset.Region,
		Zone:         asset.Zone,
		Status:       asset.Status,
		Tags:         tags,
		Metadata:     asset.Metadata,
		Cost:         asset.Cost,
		CreateTime:   asset.CreateTime,
		UpdateTime:   asset.UpdateTime,
		DiscoverTime: asset.DiscoverTime,
	}
}
