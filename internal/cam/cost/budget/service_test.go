package budget

import (
	"context"
	"testing"
	"time"

	alertdomain "github.com/Havens-blog/e-cam-service/internal/alert/domain"
	alertservice "github.com/Havens-blog/e-cam-service/internal/alert/service"
	costdomain "github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockBudgetDAO struct {
	createFn         func(ctx context.Context, budget costdomain.BudgetRule) (int64, error)
	updateFn         func(ctx context.Context, budget costdomain.BudgetRule) error
	getByIDFn        func(ctx context.Context, id int64) (costdomain.BudgetRule, error)
	listFn           func(ctx context.Context, filter repository.BudgetFilter) ([]costdomain.BudgetRule, error)
	countFn          func(ctx context.Context, filter repository.BudgetFilter) (int64, error)
	listActiveFn     func(ctx context.Context, tenantID string) ([]costdomain.BudgetRule, error)
	updateStatusFn   func(ctx context.Context, id int64, status string) error
	updateNotifiedFn func(ctx context.Context, id int64, notifiedAt map[string]time.Time) error
	deleteFn         func(ctx context.Context, id int64) error
}

func (m *mockBudgetDAO) Create(ctx context.Context, budget costdomain.BudgetRule) (int64, error) {
	if m.createFn != nil {
		return m.createFn(ctx, budget)
	}
	return 1, nil
}
func (m *mockBudgetDAO) Update(ctx context.Context, budget costdomain.BudgetRule) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, budget)
	}
	return nil
}
func (m *mockBudgetDAO) GetByID(ctx context.Context, id int64) (costdomain.BudgetRule, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return costdomain.BudgetRule{}, nil
}
func (m *mockBudgetDAO) List(ctx context.Context, filter repository.BudgetFilter) ([]costdomain.BudgetRule, error) {
	if m.listFn != nil {
		return m.listFn(ctx, filter)
	}
	return nil, nil
}
func (m *mockBudgetDAO) Count(ctx context.Context, filter repository.BudgetFilter) (int64, error) {
	if m.countFn != nil {
		return m.countFn(ctx, filter)
	}
	return 0, nil
}
func (m *mockBudgetDAO) ListActive(ctx context.Context, tenantID string) ([]costdomain.BudgetRule, error) {
	if m.listActiveFn != nil {
		return m.listActiveFn(ctx, tenantID)
	}
	return nil, nil
}
func (m *mockBudgetDAO) UpdateStatus(ctx context.Context, id int64, status string) error {
	if m.updateStatusFn != nil {
		return m.updateStatusFn(ctx, id, status)
	}
	return nil
}
func (m *mockBudgetDAO) UpdateNotifiedAt(ctx context.Context, id int64, notifiedAt map[string]time.Time) error {
	if m.updateNotifiedFn != nil {
		return m.updateNotifiedFn(ctx, id, notifiedAt)
	}
	return nil
}
func (m *mockBudgetDAO) Delete(ctx context.Context, id int64) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}

type mockBillDAO struct {
	sumAmountFn func(ctx context.Context, filter repository.UnifiedBillFilter) (float64, error)
}

