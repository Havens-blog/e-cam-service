package common

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
)

// OSSStubAdapter OSS 存根适配器 (未实现)
type OSSStubAdapter struct {
	Provider string
}

// NewOSSStubAdapter 创建 OSS 存根适配器
func NewOSSStubAdapter(provider string) *OSSStubAdapter {
	return &OSSStubAdapter{Provider: provider}
}

func (a *OSSStubAdapter) ListBuckets(ctx context.Context, region string) ([]types.OSSBucket, error) {
	return nil, fmt.Errorf("%s OSS adapter not implemented", a.Provider)
}

func (a *OSSStubAdapter) GetBucket(ctx context.Context, bucketName string) (*types.OSSBucket, error) {
	return nil, fmt.Errorf("%s OSS adapter not implemented", a.Provider)
}

func (a *OSSStubAdapter) GetBucketStats(ctx context.Context, bucketName string) (*types.OSSBucketStats, error) {
	return nil, fmt.Errorf("%s OSS adapter not implemented", a.Provider)
}

func (a *OSSStubAdapter) ListBucketsWithFilter(ctx context.Context, region string, filter *types.OSSBucketFilter) ([]types.OSSBucket, error) {
	return nil, fmt.Errorf("%s OSS adapter not implemented", a.Provider)
}
