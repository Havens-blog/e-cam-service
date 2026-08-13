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

// syncRegionSnapshot 同步单个地域的快照
func (e *SyncAssetsExecutor) syncRegionSnapshot(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_snapshot", account.Provider)

	e.logger.Info("开始同步快照",
		elog.String("region", region),
		elog.String("model_uid", modelUID),
		elog.Int64("tenant_id", account.TenantID))

	snapshotAdapter := adapter.Snapshot()
	if snapshotAdapter == nil {
		e.logger.Warn("Snapshot适配器不可用", elog.String("provider", string(account.Provider)))
		return 0, nil
	}

	cloudInstances, err := snapshotAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取快照列表失败: %w", err)
	}

	e.logger.Info("获取到云端快照",
		elog.String("region", region),
		elog.Int("count", len(cloudInstances)))

	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.SnapshotID] = true
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
			e.logger.Error("删除过期快照失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期快照", elog.Int64("deleted", deleted))
		}
	}

	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertSnapshotToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存快照失败", elog.String("asset_id", inst.SnapshotID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域快照完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int("deleted", len(toDelete)))

	return synced, nil
}

// convertSnapshotToInstance 将快照转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertSnapshotToInstance(inst types.SnapshotInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_snapshot", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"status":      inst.Status,
		"region":      inst.Region,
		"provider":    inst.Provider,
		"description": inst.Description,

		// 快照类型
		"snapshot_type":  inst.SnapshotType,
		"category":       inst.Category,
		"instant_access": inst.InstantAccess,

		// 状态信息
		"progress": inst.Progress,

		// 容量信息
		"source_disk_size": inst.SourceDiskSize,
		"snapshot_size":    inst.SnapshotSize,

		// 来源信息
		"source_disk_id":       inst.SourceDiskID,
		"source_disk_type":     inst.SourceDiskType,
		"source_disk_category": inst.SourceDiskCategory,
		"source_instance_id":   inst.SourceInstanceID,
		"source_instance_name": inst.SourceInstanceName,

		// 加密信息
		"encrypted":  inst.Encrypted,
		"kms_key_id": inst.KMSKeyID,

		// 使用信息
		"usage":            inst.Usage,
		"used_image_count": inst.UsedImageCount,
		"used_disk_count":  inst.UsedDiskCount,

		// 保留信息
		"retention_days": inst.RetentionDays,

		// 资源组
		"resource_group_id": inst.ResourceGroupID,

		// 时间信息
		"creation_time":      inst.CreationTime,
		"last_modified_time": inst.LastModifiedTime,

		// 云账号信息
		"cloud_account_id":   account.ID,
		"cloud_account_name": account.Name,

		// 标签
		"tags": inst.Tags,
	}

	return camdomain.Instance{
		ModelUID:   modelUID,
		AssetID:    inst.SnapshotID,
		AssetName:  inst.SnapshotName,
		TenantID:   account.TenantID,
		AccountID:  account.ID,
		Attributes: attributes,
	}
}
