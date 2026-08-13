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

// syncRegionOSS 同步单个地域的 OSS 存储桶
// 注意：OSS 是全局服务，bucket 名称全局唯一，不按 region 隔离
func (e *SyncAssetsExecutor) syncRegionOSS(
	ctx context.Context,
	adapter cloudx.CloudAdapter,
	account *domain.CloudAccount,
	region string,
) (int, error) {
	modelUID := fmt.Sprintf("%s_oss", account.Provider)

	e.logger.Info("开始同步OSS存储桶",
		elog.String("region", region),
		elog.String("model_uid", modelUID),
		elog.Int64("tenant_id", account.TenantID))

	// 获取云端实例
	ossAdapter := adapter.OSS()
	if ossAdapter == nil {
		return 0, fmt.Errorf("OSS适配器不可用")
	}

	// OSS 是全局服务，ListBuckets 会返回所有 bucket
	// 传入 region 参数，让适配器按需过滤（有些云厂商支持按 region 过滤）
	cloudBuckets, err := ossAdapter.ListBuckets(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取OSS存储桶失败: %w", err)
	}

	// 如果指定了 region，只同步该 region 的 bucket
	if region != "" {
		filtered := make([]types.OSSBucket, 0)
		for _, bucket := range cloudBuckets {
			if bucket.Region == region {
				filtered = append(filtered, bucket)
			}
		}
		cloudBuckets = filtered
	}

	e.logger.Info("获取到云端OSS存储桶",
		elog.String("region", region),
		elog.Int("count", len(cloudBuckets)))

	// 新增或更新实例（不删除，因为 OSS 是全局服务，其他 region 的 bucket 不应该被删除）
	synced := 0
	for _, bucket := range cloudBuckets {
		instance := e.convertOSSToInstance(bucket, account)
		if err := e.instanceRepo.Upsert(ctx, instance); err != nil {
			e.logger.Error("保存OSS存储桶失败", elog.String("asset_id", bucket.BucketName), elog.FieldErr(err))
			continue
		}
		synced++
	}

	e.logger.Info("同步地域OSS完成",
		elog.String("region", region),
		elog.Int("synced", synced))

	return synced, nil
}

// convertOSSToInstance 将 OSS 存储桶转换为 Instance 领域模型
func (e *SyncAssetsExecutor) convertOSSToInstance(bucket types.OSSBucket, account *domain.CloudAccount) camdomain.Instance {
	modelUID := fmt.Sprintf("%s_oss", account.Provider)

	attributes := map[string]any{
		// 基本信息
		"region":      bucket.Region,
		"location":    bucket.Location,
		"provider":    bucket.Provider,
		"bucket_name": bucket.BucketName,

		// 存储配置
		"storage_class": bucket.StorageClass,
		"acl":           bucket.ACL,
		"versioning":    bucket.Versioning,

		// 加密信息
		"server_side_encryption": bucket.ServerSideEncryption,
		"kms_key_id":             bucket.KMSKeyID,

		// 访问信息
		"extranet_endpoint":     bucket.ExtranetEndpoint,
		"intranet_endpoint":     bucket.IntranetEndpoint,
		"transfer_acceleration": bucket.TransferAcceleration,

		// 统计信息
		"object_count": bucket.ObjectCount,
		"storage_size": bucket.StorageSize,

		// 计费信息
		"creation_time": bucket.CreationTime,

		// 云账号信息
		"cloud_account_id":   account.ID,
		"cloud_account_name": account.Name,

		// 标签
		"tags": bucket.Tags,
	}

	return camdomain.Instance{
		ModelUID:   modelUID,
		AssetID:    bucket.BucketName,
		AssetName:  bucket.BucketName,
		TenantID:   account.TenantID,
		AccountID:  account.ID,
		Attributes: attributes,
	}
}
