package anomaly

import (
	"context"
	"testing"

	alertdomain "github.com/Havens-blog/e-cam-service/internal/alert/domain"
	alertservice "github.com/Havens-blog/e-cam-service/internal/alert/service"
	costdomain "github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========== Mock DAOs ==========

type mockAnomalyDAO struct {
	createFn      func(ctx context.Context, anomaly costdomain.CostAnomaly) (int64, error)
	createBatchFn func(ctx context.Context, anomalies []costdomain.CostAnomaly) (int64, error)
	getByIDFn     func(ctx context.Context, id int64) (costdomain.CostAnomaly, error)
	listFn        func(ctx context.Context, filter repository.AnomalyFilter) ([]costdomain.CostAnomaly, error)
	countFn       func(ctx context.Context, filter repository.AnomalyFilter) (int64, error)
	// track created anomalies
	createdAnomalies []costdomain.CostAnomaly
}

func (m *mockAnomalyDAO) Create(ctx context.Context, anomaly costdomain.CostAnomaly) (int64, error) {
	m.createdAnomalies = append(m.createdAnomalies, anomaly)
	if m.createFn != nil {
		return m.createFn(ctx, anomaly)
	}
	return 1, nil
}

func (m *mockAnomalyDAO) CreateBatch(ctx context.Context, anomalies []costdomain.CostAnomaly) (int64, error) {
	m.createdAnomalies = append(m.createdAnomalies, anomalies...)
	if m.createBatchFn != nil {
		return m.createBatchFn(ctx, anomalies)
	}
	return int64(len(anomalies)), nil
}

func (m *mockAnomalyDAO) GetByID(ctx context.Context, id int64) (costdomain.CostAnomaly, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return costdomain.CostAnomaly{}, nil
}

func (m *mockAnomalyDAO) List(ctx context.Context, filter repository.AnomalyFilter) ([]costdomain.CostAnomaly, error) {
	if m.listFn != nil {
		return m.listFn(ctx, filter)
	}
	return nil, nil
}

func (m *mockAnomalyDAO) Count(ctx context.Context, filter repository.AnomalyFilter) (int64, error) {
	if m.countFn != nil {
		return m.countFn(ctx, filter)
	}
	return 0, nil
}

type mockBillDAO struct {
	aggregateByFieldFn func(ctx context.Context, tenantID, field, startDate, endDate string) ([]repository.AggregateResult, error)
	aggregateDailyFn   func(ctx context.Context, tenantID, startDate, endDate string, filter repository.UnifiedBillFilter) ([]repository.DailyAmount, error)
	sumAmountFn        func(ctx context.Context, filter repository.UnifiedBillFilter) (float64, error)
}

func (m *mockBillDAO) AggregateByField(ctx context.Context, tenantID, field, startDate, endDate string) ([]repository.AggregateResult, error) {
	if m.aggregateByFieldFn != nil {
		return m.aggregateByFieldFn(ctx, tenantID, field, startDate, endDate)
	}
	return nil, nil
}

func (m *mockBillDAO) AggregateDailyAmount(ctx context.Context, tenantID, startDate, endDate string, filter repository.UnifiedBillFilter) ([]repository.DailyAmount, error) {
	if m.aggregateDailyFn != nil {
		return m.aggregateDailyFn(ctx, tenantID, startDate, endDate, filter)
	}
	return nil, nil
}

func (m *mockBillDAO) SumAmount(ctx context.Context, filter repository.UnifiedBillFilter) (float64, error) {
	if m.sumAmountFn != nil {
		return m.sumAmountFn(ctx, filter)
	}
	return 0, nil
}

// Stub methods for BillDAO interface
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

// ========== Test Setup ==========

func setupTestService(t *testing.T, anomalyDAO *mockAnomalyDAO, billDAO *mockBillDAO, alertDAO *mockAlertDAO) *AnomalyService {
	t.Helper()
	logger := elog.DefaultLogger
	alertSvc := alertservice.NewAlertService(alertDAO, logger)
	return NewAnomalyService(anomalyDAO, billDAO, alertSvc, logger)
}

// ========== Tests ==========

func TestDetectAnomalies_WithAnomaliesFound(t *testing.T) {
	anomalyDAO := &mockAnomalyDAO{}
	// Baseline: 30 days, total 3000 per dimension value → daily avg = 100
	// Current day: 250 → deviation = 150% → should trigger warning (>100%)
	billDAO := &mockBillDAO{
		aggregateByFieldFn: func(_ context.Context, tenantID, field, startDate, endDate string) ([]repository.AggregateResult, error) {
			if startDate == "2024-01-15" && endDate == "2024-01-15" {
				// Current day costs
				return []repository.AggregateResult{
					{Key: "ecs", AmountCNY: 250},
				}, nil
			}
			// Baseline period: 30 days total = 3000 → avg = 100/day
			return []repository.AggregateResult{
				{Key: "ecs", AmountCNY: 3000},
			}, nil
		},
		aggregateDailyFn: func(_ context.Context, _, _, _ string, _ repository.UnifiedBillFilter) ([]repository.DailyAmount, error) {
			return make([]repository.DailyAmount, 30), nil
		},
	}
	alertDAO := &mockAlertDAO{
		listRulesFn: func(_ context.Context, _ alertdomain.AlertRuleFilter) ([]alertdomain.AlertRule, int64, error) {
			return []alertdomain.AlertRule{{ID: 1, Enabled: true}}, 1, nil
		},
	}

	svc := setupTestService(t, anomalyDAO, billDAO, alertDAO)

	err := svc.DetectAnomalies(context.Background(), "tenant1", "2024-01-15")
	require.NoError(t, err)

	// Should have created anomalies
	assert.NotEmpty(t, anomalyDAO.createdAnomalies)

	// Verify anomaly fields
	found := false
	for _, a := range anomalyDAO.createdAnomalies {
		if a.DimensionValue == "ecs" {
			found = true
			assert.Equal(t, "2024-01-15", a.AnomalyDate)
			assert.Equal(t, 250.0, a.ActualAmount)
			assert.Equal(t, 100.0, a.BaselineAmount)
			assert.Equal(t, 150.0, a.DeviationPct)
			assert.Equal(t, "warning", a.Severity)
			assert.NotEmpty(t, a.PossibleCause)
			assert.Equal(t, "tenant1", a.TenantID)
			break
		}
	}
	assert.True(t, found, "expected anomaly for 'ecs' dimension value")
}

func TestDetectAnomalies_NoAnomalies(t *testing.T) {
	anomalyDAO := &mockAnomalyDAO{}
	// Baseline avg = 100, current = 120 → deviation = 20% < 50% threshold
	billDAO := &mockBillDAO{
		aggregateByFieldFn: func(_ context.Context, _, field, startDate, endDate string) ([]repository.AggregateResult, error) {
			if startDate == endDate {
				return []repository.AggregateResult{
					{Key: "ecs", AmountCNY: 120},
				}, nil
			}
			return []repository.AggregateResult{
				{Key: "ecs", AmountCNY: 3000},
			}, nil
		},
		aggregateDailyFn: func(_ context.Context, _, _, _ string, _ repository.UnifiedBillFilter) ([]repository.DailyAmount, error) {
			return make([]repository.DailyAmount, 30), nil
		},
	}
	alertDAO := &mockAlertDAO{}

	svc := setupTestService(t, anomalyDAO, billDAO, alertDAO)

	err := svc.DetectAnomalies(context.Background(), "tenant1", "2024-01-15")
	require.NoError(t, err)

	// No anomalies should be created
	assert.Empty(t, anomalyDAO.createdAnomalies)
	assert.Empty(t, alertDAO.emittedEvents)
}

func TestDetectAnomalies_SeverityLevels(t *testing.T) {
	tests := []struct {
		name          string
		currentAmount float64
		baselineTotal float64
		expectedSev   string
		expectAnomaly bool
	}{
		{
			name:          "critical: >200% deviation",
			currentAmount: 350,
			baselineTotal: 3000, // avg=100, deviation=250%
			expectedSev:   "critical",
			expectAnomaly: true,
		},
		{
			name:          "warning: >100% deviation",
			currentAmount: 220,
			baselineTotal: 3000, // avg=100, deviation=120%
			expectedSev:   "warning",
			expectAnomaly: true,
		},
		{
			name:          "info: >threshold deviation",
			currentAmount: 160,
			baselineTotal: 3000, // avg=100, deviation=60%
			expectedSev:   "info",
			expectAnomaly: true,
		},
		{
			name:          "no anomaly: within threshold",
			currentAmount: 130,
			baselineTotal: 3000, // avg=100, deviation=30%
			expectedSev:   "",
			expectAnomaly: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			anomalyDAO := &mockAnomalyDAO{}
			billDAO := &mockBillDAO{
				aggregateByFieldFn: func(_ context.Context, _, _, startDate, endDate string) ([]repository.AggregateResult, error) {
					if startDate == endDate {
						return []repository.AggregateResult{
							{Key: "ecs", AmountCNY: tt.currentAmount},
						}, nil
					}
					return []repository.AggregateResult{
						{Key: "ecs", AmountCNY: tt.baselineTotal},
					}, nil
				},
				aggregateDailyFn: func(_ context.Context, _, _, _ string, _ repository.UnifiedBillFilter) ([]repository.DailyAmount, error) {
					return make([]repository.DailyAmount, 30), nil
				},
			}
			alertDAO := &mockAlertDAO{
				listRulesFn: func(_ context.Context, _ alertdomain.AlertRuleFilter) ([]alertdomain.AlertRule, int64, error) {
					return []alertdomain.AlertRule{{ID: 1, Enabled: true}}, 1, nil
				},
			}

			svc := setupTestService(t, anomalyDAO, billDAO, alertDAO)
			err := svc.DetectAnomalies(context.Background(), "tenant1", "2024-01-15")
			require.NoError(t, err)

			if tt.expectAnomaly {
				assert.NotEmpty(t, anomalyDAO.createdAnomalies)
				found := false
				for _, a := range anomalyDAO.createdAnomalies {
					if a.DimensionValue == "ecs" {
						found = true
						assert.Equal(t, tt.expectedSev, a.Severity)
					}
				}
				assert.True(t, found)
			} else {
				// Check no anomaly for "ecs" was created
				for _, a := range anomalyDAO.createdAnomalies {
					assert.NotEqual(t, "ecs", a.DimensionValue)
				}
			}
		})
	}
}

