package executor

import (
	"context"
	"fmt"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/asset"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// syncRegionECS 同步单个地域的 ECS 实例
func (e *SyncAssetsExecutor) syncRegionECS(
	ctx context.Context,
	adapter asset.CloudAssetAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_ecs", account.Provider)

	// 获取云端实例
	cloudInstances, err := adapter.GetECSInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取ECS实例失败: %w", err)
	}

	items := make([]syncItem, 0, len(cloudInstances))
	for _, inst := range cloudInstances {
		items = append(items, syncItem{
			AssetID: inst.InstanceID,
			ToInstance: func() (camdomain.Instance, error) {
				return e.convertECSToInstance(inst, account), nil
			},
		})
	}

	synced, deleted, err := e.diffAndUpsert(ctx, account.TenantID, modelUID, account.ID, region, items)
	if err != nil {
		return synced, err
	}

	e.logger.Info("同步地域ECS完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int64("deleted", deleted))

	return synced, nil
}

// convertECSToInstance 将 ECS 实例转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertECSToInstance(inst types.ECSInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_ecs", inst.Provider)

	// 安全组ID列表
	securityGroupIDs := make([]string, 0, len(inst.SecurityGroups))
	for _, sg := range inst.SecurityGroups {
		securityGroupIDs = append(securityGroupIDs, sg.ID)
	}

	attributes := map[string]any{
		// 基本信息
		"status":        inst.Status,
		"region":        inst.Region,
		"zone":          inst.Zone,
		"provider":      inst.Provider,
		"description":   inst.Description,
		"host_name":     inst.HostName,
		"key_pair_name": inst.KeyPairName,

		// 配置信息
		"instance_type":        inst.InstanceType,
		"instance_type_family": inst.InstanceTypeFamily,
		"cpu":                  inst.CPU,
		"memory":               inst.Memory,
		"os_type":              inst.OSType,
		"os_name":              inst.OSName,

		// 镜像信息
		"image_id":   inst.ImageID,
		"image_name": inst.ImageName,

		// 网络信息
		"public_ip":                  inst.PublicIP,
		"private_ip":                 inst.PrivateIP,
		"vpc_id":                     inst.VPCID,
		"vpc_name":                   inst.VPCName,
		"vswitch_id":                 inst.VSwitchID,
		"vswitch_name":               inst.VSwitchName,
		"security_groups":            inst.SecurityGroups,
		"security_group_ids":         securityGroupIDs,
		"internet_max_bandwidth_in":  inst.InternetMaxBandwidthIn,
		"internet_max_bandwidth_out": inst.InternetMaxBandwidthOut,
		"network_type":               inst.NetworkType,
		"instance_network_type":      inst.InstanceNetworkType,

		// 系统盘信息
		"system_disk":          inst.SystemDisk,
		"system_disk_id":       inst.SystemDisk.DiskID,
		"system_disk_category": inst.SystemDisk.Category,
		"system_disk_size":     inst.SystemDisk.Size,

		// 数据盘信息
		"data_disks": inst.DataDisks,

		// 计费信息
		"charge_type":       inst.ChargeType,
		"creation_time":     inst.CreationTime,
		"expired_time":      inst.ExpiredTime,
		"auto_renew":        inst.AutoRenew,
		"auto_renew_period": inst.AutoRenewPeriod,

		// 项目/资源组信息
		"project_id":   inst.ProjectID,
		"project_name": inst.ProjectName,

		// 云账号信息
		"cloud_account_id":   account.ID,
		"cloud_account_name": account.Name,

		// IO优化
		"io_optimized": inst.IoOptimized,

		// 标签
		"tags": inst.Tags,
	}

	return camdomain.Instance{
		ModelUID:   modelUID,
		AssetID:    inst.InstanceID,
		AssetName:  inst.InstanceName,
		TenantID:   account.TenantID,
		AccountID:  account.ID,
		Attributes: attributes,
	}
}
