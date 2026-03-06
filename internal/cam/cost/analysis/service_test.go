package analysis

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBillDAO 用于测试的 BillDAO mock
type mockBillDAO struct {
	sumAmountFn        func(ctx context.Context, filter repository.UnifiedBillFilter) (float64, error)
	aggregateByFieldFn func(ctx context.Context, tenantID string, field string, startDate, endDate string) ([]repository.AggregateResult, error)
	aggregateDailyFn   func(ctx context.Context, tenantID string, startDate, endDate string, filter repository.UnifiedBillFilter) ([]repository.DailyAmount, error)
}

func (m *mockBillDAO) SumAmount(ctx context.Context, filter repository.UnifiedBillFilter) (float64, error) {
	if m.sumAmountFn != nil {
		return m.sumAmountFn(ctx, filter)
	}
	return 0, nil
}

func (m *mockBillDAO) AggregateByField(ctx context.Context, tenantID string, field string, startDate, endDate string) ([]repository.AggregateResult, error) {
	if m.aggregateByFieldFn != nil {
		return m.aggregateByFieldFn(ctx, tenantID, field, startDate, endDate)
	}
	return nil, nil
}

func (m *mockBillDAO) AggregateDailyAmount(ctx context.Context, tenantID string, startDate, endDate string, filter repository.UnifiedBillFilter) ([]repository.DailyAmount, error) {
	if m.aggregateDailyFn != nil {
		return m.aggregateDailyFn(ctx, tenantID, startDate, endDate, filter)
	}
	return nil, nil
}

// --- BillDAO interface stubs (unused by CostService) ---

func (m *mockBillDAO) InsertRawBill(_ context.Context, _ domain.RawBillRecord) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) InsertRawBills(_ context.Context, _ []domain.RawBillRecord) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) GetRawBillByID(_ context.Context, _ int64) (domain.RawBillRecord, error) {
	return domain.RawBillRecord{}, nil
}
func (m *mockBillDAO) ListRawBills(_ context.Context, _ int64, _, _ string) ([]domain.RawBillRecord, error) {
	return nil, nil
}
func (m *mockBillDAO) ListRawBillsByCollectID(_ context.Context, _ string) ([]domain.RawBillRecord, error) {
	return nil, nil
}
func (m *mockBillDAO) InsertUnifiedBill(_ context.Context, _ domain.UnifiedBill) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) InsertUnifiedBills(_ context.Context, _ []domain.UnifiedBill) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) GetUnifiedBillByID(_ context.Context, _ int64) (domain.UnifiedBill, error) {
	return domain.UnifiedBill{}, nil
}
func (m *mockBillDAO) ListUnifiedBills(_ context.Context, _ repository.UnifiedBillFilter) ([]domain.UnifiedBill, error) {
	return nil, nil
}
func (m *mockBillDAO) CountUnifiedBills(_ context.Context, _ repository.UnifiedBillFilter) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) DeleteUnifiedBillsByPeriod(_ context.Context, _, _ string) error { return nil }
func (m *mockBillDAO) DeleteRawBillsByAccountAndRange(_ context.Context, _ int64, _, _ string) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) DeleteUnifiedBillsByAccountAndRange(_ context.Context, _ int64, _, _ string) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) AggregateByTag(_ context.Context, _ string, _, _ string) ([]repository.AggregateResult, error) {
	return nil, nil
}

// --- Test helpers ---

func setupTestService(t *testing.T, dao *mockBillDAO) (*CostService, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	svc := NewCostService(dao, rdb, nil)
	return svc, mr
}

// --- Tests ---

func TestGetCostSummary(t *testing.T) {
	callCount := 0
	dao := &mockBillDAO{
		sumAmountFn: func(_ context.Context, _ repository.UnifiedBillFilter) (float64, error) {
			callCount++
			if callCount == 1 {
				return 1500.0, nil
			}
			return 1000.0, nil
		},
	}
	svc, _ := setupTestService(t, dao)

	summary, err := svc.GetCostSummary(context.Background(), CostFilter{TenantID: "t1"})
	require.NoError(t, err)
	assert.Equal(t, 1500.0, summary.CurrentMonthAmount)
	assert.Equal(t, 1000.0, summary.LastMonthAmount)
	assert.InDelta(t, 50.0, summary.MoMChangePercent, 0.01)
}