func (m *mockBillDAO) SumAmount(ctx context.Context, filter repository.UnifiedBillFilter) (float64, error) {
	if m.sumAmountFn != nil {
		return m.sumAmountFn(ctx, filter)
	}
	return 0, nil
}
func (m *mockBillDAO) InsertRawBill(_ context.Context, _ costdomain.RawBillRecord) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) InsertRawBills(_ context.Context, _ []costdomain.RawBillRecord) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) GetRawBillByID(_ context.Context, _ int64) (costdomain.RawBillRecord, error) {
	return costdomain.RawBillRecord{}, nil
}
func (m *mockBillDAO) ListRawBills(_ context.Context, _ int64, _, _ string) ([]costdomain.RawBillRecord, error) {
	return nil, nil
}
func (m *mockBillDAO) ListRawBillsByCollectID(_ context.Context, _ string) ([]costdomain.RawBillRecord, error) {
	return nil, nil
}
func (m *mockBillDAO) InsertUnifiedBill(_ context.Context, _ costdomain.UnifiedBill) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) InsertUnifiedBills(_ context.Context, _ []costdomain.UnifiedBill) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) GetUnifiedBillByID(_ context.Context, _ int64) (costdomain.UnifiedBill, error) {
	return costdomain.UnifiedBill{}, nil
}
func (m *mockBillDAO) ListUnifiedBills(_ context.Context, _ repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
	return nil, nil
}
func (m *mockBillDAO) CountUnifiedBills(_ context.Context, _ repository.UnifiedBillFilter) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) AggregateByField(_ context.Context, _ string, _ string, _, _ string, _ repository.UnifiedBillFilter) ([]repository.AggregateResult, error) {
	return nil, nil
}
func (m *mockBillDAO) AggregateDailyAmount(_ context.Context, _ string, _, _ string, _ repository.UnifiedBillFilter) ([]repository.DailyAmount, error) {
	return nil, nil
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

type mockAlertDAO struct {
	emittedEvents []alertdomain.AlertEvent
	listRulesFn   func(ctx context.Context, filter alertdomain.AlertRuleFilter) ([]alertdomain.AlertRule, int64, error)
}

func (m *mockAlertDAO) CreateRule(_ context.Context, _ alertdomain.AlertRule) (int64, error) {
	return 0, nil
}
func (m *mockAlertDAO) UpdateRule(_ context.Context, _ alertdomain.AlertRule) error { return nil }
func (m *mockAlertDAO) GetRuleByID(_ context.Context, _ int64) (alertdomain.AlertRule, error) {
	return alertdomain.AlertRule{}, nil
}
func (m *mockAlertDAO) ListRules(_ context.Context, filter alertdomain.AlertRuleFilter) ([]alertdomain.AlertRule, int64, error) {
	if m.listRulesFn != nil {
		return m.listRulesFn(nil, filter)
	}
	return nil, 0, nil
}
func (m *mockAlertDAO) DeleteRule(_ context.Context, _ int64) error { return nil }
func (m *mockAlertDAO) CreateEvent(_ context.Context, event alertdomain.AlertEvent) (int64, error) {
	m.emittedEvents = append(m.emittedEvents, event)
	return int64(len(m.emittedEvents)), nil
}
func (m *mockAlertDAO) UpdateEventStatus(_ context.Context, _ int64, _ alertdomain.EventStatus) error {
	return nil
}
func (m *mockAlertDAO) ListEvents(_ context.Context, _ alertdomain.AlertEventFilter) ([]alertdomain.AlertEvent, int64, error) {
	return nil, 0, nil
}
func (m *mockAlertDAO) GetPendingEvents(_ context.Context, _ int) ([]alertdomain.AlertEvent, error) {
	return nil, nil
}
func (m *mockAlertDAO) IncrementRetry(_ context.Context, _ int64) error { return nil }
func (m *mockAlertDAO) CreateChannel(_ context.Context, _ alertdomain.NotificationChannel) (int64, error) {
	return 0, nil
}
func (m *mockAlertDAO) UpdateChannel(_ context.Context, _ alertdomain.NotificationChannel) error {
	return nil
}
func (m *mockAlertDAO) GetChannelByID(_ context.Context, _ int64) (alertdomain.NotificationChannel, error) {
	return alertdomain.NotificationChannel{}, nil
}
func (m *mockAlertDAO) ListChannels(_ context.Context, _ alertdomain.ChannelFilter) ([]alertdomain.NotificationChannel, int64, error) {
	return nil, 0, nil
}
func (m *mockAlertDAO) DeleteChannel(_ context.Context, _ int64) error { return nil }
func (m *mockAlertDAO) GetChannelsByIDs(_ context.Context, _ []int64) ([]alertdomain.NotificationChannel, error) {
	return nil, nil
}
func (m *mockAlertDAO) InitIndexes(_ context.Context) error { return nil }

func setupTestService(t *testing.T, budgetDAO *mockBudgetDAO, billDAO *mockBillDAO, alertDAO *mockAlertDAO) *BudgetService {
	t.Helper()
	logger := elog.DefaultLogger
	alertSvc := alertservice.NewAlertService(alertDAO, logger)
	return NewBudgetService(budgetDAO, billDAO, alertSvc, logger)
}

func TestCreateBudget_Success(t *testing.T) {
	var created costdomain.BudgetRule
	budgetDAO := &mockBudgetDAO{
		createFn: func(_ context.Context, b costdomain.BudgetRule) (int64, error) {
			created = b
			return 1, nil
		},
	}
	svc := setupTestService(t, budgetDAO, &mockBillDAO{}, &mockAlertDAO{})
	id, err := svc.CreateBudget(context.Background(), costdomain.BudgetRule{
		Name: "Monthly AWS Budget", AmountLimit: 10000,
		ScopeType: "provider", ScopeValue: "aws",
		Thresholds: []float64{50, 80, 100}, TenantID: "tenant1",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), id)
	assert.Equal(t, "active", created.Status)
	assert.Equal(t, "monthly", created.Period)
	assert.NotNil(t, created.NotifiedAt)
}

func TestCreateBudget_EmptyName(t *testing.T) {
	svc := setupTestService(t, &mockBudgetDAO{}, &mockBillDAO{}, &mockAlertDAO{})
	_, err := svc.CreateBudget(context.Background(), costdomain.BudgetRule{AmountLimit: 10000, Thresholds: []float64{50}})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name")
}

func TestCreateBudget_InvalidAmountLimit(t *testing.T) {
	svc := setupTestService(t, &mockBudgetDAO{}, &mockBillDAO{}, &mockAlertDAO{})
	_, err := svc.CreateBudget(context.Background(), costdomain.BudgetRule{Name: "Test", AmountLimit: -100, Thresholds: []float64{50}})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "positive")
}

