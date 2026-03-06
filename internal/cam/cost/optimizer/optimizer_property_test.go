package optimizer

import (
	"context"
	"fmt"
	"testing"
	"time"

	costdomain "github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/mongo"
	"pgregory.net/rapid"
)

// ========== Property 28: 低 CPU 利用率降配建议 ==========

// TestProperty28_LowCPUDownsizeRecommendation verifies that for any compute
// instance with consecutive billing days >= 7, a "downsize" recommendation is
// generated with estimated saving = daily_avg * 30 * 0.30. Instances with < 7
// consecutive days should NOT get a recommendation.
//
// **Validates: Requirements 8.1, 8.4**
func TestProperty28_LowCPUDownsizeRecommendation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate random parameters
		days := rapid.IntRange(1, 20).Draw(rt, "days")
		dailyAmount := rapid.Float64Range(1, 1000).Draw(rt, "dailyAmount")
		resourceID := fmt.Sprintf("i-%s", rapid.StringMatching(`[a-z0-9]{8}`).Draw(rt, "resID"))
		resourceName := fmt.Sprintf("ecs-%s", rapid.StringMatching(`[a-z]{4}`).Draw(rt, "resName"))
		tenantID := "tenant-prop28"

		// Build compute bills for the resource spanning `days` consecutive days
		now := time.Now()
		var bills []costdomain.UnifiedBill
		for i := 0; i < days; i++ {
			date := now.AddDate(0, 0, -days+i).Format("2006-01-02")
			bills = append(bills, costdomain.UnifiedBill{
				ID:           int64(i + 1),
				Provider:     "aliyun",
				AccountID:    100,
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

		optDAO := &mockOptimizerDAO{}
		billDAO := &mockBillDAO{
			listUnifiedBillsFn: func(_ context.Context, filter repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
				if filter.ServiceType == "compute" {
					return bills, nil
				}
				if filter.ServiceType == "storage" {
					return nil, nil
				}
				return nil, nil
			},
		}

		svc := NewOptimizerService(optDAO, billDAO, elog.DefaultLogger)
		err := svc.GenerateRecommendations(context.Background(), tenantID)
		assert.NoError(rt, err)

		if days >= lowCPUConsecutiveDays {
			// Should generate a downsize recommendation
			var found bool
			for _, rec := range optDAO.createdRecs {
				if rec.Type == RecTypeDownsize && rec.ResourceID == resourceID {
					found = true
					assert.Equal(rt, "aliyun", rec.Provider)
					assert.Equal(rt, resourceName, rec.ResourceName)
					assert.Equal(rt, StatusPending, rec.Status)
					assert.NotEmpty(rt, rec.ResourceID)
					assert.NotEmpty(rt, rec.ResourceName)

					// Verify saving calculation: daily_avg * 30 * 0.30
					expectedSaving := dailyAmount * 30 * downsizeSavingRatio
					assert.InDelta(rt, expectedSaving, rec.EstimatedSaving, 1e-6,
						"estimated saving should be daily_avg * 30 * 0.30")
					assert.GreaterOrEqual(rt, rec.EstimatedSaving, 0.0)
				}
			}
			assert.True(rt, found,
				"expected downsize recommendation for resource with %d days (>= %d)", days, lowCPUConsecutiveDays)
		} else {
			// Should NOT generate a downsize recommendation
			for _, rec := range optDAO.createdRecs {
				if rec.Type == RecTypeDownsize && rec.ResourceID == resourceID {
					rt.Fatalf("should NOT generate downsize recommendation for resource with %d days (< %d)",
						days, lowCPUConsecutiveDays)
				}
			}
		}
	})
}

// ========== Property 29: 未挂载云盘释放建议 ==========

