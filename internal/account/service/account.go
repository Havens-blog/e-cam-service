// Package service 云账号服务层
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/account/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/google/uuid"
	"github.com/gotomicro/ego/core/elog"
)

// 任务类型常量
const TaskTypeSyncAssets taskx.TaskType = "cam:sync_assets"

// CloudAccountService 云账号服务接口
type CloudAccountService interface {
	CreateAccount(ctx context.Context, req *domain.CreateCloudAccountRequest) (*domain.CloudAccount, error)
	GetAccount(ctx context.Context, id int64) (*domain.CloudAccount, error)
	ListAccounts(ctx context.Context, filter domain.CloudAccountFilter) ([]*domain.CloudAccount, int64, error)
	UpdateAccount(ctx context.Context, id int64, req *domain.UpdateCloudAccountRequest) error
	DeleteAccount(ctx context.Context, id int64) error
	TestConnection(ctx context.Context, id int64) (*domain.ConnectionTestResult, error)
	EnableAccount(ctx context.Context, id int64) error
	DisableAccount(ctx context.Context, id int64) error
	SyncAccount(ctx context.Context, id int64, req *domain.SyncAccountRequest) (*domain.SyncResult, error)
}

type cloudAccountService struct {
	repo             repository.CloudAccountRepository
	taskQueue        *taskx.Queue
	logger           *elog.Component
	validatorFactory cloudx.CloudValidatorFactory
}

// NewCloudAccountService 创建云账号服务
func NewCloudAccountService(
	repo repository.CloudAccountRepository,
	taskQueue *taskx.Queue,
	logger *elog.Component,
) CloudAccountService {
	if logger == nil {
		logger = elog.Load("default").Build()
	}
	return &cloudAccountService{
		repo:             repo,
		taskQueue:        taskQueue,
		logger:           logger,
		validatorFactory: cloudx.NewCloudValidatorFactory(),
	}
}

func (s *cloudAccountService) CreateAccount(ctx context.Context, req *domain.CreateCloudAccountRequest) (*domain.CloudAccount, error) {
	// 检查账号名称是否已存在
	_, err := s.repo.GetByName(ctx, req.Name, req.TenantID)
	if err == nil {
		return nil, errs.AccountAlreadyExist
	}

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

	if err := account.Validate(); err != nil {
		return nil, err
	}

	// 验证云厂商凭证
	if err := s.validateCloudCredentials(ctx, &account); err != nil {
		s.logger.Error("cloud credentials validation failed", elog.FieldErr(err))
		return nil, err
	}

	id, err := s.repo.Create(ctx, account)
	if err != nil {
		s.logger.Error("failed to create cloud account", elog.FieldErr(err))
		return nil, errs.SystemError
	}

	account.ID = id
	s.logger.Info("cloud account created", elog.Int64("id", id))
	return &account, nil
}

func (s *cloudAccountService) GetAccount(ctx context.Context, id int64) (*domain.CloudAccount, error) {
	account, err := s.repo.GetByID(ctx, id)
	if err != nil {
		s.logger.Error("failed to get cloud account", elog.FieldErr(err), elog.Int64("id", id))
		return nil, errs.AccountNotFound
	}
	return account.MaskSensitiveData(), nil
}

func (s *cloudAccountService) ListAccounts(ctx context.Context, filter domain.CloudAccountFilter) ([]*domain.CloudAccount, int64, error) {
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

	maskedAccounts := make([]*domain.CloudAccount, len(accounts))
	for i, account := range accounts {
		maskedAccounts[i] = account.MaskSensitiveData()
	}
	return maskedAccounts, total, nil
}

func (s *cloudAccountService) UpdateAccount(ctx context.Context, id int64, req *domain.UpdateCloudAccountRequest) error {
	account, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return errs.AccountNotFound
	}

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
	if req.TenantID != nil {
		account.TenantID = *req.TenantID
	}

	account.UpdateTime = time.Now()
	account.UTime = account.UpdateTime.Unix()

	if err := s.repo.Update(ctx, account); err != nil {
		s.logger.Error("failed to update cloud account", elog.FieldErr(err), elog.Int64("id", id))
		return errs.SystemError
	}

	s.logger.Info("cloud account updated", elog.Int64("id", id))
	return nil
}

func (s *cloudAccountService) DeleteAccount(ctx context.Context, id int64) error {
	account, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return errs.AccountNotFound
	}

	if account.Status == domain.CloudAccountStatusTesting {
		return errs.SyncInProgress
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		s.logger.Error("failed to delete cloud account", elog.FieldErr(err), elog.Int64("id", id))
		return errs.SystemError
	}

	s.logger.Info("cloud account deleted", elog.Int64("id", id), elog.String("name", account.Name))
	return nil
}

