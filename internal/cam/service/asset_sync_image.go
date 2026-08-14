package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

func (s *assetSyncService) syncImageInstances(
	ctx context.Context,
	tenantID int64,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}
	modelUID := fmt.Sprintf("%s_image", account.Provider)

	imageAdapter := adapter.Image()
	if imageAdapter == nil {
		s.logger.Warn("Image适配器不可用", elog.String("provider", string(account.Provider)))
		return result, nil
	}

	instances, err := imageAdapter.ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取镜像失败: %w", err)
	}

	cloudAssetIDs := make(map[string]bool, len(instances))
	for _, inst := range instances {
		cloudAssetIDs[inst.ImageID] = true
	}
	s.cleanupStaleInstances(ctx, tenantID, modelUID, account.ID, region, cloudAssetIDs)

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
