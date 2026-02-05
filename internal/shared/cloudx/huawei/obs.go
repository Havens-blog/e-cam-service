package huawei

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
	"github.com/huaweicloud/huaweicloud-sdk-go-obs/obs"
)

// OBSAdapter 华为云 OBS (对象存储服务) 适配器
type OBSAdapter struct {
	accessKeyID     string
	accessKeySecret string
	defaultRegion   string
	logger          *elog.Component
}

// NewOBSAdapter 创建 OBS 适配器
func NewOBSAdapter(accessKeyID, accessKeySecret, defaultRegion string, logger *elog.Component) *OBSAdapter {
	return &OBSAdapter{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		defaultRegion:   defaultRegion,
		logger:          logger,
	}
}

// createClient 创建 OBS 客户端
func (a *OBSAdapter) createClient(region string) (*obs.ObsClient, error) {
	if region == "" {
		region = a.defaultRegion
	}

	endpoint := fmt.Sprintf("obs.%s.myhuaweicloud.com", region)
	return obs.New(a.accessKeyID, a.accessKeySecret, endpoint)
}

// ListBuckets 获取存储桶列表
func (a *OBSAdapter) ListBuckets(ctx context.Context, region string) ([]types.OSSBucket, error) {
	client, err := a.createClient(region)
	if err != nil {
		return nil, fmt.Errorf("创建OBS客户端失败: %w", err)
	}
	defer client.Close()

	output, err := client.ListBuckets(nil)
	if err != nil {
		return nil, fmt.Errorf("获取OBS存储桶列表失败: %w", err)
	}

	var buckets []types.OSSBucket
	for _, bucket := range output.Buckets {
		ossBucket := a.convertToOSSBucket(&bucket)

		// 如果指定了 region，只返回该 region 的 bucket
		if region != "" && ossBucket.Region != region {
			continue
		}

		// 获取 bucket 统计信息（存储大小和对象数量）
		stats, err := a.GetBucketStats(ctx, bucket.Name)
		if err == nil && stats != nil {
			ossBucket.ObjectCount = stats.ObjectCount
			ossBucket.StorageSize = stats.StorageSize
		} else {
			a.logger.Warn("获取OBS存储桶统计信息失败",
				elog.String("bucket", bucket.Name),
				elog.FieldErr(err))
		}

		buckets = append(buckets, ossBucket)
	}

	return buckets, nil
}

// GetBucket 获取单个存储桶详情
func (a *OBSAdapter) GetBucket(ctx context.Context, bucketName string) (*types.OSSBucket, error) {
	client, err := a.createClient("")
	if err != nil {
		return nil, fmt.Errorf("创建OBS客户端失败: %w", err)
	}
	defer client.Close()

	// 获取 bucket 位置
	locationOutput, err := client.GetBucketLocation(bucketName)
	if err != nil {
		return nil, fmt.Errorf("获取存储桶位置失败: %w", err)
	}

	region := locationOutput.Location
	bucket := &types.OSSBucket{
		BucketName:       bucketName,
		Region:           region,
		Location:         region,
		Provider:         string(domain.CloudProviderHuawei),
		ExtranetEndpoint: fmt.Sprintf("%s.obs.%s.myhuaweicloud.com", bucketName, region),
		IntranetEndpoint: fmt.Sprintf("%s.obs.%s.myhuaweicloud.com", bucketName, region),
	}

	// 获取 ACL
	aclOutput, err := client.GetBucketAcl(bucketName)
	if err == nil {
		bucket.ACL = a.parseACL(aclOutput)
	}

	// 获取版本控制状态
	versionOutput, err := client.GetBucketVersioning(bucketName)
	if err == nil {
		bucket.Versioning = string(versionOutput.Status)
	}

	// 获取加密配置
	encryptOutput, err := client.GetBucketEncryption(bucketName)
	if err == nil {
		bucket.ServerSideEncryption = encryptOutput.SSEAlgorithm
		bucket.KMSKeyID = encryptOutput.KMSMasterKeyID
	}

	// 获取存储类型
	metaOutput, err := client.GetBucketMetadata(&obs.GetBucketMetadataInput{Bucket: bucketName})
	if err == nil {
		bucket.StorageClass = string(metaOutput.StorageClass)
	}

	return bucket, nil
}

// parseACL 解析 ACL
func (a *OBSAdapter) parseACL(output *obs.GetBucketAclOutput) string {
	if output == nil {
		return "private"
	}

	hasPublicRead := false
	hasPublicWrite := false

	for _, grant := range output.Grants {
		if grant.Grantee.URI == obs.GroupAllUsers {
			if grant.Permission == obs.PermissionRead {
				hasPublicRead = true
			}
			if grant.Permission == obs.PermissionWrite {
				hasPublicWrite = true
			}
		}
	}

	if hasPublicRead && hasPublicWrite {
		return "public-read-write"
	}
	if hasPublicRead {
		return "public-read"
	}
	return "private"
}

// GetBucketStats 获取存储桶统计信息
func (a *OBSAdapter) GetBucketStats(ctx context.Context, bucketName string) (*types.OSSBucketStats, error) {
	client, err := a.createClient("")
	if err != nil {
		return nil, fmt.Errorf("创建OBS客户端失败: %w", err)
	}
	defer client.Close()

	output, err := client.GetBucketStorageInfo(bucketName)
	if err != nil {
		return nil, fmt.Errorf("获取存储桶统计信息失败: %w", err)
	}

	return &types.OSSBucketStats{
		BucketName:  bucketName,
		ObjectCount: int64(output.ObjectNumber),
		StorageSize: output.Size,
	}, nil
}

// ListBucketsWithFilter 带过滤条件获取存储桶列表
func (a *OBSAdapter) ListBucketsWithFilter(ctx context.Context, region string, filter *types.OSSBucketFilter) ([]types.OSSBucket, error) {
	buckets, err := a.ListBuckets(ctx, region)
	if err != nil {
		return nil, err
	}

	if filter == nil {
		return buckets, nil
	}

	var filtered []types.OSSBucket
	for _, bucket := range buckets {
		// 按前缀过滤
		if filter.Prefix != "" && len(bucket.BucketName) >= len(filter.Prefix) {
			if bucket.BucketName[:len(filter.Prefix)] != filter.Prefix {
				continue
			}
		}
		// 按存储类型过滤
		if filter.StorageClass != "" && bucket.StorageClass != filter.StorageClass {
			continue
		}
		filtered = append(filtered, bucket)
	}

	return filtered, nil
}

// convertToOSSBucket 转换为统一的 OSS 存储桶结构
func (a *OBSAdapter) convertToOSSBucket(bucket *obs.Bucket) types.OSSBucket {
	ossBucket := types.OSSBucket{
		BucketName:   bucket.Name,
		Region:       bucket.Location,
		Location:     bucket.Location,
		CreationTime: bucket.CreationDate,
		Provider:     string(domain.CloudProviderHuawei),
	}

	if bucket.Location != "" {
		ossBucket.ExtranetEndpoint = fmt.Sprintf("%s.obs.%s.myhuaweicloud.com", bucket.Name, bucket.Location)
		ossBucket.IntranetEndpoint = fmt.Sprintf("%s.obs.%s.myhuaweicloud.com", bucket.Name, bucket.Location)
	}

	return ossBucket
}
