package executor

import (
	"context"
	"fmt"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

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
		elog.Int64("tenant_id", account.TenantID))

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
