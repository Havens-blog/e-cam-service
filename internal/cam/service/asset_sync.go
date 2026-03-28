package service

import (
	"context"
	"fmt"
	"time"

	auditdomain "github.com/Havens-blog/e-cam-service/internal/audit/domain"
	auditservice "github.com/Havens-blog/e-cam-service/internal/audit/service"
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
	// SetChangeTracker 设置变更追踪器（可选）
	SetChangeTracker(ct *auditservice.ChangeTracker)
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
	changeTracker  *auditservice.ChangeTracker // 资产变更追踪器（可选）
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

// SetChangeTracker 设置变更追踪器（可选注入，不影响原有构造函数签名）
func (s *assetSyncService) SetChangeTracker(ct *auditservice.ChangeTracker) {
	s.changeTracker = ct
}

// trackAndUpsert 在 Upsert 前追踪变更，然后执行 Upsert
func (s *assetSyncService) trackAndUpsert(ctx context.Context, instance domain.Instance) error {
	if s.changeTracker != nil {
		// 查询旧实例
		old, err := s.instanceRepo.GetByAssetID(ctx, instance.TenantID, instance.ModelUID, instance.AssetID)
		if err != nil {
			s.logger.Warn("查询旧实例用于变更追踪失败",
				elog.FieldErr(err),
				elog.String("asset_id", instance.AssetID),
			)
		} else if old.AssetID != "" && old.Attributes != nil {
			// 旧实例存在，追踪变更
			meta := auditdomain.ChangeMetadata{
				AssetID:      instance.AssetID,
				AssetName:    instance.AssetName,
				ModelUID:     instance.ModelUID,
				TenantID:     instance.TenantID,
				AccountID:    instance.AccountID,
				ChangeSource: "sync_task",
			}
			// 从新属性中提取 provider 和 region
			if p, ok := instance.Attributes["provider"].(string); ok {
				meta.Provider = p
			}
			if r, ok := instance.Attributes["region"].(string); ok {
				meta.Region = r
			}
			// TrackChanges 内部失败仅记录日志，不影响同步
			_, _ = s.changeTracker.TrackChanges(ctx, meta, old.Attributes, instance.Attributes)
		}
	}
	return s.instanceRepo.Upsert(ctx, instance)
}

// SyncAssets 同步云资产到 CMDB
func (s *assetSyncService) SyncAssets(ctx context.Context, tenantID, provider string, assetTypes []string) (*SyncResult, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}

	if len(assetTypes) == 0 {
		assetTypes = []string{
			"ecs", "disk", "snapshot", "security_group", "image",
			"rds", "redis", "mongodb",
			"vpc", "eip", "lb", "cdn", "waf",
			"nas", "oss",
		}
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
		assetTypes = []string{
			"ecs", "disk", "snapshot", "security_group", "image",
			"rds", "redis", "mongodb",
			"vpc", "eip", "lb", "cdn", "waf",
			"nas", "oss",
		}
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

		case "vswitch", "cloud_vswitch", "subnet":
			synced, err = s.syncVSwitchInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步VSwitch失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["vswitch"] += synced.TotalSynced

		case "lb", "cloud_lb", "slb", "alb", "nlb":
			synced, err = s.syncLBInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步LB失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["lb"] += synced.TotalSynced

		case "cdn", "cloud_cdn":
			synced, err = s.syncCDNInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步CDN失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["cdn"] += synced.TotalSynced

		case "waf", "cloud_waf":
			synced, err = s.syncWAFInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步WAF失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["waf"] += synced.TotalSynced

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

		case "disk", "cloud_disk":
			synced, err = s.syncDiskInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步云盘失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["disk"] += synced.TotalSynced

		case "snapshot", "cloud_snapshot":
			synced, err = s.syncSnapshotInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步快照失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["snapshot"] += synced.TotalSynced

		case "security_group", "cloud_security_group":
			synced, err = s.syncSecurityGroupInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步安全组失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["security_group"] += synced.TotalSynced

		case "image", "cloud_image":
			synced, err = s.syncImageInstances(ctx, tenantID, adapter, account, region)
			if err != nil {
				s.logger.Error("同步镜像失败", elog.String("region", region), elog.FieldErr(err))
				result.Failed++
				continue
			}
			result.ByAssetType["image"] += synced.TotalSynced

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
			for _, netType := range []string{"vpc", "eip", "lb", "cdn", "waf"} {
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

		err := s.trackAndUpsert(ctx, cmdbInstance)
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
		if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
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
		if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
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
		if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
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
		if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
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
		if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
			result.Failed++
			continue
		}
		result.TotalSynced++
	}
	return result, nil
}

