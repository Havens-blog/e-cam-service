package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// AssetSyncService 资产同步服务 - 同步到 CMDB c_instance
type AssetSyncService interface {
	// SyncAssets 同步云资产到 CMDB
	SyncAssets(ctx context.Context, tenantID, provider string, assetTypes []string) (*SyncResult, error)
	// SyncAccountAssets 同步指定账号的资产
	SyncAccountAssets(ctx context.Context, tenantID string, accountID int64, assetTypes []string) (*SyncResult, error)
	// SyncRelations 同步资产关系
	SyncRelations(ctx context.Context, tenantID string) (*RelationSyncResult, error)
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

// RelationSyncResult 关系同步结果
type RelationSyncResult struct {
	TotalSynced    int            `json:"total_synced"`
	Created        int            `json:"created"`
	Skipped        int            `json:"skipped"`
	Failed         int            `json:"failed"`
	ByRelationType map[string]int `json:"by_relation_type"`
	StartTime      time.Time      `json:"start_time"`
	EndTime        time.Time      `json:"end_time"`
	DurationMs     int64          `json:"duration_ms"`
}

type assetSyncService struct {
	instanceRepo   repository.InstanceRepository
	relationRepo   repository.InstanceRelationRepository
	accountRepo    repository.CloudAccountRepository
	adapterFactory *cloudx.AdapterFactory
	logger         *elog.Component
}

// NewAssetSyncService 创建资产同步服务
func NewAssetSyncService(
	instanceRepo repository.InstanceRepository,
	relationRepo repository.InstanceRelationRepository,
	accountRepo repository.CloudAccountRepository,
	adapterFactory *cloudx.AdapterFactory,
	logger *elog.Component,
) AssetSyncService {
	return &assetSyncService{
		instanceRepo:   instanceRepo,
		relationRepo:   relationRepo,
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

	// 使用 cloudx 适配器工厂创建适配器
	adapter, err := s.adapterFactory.CreateAdapter(account)
	if err != nil {
		return nil, fmt.Errorf("创建适配器失败: %w", err)
	}

	// 获取所有地域
	regions, err := adapter.ECS().GetRegions(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取地域列表失败: %w", err)
	}

	// 过滤支持的地域
	if len(account.Config.SupportedRegions) > 0 {
		regionMap := make(map[string]bool)
		for _, r := range account.Config.SupportedRegions {
			regionMap[r] = true
		}
		filtered := make([]types.Region, 0)
		for _, r := range regions {
			if regionMap[r.ID] {
				filtered = append(filtered, r)
			}
		}
		regions = filtered
	}

	s.logger.Debug("开始同步地域资产",
		elog.String("account", account.Name),
		elog.Int("region_count", len(regions)))

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
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
	assetTypes []string,
) (*SyncResult, error) {
	result := &SyncResult{
		ByAssetType: make(map[string]int),
		ByRegion:    make(map[string]int),
	}

	for _, assetType := range assetTypes {
		var synced *SyncResult
		var err error

		switch assetType {
		case "ecs", "cloud_vm":
			synced, err = s.syncECSInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步ECS实例失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["ecs"] += synced.TotalSynced

		case "rds", "cloud_rds":
			synced, err = s.syncRDSInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步RDS实例失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["rds"] += synced.TotalSynced

		case "redis", "cloud_redis":
			synced, err = s.syncRedisInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步Redis实例失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["redis"] += synced.TotalSynced

		case "mongodb", "cloud_mongodb":
			synced, err = s.syncMongoDBInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步MongoDB实例失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["mongodb"] += synced.TotalSynced

		case "vpc", "cloud_vpc":
			synced, err = s.syncVPCInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步VPC失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["vpc"] += synced.TotalSynced

		case "eip", "cloud_eip":
			synced, err = s.syncEIPInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步EIP失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["eip"] += synced.TotalSynced

		case "nas", "cloud_nas":
			synced, err = s.syncNASInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步NAS失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["nas"] += synced.TotalSynced

		case "oss", "cloud_oss":
			synced, err = s.syncOSSBuckets(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步OSS失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["oss"] += synced.TotalSynced

		case "database":
			// 聚合类型：同步所有数据库资源
			for _, dbType := range []string{"rds", "redis", "mongodb"} {
				dbResult, _ := s.syncRegion(ctx, tenantID, adapter, account, region, []string{dbType})
				if dbResult != nil {
					s.mergeResult(result, dbResult)
				}
			}
			continue

		case "network":
			// 聚合类型：同步所有网络资源
			for _, netType := range []string{"vpc", "eip"} {
				netResult, _ := s.syncRegion(ctx, tenantID, adapter, account, region, []string{netType})
				if netResult != nil {
					s.mergeResult(result, netResult)
				}
			}
			continue

		case "storage":
			// 聚合类型：同步所有存储资源
			for _, storageType := range []string{"nas", "oss"} {
				storageResult, _ := s.syncRegion(ctx, tenantID, adapter, account, region, []string{storageType})
				if storageResult != nil {
					s.mergeResult(result, storageResult)
				}
			}
			continue

		default:
			s.logger.Warn("不支持的资源类型", elog.String("asset_type", assetType))
			continue
		}

		if synced != nil {
			result.ByRegion[region] += synced.TotalSynced
			result.TotalSynced += synced.TotalSynced
			result.Created += synced.Created
			result.Updated += synced.Updated
		}
	}

	return result, nil
}

// syncECSInstances 同步 ECS 实例到 CMDB
func (s *assetSyncService) syncECSInstances(
	ctx context.Context,
	tenantID string,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{
		ByAssetType: make(map[string]int),
		ByRegion:    make(map[string]int),
	}

	instances, err := adapter.ECS().ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取ECS实例失败: %w", err)
	}

	if len(instances) == 0 {
		return result, nil
	}

	s.logger.Debug("获取到ECS实例",
		elog.String("region", region),
		elog.Int("count", len(instances)))

	for _, inst := range instances {
		cmdbInstance := s.convertECSToCMDBInstance(tenantID, account, inst)

		err := s.instanceRepo.Upsert(ctx, cmdbInstance)
		if err != nil {
			s.logger.Error("保存ECS实例失败",
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
	inst types.ECSInstance,
) domain.Instance {
	var securityGroupIDs []string
	for _, sg := range inst.SecurityGroups {
		securityGroupIDs = append(securityGroupIDs, sg.ID)
	}

	attrs := map[string]interface{}{
		"provider":             string(account.Provider),
		"cloud_account_id":     account.ID,
		"region":               inst.Region,
		"zone":                 inst.Zone,
		"instance_id":          inst.InstanceID,
		"instance_name":        inst.InstanceName,
		"status":               inst.Status,
		"create_time":          inst.CreationTime,
		"expire_time":          inst.ExpiredTime,
		"instance_type":        inst.InstanceType,
		"cpu":                  inst.CPU,
		"memory":               inst.Memory,
		"os_type":              inst.OSType,
		"os_name":              inst.OSName,
		"image_id":             inst.ImageID,
		"private_ip":           inst.PrivateIP,
		"public_ip":            inst.PublicIP,
		"vpc_id":               inst.VPCID,
		"subnet_id":            inst.VSwitchID,
		"security_groups":      securityGroupIDs,
		"system_disk_size":     inst.SystemDisk.Size,
		"system_disk_category": inst.SystemDisk.Category,
		"charge_type":          inst.ChargeType,
		"hostname":             inst.HostName,
		"key_pair_name":        inst.KeyPairName,
		"description":          inst.Description,
		"tags":                 inst.Tags,
	}

	return domain.Instance{
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

// ==================== RDS 同步 ====================

func (s *assetSyncService) syncRDSInstances(
	ctx context.Context,
	tenantID string,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}

	instances, err := adapter.RDS().ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取RDS实例失败: %w", err)
	}

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"region": inst.Region, "zone": inst.Zone, "instance_id": inst.InstanceID,
			"instance_name": inst.InstanceName, "status": inst.Status,
			"engine": inst.Engine, "engine_version": inst.EngineVersion,
			"instance_class": inst.DBInstanceClass, "cpu": inst.CPU, "memory": inst.Memory,
			"storage": inst.Storage, "storage_type": inst.StorageType,
			"connection_string": inst.ConnectionString, "port": inst.Port,
			"vpc_id": inst.VPCID, "subnet_id": inst.VSwitchID,
			"private_ip": inst.PrivateIP, "public_ip": inst.PublicIP,
			"category": inst.Category, "charge_type": inst.ChargeType,
			"create_time": inst.CreationTime, "expire_time": inst.ExpiredTime,
			"tags": inst.Tags, "description": inst.Description,
		}
		cmdbInstance := domain.Instance{
			ModelUID: "cloud_rds", AssetID: inst.InstanceID, AssetName: inst.InstanceName,
			TenantID: tenantID, AccountID: account.ID, Attributes: attrs,
		}
		if err := s.instanceRepo.Upsert(ctx, cmdbInstance); err != nil {
			result.Failed++
			continue
		}
		result.TotalSynced++
	}
	return result, nil
}

// ==================== Redis 同步 ====================

func (s *assetSyncService) syncRedisInstances(
	ctx context.Context,
	tenantID string,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}

	instances, err := adapter.Redis().ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取Redis实例失败: %w", err)
	}

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"region": inst.Region, "zone": inst.Zone, "instance_id": inst.InstanceID,
			"instance_name": inst.InstanceName, "status": inst.Status,
			"engine_version": inst.EngineVersion, "instance_class": inst.InstanceClass,
			"architecture": inst.Architecture, "capacity": inst.Capacity,
			"bandwidth": inst.Bandwidth, "connections": inst.Connections,
			"shard_count": inst.ShardCount, "connection_domain": inst.ConnectionDomain,
			"port": inst.Port, "vpc_id": inst.VPCID, "subnet_id": inst.VSwitchID,
			"private_ip": inst.PrivateIP, "charge_type": inst.ChargeType,
			"create_time": inst.CreationTime, "expire_time": inst.ExpiredTime,
			"tags": inst.Tags, "description": inst.Description,
		}
		cmdbInstance := domain.Instance{
			ModelUID: "cloud_redis", AssetID: inst.InstanceID, AssetName: inst.InstanceName,
			TenantID: tenantID, AccountID: account.ID, Attributes: attrs,
		}
		if err := s.instanceRepo.Upsert(ctx, cmdbInstance); err != nil {
			result.Failed++
			continue
		}
		result.TotalSynced++
	}
	return result, nil
}