func TestCreateBudget_InvalidThresholds(t *testing.T) {
	svc := setupTestService(t, &mockBudgetDAO{}, &mockBillDAO{}, &mockAlertDAO{})
	_, err := svc.CreateBudget(context.Background(), costdomain.BudgetRule{Name: "T", AmountLimit: 1000, Thresholds: []float64{0}})
	assert.Error(t, err)
	_, err = svc.CreateBudget(context.Background(), costdomain.BudgetRule{Name: "T", AmountLimit: 1000, Thresholds: []float64{150}})
	assert.Error(t, err)
	_, err = svc.CreateBudget(context.Background(), costdomain.BudgetRule{Name: "T", AmountLimit: 1000, Thresholds: []float64{80, 50, 100}})
	assert.Error(t, err)
}

func TestGetBudgetProgress(t *testing.T) {
	budgetDAO := &mockBudgetDAO{
		getByIDFn: func(_ context.Context, id int64) (costdomain.BudgetRule, error) {
			return costdomain.BudgetRule{ID: id, Name: "Test Budget", AmountLimit: 10000, ScopeType: "all", TenantID: "t1", Status: "active"}, nil
		},
	}
	billDAO := &mockBillDAO{sumAmountFn: func(_ context.Context, _ repository.UnifiedBillFilter) (float64, error) { return 7500, nil }}
	svc := setupTestService(t, budgetDAO, billDAO, &mockAlertDAO{})
	progress, err := svc.GetBudgetProgress(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), progress.BudgetID)
	assert.Equal(t, 10000.0, progress.AmountLimit)
	assert.Equal(t, 7500.0, progress.CurrentSpend)
	assert.Equal(t, 2500.0, progress.RemainingAmount)
	assert.Equal(t, 75.0, progress.UsagePercent)
}

