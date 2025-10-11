package service

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockAssetRepository 模拟资产仓储
type MockAssetRepository struct {
	mock.Mock
}

func (m *MockAssetRepository) CreateAsset(ctx context.Context, asset domain.CloudAsset) (int64, error) {
	args := m.Called(ctx, asset)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockAssetRepository) CreateMultiAssets(ctx context.Context, assets []domain.CloudAsset) (int64, error) {
	args := m.Called(ctx, assets)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockAssetRepository) UpdateAsset(ctx context.Context, asset domain.CloudAsset) error {
	args := m.Called(ctx, asset)
	return args.Error(0)
}

func (m *MockAssetRepository) GetAssetById(ctx context.Context, id int64) (domain.CloudAsset, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(domain.CloudAsset), args.Error(1)
}

func (m *MockAssetRepository) GetAssetByAssetId(ctx context.Context, assetId string) (domain.CloudAsset, error) {
	args := m.Called(ctx, assetId)
	return args.Get(0).(domain.CloudAsset), args.Error(1)
}

func (m *MockAssetRepository) ListAssets(ctx context.Context, filter domain.AssetFilter) ([]domain.CloudAsset, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).([]domain.CloudAsset), args.Error(1)
}

func (m *MockAssetRepository) CountAssets(ctx context.Context, filter domain.AssetFilter) (int64, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockAssetRepository) DeleteAsset(ctx context.Context, id int64) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// TestService_CreateAsset 测试创建资产
func TestService_CreateAsset(t *testing.T) {
	tests := []struct {
		name    string
		asset   domain.CloudAsset
		mockFn  func(*MockAssetRepository)
		want    int64
		wantErr bool
	}{
		{
			name: "创建资产成功",
			asset: domain.CloudAsset{
				AssetId:   "i-123456",
				AssetName: "test-ecs",
				AssetType: "ecs",
				Provider:  "aliyun",
				Region:    "cn-hangzhou",
				Status:    "running",
			},
			mockFn: func(repo *MockAssetRepository) {
				repo.On("GetAssetByAssetId", mock.Anything, "i-123456").
					Return(domain.CloudAsset{}, assert.AnError)
				repo.On("CreateAsset", mock.Anything, mock.AnythingOfType("domain.CloudAsset")).
					Return(int64(1), nil)
			},
			want:    1,
			wantErr: false,
		},
		{
			name: "资产已存在",
			asset: domain.CloudAsset{
				AssetId:   "i-123456",
				AssetName: "test-ecs",
				AssetType: "ecs",
				Provider:  "aliyun",
				Region:    "cn-hangzhou",
				Status:    "running",
			},
			mockFn: func(repo *MockAssetRepository) {
				repo.On("GetAssetByAssetId", mock.Anything, "i-123456").
					Return(domain.CloudAsset{Id: 1}, nil)
			},
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockAssetRepository)
			tt.mockFn(mockRepo)

			s := NewService(mockRepo)
			ctx := context.Background()

			got, err := s.CreateAsset(ctx, tt.asset)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
			mockRepo.AssertExpectations(t)
		})
	}
}

