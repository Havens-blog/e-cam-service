package aws

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/gotomicro/ego/core/elog"
)

// S3Adapter AWS S3 适配器
type S3Adapter struct {
	accessKeyID     string
	accessKeySecret string
	defaultRegion   string
	logger          *elog.Component
}

// NewS3Adapter 创建 S3 适配器
func NewS3Adapter(accessKeyID, accessKeySecret, defaultRegion string, logger *elog.Component) *S3Adapter {
	return &S3Adapter{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		defaultRegion:   defaultRegion,
		logger:          logger,
	}
}

// createClient 创建 S3 客户端
func (a *S3Adapter) createClient(ctx context.Context, region string) (*s3.Client, error) {
	if region == "" {
		region = a.defaultRegion
	}

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			a.accessKeyID,
			a.accessKeySecret,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("加载AWS配置失败: %w", err)
	}

	return s3.NewFromConfig(cfg), nil
}

// ListBuckets 获取存储桶列表
func (a *S3Adapter) ListBuckets(ctx context.Context, region string) ([]types.OSSBucket, error) {
	client, err := a.createClient(ctx, region)
	if err != nil {
		return nil, err
	}

	output, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf("获取S3存储桶列表失败: %w", err)
	}

	var buckets []types.OSSBucket
	for _, bucket := range output.Buckets {
		ossBucket := a.convertToOSSBucket(&bucket)

		// 获取 bucket 的 region
		bucketRegion, err := a.getBucketRegion(ctx, client, *bucket.Name)
		if err == nil {
			ossBucket.Region = bucketRegion
			ossBucket.Location = bucketRegion
		}

		// 如果指定了 region，只返回该 region 的 bucket
		if region != "" && ossBucket.Region != region {
			continue
		}

		// 获取 bucket 统计信息（存储大小和对象数量）
		stats, err := a.GetBucketStats(ctx, *bucket.Name)
		if err == nil && stats != nil {
			ossBucket.ObjectCount = stats.ObjectCount
			ossBucket.StorageSize = stats.StorageSize
		} else {
			a.logger.Warn("获取S3存储桶统计信息失败",
				elog.String("bucket", *bucket.Name),
				elog.FieldErr(err))
		}

		buckets = append(buckets, ossBucket)
	}

	return buckets, nil
}

// getBucketRegion 获取 bucket 所在的 region
func (a *S3Adapter) getBucketRegion(ctx context.Context, client *s3.Client, bucketName string) (string, error) {
	output, err := client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		return "", err
	}

	// 空字符串表示 us-east-1
	if output.LocationConstraint == "" {
		return "us-east-1", nil
	}
	return string(output.LocationConstraint), nil
}

// GetBucket 获取单个存储桶详情
func (a *S3Adapter) GetBucket(ctx context.Context, bucketName string) (*types.OSSBucket, error) {
	client, err := a.createClient(ctx, "")
	if err != nil {
		return nil, err
	}

	// 获取 bucket region
	bucketRegion, err := a.getBucketRegion(ctx, client, bucketName)
	if err != nil {
		return nil, fmt.Errorf("获取存储桶region失败: %w", err)
	}

	// 使用正确的 region 创建客户端
	regionClient, err := a.createClient(ctx, bucketRegion)
	if err != nil {
		return nil, err
	}

	bucket := &types.OSSBucket{
		BucketName: bucketName,
		Region:     bucketRegion,
		Location:   bucketRegion,
		Provider:   string(domain.CloudProviderAWS),
	}

	// 获取 ACL
	aclOutput, err := regionClient.GetBucketAcl(ctx, &s3.GetBucketAclInput{
		Bucket: aws.String(bucketName),
	})
	if err == nil {
		bucket.ACL = a.parseACL(aclOutput)
	}

	// 获取版本控制状态
	versionOutput, err := regionClient.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucketName),
	})
	if err == nil {
		bucket.Versioning = string(versionOutput.Status)
	}

	// 获取加密配置
	encryptOutput, err := regionClient.GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{
		Bucket: aws.String(bucketName),
	})
	if err == nil && encryptOutput.ServerSideEncryptionConfiguration != nil {
		for _, rule := range encryptOutput.ServerSideEncryptionConfiguration.Rules {
			if rule.ApplyServerSideEncryptionByDefault != nil {
				bucket.ServerSideEncryption = string(rule.ApplyServerSideEncryptionByDefault.SSEAlgorithm)
				if rule.ApplyServerSideEncryptionByDefault.KMSMasterKeyID != nil {
					bucket.KMSKeyID = *rule.ApplyServerSideEncryptionByDefault.KMSMasterKeyID
				}
			}
		}
	}

	// 设置 endpoint
	bucket.ExtranetEndpoint = fmt.Sprintf("%s.s3.%s.amazonaws.com", bucketName, bucketRegion)
	bucket.IntranetEndpoint = fmt.Sprintf("%s.s3.%s.amazonaws.com", bucketName, bucketRegion)

	return bucket, nil
}

// parseACL 解析 ACL
func (a *S3Adapter) parseACL(output *s3.GetBucketAclOutput) string {
	if output == nil {
		return "private"
	}

	hasPublicRead := false
	hasPublicWrite := false

	for _, grant := range output.Grants {
		if grant.Grantee != nil && grant.Grantee.URI != nil {
			uri := *grant.Grantee.URI
			if uri == "http://acs.amazonaws.com/groups/global/AllUsers" {
				if grant.Permission == s3types.PermissionRead {
					hasPublicRead = true
				}
				if grant.Permission == s3types.PermissionWrite {
					hasPublicWrite = true
				}
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
func (a *S3Adapter) GetBucketStats(ctx context.Context, bucketName string) (*types.OSSBucketStats, error) {
	// S3 没有直接的 API 获取统计信息，需要通过 CloudWatch 或遍历对象
	// 这里返回基本信息
	return &types.OSSBucketStats{
		BucketName: bucketName,
	}, nil
}

// ListBucketsWithFilter 带过滤条件获取存储桶列表
func (a *S3Adapter) ListBucketsWithFilter(ctx context.Context, region string, filter *types.OSSBucketFilter) ([]types.OSSBucket, error) {
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
func (a *S3Adapter) convertToOSSBucket(bucket *s3types.Bucket) types.OSSBucket {
	ossBucket := types.OSSBucket{
		Provider:     string(domain.CloudProviderAWS),
		StorageClass: "STANDARD", // S3 默认存储类型
	}

	if bucket.Name != nil {
		ossBucket.BucketName = *bucket.Name
	}
	if bucket.CreationDate != nil {
		ossBucket.CreationTime = *bucket.CreationDate
	}

	return ossBucket
}
