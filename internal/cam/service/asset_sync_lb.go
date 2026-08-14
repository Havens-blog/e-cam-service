package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// syncLBInstances 同步负载均衡实例
func (s *assetSyncService) syncLBInstances(
	ctx context.Context,
	tenantID int64,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}

	modelUID := fmt.Sprintf("%s_lb", account.Provider)

	lbAdapter := adapter.LB()
	if lbAdapter == nil {
		s.logger.Warn("LB适配器不可用", elog.String("provider", string(account.Provider)))
		return result, nil
	}

	instances, err := lbAdapter.ListInstances(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取LB失败: %w", err)
	}

	cloudAssetIDs := make(map[string]bool, len(instances))
	for _, inst := range instances {
		cloudAssetIDs[inst.LoadBalancerID] = true
	}
	s.cleanupStaleInstances(ctx, tenantID, modelUID, account.ID, region, cloudAssetIDs)

	for _, inst := range instances {
		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"region": inst.Region, "load_balancer_id": inst.LoadBalancerID,
			"load_balancer_name": inst.LoadBalancerName, "load_balancer_type": inst.LoadBalancerType,
			"status": inst.Status, "address": inst.Address,
			"vip":          inst.Address, // VIP 地址，用于拓扑链路匹配
			"address_type": inst.AddressType, "address_ip_version": inst.AddressIPVersion,
			"vpc_id": inst.VPCID, "vswitch_id": inst.VSwitchID,
			"network_type": inst.NetworkType, "load_balancer_spec": inst.LoadBalancerSpec,
			"bandwidth": inst.Bandwidth, "internet_charge_type": inst.InternetChargeType,
			"charge_type": inst.ChargeType, "zone": inst.Zone,
			"slave_zone": inst.SlaveZone, "listener_count": inst.ListenerCount,
			"backend_server_count": inst.BackendServerCount,
			"creation_time":        inst.CreationTime, "expired_time": inst.ExpiredTime,
			"resource_group_id": inst.ResourceGroupID,
			"tags":              inst.Tags, "description": inst.Description,
		}
		assetName := inst.LoadBalancerName
		if assetName == "" {
			assetName = inst.LoadBalancerID
		}
		cmdbInstance := domain.Instance{
			ModelUID: fmt.Sprintf("%s_lb", account.Provider), AssetID: inst.LoadBalancerID, AssetName: assetName,
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