// syncVSwitchInstances 同步交换机/子网实例
func (s *assetSyncService) syncVSwitchInstances(
	ctx context.Context,
	tenantID string,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}

	instances, err := adapter.VSwitch().ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取VSwitch失败: %w", err)
	}

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider":           string(account.Provider),
			"cloud_account_id":   account.ID,
			"region":             inst.Region,
			"zone":               inst.Zone,
			"vswitch_id":         inst.VSwitchID,
			"status":             inst.Status,
			"cidr_block":         inst.CidrBlock,
			"ipv6_cidr_block":    inst.IPv6CidrBlock,
			"enable_ipv6":        inst.EnableIPv6,
			"is_default":         inst.IsDefault,
			"gateway_ip":         inst.GatewayIP,
			"vpc_id":             inst.VPCID,
			"vpc_name":           inst.VPCName,
			"available_ip_count": inst.AvailableIPCount,
			"total_ip_count":     inst.TotalIPCount,
			"route_table_id":     inst.RouteTableID,
			"create_time":        inst.CreationTime,
			"resource_group_id":  inst.ResourceGroupID,
			"tags":               inst.Tags,
			"description":        inst.Description,
		}
		assetName := inst.VSwitchName
		if assetName == "" {
			assetName = inst.VSwitchID
		}
		cmdbInstance := domain.Instance{
			ModelUID:   "cloud_vswitch",
			AssetID:    inst.VSwitchID,
			AssetName:  assetName,
			TenantID:   tenantID,
			AccountID:  account.ID,
			Attributes: attrs,
		}
		if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
			result.Failed++
			continue
		}
		result.TotalSynced++
	}
	return result, nil
}

// syncLBInstances 同步负载均衡实例
func (s *assetSyncService) syncLBInstances(
	ctx context.Context,
	tenantID string,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}

	lbAdapter := adapter.LB()
	if lbAdapter == nil {
		s.logger.Warn("LB适配器不可用", elog.String("provider", string(account.Provider)))
		return result, nil
	}

	instances, err := lbAdapter.ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取LB失败: %w", err)
	}

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"region": inst.Region, "load_balancer_id": inst.LoadBalancerID,
			"load_balancer_name": inst.LoadBalancerName, "load_balancer_type": inst.LoadBalancerType,
			"status": inst.Status, "address": inst.Address,
			"address_type": inst.AddressType, "address_ip_version": inst.AddressIPVersion,
			"vpc_id": inst.VPCID, "vswitch_id": inst.VSwitchID,
			"network_type": inst.NetworkType, "load_balancer_spec": inst.LoadBalancerSpec,
			"bandwidth": inst.Bandwidth, "internet_charge_type": inst.InternetChargeType,
			"charge_type": inst.ChargeType, "zone": inst.Zone,
			"slave_zone": inst.SlaveZone, "listener_count": inst.ListenerCount,
			"backend_server_count": inst.BackendServerCount,
			"creation_time":        inst.CreationTime, "expired_time": inst.ExpiredTime,
			"resource_group_id": inst.ResourceGroupID,
			"tags":              inst.Tags, "description": inst.Description,
		}
		assetName := inst.LoadBalancerName
		if assetName == "" {
			assetName = inst.LoadBalancerID
		}
		cmdbInstance := domain.Instance{
			ModelUID: "cloud_lb", AssetID: inst.LoadBalancerID, AssetName: assetName,
			TenantID: tenantID, AccountID: account.ID, Attributes: attrs,
		}
		if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
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
			"capacity": inst.Capacity, "used_capacity": inst.UsedCapacity, "metered_size": inst.MeteredSize,
			"vpc_id": inst.VPCID, "vswitch_id": inst.VSwitchID,
			"charge_type": inst.ChargeType, "encrypt_type": inst.EncryptType,
			"kms_key_id": inst.KMSKeyID, "mount_targets": inst.MountTargets,
			"mount_target_count": len(inst.MountTargets),
			"create_time":        inst.CreationTime, "tags": inst.Tags, "description": inst.Description,
		}
		assetName := inst.Description
		if assetName == "" {
			assetName = inst.FileSystemID
		}
		cmdbInstance := domain.Instance{
			ModelUID: "cloud_nas", AssetID: inst.FileSystemID, AssetName: assetName,
			TenantID: tenantID, AccountID: account.ID, Attributes: attrs,
		}
		if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
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
		if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
			result.Failed++
			continue
		}
		result.TotalSynced++
	}
	return result, nil
}

