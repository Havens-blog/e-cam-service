package aliyun

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/gotomicro/ego/core/elog"
)

// OSSAdapter 阿里云OSS适配器
type OSSAdapter struct {
	accessKeyID     string
	accessKeySecret string
	defaultRegion   string
	logger          *elog.Component
}

// NewOSSAdapter 创建OSS适配器
func NewOSSAdapter(accessKeyID, accessKeySecret, defaultRegion string, logger *elog.Component) *OSSAdapter {
	return &OSSAdapter{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		defaultRegion:   defaultRegion,
		logger:          logger,
	}
}

// createClient 创建OSS客户端
func (a *OSSAdapter) createClient(region string) (*oss.Client, error) {
	if region == "" {
		region = a.defaultRegion
	}

	endpoint := fmt.Sprintf("oss-%s.aliyuncs.com", region)
	return oss.New(endpoint, a.accessKeyID, a.accessKeySecret)
}

// ListBuckets 获取存储桶列表
func (a *OSSAdapter) ListBuckets(ctx context.Context, region string) ([]types.OSSBucket, error) {
	client, err := a.createClient(region)
	if err != nil {
		return nil, fmt.Errorf("创建OSS客户端失败: %w", err)
	}

	var buckets []types.OSSBucket
	marker := ""

	for {
		result, err := client.ListBuckets(oss.Marker(marker), oss.MaxKeys(100))
		if err != nil {
			return nil, fmt.Errorf("获取OSS存储桶列表失败: %w", err)
		}

		for _, bucket := range result.Buckets {
			ossBucket := a.convertToOSSBucket(bucket)

			// 如果指定了region，只返回该region的bucket
			if region != "" && ossBucket.Region != region {
				continue
			}

			// 获取 bucket 统计信息（存储大小和对象数量）
			stats, err := a.GetBucketStats(ctx, bucket.Name)
			if err == nil && stats != nil {
				ossBucket.ObjectCount = stats.ObjectCount
				ossBucket.StorageSize = stats.StorageSize
			} else {
				a.logger.Warn("获取OSS存储桶统计信息失败",
					elog.String("bucket", bucket.Name),
					elog.FieldErr(err))
			}

			buckets = append(buckets, ossBucket)
		}

		if !result.IsTruncated {
			break
		}
		marker = result.NextMarker
	}

	return buckets, nil
}

// GetBucket 获取单个存储桶详情
func (a *OSSAdapter) GetBucket(ctx context.Context, bucketName string) (*types.OSSBucket, error) {
	// 先获取bucket列表找到bucket的region
	client, err := a.createClient("")
	if err != nil {
		return nil, fmt.Errorf("创建OSS客户端失败: %w", err)
	}

	// 获取bucket信息
	result, err := client.GetBucketInfo(bucketName)
	if err != nil {
		return nil, fmt.Errorf("获取OSS存储桶信息失败: %w", err)
	}

	bucket := a.convertBucketInfoToOSSBucket(result.BucketInfo)

	// 获取bucket统计信息
	stats, err := a.GetBucketStats(ctx, bucketName)
	if err == nil && stats != nil {
		bucket.ObjectCount = stats.ObjectCount
		bucket.StorageSize = stats.StorageSize
	}

	return &bucket, nil
}

// GetBucketStats 获取存储桶统计信息
func (a *OSSAdapter) GetBucketStats(ctx context.Context, bucketName string) (*types.OSSBucketStats, error) {
	client, err := a.createClient("")
	if err != nil {
		return nil, fmt.Errorf("创建OSS客户端失败: %w", err)
	}

	result, err := client.GetBucketStat(bucketName)
	if err != nil {
		return nil, fmt.Errorf("获取OSS存储桶统计信息失败: %w", err)
	}

	return &types.OSSBucketStats{
		BucketName:       bucketName,
		ObjectCount:      result.ObjectCount,
		StorageSize:      result.Storage,
		MultipartCount:   result.MultipartUploadCount,
		LiveChannelCount: result.LiveChannelCount,
		LastModifiedTime: result.LastModifiedTime,
	}, nil
}

// ListBucketsWithFilter 带过滤条件获取存储桶列表
func (a *OSSAdapter) ListBucketsWithFilter(ctx context.Context, region string, filter *types.OSSBucketFilter) ([]types.OSSBucket, error) {
	client, err := a.createClient(region)
	if err != nil {
		return nil, fmt.Errorf("创建OSS客户端失败: %w", err)
	}

	var options []oss.Option
	if filter != nil && filter.Prefix != "" {
		options = append(options, oss.Prefix(filter.Prefix))
	}

	var buckets []types.OSSBucket
	marker := ""

	for {
		opts := append(options, oss.Marker(marker), oss.MaxKeys(100))
		result, err := client.ListBuckets(opts...)
		if err != nil {
			return nil, fmt.Errorf("获取OSS存储桶列表失败: %w", err)
		}

		for _, bucket := range result.Buckets {
			ossBucket := a.convertToOSSBucket(bucket)

			// 应用过滤条件
			if region != "" && ossBucket.Region != region {
				continue
			}
			if filter != nil && filter.StorageClass != "" && ossBucket.StorageClass != filter.StorageClass {
				continue
			}

			buckets = append(buckets, ossBucket)
		}

		if !result.IsTruncated {
			break
		}
		marker = result.NextMarker
	}

	return buckets, nil
}

// convertToOSSBucket 转换为统一的OSS存储桶结构
func (a *OSSAdapter) convertToOSSBucket(bucket oss.BucketProperties) types.OSSBucket {
	// 从Location提取region (如 oss-cn-hangzhou -> cn-hangzhou)
	region := bucket.Location
	if len(region) > 4 && region[:4] == "oss-" {
		region = region[4:]
	}

	return types.OSSBucket{
		BucketName:       bucket.Name,
		Region:           region,
		Location:         bucket.Location,
		CreationTime:     bucket.CreationDate,
		StorageClass:     bucket.StorageClass,
		ExtranetEndpoint: fmt.Sprintf("%s.oss-%s.aliyuncs.com", bucket.Name, region),
		IntranetEndpoint: fmt.Sprintf("%s.oss-%s-internal.aliyuncs.com", bucket.Name, region),
		Provider:         "aliyun",
	}
}

// convertBucketInfoToOSSBucket 从BucketInfo转换
func (a *OSSAdapter) convertBucketInfoToOSSBucket(info oss.BucketInfo) types.OSSBucket {
	region := info.Location
	if len(region) > 4 && region[:4] == "oss-" {
		region = region[4:]
	}

	bucket := types.OSSBucket{
		BucketName:       info.Name,
		Region:           region,
		Location:         info.Location,
		CreationTime:     info.CreationDate,
		StorageClass:     info.StorageClass,
		ACL:              string(info.ACL),
		ExtranetEndpoint: info.ExtranetEndpoint,
		IntranetEndpoint: info.IntranetEndpoint,
		Provider:         "aliyun",
	}

	// 版本控制
	if info.Versioning != "" {
		bucket.Versioning = info.Versioning
	}

	// 跨区域复制
	bucket.CrossRegionReplication = info.CrossRegionReplication == "Enabled"

	// 传输加速
	bucket.TransferAcceleration = info.TransferAcceleration == "Enabled"

	return bucket
}
