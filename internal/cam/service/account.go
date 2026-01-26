package service

import (
	"context"
	"fmt"
	"time"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/asset"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
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
	instanceRepo     repository.InstanceRepository
	adapterFactory   *asset.AdapterFactory
	logger           *elog.Component
	validatorFactory cloudx.CloudValidatorFactory
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
	logger *elog.Component,
) CloudAccountService {
	if logger == nil {
		logger = elog.Load("default").Build()
	}
	return &cloudAccountService{
		repo:             repo,
		instanceRepo:     instanceRepo,
		adapterFactory:   adapterFactory,
		logger:           logger,
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
	if req.Environment != nil {
		account.Environment = *req.Environment
	}
	if req.AccessKeyID != nil {
		account.AccessKeyID = *req.AccessKeyID
	}
	if req.AccessKeySecret != nil {
		account.AccessKeySecret = *req.AccessKeySecret // TODO: 需要加密存储
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

	syncTime := time.Now()
	result := &domain.SyncResult{
		SyncID:    fmt.Sprintf("sync_%d_%d", id, syncTime.Unix()),
		Status:    "running",
		StartTime: syncTime,
	}

	s.logger.Info("开始同步云账号资产",
		elog.Int64("account_id", id),
		elog.String("account_name", account.Name),
		elog.String("sync_id", result.SyncID))

	// 获取要同步的资源类型
	assetTypes := req.AssetTypes
	if len(assetTypes) == 0 {
		assetTypes = []string{"ecs"} // 默认只同步 ECS
	}

	// 执行同步
	synced, err := s.syncAccountAssets(ctx, &account, assetTypes)
	if err != nil {
		s.logger.Error("同步云账号资产失败",
			elog.Int64("account_id", id),
			elog.FieldErr(err))
		result.Status = "failed"
		result.Message = err.Error()
		return result, nil
	}

	// 更新同步时间和资产数量
	if err := s.repo.UpdateSyncTime(ctx, id, syncTime, int64(synced)); err != nil {
		s.logger.Warn("更新同步时间失败", elog.FieldErr(err))
	}

	result.Status = "success"
	result.Message = fmt.Sprintf("同步完成，共同步 %d 个资产", synced)

	s.logger.Info("云账号资产同步完成",
		elog.Int64("account_id", id),
		elog.String("sync_id", result.SyncID),
		elog.Int("synced", synced))

	return result, nil
}

// syncAccountAssets 同步单个账号的资产
func (s *cloudAccountService) syncAccountAssets(ctx context.Context, account *domain.CloudAccount, assetTypes []string) (int, error) {
	s.logger.Info("同步账号资产",
		elog.String("account", account.Name),
		elog.Any("asset_types", assetTypes))

	// 使用资产适配器工厂创建适配器
	assetAdapter, err := s.adapterFactory.CreateAdapterFromDomain(account)
	if err != nil {
		return 0, fmt.Errorf("创建适配器失败: %w", err)
	}

	// 获取所有地域
	regions, err := assetAdapter.GetRegions(ctx)
	if err != nil {
		return 0, fmt.Errorf("获取地域列表失败: %w", err)
	}

	// 如果账号配置了支持的地域，则只同步这些地域
	supportedRegions := account.Config.SupportedRegions
	if len(supportedRegions) > 0 {
		regionMap := make(map[string]bool)
		for _, r := range supportedRegions {
			regionMap[r] = true
		}

		filteredRegions := make([]types.Region, 0)
		for _, r := range regions {
			if regionMap[r.ID] {
				filteredRegions = append(filteredRegions, r)
			}
		}
		regions = filteredRegions
	}

	// 同步每个地域的资产
	totalSynced := 0
	for _, region := range regions {
		synced, err := s.syncRegionAssets(ctx, assetAdapter, account, region.ID, assetTypes)
		if err != nil {
			s.logger.Error("同步地域资产失败",
				elog.String("region", region.ID),
				elog.FieldErr(err))
			continue
		}
		totalSynced += synced
	}

	return totalSynced, nil
}

// syncRegionAssets 同步单个地域的资产
func (s *cloudAccountService) syncRegionAssets(
	ctx context.Context,
	assetAdapter asset.CloudAssetAdapter,
	account *domain.CloudAccount,
	region string,
	assetTypes []string,
) (int, error) {
	totalSynced := 0

	for _, assetType := range assetTypes {
		switch assetType {
		case "ecs":
			synced, err := s.syncRegionECSInstances(ctx, assetAdapter, account, region)
			if err != nil {
				s.logger.Error("同步地域ECS实例失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		default:
			s.logger.Warn("不支持的资源类型",
				elog.String("asset_type", assetType),
				elog.String("region", region))
		}
	}

	return totalSynced, nil
}

// syncRegionECSInstances 同步单个地域的 ECS 实例到 c_instance 表
// 实现完整的增删改同步逻辑
func (s *cloudAccountService) syncRegionECSInstances(
	ctx context.Context,
	assetAdapter asset.CloudAssetAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	// 构建模型 UID
	modelUID := fmt.Sprintf("%s_ecs", account.Provider)

	// 1. 获取云端 ECS 实例
	cloudInstances, err := assetAdapter.GetECSInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取ECS实例失败: %w", err)
	}

	// 2. 获取本地数据库中该地域的所有 AssetID
	localAssetIDs, err := s.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		s.logger.Warn("获取本地实例列表失败",
			elog.String("region", region),
			elog.FieldErr(err))
		localAssetIDs = []string{}
	}

	// 3. 构建云端 AssetID 集合
	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.InstanceID] = true
	}

	// 4. 找出需要删除的实例（本地有但云端没有）
	var toDelete []string
	for _, assetID := range localAssetIDs {
		if !cloudAssetIDSet[assetID] {
			toDelete = append(toDelete, assetID)
		}
	}

	// 5. 删除已不存在的实例
	if len(toDelete) > 0 {
		deleted, err := s.instanceRepo.DeleteByAssetIDs(ctx, account.TenantID, modelUID, toDelete)
		if err != nil {
			s.logger.Error("删除过期实例失败",
				elog.String("region", region),
				elog.FieldErr(err))
		} else {
			s.logger.Info("删除过期实例",
				elog.String("region", region),
				elog.Int64("deleted", deleted))
		}
	}

	// 6. 新增或更新云端实例
	synced := 0
	for _, inst := range cloudInstances {
		instance := s.convertECSToInstance(inst, account)

		// Upsert 会根据 tenant_id + model_uid + asset_id 判断是新增还是更新
		err := s.instanceRepo.Upsert(ctx, instance)
		if err != nil {
			s.logger.Error("保存实例失败",
				elog.String("asset_id", inst.InstanceID),
				elog.FieldErr(err))
			continue
		}
		synced++
	}

	s.logger.Info("同步地域ECS实例完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int("deleted", len(toDelete)))

	return synced, nil
}

// convertECSToInstance 将 ECS 实例转换为 Instance 领域模型
func (s *cloudAccountService) convertECSToInstance(inst types.ECSInstance, account *domain.CloudAccount) camdomain.Instance {
	// 构建模型 UID，格式: {provider}_ecs
	modelUID := fmt.Sprintf("%s_ecs", inst.Provider)

	// 构建动态属性
	attributes := map[string]interface{}{
		// 基本信息
		"status":        inst.Status,
		"region":        inst.Region,
		"zone":          inst.Zone,
		"provider":      inst.Provider,
		"description":   inst.Description,
		"host_name":     inst.HostName,
		"key_pair_name": inst.KeyPairName,

		// 配置信息
		"instance_type":        inst.InstanceType,
		"instance_type_family": inst.InstanceTypeFamily,
		"cpu":                  inst.CPU,
		"memory":               inst.Memory,
		"os_type":              inst.OSType,
		"os_name":              inst.OSName,
		"image_id":             inst.ImageID,

		// 网络信息
		"public_ip":                  inst.PublicIP,
		"private_ip":                 inst.PrivateIP,
		"vpc_id":                     inst.VPCID,
		"vswitch_id":                 inst.VSwitchID,
		"security_groups":            inst.SecurityGroups,
		"internet_max_bandwidth_in":  inst.InternetMaxBandwidthIn,
		"internet_max_bandwidth_out": inst.InternetMaxBandwidthOut,
		"network_type":               inst.NetworkType,
		"instance_network_type":      inst.InstanceNetworkType,

		// 存储信息
		"system_disk_category": inst.SystemDiskCategory,
		"system_disk_size":     inst.SystemDiskSize,
		"data_disks":           inst.DataDisks,
		"io_optimized":         inst.IoOptimized,

		// 计费信息
		"charge_type":       inst.ChargeType,
		"creation_time":     inst.CreationTime,
		"expired_time":      inst.ExpiredTime,
		"auto_renew":        inst.AutoRenew,
		"auto_renew_period": inst.AutoRenewPeriod,

		// 标签
		"tags": inst.Tags,
	}

	return camdomain.Instance{
		ModelUID:   modelUID,
		AssetID:    inst.InstanceID,
		AssetName:  inst.InstanceName,
		TenantID:   account.TenantID,
		AccountID:  account.ID,
		Attributes: attributes,
	}
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
