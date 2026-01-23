package service

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/asset"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockAssetRepository 模拟资产仓储
type MockAssetRepository struct {
	mock.Mock
}

func (m *MockAssetRepository) CreateAsset(ctx context.Context, a domain.CloudAsset) (int64, error) {
	args := m.Called(ctx, a)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockAssetRepository) CreateMultiAssets(ctx context.Context, assets []domain.CloudAsset) (int64, error) {
	args := m.Called(ctx, assets)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockAssetRepository) UpdateAsset(ctx context.Context, a domain.CloudAsset) error {
	args := m.Called(ctx, a)
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

// TestService_CreateAsset tests asset creation
func TestService_CreateAsset(t *testing.T) {
	tests := []struct {
		name    string
		asset   domain.CloudAsset
		mockFn  func(*MockAssetRepository)
		want    int64
		wantErr bool
	}{
		{
			name: "create asset success",
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
			name: "asset already exists",
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
			mockAccountRepo := new(MockCloudAccountRepository)
			adapterFactory := asset.NewAdapterFactory(nil)
			tt.mockFn(mockRepo)

			s := NewService(mockRepo, mockAccountRepo, adapterFactory, nil)
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

// TestService_ListAssets tests listing assets
func TestService_ListAssets(t *testing.T) {
	mockRepo := new(MockAssetRepository)
	mockAccountRepo := new(MockCloudAccountRepository)
	adapterFactory := asset.NewAdapterFactory(nil)

	assets := []domain.CloudAsset{
		{Id: 1, AssetId: "i-123456", AssetName: "test-ecs-1", AssetType: "ecs", Provider: "aliyun", Region: "cn-hangzhou", Status: "running"},
		{Id: 2, AssetId: "i-789012", AssetName: "test-ecs-2", AssetType: "ecs", Provider: "aliyun", Region: "cn-beijing", Status: "stopped"},
	}

	mockRepo.On("ListAssets", mock.Anything, mock.AnythingOfType("domain.AssetFilter")).Return(assets, nil)
	mockRepo.On("CountAssets", mock.Anything, mock.AnythingOfType("domain.AssetFilter")).Return(int64(2), nil)

	s := NewService(mockRepo, mockAccountRepo, adapterFactory, nil)
	ctx := context.Background()

	filter := domain.AssetFilter{Provider: "aliyun", Limit: 10}
	gotAssets, gotTotal, err := s.ListAssets(ctx, filter)

	assert.NoError(t, err)
	assert.Equal(t, assets, gotAssets)
	assert.Equal(t, int64(2), gotTotal)
	mockRepo.AssertExpectations(t)
}

// TestService_UpdateAsset tests updating an asset
func TestService_UpdateAsset(t *testing.T) {
	tests := []struct {
		name    string
		asset   domain.CloudAsset
		mockFn  func(*MockAssetRepository)
		wantErr bool
	}{
		{
			name: "update asset success",
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
				existing := domain.CloudAsset{Id: 1, AssetId: "i-123456", AssetName: "test-ecs"}
				repo.On("GetAssetById", mock.Anything, int64(1)).Return(existing, nil)
				repo.On("UpdateAsset", mock.Anything, mock.AnythingOfType("domain.CloudAsset")).Return(nil)
			},
			wantErr: false,
		},
		{
			name:  "asset not found",
			asset: domain.CloudAsset{Id: 999},
			mockFn: func(repo *MockAssetRepository) {
				repo.On("GetAssetById", mock.Anything, int64(999)).Return(domain.CloudAsset{}, assert.AnError)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockAssetRepository)
			mockAccountRepo := new(MockCloudAccountRepository)
			adapterFactory := asset.NewAdapterFactory(nil)
			tt.mockFn(mockRepo)

			s := NewService(mockRepo, mockAccountRepo, adapterFactory, nil)
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

// TestService_GetAssetStatistics tests getting asset statistics
func TestService_GetAssetStatistics(t *testing.T) {
	mockRepo := new(MockAssetRepository)
	mockAccountRepo := new(MockCloudAccountRepository)
	adapterFactory := asset.NewAdapterFactory(nil)
	mockRepo.On("CountAssets", mock.Anything, domain.AssetFilter{}).Return(int64(100), nil)

	s := NewService(mockRepo, mockAccountRepo, adapterFactory, nil)
	ctx := context.Background()

	stats, err := s.GetAssetStatistics(ctx)

	assert.NoError(t, err)
	assert.Equal(t, int64(100), stats.TotalAssets)
	assert.NotNil(t, stats.ProviderStats)
	assert.NotNil(t, stats.AssetTypeStats)
	mockRepo.AssertExpectations(t)
}
