package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/internal/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/internal/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/internal/service"
	"github.com/Havens-blog/e-cam-service/pkg/ginx"
	"github.com/gin-gonic/gin"
)

// Handler CAM HTTP处理器
type Handler struct {
	svc        service.Service
	accountSvc service.CloudAccountService
}

// NewHandler 创建CAM处理器
func NewHandler(svc service.Service, accountSvc service.CloudAccountService) *Handler {
	return &Handler{
		svc:        svc,
		accountSvc: accountSvc,
	}
}

// PrivateRoutes 注册私有路由
func (h *Handler) PrivateRoutes(server *gin.Engine) {
	camGroup := server.Group("/api/v1/cam")
	{
		// 资产管理
		camGroup.POST("/assets", ginx.WrapBody[CreateAssetReq](h.CreateAsset))
		camGroup.POST("/assets/batch", ginx.WrapBody[CreateMultiAssetsReq](h.CreateMultiAssets))
		camGroup.PUT("/assets", ginx.WrapBody[UpdateAssetReq](h.UpdateAsset))
		camGroup.GET("/assets/:id", h.GetAssetById)
		camGroup.POST("/assets/list", ginx.WrapBody[ListAssetsReq](h.ListAssets))
		camGroup.DELETE("/assets/:id", h.DeleteAsset)

		// 资产发现
		camGroup.POST("/discover", ginx.WrapBody[DiscoverAssetsReq](h.DiscoverAssets))
		camGroup.POST("/sync", ginx.WrapBody[SyncAssetsReq](h.SyncAssets))

		// 统计分析
		camGroup.GET("/statistics", h.GetAssetStatistics)
		camGroup.POST("/cost-analysis", ginx.WrapBody[CostAnalysisReq](h.GetCostAnalysis))

		// 云账号管理
		camGroup.POST("/cloudaccounts", ginx.WrapBody[CreateCloudAccountReq](h.CreateCloudAccount))
		camGroup.GET("/cloudaccounts/:id", h.GetCloudAccount)
		camGroup.POST("/cloudaccounts/list", ginx.WrapBody[ListCloudAccountsReq](h.ListCloudAccounts))
		camGroup.PUT("/cloudaccounts/:id", ginx.WrapBody[UpdateCloudAccountReq](h.UpdateCloudAccount))
		camGroup.DELETE("/cloudaccounts/:id", h.DeleteCloudAccount)

		// 云账号操作
		camGroup.POST("/cloudaccounts/:id/test", h.TestCloudAccountConnection)
		camGroup.POST("/cloudaccounts/:id/enable", h.EnableCloudAccount)
		camGroup.POST("/cloudaccounts/:id/disable", h.DisableCloudAccount)
		camGroup.POST("/cloudaccounts/:id/sync", ginx.WrapBody[SyncAccountReq](h.SyncCloudAccount))
	}
}

