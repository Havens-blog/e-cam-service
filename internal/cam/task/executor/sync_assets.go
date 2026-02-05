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

// expandAssetTypes 展开资产类型，支持 database, network, storage, middleware 等聚合类型
func expandAssetTypes(assetTypes []string) []string {
	expanded := make([]string, 0, len(assetTypes)*3)
	seen := make(map[string]bool)

	for _, t := range assetTypes {
		switch t {
		case "database", "db":
			// database 展开为 rds, redis, mongodb
			for _, dbType := range []string{"rds", "redis", "mongodb"} {
				if !seen[dbType] {
					expanded = append(expanded, dbType)
					seen[dbType] = true
				}
			}
		case "network", "net":
			// network 展开为 vpc, eip
			for _, netType := range []string{"vpc", "eip"} {
				if !seen[netType] {
					expanded = append(expanded, netType)
					seen[netType] = true
				}
			}
		case "storage":
			// storage 展开为 nas, oss
			for _, storageType := range []string{"nas", "oss"} {
				if !seen[storageType] {
					expanded = append(expanded, storageType)
					seen[storageType] = true
				}
			}
		case "middleware", "mw":
			// middleware 展开为 kafka, elasticsearch
			for _, mwType := range []string{"kafka", "elasticsearch"} {
				if !seen[mwType] {
					expanded = append(expanded, mwType)
					seen[mwType] = true
				}
			}
		default:
			if !seen[t] {
				expanded = append(expanded, t)
				seen[t] = true
			}
		}
	}
	return expanded
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

	// 展开资产类型（支持 database -> rds, redis, mongodb）
	expandedTypes := expandAssetTypes(assetTypes)

	// 获取 cloudx 适配器用于数据库资源同步
	var cloudxAdapter cloudx.CloudAdapter
	var cloudxErr error

	for _, assetType := range expandedTypes {
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
		case "vpc":
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
			synced, err := e.syncRegionVPC(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步VPC失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "eip":
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
			synced, err := e.syncRegionEIP(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步EIP失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "nas":
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
			synced, err := e.syncRegionNAS(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步NAS失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "oss":
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
			synced, err := e.syncRegionOSS(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步OSS失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "kafka":
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
			synced, err := e.syncRegionKafka(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步Kafka失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "elasticsearch", "es":
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
			synced, err := e.syncRegionElasticsearch(ctx, cloudxAdapter, account, region)
			if err != nil {
				e.logger.Error("同步Elasticsearch失败",
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

// syncRegionVPC 同步单个地域的 VPC
func (e *SyncAssetsExecutor) syncRegionVPC(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_vpc", account.Provider)

	// 获取云端实例
	vpcAdapter := adapter.VPC()
	if vpcAdapter == nil {
		return 0, fmt.Errorf("VPC适配器不可用")
	}

	cloudInstances, err := vpcAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取VPC列表失败: %w", err)
	}

	// 获取本地实例 AssetID 列表
	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	// 构建云端 AssetID 集合
	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.VPCID] = true
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
			e.logger.Error("删除过期VPC失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期VPC", elog.Int64("deleted", deleted))
		}
	}

	// 新增或更新实例
	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertVPCToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存VPC失败", elog.String("asset_id", inst.VPCID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域VPC完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int("deleted", len(toDelete)))

	return synced, nil
}

// syncRegionEIP 同步单个地域的 EIP
func (e *SyncAssetsExecutor) syncRegionEIP(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_eip", account.Provider)

	// 获取云端实例
	eipAdapter := adapter.EIP()
	if eipAdapter == nil {
		return 0, fmt.Errorf("EIP适配器不可用")
	}

	cloudInstances, err := eipAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取EIP列表失败: %w", err)
	}

	// 获取本地实例 AssetID 列表
	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	// 构建云端 AssetID 集合
	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.AllocationID] = true
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
			e.logger.Error("删除过期EIP失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期EIP", elog.Int64("deleted", deleted))
		}
	}

	// 新增或更新实例
	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertEIPToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存EIP失败", elog.String("asset_id", inst.AllocationID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域EIP完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int("deleted", len(toDelete)))

	return synced, nil
}

// convertVPCToInstance 将 VPC 转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertVPCToInstance(inst types.VPCInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_vpc", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"status":      inst.Status,
		"region":      inst.Region,
		"provider":    inst.Provider,
		"description": inst.Description,

		// 网络配置
		"cidr_block":         inst.CidrBlock,
		"secondary_cidrs":    inst.SecondaryCidrs,
		"ipv6_cidr_block":    inst.IPv6CidrBlock,
		"enable_ipv6":        inst.EnableIPv6,
		"is_default":         inst.IsDefault,
		"dhcp_options_id":    inst.DhcpOptionsID,
		"enable_dns_support": inst.EnableDnsSupport,

		// 关联资源统计
		"vswitch_count":        inst.VSwitchCount,
		"route_table_count":    inst.RouteTableCount,
		"nat_gateway_count":    inst.NatGatewayCount,
		"security_group_count": inst.SecurityGroupCount,

		// 计费信息
		"creation_time": inst.CreationTime,

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
		AssetID:    inst.VPCID,
		AssetName:  inst.VPCName,
		TenantID:   account.TenantID,
		AccountID:  account.ID,
		Attributes: attributes,
	}
}