// TestService_ListAssets 测试获取资产列表
func TestService_ListAssets(t *testing.T) {
	tests := []struct {
		name       string
		filter     domain.AssetFilter
		mockFn     func(*MockAssetRepository)
		wantAssets []domain.CloudAsset
		wantTotal  int64
		wantErr    bool
	}{
		{
			name: "获取资产列表成功",
			filter: domain.AssetFilter{
				Provider: "aliyun",
				Limit:    10,
			},
			mockFn: func(repo *MockAssetRepository) {
				assets := []domain.CloudAsset{
					{
						Id:        1,
						AssetId:   "i-123456",
						AssetName: "test-ecs-1",
						AssetType: "ecs",
						Provider:  "aliyun",
						Region:    "cn-hangzhou",
						Status:    "running",
					},
					{
						Id:        2,
						AssetId:   "i-789012",
						AssetName: "test-ecs-2",
						AssetType: "ecs",
						Provider:  "aliyun",
						Region:    "cn-beijing",
						Status:    "stopped",
					},
				}
				repo.On("ListAssets", mock.Anything, mock.AnythingOfType("domain.AssetFilter")).
					Return(assets, nil)
				repo.On("CountAssets", mock.Anything, mock.AnythingOfType("domain.AssetFilter")).
					Return(int64(2), nil)
			},
			wantAssets: []domain.CloudAsset{
				{
					Id:        1,
					AssetId:   "i-123456",
					AssetName: "test-ecs-1",
					AssetType: "ecs",
					Provider:  "aliyun",
					Region:    "cn-hangzhou",
					Status:    "running",
				},
				{
					Id:        2,
					AssetId:   "i-789012",
					AssetName: "test-ecs-2",
					AssetType: "ecs",
					Provider:  "aliyun",
					Region:    "cn-beijing",
					Status:    "stopped",
				},
			},
			wantTotal: 2,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockAssetRepository)
			tt.mockFn(mockRepo)

			s := NewService(mockRepo)
			ctx := context.Background()

			gotAssets, gotTotal, err := s.ListAssets(ctx, tt.filter)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.wantAssets, gotAssets)
			assert.Equal(t, tt.wantTotal, gotTotal)
			mockRepo.AssertExpectations(t)
		})
	}
}

// TestService_UpdateAsset 测试更新资产
func TestService_UpdateAsset(t *testing.T) {
	tests := []struct {
		name    string
		asset   domain.CloudAsset
		mockFn  func(*MockAssetRepository)
		wantErr bool
	}{
		{
			name: "更新资产成功",
			asset: domain.CloudAsset{
				Id:        1,
				AssetId:   "i-123456",
				AssetName: "updated-ecs",
				AssetType: "ecs",
				Provider:  "aliyun",
				Region:    "cn-hangzhou",
				Status:    "running",
			},
			mockFn: func(repo *MockAssetRepository) {
				existingAsset := domain.CloudAsset{
					Id:        1,
					AssetId:   "i-123456",
					AssetName: "test-ecs",
					AssetType: "ecs",
					Provider:  "aliyun",
					Region:    "cn-hangzhou",
					Status:    "running",
				}
				repo.On("GetAssetById", mock.Anything, int64(1)).
					Return(existingAsset, nil)
				repo.On("UpdateAsset", mock.Anything, mock.AnythingOfType("domain.CloudAsset")).
					Return(nil)
			},
			wantErr: false,
		},
		{
			name: "资产不存在",
			asset: domain.CloudAsset{
				Id: 999,
			},
			mockFn: func(repo *MockAssetRepository) {
				repo.On("GetAssetById", mock.Anything, int64(999)).
					Return(domain.CloudAsset{}, assert.AnError)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockAssetRepository)
			tt.mockFn(mockRepo)

			s := NewService(mockRepo)
			ctx := context.Background()

			err := s.UpdateAsset(ctx, tt.asset)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			mockRepo.AssertExpectations(t)
		})
	}
}

// TestService_GetAssetStatistics 测试获取资产统计
func TestService_GetAssetStatistics(t *testing.T) {
	mockRepo := new(MockAssetRepository)
	mockRepo.On("CountAssets", mock.Anything, domain.AssetFilter{}).
		Return(int64(100), nil)

	s := NewService(mockRepo)
	ctx := context.Background()

	stats, err := s.GetAssetStatistics(ctx)

	assert.NoError(t, err)
	assert.Equal(t, int64(100), stats.TotalAssets)
	assert.NotNil(t, stats.ProviderStats)
	assert.NotNil(t, stats.AssetTypeStats)
	assert.NotNil(t, stats.RegionStats)
	assert.NotNil(t, stats.StatusStats)
	mockRepo.AssertExpectations(t)
}