func TestGetCostSummary_LastMonthZero(t *testing.T) {
	callCount := 0
	dao := &mockBillDAO{
		sumAmountFn: func(_ context.Context, _ repository.UnifiedBillFilter) (float64, error) {
			callCount++
			if callCount == 1 {
				return 500.0, nil
			}
			return 0, nil
		},
	}
	svc, _ := setupTestService(t, dao)

	summary, err := svc.GetCostSummary(context.Background(), CostFilter{TenantID: "t1"})
	require.NoError(t, err)
	assert.Equal(t, 500.0, summary.CurrentMonthAmount)
	assert.Equal(t, 0.0, summary.LastMonthAmount)
	assert.Equal(t, 0.0, summary.MoMChangePercent)
}

func TestGetCostSummary_Cache(t *testing.T) {
	callCount := 0
	dao := &mockBillDAO{
		sumAmountFn: func(_ context.Context, _ repository.UnifiedBillFilter) (float64, error) {
			callCount++
			if callCount <= 3 {
				if callCount == 1 {
					return 100.0, nil // 当月
				}
				if callCount == 2 {
					return 80.0, nil // 上月整月
				}
				return 50.0, nil // 上月同期
			}
			return 999.0, nil
		},
	}
	svc, _ := setupTestService(t, dao)
	ctx := context.Background()
	filter := CostFilter{TenantID: "t1"}

	s1, err := svc.GetCostSummary(ctx, filter)
	require.NoError(t, err)
	assert.Equal(t, 100.0, s1.CurrentMonthAmount)

	// Second call should hit cache
	s2, err := svc.GetCostSummary(ctx, filter)
	require.NoError(t, err)
	assert.Equal(t, 100.0, s2.CurrentMonthAmount)
	assert.Equal(t, 3, callCount)
}

func TestGetCostTrend_Daily(t *testing.T) {
	dao := &mockBillDAO{
		aggregateDailyFn: func(_ context.Context, _ string, _, _ string, _ repository.UnifiedBillFilter) ([]repository.DailyAmount, error) {
			return []repository.DailyAmount{
				{Date: "2024-01-03", Amount: 30, AmountCNY: 30},
				{Date: "2024-01-01", Amount: 10, AmountCNY: 10},
				{Date: "2024-01-02", Amount: 20, AmountCNY: 20},
			}, nil
		},
	}
	svc, _ := setupTestService(t, dao)

	points, err := svc.GetCostTrend(context.Background(), CostTrendFilter{
		CostFilter:  CostFilter{TenantID: "t1", StartDate: "2024-01-01", EndDate: "2024-01-03"},
		Granularity: "daily",
	})
	require.NoError(t, err)
	require.Len(t, points, 3)
	assert.Equal(t, "2024-01-01", points[0].Date)
	assert.Equal(t, "2024-01-02", points[1].Date)
	assert.Equal(t, "2024-01-03", points[2].Date)
}

func TestGetCostTrend_Weekly(t *testing.T) {
	dao := &mockBillDAO{
		aggregateDailyFn: func(_ context.Context, _ string, _, _ string, _ repository.UnifiedBillFilter) ([]repository.DailyAmount, error) {
			return []repository.DailyAmount{
				{Date: "2024-01-01", Amount: 10, AmountCNY: 10}, // Monday
				{Date: "2024-01-02", Amount: 20, AmountCNY: 20}, // Tuesday (same week)
				{Date: "2024-01-08", Amount: 30, AmountCNY: 30}, // Next Monday
			}, nil
		},
	}
	svc, _ := setupTestService(t, dao)

	points, err := svc.GetCostTrend(context.Background(), CostTrendFilter{
		CostFilter:  CostFilter{TenantID: "t1", StartDate: "2024-01-01", EndDate: "2024-01-08"},
		Granularity: "weekly",
	})
	require.NoError(t, err)
	require.Len(t, points, 2)
	assert.Equal(t, "2024-01-01", points[0].Date)
	assert.InDelta(t, 30.0, points[0].Amount, 0.01)
	assert.Equal(t, "2024-01-08", points[1].Date)
	assert.InDelta(t, 30.0, points[1].Amount, 0.01)
}

