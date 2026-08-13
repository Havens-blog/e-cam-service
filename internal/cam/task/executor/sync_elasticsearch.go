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

// syncRegionElasticsearch 同步单个地域的 Elasticsearch 实例
func (e *SyncAssetsExecutor) syncRegionElasticsearch(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_elasticsearch", account.Provider)

	e.logger.Info("开始同步Elasticsearch实例",
		elog.String("region", region),
		elog.String("model_uid", modelUID),
		elog.Int64("tenant_id", account.TenantID))

	// 获取云端实例
	esAdapter := adapter.Elasticsearch()
	if esAdapter == nil {
		e.logger.Warn("Elasticsearch适配器不可用", elog.String("provider", string(account.Provider)))
		return 0, nil // 返回0而不是错误，因为某些云厂商可能未实现
	}

	cloudInstances, err := esAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取Elasticsearch实例失败: %w", err)
	}

	e.logger.Info("获取到云端Elasticsearch实例",
		elog.String("region", region),
		elog.Int("count", len(cloudInstances)))

	// 获取本地实例 AssetID 列表
	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	// 构建云端 AssetID 集合
	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.InstanceID] = true
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
			e.logger.Error("删除过期Elasticsearch实例失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期Elasticsearch实例", elog.Int64("deleted", deleted))
		}
	}

	// 新增或更新实例
	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertElasticsearchToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存Elasticsearch实例失败", elog.String("asset_id", inst.InstanceID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域Elasticsearch完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int("deleted", len(toDelete)))

	return synced, nil
}

// convertElasticsearchToInstance 将 Elasticsearch 实例转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertElasticsearchToInstance(inst types.ElasticsearchInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_elasticsearch", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"status":      inst.Status,
		"region":      inst.Region,
		"zone":        inst.Zone,
		"provider":    inst.Provider,
		"description": inst.Description,

		// 版本信息
		"version":      inst.Version,
		"engine_type":  inst.EngineType,
		"license_type": inst.LicenseType,

		// 节点配置
		"node_count":     inst.NodeCount,
		"node_spec":      inst.NodeSpec,
		"node_cpu":       inst.NodeCPU,
		"node_memory":    inst.NodeMemory,
		"node_disk_size": inst.NodeDiskSize,
		"node_disk_type": inst.NodeDiskType,
		"master_count":   inst.MasterCount,
		"master_spec":    inst.MasterSpec,
		"client_count":   inst.ClientCount,
		"client_spec":    inst.ClientSpec,
		"warm_count":     inst.WarmCount,
		"warm_spec":      inst.WarmSpec,
		"warm_disk_size": inst.WarmDiskSize,
		"kibana_count":   inst.KibanaCount,
		"kibana_spec":    inst.KibanaSpec,

		// 存储信息
		"total_disk_size": inst.TotalDiskSize,
		"used_disk_size":  inst.UsedDiskSize,
		"index_count":     inst.IndexCount,
		"doc_count":       inst.DocCount,
		"shard_count":     inst.ShardCount,

		// 网络信息
		"vpc_id":               inst.VPCID,
		"vswitch_id":           inst.VSwitchID,
		"security_group_id":    inst.SecurityGroupID,
		"private_endpoint":     inst.PrivateEndpoint,
		"public_endpoint":      inst.PublicEndpoint,
		"kibana_endpoint":      inst.KibanaEndpoint,
		"kibana_private_url":   inst.KibanaPrivateURL,
		"kibana_public_url":    inst.KibanaPublicURL,
		"port":                 inst.Port,
		"enable_public_access": inst.EnablePublicAccess,

		// 安全配置
		"ssl_enabled":       inst.SSLEnabled,
		"auth_enabled":      inst.AuthEnabled,
		"encrypt_type":      inst.EncryptType,
		"kms_key_id":        inst.KMSKeyID,
		"whitelist_enabled": inst.WhitelistEnabled,
		"whitelist_ips":     inst.WhitelistIPs,

		// 高可用配置
		"zone_count":        inst.ZoneCount,
		"zone_ids":          inst.ZoneIDs,
		"enable_ha":         inst.EnableHA,
		"enable_auto_scale": inst.EnableAutoScale,

		// 计费信息
		"charge_type":   inst.ChargeType,
		"creation_time": inst.CreationTime,
		"expired_time":  inst.ExpiredTime,
		"update_time":   inst.UpdateTime,

		// 项目/资源组信息
		"project_id":        inst.ProjectID,
		"project_name":      inst.ProjectName,
		"resource_group_id": inst.ResourceGroupID,

		// 云账号信息
		"cloud_account_id":   account.ID,
		"cloud_account_name": account.Name,

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
