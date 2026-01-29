package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/asset"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/gotomicro/ego/core/elog"
)

// 定义任务类型常量
const (
	TaskTypeSyncAssets taskx.TaskType = "cam:sync_assets"
)

// SyncAssetsExecutor 同步资产任务执行器
type SyncAssetsExecutor struct {
	accountRepo    repository.CloudAccountRepository
	instanceRepo   repository.InstanceRepository
	adapterFactory *asset.AdapterFactory
	cloudxFactory  *cloudx.AdapterFactory
	taskRepo       taskx.TaskRepository
	logger         *elog.Component
}

// NewSyncAssetsExecutor 创建同步资产任务执行器
func NewSyncAssetsExecutor(
	accountRepo repository.CloudAccountRepository,
	instanceRepo repository.InstanceRepository,
	adapterFactory *asset.AdapterFactory,
	taskRepo taskx.TaskRepository,
	logger *elog.Component,
) *SyncAssetsExecutor {
	return &SyncAssetsExecutor{
		accountRepo:    accountRepo,
		instanceRepo:   instanceRepo,
		adapterFactory: adapterFactory,
		cloudxFactory:  cloudx.NewAdapterFactory(logger),
		taskRepo:       taskRepo,
		logger:         logger,
	}
}

// GetType 获取任务类型
func (e *SyncAssetsExecutor) GetType() taskx.TaskType {
	return TaskTypeSyncAssets
}

