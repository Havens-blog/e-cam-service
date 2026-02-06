// Package executor 任务执行器 - 数据库资源同步
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

// syncRegionRDS 同步单个地域的 RDS 实例
func (e *SyncAssetsExecutor) syncRegionRDS(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_rds", account.Provider)

	rdsAdapter := adapter.RDS()
	if rdsAdapter == nil {
		return 0, fmt.Errorf("RDS适配器不可用")
	}

	cloudInstances, err := rdsAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取RDS实例失败: %w", err)
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
			e.logger.Error("删除过期RDS实例失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期RDS实例", elog.Int64("deleted", deleted))
		}
	}

	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertRDSToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存RDS实例失败", elog.String("asset_id", inst.InstanceID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域RDS完成", elog.String("region", region), elog.Int("synced", synced))
	return synced, nil
}

// syncRegionRedis 同步单个地域的 Redis 实例
func (e *SyncAssetsExecutor) syncRegionRedis(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_redis", account.Provider)

	redisAdapter := adapter.Redis()
	if redisAdapter == nil {
		return 0, fmt.Errorf("Redis适配器不可用")
	}

	cloudInstances, err := redisAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取Redis实例失败: %w", err)
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
			e.logger.Error("删除过期Redis实例失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期Redis实例", elog.Int64("deleted", deleted))
		}
	}

	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertRedisToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存Redis实例失败", elog.String("asset_id", inst.InstanceID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域Redis完成", elog.String("region", region), elog.Int("synced", synced))
	return synced, nil
}

// syncRegionMongoDB 同步单个地域的 MongoDB 实例
func (e *SyncAssetsExecutor) syncRegionMongoDB(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_mongodb", account.Provider)

	mongodbAdapter := adapter.MongoDB()
	if mongodbAdapter == nil {
		return 0, fmt.Errorf("MongoDB适配器不可用")
	}

	cloudInstances, err := mongodbAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取MongoDB实例失败: %w", err)
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
			e.logger.Error("删除过期MongoDB实例失败", elog.FieldErr(err))
		} else {
			e.logger.Info("删除过期MongoDB实例", elog.Int64("deleted", deleted))
		}
	}

	synced := 0
	for _, inst := range cloudInstances {
		instance := e.convertMongoDBToInstance(inst, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存MongoDB实例失败", elog.String("asset_id", inst.InstanceID), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域MongoDB完成", elog.String("region", region), elog.Int("synced", synced))
	return synced, nil
}

// convertRDSToInstance 将 RDS 实例转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertRDSToInstance(inst types.RDSInstance, account *domain.CloudAccount) assetdomain.Instance {
	modelUID := fmt.Sprintf("%s_rds", account.Provider)

	attributes := map[string]any{
		"status": inst.Status, "region": inst.Region, "zone": inst.Zone,
		"provider": inst.Provider, "description": inst.Description,
		"engine": inst.Engine, "engine_version": inst.EngineVersion,
		"db_instance_class": inst.DBInstanceClass,
		"cpu":               inst.CPU, "memory": inst.Memory, "storage": inst.Storage,
		"storage_type": inst.StorageType, "max_iops": inst.MaxIOPS,
		"connection_string": inst.ConnectionString, "port": inst.Port,
		"vpc_id": inst.VPCID, "vswitch_id": inst.VSwitchID,
		"private_ip": inst.PrivateIP, "public_ip": inst.PublicIP,
		"category": inst.Category, "replication_mode": inst.ReplicationMode,
		"secondary_zone": inst.SecondaryZone, "read_replica_count": inst.ReadReplicaCount,
		"charge_type": inst.ChargeType, "creation_time": inst.CreationTime, "expired_time": inst.ExpiredTime,
		"security_ip_list": inst.SecurityIPList, "ssl_enabled": inst.SSLEnabled,
		"backup_retention_period": inst.BackupRetentionPeriod, "preferred_backup_time": inst.PreferredBackupTime,
		"project_id": inst.ProjectID, "project_name": inst.ProjectName,
		"cloud_account_id": account.ID, "cloud_account_name": account.Name,
		"tags": inst.Tags,
	}

	return assetdomain.Instance{
		ModelUID: modelUID, AssetID: inst.InstanceID, AssetName: inst.InstanceName,
		TenantID: account.TenantID, AccountID: account.ID, Attributes: attributes,
	}
}

