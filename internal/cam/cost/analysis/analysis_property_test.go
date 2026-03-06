package analysis

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/alicebob/miniredis/v2"
	"github.com/gotomicro/ego/core/elog"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// propertyMockBillDAO is a mock BillDAO for property tests.
type propertyMockBillDAO struct {
	sumAmountFn        func(ctx context.Context, filter repository.UnifiedBillFilter) (float64, error)
	aggregateByFieldFn func(ctx context.Context, tenantID string, field string, startDate, endDate string) ([]repository.AggregateResult, error)
	aggregateDailyFn   func(ctx context.Context, tenantID string, startDate, endDate string, filter repository.UnifiedBillFilter) ([]repository.DailyAmount, error)
}

func (m *propertyMockBillDAO) SumAmount(ctx context.Context, filter repository.UnifiedBillFilter) (float64, error) {
	if m.sumAmountFn != nil {
		return m.sumAmountFn(ctx, filter)
	}
	return 0, nil
}

func (m *propertyMockBillDAO) AggregateByField(ctx context.Context, tenantID string, field string, startDate, endDate string) ([]repository.AggregateResult, error) {
	if m.aggregateByFieldFn != nil {
		return m.aggregateByFieldFn(ctx, tenantID, field, startDate, endDate)
	}
	return nil, nil
}

func (m *propertyMockBillDAO) AggregateDailyAmount(ctx context.Context, tenantID string, startDate, endDate string, filter repository.UnifiedBillFilter) ([]repository.DailyAmount, error) {
	if m.aggregateDailyFn != nil {
		return m.aggregateDailyFn(ctx, tenantID, startDate, endDate, filter)
	}
	return nil, nil
}

// --- BillDAO interface stubs (unused by CostService) ---

func (m *propertyMockBillDAO) InsertRawBill(_ context.Context, _ domain.RawBillRecord) (int64, error) {
	return 0, nil
}
func (m *propertyMockBillDAO) InsertRawBills(_ context.Context, _ []domain.RawBillRecord) (int64, error) {
	return 0, nil
}
func (m *propertyMockBillDAO) GetRawBillByID(_ context.Context, _ int64) (domain.RawBillRecord, error) {
	return domain.RawBillRecord{}, nil
}
func (m *propertyMockBillDAO) ListRawBills(_ context.Context, _ int64, _, _ string) ([]domain.RawBillRecord, error) {
	return nil, nil
}
func (m *propertyMockBillDAO) ListRawBillsByCollectID(_ context.Context, _ string) ([]domain.RawBillRecord, error) {
	return nil, nil
}
func (m *propertyMockBillDAO) InsertUnifiedBill(_ context.Context, _ domain.UnifiedBill) (int64, error) {
	return 0, nil
}
func (m *propertyMockBillDAO) InsertUnifiedBills(_ context.Context, _ []domain.UnifiedBill) (int64, error) {
	return 0, nil
}
func (m *propertyMockBillDAO) GetUnifiedBillByID(_ context.Context, _ int64) (domain.UnifiedBill, error) {
	return domain.UnifiedBill{}, nil
}
func (m *propertyMockBillDAO) ListUnifiedBills(_ context.Context, _ repository.UnifiedBillFilter) ([]domain.UnifiedBill, error) {
	return nil, nil
}
func (m *propertyMockBillDAO) CountUnifiedBills(_ context.Context, _ repository.UnifiedBillFilter) (int64, error) {
	return 0, nil
}
func (m *propertyMockBillDAO) DeleteUnifiedBillsByPeriod(_ context.Context, _, _ string) error {
	return nil
}
func (m *propertyMockBillDAO) DeleteRawBillsByAccountAndRange(_ context.Context, _ int64, _, _ string) (int64, error) {
	return 0, nil
}
func (m *propertyMockBillDAO) DeleteUnifiedBillsByAccountAndRange(_ context.Context, _ int64, _, _ string) (int64, error) {
	return 0, nil
}
func (m *propertyMockBillDAO) AggregateByTag(_ context.Context, _ string, _, _ string) ([]repository.AggregateResult, error) {
	return nil, nil
}

// newPropertyCostService creates a CostService with miniredis for property tests.
// Uses the outer *testing.T (not *rapid.T) for miniredis setup.
func newPropertyCostService(t *testing.T, dao *propertyMockBillDAO) *CostService {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return NewCostService(dao, rdb, elog.DefaultLogger)
}

const floatTolerance = 1e-9