// Execute 执行任务
func (e *SyncAssetsExecutor) Execute(ctx context.Context, t *taskx.Task) error {
	e.logger.Info("开始执行同步资产任务", elog.String("task_id", t.ID))

	// 解析任务参数
	var params SyncAssetsParams
	paramsBytes, err := json.Marshal(t.Params)
	if err != nil {
		return fmt.Errorf("序列化任务参数失败: %w", err)
	}
	if err := json.Unmarshal(paramsBytes, &params); err != nil {
		return fmt.Errorf("解析任务参数失败: %w", err)
	}

	e.logger.Info("任务参数",
		elog.Int64("account_id", params.AccountID),
		elog.Any("asset_types", params.AssetTypes))

	// 更新进度: 开始同步
	e.taskRepo.UpdateProgress(ctx, t.ID, 10, "正在获取云账号信息")

	// 获取云账号
	account, err := e.accountRepo.GetByID(ctx, params.AccountID)
	if err != nil {
		return fmt.Errorf("获取云账号失败: %w", err)
	}

	// 更新进度
	e.taskRepo.UpdateProgress(ctx, t.ID, 20, "正在创建云适配器")

	// 创建适配器
	adapter, err := e.adapterFactory.CreateAdapterFromDomain(&account)
	if err != nil {
		return fmt.Errorf("创建适配器失败: %w", err)
	}

	// 更新进度
	e.taskRepo.UpdateProgress(ctx, t.ID, 30, "正在获取地域列表")

	// 获取地域列表
	regions, err := adapter.GetRegions(ctx)
	if err != nil {
		return fmt.Errorf("获取地域列表失败: %w", err)
	}

	// 过滤地域
	if len(params.Regions) > 0 {
		regionMap := make(map[string]bool)
		for _, r := range params.Regions {
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

	// 同步资产
	totalSynced := 0
	totalRegions := len(regions)

	for i, region := range regions {
		progress := 30 + (i*60)/totalRegions
		e.taskRepo.UpdateProgress(ctx, t.ID, progress, fmt.Sprintf("正在同步地域 %s (%d/%d)", region.ID, i+1, totalRegions))

		synced, err := e.syncRegionAssets(ctx, adapter, &account, region.ID, params.AssetTypes)
		if err != nil {
			e.logger.Error("同步地域资产失败",
				elog.String("region", region.ID),
				elog.FieldErr(err))
			continue
		}
		totalSynced += synced
	}

	// 更新同步时间
	e.taskRepo.UpdateProgress(ctx, t.ID, 95, "正在更新同步状态")

	// 更新云账号的最后同步时间
	if err := e.accountRepo.UpdateSyncTime(ctx, params.AccountID, time.Now(), int64(totalSynced)); err != nil {
		e.logger.Error("更新同步时间失败",
			elog.Int64("account_id", params.AccountID),
			elog.FieldErr(err))
	}

	// 构建结果
	result := SyncAssetsResult{
		TotalCount: totalSynced,
		Details: map[string]any{
			"regions_synced": len(regions),
			"asset_types":    params.AssetTypes,
		},
	}

	resultBytes, _ := json.Marshal(result)
	var resultMap map[string]any
	json.Unmarshal(resultBytes, &resultMap)

	t.Result = resultMap
	t.Progress = 100
	t.Message = fmt.Sprintf("同步完成，共同步 %d 个资产", totalSynced)

	e.logger.Info("同步资产任务执行完成",
		elog.String("task_id", t.ID),
		elog.Int("total_synced", totalSynced))

	return nil
}

// syncRegionAssets 同步单个地域的资产
func (e *SyncAssetsExecutor) syncRegionAssets(
	ctx context.Context,
	adapter asset.CloudAssetAdapter,
	account *domain.CloudAccount,
	region string,
	assetTypes []string,
) (int, error) {
	totalSynced := 0

	// 获取 cloudx 适配器用于数据库资源同步
	var cloudxAdapter cloudx.CloudAdapter
	var cloudxErr error

	for _, assetType := range assetTypes {
		switch assetType {
		case "ecs":
			synced, err := e.syncRegionECS(ctx, adapter, account, region)
			if err != nil {
				e.logger.Error("同步ECS失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "rds":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionRDS(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步RDS失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "redis":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionRedis(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步Redis失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "mongodb":
			// 懒加载 cloudx 适配器
			if cloudxAdapter == nil && cloudxErr == nil {
				cloudxAdapter, cloudxErr = e.cloudxFactory.CreateAdapter(account)
				if cloudxErr != nil {
					e.logger.Error("创建cloudx适配器失败", elog.FieldErr(cloudxErr))
				}
			}
			if cloudxAdapter == nil {
				continue
			}
			synced, err := e.syncRegionMongoDB(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步MongoDB失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		default:
			e.logger.Warn("不支持的资源类型", elog.String("asset_type", assetType))
		}
	}

	return totalSynced, nil
}

// syncRegionECS 同步单个地域的 ECS 实例
func (e *SyncAssetsExecutor) syncRegionECS(
	ctx context.Context,
	adapter asset.CloudAssetAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_ecs", account.Provider)

	// 获取云端实例
	cloudInstances, err := adapter.GetECSInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取ECS实例失败: %w", err)
	}

	// 获取本地实例 AssetID 列表
	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	// 构建云端 AssetID 集合
	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.InstanceID] = true
	}

	// 删除已不存在的实例
	var toDelete []string
	for _, assetID := range localAssetIDs {
		if !cloudAssetIDSet[assetID] {
			toDelete = append(toDelete, assetID)
		}
	}

	if len(toDelete) > 0 {
		deleted, err := e.instanceRepo.DeleteByAssetIDs(ctx, account.TenantID, modelUID, toDelete)
		if err != nil {
			e.logger.Error("删除过期实例失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期实例", elog.Int64("deleted", deleted))
		}
	}

	// 新增或更新实例
	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertECSToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存实例失败", elog.String("asset_id", inst.InstanceID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域ECS完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int("deleted", len(toDelete)))

	return synced, nil
}

// convertECSToInstance 将 ECS 实例转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertECSToInstance(inst types.ECSInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_ecs", inst.Provider)

	// 安全组ID列表
	securityGroupIDs := make([]string, 0, len(inst.SecurityGroups))
	for _, sg := range inst.SecurityGroups {
		securityGroupIDs = append(securityGroupIDs, sg.ID)
	}

	attributes := map[string]any{
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

		// 镜像信息
		"image_id":   inst.ImageID,
		"image_name": inst.ImageName,

		// 网络信息
		"public_ip":                  inst.PublicIP,
		"private_ip":                 inst.PrivateIP,
		"vpc_id":                     inst.VPCID,
		"vpc_name":                   inst.VPCName,
		"vswitch_id":                 inst.VSwitchID,
		"vswitch_name":               inst.VSwitchName,
		"security_groups":            inst.SecurityGroups,
		"security_group_ids":         securityGroupIDs,
		"internet_max_bandwidth_in":  inst.InternetMaxBandwidthIn,
		"internet_max_bandwidth_out": inst.InternetMaxBandwidthOut,
		"network_type":               inst.NetworkType,
		"instance_network_type":      inst.InstanceNetworkType,

		// 系统盘信息
		"system_disk":          inst.SystemDisk,
		"system_disk_id":       inst.SystemDisk.DiskID,
		"system_disk_category": inst.SystemDisk.Category,
		"system_disk_size":     inst.SystemDisk.Size,

		// 数据盘信息
		"data_disks": inst.DataDisks,

		// 计费信息
		"charge_type":       inst.ChargeType,
		"creation_time":     inst.CreationTime,
		"expired_time":      inst.ExpiredTime,
		"auto_renew":        inst.AutoRenew,
		"auto_renew_period": inst.AutoRenewPeriod,

		// 项目/资源组信息
		"project_id":   inst.ProjectID,
		"project_name": inst.ProjectName,

		// 云账号信息
		"cloud_account_id":   account.ID,
		"cloud_account_name": account.Name,

		// IO优化
		"io_optimized": inst.IoOptimized,

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

// syncRegionRDS 同步单个地域的 RDS 实例
func (e *SyncAssetsExecutor) syncRegionRDS(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_rds", account.Provider)

	e.logger.Info("开始同步RDS实例",
		elog.String("region", region),
		elog.String("model_uid", modelUID),
		elog.String("tenant_id", account.TenantID))

	// 获取云端实例
	rdsAdapter := adapter.RDS()
	if rdsAdapter == nil {
		return 0, fmt.Errorf("RDS适配器不可用")
	}

	cloudInstances, err := rdsAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取RDS实例失败: %w", err)
	}

	e.logger.Info("获取到云端RDS实例",
		elog.String("region", region),
		elog.Int("count", len(cloudInstances)))

	// 获取本地实例 AssetID 列表
	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	// 构建云端 AssetID 集合
	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.InstanceID] = true
	}

	// 删除已不存在的实例
	var toDelete []string
	for _, assetID := range localAssetIDs {
		if !cloudAssetIDSet[assetID] {
			toDelete = append(toDelete, assetID)
		}
	}

	if len(toDelete) > 0 {
		deleted, err := e.instanceRepo.DeleteByAssetIDs(ctx, account.TenantID, modelUID, toDelete)
		if err != nil {
			e.logger.Error("删除过期RDS实例失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期RDS实例", elog.Int64("deleted", deleted))
		}
	}

	// 新增或更新实例
	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertRDSToInstance(inst, account)
		e.logger.Info("准备保存RDS实例",
			elog.String("asset_id", inst.InstanceID),
			elog.String("asset_name", inst.InstanceName),
			elog.String("model_uid", instance.ModelUID),
			elog.String("tenant_id", instance.TenantID),
			elog.Int64("account_id", instance.AccountID))
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存RDS实例失败", elog.String("asset_id", inst.InstanceID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域RDS完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int("deleted", len(toDelete)))

	return synced, nil
}

