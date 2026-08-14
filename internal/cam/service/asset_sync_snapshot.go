package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

func (s *assetSyncService) syncSnapshotInstances(
	ctx context.Context,
	tenantID int64,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}
	modelUID := fmt.Sprintf("%s_snapshot", account.Provider)

	snapshotAdapter := adapter.Snapshot()
	if snapshotAdapter == nil {
		s.logger.Warn("Snapshot适配器不可用", elog.String("provider", string(account.Provider)))
		return result, nil
	}

	instances, err := snapshotAdapter.ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取快照失败: %w", err)
	}

	cloudAssetIDs := make(map[string]bool, len(instances))
	for _, inst := range instances {
		cloudAssetIDs[inst.SnapshotID] = true
	}
	s.cleanupStaleInstances(ctx, tenantID, modelUID, account.ID, region, cloudAssetIDs)

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
			ModelUID: fmt.Sprintf("%s_snapshot", account.Provider), AssetID: inst.SnapshotID, AssetName: assetName,
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
