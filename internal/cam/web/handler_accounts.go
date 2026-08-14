package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-cam-service/pkg/ginx"
	"github.com/gin-gonic/gin"
)

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
		TenantID: middleware.GetTenantID(ctx),
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
	tenantID := middleware.GetTenantID(ctx)

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
		// 此处没有 TenantID：更新操作不具备"改变账号归属租户"的语义。
		// 该字段已从 domain.UpdateCloudAccountRequest 中删除（连同 service 层
		// 两处 `if req.TenantID != nil` 门），故不存在任何经更新改写归属的路径。
		// 归属只在创建时由会话租户确定。
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