func TestGetAnomalyEvents(t *testing.T) {
	expected := []costdomain.CostAnomaly{
		{ID: 1, Dimension: "service_type", Severity: "critical", TenantID: "t1"},
		{ID: 2, Dimension: "cloud_account", Severity: "warning", TenantID: "t1"},
	}
	anomalyDAO := &mockAnomalyDAO{
		listFn: func(_ context.Context, f repository.AnomalyFilter) ([]costdomain.CostAnomaly, error) {
			assert.Equal(t, "t1", f.TenantID)
			return expected, nil
		},
		countFn: func(_ context.Context, f repository.AnomalyFilter) (int64, error) {
			return 2, nil
		},
	}
	svc := setupTestService(t, anomalyDAO, &mockBillDAO{}, &mockAlertDAO{})

	anomalies, count, err := svc.GetAnomalyEvents(context.Background(), "t1", repository.AnomalyFilter{
		SortBy: "severity",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
	assert.Len(t, anomalies, 2)
	assert.Equal(t, "critical", anomalies[0].Severity)
}

func TestDetectAnomalies_AlertIntegration(t *testing.T) {
	anomalyDAO := &mockAnomalyDAO{}
	// Deviation > 200% → critical
	billDAO := &mockBillDAO{
		aggregateByFieldFn: func(_ context.Context, _, _, startDate, endDate string) ([]repository.AggregateResult, error) {
			if startDate == endDate {
				return []repository.AggregateResult{
					{Key: "account-1", AmountCNY: 500},
				}, nil
			}
			return []repository.AggregateResult{
				{Key: "account-1", AmountCNY: 3000}, // avg=100
			}, nil
		},
		aggregateDailyFn: func(_ context.Context, _, _, _ string, _ repository.UnifiedBillFilter) ([]repository.DailyAmount, error) {
			return make([]repository.DailyAmount, 30), nil
		},
	}
	alertDAO := &mockAlertDAO{
		listRulesFn: func(_ context.Context, _ alertdomain.AlertRuleFilter) ([]alertdomain.AlertRule, int64, error) {
			return []alertdomain.AlertRule{{ID: 1, Enabled: true}}, 1, nil
		},
	}

	svc := setupTestService(t, anomalyDAO, billDAO, alertDAO)
	err := svc.DetectAnomalies(context.Background(), "tenant1", "2024-01-15")
	require.NoError(t, err)

	// Verify EmitEvent was called
	assert.NotEmpty(t, alertDAO.emittedEvents)
	foundAlert := false
	for _, evt := range alertDAO.emittedEvents {
		if evt.Type == "cost_anomaly" {
			foundAlert = true
			assert.Equal(t, alertdomain.SeverityCritical, evt.Severity)
			assert.Equal(t, "tenant1", evt.TenantID)
			assert.Contains(t, evt.Title, "成本异常")
			assert.NotNil(t, evt.Content["dimension"])
			assert.NotNil(t, evt.Content["actual_amount"])
			assert.NotNil(t, evt.Content["baseline_amount"])
		}
	}
	assert.True(t, foundAlert, "expected cost_anomaly alert event")
}

func TestDetectAnomalies_ZeroBaseline(t *testing.T) {
	anomalyDAO := &mockAnomalyDAO{}
	// No baseline data → should skip
	billDAO := &mockBillDAO{
		aggregateByFieldFn: func(_ context.Context, _, _, startDate, endDate string) ([]repository.AggregateResult, error) {
			if startDate == endDate {
				return []repository.AggregateResult{
					{Key: "ecs", AmountCNY: 100},
				}, nil
			}
			// No baseline data
			return nil, nil
		},
		aggregateDailyFn: func(_ context.Context, _, _, _ string, _ repository.UnifiedBillFilter) ([]repository.DailyAmount, error) {
			return nil, nil
		},
	}
	alertDAO := &mockAlertDAO{}

	svc := setupTestService(t, anomalyDAO, billDAO, alertDAO)
	err := svc.DetectAnomalies(context.Background(), "tenant1", "2024-01-15")
	require.NoError(t, err)

	// No anomalies when baseline is zero/missing
	assert.Empty(t, anomalyDAO.createdAnomalies)
}

func TestDetectAnomalies_EmptyBills(t *testing.T) {
	anomalyDAO := &mockAnomalyDAO{}
	billDAO := &mockBillDAO{
		aggregateByFieldFn: func(_ context.Context, _, _, _, _ string) ([]repository.AggregateResult, error) {
			return nil, nil
		},
		aggregateDailyFn: func(_ context.Context, _, _, _ string, _ repository.UnifiedBillFilter) ([]repository.DailyAmount, error) {
			return nil, nil
		},
	}
	alertDAO := &mockAlertDAO{}

	svc := setupTestService(t, anomalyDAO, billDAO, alertDAO)
	err := svc.DetectAnomalies(context.Background(), "tenant1", "2024-01-15")
	require.NoError(t, err)

	assert.Empty(t, anomalyDAO.createdAnomalies)
	assert.Empty(t, alertDAO.emittedEvents)
}

func TestClassifySeverity(t *testing.T) {
	assert.Equal(t, "critical", classifySeverity(250))
	assert.Equal(t, "critical", classifySeverity(201))
	assert.Equal(t, "warning", classifySeverity(150))
	assert.Equal(t, "warning", classifySeverity(101))
	assert.Equal(t, "info", classifySeverity(100))
	assert.Equal(t, "info", classifySeverity(60))
	assert.Equal(t, "info", classifySeverity(1))
}

func TestSetThreshold(t *testing.T) {
	svc := NewAnomalyService(nil, nil, nil, elog.DefaultLogger)
	assert.Equal(t, defaultThresholdPct, svc.thresholdPct)

	svc.SetThreshold(30.0)
	assert.Equal(t, 30.0, svc.thresholdPct)
}