// convertEIPToInstance 将 EIP 转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertEIPToInstance(inst types.EIPInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_eip", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"status":      inst.Status,
		"region":      inst.Region,
		"zone":        inst.Zone,
		"provider":    inst.Provider,
		"description": inst.Description,

		// IP信息
		"ip_address":         inst.IPAddress,
		"private_ip_address": inst.PrivateIPAddress,
		"ip_version":         inst.IPVersion,

		// 带宽信息
		"bandwidth":              inst.Bandwidth,
		"internet_charge_type":   inst.InternetChargeType,
		"bandwidth_package_id":   inst.BandwidthPackageID,
		"bandwidth_package_name": inst.BandwidthPackageName,

		// 绑定资源信息
		"instance_id":   inst.InstanceID,
		"instance_type": inst.InstanceType,
		"instance_name": inst.InstanceName,

		// 网络信息
		"vpc_id":            inst.VPCID,
		"vswitch_id":        inst.VSwitchID,
		"network_interface": inst.NetworkInterface,
		"isp":               inst.ISP,
		"netmode":           inst.Netmode,
		"segment_id":        inst.SegmentID,
		"public_ip_pool":    inst.PublicIPPool,
		"resource_group_id": inst.ResourceGroupID,
		"security_group_id": inst.SecurityGroupID,

		// 计费信息
		"charge_type":   inst.ChargeType,
		"creation_time": inst.CreationTime,
		"expired_time":  inst.ExpiredTime,

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
		AssetID:    inst.AllocationID,
		AssetName:  inst.Name,
		TenantID:   account.TenantID,
		AccountID:  account.ID,
		Attributes: attributes,
	}
}

// syncRegionNAS 同步单个地域的 NAS 文件系统
func (e *SyncAssetsExecutor) syncRegionNAS(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_nas", account.Provider)

	e.logger.Info("开始同步NAS文件系统",
		elog.String("region", region),
		elog.String("model_uid", modelUID),
		elog.String("tenant_id", account.TenantID))

	// 获取云端实例
	nasAdapter := adapter.NAS()
	if nasAdapter == nil {
		return 0, fmt.Errorf("NAS适配器不可用")
	}

	cloudInstances, err := nasAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取NAS文件系统失败: %w", err)
	}

	e.logger.Info("获取到云端NAS文件系统",
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
		cloudAssetIDSet[inst.FileSystemID] = true
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
			e.logger.Error("删除过期NAS文件系统失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期NAS文件系统", elog.Int64("deleted", deleted))
		}
	}

	// 新增或更新实例
	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertNASToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存NAS文件系统失败", elog.String("asset_id", inst.FileSystemID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域NAS完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int("deleted", len(toDelete)))

	return synced, nil
}

