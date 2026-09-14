package executor

import (
	"context"
	"fmt"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
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

	// 获取云端实例
	vpcAdapter := adapter.VPC()
	if vpcAdapter == nil {
		return 0, fmt.Errorf("VPC适配器不可用")
	}

	cloudInstances, err := vpcAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取VPC列表失败: %w", err)
	}

	items := make([]syncItem, 0, len(cloudInstances))
	for _, inst := range cloudInstances {
		items = append(items, syncItem{
			AssetID: inst.VPCID,
			ToInstance: func() (camdomain.Instance, error) {
				return e.convertVPCToInstance(inst, account), nil
			},
		})
	}

	synced, deleted, err := e.diffAndUpsert(ctx, account.TenantID, modelUID, account.ID, region, items)
	if err != nil {
		return synced, err
	}

	e.logger.Info("同步地域VPC完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int64("deleted", deleted))

	return synced, nil
}

// convertVPCToInstance 将 VPC 转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertVPCToInstance(inst types.VPCInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_vpc", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"status":      inst.Status,
		"region":      inst.Region,
		"provider":    inst.Provider,
		"description": inst.Description,

		// 网络配置
		"cidr_block":         inst.CidrBlock,
		"secondary_cidrs":    inst.SecondaryCidrs,
		"ipv6_cidr_block":    inst.IPv6CidrBlock,
		"enable_ipv6":        inst.EnableIPv6,
		"is_default":         inst.IsDefault,
		"dhcp_options_id":    inst.DhcpOptionsID,
		"enable_dns_support": inst.EnableDnsSupport,

		// 关联资源统计
		"vswitch_count":        inst.VSwitchCount,
		"route_table_count":    inst.RouteTableCount,
		"nat_gateway_count":    inst.NatGatewayCount,
		"security_group_count": inst.SecurityGroupCount,

		// 计费信息
		"creation_time": inst.CreationTime,

		// 项目/资源组信息
		"project_id":   inst.ProjectID,
		"project_name": inst.ProjectName,

		// 云账号信息
		"cloud_account_id":   account.ID,
		"cloud_account_name": account.Name,

		// 标签
		"tags": inst.Tags,
	}

	return camdomain.Instance{
		ModelUID:   modelUID,
		AssetID:    inst.VPCID,
		AssetName:  inst.VPCName,
		TenantID:   account.TenantID,
		AccountID:  account.ID,
		Attributes: attributes,
	}
}
