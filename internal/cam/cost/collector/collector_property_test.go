package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/alicebob/miniredis/v2"
	"github.com/gotomicro/ego/core/elog"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// mockCollectLogDAO is a minimal mock for CollectLogDAO used in property tests.
type mockCollectLogDAO struct {
	lastSuccess *domain.CollectLog
	lastFailed  *domain.CollectLog
	successErr  error
	failedErr   error
}

func (m *mockCollectLogDAO) Create(_ context.Context, _ domain.CollectLog) (int64, error) {
	return 0, nil
}

func (m *mockCollectLogDAO) Update(_ context.Context, _ domain.CollectLog) error {
	return nil
}

func (m *mockCollectLogDAO) GetByID(_ context.Context, _ int64) (domain.CollectLog, error) {
	return domain.CollectLog{}, nil
}

func (m *mockCollectLogDAO) GetLastSuccess(_ context.Context, _ int64) (domain.CollectLog, error) {
	if m.successErr != nil {
		return domain.CollectLog{}, m.successErr
	}
	if m.lastSuccess != nil {
		return *m.lastSuccess, nil
	}
	return domain.CollectLog{}, errors.New("no success log")
}

func (m *mockCollectLogDAO) GetLastFailed(_ context.Context, _ int64) (domain.CollectLog, error) {
	if m.failedErr != nil {
		return domain.CollectLog{}, m.failedErr
	}
	if m.lastFailed != nil {
		return *m.lastFailed, nil
	}
	return domain.CollectLog{}, errors.New("no failed log")
}

func (m *mockCollectLogDAO) List(_ context.Context, _ repository.CollectLogFilter) ([]domain.CollectLog, error) {
	return nil, nil
}

func (m *mockCollectLogDAO) Count(_ context.Context, _ repository.CollectLogFilter) (int64, error) {
	return 0, nil
}

// newTestCollectorService creates a CollectorService with the given mock DAO for testing.
func newTestCollectorService(dao repository.CollectLogDAO) *CollectorService {
	return &CollectorService{
		collectLogDAO: dao,
		logger:        elog.DefaultLogger,
	}
}