// syncRegionOSS 同步单个地域的 OSS 存储桶
// 注意：OSS 是全局服务，bucket 名称全局唯一，不按 region 隔离
func (e *SyncAssetsExecutor) syncRegionOSS(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_oss", account.Provider)

	e.logger.Info("开始同步OSS存储桶",
		elog.String("region", region),
		elog.String("model_uid", modelUID),
		elog.String("tenant_id", account.TenantID))

	// 获取云端实例
	ossAdapter := adapter.OSS()
	if ossAdapter == nil {
		return 0, fmt.Errorf("OSS适配器不可用")
	}

	// OSS 是全局服务，ListBuckets 会返回所有 bucket
	// 传入 region 参数，让适配器按需过滤（有些云厂商支持按 region 过滤）
	cloudBuckets, err := ossAdapter.ListBuckets(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取OSS存储桶失败: %w", err)
	}

	// 如果指定了 region，只同步该 region 的 bucket
	if region != "" {
		filtered := make([]types.OSSBucket, 0)
		for _, bucket := range cloudBuckets {
			if bucket.Region == region {
				filtered = append(filtered, bucket)
			}
		}
		cloudBuckets = filtered
	}

	e.logger.Info("获取到云端OSS存储桶",
		elog.String("region", region),
		elog.Int("count", len(cloudBuckets)))

	// 新增或更新实例（不删除，因为 OSS 是全局服务，其他 region 的 bucket 不应该被删除）
	synced := 0
	for _, bucket := range cloudBuckets {
		instance := e.convertOSSToInstance(bucket, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存OSS存储桶失败", elog.String("asset_id", bucket.BucketName), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域OSS完成",
		elog.String("region", region),
		elog.Int("synced", synced))

	return synced, nil
}

// convertNASToInstance 将 NAS 文件系统转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertNASToInstance(inst types.NASInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_nas", account.Provider)

	// 处理挂载点信息
	mountTargets := make([]map[string]any, 0, len(inst.MountTargets))
	for _, mt := range inst.MountTargets {
		mountTargets = append(mountTargets, map[string]any{
			"mount_target_id":     mt.MountTargetID,
			"mount_target_domain": mt.MountTargetDomain,
			"network_type":        mt.NetworkType,
			"vpc_id":              mt.VPCID,
			"vswitch_id":          mt.VSwitchID,
			"status":              mt.Status,
		})
	}

	attributes := map[string]any{
		// 基本信息
		"status":      inst.Status,
		"region":      inst.Region,
		"zone":        inst.Zone,
		"provider":    inst.Provider,
		"description": inst.Description,

		// 文件系统信息
		"file_system_type": inst.FileSystemType,
		"protocol_type":    inst.ProtocolType,
		"storage_type":     inst.StorageType,

		// 容量信息
		"capacity":      inst.Capacity,
		"used_capacity": inst.UsedCapacity,
		"metered_size":  inst.MeteredSize,

		// 网络信息
		"vpc_id":        inst.VPCID,
		"vswitch_id":    inst.VSwitchID,
		"mount_targets": mountTargets,

		// 加密信息
		"encrypt_type": inst.EncryptType,
		"kms_key_id":   inst.KMSKeyID,

		// 计费信息
		"charge_type":   inst.ChargeType,
		"creation_time": inst.CreationTime,
		"expired_time":  inst.ExpiredTime,

		// 云账号信息
		"cloud_account_id":   account.ID,
		"cloud_account_name": account.Name,

		// 标签
		"tags": inst.Tags,
	}

	assetName := inst.FileSystemName
	if assetName == "" {
		assetName = inst.Description
	}
	if assetName == "" {
		assetName = inst.FileSystemID
	}

	return camdomain.Instance{
		ModelUID:   modelUID,
		AssetID:    inst.FileSystemID,
		AssetName:  assetName,
		TenantID:   account.TenantID,
		AccountID:  account.ID,
		Attributes: attributes,
	}
}