// TestProperty11_CostSummaryCompleteness verifies that for any valid CostFilter,
// GetCostSummary returns: CurrentMonthAmount >= 0, LastMonthAmount >= 0,
// and MoMChangePercent == (current - last) / last * 100 when last > 0, else 0.
//
// **Validates: Requirements 4.1**
func TestProperty11_CostSummaryCompleteness(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		currentAmount := rapid.Float64Range(0, 1e8).Draw(rt, "currentAmount")
		lastAmount := rapid.Float64Range(0, 1e8).Draw(rt, "lastAmount")

		callCount := 0
		dao := &propertyMockBillDAO{
			sumAmountFn: func(_ context.Context, _ repository.UnifiedBillFilter) (float64, error) {
				callCount++
				if callCount == 1 {
					return currentAmount, nil
				}
				return lastAmount, nil
			},
		}
		svc := newPropertyCostService(t, dao)

		summary, err := svc.GetCostSummary(context.Background(), CostFilter{TenantID: "prop-test"})
		assert.NoError(rt, err)
		if err != nil {
			return
		}

		// CurrentMonthAmount >= 0
		assert.GreaterOrEqual(rt, summary.CurrentMonthAmount, 0.0,
			"CurrentMonthAmount must be >= 0")

		// LastMonthAmount >= 0
		assert.GreaterOrEqual(rt, summary.LastMonthAmount, 0.0,
			"LastMonthAmount must be >= 0")

		// MoMChangePercent correctness
		if lastAmount > 0 {
			expectedMoM := (currentAmount - lastAmount) / lastAmount * 100
			assert.InDelta(rt, expectedMoM, summary.MoMChangePercent, floatTolerance,
				"MoMChangePercent should be (current-last)/last*100 when last > 0")
		} else {
			assert.Equal(rt, 0.0, summary.MoMChangePercent,
				"MoMChangePercent should be 0 when last month amount is 0")
		}
	})
}

// TestProperty12_CostDistributionConservation verifies that for any cost distribution
// result, the sum of all items' Percent equals 100% (within floating point tolerance),
// and each item's Percent == item.AmountCNY / total * 100.
//
// **Validates: Requirements 4.2**
func TestProperty12_CostDistributionConservation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate 1-10 aggregate result items with positive AmountCNY
		numItems := rapid.IntRange(1, 10).Draw(rt, "numItems")
		results := make([]repository.AggregateResult, numItems)
		for i := 0; i < numItems; i++ {
			amountCNY := rapid.Float64Range(0.01, 1e6).Draw(rt, fmt.Sprintf("amountCNY_%d", i))
			results[i] = repository.AggregateResult{
				Key:       fmt.Sprintf("item_%d", i),
				Amount:    amountCNY,
				AmountCNY: amountCNY,
			}
		}

		dao := &propertyMockBillDAO{
			aggregateByFieldFn: func(_ context.Context, _ string, _ string, _, _ string) ([]repository.AggregateResult, error) {
				return results, nil
			},
		}
		svc := newPropertyCostService(t, dao)

		items, err := svc.GetCostDistribution(context.Background(), CostFilter{
			TenantID:  "prop-test",
			StartDate: "2024-01-01",
			EndDate:   "2024-01-31",
		}, "provider")
		assert.NoError(rt, err)
		if err != nil {
			return
		}

		assert.Len(rt, items, numItems)

		// Compute expected total
		var totalCNY float64
		for _, r := range results {
			totalCNY += r.AmountCNY
		}

		// Verify each item's Percent
		var percentSum float64
		for i, item := range items {
			expectedPct := results[i].AmountCNY / totalCNY * 100
			assert.InDelta(rt, expectedPct, item.Percent, floatTolerance,
				"item %d Percent should be amountCNY/total*100", i)
			percentSum += item.Percent
		}

		// Sum of all Percent values should be ~100%
		assert.InDelta(rt, 100.0, percentSum, 1e-6,
			"sum of all Percent values should equal 100%%")
	})
}

