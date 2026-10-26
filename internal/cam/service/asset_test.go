package service

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cloudx-sdk/asset"
	"github.com/gotomicro/ego/core/elog"
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

// AggregateDailyCost 模拟按账期成本聚合（承接 GetCostAnalysis 的账期趋势逻辑）
func (m *MockAssetRepository) AggregateDailyCost(ctx context.Context, tenantID int64, provider string, days int) ([]dao.DailyCostRow, error) {
	args := m.Called(ctx, tenantID, provider, days)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]dao.DailyCostRow), args.Error(1)
}

// MockDashboardService 模拟仪表盘服务（GetAssetStatistics 复用其 ecam_instance 聚合）
type MockDashboardService struct {
	mock.Mock
}

func (m *MockDashboardService) GetOverview(ctx context.Context, tenantID int64) (*DashboardOverview, error) {
	args := m.Called(ctx, tenantID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*DashboardOverview), args.Error(1)
}

func (m *MockDashboardService) CountByProvider(ctx context.Context, tenantID int64) ([]dao.GroupCount, error) {
	args := m.Called(ctx, tenantID)
	return args.Get(0).([]dao.GroupCount), args.Error(1)
}

func (m *MockDashboardService) CountByRegion(ctx context.Context, tenantID int64) ([]dao.GroupCount, error) {
	args := m.Called(ctx, tenantID)
	return args.Get(0).([]dao.GroupCount), args.Error(1)
}

func (m *MockDashboardService) CountByAssetType(ctx context.Context, tenantID int64) ([]dao.GroupCount, error) {
	args := m.Called(ctx, tenantID)
	return args.Get(0).([]dao.GroupCount), args.Error(1)
}

func (m *MockDashboardService) CountByAccount(ctx context.Context, tenantID int64) ([]dao.GroupCount, error) {
	args := m.Called(ctx, tenantID)
	return args.Get(0).([]dao.GroupCount), args.Error(1)
}

