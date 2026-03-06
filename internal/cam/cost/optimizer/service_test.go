package optimizer

import (
	"context"
	"testing"
	"time"

	costdomain "github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
)

// ========== Mock DAOs ==========

type mockOptimizerDAO struct {
	createFn             func(ctx context.Context, rec costdomain.Recommendation) (int64, error)
	createBatchFn        func(ctx context.Context, recs []costdomain.Recommendation) (int64, error)
	getByIDFn            func(ctx context.Context, id int64) (costdomain.Recommendation, error)
	updateFn             func(ctx context.Context, rec costdomain.Recommendation) error
	listFn               func(ctx context.Context, filter repository.RecommendationFilter) ([]costdomain.Recommendation, error)
	countFn              func(ctx context.Context, filter repository.RecommendationFilter) (int64, error)
	findByResourceTypeFn func(ctx context.Context, tenantID, resourceID, recType string) (costdomain.Recommendation, error)
	createdRecs          []costdomain.Recommendation
	updatedRecs          []costdomain.Recommendation
}

func (m *mockOptimizerDAO) Create(ctx context.Context, rec costdomain.Recommendation) (int64, error) {
	m.createdRecs = append(m.createdRecs, rec)
	if m.createFn != nil {
		return m.createFn(ctx, rec)
	}
	return 1, nil
}

func (m *mockOptimizerDAO) CreateBatch(ctx context.Context, recs []costdomain.Recommendation) (int64, error) {
	m.createdRecs = append(m.createdRecs, recs...)
	if m.createBatchFn != nil {
		return m.createBatchFn(ctx, recs)
	}
	return int64(len(recs)), nil
}

func (m *mockOptimizerDAO) GetByID(ctx context.Context, id int64) (costdomain.Recommendation, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return costdomain.Recommendation{}, nil
}

func (m *mockOptimizerDAO) Update(ctx context.Context, rec costdomain.Recommendation) error {
	m.updatedRecs = append(m.updatedRecs, rec)
	if m.updateFn != nil {
		return m.updateFn(ctx, rec)
	}
	return nil
}

func (m *mockOptimizerDAO) List(ctx context.Context, filter repository.RecommendationFilter) ([]costdomain.Recommendation, error) {
	if m.listFn != nil {
		return m.listFn(ctx, filter)
	}
	return nil, nil
}

func (m *mockOptimizerDAO) Count(ctx context.Context, filter repository.RecommendationFilter) (int64, error) {
	if m.countFn != nil {
		return m.countFn(ctx, filter)
	}
	return 0, nil
}

func (m *mockOptimizerDAO) FindByResourceAndType(ctx context.Context, tenantID, resourceID, recType string) (costdomain.Recommendation, error) {
	if m.findByResourceTypeFn != nil {
		return m.findByResourceTypeFn(ctx, tenantID, resourceID, recType)
	}
	return costdomain.Recommendation{}, mongo.ErrNoDocuments
}

type mockBillDAO struct {
	listUnifiedBillsFn func(ctx context.Context, filter repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error)
}

