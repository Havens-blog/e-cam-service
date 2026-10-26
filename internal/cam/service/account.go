package service

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/governance"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cloudx-sdk"
	"github.com/Havens-blog/e-cloudx-sdk/asset"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/Havens-blog/e-common-go/taskx"
	"github.com/google/uuid"
	"github.com/gotomicro/ego/core/elog"
)

// 任务类型常量 (避免循环导入)
const TaskTypeSyncAssets taskx.TaskType = "cam:sync_assets"

// CloudAccountService 云账号服务接口
type CloudAccountService interface {
	// CreateAccount 创建云账号
	CreateAccount(ctx context.Context, req *domain.CreateCloudAccountRequest) (*domain.CloudAccount, error)

	// GetAccount 获取云账号详情
	GetAccount(ctx context.Context, id int64) (*domain.CloudAccount, error)

	// GetAccountWithCredentials 获取云账号（包含完整凭证，仅供内部服务使用）
	GetAccountWithCredentials(ctx context.Context, id int64) (*domain.CloudAccount, error)

	// ListAccounts 获取云账号列表
	ListAccounts(ctx context.Context, filter domain.CloudAccountFilter) ([]*domain.CloudAccount, int64, error)

	// UpdateAccount 更新云账号
	UpdateAccount(ctx context.Context, id int64, req *domain.UpdateCloudAccountRequest) error

	// DeleteAccount 删除云账号
	DeleteAccount(ctx context.Context, id int64) error

	// TestConnection 测试云账号连接
	TestConnection(ctx context.Context, id int64) (*domain.ConnectionTestResult, error)

	// EnableAccount 启用云账号
	EnableAccount(ctx context.Context, id int64) error

	// DisableAccount 禁用云账号
	DisableAccount(ctx context.Context, id int64) error

	// SyncAccount 同步云账号资产
	SyncAccount(ctx context.Context, id int64, req *domain.SyncAccountRequest) (*domain.SyncResult, error)
}

type cloudAccountService struct {
	repo             repository.CloudAccountRepository
	instanceRepo     repository.InstanceRepository
	adapterFactory   *asset.AdapterFactory
	taskQueue        *taskx.Queue
	logger           *elog.Component
	validatorFactory cloudx.CloudValidatorFactory
	// governance 租户治理配置端口（eiam MaxAccounts/AllowedVendors），
	// CreateAccount 侧的 R2.4/R7 强制校验数据源；nil 视为查询失败 fail-closed
	governance governance.TenantGovernancePort
}

// ensureLogger 确保 logger 不为 nil
func (s *cloudAccountService) ensureLogger() *elog.Component {
	if s.logger == nil {
		s.logger = elog.Load("default").Build()
	}
	return s.logger
}

// NewCloudAccountService 创建云账号服务
func NewCloudAccountService(
	repo repository.CloudAccountRepository,
	instanceRepo repository.InstanceRepository,
	adapterFactory *asset.AdapterFactory,
	taskQueue *taskx.Queue,
	logger *elog.Component,
	governancePort governance.TenantGovernancePort,
) CloudAccountService {
	if logger == nil {
		logger = elog.Load("default").Build()
	}
	return &cloudAccountService{
		repo:             repo,
		instanceRepo:     instanceRepo,
		adapterFactory:   adapterFactory,
		taskQueue:        taskQueue,
		logger:           logger,
		validatorFactory: cloudx.NewCloudValidatorFactory(),
		governance:       governancePort,
	}
}

// CreateAccount 创建云账号
func (s *cloudAccountService) CreateAccount(ctx context.Context, req *domain.CreateCloudAccountRequest) (*domain.CloudAccount, error) {
	// 租户治理强制校验（R7 云厂商白名单 + R2.4 云账号配额）：置于查重/落库之前，
	// 越白名单或超配额的请求不进入后续任何流程；fail-closed——治理配置查询失败
	// 即拒绝，绝不静默放行（配额/白名单形同虚设的中间态不可接受）
	if err := s.checkTenantGovernance(ctx, req); err != nil {
		return nil, err
	}

	// 检查账号名称是否已存在
	_, err := s.repo.GetByName(ctx, req.Name, req.TenantID)
	if err == nil {
		return nil, errs.AccountAlreadyExist
	}

	// 构建云账号实体
	now := time.Now()
	account := domain.CloudAccount{
		Name:            req.Name,
		Provider:        req.Provider,
		Environment:     req.Environment,
		AccessKeyID:     req.AccessKeyID,
		AccessKeySecret: req.AccessKeySecret,
		Regions:         req.Regions,
		Description:     req.Description,
		Status:          domain.CloudAccountStatusActive,
		Config:          req.Config,
		TenantID:        req.TenantID,
		CreateTime:      now,
		UpdateTime:      now,
		CTime:           now.Unix(),
		UTime:           now.Unix(),
	}

	// 验证账号数据
	if err := account.Validate(); err != nil {
		return nil, err
	}

	// 验证云厂商凭证
	if err := s.validateCloudCredentials(ctx, &account); err != nil {
		s.logger.Error("cloud credentials validation failed", elog.FieldErr(err))
		return nil, err
	}

	// 创建账号
	id, err := s.repo.Create(ctx, account)
	if err != nil {
		s.logger.Error("failed to create cloud account", elog.Any("错误信息", err))
		return nil, errs.SystemError
	}

	account.ID = id
	s.logger.Info("cloud account created successfully", elog.Any("id:", id))

	return &account, nil
}