// ==================== 云盘同步 ====================

func (s *assetSyncService) syncDiskInstances(
	ctx context.Context,
	tenantID string,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}

	diskAdapter := adapter.Disk()
	if diskAdapter == nil {
		s.logger.Warn("Disk适配器不可用", elog.String("provider", string(account.Provider)))
		return result, nil
	}

	instances, err := diskAdapter.ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取云盘失败: %w", err)
	}

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"region": inst.Region, "zone": inst.Zone,
			"disk_id": inst.DiskID, "disk_name": inst.DiskName,
			"disk_type": inst.DiskType, "category": inst.Category,
			"performance_level": inst.PerformanceLevel,
			"size":              inst.Size, "iops": inst.IOPS, "throughput": inst.Throughput,
			"status": inst.Status, "portable": inst.Portable,
			"delete_with_instance": inst.DeleteWithInstance,
			"enable_auto_snapshot": inst.EnableAutoSnapshot,
			"instance_id":          inst.InstanceID, "instance_name": inst.InstanceName,
			"device": inst.Device, "attached_time": inst.AttachedTime,
			"encrypted": inst.Encrypted, "kms_key_id": inst.KMSKeyID,
			"source_snapshot_id":      inst.SourceSnapshotID,
			"auto_snapshot_policy_id": inst.AutoSnapshotPolicyID,
			"snapshot_count":          inst.SnapshotCount,
			"image_id":                inst.ImageID,
			"charge_type":             inst.ChargeType, "expired_time": inst.ExpiredTime,
			"resource_group_id": inst.ResourceGroupID,
			"creation_time":     inst.CreationTime,
			"tags":              inst.Tags, "description": inst.Description,
			"multi_attach": inst.MultiAttach,
		}
		assetName := inst.DiskName
		if assetName == "" {
			assetName = inst.DiskID
		}
		cmdbInstance := domain.Instance{
			ModelUID: "cloud_disk", AssetID: inst.DiskID, AssetName: assetName,
			TenantID: tenantID, AccountID: account.ID, Attributes: attrs,
		}
		if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
			result.Failed++
			continue
		}
		result.TotalSynced++
	}
	return result, nil
}

// ==================== 快照同步 ====================