func (s *cloudAccountService) TestConnection(ctx context.Context, id int64) (*domain.ConnectionTestResult, error) {
	account, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, errs.AccountNotFound
	}

	testTime := time.Now()
	if err := s.repo.UpdateTestTime(ctx, id, testTime, domain.CloudAccountStatusTesting, ""); err != nil {
		s.logger.Error("failed to update test status", elog.FieldErr(err), elog.Int64("id", id))
	}

	validator, err := s.validatorFactory.CreateValidator(account.Provider)
	if err != nil {
		s.logger.Error("failed to create validator", elog.FieldErr(err), elog.String("provider", string(account.Provider)))
		return nil, fmt.Errorf("不支持的云厂商: %s", account.Provider)
	}

	validationResult, err := validator.ValidateCredentials(ctx, &account)
	if err != nil {
		s.logger.Error("credential validation failed", elog.FieldErr(err), elog.Int64("id", id))
		if updateErr := s.repo.UpdateTestTime(ctx, id, testTime, domain.CloudAccountStatusError, err.Error()); updateErr != nil {
			s.logger.Error("failed to update error status", elog.FieldErr(updateErr), elog.Int64("id", id))
		}
		return &domain.ConnectionTestResult{
			Status:   "failed",
			Message:  fmt.Sprintf("连接测试失败: %v", err),
			TestTime: testTime,
		}, nil
	}

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

func (s *cloudAccountService) EnableAccount(ctx context.Context, id int64) error {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return errs.AccountNotFound
	}

	if err := s.repo.UpdateStatus(ctx, id, domain.CloudAccountStatusActive); err != nil {
		s.logger.Error("failed to enable cloud account", elog.FieldErr(err), elog.Int64("id", id))
		return errs.SystemError
	}

	s.logger.Info("cloud account enabled", elog.Int64("id", id))
	return nil
}

func (s *cloudAccountService) DisableAccount(ctx context.Context, id int64) error {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return errs.AccountNotFound
	}

	if err := s.repo.UpdateStatus(ctx, id, domain.CloudAccountStatusDisabled); err != nil {
		s.logger.Error("failed to disable cloud account", elog.FieldErr(err), elog.Int64("id", id))
		return errs.SystemError
	}

	s.logger.Info("cloud account disabled", elog.Int64("id", id))
	return nil
}

func (s *cloudAccountService) SyncAccount(ctx context.Context, id int64, req *domain.SyncAccountRequest) (*domain.SyncResult, error) {
	account, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, errs.AccountNotFound
	}

	if !account.IsActive() {
		return nil, errs.AccountDisabled
	}

	taskID := uuid.New().String()
	syncTime := time.Now()

	assetTypes := req.AssetTypes
	if len(assetTypes) == 0 {
		assetTypes = []string{"ecs"}
	}

	paramsMap := map[string]any{
		"provider":    string(account.Provider),
		"asset_types": assetTypes,
		"regions":     req.Regions,
		"account_id":  id,
		"tenant_id":   account.TenantID,
	}

	t := &taskx.Task{
		ID:        taskID,
		Type:      TaskTypeSyncAssets,
		Status:    taskx.TaskStatusPending,
		Params:    paramsMap,
		Progress:  0,
		Message:   "任务已创建，等待执行",
		CreatedBy: "system",
	}

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

	return &domain.SyncResult{
		SyncID:    taskID,
		Status:    "pending",
		Message:   "同步任务已提交，正在后台执行",
		StartTime: syncTime,
	}, nil
}

func (s *cloudAccountService) validateCloudCredentials(ctx context.Context, account *domain.CloudAccount) error {
	validator, err := s.validatorFactory.CreateValidator(account.Provider)
	if err != nil {
		return fmt.Errorf("不支持的云厂商 %s: %w", account.Provider, err)
	}

	validateCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, err := validator.ValidateCredentials(validateCtx, account)
	if err != nil {
		return fmt.Errorf("凭证验证失败: %w", err)
	}

	if !result.Valid {
		return fmt.Errorf("凭证无效: %s", result.Message)
	}

	s.logger.Info("cloud credentials validated",
		elog.String("provider", string(account.Provider)),
		elog.String("account_info", result.AccountInfo),
		elog.Int64("response_time", result.ResponseTime))

	return nil
}
