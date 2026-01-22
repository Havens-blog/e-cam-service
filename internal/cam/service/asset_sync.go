package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	syncdomain "github.com/Havens-blog/e-cam-service/internal/cam/sync/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/sync/service/adapters"
	cmdbdomain "github.com/Havens-blog/e-cam-service/internal/cmdb/domain"
	cmdbrepository "github.com/Havens-blog/e-cam-service/internal/cmdb/repository"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// AssetSyncService 资产同步服务 - 同步到 CMDB c_instance
type AssetSyncService interface {
	// SyncAssets 同步云资产到 CMDB
	SyncAssets(ctx context.Context, tenantID, provider string, assetTypes []string) (*SyncResult, error)
	// SyncAccountAssets 同步指定账号的资产
	SyncAccountAssets(ctx context.Context, tenantID string, accountID int64, assetTypes []string) (*SyncResult, error)
}

// SyncResult 同步结果
type SyncResult struct {
	TotalSynced int            `json:"total_synced"`
	Created     int            `json:"created"`
	Updated     int            `json:"updated"`
	Failed      int            `json:"failed"`
	ByAssetType map[string]int `json:"by_asset_type"`
	ByRegion    map[string]int `json:"by_region"`
	StartTime   time.Time      `json:"start_time"`
	EndTime     time.Time      `json:"end_time"`
	DurationMs  int64          `json:"duration_ms"`
}

type assetSyncService struct {
	instanceRepo   cmdbrepository.InstanceRepository
	accountRepo    repository.CloudAccountRepository
	adapterFactory *adapters.AdapterFactory
	logger         *elog.Component
}

// NewAssetSyncService 创建资产同步服务
func NewAssetSyncService(
	instanceRepo cmdbrepository.InstanceRepository,
	accountRepo repository.CloudAccountRepository,
	adapterFactory *adapters.AdapterFactory,
	logger *elog.Component,
) AssetSyncService {
	return &assetSyncService{
		instanceRepo:   instanceRepo,
		accountRepo:    accountRepo,
		adapterFactory: adapterFactory,
		logger:         logger,
	}
}

// SyncAssets 同步云资产到 CMDB
func (s *assetSyncService) SyncAssets(ctx context.Context, tenantID, provider string, assetTypes []string) (*SyncResult, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}

	if len(assetTypes) == 0 {
		assetTypes = []string{"ecs"}
	}

	result := &SyncResult{
		ByAssetType: make(map[string]int),
		ByRegion:    make(map[string]int),
		StartTime:   time.Now(),
	}

	s.logger.Info("开始同步云资产到CMDB",
		elog.String("tenant_id", tenantID),
		elog.String("provider", provider),
		elog.Any("asset_types", assetTypes))

	// 获取该云厂商的所有可用账号
	filter := shareddomain.CloudAccountFilter{
		Provider: shareddomain.CloudProvider(provider),
		Status:   shareddomain.CloudAccountStatusActive,
		Limit:    100,
	}

	accounts, _, err := s.accountRepo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("获取云账号失败: %w", err)
	}

	if len(accounts) == 0 {
		return nil, fmt.Errorf("未找到可用的 %s 云账号", provider)
	}

	// 同步每个账号
	for i := range accounts {
		accountResult, err := s.syncSingleAccount(ctx, tenantID, &accounts[i], assetTypes)
		if err != nil {
			s.logger.Error("同步账号资产失败",
				elog.String("account", accounts[i].Name),
				elog.FieldErr(err))
			continue
		}
		s.mergeResult(result, accountResult)
	}

	result.EndTime = time.Now()
	result.DurationMs = result.EndTime.Sub(result.StartTime).Milliseconds()

	s.logger.Info("云资产同步完成",
		elog.String("provider", provider),
		elog.Int("total_synced", result.TotalSynced),
		elog.Int64("duration_ms", result.DurationMs))

	return result, nil
}

