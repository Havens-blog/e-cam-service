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

// syncRegionENI 同步单个地域的弹性网卡
func (e *SyncAssetsExecutor) syncRegionENI(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_eni", account.Provider)

	eniAdapter := adapter.ENI()
	if eniAdapter == nil {
		return 0, fmt.Errorf("ENI适配器不可用")
	}

	cloudInstances, err := eniAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取弹性网卡列表失败: %w", err)
	}

	// 获取本地实例 AssetID 列表
	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	// 构建云端 AssetID 集合
	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.ENIID] = true
	}

	// 删除已不存在的实例
	var toDelete []string
	for _, assetID := range localAssetIDs {
		if !cloudAssetIDSet[assetID] {
			toDelete = append(toDelete, assetID)
		}
	}

	if len(toDelete) > 0 {
		deleted, err := e.instanceRepo.DeleteByAssetIDs(ctx, account.TenantID, modelUID, toDelete)
		if err != nil {
			e.logger.Error("删除过期ENI失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期ENI", elog.Int64("deleted", deleted))
		}
	}

	// 新增或更新实例
	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertENIToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存ENI失败", elog.String("asset_id", inst.ENIID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域ENI完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int("deleted", len(toDelete)))

	return synced, nil
}

// convertENIToInstance 将 ENI 转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertENIToInstance(inst types.ENIInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_eni", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"status":      inst.Status,
		"type":        inst.Type,
		"region":      inst.Region,
		"zone":        inst.Zone,
		"provider":    inst.Provider,
		"description": inst.Description,

		// 网络信息
		"vpc_id":               inst.VPCID,
		"subnet_id":            inst.SubnetID,
		"primary_private_ip":   inst.PrimaryPrivateIP,
		"private_ip_addresses": inst.PrivateIPAddresses,
		"mac_address":          inst.MacAddress,
		"ipv6_addresses":       inst.IPv6Addresses,

		// 绑定信息
		"instance_id":   inst.InstanceID,
		"instance_name": inst.InstanceName,
		"device_index":  inst.DeviceIndex,

		// 安全组
		"security_group_ids": inst.SecurityGroupIDs,

		// 公网信息
		"public_ip":     inst.PublicIP,
		"eip_addresses": inst.EIPAddresses,

		// 资源信息
		"resource_group_id": inst.ResourceGroupID,
		"project_id":        inst.ProjectID,

		// 计费信息
		"creation_time": inst.CreationTime,

		// 云账号信息
		"cloud_account_id":   account.ID,
		"cloud_account_name": account.Name,

		// 标签
		"tags": inst.Tags,
	}

	return camdomain.Instance{
		ModelUID:   modelUID,
		AssetID:    inst.ENIID,
		AssetName:  inst.ENIName,
		TenantID:   account.TenantID,
		AccountID:  account.ID,
		Attributes: attributes,
	}
}