func TestGetCostTrend_Monthly(t *testing.T) {
	dao := &mockBillDAO{
		aggregateDailyFn: func(_ context.Context, _ string, _, _ string, _ repository.UnifiedBillFilter) ([]repository.DailyAmount, error) {
			return []repository.DailyAmount{
				{Date: "2024-01-15", Amount: 100, AmountCNY: 100},
				{Date: "2024-01-20", Amount: 50, AmountCNY: 50},
				{Date: "2024-02-10", Amount: 200, AmountCNY: 200},
			}, nil
		},
	}
	svc, _ := setupTestService(t, dao)

	points, err := svc.GetCostTrend(context.Background(), CostTrendFilter{
		CostFilter:  CostFilter{TenantID: "t1", StartDate: "2024-01-01", EndDate: "2024-02-28"},
		Granularity: "monthly",
	})
	require.NoError(t, err)
	require.Len(t, points, 2)
	assert.Equal(t, "2024-01", points[0].Date)
	assert.InDelta(t, 150.0, points[0].Amount, 0.01)
	assert.Equal(t, "2024-02", points[1].Date)
	assert.InDelta(t, 200.0, points[1].Amount, 0.01)
}

func TestGetCostDistribution(t *testing.T) {
	dao := &mockBillDAO{
		aggregateByFieldFn: func(_ context.Context, _ string, _ string, _, _ string) ([]repository.AggregateResult, error) {
			return []repository.AggregateResult{
				{Key: "aliyun", Amount: 300, AmountCNY: 300},
				{Key: "aws", Amount: 200, AmountCNY: 200},
			}, nil
		},
	}
	svc, _ := setupTestService(t, dao)

	items, err := svc.GetCostDistribution(context.Background(), CostFilter{
		TenantID:  "t1",
		StartDate: "2024-01-01",
		EndDate:   "2024-01-31",
	}, "provider")
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "aliyun", items[0].Key)
	assert.InDelta(t, 60.0, items[0].Percent, 0.01)
	assert.Equal(t, "aws", items[1].Key)
	assert.InDelta(t, 40.0, items[1].Percent, 0.01)
}