// TestProperty29_UnattachedDiskReleaseRecommendation verifies that for any
// storage resource that has no corresponding compute resource in the same period,
// a "release_disk" recommendation is generated with estimated saving =
// daily_avg * 30 * 1.0. Storage resources with matching compute resources
// should NOT get a recommendation.
//
// **Validates: Requirements 8.2, 8.4**
func TestProperty29_UnattachedDiskReleaseRecommendation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate random storage resources
		numStorage := rapid.IntRange(1, 5).Draw(rt, "numStorage")
		numCompute := rapid.IntRange(0, 3).Draw(rt, "numCompute")
		tenantID := "tenant-prop29"
		now := time.Now()

		// Generate storage bills
		type diskEntry struct {
			resourceID   string
			resourceName string
			dailyAmount  float64
			billDays     int
		}
		var disks []diskEntry
		var storageBills []costdomain.UnifiedBill
		billID := int64(1)

		for i := 0; i < numStorage; i++ {
			resID := fmt.Sprintf("d-disk-%03d", i)
			resName := fmt.Sprintf("disk-%d", i)
			dailyAmt := rapid.Float64Range(1, 500).Draw(rt, fmt.Sprintf("diskAmt%d", i))
			billDays := rapid.IntRange(1, 7).Draw(rt, fmt.Sprintf("diskDays%d", i))

			disks = append(disks, diskEntry{
				resourceID:   resID,
				resourceName: resName,
				dailyAmount:  dailyAmt,
				billDays:     billDays,
			})

			for d := 0; d < billDays; d++ {
				date := now.AddDate(0, 0, -billDays+d).Format("2006-01-02")
				storageBills = append(storageBills, costdomain.UnifiedBill{
					ID:           billID,
					Provider:     "aliyun",
					AccountID:    100,
					ResourceID:   resID,
					ResourceName: resName,
					ServiceType:  "storage",
					Region:       "cn-beijing",
					Amount:       dailyAmt,
					AmountCNY:    dailyAmt,
					Currency:     "CNY",
					TenantID:     tenantID,
					BillingDate:  date,
				})
				billID++
			}
		}

		// Generate compute bills — some may share resource IDs with storage (attached)
		computeResourceIDs := make(map[string]bool)
		var computeBills []costdomain.UnifiedBill
		for i := 0; i < numCompute; i++ {
			// Randomly pick: either use a storage resource ID (attached) or a unique compute ID
			useStorageID := rapid.Bool().Draw(rt, fmt.Sprintf("useStorageID%d", i))
			var resID string
			if useStorageID && len(disks) > 0 {
				idx := rapid.IntRange(0, len(disks)-1).Draw(rt, fmt.Sprintf("storageIdx%d", i))
				resID = disks[idx].resourceID
			} else {
				resID = fmt.Sprintf("i-compute-%03d", i)
			}
			computeResourceIDs[resID] = true

			date := now.AddDate(0, 0, -1).Format("2006-01-02")
			computeBills = append(computeBills, costdomain.UnifiedBill{
				ID:           billID,
				Provider:     "aliyun",
				AccountID:    100,
				ResourceID:   resID,
				ResourceName: fmt.Sprintf("ecs-%d", i),
				ServiceType:  "compute",
				Region:       "cn-beijing",
				Amount:       50,
				AmountCNY:    50,
				Currency:     "CNY",
				TenantID:     tenantID,
				BillingDate:  date,
			})
			billID++
		}

		optDAO := &mockOptimizerDAO{}
		billDAO := &mockBillDAO{
			listUnifiedBillsFn: func(_ context.Context, filter repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
				if filter.ServiceType == "storage" {
					return storageBills, nil
				}
				if filter.ServiceType == "compute" {
					return computeBills, nil
				}
				return nil, nil
			},
		}

		svc := NewOptimizerService(optDAO, billDAO, elog.DefaultLogger)
		err := svc.GenerateRecommendations(context.Background(), tenantID)
		assert.NoError(rt, err)

		// Check each disk
		for _, disk := range disks {
			hasComputeMatch := computeResourceIDs[disk.resourceID]
			var foundRec bool
			for _, rec := range optDAO.createdRecs {
				if rec.Type == RecTypeReleaseDisk && rec.ResourceID == disk.resourceID {
					foundRec = true
					assert.NotEmpty(rt, rec.ResourceID)
					assert.NotEmpty(rt, rec.ResourceName)
					assert.GreaterOrEqual(rt, rec.EstimatedSaving, 0.0)

					// Verify saving: daily_avg * 30 * 1.0
					dailyAvg := (disk.dailyAmount * float64(disk.billDays)) / float64(disk.billDays)
					expectedSaving := dailyAvg * 30 * releaseDiskSavingRatio
					assert.InDelta(rt, expectedSaving, rec.EstimatedSaving, 1e-6,
						"estimated saving should be daily_avg * 30 * 1.0")
				}
			}

			if hasComputeMatch {
				assert.False(rt, foundRec,
					"storage resource %s with matching compute should NOT get release_disk recommendation",
					disk.resourceID)
			} else {
				assert.True(rt, foundRec,
					"unattached storage resource %s should get release_disk recommendation",
					disk.resourceID)
			}
		}
	})
}