func (m *mockBillDAO) ListUnifiedBills(ctx context.Context, filter repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
	if m.listUnifiedBillsFn != nil {
		return m.listUnifiedBillsFn(ctx, filter)
	}
	return nil, nil
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
func (m *mockBillDAO) CountUnifiedBills(_ context.Context, _ repository.UnifiedBillFilter) (int64, error) {
	return 0, nil
}
func (m *mockBillDAO) AggregateByField(_ context.Context, _, _, _, _ string) ([]repository.AggregateResult, error) {
	return nil, nil
}
func (m *mockBillDAO) AggregateDailyAmount(_ context.Context, _, _, _ string, _ repository.UnifiedBillFilter) ([]repository.DailyAmount, error) {
	return nil, nil
}
func (m *mockBillDAO) SumAmount(_ context.Context, _ repository.UnifiedBillFilter) (float64, error) {
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

// ========== Test Setup ==========

func setupTestService(t *testing.T, optDAO *mockOptimizerDAO, billDAO *mockBillDAO) *OptimizerService {
	t.Helper()
	logger := elog.DefaultLogger
	return NewOptimizerService(optDAO, billDAO, logger)
}

// ========== Tests ==========

// generateComputeBills creates compute bills for a resource spanning consecutive days
func generateComputeBills(resourceID, resourceName, provider, tenantID string, accountID int64, days int, dailyAmount float64) []costdomain.UnifiedBill {
	now := time.Now()
	var bills []costdomain.UnifiedBill
	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -days+i).Format("2006-01-02")
		bills = append(bills, costdomain.UnifiedBill{
			ID:           int64(i + 1),
			Provider:     provider,
			AccountID:    accountID,
			ResourceID:   resourceID,
			ResourceName: resourceName,
			ServiceType:  "compute",
			Region:       "cn-beijing",
			Amount:       dailyAmount,
			AmountCNY:    dailyAmount,
			Currency:     "CNY",
			ChargeType:   "postpaid",
			TenantID:     tenantID,
			BillingDate:  date,
		})
	}
	return bills
}

func TestGenerateRecommendations_DownsizeCandidates(t *testing.T) {
	optDAO := &mockOptimizerDAO{}
	// Generate 8 days of compute bills for a resource (>= 7 days threshold)
	computeBills := generateComputeBills("i-123", "test-ecs", "aliyun", "tenant1", 100, 8, 10.0)

	billDAO := &mockBillDAO{
		listUnifiedBillsFn: func(_ context.Context, filter repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
			if filter.ServiceType == "compute" {
				return computeBills, nil
			}
			return nil, nil
		},
	}

	svc := setupTestService(t, optDAO, billDAO)
	err := svc.GenerateRecommendations(context.Background(), "tenant1")
	require.NoError(t, err)

	// Should have created a downsize recommendation
	found := false
	for _, rec := range optDAO.createdRecs {
		if rec.Type == RecTypeDownsize && rec.ResourceID == "i-123" {
			found = true
			assert.Equal(t, "aliyun", rec.Provider)
			assert.Equal(t, int64(100), rec.AccountID)
			assert.Equal(t, "test-ecs", rec.ResourceName)
			assert.Equal(t, StatusPending, rec.Status)
			assert.Greater(t, rec.EstimatedSaving, 0.0)
			assert.NotEmpty(t, rec.Reason)
			assert.Equal(t, "tenant1", rec.TenantID)
		}
	}
	assert.True(t, found, "expected downsize recommendation for i-123")
}

func TestGenerateRecommendations_UnattachedDiskCandidates(t *testing.T) {
	optDAO := &mockOptimizerDAO{}
	now := time.Now()

	// Storage bills for a disk that has no corresponding compute resource
	storageBills := []costdomain.UnifiedBill{
		{
			ID: 1, Provider: "aliyun", AccountID: 100,
			ResourceID: "d-disk-001", ResourceName: "orphan-disk",
			ServiceType: "storage", Region: "cn-shanghai",
			Amount: 5.0, AmountCNY: 5.0, Currency: "CNY",
			TenantID: "tenant1", BillingDate: now.AddDate(0, 0, -1).Format("2006-01-02"),
		},
		{
			ID: 2, Provider: "aliyun", AccountID: 100,
			ResourceID: "d-disk-001", ResourceName: "orphan-disk",
			ServiceType: "storage", Region: "cn-shanghai",
			Amount: 5.0, AmountCNY: 5.0, Currency: "CNY",
			TenantID: "tenant1", BillingDate: now.AddDate(0, 0, -2).Format("2006-01-02"),
		},
	}

	billDAO := &mockBillDAO{
		listUnifiedBillsFn: func(_ context.Context, filter repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
			if filter.ServiceType == "storage" {
				return storageBills, nil
			}
			if filter.ServiceType == "compute" {
				// No compute resources → disk is unattached
				return nil, nil
			}
			return nil, nil
		},
	}

	svc := setupTestService(t, optDAO, billDAO)
	err := svc.GenerateRecommendations(context.Background(), "tenant1")
	require.NoError(t, err)

	found := false
	for _, rec := range optDAO.createdRecs {
		if rec.Type == RecTypeReleaseDisk && rec.ResourceID == "d-disk-001" {
			found = true
			assert.Equal(t, "orphan-disk", rec.ResourceName)
			assert.Equal(t, StatusPending, rec.Status)
			assert.Greater(t, rec.EstimatedSaving, 0.0)
			assert.NotEmpty(t, rec.Reason)
		}
	}
	assert.True(t, found, "expected release_disk recommendation for d-disk-001")
}

