package aws

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// s3Object S3 对象清单条目。
type s3Object struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// s3Client 按 region 构造 S3 client(静态凭证)。
func s3Client(account *domain.CloudAccount, region string) (*s3.Client, error) {
	if account.AccessKeyID == "" || account.AccessKeySecret == "" {
		return nil, fmt.Errorf("aws logquery: account %d missing credentials", account.ID)
	}
	cfg, err := awscfg.LoadDefaultConfig(context.Background(),
		awscfg.WithRegion(region),
		awscfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			account.AccessKeyID, account.AccessKeySecret, "")))
	if err != nil {
		return nil, fmt.Errorf("aws logquery: load config: %w", err)
	}
	return s3.NewFromConfig(cfg), nil
}

// resolveBucketRegion 探测桶真实 region(HeadBucket 跨区访问返回 301,
// 响应头 x-amz-bucket-region 携带真实区域;Go SDK v2 不像 boto3 自动跟随)。
// 同区/探测失败返回空串(调用方回退 hint region)。
func resolveBucketRegion(ctx context.Context, c *s3.Client, bucket string) string {
	_, err := c.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &bucket})
	if err == nil {
		return "" // 同区,无需纠正
	}
	return bucketRegionFromError(err)
}

// bucketRegionFromError 从错误链提取真实 region(smithyhttp.ResponseError
// 携带原始 *http.Response,x-amz-bucket-region 头由 301 响应返回)。
func bucketRegionFromError(err error) string {
	var re *smithyhttp.ResponseError
	if errors.As(err, &re) && re.Response != nil {
		return re.Response.Header.Get("x-amz-bucket-region")
	}
	return ""
}

// listObjects 列举前缀下对象(自动分页,maxKeys 保护)。
// startAfter 非空时从该 key 起列举(S3 按键字典序;键内嵌日期时=时间序,
// 用于跳过历史数据,避免 maxKeys 截断只拿到最旧对象)。
func listObjects(ctx context.Context, c *s3.Client, bucket, prefix, startAfter string, maxKeys int) ([]s3Object, error) {
	var out []s3Object
	in := &s3.ListObjectsV2Input{
		Bucket: &bucket,
		Prefix: &prefix,
	}
	if startAfter != "" {
		in.StartAfter = &startAfter
	}
	p := s3.NewListObjectsV2Paginator(c, in)
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3 list %s/%s: %w", bucket, prefix, err)
		}
		for _, o := range page.Contents {
			sz := int64(0)
			if o.Size != nil {
				sz = *o.Size
			}
			out = append(out, s3Object{Key: *o.Key, Size: sz, LastModified: *o.LastModified})
		}
		if len(out) >= maxKeys {
			out = out[:maxKeys]
			break
		}
	}
	// 最新优先
	sort.Slice(out, func(i, j int) bool { return out[i].LastModified.After(out[j].LastModified) })
	return out, nil
}

// s3KeyDateFloor 窗口起点(含投递回看)对应的键内嵌日期段。
// WAF 布局 <acl>/YYYY/MM/DD/HH/...;CloudFront 布局 <dist>.YYYY-MM-DD-HH.<hash>.gz。
func s3KeyDateFloor(t time.Time) string {
	return t.UTC().Format("2006/01/02/15")
}

// listCommonPrefixes 列举前缀下一级"目录"(Delimiter=/)。
func listCommonPrefixes(ctx context.Context, c *s3.Client, bucket, prefix string) ([]string, error) {
	var out []string
	p := s3.NewListObjectsV2Paginator(c, &s3.ListObjectsV2Input{
		Bucket:    &bucket,
		Prefix:    &prefix,
		Delimiter: strPtr("/"),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3 list prefixes %s/%s: %w", bucket, prefix, err)
		}
		for _, cp := range page.CommonPrefixes {
			out = append(out, *cp.Prefix)
		}
	}
	return out, nil
}

// getObjectBytes 下载对象;gz 自动解压;sizeCap 超限截断保护。
func getObjectBytes(ctx context.Context, c *s3.Client, bucket, key string, sizeCap int64) ([]byte, error) {
	resp, err := c.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &key})
	if err != nil {
		return nil, fmt.Errorf("s3 get %s/%s: %w", bucket, key, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var reader io.Reader = resp.Body
	if sizeCap > 0 {
		reader = io.LimitReader(resp.Body, sizeCap)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("s3 read %s/%s: %w", bucket, key, err)
	}
	if len(key) > 3 && key[len(key)-3:] == ".gz" {
		gz, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("gzip %s/%s: %w", bucket, key, err)
		}
		defer func() { _ = gz.Close() }()
		body, err = io.ReadAll(io.LimitReader(gz, sizeCap*4)) // 压缩比 ~4x 保护
		if err != nil {
			return nil, fmt.Errorf("gzip read %s/%s: %w", bucket, key, err)
		}
	}
	return body, nil
}

// walkPrefixes 逐级下钻 delimiter 目录(用于 WAFLogs/<acct>/WAFLogs/cloudfront/<acl>
// 四级布局解析,不硬编码账号 ID)。
func walkPrefixes(ctx context.Context, c *s3.Client, bucket, root string, depth int) ([]string, error) {
	current := []string{root}
	for i := 0; i < depth; i++ {
		var next []string
		for _, p := range current {
			prefixes, err := listCommonPrefixes(ctx, c, bucket, p)
			if err != nil {
				return nil, err
			}
			next = append(next, prefixes...)
		}
		if len(next) == 0 {
			return nil, nil
		}
		current = next
	}
	return current, nil
}

func strPtr(s string) *string { return &s }
