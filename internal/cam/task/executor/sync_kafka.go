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

// syncRegionKafka 同步单个地域的 Kafka 实例
func (e *SyncAssetsExecutor) syncRegionKafka(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_kafka", account.Provider)

	e.logger.Info("开始同步Kafka实例",
		elog.String("region", region),
		elog.String("model_uid", modelUID),
		elog.Int64("tenant_id", account.TenantID))

	// 获取云端实例
	kafkaAdapter := adapter.Kafka()
	if kafkaAdapter == nil {
		e.logger.Warn("Kafka适配器不可用", elog.String("provider", string(account.Provider)))
		return 0, nil // 返回0而不是错误，因为某些云厂商可能未实现
	}

	cloudInstances, err := kafkaAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取Kafka实例失败: %w", err)
	}

	e.logger.Info("获取到云端Kafka实例",
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
			e.logger.Error("删除过期Kafka实例失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期Kafka实例", elog.Int64("deleted", deleted))
		}
	}

	// 新增或更新实例
	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertKafkaToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存Kafka实例失败", elog.String("asset_id", inst.InstanceID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域Kafka完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int("deleted", len(toDelete)))

	return synced, nil
}

// convertKafkaToInstance 将 Kafka 实例转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertKafkaToInstance(inst types.KafkaInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_kafka", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"status":      inst.Status,
		"region":      inst.Region,
		"zone":        inst.Zone,
		"provider":    inst.Provider,
		"description": inst.Description,

		// 版本信息
		"version":      inst.Version,
		"spec_type":    inst.SpecType,
		"message_type": inst.MessageType,

		// 配置信息
		"topic_count":       inst.TopicCount,
		"topic_quota":       inst.TopicQuota,
		"partition_count":   inst.PartitionCount,
		"partition_quota":   inst.PartitionQuota,
		"consumer_groups":   inst.ConsumerGroups,
		"max_message_size":  inst.MaxMessageSize,
		"message_retention": inst.MessageRetention,
		"disk_size":         inst.DiskSize,
		"disk_used":         inst.DiskUsed,
		"disk_type":         inst.DiskType,

		// 性能配置
		"bandwidth":     inst.Bandwidth,
		"tps":           inst.TPS,
		"io_max":        inst.IOMax,
		"broker_count":  inst.BrokerCount,
		"zookeeper_num": inst.ZookeeperNum,

		// 网络信息
		"vpc_id":            inst.VPCID,
		"vswitch_id":        inst.VSwitchID,
		"security_group_id": inst.SecurityGroupID,
		"endpoint_type":     inst.EndpointType,
		"bootstrap_servers": inst.BootstrapServers,
		"ssl_endpoint":      inst.SSLEndpoint,
		"sasl_endpoint":     inst.SASLEndpoint,
		"zone_ids":          inst.ZoneIDs,

		// 安全配置
		"ssl_enabled":  inst.SSLEnabled,
		"sasl_enabled": inst.SASLEnabled,
		"acl_enabled":  inst.ACLEnabled,
		"encrypt_type": inst.EncryptType,
		"kms_key_id":   inst.KMSKeyID,

		// 计费信息
		"charge_type":   inst.ChargeType,
		"creation_time": inst.CreationTime,
		"expired_time":  inst.ExpiredTime,

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
