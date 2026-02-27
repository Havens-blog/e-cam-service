package common

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
)

// StubSecurityGroupAdapter 安全组适配器空实现
type StubSecurityGroupAdapter struct {
	Provider string
}

func (s *StubSecurityGroupAdapter) ListInstances(ctx context.Context, region string) ([]types.SecurityGroupInstance, error) {
	return nil, fmt.Errorf("%s 安全组适配器暂未实现", s.Provider)
}

func (s *StubSecurityGroupAdapter) GetInstance(ctx context.Context, region, securityGroupID string) (*types.SecurityGroupInstance, error) {
	return nil, fmt.Errorf("%s 安全组适配器暂未实现", s.Provider)
}

func (s *StubSecurityGroupAdapter) ListInstancesByIDs(ctx context.Context, region string, securityGroupIDs []string) ([]types.SecurityGroupInstance, error) {
	return nil, fmt.Errorf("%s 安全组适配器暂未实现", s.Provider)
}

func (s *StubSecurityGroupAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.SecurityGroupFilter) ([]types.SecurityGroupInstance, error) {
	return nil, fmt.Errorf("%s 安全组适配器暂未实现", s.Provider)
}

func (s *StubSecurityGroupAdapter) GetSecurityGroupRules(ctx context.Context, region, securityGroupID string) ([]types.SecurityGroupRule, error) {
	return nil, fmt.Errorf("%s 安全组适配器暂未实现", s.Provider)
}

func (s *StubSecurityGroupAdapter) ListByInstanceID(ctx context.Context, region, instanceID string) ([]types.SecurityGroupInstance, error) {
	return nil, fmt.Errorf("%s 安全组适配器暂未实现", s.Provider)
}

// StubImageAdapter 镜像适配器空实现
type StubImageAdapter struct {
	Provider string
}

func (s *StubImageAdapter) ListInstances(ctx context.Context, region string) ([]types.ImageInstance, error) {
	return nil, fmt.Errorf("%s 镜像适配器暂未实现", s.Provider)
}

func (s *StubImageAdapter) GetInstance(ctx context.Context, region, imageID string) (*types.ImageInstance, error) {
	return nil, fmt.Errorf("%s 镜像适配器暂未实现", s.Provider)
}

func (s *StubImageAdapter) ListInstancesByIDs(ctx context.Context, region string, imageIDs []string) ([]types.ImageInstance, error) {
	return nil, fmt.Errorf("%s 镜像适配器暂未实现", s.Provider)
}

func (s *StubImageAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.ImageFilter) ([]types.ImageInstance, error) {
	return nil, fmt.Errorf("%s 镜像适配器暂未实现", s.Provider)
}

// StubDiskAdapter 云盘适配器空实现
type StubDiskAdapter struct {
	Provider string
}

func (s *StubDiskAdapter) ListInstances(ctx context.Context, region string) ([]types.DiskInstance, error) {
	return nil, fmt.Errorf("%s 云盘适配器暂未实现", s.Provider)
}

func (s *StubDiskAdapter) GetInstance(ctx context.Context, region, diskID string) (*types.DiskInstance, error) {
	return nil, fmt.Errorf("%s 云盘适配器暂未实现", s.Provider)
}

func (s *StubDiskAdapter) ListInstancesByIDs(ctx context.Context, region string, diskIDs []string) ([]types.DiskInstance, error) {
	return nil, fmt.Errorf("%s 云盘适配器暂未实现", s.Provider)
}

func (s *StubDiskAdapter) GetInstanceStatus(ctx context.Context, region, diskID string) (string, error) {
	return "", fmt.Errorf("%s 云盘适配器暂未实现", s.Provider)
}

func (s *StubDiskAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.DiskFilter) ([]types.DiskInstance, error) {
	return nil, fmt.Errorf("%s 云盘适配器暂未实现", s.Provider)
}

func (s *StubDiskAdapter) ListByInstanceID(ctx context.Context, region, instanceID string) ([]types.DiskInstance, error) {
	return nil, fmt.Errorf("%s 云盘适配器暂未实现", s.Provider)
}

// StubSnapshotAdapter 快照适配器空实现
type StubSnapshotAdapter struct {
	Provider string
}

func (s *StubSnapshotAdapter) ListInstances(ctx context.Context, region string) ([]types.SnapshotInstance, error) {
	return nil, fmt.Errorf("%s 快照适配器暂未实现", s.Provider)
}

func (s *StubSnapshotAdapter) GetInstance(ctx context.Context, region, snapshotID string) (*types.SnapshotInstance, error) {
	return nil, fmt.Errorf("%s 快照适配器暂未实现", s.Provider)
}

func (s *StubSnapshotAdapter) ListInstancesByIDs(ctx context.Context, region string, snapshotIDs []string) ([]types.SnapshotInstance, error) {
	return nil, fmt.Errorf("%s 快照适配器暂未实现", s.Provider)
}

func (s *StubSnapshotAdapter) GetInstanceStatus(ctx context.Context, region, snapshotID string) (string, error) {
	return "", fmt.Errorf("%s 快照适配器暂未实现", s.Provider)
}

func (s *StubSnapshotAdapter) ListInstancesWithFilter(ctx context.Context, region string, filter *types.SnapshotFilter) ([]types.SnapshotInstance, error) {
	return nil, fmt.Errorf("%s 快照适配器暂未实现", s.Provider)
}

func (s *StubSnapshotAdapter) ListByDiskID(ctx context.Context, region, diskID string) ([]types.SnapshotInstance, error) {
	return nil, fmt.Errorf("%s 快照适配器暂未实现", s.Provider)
}

func (s *StubSnapshotAdapter) ListByInstanceID(ctx context.Context, region, instanceID string) ([]types.SnapshotInstance, error) {
	return nil, fmt.Errorf("%s 快照适配器暂未实现", s.Provider)
}