// TestProperty13_TrendDataConservation verifies that for any cost trend query,
// the sum of all aggregated trend points' Amount equals the sum of input daily amounts.
// Tests aggregateWeekly and aggregateMonthly directly.
//
// **Validates: Requirements 4.3**
func TestProperty13_TrendDataConservation(t *testing.T) {
	t.Run("daily_conservation", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			numDays := rapid.IntRange(1, 28).Draw(rt, "numDays")
			daily := make([]repository.DailyAmount, numDays)
			var expectedSum float64
			for i := 0; i < numDays; i++ {
				amount := rapid.Float64Range(0, 1e6).Draw(rt, fmt.Sprintf("amount_%d", i))
				daily[i] = repository.DailyAmount{
					Date:      fmt.Sprintf("2024-01-%02d", i+1),
					Amount:    amount,
					AmountCNY: amount,
				}
				expectedSum += amount
			}

			points := convertDailyToPoints(daily)

			var actualSum float64
			for _, p := range points {
				actualSum += p.Amount
			}

			assert.InDelta(rt, expectedSum, actualSum, floatTolerance,
				"sum of daily points should equal sum of input amounts")
		})
	})

	t.Run("weekly_conservation", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			numDays := rapid.IntRange(1, 28).Draw(rt, "numDays")
			daily := make([]repository.DailyAmount, numDays)
			var expectedSum float64
			for i := 0; i < numDays; i++ {
				amount := rapid.Float64Range(0, 1e6).Draw(rt, fmt.Sprintf("amount_%d", i))
				daily[i] = repository.DailyAmount{
					Date:      fmt.Sprintf("2024-01-%02d", i+1),
					Amount:    amount,
					AmountCNY: amount,
				}
				expectedSum += amount
			}

			points := aggregateWeekly(daily)

			var actualSum float64
			for _, p := range points {
				actualSum += p.Amount
			}

			assert.InDelta(rt, expectedSum, actualSum, 1e-6,
				"sum of weekly aggregated points should equal sum of input amounts")
		})
	})

	t.Run("monthly_conservation", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			numDays := rapid.IntRange(1, 28).Draw(rt, "numDays")
			daily := make([]repository.DailyAmount, numDays)
			var expectedSum float64
			months := []string{"2024-01", "2024-02", "2024-03"}
			for i := 0; i < numDays; i++ {
				amount := rapid.Float64Range(0, 1e6).Draw(rt, fmt.Sprintf("amount_%d", i))
				monthIdx := i % len(months)
				day := (i/len(months))%28 + 1
				daily[i] = repository.DailyAmount{
					Date:      fmt.Sprintf("%s-%02d", months[monthIdx], day),
					Amount:    amount,
					AmountCNY: amount,
				}
				expectedSum += amount
			}

			points := aggregateMonthly(daily)

			var actualSum float64
			for _, p := range points {
				actualSum += p.Amount
			}

			assert.InDelta(rt, expectedSum, actualSum, 1e-6,
				"sum of monthly aggregated points should equal sum of input amounts")
		})
	})
}

// TestProperty14_FilterCorrectness verifies that for any filter combination
// (provider, service_type, region), toUnifiedBillFilter correctly maps all fields,
// ensuring filtered results are a proper subset of unfiltered results.
//
// **Validates: Requirements 4.4**
func TestProperty14_FilterCorrectness(t *testing.T) {
	providers := []string{"", "aliyun", "aws", "huawei", "tencent", "volcano"}
	serviceTypes := []string{"", "compute", "storage", "network", "database", "middleware", "other"}
	regions := []string{"", "cn-hangzhou", "us-east-1", "cn-beijing", "ap-southeast-1"}

	rapid.Check(t, func(rt *rapid.T) {
		provider := rapid.SampledFrom(providers).Draw(rt, "provider")
		serviceType := rapid.SampledFrom(serviceTypes).Draw(rt, "serviceType")
		region := rapid.SampledFrom(regions).Draw(rt, "region")
		accountID := rapid.Int64Range(0, 100000).Draw(rt, "accountID")
		tenantID := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "tenantID")

		filter := CostFilter{
			TenantID:    tenantID,
			Provider:    provider,
			AccountID:   accountID,
			ServiceType: serviceType,
			Region:      region,
			StartDate:   "2024-01-01",
			EndDate:     "2024-12-31",
		}

		ubf := toUnifiedBillFilter(filter)

		// All fields must be correctly mapped
		assert.Equal(rt, filter.TenantID, ubf.TenantID, "TenantID must match")
		assert.Equal(rt, filter.Provider, ubf.Provider, "Provider must match")
		assert.Equal(rt, filter.AccountID, ubf.AccountID, "AccountID must match")
		assert.Equal(rt, filter.ServiceType, ubf.ServiceType, "ServiceType must match")
		assert.Equal(rt, filter.Region, ubf.Region, "Region must match")
		assert.Equal(rt, filter.StartDate, ubf.StartDate, "StartDate must match")
		assert.Equal(rt, filter.EndDate, ubf.EndDate, "EndDate must match")

		// Verify filtered total <= unfiltered total via mock
		unfilteredAmount := rapid.Float64Range(100, 1e6).Draw(rt, "unfilteredAmount")
		filteredRatio := rapid.Float64Range(0, 1).Draw(rt, "filteredRatio")
		filteredAmount := unfilteredAmount * filteredRatio

		callCount := 0
		dao := &propertyMockBillDAO{
			sumAmountFn: func(_ context.Context, f repository.UnifiedBillFilter) (float64, error) {
				callCount++
				if f.Provider != "" || f.ServiceType != "" || f.Region != "" {
					return filteredAmount, nil
				}
				return unfilteredAmount, nil
			},
		}
		svc := newPropertyCostService(t, dao)

		// Get filtered summary
		filteredSummary, err := svc.GetCostSummary(context.Background(), filter)
		assert.NoError(rt, err)
		if err != nil {
			return
		}

		// Get unfiltered summary
		unfilteredFilter := CostFilter{TenantID: tenantID}
		unfilteredSummary, err := svc.GetCostSummary(context.Background(), unfilteredFilter)
		assert.NoError(rt, err)
		if err != nil {
			return
		}

		// Filtered total should be <= unfiltered total
		assert.LessOrEqual(rt, filteredSummary.CurrentMonthAmount, unfilteredSummary.CurrentMonthAmount+floatTolerance,
			"filtered total should be <= unfiltered total")
	})
}