func TestGetBudgetProgress_OverBudget(t *testing.T) {
	budgetDAO := &mockBudgetDAO{
		getByIDFn: func(_ context.Context, _ int64) (costdomain.BudgetRule, error) {
			return costdomain.BudgetRule{ID: 1, Name: "T", AmountLimit: 1000, ScopeType: "all", TenantID: "t1", Status: "active"}, nil
		},
	}
	billDAO := &mockBillDAO{sumAmountFn: func(_ context.Context, _ repository.UnifiedBillFilter) (float64, error) { return 1500, nil }}
	svc := setupTestService(t, budgetDAO, billDAO, &mockAlertDAO{})
	progress, err := svc.GetBudgetProgress(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, 0.0, progress.RemainingAmount)
	assert.Equal(t, 150.0, progress.UsagePercent)
}

func TestCheckBudgets_TriggersAlert(t *testing.T) {
	alertDAO := &mockAlertDAO{listRulesFn: func(_ context.Context, filter alertdomain.AlertRuleFilter) ([]alertdomain.AlertRule, int64, error) {
		return []alertdomain.AlertRule{{ID: 1, Type: filter.Type, Enabled: true}}, 1, nil
	}}
	budgetDAO := &mockBudgetDAO{listActiveFn: func(_ context.Context, _ string) ([]costdomain.BudgetRule, error) {
		return []costdomain.BudgetRule{{ID: 1, Name: "Test", AmountLimit: 10000, ScopeType: "all", Thresholds: []float64{50, 80, 100}, TenantID: "t1", Status: "active"}}, nil
	}}
	billDAO := &mockBillDAO{sumAmountFn: func(_ context.Context, _ repository.UnifiedBillFilter) (float64, error) { return 8500, nil }}
	var notifiedAt map[string]time.Time
	budgetDAO.updateNotifiedFn = func(_ context.Context, _ int64, na map[string]time.Time) error { notifiedAt = na; return nil }
	svc := setupTestService(t, budgetDAO, billDAO, alertDAO)
	err := svc.CheckBudgets(context.Background(), "t1")
	require.NoError(t, err)
	assert.Len(t, alertDAO.emittedEvents, 2) // 50% and 80% thresholds
	assert.NotNil(t, notifiedAt)
	assert.Contains(t, notifiedAt, "50")
	assert.Contains(t, notifiedAt, "80")
}

func TestCheckBudgets_NoAlertBelowThreshold(t *testing.T) {
	alertDAO := &mockAlertDAO{}
	budgetDAO := &mockBudgetDAO{listActiveFn: func(_ context.Context, _ string) ([]costdomain.BudgetRule, error) {
		return []costdomain.BudgetRule{{ID: 1, Name: "T", AmountLimit: 10000, ScopeType: "all", Thresholds: []float64{50, 80, 100}, TenantID: "t1", Status: "active"}}, nil
	}}
	billDAO := &mockBillDAO{sumAmountFn: func(_ context.Context, _ repository.UnifiedBillFilter) (float64, error) { return 3000, nil }}
	svc := setupTestService(t, budgetDAO, billDAO, alertDAO)
	err := svc.CheckBudgets(context.Background(), "t1")
	require.NoError(t, err)
	assert.Empty(t, alertDAO.emittedEvents)
}

func TestCheckBudgets_SkipsAlreadyNotified(t *testing.T) {
	alertDAO := &mockAlertDAO{listRulesFn: func(_ context.Context, filter alertdomain.AlertRuleFilter) ([]alertdomain.AlertRule, int64, error) {
		return []alertdomain.AlertRule{{ID: 1, Type: filter.Type, Enabled: true}}, 1, nil
	}}
	budgetDAO := &mockBudgetDAO{listActiveFn: func(_ context.Context, _ string) ([]costdomain.BudgetRule, error) {
		return []costdomain.BudgetRule{{
			ID: 1, Name: "T", AmountLimit: 10000, ScopeType: "all", Thresholds: []float64{50, 80},
			NotifiedAt: map[string]time.Time{"50": time.Now()}, TenantID: "t1", Status: "active",
		}}, nil
	}}
	billDAO := &mockBillDAO{sumAmountFn: func(_ context.Context, _ repository.UnifiedBillFilter) (float64, error) { return 8500, nil }}
	budgetDAO.updateNotifiedFn = func(_ context.Context, _ int64, _ map[string]time.Time) error { return nil }
	svc := setupTestService(t, budgetDAO, billDAO, alertDAO)
	err := svc.CheckBudgets(context.Background(), "t1")
	require.NoError(t, err)
	assert.Len(t, alertDAO.emittedEvents, 1) // Only 80% (50% already notified)
}

