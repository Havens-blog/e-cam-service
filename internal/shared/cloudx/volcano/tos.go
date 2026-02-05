package volcano

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/gotomicro/ego/core/elog"
	"github.com/volcengine/ve-tos-golang-sdk/v2/tos"
	"github.com/volcengine/ve-tos-golang-sdk/v2/tos/enum"
)

// TOSAdapter 火山引擎TOS对象存储适配器
type TOSAdapter struct {
	accessKeyID     string
	accessKeySecret string
	defaultRegion   string
	logger          *elog.Component
}

// NewTOSAdapter 创建TOS适配器
func NewTOSAdapter(accessKeyID, accessKeySecret, defaultRegion string, logger *elog.Component) *TOSAdapter {
	return &TOSAdapter{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		defaultRegion:   defaultRegion,
		logger:          logger,
	}
}

// createClient 创建TOS客户端
func (a *TOSAdapter) createClient(region string) (*tos.ClientV2, error) {
	if region == "" {
		region = a.defaultRegion
	}

	endpoint := fmt.Sprintf("tos-%s.volces.com", region)

	client, err := tos.NewClientV2(
		endpoint,
		tos.WithRegion(region),
		tos.WithCredentials(tos.NewStaticCredentials(a.accessKeyID, a.accessKeySecret)),
	)
	if err != nil {
		return nil, fmt.Errorf("创建TOS客户端失败: %w", err)
	}

	return client, nil
}

// ListBuckets 获取存储桶列表
func (a *TOSAdapter) ListBuckets(ctx context.Context, region string) ([]types.OSSBucket, error) {
	client, err := a.createClient(region)
	if err != nil {
		return nil, err
	}

	output, err := client.ListBuckets(ctx, &tos.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf("获取TOS存储桶列表失败: %w", err)
	}

	var buckets []types.OSSBucket
	for _, bucket := range output.Buckets {
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
			a.logger.Warn("获取TOS存储桶统计信息失败",
				elog.String("bucket", bucket.Name),
				elog.FieldErr(err))
		}

		buckets = append(buckets, ossBucket)
	}

	a.logger.Info("获取火山引擎TOS存储桶列表成功",
		elog.String("region", region),
		elog.Int("count", len(buckets)))

	return buckets, nil
}

// GetBucket 获取单个存储桶详情
func (a *TOSAdapter) GetBucket(ctx context.Context, bucketName string) (*types.OSSBucket, error) {
	// 先获取bucket列表找到bucket的region
	client, err := a.createClient("")
	if err != nil {
		return nil, err
	}

	// 获取bucket列表
	listOutput, err := client.ListBuckets(ctx, &tos.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf("获取TOS存储桶列表失败: %w", err)
	}

	// 查找目标bucket
	var targetBucket *tos.ListedBucket
	for i := range listOutput.Buckets {
		if listOutput.Buckets[i].Name == bucketName {
			targetBucket = &listOutput.Buckets[i]
			break
		}
	}

	if targetBucket == nil {
		return nil, fmt.Errorf("存储桶不存在: %s", bucketName)
	}

	bucket := a.convertToOSSBucket(*targetBucket)

	// 获取bucket详细信息
	regionClient, err := a.createClient(bucket.Region)
	if err == nil {
		// 获取ACL
		aclOutput, err := regionClient.GetBucketACL(ctx, &tos.GetBucketACLInput{Bucket: bucketName})
		if err == nil {
			bucket.ACL = a.convertGrantsToACL(aclOutput.Grants)
		}

		// 获取版本控制状态
		versionOutput, err := regionClient.GetBucketVersioning(ctx, &tos.GetBucketVersioningInput{Bucket: bucketName})
		if err == nil {
			bucket.Versioning = string(versionOutput.Status)
		}
	}

	return &bucket, nil
}

// GetBucketStats 获取存储桶统计信息
func (a *TOSAdapter) GetBucketStats(ctx context.Context, bucketName string) (*types.OSSBucketStats, error) {
	// TOS API 不直接提供统计信息，需要通过其他方式获取
	// 这里返回基本信息
	return &types.OSSBucketStats{
		BucketName: bucketName,
	}, nil
}

// ListBucketsWithFilter 带过滤条件获取存储桶列表
func (a *TOSAdapter) ListBucketsWithFilter(ctx context.Context, region string, filter *types.OSSBucketFilter) ([]types.OSSBucket, error) {
	buckets, err := a.ListBuckets(ctx, region)
	if err != nil {
		return nil, err
	}

	if filter == nil {
		return buckets, nil
	}

	var filtered []types.OSSBucket
	for _, bucket := range buckets {
		// 按名称前缀过滤
		if filter.Prefix != "" && !hasPrefix(bucket.BucketName, filter.Prefix) {
			continue
		}

		// 按存储类型过滤
		if filter.StorageClass != "" && bucket.StorageClass != filter.StorageClass {
			continue
		}

		// 按bucket名称列表过滤
		if len(filter.BucketNames) > 0 && !containsString(filter.BucketNames, bucket.BucketName) {
			continue
		}

		filtered = append(filtered, bucket)
	}

	return filtered, nil
}

// convertToOSSBucket 转换为统一的OSS存储桶结构
func (a *TOSAdapter) convertToOSSBucket(bucket tos.ListedBucket) types.OSSBucket {
	region := bucket.Location
	// 火山引擎的Location格式可能是 cn-beijing
	if region == "" {
		region = a.defaultRegion
	}

	// 解析创建时间
	var creationTime time.Time
	if bucket.CreationDate != "" {
		if t, err := time.Parse(time.RFC3339, bucket.CreationDate); err == nil {
			creationTime = t
		} else if t, err := time.Parse("2006-01-02T15:04:05Z", bucket.CreationDate); err == nil {
			creationTime = t
		}
	}

	// 使用SDK返回的endpoint，如果没有则构造
	extranetEndpoint := bucket.ExtranetEndpoint
	if extranetEndpoint == "" {
		extranetEndpoint = fmt.Sprintf("%s.tos-%s.volces.com", bucket.Name, region)
	}
	intranetEndpoint := bucket.IntranetEndpoint
	if intranetEndpoint == "" {
		intranetEndpoint = fmt.Sprintf("%s.tos-%s.ivolces.com", bucket.Name, region)
	}

	return types.OSSBucket{
		BucketName:       bucket.Name,
		Region:           region,
		Location:         bucket.Location,
		CreationTime:     creationTime,
		ExtranetEndpoint: extranetEndpoint,
		IntranetEndpoint: intranetEndpoint,
		Provider:         "volcano",
	}
}

// convertGrantsToACL 将权限列表转换为ACL字符串
func (a *TOSAdapter) convertGrantsToACL(grants []tos.GrantV2) string {
	// 简化处理，根据权限判断ACL类型
	hasPublicRead := false
	hasPublicWrite := false

	for _, grant := range grants {
		if grant.GranteeV2.Canned == enum.CannedAllUsers {
			if grant.Permission == enum.PermissionRead {
				hasPublicRead = true
			}
			if grant.Permission == enum.PermissionWrite {
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

// hasPrefix 检查字符串是否有指定前缀
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// containsString 检查切片是否包含指定字符串
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
