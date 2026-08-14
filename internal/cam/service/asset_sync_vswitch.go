package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// syncVSwitchInstances 同步交换机/子网实例
func (s *assetSyncService) syncVSwitchInstances(
	ctx context.Context,
	tenantID int64,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}
	modelUID := fmt.Sprintf("%s_vswitch", account.Provider)

	instances, err := adapter.VSwitch().ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取VSwitch失败: %w", err)
	}

	cloudAssetIDs := make(map[string]bool, len(instances))
	for _, inst := range instances {
		cloudAssetIDs[inst.VSwitchID] = true
	}
	s.cleanupStaleInstances(ctx, tenantID, modelUID, account.ID, region, cloudAssetIDs)

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider":           string(account.Provider),
			"cloud_account_id":   account.ID,
			"region":             inst.Region,
			"zone":               inst.Zone,
			"vswitch_id":         inst.VSwitchID,
			"status":             inst.Status,
			"cidr_block":         inst.CidrBlock,
			"ipv6_cidr_block":    inst.IPv6CidrBlock,
			"enable_ipv6":        inst.EnableIPv6,
			"is_default":         inst.IsDefault,
			"gateway_ip":         inst.GatewayIP,
			"vpc_id":             inst.VPCID,
			"vpc_name":           inst.VPCName,
			"available_ip_count": inst.AvailableIPCount,
			"total_ip_count":     inst.TotalIPCount,
			"route_table_id":     inst.RouteTableID,
			"create_time":        inst.CreationTime,
			"resource_group_id":  inst.ResourceGroupID,
			"tags":               inst.Tags,
			"description":        inst.Description,
		}
		assetName := inst.VSwitchName
		if assetName == "" {
			assetName = inst.VSwitchID
		}
		cmdbInstance := domain.Instance{
			ModelUID:   fmt.Sprintf("%s_vswitch", account.Provider),
			AssetID:    inst.VSwitchID,
			AssetName:  assetName,
			TenantID:   tenantID,
			AccountID:  account.ID,
			Attributes: attrs,
		}
		if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
			result.Failed++
			continue
		}
		result.TotalSynced++
	}
	return result, nil
}
