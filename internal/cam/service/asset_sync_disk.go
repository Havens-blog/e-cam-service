package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

func (s *assetSyncService) syncDiskInstances(
	ctx context.Context,
	tenantID int64,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}
	modelUID := fmt.Sprintf("%s_disk", account.Provider)

	diskAdapter := adapter.Disk()
	if diskAdapter == nil {
		s.logger.Warn("Disk适配器不可用", elog.String("provider", string(account.Provider)))
		return result, nil
	}

	instances, err := diskAdapter.ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取云盘失败: %w", err)
	}

	cloudAssetIDs := make(map[string]bool, len(instances))
	for _, inst := range instances {
		cloudAssetIDs[inst.DiskID] = true
	}
	s.cleanupStaleInstances(ctx, tenantID, modelUID, account.ID, region, cloudAssetIDs)

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
			ModelUID: fmt.Sprintf("%s_disk", account.Provider), AssetID: inst.DiskID, AssetName: assetName,
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
