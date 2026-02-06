// Package executor 任务执行器 - 网络资源同步
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

// syncRegionVPC 同步单个地域的 VPC
func (e *SyncAssetsExecutor) syncRegionVPC(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_vpc", account.Provider)

	vpcAdapter := adapter.VPC()
	if vpcAdapter == nil {
		return 0, fmt.Errorf("VPC适配器不可用")
	}

	cloudInstances, err := vpcAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取VPC列表失败: %w", err)
	}

	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.VPCID] = true
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
			e.logger.Error("删除过期VPC失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期VPC", elog.Int64("deleted", deleted))
		}
	}

	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertVPCToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存VPC失败", elog.String("asset_id", inst.VPCID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域VPC完成", elog.String("region", region), elog.Int("synced", synced))
	return synced, nil
}

// syncRegionEIP 同步单个地域的 EIP
func (e *SyncAssetsExecutor) syncRegionEIP(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_eip", account.Provider)

	eipAdapter := adapter.EIP()
	if eipAdapter == nil {
		return 0, fmt.Errorf("EIP适配器不可用")
	}

	cloudInstances, err := eipAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取EIP列表失败: %w", err)
	}

	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.AllocationID] = true
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
			e.logger.Error("删除过期EIP失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期EIP", elog.Int64("deleted", deleted))
		}
	}

	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertEIPToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存EIP失败", elog.String("asset_id", inst.AllocationID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域EIP完成", elog.String("region", region), elog.Int("synced", synced))
	return synced, nil
}

// convertVPCToInstance 将 VPC 转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertVPCToInstance(inst types.VPCInstance, account *domain.CloudAccount) assetdomain.Instance {
	modelUID := fmt.Sprintf("%s_vpc", account.Provider)

	attributes := map[string]any{
		"status": inst.Status, "region": inst.Region, "provider": inst.Provider,
		"description": inst.Description, "cidr_block": inst.CidrBlock,
		"secondary_cidrs": inst.SecondaryCidrs, "ipv6_cidr_block": inst.IPv6CidrBlock,
		"enable_ipv6": inst.EnableIPv6, "is_default": inst.IsDefault,
		"dhcp_options_id": inst.DhcpOptionsID, "enable_dns_support": inst.EnableDnsSupport,
		"vswitch_count": inst.VSwitchCount, "route_table_count": inst.RouteTableCount,
		"nat_gateway_count": inst.NatGatewayCount, "security_group_count": inst.SecurityGroupCount,
		"creation_time": inst.CreationTime,
		"project_id":    inst.ProjectID, "project_name": inst.ProjectName,
		"cloud_account_id": account.ID, "cloud_account_name": account.Name,
		"tags": inst.Tags,
	}

	return assetdomain.Instance{
		ModelUID: modelUID, AssetID: inst.VPCID, AssetName: inst.VPCName,
		TenantID: account.TenantID, AccountID: account.ID, Attributes: attributes,
	}
}

// convertEIPToInstance 将 EIP 转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertEIPToInstance(inst types.EIPInstance, account *domain.CloudAccount) assetdomain.Instance {
	modelUID := fmt.Sprintf("%s_eip", account.Provider)

	attributes := map[string]any{
		"status": inst.Status, "region": inst.Region, "zone": inst.Zone,
		"provider": inst.Provider, "description": inst.Description,
		"ip_address": inst.IPAddress, "private_ip_address": inst.PrivateIPAddress,
		"ip_version": inst.IPVersion, "bandwidth": inst.Bandwidth,
		"internet_charge_type": inst.InternetChargeType,
		"bandwidth_package_id": inst.BandwidthPackageID, "bandwidth_package_name": inst.BandwidthPackageName,
		"instance_id": inst.InstanceID, "instance_type": inst.InstanceType, "instance_name": inst.InstanceName,
		"vpc_id": inst.VPCID, "vswitch_id": inst.VSwitchID,
		"network_interface": inst.NetworkInterface, "isp": inst.ISP,
		"netmode": inst.Netmode, "segment_id": inst.SegmentID,
		"public_ip_pool": inst.PublicIPPool, "resource_group_id": inst.ResourceGroupID,
		"security_group_id": inst.SecurityGroupID,
		"charge_type":       inst.ChargeType, "creation_time": inst.CreationTime, "expired_time": inst.ExpiredTime,
		"project_id": inst.ProjectID, "project_name": inst.ProjectName,
		"cloud_account_id": account.ID, "cloud_account_name": account.Name,
		"tags": inst.Tags,
	}

	return assetdomain.Instance{
		ModelUID: modelUID, AssetID: inst.AllocationID, AssetName: inst.Name,
		TenantID: account.TenantID, AccountID: account.ID, Attributes: attributes,
	}
}
