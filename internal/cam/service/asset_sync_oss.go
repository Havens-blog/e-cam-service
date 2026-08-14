package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

func (s *assetSyncService) syncOSSBuckets(
	ctx context.Context,
	tenantID int64,
	adapter cloudx.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (*SyncResult, error) {
	result := &SyncResult{ByAssetType: make(map[string]int), ByRegion: make(map[string]int)}
	modelUID := fmt.Sprintf("%s_oss", account.Provider)

	buckets, err := adapter.OSS().ListBuckets(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("获取OSS存储桶失败: %w", err)
	}

	cloudAssetIDs := make(map[string]bool, len(buckets))
	for _, bucket := range buckets {
		if region == "" || bucket.Region == region {
			cloudAssetIDs[bucket.BucketName] = true
		}
	}
	s.cleanupStaleInstances(ctx, tenantID, modelUID, account.ID, "", cloudAssetIDs)

	for _, bucket := range buckets {
		// 如果指定了region，只同步该region的bucket
		if region != "" && bucket.Region != region {
			continue
		}

		attrs := map[string]interface{}{
			"provider": string(account.Provider), "cloud_account_id": account.ID,
			"region": bucket.Region, "bucket_name": bucket.BucketName,
			"storage_class": bucket.StorageClass, "acl": bucket.ACL,
			"versioning":  bucket.Versioning,
			"create_time": bucket.CreationTime, "tags": bucket.Tags,
		}
		cmdbInstance := domain.Instance{
			ModelUID: fmt.Sprintf("%s_oss", account.Provider), AssetID: bucket.BucketName, AssetName: bucket.BucketName,
			TenantID: tenantID, AccountID: account.ID, Attributes: attrs,
		}
		if err := s.trackAndUpsert(ctx, cmdbInstance); err != nil {
			result.Failed++
			continue
		}
		result.TotalSynced++
	}
	return result, nil
}
