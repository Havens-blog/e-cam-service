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

// syncRegionEIP 同步单个地域的 EIP
func (e *SyncAssetsExecutor) syncRegionEIP(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_eip", account.Provider)

	// 获取云端实例
	eipAdapter := adapter.EIP()
	if eipAdapter == nil {
		return 0, fmt.Errorf("EIP适配器不可用")
	}

	cloudInstances, err := eipAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取EIP列表失败: %w", err)
	}

	items := make([]syncItem, 0, len(cloudInstances))
	for _, inst := range cloudInstances {
		items = append(items, syncItem{
			AssetID: inst.AllocationID,
			ToInstance: func() (camdomain.Instance, error) {
				return e.convertEIPToInstance(inst, account), nil
			},
		})
	}

	synced, deleted, err := e.diffAndUpsert(ctx, account.TenantID, modelUID, account.ID, region, items)
	if err != nil {
		return synced, err
	}

	e.logger.Info("同步地域EIP完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int64("deleted", deleted))

	return synced, nil
}

// convertEIPToInstance 将 EIP 转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertEIPToInstance(inst types.EIPInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_eip", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"status":      inst.Status,
		"region":      inst.Region,
		"zone":        inst.Zone,
		"provider":    inst.Provider,
		"description": inst.Description,

		// IP信息
		"ip_address":         inst.IPAddress,
		"private_ip_address": inst.PrivateIPAddress,
		"ip_version":         inst.IPVersion,

		// 带宽信息
		"bandwidth":              inst.Bandwidth,
		"internet_charge_type":   inst.InternetChargeType,
		"bandwidth_package_id":   inst.BandwidthPackageID,
		"bandwidth_package_name": inst.BandwidthPackageName,

		// 绑定资源信息
		"instance_id":   inst.InstanceID,
		"instance_type": inst.InstanceType,
		"instance_name": inst.InstanceName,

		// 网络信息
		"vpc_id":            inst.VPCID,
		"vswitch_id":        inst.VSwitchID,
		"network_interface": inst.NetworkInterface,
		"isp":               inst.ISP,
		"netmode":           inst.Netmode,
		"segment_id":        inst.SegmentID,
		"public_ip_pool":    inst.PublicIPPool,
		"resource_group_id": inst.ResourceGroupID,
		"security_group_id": inst.SecurityGroupID,

		// 计费信息
		"charge_type":   inst.ChargeType,
		"creation_time": inst.CreationTime,
		"expired_time":  inst.ExpiredTime,

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
		AssetID:    inst.AllocationID,
		AssetName:  inst.Name,
		TenantID:   account.TenantID,
		AccountID:  account.ID,
		Attributes: attributes,
	}
}
