package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// ==================== MongoDB 同步 ====================

func (s *assetSyncService) syncMongoDBInstances(
	ctx context.Context,
	tenantID int64,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}
	modelUID := fmt.Sprintf("%s_mongodb", account.Provider)

	instances, err := adapter.MongoDB().ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取MongoDB实例失败: %w", err)
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
			"engine_version": inst.EngineVersion, "instance_class": inst.InstanceClass,
			"db_type": inst.DBInstanceType, "cpu": inst.CPU, "memory": inst.Memory,
			"storage": inst.Storage, "storage_type": inst.StorageType,
			"shard_count": inst.ShardCount, "node_count": inst.NodeCount,
			"connection_string": inst.ConnectionString, "port": inst.Port,
			"vpc_id": inst.VPCID, "subnet_id": inst.VSwitchID,
			"charge_type": inst.ChargeType, "create_time": inst.CreationTime,
			"expire_time": inst.ExpiredTime, "tags": inst.Tags, "description": inst.Description,
		}
		cmdbInstance := domain.Instance{
			ModelUID: fmt.Sprintf("%s_mongodb", account.Provider), AssetID: inst.InstanceID, AssetName: inst.InstanceName,
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