// checkTenantGovernance 创建云账号前的租户治理强制校验（R7 + R2.4）。
// 校验顺序：缺租户上下文 → 治理配置查询（fail-closed）→ 白名单 → 配额。
func (s *cloudAccountService) checkTenantGovernance(ctx context.Context, req *domain.CreateCloudAccountRequest) error {
	// 写操作缺租户上下文直接拒绝（fail-closed 纵深防御，与 eiam 侧 R2.5/R2.6
	// 配额校验同模式）；同时避免 tid=0 触发对租户 0 的无意义跨服务查询，
	// 以及 DAO 按 tenant_id=0 退化成全量计数
	if req.TenantID <= 0 {
		return errs.ErrInvalidTenantID
	}

	if s.governance == nil {
		return fmt.Errorf("%w: 租户治理配置端口未装配", errs.ErrTenantGovernanceUnavailable)
	}
	maxAccounts, allowedVendors, err := s.governance.GetGovernance(ctx, req.TenantID)
	if err != nil {
		s.logger.Error("查询租户治理配置失败，拒绝创建云账号",
			elog.Int64("tenant_id", req.TenantID), elog.FieldErr(err))
		return fmt.Errorf("%w: %v", errs.ErrTenantGovernanceUnavailable, err)
	}

	// R7 云厂商白名单：AllowedVendors 非空且请求厂商不在列 → 拒绝（空列表 = 全部允许，R7.4）
	if len(allowedVendors) > 0 && !slices.Contains(allowedVendors, string(req.Provider)) {
		return fmt.Errorf("%w: %s", errs.ErrVendorNotAllowed, string(req.Provider))
	}

	// R2.4 云账号配额：仅 MaxAccounts > 0 才计数拦截（0/负 = 无限制，R2.7）；
	// 复用 repo.List 的 total 作账号计数（limit=1 最小取数，不新增 Count 接口）
	if maxAccounts > 0 {
		_, total, err := s.repo.List(ctx, domain.CloudAccountFilter{TenantID: req.TenantID, Limit: 1})
		if err != nil {
			s.logger.Error("统计租户云账号数量失败，拒绝创建云账号",
				elog.Int64("tenant_id", req.TenantID), elog.FieldErr(err))
			return errs.SystemError
		}
		if total >= maxAccounts {
			return fmt.Errorf("%w: 已有 %d 个云账号，上限 %d", errs.ErrTenantQuotaExceeded, total, maxAccounts)
		}
	}

	return nil
}

// GetAccount 获取云账号详情
func (s *cloudAccountService) GetAccount(ctx context.Context, id int64) (*domain.CloudAccount, error) {
	account, err := s.repo.GetByID(ctx, id)
	if err != nil {
		s.logger.Error("failed to get cloud account", elog.FieldErr(err), elog.Int64("id", id))
		return nil, errs.AccountNotFound
	}

	// 脱敏敏感数据
	maskedAccount := account.MaskSensitiveData()
	return maskedAccount, nil
}

// GetAccountWithCredentials 获取云账号（包含完整凭证，仅供内部服务使用）
func (s *cloudAccountService) GetAccountWithCredentials(ctx context.Context, id int64) (*domain.CloudAccount, error) {
	account, err := s.repo.GetByID(ctx, id)
	if err != nil {
		s.logger.Error("failed to get cloud account", elog.FieldErr(err), elog.Int64("id", id))
		return nil, errs.AccountNotFound
	}
	return &account, nil
}

// ListAccounts 获取云账号列表
func (s *cloudAccountService) ListAccounts(ctx context.Context, filter domain.CloudAccountFilter) ([]*domain.CloudAccount, int64, error) {
	// 设置默认分页参数
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}

	accounts, total, err := s.repo.List(ctx, filter)
	if err != nil {
		s.logger.Error("failed to list cloud accounts", elog.FieldErr(err))
		return nil, 0, errs.SystemError
	}

	// 脱敏敏感数据
	maskedAccounts := make([]*domain.CloudAccount, len(accounts))
	for i, account := range accounts {
		maskedAccounts[i] = account.MaskSensitiveData()
	}

	return maskedAccounts, total, nil
}

