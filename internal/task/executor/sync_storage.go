// Package executor 任务执行器 - 存储资源同步
package executor

import (
	"context"
	"fmt"

	assetdomain "github.com/Havens-blog/e-cam-service/internal/asset/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// syncRegionNAS 同步单个地域的 NAS 文件系统
func (e *SyncAssetsExecutor) syncRegionNAS(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_nas", account.Provider)

	nasAdapter := adapter.NAS()
	if nasAdapter == nil {
		return 0, fmt.Errorf("NAS适配器不可用")
	}

	cloudInstances, err := nasAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取NAS文件系统失败: %w", err)
	}

	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.FileSystemID] = true
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
			e.logger.Error("删除过期NAS文件系统失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期NAS文件系统", elog.Int64("deleted", deleted))
		}
	}

	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertNASToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存NAS文件系统失败", elog.String("asset_id", inst.FileSystemID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域NAS完成", elog.String("region", region), elog.Int("synced", synced))
	return synced, nil
}

// syncRegionOSS 同步单个地域的 OSS 存储桶
func (e *SyncAssetsExecutor) syncRegionOSS(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	ossAdapter := adapter.OSS()
	if ossAdapter == nil {
		return 0, fmt.Errorf("OSS适配器不可用")
	}

	cloudBuckets, err := ossAdapter.ListBuckets(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取OSS存储桶失败: %w", err)
	}

	// 如果指定了 region，只同步该 region 的 bucket
	if region != "" {
		filtered := make([]types.OSSBucket, 0)
		for _, bucket := range cloudBuckets {
			if bucket.Region == region {
				filtered = append(filtered, bucket)
			}
		}
		cloudBuckets = filtered
	}

	// OSS 是全局服务，不删除其他 region 的 bucket
	synced := 0
	for _, bucket := range cloudBuckets {
		instance := e.convertOSSToInstance(bucket, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存OSS存储桶失败", elog.String("asset_id", bucket.BucketName), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域OSS完成", elog.String("region", region), elog.Int("synced", synced))
	return synced, nil
}

// convertNASToInstance 将 NAS 文件系统转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertNASToInstance(inst types.NASInstance, account *domain.CloudAccount) assetdomain.Instance {
	modelUID := fmt.Sprintf("%s_nas", account.Provider)

	mountTargets := make([]map[string]any, 0, len(inst.MountTargets))
	for _, mt := range inst.MountTargets {
		mountTargets = append(mountTargets, map[string]any{
			"mount_target_id":     mt.MountTargetID,
			"mount_target_domain": mt.MountTargetDomain,
			"network_type":        mt.NetworkType,
			"vpc_id":              mt.VPCID,
			"vswitch_id":          mt.VSwitchID,
			"status":              mt.Status,
		})
	}

	attributes := map[string]any{
		"status": inst.Status, "region": inst.Region, "zone": inst.Zone,
		"provider": inst.Provider, "description": inst.Description,
		"file_system_type": inst.FileSystemType, "protocol_type": inst.ProtocolType,
		"storage_type": inst.StorageType, "capacity": inst.Capacity,
		"used_capacity": inst.UsedCapacity, "metered_size": inst.MeteredSize,
		"vpc_id": inst.VPCID, "vswitch_id": inst.VSwitchID,
		"mount_targets": mountTargets, "encrypt_type": inst.EncryptType, "kms_key_id": inst.KMSKeyID,
		"charge_type": inst.ChargeType, "creation_time": inst.CreationTime, "expired_time": inst.ExpiredTime,
		"cloud_account_id": account.ID, "cloud_account_name": account.Name,
		"tags": inst.Tags,
	}

	assetName := inst.FileSystemName
	if assetName == "" {
		assetName = inst.Description
	}
	if assetName == "" {
		assetName = inst.FileSystemID
	}

	return assetdomain.Instance{
		ModelUID: modelUID, AssetID: inst.FileSystemID, AssetName: assetName,
		TenantID: account.TenantID, AccountID: account.ID, Attributes: attributes,
	}
}

// convertOSSToInstance 将 OSS 存储桶转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertOSSToInstance(bucket types.OSSBucket, account *domain.CloudAccount) assetdomain.Instance {
	modelUID := fmt.Sprintf("%s_oss", account.Provider)

	attributes := map[string]any{
		"region": bucket.Region, "location": bucket.Location,
		"provider": bucket.Provider, "bucket_name": bucket.BucketName,
		"storage_class": bucket.StorageClass, "acl": bucket.ACL, "versioning": bucket.Versioning,
		"server_side_encryption": bucket.ServerSideEncryption, "kms_key_id": bucket.KMSKeyID,
		"extranet_endpoint": bucket.ExtranetEndpoint, "intranet_endpoint": bucket.IntranetEndpoint,
		"transfer_acceleration": bucket.TransferAcceleration,
		"object_count":          bucket.ObjectCount, "storage_size": bucket.StorageSize,
		"creation_time":    bucket.CreationTime,
		"cloud_account_id": account.ID, "cloud_account_name": account.Name,
		"tags": bucket.Tags,
	}

	return assetdomain.Instance{
		ModelUID: modelUID, AssetID: bucket.BucketName, AssetName: bucket.BucketName,
		TenantID: account.TenantID, AccountID: account.ID, Attributes: attributes,
	}
}