// TestProperty2_IncrementalTimeRangeCalculation verifies that the incremental
// collection time range calculation satisfies the following properties:
// - With a previous success log: start == successLog.BillEnd, start < end
// - With a failed log (retry): start == failedLog.BillStart, start < end
// - No history: start == first day of current month (UTC), start < end
// - The calculated start time must always be before the end time
//
// **Validates: Requirements 2.2**
func TestProperty2_IncrementalTimeRangeCalculation(t *testing.T) {
	ctx := context.Background()

	t.Run("with_previous_success_log", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			// Generate a random BillEnd time in the past (1 hour to 365 days ago)
			daysAgo := rapid.IntRange(0, 365).Draw(t, "daysAgo")
			hoursAgo := rapid.IntRange(1, 23).Draw(t, "hoursAgo")
			billEnd := time.Now().UTC().AddDate(0, 0, -daysAgo).Add(-time.Duration(hoursAgo) * time.Hour)
			billEnd = billEnd.Truncate(time.Second)

			accountID := rapid.Int64Range(1, 100000).Draw(t, "accountID")

			dao := &mockCollectLogDAO{
				failedErr: errors.New("no failed log"),
				lastSuccess: &domain.CollectLog{
					BillEnd: billEnd,
				},
			}
			svc := newTestCollectorService(dao)

			start, end := svc.calculateIncrementalRange(ctx, accountID)

			// start should equal the previous success BillEnd
			assert.True(t, start.Equal(billEnd),
				"start (%v) should equal previous success BillEnd (%v)", start, billEnd)

			// start must be before end
			assert.True(t, start.Before(end),
				"start (%v) must be before end (%v)", start, end)
		})
	})

	t.Run("with_failed_log_retry", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			// Generate a random BillStart time in the past
			daysAgo := rapid.IntRange(0, 365).Draw(t, "daysAgo")
			hoursAgo := rapid.IntRange(1, 23).Draw(t, "hoursAgo")
			billStart := time.Now().UTC().AddDate(0, 0, -daysAgo).Add(-time.Duration(hoursAgo) * time.Hour)
			billStart = billStart.Truncate(time.Second)

			accountID := rapid.Int64Range(1, 100000).Draw(t, "accountID")

			dao := &mockCollectLogDAO{
				lastFailed: &domain.CollectLog{
					BillStart: billStart,
				},
				successErr: errors.New("no success log"),
			}
			svc := newTestCollectorService(dao)

			start, end := svc.calculateIncrementalRange(ctx, accountID)

			// start should equal the failed log's BillStart (retry from failure point)
			assert.True(t, start.Equal(billStart),
				"start (%v) should equal failed BillStart (%v)", start, billStart)

			// start must be before end
			assert.True(t, start.Before(end),
				"start (%v) must be before end (%v)", start, end)
		})
	})

	t.Run("no_history_defaults_to_month_start", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			accountID := rapid.Int64Range(1, 100000).Draw(t, "accountID")

			dao := &mockCollectLogDAO{
				failedErr:  errors.New("no failed log"),
				successErr: errors.New("no success log"),
			}
			svc := newTestCollectorService(dao)

			start, end := svc.calculateIncrementalRange(ctx, accountID)

			// start should be the first day of the current month in UTC
			now := time.Now().UTC()
			expectedMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
			assert.True(t, start.Equal(expectedMonthStart),
				"start (%v) should equal current month start (%v)", start, expectedMonthStart)

			// start must be before end
			assert.True(t, start.Before(end),
				"start (%v) must be before end (%v)", start, end)
		})
	})
}

// TestProperty4_CollectLogFieldCompleteness verifies that for any completed
// collection (success or failure), the collect log has:
// - AccountID > 0
// - Provider non-empty
// - Status in {"running", "success", "failed"}
// - StartTime non-zero
// - Duration >= 0
// - If Status == "success" then RecordCount >= 0
// - EndTime is non-zero and >= StartTime
//
// **Validates: Requirements 2.4**
func TestProperty4_CollectLogFieldCompleteness(t *testing.T) {
	validProviders := []string{"aliyun", "aws", "volcano", "huawei", "tencent"}
	validStatuses := []string{"success", "failed"}

	rapid.Check(t, func(t *rapid.T) {
		// Generate random collect log parameters
		accountID := rapid.Int64Range(1, 999999).Draw(t, "accountID")
		providerIdx := rapid.IntRange(0, len(validProviders)-1).Draw(t, "providerIdx")
		provider := validProviders[providerIdx]
		statusIdx := rapid.IntRange(0, len(validStatuses)-1).Draw(t, "statusIdx")
		status := validStatuses[statusIdx]
		recordCount := rapid.Int64Range(0, 100000).Draw(t, "recordCount")

		var errMsg string
		if status == "failed" {
			errMsg = rapid.StringMatching(`[a-z]{3,20}`).Draw(t, "errMsg")
		}

		// Create a CollectLog with valid initial fields (simulating what CollectAccount sets before calling finishCollectLog)
		secondsAgo := rapid.IntRange(1, 3600).Draw(t, "secondsAgo")
		startTime := time.Now().Add(-time.Duration(secondsAgo) * time.Second)
		collectLog := &domain.CollectLog{
			ID:         rapid.Int64Range(1, 999999).Draw(t, "logID"),
			AccountID:  accountID,
			Provider:   provider,
			Status:     "running",
			StartTime:  startTime,
			TenantID:   "test-tenant",
			CreateTime: time.Now().Unix(),
		}

		// Create service with mock DAO (Update just needs to not panic)
		dao := &mockCollectLogDAO{}
		svc := newTestCollectorService(dao)

		// Call finishCollectLog to simulate completion
		svc.finishCollectLog(context.Background(), collectLog, startTime, recordCount, status, errMsg)

		// Verify Property 4 constraints
		// 1. AccountID > 0
		assert.Greater(t, collectLog.AccountID, int64(0),
			"AccountID must be > 0, got %d", collectLog.AccountID)

		// 2. Provider non-empty
		assert.NotEmpty(t, collectLog.Provider,
			"Provider must be non-empty")

		// 3. Status in {"running", "success", "failed"}
		assert.Contains(t, []string{"running", "success", "failed"}, collectLog.Status,
			"Status must be one of running/success/failed, got %s", collectLog.Status)

		// 4. StartTime non-zero
		assert.False(t, collectLog.StartTime.IsZero(),
			"StartTime must be non-zero")

		// 5. Duration >= 0
		assert.GreaterOrEqual(t, collectLog.Duration, int64(0),
			"Duration must be >= 0, got %d", collectLog.Duration)

		// 6. If Status == "success" then RecordCount >= 0
		if collectLog.Status == "success" {
			assert.GreaterOrEqual(t, collectLog.RecordCount, int64(0),
				"RecordCount must be >= 0 when status is success, got %d", collectLog.RecordCount)
		}

		// 7. EndTime is non-zero and >= StartTime
		assert.False(t, collectLog.EndTime.IsZero(),
			"EndTime must be non-zero")
		assert.True(t, !collectLog.EndTime.Before(collectLog.StartTime),
			"EndTime (%v) must be >= StartTime (%v)", collectLog.EndTime, collectLog.StartTime)
	})
}