// SyncAccountAssets 同步指定账号的资产
func (s *assetSyncService) SyncAccountAssets(ctx context.Context, tenantID string, accountID int64, assetTypes []string) (*SyncResult, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}

	if len(assetTypes) == 0 {
		assetTypes = []string{"ecs"}
	}

	// 获取账号信息
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("获取云账号失败: %w", err)
	}

	result := &SyncResult{
		ByAssetType: make(map[string]int),
		ByRegion:    make(map[string]int),
		StartTime:   time.Now(),
	}

	accountResult, err := s.syncSingleAccount(ctx, tenantID, &account, assetTypes)
	if err != nil {
		return nil, err
	}
	s.mergeResult(result, accountResult)

	result.EndTime = time.Now()
	result.DurationMs = result.EndTime.Sub(result.StartTime).Milliseconds()

	return result, nil
}

// syncSingleAccount 同步单个账号的资产
func (s *assetSyncService) syncSingleAccount(
	ctx context.Context,
	tenantID string,
	account *shareddomain.CloudAccount,
	assetTypes []string,
) (*SyncResult, error) {
	result := &SyncResult{
		ByAssetType: make(map[string]int),
		ByRegion:    make(map[string]int),
	}

	// 转换为同步域的账号格式
	defaultRegion := ""
	if len(account.Regions) > 0 {
		defaultRegion = account.Regions[0]
	}

	syncAccount := &syncdomain.CloudAccount{
		ID:              account.ID,
		Name:            account.Name,
		Provider:        syncdomain.CloudProvider(account.Provider),
		AccessKeyID:     account.AccessKeyID,
		AccessKeySecret: account.AccessKeySecret,
		DefaultRegion:   defaultRegion,
		Enabled:         account.Status == shareddomain.CloudAccountStatusActive,
	}

	// 创建适配器
	adapter, err := s.adapterFactory.CreateAdapter(syncAccount)
	if err != nil {
		return nil, fmt.Errorf("创建适配器失败: %w", err)
	}

	// 获取所有地域
	regions, err := adapter.GetRegions(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取地域列表失败: %w", err)
	}

	// 过滤支持的地域
	if len(account.Config.SupportedRegions) > 0 {
		regionMap := make(map[string]bool)
		for _, r := range account.Config.SupportedRegions {
			regionMap[r] = true
		}
		filtered := make([]syncdomain.Region, 0)
		for _, r := range regions {
			if regionMap[r.ID] {
				filtered = append(filtered, r)
			}
		}
		regions = filtered
	}
	s.logger.Debug("sync regions assets ")
	// 同步每个地域
	for _, region := range regions {
		regionResult, err := s.syncRegion(ctx, tenantID, adapter, account, region.ID, assetTypes)
		if err != nil {
			s.logger.Error("同步地域资产失败",
				elog.String("region", region.ID),
				elog.FieldErr(err))
			continue
		}
		s.mergeResult(result, regionResult)
	}

	// 更新账号同步时间
	now := time.Now()
	_ = s.accountRepo.UpdateSyncTime(ctx, account.ID, now, int64(result.TotalSynced))

	return result, nil
}

// syncRegion 同步单个地域的资产
func (s *assetSyncService) syncRegion(
	ctx context.Context,
	tenantID string,
	adapter syncdomain.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
	assetTypes []string,
) (*SyncResult, error) {
	result := &SyncResult{
		ByAssetType: make(map[string]int),
		ByRegion:    make(map[string]int),
	}

	for _, assetType := range assetTypes {
		switch assetType {
		case "ecs", "cloud_vm":
			synced, err := s.syncECSInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步ECS实例失败",
					elog.String("region", region),
					elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["ecs"] += synced.TotalSynced
			result.ByRegion[region] += synced.TotalSynced
			result.TotalSynced += synced.TotalSynced
			result.Created += synced.Created
			result.Updated += synced.Updated

		// TODO: 添加其他资源类型
		// case "vpc":
		// case "rds":
		default:
			s.logger.Warn("不支持的资源类型", elog.String("asset_type", assetType))
		}
	}

	return result, nil
}

