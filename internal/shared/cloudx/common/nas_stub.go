package common

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
)

// NASStubAdapter NAS 存根适配器 (未实现)
type NASStubAdapter struct {
	Provider string
}

// NewNASStubAdapter 创建 NAS 存根适配器
func NewNASStubAdapter(provider string) *NASStubAdapter {
	return &NASStubAdapter{Provider: provider}
}

func (a *NASStubAdapter) ListInstances(ctx context.Context, region string) ([]types.NASInstance, error) {
	return nil, fmt.Errorf("%s NAS adapter not implemented", a.Provider)
}

func (a *NASStubAdapter) GetInstance(ctx context.Context, region, fileSystemID string) (*types.NASInstance, error) {
	return nil, fmt.Errorf("%s NAS adapter not implemented", a.Provider)
}

func (a *NASStubAdapter) ListInstancesByIDs(ctx context.Context, region string, fileSystemIDs []string) ([]types.NASInstance, error) {
	return nil, fmt.Errorf("%s NAS adapter not implemented", a.Provider)
}

func (a *NASStubAdapter) GetInstanceStatus(ctx context.Context, region, fileSystemID string) (string, error) {
	return "", fmt.Errorf("%s NAS adapter not implemented", a.Provider)
}

func (a *NASStubAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.NASInstanceFilter) ([]types.NASInstance, error) {
	return nil, fmt.Errorf("%s NAS adapter not implemented", a.Provider)
}
