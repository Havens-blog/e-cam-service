// Package executor 任务执行器
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	accountrepo "github.com/Havens-blog/e-cam-service/internal/account/repository"
	assetdomain "github.com/Havens-blog/e-cam-service/internal/asset/domain"
	assetrepo "github.com/Havens-blog/e-cam-service/internal/asset/repository"
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
	accountRepo    accountrepo.CloudAccountRepository
	instanceRepo   assetrepo.InstanceRepository
	adapterFactory *asset.AdapterFactory
	cloudxFactory  *cloudx.AdapterFactory
	taskRepo       taskx.TaskRepository
	logger         *elog.Component
}

// NewSyncAssetsExecutor 创建同步资产任务执行器
func NewSyncAssetsExecutor(
	accountRepo accountrepo.CloudAccountRepository,
	instanceRepo assetrepo.InstanceRepository,
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
			for _, dbType := range []string{"rds", "redis", "mongodb"} {
				if !seen[dbType] {
					expanded = append(expanded, dbType)
					seen[dbType] = true
				}
			}
		case "network", "net":
			for _, netType := range []string{"vpc", "eip"} {
				if !seen[netType] {
					expanded = append(expanded, netType)
					seen[netType] = true
				}
			}
		case "storage":
			for _, storageType := range []string{"nas", "oss"} {
				if !seen[storageType] {
					expanded = append(expanded, storageType)
					seen[storageType] = true
				}
			}
		case "middleware", "mw":
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

	// 展开资产类型
	expandedTypes := expandAssetTypes(assetTypes)

	// 获取 cloudx 适配器用于数据库资源同步
	var cloudxAdapter cloudx.CloudAdapter
	var cloudxErr error

	for _, assetType := range expandedTypes {
		switch assetType {
		case "ecs":
			synced, err := e.syncRegionECS(ctx, adapter, account, region)
			if err != nil {
				e.logger.Error("同步ECS失败", elog.String("region", region), elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "rds":
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
				e.logger.Error("同步RDS失败", elog.String("region", region), elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "redis":
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
				e.logger.Error("同步Redis失败", elog.String("region", region), elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "mongodb":
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
				e.logger.Error("同步MongoDB失败", elog.String("region", region), elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "vpc":
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
				e.logger.Error("同步VPC失败", elog.String("region", region), elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "eip":
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
				e.logger.Error("同步EIP失败", elog.String("region", region), elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "nas":
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
				e.logger.Error("同步NAS失败", elog.String("region", region), elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "oss":
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
				e.logger.Error("同步OSS失败", elog.String("region", region), elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "kafka":
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
				e.logger.Error("同步Kafka失败", elog.String("region", region), elog.FieldErr(err))
				continue
			}
			totalSynced += synced
		case "elasticsearch", "es":
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
				e.logger.Error("同步Elasticsearch失败", elog.String("region", region), elog.FieldErr(err))
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

	cloudInstances, err := adapter.GetECSInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取ECS实例失败: %w", err)
	}

	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.InstanceID] = true
	}

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
func (e *SyncAssetsExecutor) convertECSToInstance(inst types.ECSInstance, account *domain.CloudAccount) assetdomain.Instance {
	modelUID := fmt.Sprintf("%s_ecs", inst.Provider)

	securityGroupIDs := make([]string, 0, len(inst.SecurityGroups))
	for _, sg := range inst.SecurityGroups {
		securityGroupIDs = append(securityGroupIDs, sg.ID)
	}

	attributes := map[string]any{
		"status": inst.Status, "region": inst.Region, "zone": inst.Zone,
		"provider": inst.Provider, "description": inst.Description,
		"host_name": inst.HostName, "key_pair_name": inst.KeyPairName,
		"instance_type": inst.InstanceType, "instance_type_family": inst.InstanceTypeFamily,
		"cpu": inst.CPU, "memory": inst.Memory, "os_type": inst.OSType, "os_name": inst.OSName,
		"image_id": inst.ImageID, "image_name": inst.ImageName,
		"public_ip": inst.PublicIP, "private_ip": inst.PrivateIP,
		"vpc_id": inst.VPCID, "vpc_name": inst.VPCName,
		"vswitch_id": inst.VSwitchID, "vswitch_name": inst.VSwitchName,
		"security_groups": inst.SecurityGroups, "security_group_ids": securityGroupIDs,
		"internet_max_bandwidth_in":  inst.InternetMaxBandwidthIn,
		"internet_max_bandwidth_out": inst.InternetMaxBandwidthOut,
		"network_type":               inst.NetworkType, "instance_network_type": inst.InstanceNetworkType,
		"system_disk": inst.SystemDisk, "system_disk_id": inst.SystemDisk.DiskID,
		"system_disk_category": inst.SystemDisk.Category, "system_disk_size": inst.SystemDisk.Size,
		"data_disks": inst.DataDisks, "charge_type": inst.ChargeType,
		"creation_time": inst.CreationTime, "expired_time": inst.ExpiredTime,
		"auto_renew": inst.AutoRenew, "auto_renew_period": inst.AutoRenewPeriod,
		"project_id": inst.ProjectID, "project_name": inst.ProjectName,
		"cloud_account_id": account.ID, "cloud_account_name": account.Name,
		"io_optimized": inst.IoOptimized, "tags": inst.Tags,
	}

	return assetdomain.Instance{
		ModelUID: modelUID, AssetID: inst.InstanceID, AssetName: inst.InstanceName,
		TenantID: account.TenantID, AccountID: account.ID, Attributes: attributes,
	}
}
