package budget

import (
	"context"
	"fmt"
	"math"
	"sort"
	"testing"
	"time"

	alertdomain "github.com/Havens-blog/e-cam-service/internal/alert/domain"
	costdomain "github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

const floatTolerance = 1e-9

// genSortedThresholds generates a sorted ascending slice of thresholds in (0, 100].
func genSortedThresholds(rt *rapid.T) []float64 {
	n := rapid.IntRange(1, 5).Draw(rt, "numThresholds")
	thresholds := make([]float64, n)
	for i := 0; i < n; i++ {
		thresholds[i] = rapid.Float64Range(0.01, 100.0).Draw(rt, fmt.Sprintf("threshold_%d", i))
	}
	sort.Float64s(thresholds)
	// Deduplicate to ensure strictly ascending
	deduped := thresholds[:1]
	for i := 1; i < len(thresholds); i++ {
		if thresholds[i] > deduped[len(deduped)-1] {
			deduped = append(deduped, thresholds[i])
		}
	}
	if len(deduped) == 0 {
		deduped = []float64{50}
	}
	return deduped
}

// TestProperty16_BudgetRuleCreationRoundTrip verifies that for any valid budget rule
// (Name non-empty, AmountLimit > 0, Thresholds sorted ascending in (0,100]),
// CreateBudget should succeed and the created budget should have Status="active",
// Period="monthly", and NotifiedAt initialized as empty map.
//
// **Validates: Requirements 5.1, 5.2**
func TestProperty16_BudgetRuleCreationRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		name := rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9 ]{0,49}`).Draw(rt, "name")
		amountLimit := rapid.Float64Range(0.01, 1e8).Draw(rt, "amountLimit")
		thresholds := genSortedThresholds(rt)
		scopeType := rapid.SampledFrom([]string{"all", "provider", "account"}).Draw(rt, "scopeType")
		tenantID := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "tenantID")

		var captured costdomain.BudgetRule
		budgetDAO := &mockBudgetDAO{
			createFn: func(_ context.Context, b costdomain.BudgetRule) (int64, error) {
				captured = b
				return 1, nil
			},
		}
		svc := setupTestService(t, budgetDAO, &mockBillDAO{}, &mockAlertDAO{})

		id, err := svc.CreateBudget(context.Background(), costdomain.BudgetRule{
			Name:        name,
			AmountLimit: amountLimit,
			Thresholds:  thresholds,
			ScopeType:   scopeType,
			TenantID:    tenantID,
		})

		assert.NoError(rt, err, "CreateBudget should succeed for valid input")
		if err != nil {
			return
		}
		assert.Equal(rt, int64(1), id)
		assert.Equal(rt, "active", captured.Status, "Status should be 'active'")
		assert.Equal(rt, "monthly", captured.Period, "Period should be 'monthly'")
		assert.NotNil(rt, captured.NotifiedAt, "NotifiedAt should not be nil")
		assert.Empty(rt, captured.NotifiedAt, "NotifiedAt should be empty map")
	})
}

// TestProperty17_BudgetThresholdAlertTrigger verifies that for any budget with
// AmountLimit > 0 and current spend >= threshold% of AmountLimit, CheckBudgets
// should trigger an alert event with correct severity and content fields.
//
// **Validates: Requirements 5.3**
func TestProperty17_BudgetThresholdAlertTrigger(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		amountLimit := rapid.Float64Range(1, 1e8).Draw(rt, "amountLimit")
		thresholds := genSortedThresholds(rt)
		// Generate currentSpend that exceeds at least the lowest threshold
		minThreshold := thresholds[0]
		minSpend := amountLimit * minThreshold / 100
		currentSpend := rapid.Float64Range(minSpend, amountLimit*1.5).Draw(rt, "currentSpend")
		usagePercent := currentSpend / amountLimit * 100

		budgetID := rapid.Int64Range(1, 10000).Draw(rt, "budgetID")
		budgetName := rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9]{0,19}`).Draw(rt, "budgetName")
		tenantID := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "tenantID")

		alertDAO := &mockAlertDAO{
			listRulesFn: func(_ context.Context, filter alertdomain.AlertRuleFilter) ([]alertdomain.AlertRule, int64, error) {
				return []alertdomain.AlertRule{{ID: 1, Type: filter.Type, Enabled: true}}, 1, nil
			},
		}
		budgetDAO := &mockBudgetDAO{
			listActiveFn: func(_ context.Context, _ string) ([]costdomain.BudgetRule, error) {
				return []costdomain.BudgetRule{{
					ID:          budgetID,
					Name:        budgetName,
					AmountLimit: amountLimit,
					ScopeType:   "all",
					Thresholds:  thresholds,
					NotifiedAt:  make(map[string]time.Time),
					TenantID:    tenantID,
					Status:      "active",
				}}, nil
			},
			updateNotifiedFn: func(_ context.Context, _ int64, _ map[string]time.Time) error {
				return nil
			},
		}
		billDAO := &mockBillDAO{
			sumAmountFn: func(_ context.Context, _ repository.UnifiedBillFilter) (float64, error) {
				return currentSpend, nil
			},
		}

		svc := setupTestService(t, budgetDAO, billDAO, alertDAO)
		err := svc.CheckBudgets(context.Background(), tenantID)
		assert.NoError(rt, err)

		// Determine which thresholds should have been triggered
		var expectedTriggered []float64
		for _, th := range thresholds {
			if usagePercent >= th {
				expectedTriggered = append(expectedTriggered, th)
			}
		}

		assert.Len(rt, alertDAO.emittedEvents, len(expectedTriggered),
			"should emit exactly one alert per exceeded threshold")

		// Verify each emitted event
		for i, event := range alertDAO.emittedEvents {
			th := expectedTriggered[i]
			expectedSeverity := thresholdSeverity(th)

			assert.Equal(rt, alertdomain.AlertType("budget_threshold"), event.Type,
				"alert Type should be budget_threshold")
			assert.Equal(rt, expectedSeverity, event.Severity,
				"alert severity for threshold %.2f should be %s", th, expectedSeverity)

			// Verify Content contains required fields
			assert.Contains(rt, event.Content, "budget_id", "Content should contain budget_id")
			assert.Contains(rt, event.Content, "budget_name", "Content should contain budget_name")
			assert.Contains(rt, event.Content, "threshold", "Content should contain threshold")

			contentBudgetID, _ := event.Content["budget_id"].(int64)
			assert.Equal(rt, budgetID, contentBudgetID, "Content budget_id should match")

			contentBudgetName, _ := event.Content["budget_name"].(string)
			assert.Equal(rt, budgetName, contentBudgetName, "Content budget_name should match")

			contentThreshold, _ := event.Content["threshold"].(float64)
			assert.InDelta(rt, th, contentThreshold, floatTolerance,
				"Content threshold should match the triggered threshold")
		}
	})
}