// TestProperty5_FailedCollectionRetryRange verifies that when a previous collection
// has failed, the next scheduled collection retries from the failed collection's
// BillStart time, and the retry range covers the entire failed range.
//
// For any failed collect log with BillStart=Fs and BillEnd=Fe (both in the past):
// 1. The returned start time equals Fs (retry from failure point)
// 2. The returned end time >= Fe (covers the entire failed range)
// 3. start < end
//
// This holds regardless of whether a success log exists (as long as it's older).
//
// **Validates: Requirements 2.5**
func TestProperty5_FailedCollectionRetryRange(t *testing.T) {
	ctx := context.Background()

	t.Run("failed_log_takes_priority_over_no_success", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			now := time.Now().UTC()

			// Generate a random failed BillEnd in the past (at least 1 hour ago)
			endHoursAgo := rapid.IntRange(1, 8760).Draw(t, "endHoursAgo") // 1 hour to ~1 year ago
			failedBillEnd := now.Add(-time.Duration(endHoursAgo) * time.Hour)
			failedBillEnd = failedBillEnd.Truncate(time.Second)

			// Generate a random failed BillStart before BillEnd (1 to 720 hours before BillEnd)
			spanHours := rapid.IntRange(1, 720).Draw(t, "spanHours")
			failedBillStart := failedBillEnd.Add(-time.Duration(spanHours) * time.Hour)
			failedBillStart = failedBillStart.Truncate(time.Second)

			accountID := rapid.Int64Range(1, 100000).Draw(t, "accountID")

			dao := &mockCollectLogDAO{
				lastFailed: &domain.CollectLog{
					BillStart: failedBillStart,
					BillEnd:   failedBillEnd,
				},
				successErr: errors.New("no success log"),
			}
			svc := newTestCollectorService(dao)

			start, end := svc.calculateIncrementalRange(ctx, accountID)

			// 1. start must equal the failed log's BillStart
			assert.True(t, start.Equal(failedBillStart),
				"start (%v) should equal failed BillStart (%v)", start, failedBillStart)

			// 2. end must be >= the failed log's BillEnd (covers entire failed range)
			assert.True(t, !end.Before(failedBillEnd),
				"end (%v) must be >= failed BillEnd (%v)", end, failedBillEnd)

			// 3. start must be before end
			assert.True(t, start.Before(end),
				"start (%v) must be before end (%v)", start, end)
		})
	})

	t.Run("failed_log_takes_priority_over_older_success", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			now := time.Now().UTC()

			// Generate a random failed BillEnd in the past (at least 1 hour ago)
			endHoursAgo := rapid.IntRange(1, 8760).Draw(t, "endHoursAgo")
			failedBillEnd := now.Add(-time.Duration(endHoursAgo) * time.Hour)
			failedBillEnd = failedBillEnd.Truncate(time.Second)

			// Generate a random failed BillStart before BillEnd
			spanHours := rapid.IntRange(1, 720).Draw(t, "spanHours")
			failedBillStart := failedBillEnd.Add(-time.Duration(spanHours) * time.Hour)
			failedBillStart = failedBillStart.Truncate(time.Second)

			// Generate a success log that is OLDER than the failed log
			extraDaysBack := rapid.IntRange(1, 30).Draw(t, "extraDaysBack")
			successBillEnd := failedBillStart.AddDate(0, 0, -extraDaysBack)
			successBillEnd = successBillEnd.Truncate(time.Second)

			accountID := rapid.Int64Range(1, 100000).Draw(t, "accountID")

			dao := &mockCollectLogDAO{
				lastFailed: &domain.CollectLog{
					BillStart: failedBillStart,
					BillEnd:   failedBillEnd,
				},
				lastSuccess: &domain.CollectLog{
					BillEnd: successBillEnd,
				},
			}
			svc := newTestCollectorService(dao)

			start, end := svc.calculateIncrementalRange(ctx, accountID)

			// 1. start must equal the failed log's BillStart (failed takes priority)
			assert.True(t, start.Equal(failedBillStart),
				"start (%v) should equal failed BillStart (%v), not success BillEnd (%v)",
				start, failedBillStart, successBillEnd)

			// 2. end must be >= the failed log's BillEnd
			assert.True(t, !end.Before(failedBillEnd),
				"end (%v) must be >= failed BillEnd (%v)", end, failedBillEnd)

			// 3. start must be before end
			assert.True(t, start.Before(end),
				"start (%v) must be before end (%v)", start, end)
		})
	})
}

