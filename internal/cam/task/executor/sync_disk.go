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

// syncRegionDisk 同步单个地域的云盘
func (e *SyncAssetsExecutor) syncRegionDisk(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_disk", account.Provider)

	e.logger.Info("开始同步云盘",
		elog.String("region", region),
		elog.String("model_uid", modelUID),
		elog.Int64("tenant_id", account.TenantID))

	diskAdapter := adapter.Disk()
	if diskAdapter == nil {
		e.logger.Warn("Disk适配器不可用", elog.String("provider", string(account.Provider)))
		return 0, nil
	}

	cloudInstances, err := diskAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取云盘列表失败: %w", err)
	}

	e.logger.Info("获取到云端云盘",
		elog.String("region", region),
		elog.Int("count", len(cloudInstances)))

	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.DiskID] = true
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
			e.logger.Error("删除过期云盘失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期云盘", elog.Int64("deleted", deleted))
		}
	}

	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertDiskToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存云盘失败", elog.String("asset_id", inst.DiskID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域云盘完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int("deleted", len(toDelete)))

	return synced, nil
}

// convertDiskToInstance 将云盘转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertDiskToInstance(inst types.DiskInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_disk", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"status":      inst.Status,
		"region":      inst.Region,
		"zone":        inst.Zone,
		"provider":    inst.Provider,
		"description": inst.Description,

		// 磁盘类型
		"disk_type":         inst.DiskType,
		"category":          inst.Category,
		"performance_level": inst.PerformanceLevel,

		// 容量信息
		"size":       inst.Size,
		"iops":       inst.IOPS,
		"throughput": inst.Throughput,

		// 状态信息
		"portable":             inst.Portable,
		"delete_auto_snapshot": inst.DeleteAutoSnapshot,
		"delete_with_instance": inst.DeleteWithInstance,
		"enable_auto_snapshot": inst.EnableAutoSnapshot,

		// 挂载信息
		"instance_id":   inst.InstanceID,
		"instance_name": inst.InstanceName,
		"device":        inst.Device,
		"attached_time": inst.AttachedTime,
		"attachments":   inst.Attachments,
		"multi_attach":  inst.MultiAttach,

		// 加密信息
		"encrypted":  inst.Encrypted,
		"kms_key_id": inst.KMSKeyID,

		// 快照信息
		"source_snapshot_id":      inst.SourceSnapshotID,
		"auto_snapshot_policy_id": inst.AutoSnapshotPolicyID,
		"snapshot_count":          inst.SnapshotCount,

		// 镜像信息
		"image_id": inst.ImageID,

		// 计费信息
		"charge_type":   inst.ChargeType,
		"expired_time":  inst.ExpiredTime,
		"creation_time": inst.CreationTime,

		// 资源组
		"resource_group_id": inst.ResourceGroupID,

		// 云账号信息
		"cloud_account_id":   account.ID,
		"cloud_account_name": account.Name,

		// 标签
		"tags": inst.Tags,
	}

	return camdomain.Instance{
		ModelUID:   modelUID,
		AssetID:    inst.DiskID,
		AssetName:  inst.DiskName,
		TenantID:   account.TenantID,
		AccountID:  account.ID,
		Attributes: attributes,
	}
}