func (s *assetSyncService) syncSnapshotInstances(
	ctx context.Context,
	tenantID string,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}

	snapshotAdapter := adapter.Snapshot()
	if snapshotAdapter == nil {
		s.logger.Warn("Snapshot适配器不可用", elog.String("provider", string(account.Provider)))
		return result, nil
	}

	instances, err := snapshotAdapter.ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取快照失败: %w", err)
	}

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"region":      inst.Region,
			"snapshot_id": inst.SnapshotID, "snapshot_name": inst.SnapshotName,
			"snapshot_type": inst.SnapshotType, "category": inst.Category,
			"instant_access": inst.InstantAccess,
			"status":         inst.Status, "progress": inst.Progress,
			"source_disk_size": inst.SourceDiskSize, "snapshot_size": inst.SnapshotSize,
			"source_disk_id": inst.SourceDiskID, "source_disk_type": inst.SourceDiskType,
			"source_disk_category": inst.SourceDiskCategory,
			"source_instance_id":   inst.SourceInstanceID, "source_instance_name": inst.SourceInstanceName,
			"encrypted": inst.Encrypted, "kms_key_id": inst.KMSKeyID,
			"usage": inst.Usage, "retention_days": inst.RetentionDays,
			"resource_group_id": inst.ResourceGroupID,
			"creation_time":     inst.CreationTime,
			"tags":              inst.Tags, "description": inst.Description,
		}
		assetName := inst.SnapshotName
		if assetName == "" {
			assetName = inst.SnapshotID
		}
		cmdbInstance := domain.Instance{
			ModelUID: "cloud_snapshot", AssetID: inst.SnapshotID, AssetName: assetName,
			TenantID: tenantID, AccountID: account.ID, Attributes: attrs,
		}
		if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
			result.Failed++
			continue
		}
		result.TotalSynced++
	}
	return result, nil
}

// ==================== 安全组同步 ====================

func (s *assetSyncService) syncSecurityGroupInstances(
	ctx context.Context,
	tenantID string,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}

	sgAdapter := adapter.SecurityGroup()
	if sgAdapter == nil {
		s.logger.Warn("SecurityGroup适配器不可用", elog.String("provider", string(account.Provider)))
		return result, nil
	}

	instances, err := sgAdapter.ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取安全组失败: %w", err)
	}

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"region":              inst.Region,
			"security_group_id":   inst.SecurityGroupID,
			"security_group_name": inst.SecurityGroupName,
			"security_group_type": inst.SecurityGroupType,
			"vpc_id":              inst.VPCID, "vpc_name": inst.VPCName,
			"ingress_rule_count": inst.IngressRuleCount,
			"egress_rule_count":  inst.EgressRuleCount,
			"instance_count":     inst.InstanceCount,
			"instance_ids":       inst.InstanceIDs,
			"resource_group_id":  inst.ResourceGroupID,
			"creation_time":      inst.CreationTime,
			"tags":               inst.Tags, "description": inst.Description,
		}
		assetName := inst.SecurityGroupName
		if assetName == "" {
			assetName = inst.SecurityGroupID
		}
		cmdbInstance := domain.Instance{
			ModelUID: "cloud_security_group", AssetID: inst.SecurityGroupID, AssetName: assetName,
			TenantID: tenantID, AccountID: account.ID, Attributes: attrs,
		}
		if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
			result.Failed++
			continue
		}
		result.TotalSynced++
	}
	return result, nil
}

// ==================== 镜像同步 ====================

func (s *assetSyncService) syncImageInstances(
	ctx context.Context,
	tenantID string,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}

	imageAdapter := adapter.Image()
	if imageAdapter == nil {
		s.logger.Warn("Image适配器不可用", elog.String("provider", string(account.Provider)))
		return result, nil
	}

	instances, err := imageAdapter.ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取镜像失败: %w", err)
	}

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"region": inst.Region, "image_id": inst.ImageID,
			"image_name": inst.ImageName, "status": inst.Status,
			"image_owner_alias": inst.ImageOwnerAlias, "os_type": inst.OSType,
			"os_name": inst.OSName, "platform": inst.Platform,
			"architecture": inst.Architecture, "size": inst.Size,
			"description": inst.Description, "creation_time": inst.CreationTime,
			"source_instance_id":   inst.SourceInstanceID,
			"source_snapshot_id":   inst.SourceSnapshotID,
			"disk_device_mappings": inst.DiskDeviceMappings,
			"boot_mode":            inst.BootMode, "tags": inst.Tags,
			"instance_count": inst.InstanceCount,
		}
		assetName := inst.ImageName
		if assetName == "" {
			assetName = inst.ImageID
		}
		cmdbInstance := domain.Instance{
			ModelUID: fmt.Sprintf("%s_image", account.Provider),
			AssetID:  inst.ImageID, AssetName: assetName,
			TenantID: tenantID, AccountID: account.ID, Attributes: attrs,
		}
		if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
			result.Failed++
			continue
		}
		result.TotalSynced++
	}
	return result, nil
}