func TestDeactivateBudgetsByScope(t *testing.T) {
	alertDAO := &mockAlertDAO{listRulesFn: func(_ context.Context, filter alertdomain.AlertRuleFilter) ([]alertdomain.AlertRule, int64, error) {
		return []alertdomain.AlertRule{{ID: 1, Type: filter.Type, Enabled: true}}, 1, nil
	}}
	var deactivatedIDs []int64
	budgetDAO := &mockBudgetDAO{
		listFn: func(_ context.Context, _ repository.BudgetFilter) ([]costdomain.BudgetRule, error) {
			return []costdomain.BudgetRule{
				{ID: 1, Name: "A", ScopeType: "account", ScopeValue: "123", TenantID: "t1"},
				{ID: 2, Name: "B", ScopeType: "account", ScopeValue: "456", TenantID: "t1"},
			}, nil
		},
		updateStatusFn: func(_ context.Context, id int64, status string) error {
			assert.Equal(t, "inactive", status)
			deactivatedIDs = append(deactivatedIDs, id)
			return nil
		},
	}
	svc := setupTestService(t, budgetDAO, &mockBillDAO{}, alertDAO)
	err := svc.DeactivateBudgetsByScope(context.Background(), "t1", "account", "123")
	require.NoError(t, err)
	assert.Equal(t, []int64{1}, deactivatedIDs)
	assert.Len(t, alertDAO.emittedEvents, 1)
}

func TestThresholdSeverity(t *testing.T) {
	assert.Equal(t, alertdomain.SeverityInfo, thresholdSeverity(50))
	assert.Equal(t, alertdomain.SeverityInfo, thresholdSeverity(79))
	assert.Equal(t, alertdomain.SeverityWarning, thresholdSeverity(80))
	assert.Equal(t, alertdomain.SeverityWarning, thresholdSeverity(99))
	assert.Equal(t, alertdomain.SeverityCritical, thresholdSeverity(100))
}

func TestIsInCurrentMonth(t *testing.T) {
	now := time.Now()
	assert.True(t, isInCurrentMonth(now))
	assert.True(t, isInCurrentMonth(time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)))
	assert.False(t, isInCurrentMonth(now.AddDate(0, -1, 0)))
	assert.False(t, isInCurrentMonth(now.AddDate(-1, 0, 0)))
}

func TestDeleteBudget(t *testing.T) {
	deleted := false
	budgetDAO := &mockBudgetDAO{deleteFn: func(_ context.Context, id int64) error { assert.Equal(t, int64(42), id); deleted = true; return nil }}
	svc := setupTestService(t, budgetDAO, &mockBillDAO{}, &mockAlertDAO{})
	err := svc.DeleteBudget(context.Background(), 42)
	require.NoError(t, err)
	assert.True(t, deleted)
}

func TestUpdateBudget_Validation(t *testing.T) {
	svc := setupTestService(t, &mockBudgetDAO{}, &mockBillDAO{}, &mockAlertDAO{})
	err := svc.UpdateBudget(context.Background(), costdomain.BudgetRule{Name: "", AmountLimit: 1000})
	assert.Error(t, err)
	err = svc.UpdateBudget(context.Background(), costdomain.BudgetRule{Name: "Test", AmountLimit: 0})
	assert.Error(t, err)
}