// convertOSSToInstance 将 OSS 存储桶转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertOSSToInstance(bucket types.OSSBucket, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_oss", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"region":      bucket.Region,
		"location":    bucket.Location,
		"provider":    bucket.Provider,
		"bucket_name": bucket.BucketName,

		// 存储配置
		"storage_class": bucket.StorageClass,
		"acl":           bucket.ACL,
		"versioning":    bucket.Versioning,

		// 加密信息
		"server_side_encryption": bucket.ServerSideEncryption,
		"kms_key_id":             bucket.KMSKeyID,

		// 访问信息
		"extranet_endpoint":     bucket.ExtranetEndpoint,
		"intranet_endpoint":     bucket.IntranetEndpoint,
		"transfer_acceleration": bucket.TransferAcceleration,

		// 统计信息
		"object_count": bucket.ObjectCount,
		"storage_size": bucket.StorageSize,

		// 计费信息
		"creation_time": bucket.CreationTime,

		// 云账号信息
		"cloud_account_id":   account.ID,
		"cloud_account_name": account.Name,

		// 标签
		"tags": bucket.Tags,
	}

	return camdomain.Instance{
		ModelUID:   modelUID,
		AssetID:    bucket.BucketName,
		AssetName:  bucket.BucketName,
		TenantID:   account.TenantID,
		AccountID:  account.ID,
		Attributes: attributes,
	}
}

// syncRegionKafka 同步单个地域的 Kafka 实例
func (e *SyncAssetsExecutor) syncRegionKafka(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_kafka", account.Provider)

	e.logger.Info("开始同步Kafka实例",
		elog.String("region", region),
		elog.String("model_uid", modelUID),
		elog.String("tenant_id", account.TenantID))

	// 获取云端实例
	kafkaAdapter := adapter.Kafka()
	if kafkaAdapter == nil {
		e.logger.Warn("Kafka适配器不可用", elog.String("provider", string(account.Provider)))
		return 0, nil // 返回0而不是错误，因为某些云厂商可能未实现
	}

	cloudInstances, err := kafkaAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取Kafka实例失败: %w", err)
	}

	e.logger.Info("获取到云端Kafka实例",
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
			e.logger.Error("删除过期Kafka实例失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期Kafka实例", elog.Int64("deleted", deleted))
		}
	}

	// 新增或更新实例
	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertKafkaToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存Kafka实例失败", elog.String("asset_id", inst.InstanceID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域Kafka完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int("deleted", len(toDelete)))

	return synced, nil
}

// syncRegionElasticsearch 同步单个地域的 Elasticsearch 实例
func (e *SyncAssetsExecutor) syncRegionElasticsearch(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_elasticsearch", account.Provider)

	e.logger.Info("开始同步Elasticsearch实例",
		elog.String("region", region),
		elog.String("model_uid", modelUID),
		elog.String("tenant_id", account.TenantID))

	// 获取云端实例
	esAdapter := adapter.Elasticsearch()
	if esAdapter == nil {
		e.logger.Warn("Elasticsearch适配器不可用", elog.String("provider", string(account.Provider)))
		return 0, nil // 返回0而不是错误，因为某些云厂商可能未实现
	}

	cloudInstances, err := esAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取Elasticsearch实例失败: %w", err)
	}

	e.logger.Info("获取到云端Elasticsearch实例",
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
			e.logger.Error("删除过期Elasticsearch实例失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期Elasticsearch实例", elog.Int64("deleted", deleted))
		}
	}

	// 新增或更新实例
	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertElasticsearchToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存Elasticsearch实例失败", elog.String("asset_id", inst.InstanceID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域Elasticsearch完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int("deleted", len(toDelete)))

	return synced, nil
}