// syncRegionRedis 同步单个地域的 Redis 实例
func (e *SyncAssetsExecutor) syncRegionRedis(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_redis", account.Provider)

	// 获取云端实例
	redisAdapter := adapter.Redis()
	if redisAdapter == nil {
		return 0, fmt.Errorf("Redis适配器不可用")
	}

	cloudInstances, err := redisAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取Redis实例失败: %w", err)
	}

	// 获取本地实例 AssetID 列表
	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	// 构建云端 AssetID 集合
	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.InstanceID] = true
	}

	// 删除已不存在的实例
	var toDelete []string
	for _, assetID := range localAssetIDs {
		if !cloudAssetIDSet[assetID] {
			toDelete = append(toDelete, assetID)
		}
	}

	if len(toDelete) > 0 {
		deleted, err := e.instanceRepo.DeleteByAssetIDs(ctx, account.TenantID, modelUID, toDelete)
		if err != nil {
			e.logger.Error("删除过期Redis实例失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期Redis实例", elog.Int64("deleted", deleted))
		}
	}

	// 新增或更新实例
	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertRedisToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存Redis实例失败", elog.String("asset_id", inst.InstanceID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域Redis完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int("deleted", len(toDelete)))

	return synced, nil
}

// syncRegionMongoDB 同步单个地域的 MongoDB 实例
func (e *SyncAssetsExecutor) syncRegionMongoDB(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_mongodb", account.Provider)

	// 获取云端实例
	mongodbAdapter := adapter.MongoDB()
	if mongodbAdapter == nil {
		return 0, fmt.Errorf("MongoDB适配器不可用")
	}

	cloudInstances, err := mongodbAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取MongoDB实例失败: %w", err)
	}

	// 获取本地实例 AssetID 列表
	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	// 构建云端 AssetID 集合
	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.InstanceID] = true
	}

	// 删除已不存在的实例
	var toDelete []string
	for _, assetID := range localAssetIDs {
		if !cloudAssetIDSet[assetID] {
			toDelete = append(toDelete, assetID)
		}
	}

	if len(toDelete) > 0 {
		deleted, err := e.instanceRepo.DeleteByAssetIDs(ctx, account.TenantID, modelUID, toDelete)
		if err != nil {
			e.logger.Error("删除过期MongoDB实例失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期MongoDB实例", elog.Int64("deleted", deleted))
		}
	}

	// 新增或更新实例
	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertMongoDBToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存MongoDB实例失败", elog.String("asset_id", inst.InstanceID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域MongoDB完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int("deleted", len(toDelete)))

	return synced, nil
}