// TestProperty18_BudgetProgressCalculation verifies that for any budget with
// AmountLimit > 0 and any non-negative current spend, GetBudgetProgress returns
// correct UsagePercent, RemainingAmount, and CurrentSpend.
//
// **Validates: Requirements 5.4**
func TestProperty18_BudgetProgressCalculation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		amountLimit := rapid.Float64Range(0.01, 1e8).Draw(rt, "amountLimit")
		currentSpend := rapid.Float64Range(0, amountLimit*2).Draw(rt, "currentSpend")
		budgetID := rapid.Int64Range(1, 10000).Draw(rt, "budgetID")

		budgetDAO := &mockBudgetDAO{
			getByIDFn: func(_ context.Context, id int64) (costdomain.BudgetRule, error) {
				return costdomain.BudgetRule{
					ID:          id,
					Name:        "Test Budget",
					AmountLimit: amountLimit,
					ScopeType:   "all",
					TenantID:    "t1",
					Status:      "active",
				}, nil
			},
		}
		billDAO := &mockBillDAO{
			sumAmountFn: func(_ context.Context, _ repository.UnifiedBillFilter) (float64, error) {
				return currentSpend, nil
			},
		}

		svc := setupTestService(t, budgetDAO, billDAO, &mockAlertDAO{})
		progress, err := svc.GetBudgetProgress(context.Background(), budgetID)
		assert.NoError(rt, err)
		if err != nil {
			return
		}

		// UsagePercent == currentSpend / AmountLimit * 100
		expectedUsage := currentSpend / amountLimit * 100
		assert.InDelta(rt, expectedUsage, progress.UsagePercent, floatTolerance,
			"UsagePercent should be currentSpend/amountLimit*100")

		// RemainingAmount == max(0, AmountLimit - currentSpend)
		expectedRemaining := math.Max(0, amountLimit-currentSpend)
		assert.InDelta(rt, expectedRemaining, progress.RemainingAmount, floatTolerance,
			"RemainingAmount should be max(0, amountLimit-currentSpend)")

		// CurrentSpend == the queried amount
		assert.InDelta(rt, currentSpend, progress.CurrentSpend, floatTolerance,
			"CurrentSpend should equal the queried spend amount")
	})
}
