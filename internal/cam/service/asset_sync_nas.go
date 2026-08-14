package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

func (s *assetSyncService) syncNASInstances(
	ctx context.Context,
	tenantID int64,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}
	modelUID := fmt.Sprintf("%s_nas", account.Provider)

	instances, err := adapter.NAS().ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取NAS文件系统失败: %w", err)
	}

	cloudAssetIDs := make(map[string]bool, len(instances))
	for _, inst := range instances {
		cloudAssetIDs[inst.FileSystemID] = true
	}
	s.cleanupStaleInstances(ctx, tenantID, modelUID, account.ID, region, cloudAssetIDs)

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"region": inst.Region, "zone": inst.Zone, "file_system_id": inst.FileSystemID,
			"status": inst.Status, "file_system_type": inst.FileSystemType,
			"protocol_type": inst.ProtocolType, "storage_type": inst.StorageType,
			"capacity": inst.Capacity, "used_capacity": inst.UsedCapacity, "metered_size": inst.MeteredSize,
			"vpc_id": inst.VPCID, "vswitch_id": inst.VSwitchID,
			"charge_type": inst.ChargeType, "encrypt_type": inst.EncryptType,
			"kms_key_id": inst.KMSKeyID, "mount_targets": inst.MountTargets,
			"mount_target_count": len(inst.MountTargets),
			"create_time":        inst.CreationTime, "tags": inst.Tags, "description": inst.Description,
		}
		assetName := inst.Description
		if assetName == "" {
			assetName = inst.FileSystemID
		}
		cmdbInstance := domain.Instance{
			ModelUID: fmt.Sprintf("%s_nas", account.Provider), AssetID: inst.FileSystemID, AssetName: assetName,
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