// ========== Property 30: 按量转包年包月建议 ==========

// TestProperty30_ConvertPrepaidRecommendation verifies that for any postpaid
// resource with billing days >= 30, a "convert_prepaid" recommendation is
// generated with estimated saving = daily_avg * 30 * 0.40. Resources with < 30
// days or non-postpaid should NOT get a recommendation.
//
// **Validates: Requirements 8.3, 8.4**
func TestProperty30_ConvertPrepaidRecommendation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		days := rapid.IntRange(1, 45).Draw(rt, "days")
		chargeType := rapid.SampledFrom([]string{"postpaid", "prepaid"}).Draw(rt, "chargeType")
		dailyAmount := rapid.Float64Range(1, 1000).Draw(rt, "dailyAmount")
		resourceID := fmt.Sprintf("i-%s", rapid.StringMatching(`[a-z0-9]{8}`).Draw(rt, "resID"))
		resourceName := fmt.Sprintf("vm-%s", rapid.StringMatching(`[a-z]{4}`).Draw(rt, "resName"))
		tenantID := "tenant-prop30"

		now := time.Now()
		var bills []costdomain.UnifiedBill
		for i := 0; i < days; i++ {
			date := now.AddDate(0, 0, -days+i).Format("2006-01-02")
			bills = append(bills, costdomain.UnifiedBill{
				ID:           int64(i + 1),
				Provider:     "aws",
				AccountID:    200,
				ResourceID:   resourceID,
				ResourceName: resourceName,
				ServiceType:  "compute",
				Region:       "us-east-1",
				Amount:       dailyAmount,
				AmountCNY:    dailyAmount * 7.0,
				Currency:     "USD",
				ChargeType:   chargeType,
				TenantID:     tenantID,
				BillingDate:  date,
			})
		}

		optDAO := &mockOptimizerDAO{}
		billDAO := &mockBillDAO{
			listUnifiedBillsFn: func(_ context.Context, filter repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
				if filter.ServiceType == "compute" {
					// For downsize detection — return bills only if postpaid compute
					return bills, nil
				}
				if filter.ServiceType == "storage" {
					return nil, nil
				}
				// For on-demand detection (no service type filter)
				return bills, nil
			},
		}

		svc := NewOptimizerService(optDAO, billDAO, elog.DefaultLogger)
		err := svc.GenerateRecommendations(context.Background(), tenantID)
		assert.NoError(rt, err)

		shouldGenerate := chargeType == "postpaid" && days >= onDemandRunningDays

		var foundConvert bool
		for _, rec := range optDAO.createdRecs {
			if rec.Type == RecTypeConvertPrepaid && rec.ResourceID == resourceID {
				foundConvert = true
				assert.NotEmpty(rt, rec.ResourceID)
				assert.NotEmpty(rt, rec.ResourceName)
				assert.GreaterOrEqual(rt, rec.EstimatedSaving, 0.0)

				// Verify saving: daily_avg * 30 * 0.40
				// The service uses AmountCNY for calculation
				dailyAvg := (dailyAmount * 7.0 * float64(days)) / float64(days)
				expectedSaving := dailyAvg * 30 * convertPrepaidSavingRatio
				assert.InDelta(rt, expectedSaving, rec.EstimatedSaving, 1e-6,
					"estimated saving should be daily_avg * 30 * 0.40")
			}
		}

		if shouldGenerate {
			assert.True(rt, foundConvert,
				"postpaid resource with %d days (>= %d) should get convert_prepaid recommendation",
				days, onDemandRunningDays)
		} else {
			assert.False(rt, foundConvert,
				"resource with chargeType=%s and %d days should NOT get convert_prepaid recommendation",
				chargeType, days)
		}
	})
}