// ==================== MongoDB 同步 ====================

func (s *assetSyncService) syncMongoDBInstances(
	ctx context.Context,
	tenantID string,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}

	instances, err := adapter.MongoDB().ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取MongoDB实例失败: %w", err)
	}

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"region": inst.Region, "zone": inst.Zone, "instance_id": inst.InstanceID,
			"instance_name": inst.InstanceName, "status": inst.Status,
			"engine_version": inst.EngineVersion, "instance_class": inst.InstanceClass,
			"db_type": inst.DBInstanceType, "cpu": inst.CPU, "memory": inst.Memory,
			"storage": inst.Storage, "storage_type": inst.StorageType,
			"shard_count": inst.ShardCount, "node_count": inst.NodeCount,
			"connection_string": inst.ConnectionString, "port": inst.Port,
			"vpc_id": inst.VPCID, "subnet_id": inst.VSwitchID,
			"charge_type": inst.ChargeType, "create_time": inst.CreationTime,
			"expire_time": inst.ExpiredTime, "tags": inst.Tags, "description": inst.Description,
		}
		cmdbInstance := domain.Instance{
			ModelUID: "cloud_mongodb", AssetID: inst.InstanceID, AssetName: inst.InstanceName,
			TenantID: tenantID, AccountID: account.ID, Attributes: attrs,
		}
		if err := s.instanceRepo.Upsert(ctx, cmdbInstance); err != nil {
			result.Failed++
			continue
		}
		result.TotalSynced++
	}
	return result, nil
}