// CreateAsset 创建资产
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
func (h *Handler) CreateMultiAssets(ctx *gin.Context, req CreateMultiAssetsReq) (ginx.Result, error) {
	if len(req.Assets) == 0 {
		return ErrorResult(errs.ParamsError), nil
	}

	assets := make([]domain.CloudAsset, len(req.Assets))
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
func (h *Handler) UpdateAsset(ctx *gin.Context, req UpdateAssetReq) (ginx.Result, error) {
	// 先获取现有资产
	existingAsset, err := h.svc.GetAssetById(ctx.Request.Context(), req.Id)
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
		tags := make([]domain.Tag, len(req.Tags))
		for i, tag := range req.Tags {
			tags[i] = domain.Tag{Key: tag.Key, Value: tag.Value}
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
func (h *Handler) ListAssets(ctx *gin.Context, req ListAssetsReq) (ginx.Result, error) {
	filter := domain.AssetFilter{
		Provider:  req.Provider,
		AssetType: req.AssetType,
		Region:    req.Region,
		Status:    req.Status,
		AssetName: req.AssetName,
		Offset:    req.Offset,
		Limit:     req.Limit,
	}

	assets, total, err := h.svc.ListAssets(ctx.Request.Context(), filter)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	assetVOs := make([]CloudAsset, len(assets))
	for i, asset := range assets {
		assetVOs[i] = h.toAssetVO(asset)
	}

	resp := AssetListResp{
		Assets: assetVOs,
		Total:  total,
	}

	return Result(resp), nil
}

// DeleteAsset 删除资产
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
func (h *Handler) DiscoverAssets(ctx *gin.Context, req DiscoverAssetsReq) (ginx.Result, error) {
	assets, err := h.svc.DiscoverAssets(ctx.Request.Context(), req.Provider, req.Region)
	if err != nil {
		return ErrorResultWithMsg(errs.DiscoveryFailed, err.Error()), nil
	}

	assetVOs := make([]CloudAsset, len(assets))
	for i, asset := range assets {
		assetVOs[i] = h.toAssetVO(asset)
	}

	return Result(map[string]interface{}{
		"assets": assetVOs,
		"count":  len(assetVOs),
	}), nil
}

// SyncAssets 同步资产
func (h *Handler) SyncAssets(ctx *gin.Context, req SyncAssetsReq) (ginx.Result, error) {
	err := h.svc.SyncAssets(ctx.Request.Context(), req.Provider)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(nil), nil
}

// GetAssetStatistics 获取资产统计
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
func (h *Handler) GetCostAnalysis(ctx *gin.Context, req CostAnalysisReq) (ginx.Result, error) {
	analysis, err := h.svc.GetCostAnalysis(ctx.Request.Context(), req.Provider, req.Days)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
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

	return Result(resp), nil
}

// toDomain 将请求转换为领域模型
func (h *Handler) toDomain(req CreateAssetReq) domain.CloudAsset {
	tags := make([]domain.Tag, len(req.Tags))
	for i, tag := range req.Tags {
		tags[i] = domain.Tag{Key: tag.Key, Value: tag.Value}
	}

	return domain.CloudAsset{
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
func (h *Handler) toAssetVO(asset domain.CloudAsset) CloudAsset {
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

// ==================== 云账号处理器 ====================

// CreateCloudAccount 创建云账号
func (h *Handler) CreateCloudAccount(ctx *gin.Context, req CreateCloudAccountReq) (ginx.Result, error) {
	domainReq := &domain.CreateCloudAccountRequest{
		Name:            req.Name,
		Provider:        domain.CloudProvider(req.Provider),
		Environment:     domain.Environment(req.Environment),
		AccessKeyID:     req.AccessKeyID,
		AccessKeySecret: req.AccessKeySecret,
		Region:          req.Region,
		Description:     req.Description,
		Config: domain.CloudAccountConfig{
			EnableAutoSync:       req.Config.EnableAutoSync,
			SyncInterval:         req.Config.SyncInterval,
			ReadOnly:             req.Config.ReadOnly,
			ShowSubAccounts:      req.Config.ShowSubAccounts,
			EnableCostMonitoring: req.Config.EnableCostMonitoring,
			SupportedRegions:     req.Config.SupportedRegions,
			SupportedAssetTypes:  req.Config.SupportedAssetTypes,
		},
		TenantID: req.TenantID,
	}

	account, err := h.accountSvc.CreateAccount(ctx.Request.Context(), domainReq)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(h.toCloudAccountVO(*account)), nil
}

// GetCloudAccount 获取云账号详情
func (h *Handler) GetCloudAccount(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		ctx.JSON(400, ErrorResult(errs.ParamsError))
		return
	}

	account, err := h.accountSvc.GetAccount(ctx.Request.Context(), id)
	if err != nil {
		ctx.JSON(404, ErrorResult(errs.AccountNotFound))
		return
	}

	ctx.JSON(200, Result(h.toCloudAccountVO(*account)))
}

// ListCloudAccounts 获取云账号列表
func (h *Handler) ListCloudAccounts(ctx *gin.Context, req ListCloudAccountsReq) (ginx.Result, error) {
	filter := domain.CloudAccountFilter{
		Provider:    domain.CloudProvider(req.Provider),
		Environment: domain.Environment(req.Environment),
		Status:      domain.CloudAccountStatus(req.Status),
		TenantID:    req.TenantID,
		Offset:      req.Offset,
		Limit:       req.Limit,
	}

	accounts, total, err := h.accountSvc.ListAccounts(ctx.Request.Context(), filter)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	accountVOs := make([]CloudAccount, len(accounts))
	for i, account := range accounts {
		accountVOs[i] = h.toCloudAccountVO(*account)
	}

	resp := CloudAccountListResp{
		Accounts: accountVOs,
		Total:    total,
	}

	return Result(resp), nil
}

// UpdateCloudAccount 更新云账号
func (h *Handler) UpdateCloudAccount(ctx *gin.Context, req UpdateCloudAccountReq) (ginx.Result, error) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return ErrorResult(errs.ParamsError), nil
	}

	domainReq := &domain.UpdateCloudAccountRequest{
		Name:        req.Name,
		Description: req.Description,
	}

	if req.Config != nil {
		domainReq.Config = &domain.CloudAccountConfig{
			EnableAutoSync:       req.Config.EnableAutoSync,
			SyncInterval:         req.Config.SyncInterval,
			ReadOnly:             req.Config.ReadOnly,
			ShowSubAccounts:      req.Config.ShowSubAccounts,
			EnableCostMonitoring: req.Config.EnableCostMonitoring,
			SupportedRegions:     req.Config.SupportedRegions,
			SupportedAssetTypes:  req.Config.SupportedAssetTypes,
		}
	}

	err = h.accountSvc.UpdateAccount(ctx.Request.Context(), id, domainReq)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(nil), nil
}

// DeleteCloudAccount 删除云账号
func (h *Handler) DeleteCloudAccount(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		ctx.JSON(400, ErrorResult(errs.ParamsError))
		return
	}

	err = h.accountSvc.DeleteAccount(ctx.Request.Context(), id)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(nil))
}

// TestCloudAccountConnection 测试云账号连接
func (h *Handler) TestCloudAccountConnection(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		ctx.JSON(400, ErrorResult(errs.ParamsError))
		return
	}

	result, err := h.accountSvc.TestConnection(ctx.Request.Context(), id)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	resp := ConnectionTestResult{
		Status:   result.Status,
		Message:  result.Message,
		Regions:  result.Regions,
		TestTime: result.TestTime,
	}

	ctx.JSON(200, Result(resp))
}

