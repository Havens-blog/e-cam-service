package web

import (
	"strconv"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/ginx"
	"github.com/gin-gonic/gin"
)

// Handler CAM HTTP处理器
type Handler struct {
	svc        service.Service
	accountSvc service.CloudAccountService
	modelSvc   service.ModelService
}

// NewHandler 创建CAM处理器
func NewHandler(svc service.Service, accountSvc service.CloudAccountService, modelSvc service.ModelService) *Handler {
	return &Handler{
		svc:        svc,
		accountSvc: accountSvc,
		modelSvc:   modelSvc,
	}
}

// PrivateRoutes 注册私有路由
func (h *Handler) PrivateRoutes(server *gin.Engine) {
	camGroup := server.Group("/api/v1/cam")
	{
		// 资产管理
		camGroup.POST("/assets", ginx.WrapBody[CreateAssetReq](h.CreateAsset))
		camGroup.POST("/assets/batch", ginx.WrapBody[CreateMultiAssetsReq](h.CreateMultiAssets))
		camGroup.PUT("/assets/:id", ginx.WrapBody[UpdateAssetReq](h.UpdateAsset))
		camGroup.GET("/assets/:id", h.GetAssetById)
		camGroup.GET("/assets", h.ListAssets)
		camGroup.DELETE("/assets/:id", h.DeleteAsset)

		// 资产发现
		camGroup.POST("/assets/discover", ginx.WrapBody[DiscoverAssetsReq](h.DiscoverAssets))
		camGroup.POST("/assets/sync", ginx.WrapBody[SyncAssetsReq](h.SyncAssets))

		// 统计分析
		camGroup.GET("/assets/statistics", h.GetAssetStatistics)
		camGroup.GET("/assets/cost-analysis", h.GetCostAnalysis)

		// 云账号管理
		camGroup.POST("/cloud-accounts", ginx.WrapBody[CreateCloudAccountReq](h.CreateCloudAccount))
		camGroup.GET("/cloud-accounts/:id", h.GetCloudAccount)
		camGroup.GET("/cloud-accounts", h.ListCloudAccounts)
		camGroup.PUT("/cloud-accounts/:id", ginx.WrapBody[UpdateCloudAccountReq](h.UpdateCloudAccount))
		camGroup.DELETE("/cloud-accounts/:id", h.DeleteCloudAccount)

		// 云账号操作
		camGroup.POST("/cloud-accounts/:id/test-connection", h.TestCloudAccountConnection)
		camGroup.POST("/cloud-accounts/:id/enable", h.EnableCloudAccount)
		camGroup.POST("/cloud-accounts/:id/disable", h.DisableCloudAccount)
		camGroup.POST("/cloud-accounts/:id/sync", ginx.WrapBody[SyncAccountReq](h.SyncCloudAccount))

		// 模型管理
		camGroup.POST("/models", ginx.WrapBody[CreateModelReq](h.CreateModel))
		camGroup.GET("/models/:uid", h.GetModel)
		camGroup.GET("/models", h.ListModels)
		camGroup.PUT("/models/:uid", ginx.WrapBody[UpdateModelReq](h.UpdateModel))
		camGroup.DELETE("/models/:uid", h.DeleteModel)

		// 字段管理
		camGroup.POST("/models/:uid/fields", ginx.WrapBody[CreateFieldReq](h.AddField))
		camGroup.GET("/models/:uid/fields", h.GetModelFields)
		camGroup.PUT("/fields/:field_uid", ginx.WrapBody[UpdateFieldReq](h.UpdateField))
		camGroup.DELETE("/fields/:field_uid", h.DeleteField)

		// 字段分组管理
		camGroup.POST("/models/:uid/field-groups", ginx.WrapBody[CreateFieldGroupReq](h.AddFieldGroup))
		camGroup.GET("/models/:uid/field-groups", h.GetModelFieldGroups)
		camGroup.PUT("/field-groups/:id", ginx.WrapBody[UpdateFieldGroupReq](h.UpdateFieldGroup))
		camGroup.DELETE("/field-groups/:id", h.DeleteFieldGroup)

		// 菜单管理
		camGroup.GET("/menus", h.GetMenus)
	}
}

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

// ==================== 云账号处理器 ====================