// ==================== VPC 同步 ====================

func (s *assetSyncService) syncVPCInstances(
	ctx context.Context,
	tenantID string,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}

	instances, err := adapter.VPC().ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取VPC失败: %w", err)
	}

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"region": inst.Region, "vpc_id": inst.VPCID, "vpc_name": inst.VPCName,
			"status": inst.Status, "cidr_block": inst.CidrBlock,
			"secondary_cidrs": inst.SecondaryCidrs, "ipv6_cidr_block": inst.IPv6CidrBlock,
			"enable_ipv6": inst.EnableIPv6, "is_default": inst.IsDefault,
			"vswitch_count": inst.VSwitchCount, "route_table_count": inst.RouteTableCount,
			"nat_gateway_count": inst.NatGatewayCount, "security_group_count": inst.SecurityGroupCount,
			"create_time": inst.CreationTime, "tags": inst.Tags, "description": inst.Description,
		}
		cmdbInstance := domain.Instance{
			ModelUID: "cloud_vpc", AssetID: inst.VPCID, AssetName: inst.VPCName,
			TenantID: tenantID, AccountID: account.ID, Attributes: attrs,
		}
		if err := s.instanceRepo.Upsert(ctx, cmdbInstance); err != nil {
			result.Failed++
			continue
		}
		result.TotalSynced++
	}
	return result, nil
}