func TestGenerateRecommendations_ConvertPrepaidCandidates(t *testing.T) {
	optDAO := &mockOptimizerDAO{}
	// Generate 31 days of postpaid bills (>= 30 days threshold)
	now := time.Now()
	var bills []costdomain.UnifiedBill
	for i := 0; i < 31; i++ {
		date := now.AddDate(0, 0, -31+i).Format("2006-01-02")
		bills = append(bills, costdomain.UnifiedBill{
			ID: int64(i + 1), Provider: "aws", AccountID: 200,
			ResourceID: "i-aws-001", ResourceName: "long-running-ec2",
			ServiceType: "compute", Region: "us-east-1",
			Amount: 20.0, AmountCNY: 140.0, Currency: "USD",
			ChargeType: "postpaid", TenantID: "tenant1",
			BillingDate: date,
		})
	}

	billDAO := &mockBillDAO{
		listUnifiedBillsFn: func(_ context.Context, filter repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
			if filter.ServiceType == "compute" {
				return bills, nil
			}
			if filter.ServiceType == "storage" {
				return nil, nil
			}
			// For the on-demand detection (no service type filter, just postpaid)
			return bills, nil
		},
	}

	svc := setupTestService(t, optDAO, billDAO)
	err := svc.GenerateRecommendations(context.Background(), "tenant1")
	require.NoError(t, err)

	found := false
	for _, rec := range optDAO.createdRecs {
		if rec.Type == RecTypeConvertPrepaid && rec.ResourceID == "i-aws-001" {
			found = true
			assert.Equal(t, "aws", rec.Provider)
			assert.Equal(t, "long-running-ec2", rec.ResourceName)
			assert.Equal(t, StatusPending, rec.Status)
			assert.Greater(t, rec.EstimatedSaving, 0.0)
			assert.NotEmpty(t, rec.Reason)
		}
	}
	assert.True(t, found, "expected convert_prepaid recommendation for i-aws-001")
}

func TestGenerateRecommendations_SkipDismissedResources(t *testing.T) {
	now := time.Now()
	expiry := now.Add(24 * time.Hour) // still within dismiss window

	optDAO := &mockOptimizerDAO{
		findByResourceTypeFn: func(_ context.Context, tenantID, resourceID, recType string) (costdomain.Recommendation, error) {
			if resourceID == "i-dismissed" {
				return costdomain.Recommendation{
					ID:            1,
					ResourceID:    "i-dismissed",
					Type:          RecTypeDownsize,
					Status:        StatusDismissed,
					DismissedAt:   &now,
					DismissExpiry: &expiry,
					TenantID:      tenantID,
				}, nil
			}
			return costdomain.Recommendation{}, mongo.ErrNoDocuments
		},
	}

	// Generate bills for dismissed resource
	computeBills := generateComputeBills("i-dismissed", "dismissed-ecs", "aliyun", "tenant1", 100, 8, 10.0)

	billDAO := &mockBillDAO{
		listUnifiedBillsFn: func(_ context.Context, filter repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
			if filter.ServiceType == "compute" {
				return computeBills, nil
			}
			return nil, nil
		},
	}

	svc := setupTestService(t, optDAO, billDAO)
	err := svc.GenerateRecommendations(context.Background(), "tenant1")
	require.NoError(t, err)

	// Should NOT have created any recommendations (dismissed resource filtered out)
	for _, rec := range optDAO.createdRecs {
		assert.NotEqual(t, "i-dismissed", rec.ResourceID, "dismissed resource should be filtered out")
	}
}

