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

// syncRegionLB 同步单个地域的负载均衡实例
func (e *SyncAssetsExecutor) syncRegionLB(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_lb", account.Provider)

	lbAdapter := adapter.LB()
	if lbAdapter == nil {
		e.logger.Warn("LB适配器不可用", elog.String("provider", string(account.Provider)))
		return 0, nil
	}

	cloudInstances, err := lbAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取LB列表失败: %w", err)
	}

	items := make([]syncItem, 0, len(cloudInstances))
	for _, inst := range cloudInstances {
		items = append(items, syncItem{
			AssetID: inst.LoadBalancerID,
			ToInstance: func() (camdomain.Instance, error) {
				return e.convertLBToInstance(inst, account), nil
			},
		})
	}

	synced, deleted, err := e.diffAndUpsert(ctx, account.TenantID, modelUID, account.ID, region, items)
	if err != nil {
		return synced, err
	}

	e.logger.Info("同步地域LB完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int64("deleted", deleted))

	return synced, nil
}

// convertLBToInstance 将LB实例转换为CMDB实例
func (e *SyncAssetsExecutor) convertLBToInstance(inst types.LBInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_lb", account.Provider)

	attributes := map[string]any{
		"status":                inst.Status,
		"region":                inst.Region,
		"zone":                  inst.Zone,
		"slave_zone":            inst.SlaveZone,
		"provider":              inst.Provider,
		"description":           inst.Description,
		"load_balancer_type":    inst.LoadBalancerType,
		"address":               inst.Address,
		"vip":                   inst.Address, // VIP 地址，用于拓扑链路匹配
		"address_type":          inst.AddressType,
		"address_ip_version":    inst.AddressIPVersion,
		"vpc_id":                inst.VPCID,
		"vpc_name":              inst.VPCName,
		"vswitch_id":            inst.VSwitchID,
		"network_type":          inst.NetworkType,
		"load_balancer_spec":    inst.LoadBalancerSpec,
		"load_balancer_edition": inst.LoadBalancerEdition,
		"bandwidth":             inst.Bandwidth,
		"bandwidth_package_id":  inst.BandwidthPackageID,
		"listener_count":        inst.ListenerCount,
		"backend_server_count":  inst.BackendServerCount,
		"listeners":             inst.Listeners,
		"backend_servers":       inst.BackendServers,
		"charge_type":           inst.ChargeType,
		"internet_charge_type":  inst.InternetChargeType,
		"creation_time":         inst.CreationTime,
		"expired_time":          inst.ExpiredTime,
		"resource_group_id":     inst.ResourceGroupID,
		"project_id":            inst.ProjectID,
		"project_name":          inst.ProjectName,
		"cloud_account_id":      account.ID,
		"cloud_account_name":    account.Name,
		"tags":                  inst.Tags,
	}

	assetName := inst.LoadBalancerName
	if assetName == "" {
		assetName = inst.LoadBalancerID
	}

	return camdomain.Instance{
		ModelUID:   modelUID,
		AssetID:    inst.LoadBalancerID,
		AssetName:  assetName,
		TenantID:   account.TenantID,
		AccountID:  account.ID,
		Attributes: attributes,
	}
}
