package optimizer

import (
	"context"
	"errors"
	"testing"

	costdomain "github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========== RunForAllTenants（每日优化建议生成的按租户枚举入口）==========

// recordingBillDAO 造会产生降配建议的账单数据（compute 8 天 ≥ 7 天阈值），并记录
// 首次出现查询的租户（分页查询每租户多次，按首现顺序去重）。
func recordingBillDAO(seen *[]int64, seenSet map[int64]bool) *mockBillDAO {
	return &mockBillDAO{
		distinctTenantIDsFn: func(_ context.Context) ([]int64, error) {
			return []int64{3, 1, 2}, nil
		},
		listUnifiedBillsFn: func(_ context.Context, filter repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
			if !seenSet[filter.TenantID] {
				seenSet[filter.TenantID] = true
				*seen = append(*seen, filter.TenantID)
			}
			if filter.ServiceType == "compute" {
				return generateComputeBills("res-a", "web-01", "aliyun", filter.TenantID, 100, 8, 10), nil
			}
			return nil, nil
		},
	}
}

// TestRunForAllTenants_EachTenantProcessedOnce 多租户场景：DAO 枚举返回的每个租户
// 恰好执行一次建议生成（以账单分页查询为观测点），并保持枚举顺序逐个执行；
// 产出的建议归属对应租户。
func TestRunForAllTenants_EachTenantProcessedOnce(t *testing.T) {
	var (
		seen    []int64
		seenSet = map[int64]bool{}
	)
	billDAO := recordingBillDAO(&seen, seenSet)
	optDAO := &mockOptimizerDAO{}
	svc := setupTestService(t, optDAO, billDAO)

	err := svc.RunForAllTenants(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []int64{3, 1, 2}, seen)
	require.Len(t, optDAO.createdRecs, 3)
	assert.Equal(t, []int64{3, 1, 2}, []int64{
		optDAO.createdRecs[0].TenantID,
		optDAO.createdRecs[1].TenantID,
		optDAO.createdRecs[2].TenantID,
	})
}

// TestRunForAllTenants_SingleTenantFailureDoesNotBlockOthers 单租户失败不中断：
// 租户 2 的建议落库失败后，租户 3 仍被执行，错误经 errors.Join 聚合返回。
func TestRunForAllTenants_SingleTenantFailureDoesNotBlockOthers(t *testing.T) {
	var (
		seen      []int64
		seenSet   = map[int64]bool{}
		attempted []int64
	)
	billDAO := recordingBillDAO(&seen, seenSet)
	optDAO := &mockOptimizerDAO{
		createBatchFn: func(_ context.Context, recs []costdomain.Recommendation) (int64, error) {
			if len(recs) == 0 {
				return 0, nil
			}
			tenantID := recs[0].TenantID
			attempted = append(attempted, tenantID)
			if tenantID == 2 {
				return 0, errors.New("create batch failed for tenant 2")
			}
			return int64(len(recs)), nil
		},
	}
	svc := setupTestService(t, optDAO, billDAO)

	err := svc.RunForAllTenants(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant 2")
	assert.Contains(t, err.Error(), "create batch failed for tenant 2")
	// 租户 2 失败后其余租户仍被处理（枚举顺序 [3,1,2] 逐一执行）
	assert.Equal(t, []int64{3, 1, 2}, attempted)
}

// TestRunForAllTenants_EmptyTenantListCleanReturn 空枚举列表：干净返回，不触发任何生成。
func TestRunForAllTenants_EmptyTenantListCleanReturn(t *testing.T) {
	var billCalls int
	billDAO := &mockBillDAO{
		distinctTenantIDsFn: func(_ context.Context) ([]int64, error) {
			return nil, nil
		},
		listUnifiedBillsFn: func(context.Context, repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
			billCalls++
			return nil, nil
		},
	}
	svc := setupTestService(t, &mockOptimizerDAO{}, billDAO)

	err := svc.RunForAllTenants(context.Background())

	require.NoError(t, err)
	assert.Zero(t, billCalls)
}

// TestRunForAllTenants_ContextCancelStopsEarly ctx 取消时提前退出：租户 3 处理完
// 后取消 ctx，租户 1/2 不再执行，且取消不作为错误上抛。
func TestRunForAllTenants_ContextCancelStopsEarly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var (
		seen    []int64
		seenSet = map[int64]bool{}
		calls   int
	)
	billDAO := &mockBillDAO{
		distinctTenantIDsFn: func(_ context.Context) ([]int64, error) {
			return []int64{3, 1, 2}, nil
		},
		listUnifiedBillsFn: func(_ context.Context, filter repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
			if !seenSet[filter.TenantID] {
				seenSet[filter.TenantID] = true
				seen = append(seen, filter.TenantID)
			}
			calls++
			if calls == 1 {
				cancel()
			}
			if filter.ServiceType == "compute" {
				return generateComputeBills("res-a", "web-01", "aliyun", filter.TenantID, 100, 8, 10), nil
			}
			return nil, nil
		},
	}
	svc := setupTestService(t, &mockOptimizerDAO{}, billDAO)

	err := svc.RunForAllTenants(ctx)

	require.NoError(t, err)
	assert.Equal(t, []int64{3}, seen)
}

// TestRunForAllTenants_EnumerateError 枚举本身失败：直接返回包装错误，不进入逐租户循环。
func TestRunForAllTenants_EnumerateError(t *testing.T) {
	var billCalls int
	billDAO := &mockBillDAO{
		distinctTenantIDsFn: func(_ context.Context) ([]int64, error) {
			return nil, errors.New("distinct failed")
		},
		listUnifiedBillsFn: func(context.Context, repository.UnifiedBillFilter) ([]costdomain.UnifiedBill, error) {
			billCalls++
			return nil, nil
		},
	}
	svc := setupTestService(t, &mockOptimizerDAO{}, billDAO)

	err := svc.RunForAllTenants(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "distinct failed")
	assert.Zero(t, billCalls)
}