func TestListRecommendations(t *testing.T) {
	expected := []costdomain.Recommendation{
		{ID: 1, Type: RecTypeDownsize, ResourceID: "i-1", TenantID: "t1", EstimatedSaving: 100},
		{ID: 2, Type: RecTypeReleaseDisk, ResourceID: "d-1", TenantID: "t1", EstimatedSaving: 50},
	}
	optDAO := &mockOptimizerDAO{
		listFn: func(_ context.Context, f repository.RecommendationFilter) ([]costdomain.Recommendation, error) {
			assert.Equal(t, "t1", f.TenantID)
			return expected, nil
		},
		countFn: func(_ context.Context, f repository.RecommendationFilter) (int64, error) {
			return 2, nil
		},
	}

	svc := setupTestService(t, optDAO, &mockBillDAO{})
	recs, count, err := svc.ListRecommendations(context.Background(), "t1", repository.RecommendationFilter{})
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
	assert.Len(t, recs, 2)
	assert.Equal(t, RecTypeDownsize, recs[0].Type)
	assert.Equal(t, RecTypeReleaseDisk, recs[1].Type)
}

func TestDismissRecommendation(t *testing.T) {
	optDAO := &mockOptimizerDAO{
		getByIDFn: func(_ context.Context, id int64) (costdomain.Recommendation, error) {
			return costdomain.Recommendation{
				ID:         id,
				Type:       RecTypeDownsize,
				ResourceID: "i-123",
				Status:     StatusPending,
				TenantID:   "t1",
			}, nil
		},
	}

	svc := setupTestService(t, optDAO, &mockBillDAO{})
	err := svc.DismissRecommendation(context.Background(), 42)
	require.NoError(t, err)

	require.Len(t, optDAO.updatedRecs, 1)
	updated := optDAO.updatedRecs[0]
	assert.Equal(t, int64(42), updated.ID)
	assert.Equal(t, StatusDismissed, updated.Status)
	assert.NotNil(t, updated.DismissedAt)
	assert.NotNil(t, updated.DismissExpiry)

	// Verify dismiss expiry is ~30 days from now
	expectedExpiry := time.Now().Add(dismissDuration)
	assert.WithinDuration(t, expectedExpiry, *updated.DismissExpiry, 5*time.Second)
	assert.WithinDuration(t, time.Now(), *updated.DismissedAt, 5*time.Second)
}

func TestDismissRecommendation_AlreadyDismissed(t *testing.T) {
	now := time.Now()
	expiry := now.Add(24 * time.Hour)

	optDAO := &mockOptimizerDAO{
		getByIDFn: func(_ context.Context, id int64) (costdomain.Recommendation, error) {
			return costdomain.Recommendation{
				ID:            id,
				Type:          RecTypeDownsize,
				ResourceID:    "i-123",
				Status:        StatusDismissed,
				DismissedAt:   &now,
				DismissExpiry: &expiry,
				TenantID:      "t1",
			}, nil
		},
	}

	svc := setupTestService(t, optDAO, &mockBillDAO{})
	err := svc.DismissRecommendation(context.Background(), 42)
	require.NoError(t, err)

	// Should still update with new dismiss time
	require.Len(t, optDAO.updatedRecs, 1)
	updated := optDAO.updatedRecs[0]
	assert.Equal(t, StatusDismissed, updated.Status)
	assert.NotNil(t, updated.DismissedAt)
	assert.NotNil(t, updated.DismissExpiry)
	// New expiry should be ~30 days from now (refreshed)
	newExpiry := time.Now().Add(dismissDuration)
	assert.WithinDuration(t, newExpiry, *updated.DismissExpiry, 5*time.Second)
}
