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

// syncRegionImage 同步单个地域的镜像
func (e *SyncAssetsExecutor) syncRegionImage(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_image", account.Provider)

	e.logger.Info("开始同步镜像",
		elog.String("region", region),
		elog.String("model_uid", modelUID),
		elog.Int64("tenant_id", account.TenantID))

	imageAdapter := adapter.Image()
	if imageAdapter == nil {
		e.logger.Warn("Image适配器不可用", elog.String("provider", string(account.Provider)))
		return 0, nil
	}

	cloudInstances, err := imageAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取镜像列表失败: %w", err)
	}

	e.logger.Info("获取到云端镜像",
		elog.String("region", region),
		elog.Int("count", len(cloudInstances)))

	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.ImageID] = true
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
			e.logger.Error("删除过期镜像失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期镜像", elog.Int64("deleted", deleted))
		}
	}

	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertImageToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存镜像失败", elog.String("asset_id", inst.ImageID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域镜像完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int("deleted", len(toDelete)))

	return synced, nil
}

// convertImageToInstance 将镜像转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertImageToInstance(inst types.ImageInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_image", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"status":        inst.Status,
		"region":        inst.Region,
		"provider":      inst.Provider,
		"description":   inst.Description,
		"image_version": inst.ImageVersion,
		"image_family":  inst.ImageFamily,

		// 镜像类型
		"image_owner_alias": inst.ImageOwnerAlias,
		"is_self_shared":    inst.IsSelfShared,
		"is_public":         inst.IsPublic,
		"is_copied":         inst.IsCopied,

		// 操作系统信息
		"os_type":      inst.OSType,
		"os_name":      inst.OSName,
		"os_name_en":   inst.OSNameEn,
		"platform":     inst.Platform,
		"architecture": inst.Architecture,

		// 状态信息
		"progress": inst.Progress,

		// 磁盘信息
		"size":                 inst.Size,
		"disk_device_mappings": inst.DiskDeviceMappings,

		// 来源信息
		"source_instance_id": inst.SourceInstanceID,
		"source_snapshot_id": inst.SourceSnapshotID,
		"source_region":      inst.SourceRegion,

		// 使用统计
		"usage":          inst.Usage,
		"instance_count": inst.InstanceCount,

		// 资源组
		"resource_group_id": inst.ResourceGroupID,

		// 时间信息
		"creation_time": inst.CreationTime,

		// 云账号信息
		"cloud_account_id":   account.ID,
		"cloud_account_name": account.Name,

		// 功能支持
		"is_support_cloudinit":    inst.IsSupportCloudinit,
		"is_support_io_optimized": inst.IsSupportIoOptimized,
		"boot_mode":               inst.BootMode,

		// 标签
		"tags": inst.Tags,
	}

	return camdomain.Instance{
		ModelUID:   modelUID,
		AssetID:    inst.ImageID,
		AssetName:  inst.ImageName,
		TenantID:   account.TenantID,
		AccountID:  account.ID,
		Attributes: attributes,
	}
}