// syncECSInstances 同步 ECS 实例到 c_instance
func (s *assetSyncService) syncECSInstances(
	ctx context.Context,
	tenantID string,
	adapter syncdomain.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{
		ByAssetType: make(map[string]int),
		ByRegion:    make(map[string]int),
	}

	// 获取 ECS 实例
	instances, err := adapter.GetECSInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取ECS实例失败: %w", err)
	}

	if len(instances) == 0 {
		return result, nil
	}

	s.logger.Debug("获取到ECS实例",
		elog.String("region", region),
		elog.Int("count", len(instances)))

	// 转换并保存
	for _, inst := range instances {
		cmdbInstance := s.convertECSToCMDBInstance(tenantID, account, inst)

		// Upsert 到 c_instance
		err := s.instanceRepo.Upsert(ctx, cmdbInstance)
		if err != nil {
			s.logger.Error("保存实例失败",
				elog.String("asset_id", inst.InstanceID),
				elog.FieldErr(err))
			result.Failed++
			continue
		}
		result.TotalSynced++
	}

	return result, nil
}

// convertECSToCMDBInstance 将 ECS 实例转换为 CMDB Instance
func (s *assetSyncService) convertECSToCMDBInstance(
	tenantID string,
	account *shareddomain.CloudAccount,
	inst syncdomain.ECSInstance,
) cmdbdomain.Instance {
	// 构建属性 map，字段名对应 c_attribute 中定义的 field_uid
	attrs := map[string]interface{}{
		// 基本信息 (GroupID: 10 for ecs)
		"provider":         inst.Provider,
		"cloud_account_id": account.ID,
		"region":           inst.Region,
		"instance_id":      inst.InstanceID,
		"instance_name":    inst.InstanceName,
		"status":           inst.Status,
		"zone":             inst.Zone,
		"create_time":      inst.CreationTime,
		"expire_time":      inst.ExpiredTime,

		// 配置信息 (GroupID: 11 for ecs)
		"instance_type": inst.InstanceType,
		"cpu":           inst.CPU,
		"memory":        inst.Memory,
		"os_type":       inst.OSType,

		// 网络信息 (GroupID: 12 for ecs)
		"private_ip": inst.PrivateIP,
		"public_ip":  inst.PublicIP,
		"vpc_id":     inst.VPCID,
		"subnet_id":  inst.VSwitchID,

		// 扩展信息
		"image_id":             inst.ImageID,
		"os_name":              inst.OSName,
		"security_groups":      inst.SecurityGroups,
		"charge_type":          inst.ChargeType,
		"hostname":             inst.HostName,
		"key_pair_name":        inst.KeyPairName,
		"description":          inst.Description,
		"tags":                 inst.Tags,
		"system_disk_size":     inst.SystemDiskSize,
		"system_disk_category": inst.SystemDiskCategory,
		"network_type":         inst.NetworkType,
		"max_bandwidth_in":     inst.InternetMaxBandwidthIn,
		"max_bandwidth_out":    inst.InternetMaxBandwidthOut,
	}

	return cmdbdomain.Instance{
		ModelUID:   "cloud_vm",
		AssetID:    inst.InstanceID,
		AssetName:  inst.InstanceName,
		TenantID:   tenantID,
		AccountID:  account.ID,
		Attributes: attrs,
	}
}

// mergeResult 合并同步结果
func (s *assetSyncService) mergeResult(target, source *SyncResult) {
	target.TotalSynced += source.TotalSynced
	target.Created += source.Created
	target.Updated += source.Updated
	target.Failed += source.Failed

	for k, v := range source.ByAssetType {
		target.ByAssetType[k] += v
	}
	for k, v := range source.ByRegion {
		target.ByRegion[k] += v
	}
}