func (m *MockDashboardService) GetExpiringResources(ctx context.Context, tenantID int64, days int, offset, limit int64) ([]dao.Instance, int64, error) {
	args := m.Called(ctx, tenantID, days, offset, limit)
	return args.Get(0).([]dao.Instance), args.Get(1).(int64), args.Error(2)
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

			s := NewService(mockRepo, mockAccountRepo, nil, adapterFactory, nil)
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

	s := NewService(mockRepo, mockAccountRepo, nil, adapterFactory, nil)
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

			s := NewService(mockRepo, mockAccountRepo, nil, adapterFactory, nil)
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

// TestService_GetAssetStatistics tests getting asset statistics.
// 表驱动：覆盖「多厂商分布」「空表降级」「总览聚合错误」「地域聚合错误」四种口径。
// 数据源为 DashboardService（ecam_instance），不再走 AssetRepository.AggregateStats。
func TestService_GetAssetStatistics(t *testing.T) {
	tests := []struct {
		name         string
		tenantID     int64
		overview     *DashboardOverview
		overviewErr  error
		byRegion     []dao.GroupCount
		regionErr    error
		wantErr      bool
		wantTotal    int64
		wantType     map[string]int64
		wantProvider map[string]int64
		wantRegion   map[string]int64
	}{
		{
			name:     "多厂商分布 - ecam_instance 真实口径",
			tenantID: 3,
			overview: &DashboardOverview{
				Total: 36000,
				ByProvider: []dao.GroupCount{
					{Key: "aliyun", Count: 20345},
					{Key: "volcengine", Count: 6575},
					{Key: "tencent", Count: 4509},
					{Key: "aws", Count: 2774},
					{Key: "huawei", Count: 1780},
				},
				ByType: []dao.GroupCount{
					{Key: "aliyun_ecs", Count: 12000},
					{Key: "volcano_ecs", Count: 8000},
					{Key: "aliyun_oss", Count: 5000},
					{Key: "aws_cdn", Count: 1000},
				},
				ByStatus: []dao.GroupCount{
					{Key: "Running", Count: 34000},
					{Key: "Stopped", Count: 2000},
				},
			},
			byRegion: []dao.GroupCount{
				{Key: "cn-shenzhen", Count: 12000},
				{Key: "eu-central-1", Count: 8000},
			},
			wantTotal: 36000,
			// model_uid 前缀剥离后跨厂商合并：ecs = aliyun_ecs + volcano_ecs
			wantType: map[string]int64{"ecs": 20000, "oss": 5000, "cdn": 1000},
			wantProvider: map[string]int64{
				"aliyun": 20345, "volcengine": 6575, "tencent": 4509, "aws": 2774, "huawei": 1780,
			},
			wantRegion: map[string]int64{"cn-shenzhen": 12000, "eu-central-1": 8000},
		},
		{
			name:     "空表 - total=0 且各维空（前端环形图降级为 0 段）",
			tenantID: 3,
			overview: &DashboardOverview{
				Total:      0,
				ByProvider: []dao.GroupCount{},
				ByType:     []dao.GroupCount{},
				ByStatus:   []dao.GroupCount{},
			},
			byRegion:     []dao.GroupCount{},
			wantTotal:    0,
			wantType:     map[string]int64{},
			wantProvider: map[string]int64{},
			wantRegion:   map[string]int64{},
		},
		{
			name:        "总览聚合错误 - service 透传错误且不 panic",
			tenantID:    3,
			overviewErr: context.DeadlineExceeded,
			wantErr:     true,
		},
		{
			name:      "地域聚合错误 - service 透传错误且不 panic",
			tenantID:  3,
			overview:  &DashboardOverview{Total: 10, ByProvider: []dao.GroupCount{}, ByType: []dao.GroupCount{}, ByStatus: []dao.GroupCount{}},
			regionErr: context.DeadlineExceeded,
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockRepo := new(MockAssetRepository)
			mockAccountRepo := new(MockCloudAccountRepository)
			mockDashboard := new(MockDashboardService)
			adapterFactory := asset.NewAdapterFactory(nil)

			mockDashboard.On("GetOverview", mock.Anything, tc.tenantID).
				Return(tc.overview, tc.overviewErr)
			if tc.overviewErr == nil {
				// 总览成功才继续查地域
				mockDashboard.On("CountByRegion", mock.Anything, tc.tenantID).
					Return(tc.byRegion, tc.regionErr)
			}

			s := NewService(mockRepo, mockAccountRepo, mockDashboard, adapterFactory, elog.DefaultLogger)
			stats, err := s.GetAssetStatistics(context.Background(), tc.tenantID)

			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.wantTotal, stats.TotalAssets)
			assert.NotNil(t, stats.ProviderStats)
			assert.NotNil(t, stats.AssetTypeStats)
			assert.NotNil(t, stats.RegionStats)
			assert.NotNil(t, stats.StatusStats)
			if tc.wantType != nil {
				assert.Equal(t, tc.wantType, stats.AssetTypeStats)
			}
			if tc.wantProvider != nil {
				assert.Equal(t, tc.wantProvider, stats.ProviderStats)
			}
			if tc.wantRegion != nil {
				assert.Equal(t, tc.wantRegion, stats.RegionStats)
			}
			mockDashboard.AssertExpectations(t)
		})
	}
}

// TestMergeByAssetType 纯资产类型归并（表驱动）：
// model_uid 形如 "<provider>_<type>"，出图前剥离厂商前缀并跨厂商合并计数。
func TestMergeByAssetType(t *testing.T) {
	tests := []struct {
		name   string
		groups []dao.GroupCount
		want   map[string]int64
	}{
		{
			name: "跨厂商同类型合并",
			groups: []dao.GroupCount{
				{Key: "aliyun_ecs", Count: 12000},
				{Key: "volcano_ecs", Count: 8000},
				{Key: "aws_ecs", Count: 500},
			},
			want: map[string]int64{"ecs": 20500},
		},
		{
			name:   "无下划线前缀原样保留",
			groups: []dao.GroupCount{{Key: "ecs", Count: 7}},
			want:   map[string]int64{"ecs": 7},
		},
		{
			name:   "类型名自身含下划线时只剥离首个前缀",
			groups: []dao.GroupCount{{Key: "aliyun_security_group", Count: 42}},
			want:   map[string]int64{"security_group": 42},
		},
		{
			name:   "非厂商前缀（cloud_vpc）同样只剥离首段",
			groups: []dao.GroupCount{{Key: "cloud_vpc", Count: 3}},
			want:   map[string]int64{"vpc": 3},
		},
		{
			name:   "空 key 归 _empty",
			groups: []dao.GroupCount{{Key: "", Count: 5}},
			want:   map[string]int64{"_empty": 5},
		},
		{
			name:   "前缀后为空也归 _empty",
			groups: []dao.GroupCount{{Key: "aliyun_", Count: 2}},
			want:   map[string]int64{"_empty": 2},
		},
		{
			name:   "nil 输入返回空 map（非 nil）",
			groups: nil,
			want:   map[string]int64{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, mergeByAssetType(tc.groups))
		})
	}
}