// ========== Property 31: 忽略建议不重复展示 ==========

// TestProperty31_DismissedRecommendationFiltering verifies that for any
// dismissed recommendation with dismiss_expiry in the future, the same
// resource+type combination should NOT generate a new recommendation. After
// expiry, it should be allowed again.
//
// **Validates: Requirements 8.5**
func TestProperty31_DismissedRecommendationFiltering(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a dismissed recommendation with random expiry
		now := time.Now()
		// expiryOffsetHours: negative means expired, positive means still active
		expiryOffsetHours := rapid.IntRange(-720, 720).Draw(rt, "expiryOffsetHours")
		expiryTime := now.Add(time.Duration(expiryOffsetHours) * time.Hour)
		dismissedAt := now.Add(-24 * time.Hour)

		resourceID := fmt.Sprintf("i-%s", rapid.StringMatching(`[a-z0-9]{8}`).Draw(rt, "resID"))
		recType := rapid.SampledFrom([]string{RecTypeDownsize, RecTypeReleaseDisk, RecTypeConvertPrepaid}).Draw(rt, "recType")
		tenantID := "tenant-prop31"

		isExpired := expiryTime.Before(now) || expiryTime.Equal(now)

		optDAO := &mockOptimizerDAO{
			findByResourceTypeFn: func(_ context.Context, tid, rid, rtype string) (costdomain.Recommendation, error) {
				if rid == resourceID && rtype == recType {
					return costdomain.Recommendation{
						ID:            1,
						ResourceID:    resourceID,
						Type:          recType,
						Status:        StatusDismissed,
						DismissedAt:   &dismissedAt,
						DismissExpiry: &expiryTime,
						TenantID:      tid,
					}, nil
				}
				return costdomain.Recommendation{}, mongo.ErrNoDocuments
			},
		}

		// Build a candidate recommendation
		candidate := costdomain.Recommendation{
			Type:            recType,
			ResourceID:      resourceID,
			ResourceName:    "test-resource",
			Provider:        "aliyun",
			AccountID:       100,
			Region:          "cn-beijing",
			EstimatedSaving: 100.0,
			Status:          StatusPending,
			TenantID:        tenantID,
		}

		svc := NewOptimizerService(optDAO, &mockBillDAO{}, elog.DefaultLogger)
		filtered := svc.filterDismissed(context.Background(), tenantID, []costdomain.Recommendation{candidate})

		if isExpired {
			// Dismiss has expired — recommendation should be allowed
			assert.Len(rt, filtered, 1,
				"expired dismiss (expiry=%v, now=%v) should allow new recommendation",
				expiryTime.Format(time.RFC3339), now.Format(time.RFC3339))
		} else {
			// Dismiss is still active — recommendation should be filtered out
			assert.Len(rt, filtered, 0,
				"active dismiss (expiry=%v, now=%v) should filter out recommendation",
				expiryTime.Format(time.RFC3339), now.Format(time.RFC3339))
		}
	})
}
