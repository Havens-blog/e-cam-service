package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// ==================== EIP 同步 ====================

func (s *assetSyncService) syncEIPInstances(
	ctx context.Context,
	tenantID int64,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}
	modelUID := fmt.Sprintf("%s_eip", account.Provider)

	instances, err := adapter.EIP().ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取EIP失败: %w", err)
	}

	cloudAssetIDs := make(map[string]bool, len(instances))
	for _, inst := range instances {
		cloudAssetIDs[inst.AllocationID] = true
	}
	s.cleanupStaleInstances(ctx, tenantID, modelUID, account.ID, region, cloudAssetIDs)

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"region": inst.Region, "allocation_id": inst.AllocationID,
			"ip_address": inst.IPAddress, "status": inst.Status,
			"bandwidth": inst.Bandwidth, "internet_charge_type": inst.InternetChargeType,
			"isp": inst.ISP, "instance_id": inst.InstanceID,
			"instance_type": inst.InstanceType, "instance_name": inst.InstanceName,
			"vpc_id": inst.VPCID, "charge_type": inst.ChargeType,
			"create_time": inst.CreationTime, "expire_time": inst.ExpiredTime,
			"tags": inst.Tags, "description": inst.Description,
		}
		assetName := inst.Name
		if assetName == "" {
			assetName = inst.IPAddress
		}
		cmdbInstance := domain.Instance{
			ModelUID: fmt.Sprintf("%s_eip", account.Provider), AssetID: inst.AllocationID, AssetName: assetName,
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