func TestGetCostDistribution_Empty(t *testing.T) {
	dao := &mockBillDAO{
		aggregateByFieldFn: func(_ context.Context, _ string, _ string, _, _ string) ([]repository.AggregateResult, error) {
			return []repository.AggregateResult{}, nil
		},
	}
	svc, _ := setupTestService(t, dao)

	items, err := svc.GetCostDistribution(context.Background(), CostFilter{TenantID: "t1"}, "provider")
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestGetYoYComparison(t *testing.T) {
	callCount := 0
	dao := &mockBillDAO{
		sumAmountFn: func(_ context.Context, _ repository.UnifiedBillFilter) (float64, error) {
			callCount++
			if callCount == 1 {
				return 1200.0, nil
			}
			return 1000.0, nil
		},
	}
	svc, _ := setupTestService(t, dao)

	result, err := svc.GetYoYComparison(context.Background(), CostFilter{
		TenantID:  "t1",
		StartDate: "2024-01-01",
		EndDate:   "2024-01-31",
	})
	require.NoError(t, err)
	assert.Equal(t, 1200.0, result.CurrentAmount)
	assert.Equal(t, 1000.0, result.PreviousAmount)
	assert.InDelta(t, 20.0, result.ChangePercent, 0.01)
	assert.Contains(t, result.CurrentPeriod, "2024-01-01")
	assert.Contains(t, result.PreviousPeriod, "2023-01-01")
}

func TestGetYoYComparison_PreviousZero(t *testing.T) {
	callCount := 0
	dao := &mockBillDAO{
		sumAmountFn: func(_ context.Context, _ repository.UnifiedBillFilter) (float64, error) {
			callCount++
			if callCount == 1 {
				return 500.0, nil
			}
			return 0, nil
		},
	}
	svc, _ := setupTestService(t, dao)

	result, err := svc.GetYoYComparison(context.Background(), CostFilter{
		TenantID:  "t1",
		StartDate: "2024-06-01",
		EndDate:   "2024-06-30",
	})
	require.NoError(t, err)
	assert.Equal(t, 0.0, result.ChangePercent)
}

func TestNewCostService_NilRedis(t *testing.T) {
	dao := &mockBillDAO{
		sumAmountFn: func(_ context.Context, _ repository.UnifiedBillFilter) (float64, error) {
			return 100.0, nil
		},
	}
	svc := NewCostService(dao, nil, nil)

	summary, err := svc.GetCostSummary(context.Background(), CostFilter{TenantID: "t1"})
	require.NoError(t, err)
	assert.Equal(t, 100.0, summary.CurrentMonthAmount)
}

func TestAggregateWeekly_Empty(t *testing.T) {
	assert.Empty(t, aggregateWeekly(nil))
}

func TestAggregateMonthly_Empty(t *testing.T) {
	assert.Empty(t, aggregateMonthly(nil))
}

func TestConvertDailyToPoints_Sorted(t *testing.T) {
	daily := []repository.DailyAmount{
		{Date: "2024-03-01", Amount: 3, AmountCNY: 3},
		{Date: "2024-01-01", Amount: 1, AmountCNY: 1},
		{Date: "2024-02-01", Amount: 2, AmountCNY: 2},
	}
	points := convertDailyToPoints(daily)
	require.Len(t, points, 3)
	assert.Equal(t, "2024-01-01", points[0].Date)
	assert.Equal(t, "2024-02-01", points[1].Date)
	assert.Equal(t, "2024-03-01", points[2].Date)
}

func TestGetCacheKey_Deterministic(t *testing.T) {
	f := CostFilter{TenantID: "t1", Provider: "aws"}
	k1 := getCacheKey("prefix", "t1", f)
	k2 := getCacheKey("prefix", "t1", f)
	assert.Equal(t, k1, k2)

	f2 := CostFilter{TenantID: "t1", Provider: "aliyun"}
	k3 := getCacheKey("prefix", "t1", f2)
	assert.NotEqual(t, k1, k3)
}

func TestToUnifiedBillFilter(t *testing.T) {
	f := CostFilter{
		TenantID:    "t1",
		Provider:    "aws",
		AccountID:   42,
		ServiceType: "compute",
		Region:      "us-east-1",
		StartDate:   "2024-01-01",
		EndDate:     "2024-01-31",
	}
	ubf := toUnifiedBillFilter(f)
	assert.Equal(t, f.TenantID, ubf.TenantID)
	assert.Equal(t, f.Provider, ubf.Provider)
	assert.Equal(t, f.AccountID, ubf.AccountID)
	assert.Equal(t, f.ServiceType, ubf.ServiceType)
	assert.Equal(t, f.Region, ubf.Region)
	assert.Equal(t, f.StartDate, ubf.StartDate)
	assert.Equal(t, f.EndDate, ubf.EndDate)
}

func TestAggregateWeekly_SundayBelongsToPreviousWeek(t *testing.T) {
	daily := []repository.DailyAmount{
		{Date: "2024-01-01", Amount: 10, AmountCNY: 10}, // Monday
		{Date: "2024-01-07", Amount: 20, AmountCNY: 20}, // Sunday (same week)
	}
	points := aggregateWeekly(daily)
	require.Len(t, points, 1)
	assert.InDelta(t, 30.0, points[0].Amount, 0.01)
}

func TestGetCostSummary_DateRanges(t *testing.T) {
	var capturedFilters []repository.UnifiedBillFilter
	dao := &mockBillDAO{
		sumAmountFn: func(_ context.Context, filter repository.UnifiedBillFilter) (float64, error) {
			capturedFilters = append(capturedFilters, filter)
			return 100.0, nil
		},
	}
	svc, _ := setupTestService(t, dao)

	_, err := svc.GetCostSummary(context.Background(), CostFilter{TenantID: "t1"})
	require.NoError(t, err)
	require.Len(t, capturedFilters, 3) // 当月、上月整月、上月同期

	now := time.Now()
	expectedCurrentStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	assert.Equal(t, expectedCurrentStart, capturedFilters[0].StartDate)

	// 第三个查询是上月同期
	lastMonthSameDay := time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, now.Location())
	assert.Equal(t, lastMonthSameDay.Format("2006-01-02"), capturedFilters[2].StartDate)
}