// CreateCloudAccount 创建云账号
// @Summary 创建云账号
// @Description 创建新的云账号配置
// @Tags 云账号管理
// @Accept json
// @Produce json
// @Param request body CreateCloudAccountReq true "云账号信息"
// @Success 200 {object} ginx.Result{data=domain.CloudAccount} "成功"
// @Failure 400 {object} ginx.Result "请求参数错误"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/cloud-accounts [post]
func (h *Handler) CreateCloudAccount(ctx *gin.Context, req CreateCloudAccountReq) (ginx.Result, error) {
	domainReq := &domain.CreateCloudAccountRequest{
		Name:            req.Name,
		Provider:        domain.CloudProvider(req.Provider),
		Environment:     domain.Environment(req.Environment),
		AccessKeyID:     req.AccessKeyID,
		AccessKeySecret: req.AccessKeySecret,
		Regions:         req.Regions,
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
// @Summary 获取云账号详情
// @Description 根据ID获取云账号的详细信息
// @Tags 云账号管理
// @Accept json
// @Produce json
// @Param id path int true "云账号ID"
// @Success 200 {object} ginx.Result{data=CloudAccount} "成功"
// @Failure 400 {object} ginx.Result "请求参数错误"
// @Failure 404 {object} ginx.Result "云账号不存在"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/cloud-accounts/{id} [get]
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
// @Summary 获取云账号列表
// @Description 获取云账号列表，支持按云厂商、环境、状态等条件过滤
// @Tags 云账号管理
// @Accept json
// @Produce json
// @Param provider query string false "云厂商" Enums(aliyun,aws,azure)
// @Param environment query string false "环境" Enums(dev,test,prod)
// @Param status query string false "状态" Enums(active,inactive)
// @Param tenant_id query string false "租户ID"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} ginx.Result{data=[]domain.CloudAccount} "成功"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/cloud-accounts [get]
func (h *Handler) ListCloudAccounts(ctx *gin.Context) {
	// 从 query 参数获取过滤条件
	provider := ctx.Query("provider")
	environment := ctx.Query("environment")
	status := ctx.Query("status")
	tenantID := ctx.Query("tenant_id")

	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	filter := domain.CloudAccountFilter{
		Provider:    domain.CloudProvider(provider),
		Environment: domain.Environment(environment),
		Status:      domain.CloudAccountStatus(status),
		TenantID:    tenantID,
		Offset:      int64(offset),
		Limit:       int64(limit),
	}

	accounts, total, err := h.accountSvc.ListAccounts(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	accountVOs := make([]CloudAccount, len(accounts))
	for i, account := range accounts {
		accountVOs[i] = h.toCloudAccountVO(*account)
	}

	resp := CloudAccountListResp{
		Accounts: accountVOs,
		Total:    total,
	}

	ctx.JSON(200, Result(resp))
}

// UpdateCloudAccount 更新云账号
// @Summary 更新云账号
// @Description 更新指定ID的云账号信息
// @Tags 云账号管理
// @Accept json
// @Produce json
// @Param id path int true "云账号ID"
// @Param request body UpdateCloudAccountReq true "更新的云账号信息"
// @Success 200 {object} ginx.Result{data=CloudAccount} "成功"
// @Failure 400 {object} ginx.Result "请求参数错误"
// @Failure 404 {object} ginx.Result "云账号不存在"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/cloud-accounts/{id} [put]
func (h *Handler) UpdateCloudAccount(ctx *gin.Context, req UpdateCloudAccountReq) (ginx.Result, error) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return ErrorResult(errs.ParamsError), nil
	}

	domainReq := &domain.UpdateCloudAccountRequest{
		Name:            req.Name,
		AccessKeyID:     req.AccessKeyID,
		AccessKeySecret: req.AccessKeySecret,
		Regions:         req.Regions,
		Description:     req.Description,
		TenantID:        req.TenantID,
	}

	// 转换环境字段
	if req.Environment != nil {
		env := domain.Environment(*req.Environment)
		domainReq.Environment = &env
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
// @Summary 删除云账号
// @Description 删除指定ID的云账号
// @Tags 云账号管理
// @Accept json
// @Produce json
// @Param id path int true "云账号ID"
// @Success 200 {object} ginx.Result "成功"
// @Failure 400 {object} ginx.Result "请求参数错误"
// @Failure 404 {object} ginx.Result "云账号不存在"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/cloud-accounts/{id} [delete]
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
// @Summary 测试云账号连接
// @Description 测试指定云账号的连接状态
// @Tags 云账号操作
// @Accept json
// @Produce json
// @Param id path int true "云账号ID"
// @Success 200 {object} ginx.Result{data=object} "连接成功"
// @Failure 400 {object} ginx.Result "请求参数错误"
// @Failure 404 {object} ginx.Result "云账号不存在"
// @Failure 500 {object} ginx.Result "连接失败"
// @Router /cam/cloud-accounts/{id}/test-connection [post]
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
// @Summary 启用云账号
// @Description 启用指定的云账号
// @Tags 云账号操作
// @Accept json
// @Produce json
// @Param id path int true "云账号ID"
// @Success 200 {object} ginx.Result "成功"
// @Failure 400 {object} ginx.Result "请求参数错误"
// @Failure 404 {object} ginx.Result "云账号不存在"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/cloud-accounts/{id}/enable [post]
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
// @Summary 禁用云账号
// @Description 禁用指定的云账号
// @Tags 云账号操作
// @Accept json
// @Produce json
// @Param id path int true "云账号ID"
// @Success 200 {object} ginx.Result "成功"
// @Failure 400 {object} ginx.Result "请求参数错误"
// @Failure 404 {object} ginx.Result "云账号不存在"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/cloud-accounts/{id}/disable [post]
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
// @Summary 同步云账号资产（推荐）
// @Description 同步指定云账号下的云资产到本地数据库。这是同步云资产的主要接口，支持按资源类型和地域过滤同步范围。
// @Description 同步过程会自动获取云账号配置的所有地域，并逐个地域同步指定类型的资产。
// @Description 如果不指定 asset_types，默认只同步 ECS 实例。
// @Tags 云账号操作
// @Accept json
// @Produce json
// @Param id path int true "云账号ID"
// @Param request body SyncAccountReq true "同步请求参数"
// @Success 200 {object} ginx.Result{data=SyncResult} "同步成功"
// @Failure 400 {object} ginx.Result "请求参数错误"
// @Failure 404 {object} ginx.Result "云账号不存在"
// @Failure 409 {object} ginx.Result "云账号已禁用"
// @Failure 500 {object} ginx.Result "服务器错误"
// @Router /cam/cloud-accounts/{id}/sync [post]
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
		Message:   result.Message,
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
		Regions:         account.Regions,
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

// ==================== 菜单管理 ====================

// MenuItem 菜单项
type MenuItem struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Icon     string     `json:"icon,omitempty"`
	Path     string     `json:"path,omitempty"`
	Children []MenuItem `json:"children,omitempty"`
	Order    int        `json:"order"`
}

// GetMenus 获取菜单列表
// @Summary 获取菜单列表
// @Description 获取系统导航菜单的层级结构
// @Tags 系统管理
// @Accept json
// @Produce json
// @Success 200 {object} ginx.Result{data=[]MenuItem} "成功"
// @Router /cam/menus [get]
func (h *Handler) GetMenus(ctx *gin.Context) {
	menus := []MenuItem{
		{
			ID:    "asset-management",
			Name:  "资产管理",
			Icon:  "icon-asset",
			Path:  "/asset-management",
			Order: 1,
			Children: []MenuItem{
				{
					ID:    "cloud-accounts",
					Name:  "云账号",
					Path:  "/asset-management/cloud-accounts",
					Order: 1,
				},
				{
					ID:    "cloud-assets",
					Name:  "云资产",
					Path:  "/asset-management/cloud-assets",
					Order: 2,
				},
				{
					ID:    "asset-models",
					Name:  "云模型",
					Path:  "/asset-management/asset-models",
					Order: 3,
				},
				{
					ID:    "cost-analysis",
					Name:  "代价",
					Path:  "/asset-management/cost-analysis",
					Order: 4,
				},
				{
					ID:    "sync-management",
					Name:  "同步管理",
					Path:  "/asset-management/sync-management",
					Order: 5,
				},
			},
		},
		{
			ID:    "configuration",
			Name:  "配置中心",
			Icon:  "icon-config",
			Path:  "/configuration",
			Order: 2,
			Children: []MenuItem{
				{
					ID:    "system-config",
					Name:  "系统配置",
					Path:  "/configuration/system",
					Order: 1,
				},
				{
					ID:    "user-management",
					Name:  "用户管理",
					Path:  "/configuration/users",
					Order: 2,
				},
			},
		},
		{
			ID:    "monitoring",
			Name:  "监控中心",
			Icon:  "icon-monitor",
			Path:  "/monitoring",
			Order: 3,
			Children: []MenuItem{
				{
					ID:    "task-monitor",
					Name:  "任务监控",
					Path:  "/monitoring/tasks",
					Order: 1,
				},
				{
					ID:    "sync-logs",
					Name:  "同步日志",
					Path:  "/monitoring/sync-logs",
					Order: 2,
				},
			},
		},
	}

	ctx.JSON(200, Result(menus))
}