// EnableCloudAccount 启用云账号
func (h *Handler) EnableCloudAccount(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		ctx.JSON(400, ErrorResult(errs.ParamsError))
		return
	}

	err = h.accountSvc.EnableAccount(ctx.Request.Context(), id)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(nil))
}

// DisableCloudAccount 禁用云账号
func (h *Handler) DisableCloudAccount(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		ctx.JSON(400, ErrorResult(errs.ParamsError))
		return
	}

	err = h.accountSvc.DisableAccount(ctx.Request.Context(), id)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(nil))
}

// SyncCloudAccount 同步云账号资产
func (h *Handler) SyncCloudAccount(ctx *gin.Context, req SyncAccountReq) (ginx.Result, error) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return ErrorResult(errs.ParamsError), nil
	}

	domainReq := &domain.SyncAccountRequest{
		AssetTypes: req.AssetTypes,
		Regions:    req.Regions,
	}

	result, err := h.accountSvc.SyncAccount(ctx.Request.Context(), id, domainReq)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	resp := SyncResult{
		SyncID:    result.SyncID,
		Status:    result.Status,
		StartTime: result.StartTime,
	}

	return Result(resp), nil
}

// toCloudAccountVO 将领域模型转换为VO
func (h *Handler) toCloudAccountVO(account domain.CloudAccount) CloudAccount {
	return CloudAccount{
		ID:              account.ID,
		Name:            account.Name,
		Provider:        string(account.Provider),
		Environment:     string(account.Environment),
		AccessKeyID:     account.AccessKeyID,
		AccessKeySecret: account.AccessKeySecret, // 注意：这里已经在服务层做了脱敏处理
		Region:          account.Region,
		Description:     account.Description,
		Status:          string(account.Status),
		Config: CloudAccountConfigVO{
			EnableAutoSync:       account.Config.EnableAutoSync,
			SyncInterval:         account.Config.SyncInterval,
			ReadOnly:             account.Config.ReadOnly,
			ShowSubAccounts:      account.Config.ShowSubAccounts,
			EnableCostMonitoring: account.Config.EnableCostMonitoring,
			SupportedRegions:     account.Config.SupportedRegions,
			SupportedAssetTypes:  account.Config.SupportedAssetTypes,
		},
		TenantID:     account.TenantID,
		LastSyncTime: account.LastSyncTime,
		LastTestTime: account.LastTestTime,
		AssetCount:   account.AssetCount,
		ErrorMessage: account.ErrorMessage,
		CreateTime:   account.CreateTime,
		UpdateTime:   account.UpdateTime,
	}
}
