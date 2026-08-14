package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// syncECSInstances 同步 ECS 实例到 CMDB
func (s *assetSyncService) syncECSInstances(
	ctx context.Context,
	tenantID int64,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{
		ByAssetType: make(map[string]int),
		ByRegion:    make(map[string]int),
	}

	modelUID := fmt.Sprintf("%s_ecs", account.Provider)

	instances, err := adapter.ECS().ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取ECS实例失败: %w", err)
	}

	// 构建云端 AssetID 集合并清理过期实例
	cloudAssetIDs := make(map[string]bool, len(instances))
	for _, inst := range instances {
		cloudAssetIDs[inst.InstanceID] = true
	}
	s.cleanupStaleInstances(ctx, tenantID, modelUID, account.ID, region, cloudAssetIDs)

	if len(instances) == 0 {
		return result, nil
	}

	s.logger.Debug("获取到ECS实例",
		elog.String("region", region),
		elog.Int("count", len(instances)))

	for _, inst := range instances {
		cmdbInstance := s.convertECSToCMDBInstance(tenantID, account, inst)

		err := s.trackAndUpsert(ctx, cmdbInstance)
		if err != nil {
			s.logger.Error("保存ECS实例失败",
				elog.String("asset_id", inst.InstanceID),
				elog.FieldErr(err))
			result.Failed++
			continue
		}
		result.TotalSynced++
	}

	return result, nil
}

// convertECSToCMDBInstance 将 ECS 实例转换为 CMDB Instance
func (s *assetSyncService) convertECSToCMDBInstance(
	tenantID int64,
	account *shareddomain.CloudAccount,
	inst types.ECSInstance,
) domain.Instance {
	var securityGroupIDs []string
	for _, sg := range inst.SecurityGroups {
		securityGroupIDs = append(securityGroupIDs, sg.ID)
	}

	attrs := map[string]interface{}{
		"provider":             string(account.Provider),
		"cloud_account_id":     account.ID,
		"region":               inst.Region,
		"zone":                 inst.Zone,
		"instance_id":          inst.InstanceID,
		"instance_name":        inst.InstanceName,
		"status":               inst.Status,
		"create_time":          inst.CreationTime,
		"expire_time":          inst.ExpiredTime,
		"instance_type":        inst.InstanceType,
		"cpu":                  inst.CPU,
		"memory":               inst.Memory,
		"os_type":              inst.OSType,
		"os_name":              inst.OSName,
		"image_id":             inst.ImageID,
		"private_ip":           inst.PrivateIP,
		"public_ip":            inst.PublicIP,
		"vpc_id":               inst.VPCID,
		"subnet_id":            inst.VSwitchID,
		"security_groups":      securityGroupIDs,
		"system_disk_size":     inst.SystemDisk.Size,
		"system_disk_category": inst.SystemDisk.Category,
		"charge_type":          inst.ChargeType,
		"hostname":             inst.HostName,
		"key_pair_name":        inst.KeyPairName,
		"description":          inst.Description,
		"tags":                 inst.Tags,
	}

	return domain.Instance{
		ModelUID:   fmt.Sprintf("%s_ecs", account.Provider),
		AssetID:    inst.InstanceID,
		AssetName:  inst.InstanceName,
		TenantID:   tenantID,
		AccountID:  account.ID,
		Attributes: attrs,
	}
}
