package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/internal/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/internal/repository"
	"github.com/Havens-blog/e-cam-service/internal/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/domain"
	"github.com/gotomicro/ego/core/elog"
)

// CloudAccountService 云账号服务接口
type CloudAccountService interface {
	// CreateAccount 创建云账号
	CreateAccount(ctx context.Context, req *domain.CreateCloudAccountRequest) (*domain.CloudAccount, error)

	// GetAccount 获取云账号详情
	GetAccount(ctx context.Context, id int64) (*domain.CloudAccount, error)

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
	logger           *elog.Component
	validatorFactory cloudx.CloudValidatorFactory
}

// NewCloudAccountService 创建云账号服务
func NewCloudAccountService(repo repository.CloudAccountRepository, logger *elog.Component) CloudAccountService {
	return &cloudAccountService{
		repo:             repo,
		logger:           elog.DefaultLogger,
		validatorFactory: cloudx.NewCloudValidatorFactory(),
	}
}

// CreateAccount 创建云账号
func (s *cloudAccountService) CreateAccount(ctx context.Context, req *domain.CreateCloudAccountRequest) (*domain.CloudAccount, error) {
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
		AccessKeySecret: req.AccessKeySecret, // TODO: 需要加密存储
		Region:          req.Region,
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
	if req.Description != nil {
		account.Description = *req.Description
	}
	if req.Config != nil {
		account.Config = *req.Config
	}

	// 更新时间戳
	account.UpdateTime = time.Now()
	account.UTime = account.UpdateTime.Unix()

	// 执行更新
	if err := s.repo.Update(ctx, account); err != nil {
		s.logger.Error("failed to update cloud account", elog.FieldErr(err), elog.Int64("id", id))
		return errs.SystemError
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
	validationResult, err := validator.ValidateCredentials(ctx, account)
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

// SyncAccount 同步云账号资产
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

	// 检查是否只读账号
	if account.IsReadOnly() {
		return nil, errs.ReadOnlyAccount
	}

	// TODO: 实现具体的同步逻辑
	// 这里暂时返回模拟结果
	syncTime := time.Now()
	result := &domain.SyncResult{
		SyncID:    fmt.Sprintf("sync_%d_%d", id, syncTime.Unix()),
		Status:    "running",
		StartTime: syncTime,
	}

	// 更新同步时间
	if err := s.repo.UpdateSyncTime(ctx, id, syncTime, 0); err != nil {
		s.logger.Error("failed to update sync time", elog.FieldErr(err), elog.Int64("id", id))
	}

	s.logger.Info("cloud account sync started", elog.Int64("id", id), elog.String("sync_id", result.SyncID))
	return result, nil
}

// validateCloudCredentials 验证云厂商凭证
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
