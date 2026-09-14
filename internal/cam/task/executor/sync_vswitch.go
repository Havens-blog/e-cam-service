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

// syncRegionVSwitch 同步单个地域的交换机/子网
func (e *SyncAssetsExecutor) syncRegionVSwitch(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_vswitch", account.Provider)

	vswitchAdapter := adapter.VSwitch()
	if vswitchAdapter == nil {
		return 0, fmt.Errorf("VSwitch适配器不可用")
	}

	cloudInstances, err := vswitchAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取VSwitch列表失败: %w", err)
	}

	items := make([]syncItem, 0, len(cloudInstances))
	for _, inst := range cloudInstances {
		items = append(items, syncItem{
			AssetID: inst.VSwitchID,
			ToInstance: func() (camdomain.Instance, error) {
				return e.convertVSwitchToInstance(inst, account), nil
			},
		})
	}

	synced, deleted, err := e.diffAndUpsert(ctx, account.TenantID, modelUID, account.ID, region, items)
	if err != nil {
		return synced, err
	}

	e.logger.Info("同步地域VSwitch完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int64("deleted", deleted))

	return synced, nil
}

// convertVSwitchToInstance 将 VSwitch 转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertVSwitchToInstance(inst types.VSwitchInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_vswitch", account.Provider)

	attributes := map[string]any{
		"status":      inst.Status,
		"region":      inst.Region,
		"zone":        inst.Zone,
		"provider":    inst.Provider,
		"description": inst.Description,

		"cidr_block":      inst.CidrBlock,
		"ipv6_cidr_block": inst.IPv6CidrBlock,
		"enable_ipv6":     inst.EnableIPv6,
		"is_default":      inst.IsDefault,
		"gateway_ip":      inst.GatewayIP,

		"vpc_id":   inst.VPCID,
		"vpc_name": inst.VPCName,

		"available_ip_count": inst.AvailableIPCount,
		"total_ip_count":     inst.TotalIPCount,
		"route_table_id":     inst.RouteTableID,

		"creation_time":     inst.CreationTime,
		"project_id":        inst.ProjectID,
		"project_name":      inst.ProjectName,
		"resource_group_id": inst.ResourceGroupID,

		"cloud_account_id":   account.ID,
		"cloud_account_name": account.Name,
		"tags":               inst.Tags,
	}

	return camdomain.Instance{
		ModelUID:   modelUID,
		AssetID:    inst.VSwitchID,
		AssetName:  inst.VSwitchName,
		TenantID:   account.TenantID,
		AccountID:  account.ID,
		Attributes: attributes,
	}
}