// TestService_GetCostAnalysis 成本分析（表驱动：账期趋势 / 空表 / days 归一 / 聚合错误）。
// 明细表 billing_date 为账期（月初）粒度，days<=0 归一为 90 天以覆盖 3 个月度点。
func TestService_GetCostAnalysis(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  int64
		provider  string
		days      int
		rows      []dao.DailyCostRow
		rowsErr   error
		wantErr   bool
		wantLen   int
		wantTotal float64
		wantFirst DailyCost
	}{
		{
			name:     "正常账期趋势 - 3 个月度点",
			tenantID: 3,
			provider: "",
			days:     90,
			rows: []dao.DailyCostRow{
				{ID: "2026-08-01", AmountCNY: 2251246.39},
				{ID: "2026-09-01", AmountCNY: 2430946.12},
				{ID: "2026-10-01", AmountCNY: 790936.32},
			},
			wantLen:   3,
			wantTotal: 5473128.83,
			wantFirst: DailyCost{Date: "2026-08-01", Cost: 2251246.39},
		},
		{
			name:      "空表 - 无账单数据（sparkline 不渲染）",
			tenantID:  3,
			provider:  "",
			days:      90,
			rows:      []dao.DailyCostRow{},
			wantLen:   0,
			wantTotal: 0,
		},
		{
			name:      "days<=0 归一为 90（保证覆盖 3 个月度点）",
			tenantID:  3,
			provider:  "aliyun",
			days:      0,
			rows:      []dao.DailyCostRow{{ID: "2026-10-01", AmountCNY: 50}},
			wantLen:   1,
			wantTotal: 50,
			wantFirst: DailyCost{Date: "2026-10-01", Cost: 50},
		},
		{
			name:      "厂商过滤透传",
			tenantID:  3,
			provider:  "volcengine",
			days:      90,
			rows:      []dao.DailyCostRow{{ID: "2026-10-01", AmountCNY: 12.5}},
			wantLen:   1,
			wantTotal: 12.5,
			wantFirst: DailyCost{Date: "2026-10-01", Cost: 12.5},
		},
		{
			name:     "聚合错误 - service 透传错误且不 panic",
			tenantID: 3,
			rowsErr:  context.DeadlineExceeded,
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockRepo := new(MockAssetRepository)
			mockAccountRepo := new(MockCloudAccountRepository)
			adapterFactory := asset.NewAdapterFactory(nil)
			wantDays := tc.days
			if wantDays <= 0 {
				wantDays = 90
			}
			mockRepo.On("AggregateDailyCost", mock.Anything, tc.tenantID, tc.provider, wantDays).
				Return(tc.rows, tc.rowsErr)

			s := NewService(mockRepo, mockAccountRepo, nil, adapterFactory, elog.DefaultLogger)
			analysis, err := s.GetCostAnalysis(context.Background(), tc.tenantID, tc.provider, tc.days)

			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.provider, analysis.Provider)
			assert.Len(t, analysis.DailyCosts, tc.wantLen)
			assert.InDelta(t, tc.wantTotal, analysis.TotalCost, 0.001)
			if tc.wantLen > 0 {
				assert.Equal(t, tc.wantFirst, analysis.DailyCosts[0])
			}
			assert.NotNil(t, analysis.AssetCosts)
			assert.NotNil(t, analysis.RegionCosts)
			mockRepo.AssertExpectations(t)
		})
	}
}
