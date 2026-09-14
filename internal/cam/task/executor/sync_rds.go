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

// syncRegionRDS 同步单个地域的 RDS 实例
func (e *SyncAssetsExecutor) syncRegionRDS(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_rds", account.Provider)

	e.logger.Info("开始同步RDS实例",
		elog.String("region", region),
		elog.String("model_uid", modelUID),
		elog.Int64("tenant_id", account.TenantID))

	// 获取云端实例
	rdsAdapter := adapter.RDS()
	if rdsAdapter == nil {
		return 0, fmt.Errorf("RDS适配器不可用")
	}

	cloudInstances, err := rdsAdapter.ListInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取RDS实例失败: %w", err)
	}

	e.logger.Info("获取到云端RDS实例",
		elog.String("region", region),
		elog.Int("count", len(cloudInstances)))

	items := make([]syncItem, 0, len(cloudInstances))
	for _, inst := range cloudInstances {
		items = append(items, syncItem{
			AssetID: inst.InstanceID,
			ToInstance: func() (camdomain.Instance, error) {
				return e.convertRDSToInstance(inst, account), nil
			},
		})
	}

	synced, deleted, err := e.diffAndUpsert(ctx, account.TenantID, modelUID, account.ID, region, items)
	if err != nil {
		return synced, err
	}

	e.logger.Info("同步地域RDS完成",
		elog.String("region", region),
		elog.Int("synced", synced),
		elog.Int64("deleted", deleted))

	return synced, nil
}

// convertRDSToInstance 将 RDS 实例转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertRDSToInstance(inst types.RDSInstance, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_rds", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"status":      inst.Status,
		"region":      inst.Region,
		"zone":        inst.Zone,
		"provider":    inst.Provider,
		"description": inst.Description,

		// 数据库信息
		"engine":            inst.Engine,
		"engine_version":    inst.EngineVersion,
		"db_instance_class": inst.DBInstanceClass,

		// 配置信息
		"cpu":          inst.CPU,
		"memory":       inst.Memory,
		"storage":      inst.Storage,
		"storage_type": inst.StorageType,
		"max_iops":     inst.MaxIOPS,

		// 网络信息
		"connection_string": inst.ConnectionString,
		"port":              inst.Port,
		"vpc_id":            inst.VPCID,
		"vswitch_id":        inst.VSwitchID,
		"private_ip":        inst.PrivateIP,
		"public_ip":         inst.PublicIP,

		// 高可用信息
		"category":           inst.Category,
		"replication_mode":   inst.ReplicationMode,
		"secondary_zone":     inst.SecondaryZone,
		"read_replica_count": inst.ReadReplicaCount,

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