// ==================== EIP 同步 ====================

func (s *assetSyncService) syncEIPInstances(
	ctx context.Context,
	tenantID string,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}

	instances, err := adapter.EIP().ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取EIP失败: %w", err)
	}

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"region": inst.Region, "allocation_id": inst.AllocationID,
			"ip_address": inst.IPAddress, "status": inst.Status,
			"bandwidth": inst.Bandwidth, "internet_charge_type": inst.InternetChargeType,
			"isp": inst.ISP, "instance_id": inst.InstanceID,
			"instance_type": inst.InstanceType, "instance_name": inst.InstanceName,
			"vpc_id": inst.VPCID, "charge_type": inst.ChargeType,
			"create_time": inst.CreationTime, "expire_time": inst.ExpiredTime,
			"tags": inst.Tags, "description": inst.Description,
		}
		assetName := inst.Name
		if assetName == "" {
			assetName = inst.IPAddress
		}
		cmdbInstance := domain.Instance{
			ModelUID: "cloud_eip", AssetID: inst.AllocationID, AssetName: assetName,
			TenantID: tenantID, AccountID: account.ID, Attributes: attrs,
		}
		if err := s.instanceRepo.Upsert(ctx, cmdbInstance); err != nil {
			result.Failed++
			continue
		}
		result.TotalSynced++
	}
	return result, nil
}

// ==================== 资产关系同步 ====================

// SyncRelations 同步资产关系
func (s *assetSyncService) SyncRelations(ctx context.Context, tenantID string) (*RelationSyncResult, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}

	result := &RelationSyncResult{
		ByRelationType: make(map[string]int),
		StartTime:      time.Now(),
	}

	s.logger.Info("开始同步资产关系", elog.String("tenant_id", tenantID))

	// 1. 同步 ECS -> VPC 关系
	if r, err := s.syncECSToVPCRelations(ctx, tenantID); err == nil {
		result.Created += r.Created
		result.Skipped += r.Skipped
		result.Failed += r.Failed
		result.ByRelationType["ecs_belongs_to_vpc"] = r.Created
	}

	// 2. 同步 EIP -> ECS 关系
	if r, err := s.syncEIPToECSRelations(ctx, tenantID); err == nil {
		result.Created += r.Created
		result.Skipped += r.Skipped
		result.Failed += r.Failed
		result.ByRelationType["eip_bindto_ecs"] = r.Created
	}

	// 3. 同步 RDS -> VPC 关系
	if r, err := s.syncRDSToVPCRelations(ctx, tenantID); err == nil {
		result.Created += r.Created
		result.Skipped += r.Skipped
		result.Failed += r.Failed
		result.ByRelationType["rds_belongs_to_vpc"] = r.Created
	}

	// 4. 同步 Redis -> VPC 关系
	if r, err := s.syncRedisToVPCRelations(ctx, tenantID); err == nil {
		result.Created += r.Created
		result.Skipped += r.Skipped
		result.Failed += r.Failed
		result.ByRelationType["redis_belongs_to_vpc"] = r.Created
	}

	result.TotalSynced = result.Created
	result.EndTime = time.Now()
	result.DurationMs = result.EndTime.Sub(result.StartTime).Milliseconds()

	s.logger.Info("资产关系同步完成",
		elog.Int("total_created", result.Created),
		elog.Int("skipped", result.Skipped),
		elog.Int64("duration_ms", result.DurationMs))

	return result, nil
}

type relationSyncPartialResult struct {
	Created int
	Skipped int
	Failed  int
}