// UpdateAccount 更新云账号
func (s *cloudAccountService) UpdateAccount(ctx context.Context, id int64, req *domain.UpdateCloudAccountRequest) error {
	// 检查账号是否存在
	account, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return errs.AccountNotFound
	}

	// 更新字段
	if req.Name != nil {
		account.Name = *req.Name
	}
	if req.Environment != nil {
		account.Environment = *req.Environment
	}
	if req.AccessKeyID != nil {
		account.AccessKeyID = *req.AccessKeyID
	}
	if req.AccessKeySecret != nil {
		account.AccessKeySecret = *req.AccessKeySecret
	}
	if req.Regions != nil && len(req.Regions) > 0 {
		account.Regions = req.Regions
	}
	if req.Description != nil {
		account.Description = *req.Description
	}
	if req.Config != nil {
		account.Config = *req.Config
	}

	// 此处原有 `if req.TenantID != nil { account.TenantID = *req.TenantID }`。
	// 更新不具备"改变归属租户"的语义，该门已随请求结构体的 TenantID 字段一并删除，
	// 账号归属只在创建时由会话租户确定。

	// 更新时间戳
	account.UpdateTime = time.Now()
	account.UTime = account.UpdateTime.Unix()

	// 执行更新
	if err := s.repo.Update(ctx, account); err != nil {
		s.logger.Error("failed to update cloud account", elog.FieldErr(err), elog.Int64("id", id))
		return errs.SystemError
	}

	// 凭证轮换：AK/SK 变更后失效该账号的云适配器缓存，否则下次
	// CreateAdapter 会命中旧凭证的缓存适配器（adapterCache 为全局共享缓存，
	// 一次失效覆盖 account/tag/dns/sync 所有路径）。
	if req.AccessKeyID != nil || req.AccessKeySecret != nil {
		s.adapterFactory.ClearAccountCache(account.Provider, account.ID)
	}

	s.logger.Info("cloud account updated successfully", elog.Int64("id", id))
	return nil
}

// DeleteAccount 删除云账号
func (s *cloudAccountService) DeleteAccount(ctx context.Context, id int64) error {
	// 检查账号是否存在
	account, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return errs.AccountNotFound
	}

	// 检查账号状态，如果正在同步中则不允许删除
	if account.Status == domain.CloudAccountStatusTesting {
		return errs.SyncInProgress
	}

	// 执行删除
	if err := s.repo.Delete(ctx, id); err != nil {
		s.logger.Error("failed to delete cloud account", elog.FieldErr(err), elog.Int64("id", id))
		return errs.SystemError
	}

	s.logger.Info("cloud account deleted successfully", elog.Int64("id", id), elog.String("name", account.Name))
	return nil
}

// TestConnection 测试云账号连接
func (s *cloudAccountService) TestConnection(ctx context.Context, id int64) (*domain.ConnectionTestResult, error) {
	// 检查账号是否存在
	account, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, errs.AccountNotFound
	}

	// 更新测试状态
	testTime := time.Now()
	if err := s.repo.UpdateTestTime(ctx, id, testTime, domain.CloudAccountStatusTesting, ""); err != nil {
		s.logger.Error("failed to update test status", elog.FieldErr(err), elog.Int64("id", id))
	}

	// 使用云厂商验证器进行连接测试
	validator, err := s.validatorFactory.CreateValidator(account.Provider)
	if err != nil {
		s.logger.Error("failed to create validator", elog.FieldErr(err), elog.String("provider", string(account.Provider)))
		return nil, fmt.Errorf("不支持的云厂商: %s", account.Provider)
	}

	// 执行验证
	validationResult, err := validator.ValidateCredentials(ctx, &account)
	if err != nil {
		s.logger.Error("credential validation failed", elog.FieldErr(err), elog.Int64("id", id))

		// 更新错误状态
		if updateErr := s.repo.UpdateTestTime(ctx, id, testTime, domain.CloudAccountStatusError, err.Error()); updateErr != nil {
			s.logger.Error("failed to update error status", elog.FieldErr(updateErr), elog.Int64("id", id))
		}

		return &domain.ConnectionTestResult{
			Status:   "failed",
			Message:  fmt.Sprintf("连接测试失败: %v", err),
			TestTime: testTime,
		}, nil
	}

	// 构建测试结果
	result := &domain.ConnectionTestResult{
		Status:   "success",
		Message:  validationResult.Message,
		Regions:  validationResult.Regions,
		TestTime: validationResult.ValidatedAt,
	}

	if !validationResult.Valid {
		result.Status = "failed"
		result.Message = validationResult.Message
	}

	// 更新测试结果
	status := domain.CloudAccountStatusActive
	errorMsg := ""
	if result.Status != "success" {
		status = domain.CloudAccountStatusError
		errorMsg = result.Message
	}

	if err := s.repo.UpdateTestTime(ctx, id, testTime, status, errorMsg); err != nil {
		s.logger.Error("failed to update test result", elog.FieldErr(err), elog.Int64("id", id))
	}

	s.logger.Info("cloud account connection tested",
		elog.Int64("id", id),
		elog.String("status", result.Status),
		elog.Int64("response_time", validationResult.ResponseTime))

	return result, nil
}

