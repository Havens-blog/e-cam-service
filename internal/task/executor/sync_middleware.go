// Package executor 任务执行器 - 中间件资源同步
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

// syncRegionKafka 同步单个地域的 Kafka 实例
func (e *SyncAssetsExecutor) syncRegionKafka(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_kafka", account.Provider)

	kafkaAdapter := adapter.Kafka()
	if kafkaAdapter == nil {
		e.logger.Warn("Kafka适配器不可用", elog.String("provider", string(account.Provider)))
		return 0, nil
	}

	cloudInstances, err := kafkaAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取Kafka实例失败: %w", err)
	}

	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.InstanceID] = true
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
			e.logger.Error("删除过期Kafka实例失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期Kafka实例", elog.Int64("deleted", deleted))
		}
	}

	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertKafkaToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存Kafka实例失败", elog.String("asset_id", inst.InstanceID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域Kafka完成", elog.String("region", region), elog.Int("synced", synced))
	return synced, nil
}

// syncRegionElasticsearch 同步单个地域的 Elasticsearch 实例
func (e *SyncAssetsExecutor) syncRegionElasticsearch(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_elasticsearch", account.Provider)

	esAdapter := adapter.Elasticsearch()
	if esAdapter == nil {
		e.logger.Warn("Elasticsearch适配器不可用", elog.String("provider", string(account.Provider)))
		return 0, nil
	}

	cloudInstances, err := esAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取Elasticsearch实例失败: %w", err)
	}

	localAssetIDs, err := e.instanceRepo.ListAssetIDsByRegion(ctx, account.TenantID, modelUID, account.ID, region)
	if err != nil {
		localAssetIDs = []string{}
	}

	cloudAssetIDSet := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudAssetIDSet[inst.InstanceID] = true
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
			e.logger.Error("删除过期Elasticsearch实例失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期Elasticsearch实例", elog.Int64("deleted", deleted))
		}
	}

	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertElasticsearchToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存Elasticsearch实例失败", elog.String("asset_id", inst.InstanceID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域Elasticsearch完成", elog.String("region", region), elog.Int("synced", synced))
	return synced, nil
}

// convertKafkaToInstance 将 Kafka 实例转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertKafkaToInstance(inst types.KafkaInstance, account *domain.CloudAccount) assetdomain.Instance {
	modelUID := fmt.Sprintf("%s_kafka", account.Provider)

	attributes := map[string]any{
		"status": inst.Status, "region": inst.Region, "zone": inst.Zone,
		"provider": inst.Provider, "description": inst.Description,
		"version": inst.Version, "spec_type": inst.SpecType, "message_type": inst.MessageType,
		"topic_count": inst.TopicCount, "topic_quota": inst.TopicQuota,
		"partition_count": inst.PartitionCount, "partition_quota": inst.PartitionQuota,
		"consumer_groups": inst.ConsumerGroups, "max_message_size": inst.MaxMessageSize,
		"message_retention": inst.MessageRetention, "disk_size": inst.DiskSize,
		"disk_used": inst.DiskUsed, "disk_type": inst.DiskType,
		"bandwidth": inst.Bandwidth, "tps": inst.TPS, "io_max": inst.IOMax,
		"broker_count": inst.BrokerCount, "zookeeper_num": inst.ZookeeperNum,
		"vpc_id": inst.VPCID, "vswitch_id": inst.VSwitchID, "security_group_id": inst.SecurityGroupID,
		"endpoint_type": inst.EndpointType, "bootstrap_servers": inst.BootstrapServers,
		"ssl_endpoint": inst.SSLEndpoint, "sasl_endpoint": inst.SASLEndpoint, "zone_ids": inst.ZoneIDs,
		"ssl_enabled": inst.SSLEnabled, "sasl_enabled": inst.SASLEnabled, "acl_enabled": inst.ACLEnabled,
		"encrypt_type": inst.EncryptType, "kms_key_id": inst.KMSKeyID,
		"charge_type": inst.ChargeType, "creation_time": inst.CreationTime, "expired_time": inst.ExpiredTime,
		"project_id": inst.ProjectID, "project_name": inst.ProjectName, "resource_group_id": inst.ResourceGroupID,
		"cloud_account_id": account.ID, "cloud_account_name": account.Name,
		"tags": inst.Tags,
	}

	return assetdomain.Instance{
		ModelUID: modelUID, AssetID: inst.InstanceID, AssetName: inst.InstanceName,
		TenantID: account.TenantID, AccountID: account.ID, Attributes: attributes,
	}
}

// convertElasticsearchToInstance 将 Elasticsearch 实例转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertElasticsearchToInstance(inst types.ElasticsearchInstance, account *domain.CloudAccount) assetdomain.Instance {
	modelUID := fmt.Sprintf("%s_elasticsearch", account.Provider)

	attributes := map[string]any{
		"status": inst.Status, "region": inst.Region, "zone": inst.Zone,
		"provider": inst.Provider, "description": inst.Description,
		"version": inst.Version, "engine_type": inst.EngineType, "license_type": inst.LicenseType,
		"node_count": inst.NodeCount, "node_spec": inst.NodeSpec,
		"node_cpu": inst.NodeCPU, "node_memory": inst.NodeMemory,
		"node_disk_size": inst.NodeDiskSize, "node_disk_type": inst.NodeDiskType,
		"master_count": inst.MasterCount, "master_spec": inst.MasterSpec,
		"client_count": inst.ClientCount, "client_spec": inst.ClientSpec,
		"warm_count": inst.WarmCount, "warm_spec": inst.WarmSpec, "warm_disk_size": inst.WarmDiskSize,
		"kibana_count": inst.KibanaCount, "kibana_spec": inst.KibanaSpec,
		"total_disk_size": inst.TotalDiskSize, "used_disk_size": inst.UsedDiskSize,
		"index_count": inst.IndexCount, "doc_count": inst.DocCount, "shard_count": inst.ShardCount,
		"vpc_id": inst.VPCID, "vswitch_id": inst.VSwitchID, "security_group_id": inst.SecurityGroupID,
		"private_endpoint": inst.PrivateEndpoint, "public_endpoint": inst.PublicEndpoint,
		"kibana_endpoint": inst.KibanaEndpoint, "kibana_private_url": inst.KibanaPrivateURL,
		"kibana_public_url": inst.KibanaPublicURL, "port": inst.Port,
		"enable_public_access": inst.EnablePublicAccess,
		"ssl_enabled":          inst.SSLEnabled, "auth_enabled": inst.AuthEnabled,
		"encrypt_type": inst.EncryptType, "kms_key_id": inst.KMSKeyID,
		"whitelist_enabled": inst.WhitelistEnabled, "whitelist_ips": inst.WhitelistIPs,
		"zone_count": inst.ZoneCount, "zone_ids": inst.ZoneIDs,
		"enable_ha": inst.EnableHA, "enable_auto_scale": inst.EnableAutoScale,
		"charge_type": inst.ChargeType, "creation_time": inst.CreationTime,
		"expired_time": inst.ExpiredTime, "update_time": inst.UpdateTime,
		"project_id": inst.ProjectID, "project_name": inst.ProjectName, "resource_group_id": inst.ResourceGroupID,
		"cloud_account_id": account.ID, "cloud_account_name": account.Name,
		"tags": inst.Tags,
	}

	return assetdomain.Instance{
		ModelUID: modelUID, AssetID: inst.InstanceID, AssetName: inst.InstanceName,
		TenantID: account.TenantID, AccountID: account.ID, Attributes: attributes,
	}
}
