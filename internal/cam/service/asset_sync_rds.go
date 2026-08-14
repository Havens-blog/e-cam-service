package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// ==================== RDS 同步 ====================

func (s *assetSyncService) syncRDSInstances(
	ctx context.Context,
	tenantID int64,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}
	modelUID := fmt.Sprintf("%s_rds", account.Provider)

	instances, err := adapter.RDS().ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取RDS实例失败: %w", err)
	}

	cloudAssetIDs := make(map[string]bool, len(instances))
	for _, inst := range instances {
		cloudAssetIDs[inst.InstanceID] = true
	}
	s.cleanupStaleInstances(ctx, tenantID, modelUID, account.ID, region, cloudAssetIDs)

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"region": inst.Region, "zone": inst.Zone, "instance_id": inst.InstanceID,
			"instance_name": inst.InstanceName, "status": inst.Status,
			"engine": inst.Engine, "engine_version": inst.EngineVersion,
			"instance_class": inst.DBInstanceClass, "cpu": inst.CPU, "memory": inst.Memory,
			"storage": inst.Storage, "storage_type": inst.StorageType,
			"connection_string": inst.ConnectionString, "port": inst.Port,
			"vpc_id": inst.VPCID, "subnet_id": inst.VSwitchID,
			"private_ip": inst.PrivateIP, "public_ip": inst.PublicIP,
			"category": inst.Category, "charge_type": inst.ChargeType,
			"create_time": inst.CreationTime, "expire_time": inst.ExpiredTime,
			"tags": inst.Tags, "description": inst.Description,
		}
		cmdbInstance := domain.Instance{
			ModelUID: fmt.Sprintf("%s_rds", account.Provider), AssetID: inst.InstanceID, AssetName: inst.InstanceName,
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
