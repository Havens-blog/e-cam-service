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

// syncRegionMongoDB 同步单个地域的 MongoDB 实例
func (e *SyncAssetsExecutor) syncRegionMongoDB(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_mongodb", account.Provider)

	// 获取云端实例
	mongodbAdapter := adapter.MongoDB()
	if mongodbAdapter == nil {
		return 0, fmt.Errorf("MongoDB适配器不可用")
	}

	cloudInstances, err := mongodbAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取MongoDB实例失败: %w", err)
	}

	items := make([]syncItem, 0, len(cloudInstances))
	for _, inst := range cloudInstances {
		items = append(items, syncItem{
			AssetID: inst.InstanceID,
			ToInstance: func() (camdomain.Instance, error) {
				return e.convertMongoDBToInstance(inst, account), nil
			},
		})
	}

	synced, deleted, err := e.diffAndUpsert(ctx, account.TenantID, modelUID, account.ID, region, items)
	if err != nil {
		return synced, err
	}

	e.logger.Info("同步地域MongoDB完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int64("deleted", deleted))

	return synced, nil
}

// convertMongoDBToInstance 将 MongoDB 实例转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertMongoDBToInstance(inst types.MongoDBInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_mongodb", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"status":      inst.Status,
		"region":      inst.Region,
		"zone":        inst.Zone,
		"provider":    inst.Provider,
		"description": inst.Description,

		// MongoDB信息
		"engine_version":   inst.EngineVersion,
		"instance_class":   inst.InstanceClass,
		"db_instance_type": inst.DBInstanceType,

		// 配置信息
		"cpu":          inst.CPU,
		"memory":       inst.Memory,
		"storage":      inst.Storage,
		"storage_type": inst.StorageType,

		// 网络信息
		"connection_string": inst.ConnectionString,
		"port":              inst.Port,
		"vpc_id":            inst.VPCID,
		"vswitch_id":        inst.VSwitchID,

		// 副本集/分片信息
		"replica_set_name": inst.ReplicaSetName,
		"shard_count":      inst.ShardCount,
		"mongos_count":     inst.MongosCount,
		"node_count":       inst.NodeCount,

		// 计费信息
		"charge_type":   inst.ChargeType,
		"creation_time": inst.CreationTime,
		"expired_time":  inst.ExpiredTime,

		// 安全信息
		"security_ip_list": inst.SecurityIPList,
		"ssl_enabled":      inst.SSLEnabled,

		// 备份信息
		"backup_retention_period": inst.BackupRetentionPeriod,
		"preferred_backup_time":   inst.PreferredBackupTime,

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
		AssetID:    inst.InstanceID,
		AssetName:  inst.InstanceName,
		TenantID:   account.TenantID,
		AccountID:  account.ID,
		Attributes: attributes,
	}
}