// TestProperty15_ComparisonCalculation verifies that MoM/YoY change percent
// is calculated as (current - previous) / previous * 100 when previous > 0,
// and 0 when previous == 0.
//
// **Validates: Requirements 4.6**
func TestProperty15_ComparisonCalculation(t *testing.T) {
	t.Run("yoy_comparison", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			currentAmount := rapid.Float64Range(0, 1e8).Draw(rt, "currentAmount")
			// Use integer cents converted to float to avoid subnormal floats
			// that cause +Inf overflow when used as divisor
			previousCents := rapid.IntRange(0, 1e10).Draw(rt, "previousCents")
			previousAmount := float64(previousCents) / 100.0

			callCount := 0
			dao := &propertyMockBillDAO{
				sumAmountFn: func(_ context.Context, _ repository.UnifiedBillFilter) (float64, error) {
					callCount++
					if callCount == 1 {
						return currentAmount, nil
					}
					return previousAmount, nil
				},
			}
			svc := newPropertyCostService(t, dao)

			result, err := svc.GetYoYComparison(context.Background(), CostFilter{
				TenantID:  "prop-test",
				StartDate: "2024-01-01",
				EndDate:   "2024-01-31",
			})
			assert.NoError(rt, err)
			if err != nil {
				return
			}

			assert.Equal(rt, currentAmount, result.CurrentAmount,
				"CurrentAmount should match input")
			assert.Equal(rt, previousAmount, result.PreviousAmount,
				"PreviousAmount should match input")

			if previousAmount > 0 {
				expectedChange := (currentAmount - previousAmount) / previousAmount * 100
				assert.True(rt, math.Abs(result.ChangePercent-expectedChange) < floatTolerance,
					"ChangePercent should be (current-previous)/previous*100, got %v expected %v",
					result.ChangePercent, expectedChange)
			} else {
				assert.Equal(rt, 0.0, result.ChangePercent,
					"ChangePercent should be 0 when previous amount is 0")
			}
		})
	})

	t.Run("mom_comparison", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			currentAmount := rapid.Float64Range(0, 1e8).Draw(rt, "currentAmount")
			lastAmount := rapid.Float64Range(0, 1e8).Draw(rt, "lastAmount")

			callCount := 0
			dao := &propertyMockBillDAO{
				sumAmountFn: func(_ context.Context, _ repository.UnifiedBillFilter) (float64, error) {
					callCount++
					if callCount == 1 {
						return currentAmount, nil
					}
					return lastAmount, nil
				},
			}
			svc := newPropertyCostService(t, dao)

			summary, err := svc.GetCostSummary(context.Background(), CostFilter{TenantID: "prop-test"})
			assert.NoError(rt, err)
			if err != nil {
				return
			}

			if lastAmount > 0 {
				expectedMoM := (currentAmount - lastAmount) / lastAmount * 100
				assert.True(rt, math.Abs(summary.MoMChangePercent-expectedMoM) < floatTolerance,
					"MoMChangePercent should be (current-last)/last*100, got %v expected %v",
					summary.MoMChangePercent, expectedMoM)
			} else {
				assert.Equal(rt, 0.0, summary.MoMChangePercent,
					"MoMChangePercent should be 0 when last month amount is 0")
			}
		})
	})
}
