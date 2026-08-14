package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// ==================== VPC 同步 ====================

func (s *assetSyncService) syncVPCInstances(
	ctx context.Context,
	tenantID int64,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}
	modelUID := fmt.Sprintf("%s_vpc", account.Provider)

	instances, err := adapter.VPC().ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取VPC失败: %w", err)
	}

	cloudAssetIDs := make(map[string]bool, len(instances))
	for _, inst := range instances {
		cloudAssetIDs[inst.VPCID] = true
	}
	s.cleanupStaleInstances(ctx, tenantID, modelUID, account.ID, region, cloudAssetIDs)

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"region": inst.Region, "vpc_id": inst.VPCID, "vpc_name": inst.VPCName,
			"status": inst.Status, "cidr_block": inst.CidrBlock,
			"secondary_cidrs": inst.SecondaryCidrs, "ipv6_cidr_block": inst.IPv6CidrBlock,
			"enable_ipv6": inst.EnableIPv6, "is_default": inst.IsDefault,
			"vswitch_count": inst.VSwitchCount, "route_table_count": inst.RouteTableCount,
			"nat_gateway_count": inst.NatGatewayCount, "security_group_count": inst.SecurityGroupCount,
			"create_time": inst.CreationTime, "tags": inst.Tags, "description": inst.Description,
		}
		cmdbInstance := domain.Instance{
			ModelUID: fmt.Sprintf("%s_vpc", account.Provider), AssetID: inst.VPCID, AssetName: inst.VPCName,
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