// TestProperty6_CollectionMutualExclusion verifies that for any cloud account,
// at most one collection task can run at a time. If a lock is already held,
// subsequent collection attempts for the same account should fail (acquireLock
// returns false). After releasing the lock, a new acquisition should succeed.
//
// The property is tested with random accountIDs using miniredis as an in-memory
// Redis backend to exercise the real SetNX/Del lock mechanism.
//
// **Validates: Requirements 2.6**
func TestProperty6_CollectionMutualExclusion(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer client.Close()

	rapid.Check(t, func(t *rapid.T) {
		accountID := rapid.Int64Range(1, 100000).Draw(t, "accountID")

		svc := &CollectorService{
			redisClient: client,
			logger:      elog.DefaultLogger,
		}

		ctx := context.Background()

		// First acquire should succeed
		ok1, err1 := svc.acquireLock(ctx, accountID)
		assert.NoError(t, err1, "first acquireLock should not error")
		assert.True(t, ok1, "first acquireLock should succeed")

		// Second acquire for the same account should fail (mutual exclusion)
		ok2, err2 := svc.acquireLock(ctx, accountID)
		assert.NoError(t, err2, "second acquireLock should not error")
		assert.False(t, ok2, "second acquireLock should fail while lock is held")

		// Release the lock
		svc.releaseLock(ctx, accountID)

		// After release, re-acquire should succeed
		ok3, err3 := svc.acquireLock(ctx, accountID)
		assert.NoError(t, err3, "re-acquireLock after release should not error")
		assert.True(t, ok3, "re-acquireLock after release should succeed")

		// Cleanup: release the lock for the next iteration
		svc.releaseLock(ctx, accountID)
	})
}