// convertKafkaToInstance 将 Kafka 实例转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertKafkaToInstance(inst types.KafkaInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_kafka", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"status":      inst.Status,
		"region":      inst.Region,
		"zone":        inst.Zone,
		"provider":    inst.Provider,
		"description": inst.Description,

		// 版本信息
		"version":      inst.Version,
		"spec_type":    inst.SpecType,
		"message_type": inst.MessageType,

		// 配置信息
		"topic_count":       inst.TopicCount,
		"topic_quota":       inst.TopicQuota,
		"partition_count":   inst.PartitionCount,
		"partition_quota":   inst.PartitionQuota,
		"consumer_groups":   inst.ConsumerGroups,
		"max_message_size":  inst.MaxMessageSize,
		"message_retention": inst.MessageRetention,
		"disk_size":         inst.DiskSize,
		"disk_used":         inst.DiskUsed,
		"disk_type":         inst.DiskType,

		// 性能配置
		"bandwidth":     inst.Bandwidth,
		"tps":           inst.TPS,
		"io_max":        inst.IOMax,
		"broker_count":  inst.BrokerCount,
		"zookeeper_num": inst.ZookeeperNum,

		// 网络信息
		"vpc_id":            inst.VPCID,
		"vswitch_id":        inst.VSwitchID,
		"security_group_id": inst.SecurityGroupID,
		"endpoint_type":     inst.EndpointType,
		"bootstrap_servers": inst.BootstrapServers,
		"ssl_endpoint":      inst.SSLEndpoint,
		"sasl_endpoint":     inst.SASLEndpoint,
		"zone_ids":          inst.ZoneIDs,

		// 安全配置
		"ssl_enabled":  inst.SSLEnabled,
		"sasl_enabled": inst.SASLEnabled,
		"acl_enabled":  inst.ACLEnabled,
		"encrypt_type": inst.EncryptType,
		"kms_key_id":   inst.KMSKeyID,

		// 计费信息
		"charge_type":   inst.ChargeType,
		"creation_time": inst.CreationTime,
		"expired_time":  inst.ExpiredTime,

		// 项目/资源组信息
		"project_id":        inst.ProjectID,
		"project_name":      inst.ProjectName,
		"resource_group_id": inst.ResourceGroupID,

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

// convertElasticsearchToInstance 将 Elasticsearch 实例转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertElasticsearchToInstance(inst types.ElasticsearchInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_elasticsearch", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"status":      inst.Status,
		"region":      inst.Region,
		"zone":        inst.Zone,
		"provider":    inst.Provider,
		"description": inst.Description,

		// 版本信息
		"version":      inst.Version,
		"engine_type":  inst.EngineType,
		"license_type": inst.LicenseType,

		// 节点配置
		"node_count":     inst.NodeCount,
		"node_spec":      inst.NodeSpec,
		"node_cpu":       inst.NodeCPU,
		"node_memory":    inst.NodeMemory,
		"node_disk_size": inst.NodeDiskSize,
		"node_disk_type": inst.NodeDiskType,
		"master_count":   inst.MasterCount,
		"master_spec":    inst.MasterSpec,
		"client_count":   inst.ClientCount,
		"client_spec":    inst.ClientSpec,
		"warm_count":     inst.WarmCount,
		"warm_spec":      inst.WarmSpec,
		"warm_disk_size": inst.WarmDiskSize,
		"kibana_count":   inst.KibanaCount,
		"kibana_spec":    inst.KibanaSpec,

		// 存储信息
		"total_disk_size": inst.TotalDiskSize,
		"used_disk_size":  inst.UsedDiskSize,
		"index_count":     inst.IndexCount,
		"doc_count":       inst.DocCount,
		"shard_count":     inst.ShardCount,

		// 网络信息
		"vpc_id":               inst.VPCID,
		"vswitch_id":           inst.VSwitchID,
		"security_group_id":    inst.SecurityGroupID,
		"private_endpoint":     inst.PrivateEndpoint,
		"public_endpoint":      inst.PublicEndpoint,
		"kibana_endpoint":      inst.KibanaEndpoint,
		"kibana_private_url":   inst.KibanaPrivateURL,
		"kibana_public_url":    inst.KibanaPublicURL,
		"port":                 inst.Port,
		"enable_public_access": inst.EnablePublicAccess,

		// 安全配置
		"ssl_enabled":       inst.SSLEnabled,
		"auth_enabled":      inst.AuthEnabled,
		"encrypt_type":      inst.EncryptType,
		"kms_key_id":        inst.KMSKeyID,
		"whitelist_enabled": inst.WhitelistEnabled,
		"whitelist_ips":     inst.WhitelistIPs,

		// 高可用配置
		"zone_count":        inst.ZoneCount,
		"zone_ids":          inst.ZoneIDs,
		"enable_ha":         inst.EnableHA,
		"enable_auto_scale": inst.EnableAutoScale,

		// 计费信息
		"charge_type":   inst.ChargeType,
		"creation_time": inst.CreationTime,
		"expired_time":  inst.ExpiredTime,
		"update_time":   inst.UpdateTime,

		// 项目/资源组信息
		"project_id":        inst.ProjectID,
		"project_name":      inst.ProjectName,
		"resource_group_id": inst.ResourceGroupID,

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
