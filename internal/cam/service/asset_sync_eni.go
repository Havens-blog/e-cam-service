package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// syncENIInstances 同步 ENI 弹性网卡到 CMDB
func (s *assetSyncService) syncENIInstances(
	ctx context.Context,
	tenantID int64,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}
	modelUID := fmt.Sprintf("%s_eni", account.Provider)

	eniAdapter := adapter.ENI()
	if eniAdapter == nil {
		s.logger.Warn("ENI适配器不可用", elog.String("provider", string(account.Provider)))
		return result, nil
	}

	instances, err := eniAdapter.ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取ENI失败: %w", err)
	}

	cloudAssetIDs := make(map[string]bool, len(instances))
	for _, inst := range instances {
		cloudAssetIDs[inst.ENIID] = true
	}
	s.cleanupStaleInstances(ctx, tenantID, modelUID, account.ID, region, cloudAssetIDs)

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"cloud_account_name": account.Name,
			"eni_id":             inst.ENIID, "eni_name": inst.ENIName,
			"description": inst.Description, "status": inst.Status,
			"type": inst.Type, "region": inst.Region, "zone": inst.Zone,
			"vpc_id": inst.VPCID, "subnet_id": inst.SubnetID,
			"primary_private_ip":   inst.PrimaryPrivateIP,
			"private_ip_addresses": inst.PrivateIPAddresses,
			"mac_address":          inst.MacAddress,
			"ipv6_addresses":       inst.IPv6Addresses,
			"instance_id":          inst.InstanceID, "instance_name": inst.InstanceName,
			"device_index":       inst.DeviceIndex,
			"security_group_ids": inst.SecurityGroupIDs,
			"public_ip":          inst.PublicIP,
			"eip_addresses":      inst.EIPAddresses,
			"resource_group_id":  inst.ResourceGroupID,
			"project_id":         inst.ProjectID,
			"creation_time":      inst.CreationTime,
			"tags":               inst.Tags,
		}
		assetName := inst.ENIName
		if assetName == "" {
			assetName = inst.ENIID
		}
		cmdbInstance := domain.Instance{
			ModelUID: modelUID, AssetID: inst.ENIID, AssetName: assetName,
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