// convertRDSToInstance 将 RDS 实例转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertRDSToInstance(inst types.RDSInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_rds", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"status":      inst.Status,
		"region":      inst.Region,
		"zone":        inst.Zone,
		"provider":    inst.Provider,
		"description": inst.Description,

		// 数据库信息
		"engine":            inst.Engine,
		"engine_version":    inst.EngineVersion,
		"db_instance_class": inst.DBInstanceClass,

		// 配置信息
		"cpu":          inst.CPU,
		"memory":       inst.Memory,
		"storage":      inst.Storage,
		"storage_type": inst.StorageType,
		"max_iops":     inst.MaxIOPS,

		// 网络信息
		"connection_string": inst.ConnectionString,
		"port":              inst.Port,
		"vpc_id":            inst.VPCID,
		"vswitch_id":        inst.VSwitchID,
		"private_ip":        inst.PrivateIP,
		"public_ip":         inst.PublicIP,

		// 高可用信息
		"category":           inst.Category,
		"replication_mode":   inst.ReplicationMode,
		"secondary_zone":     inst.SecondaryZone,
		"read_replica_count": inst.ReadReplicaCount,

		// 计费信息
		"charge_type":   inst.ChargeType,
		"creation_time": inst.CreationTime,
		"expired_time":  inst.ExpiredTime,

		// 安全信息
		"security_ip_list": inst.SecurityIPList,
		"ssl_enabled":      inst.SSLEnabled,

		// 备份信息
		"backup_retention_period": inst.BackupRetentionPeriod,
		"preferred_backup_time":   inst.PreferredBackupTime,

		// 项目/资源组信息
		"project_id":   inst.ProjectID,
		"project_name": inst.ProjectName,

		// 云账号信息
		"cloud_account_id":   account.ID,
		"cloud_account_name": account.Name,

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

// convertRedisToInstance 将 Redis 实例转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertRedisToInstance(inst types.RedisInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_redis", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"status":      inst.Status,
		"region":      inst.Region,
		"zone":        inst.Zone,
		"provider":    inst.Provider,
		"description": inst.Description,

		// Redis信息
		"engine_version": inst.EngineVersion,
		"instance_class": inst.InstanceClass,
		"architecture":   inst.Architecture,

		// 配置信息
		"capacity":    inst.Capacity,
		"bandwidth":   inst.Bandwidth,
		"connections": inst.Connections,
		"qps":         inst.QPS,
		"shard_count": inst.ShardCount,

		// 网络信息
		"connection_domain": inst.ConnectionDomain,
		"port":              inst.Port,
		"vpc_id":            inst.VPCID,
		"vswitch_id":        inst.VSwitchID,
		"private_ip":        inst.PrivateIP,

		// 高可用信息
		"node_type":      inst.NodeType,
		"replica_count":  inst.ReplicaCount,
		"secondary_zone": inst.SecondaryZone,

		// 计费信息
		"charge_type":   inst.ChargeType,
		"creation_time": inst.CreationTime,
		"expired_time":  inst.ExpiredTime,

		// 安全信息
		"security_ip_list": inst.SecurityIPList,
		"ssl_enabled":      inst.SSLEnabled,
		"password":         inst.Password,

		// 项目/资源组信息
		"project_id":   inst.ProjectID,
		"project_name": inst.ProjectName,

		// 云账号信息
		"cloud_account_id":   account.ID,
		"cloud_account_name": account.Name,

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

// convertMongoDBToInstance 将 MongoDB 实例转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertMongoDBToInstance(inst types.MongoDBInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_mongodb", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"status":      inst.Status,
		"region":      inst.Region,
		"zone":        inst.Zone,
		"provider":    inst.Provider,
		"description": inst.Description,

		// MongoDB信息
		"engine_version":   inst.EngineVersion,
		"instance_class":   inst.InstanceClass,
		"db_instance_type": inst.DBInstanceType,

		// 配置信息
		"cpu":          inst.CPU,
		"memory":       inst.Memory,
		"storage":      inst.Storage,
		"storage_type": inst.StorageType,

		// 网络信息
		"connection_string": inst.ConnectionString,
		"port":              inst.Port,
		"vpc_id":            inst.VPCID,
		"vswitch_id":        inst.VSwitchID,

		// 副本集/分片信息
		"replica_set_name": inst.ReplicaSetName,
		"shard_count":      inst.ShardCount,
		"mongos_count":     inst.MongosCount,
		"node_count":       inst.NodeCount,

		// 计费信息
		"charge_type":   inst.ChargeType,
		"creation_time": inst.CreationTime,
		"expired_time":  inst.ExpiredTime,

		// 安全信息
		"security_ip_list": inst.SecurityIPList,
		"ssl_enabled":      inst.SSLEnabled,

		// 备份信息
		"backup_retention_period": inst.BackupRetentionPeriod,
		"preferred_backup_time":   inst.PreferredBackupTime,

		// 项目/资源组信息
		"project_id":   inst.ProjectID,
		"project_name": inst.ProjectName,

		// 云账号信息
		"cloud_account_id":   account.ID,
		"cloud_account_name": account.Name,

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