func (s *assetSyncService) syncECSToVPCRelations(ctx context.Context, tenantID string) (*relationSyncPartialResult, error) {
	result := &relationSyncPartialResult{}

	ecsInstances, err := s.instanceRepo.List(ctx, domain.InstanceFilter{TenantID: tenantID, ModelUID: "cloud_vm"})
	if err != nil {
		return nil, err
	}

	vpcInstances, err := s.instanceRepo.List(ctx, domain.InstanceFilter{TenantID: tenantID, ModelUID: "cloud_vpc"})
	if err != nil {
		return nil, err
	}

	vpcMap := make(map[string]int64)
	for _, vpc := range vpcInstances {
		if vpcID, ok := vpc.Attributes["vpc_id"].(string); ok && vpcID != "" {
			vpcMap[vpcID] = vpc.ID
		}
	}

	for _, ecs := range ecsInstances {
		vpcID, ok := ecs.Attributes["vpc_id"].(string)
		if !ok || vpcID == "" {
			continue
		}
		targetVPCID, exists := vpcMap[vpcID]
		if !exists {
			result.Skipped++
			continue
		}
		exists, _ = s.relationRepo.Exists(ctx, ecs.ID, targetVPCID, "ecs_belongs_to_vpc")
		if exists {
			result.Skipped++
			continue
		}
		_, err = s.relationRepo.Create(ctx, domain.InstanceRelation{
			SourceInstanceID: ecs.ID, TargetInstanceID: targetVPCID,
			RelationTypeUID: "ecs_belongs_to_vpc", TenantID: tenantID,
		})
		if err != nil {
			result.Failed++
			continue
		}
		result.Created++
	}
	return result, nil
}

func (s *assetSyncService) syncEIPToECSRelations(ctx context.Context, tenantID string) (*relationSyncPartialResult, error) {
	result := &relationSyncPartialResult{}

	eipInstances, err := s.instanceRepo.List(ctx, domain.InstanceFilter{TenantID: tenantID, ModelUID: "cloud_eip"})
	if err != nil {
		return nil, err
	}

	ecsInstances, err := s.instanceRepo.List(ctx, domain.InstanceFilter{TenantID: tenantID, ModelUID: "cloud_vm"})
	if err != nil {
		return nil, err
	}

	ecsMap := make(map[string]int64)
	for _, ecs := range ecsInstances {
		if instanceID, ok := ecs.Attributes["instance_id"].(string); ok && instanceID != "" {
			ecsMap[instanceID] = ecs.ID
		}
	}

	for _, eip := range eipInstances {
		instanceType, _ := eip.Attributes["instance_type"].(string)
		instanceID, _ := eip.Attributes["instance_id"].(string)
		if instanceID == "" || (instanceType != "" && instanceType != "EcsInstance" && instanceType != "Ecs") {
			continue
		}
		targetECSID, exists := ecsMap[instanceID]
		if !exists {
			result.Skipped++
			continue
		}
		exists, _ = s.relationRepo.Exists(ctx, eip.ID, targetECSID, "eip_bindto_ecs")
		if exists {
			result.Skipped++
			continue
		}
		_, err = s.relationRepo.Create(ctx, domain.InstanceRelation{
			SourceInstanceID: eip.ID, TargetInstanceID: targetECSID,
			RelationTypeUID: "eip_bindto_ecs", TenantID: tenantID,
		})
		if err != nil {
			result.Failed++
			continue
		}
		result.Created++
	}
	return result, nil
}

func (s *assetSyncService) syncRDSToVPCRelations(ctx context.Context, tenantID string) (*relationSyncPartialResult, error) {
	result := &relationSyncPartialResult{}

	rdsInstances, err := s.instanceRepo.List(ctx, domain.InstanceFilter{TenantID: tenantID, ModelUID: "cloud_rds"})
	if err != nil {
		return nil, err
	}

	vpcInstances, err := s.instanceRepo.List(ctx, domain.InstanceFilter{TenantID: tenantID, ModelUID: "cloud_vpc"})
	if err != nil {
		return nil, err
	}

	vpcMap := make(map[string]int64)
	for _, vpc := range vpcInstances {
		if vpcID, ok := vpc.Attributes["vpc_id"].(string); ok && vpcID != "" {
			vpcMap[vpcID] = vpc.ID
		}
	}

	for _, rds := range rdsInstances {
		vpcID, ok := rds.Attributes["vpc_id"].(string)
		if !ok || vpcID == "" {
			continue
		}
		targetVPCID, exists := vpcMap[vpcID]
		if !exists {
			result.Skipped++
			continue
		}
		exists, _ = s.relationRepo.Exists(ctx, rds.ID, targetVPCID, "rds_belongs_to_vpc")
		if exists {
			result.Skipped++
			continue
		}
		_, err = s.relationRepo.Create(ctx, domain.InstanceRelation{
			SourceInstanceID: rds.ID, TargetInstanceID: targetVPCID,
			RelationTypeUID: "rds_belongs_to_vpc", TenantID: tenantID,
		})
		if err != nil {
			result.Failed++
			continue
		}
		result.Created++
	}
	return result, nil
}