// ==================== CDN 同步 ====================

func (s *assetSyncService) syncCDNInstances(
	ctx context.Context,
	tenantID string,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}

	cdnAdapter := adapter.CDN()
	if cdnAdapter == nil {
		s.logger.Warn("CDN适配器不可用", elog.String("provider", string(account.Provider)))
		return result, nil
	}

	instances, err := cdnAdapter.ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取CDN失败: %w", err)
	}

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"domain_id": inst.DomainID, "domain_name": inst.DomainName,
			"cname": inst.Cname, "status": inst.Status,
			"region": inst.Region, "business_type": inst.BusinessType,
			"service_area": inst.ServiceArea, "origin_type": inst.OriginType,
			"origin_host": inst.OriginHost, "https_enabled": inst.HTTPSEnabled,
			"cert_name": inst.CertName, "http2_enabled": inst.HTTP2Enabled,
			"bandwidth": inst.Bandwidth, "traffic_total": inst.TrafficTotal,
			"creation_time": inst.CreationTime, "modified_time": inst.ModifiedTime,
			"resource_group_id": inst.ResourceGroupID,
			"tags":              inst.Tags, "description": inst.Description,
		}
		assetID := inst.DomainName
		if inst.DomainID != "" {
			assetID = inst.DomainID
		}
		assetName := inst.DomainName
		cmdbInstance := domain.Instance{
			ModelUID: "cloud_cdn", AssetID: assetID, AssetName: assetName,
			TenantID: tenantID, AccountID: account.ID, Attributes: attrs,
		}
		if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
			result.Failed++
			continue
		}
		result.TotalSynced++
	}
	return result, nil
}

// ==================== WAF 同步 ====================

func (s *assetSyncService) syncWAFInstances(
	ctx context.Context,
	tenantID string,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}

	wafAdapter := adapter.WAF()
	if wafAdapter == nil {
		s.logger.Warn("WAF适配器不可用", elog.String("provider", string(account.Provider)))
		return result, nil
	}

	instances, err := wafAdapter.ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取WAF失败: %w", err)
	}

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"instance_id": inst.InstanceID, "instance_name": inst.InstanceName,
			"status": inst.Status, "region": inst.Region,
			"edition": inst.Edition, "domain_count": inst.DomainCount,
			"domain_limit": inst.DomainLimit, "rule_count": inst.RuleCount,
			"acl_rule_count": inst.ACLRuleCount, "cc_rule_count": inst.CCRuleCount,
			"rate_limit_count": inst.RateLimitCount,
			"waf_enabled":      inst.WAFEnabled, "cc_enabled": inst.CCEnabled,
			"anti_bot_enabled": inst.AntiBotEnabled,
			"qps":              inst.QPS, "bandwidth": inst.Bandwidth,
			"exclusive_ip": inst.ExclusiveIP, "pay_type": inst.PayType,
			"creation_time": inst.CreationTime, "expired_time": inst.ExpiredTime,
			"resource_group_id": inst.ResourceGroupID,
			"tags":              inst.Tags, "description": inst.Description,
		}
		assetName := inst.InstanceName
		if assetName == "" {
			assetName = inst.InstanceID
		}
		cmdbInstance := domain.Instance{
			ModelUID: "cloud_waf", AssetID: inst.InstanceID, AssetName: assetName,
			TenantID: tenantID, AccountID: account.ID, Attributes: attrs,
		}
		if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
			result.Failed++
			continue
		}
		result.TotalSynced++
	}
	return result, nil
}