// convertRedisToInstance 将 Redis 实例转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertRedisToInstance(inst types.RedisInstance, account *domain.CloudAccount) assetdomain.Instance {
	modelUID := fmt.Sprintf("%s_redis", account.Provider)

	attributes := map[string]any{
		"status": inst.Status, "region": inst.Region, "zone": inst.Zone,
		"provider": inst.Provider, "description": inst.Description,
		"engine_version": inst.EngineVersion, "instance_class": inst.InstanceClass,
		"architecture": inst.Architecture, "capacity": inst.Capacity,
		"bandwidth": inst.Bandwidth, "connections": inst.Connections,
		"qps": inst.QPS, "shard_count": inst.ShardCount,
		"connection_domain": inst.ConnectionDomain, "port": inst.Port,
		"vpc_id": inst.VPCID, "vswitch_id": inst.VSwitchID, "private_ip": inst.PrivateIP,
		"node_type": inst.NodeType, "replica_count": inst.ReplicaCount, "secondary_zone": inst.SecondaryZone,
		"charge_type": inst.ChargeType, "creation_time": inst.CreationTime, "expired_time": inst.ExpiredTime,
		"security_ip_list": inst.SecurityIPList, "ssl_enabled": inst.SSLEnabled, "password": inst.Password,
		"project_id": inst.ProjectID, "project_name": inst.ProjectName,
		"cloud_account_id": account.ID, "cloud_account_name": account.Name,
		"tags": inst.Tags,
	}

	return assetdomain.Instance{
		ModelUID: modelUID, AssetID: inst.InstanceID, AssetName: inst.InstanceName,
		TenantID: account.TenantID, AccountID: account.ID, Attributes: attributes,
	}
}

// convertMongoDBToInstance 将 MongoDB 实例转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertMongoDBToInstance(inst types.MongoDBInstance, account *domain.CloudAccount) assetdomain.Instance {
	modelUID := fmt.Sprintf("%s_mongodb", account.Provider)

	attributes := map[string]any{
		"status": inst.Status, "region": inst.Region, "zone": inst.Zone,
		"provider": inst.Provider, "description": inst.Description,
		"engine_version": inst.EngineVersion, "instance_class": inst.InstanceClass,
		"db_instance_type": inst.DBInstanceType,
		"cpu":              inst.CPU, "memory": inst.Memory, "storage": inst.Storage, "storage_type": inst.StorageType,
		"connection_string": inst.ConnectionString, "port": inst.Port,
		"vpc_id": inst.VPCID, "vswitch_id": inst.VSwitchID,
		"replica_set_name": inst.ReplicaSetName, "shard_count": inst.ShardCount,
		"mongos_count": inst.MongosCount, "node_count": inst.NodeCount,
		"charge_type": inst.ChargeType, "creation_time": inst.CreationTime, "expired_time": inst.ExpiredTime,
		"security_ip_list": inst.SecurityIPList, "ssl_enabled": inst.SSLEnabled,
		"backup_retention_period": inst.BackupRetentionPeriod, "preferred_backup_time": inst.PreferredBackupTime,
		"project_id": inst.ProjectID, "project_name": inst.ProjectName,
		"cloud_account_id": account.ID, "cloud_account_name": account.Name,
		"tags": inst.Tags,
	}

	return assetdomain.Instance{
		ModelUID: modelUID, AssetID: inst.InstanceID, AssetName: inst.InstanceName,
		TenantID: account.TenantID, AccountID: account.ID, Attributes: attributes,
	}
}