func (s *assetSyncService) syncRedisToVPCRelations(ctx context.Context, tenantID string) (*relationSyncPartialResult, error) {
	result := &relationSyncPartialResult{}

	redisInstances, err := s.instanceRepo.List(ctx, domain.InstanceFilter{TenantID: tenantID, ModelUID: "cloud_redis"})
	if err != nil {
		return nil, err
	}

	vpcInstances, err := s.instanceRepo.List(ctx, domain.InstanceFilter{TenantID: tenantID, ModelUID: "cloud_vpc"})
	if err != nil {
		return nil, err
	}

	vpcMap := make(map[string]int64)
	for _, vpc := range vpcInstances {
		if vpcID, ok := vpc.Attributes["vpc_id"].(string); ok && vpcID != "" {
			vpcMap[vpcID] = vpc.ID
		}
	}

	for _, redis := range redisInstances {
		vpcID, ok := redis.Attributes["vpc_id"].(string)
		if !ok || vpcID == "" {
			continue
		}
		targetVPCID, exists := vpcMap[vpcID]
		if !exists {
			result.Skipped++
			continue
		}
		exists, _ = s.relationRepo.Exists(ctx, redis.ID, targetVPCID, "redis_belongs_to_vpc")
		if exists {
			result.Skipped++
			continue
		}
		_, err = s.relationRepo.Create(ctx, domain.InstanceRelation{
			SourceInstanceID: redis.ID, TargetInstanceID: targetVPCID,
			RelationTypeUID: "redis_belongs_to_vpc", TenantID: tenantID,
		})
		if err != nil {
			result.Failed++
			continue
		}
		result.Created++
	}
	return result, nil
}

// ==================== NAS 同步 ====================

func (s *assetSyncService) syncNASInstances(
	ctx context.Context,
	tenantID string,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}

	instances, err := adapter.NAS().ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取NAS文件系统失败: %w", err)
	}

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"region": inst.Region, "zone": inst.Zone, "file_system_id": inst.FileSystemID,
			"status": inst.Status, "file_system_type": inst.FileSystemType,
			"protocol_type": inst.ProtocolType, "storage_type": inst.StorageType,
			"capacity": inst.Capacity, "metered_size": inst.MeteredSize,
			"vpc_id": inst.VPCID, "vswitch_id": inst.VSwitchID,
			"charge_type": inst.ChargeType, "encrypt_type": inst.EncryptType,
			"kms_key_id": inst.KMSKeyID, "mount_targets": inst.MountTargets,
			"create_time": inst.CreationTime, "tags": inst.Tags, "description": inst.Description,
		}
		assetName := inst.Description
		if assetName == "" {
			assetName = inst.FileSystemID
		}
		cmdbInstance := domain.Instance{
			ModelUID: "cloud_nas", AssetID: inst.FileSystemID, AssetName: assetName,
			TenantID: tenantID, AccountID: account.ID, Attributes: attrs,
		}
		if err := s.instanceRepo.Upsert(ctx, cmdbInstance); err != nil {
			result.Failed++
			continue
		}
		result.TotalSynced++
	}
	return result, nil
}

// ==================== OSS 同步 ====================

func (s *assetSyncService) syncOSSBuckets(
	ctx context.Context,
	tenantID string,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}

	buckets, err := adapter.OSS().ListBuckets(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取OSS存储桶失败: %w", err)
	}

	for _, bucket := range buckets {
		// 如果指定了region，只同步该region的bucket
		if region != "" && bucket.Region != region {
			continue
		}

		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"region": bucket.Region, "bucket_name": bucket.BucketName,
			"storage_class": bucket.StorageClass, "acl": bucket.ACL,
			"versioning":  bucket.Versioning,
			"create_time": bucket.CreationTime, "tags": bucket.Tags,
		}
		cmdbInstance := domain.Instance{
			ModelUID: "cloud_oss", AssetID: bucket.BucketName, AssetName: bucket.BucketName,
			TenantID: tenantID, AccountID: account.ID, Attributes: attrs,
		}
		if err := s.instanceRepo.Upsert(ctx, cmdbInstance); err != nil {
			result.Failed++
			continue
		}
		result.TotalSynced++
	}
	return result, nil
}