// EnableAccount 启用云账号
func (s *cloudAccountService) EnableAccount(ctx context.Context, id int64) error {
	// 检查账号是否存在
	_, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return errs.AccountNotFound
	}

	// 更新状态为活跃
	if err := s.repo.UpdateStatus(ctx, id, domain.CloudAccountStatusActive); err != nil {
		s.logger.Error("failed to enable cloud account", elog.FieldErr(err), elog.Int64("id", id))
		return errs.SystemError
	}

	s.logger.Info("cloud account enabled successfully", elog.Int64("id", id))
	return nil
}

// DisableAccount 禁用云账号
func (s *cloudAccountService) DisableAccount(ctx context.Context, id int64) error {
	// 检查账号是否存在
	_, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return errs.AccountNotFound
	}

	// 更新状态为禁用
	if err := s.repo.UpdateStatus(ctx, id, domain.CloudAccountStatusDisabled); err != nil {
		s.logger.Error("failed to disable cloud account", elog.FieldErr(err), elog.Int64("id", id))
		return errs.SystemError
	}

	s.logger.Info("cloud account disabled successfully", elog.Int64("id", id))
	return nil
}

// SyncAccount 同步云账号资产 (异步执行)
func (s *cloudAccountService) SyncAccount(ctx context.Context, id int64, req *domain.SyncAccountRequest) (*domain.SyncResult, error) {
	// 检查账号是否存在
	account, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, errs.AccountNotFound
	}

	// 检查账号状态
	if !account.IsActive() {
		return nil, errs.AccountDisabled
	}

	// 生成任务ID
	taskID := uuid.New().String()
	syncTime := time.Now()

	// 获取要同步的资源类型
	assetTypes := req.AssetTypes
	if len(assetTypes) == 0 {
		// 默认同步所有常用资源类型
		assetTypes = []string{
			"ecs", "disk", "snapshot", "security_group", "image",
			"rds", "redis", "mongodb",
			"vpc", "eip", "lb", "vswitch", "cdn", "waf", "ddos", "dns",
			"nas", "oss",
			"kafka", "elasticsearch",
		}
	}

	// 构建任务参数
	paramsMap := map[string]any{
		"provider":    string(account.Provider),
		"asset_types": assetTypes,
		"regions":     req.Regions,
		"account_id":  id,
		"tenant_id":   account.TenantID,
	}

	// 创建异步任务
	t := &taskx.Task{
		ID:        taskID,
		Type:      TaskTypeSyncAssets,
		Status:    taskx.TaskStatusPending,
		Params:    paramsMap,
		Progress:  0,
		Message:   "任务已创建，等待执行",
		CreatedBy: "system",
	}

	// 提交任务到队列
	if err := s.taskQueue.Submit(t); err != nil {
		s.logger.Error("提交同步任务失败",
			elog.Int64("account_id", id),
			elog.FieldErr(err))
		return nil, fmt.Errorf("提交同步任务失败: %w", err)
	}

	s.logger.Info("同步任务已提交",
		elog.Int64("account_id", id),
		elog.String("task_id", taskID),
		elog.Any("asset_types", assetTypes))

	result := &domain.SyncResult{
		SyncID:    taskID,
		Status:    "pending",
		Message:   "同步任务已提交，正在后台执行",
		StartTime: syncTime,
	}

	return result, nil
}

// syncAccountAssets 同步单个账号的资产
func (s *cloudAccountService) validateCloudCredentials(ctx context.Context, account *domain.CloudAccount) error {
	// 创建对应的云厂商验证器
	validator, err := s.validatorFactory.CreateValidator(account.Provider)
	if err != nil {
		return fmt.Errorf("不支持的云厂商 %s: %w", account.Provider, err)
	}

	// 设置验证超时
	validateCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// 执行凭证验证
	result, err := validator.ValidateCredentials(validateCtx, account)
	if err != nil {
		return fmt.Errorf("凭证验证失败: %w", err)
	}

	if !result.Valid {
		return fmt.Errorf("凭证无效: %s", result.Message)
	}

	s.logger.Info("cloud credentials validated successfully",
		elog.String("provider", string(account.Provider)),
		elog.String("account_info", result.AccountInfo),
		elog.Int64("response_time", result.ResponseTime))

	return nil
}
