package budget

import (
	"context"
	"errors"
	"testing"

	costdomain "github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========== RunForAllTenants（每日预算检查的按租户枚举入口）==========

// TestRunForAllTenants_EachTenantCheckedOnce 多租户场景：DAO 枚举返回的每个租户
// 恰好执行一次预算检查（以 ListActive 调用为观测点），并保持枚举顺序逐个执行。
func TestRunForAllTenants_EachTenantCheckedOnce(t *testing.T) {
	var visited []int64
	budgetDAO := &mockBudgetDAO{
		distinctTenantIDsFn: func(_ context.Context) ([]int64, error) {
			return []int64{3, 1, 2}, nil
		},
		listActiveFn: func(_ context.Context, tenantID int64) ([]costdomain.BudgetRule, error) {
			visited = append(visited, tenantID)
			return nil, nil
		},
	}
	svc := setupTestService(t, budgetDAO, &mockBillDAO{}, &mockAlertDAO{})

	err := svc.RunForAllTenants(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []int64{3, 1, 2}, visited)
}

// TestRunForAllTenants_SingleTenantFailureDoesNotBlockOthers 单租户失败不中断：
// 租户 2 检查失败后，租户 3 仍被执行，错误经 errors.Join 聚合返回并带租户标识。
func TestRunForAllTenants_SingleTenantFailureDoesNotBlockOthers(t *testing.T) {
	var visited []int64
	budgetDAO := &mockBudgetDAO{
		distinctTenantIDsFn: func(_ context.Context) ([]int64, error) {
			return []int64{1, 2, 3}, nil
		},
		listActiveFn: func(_ context.Context, tenantID int64) ([]costdomain.BudgetRule, error) {
			visited = append(visited, tenantID)
			if tenantID == 2 {
				return nil, errors.New("mongo down for tenant 2")
			}
			return nil, nil
		},
	}
	svc := setupTestService(t, budgetDAO, &mockBillDAO{}, &mockAlertDAO{})

	err := svc.RunForAllTenants(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant 2")
	assert.Contains(t, err.Error(), "mongo down for tenant 2")
	assert.Equal(t, []int64{1, 2, 3}, visited)
}

// TestRunForAllTenants_EmptyTenantListCleanReturn 空枚举列表：干净返回，不触发任何租户检查。
func TestRunForAllTenants_EmptyTenantListCleanReturn(t *testing.T) {
	var listActiveCalls int
	budgetDAO := &mockBudgetDAO{
		distinctTenantIDsFn: func(_ context.Context) ([]int64, error) {
			return nil, nil
		},
		listActiveFn: func(_ context.Context, _ int64) ([]costdomain.BudgetRule, error) {
			listActiveCalls++
			return nil, nil
		},
	}
	svc := setupTestService(t, budgetDAO, &mockBillDAO{}, &mockAlertDAO{})

	err := svc.RunForAllTenants(context.Background())

	require.NoError(t, err)
	assert.Zero(t, listActiveCalls)
}

// TestRunForAllTenants_ContextCancelStopsEarly ctx 取消时提前退出：租户 1 检查完
// 后取消 ctx，租户 2/3 不再执行，且取消不作为错误上抛。
func TestRunForAllTenants_ContextCancelStopsEarly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var visited []int64
	budgetDAO := &mockBudgetDAO{
		distinctTenantIDsFn: func(_ context.Context) ([]int64, error) {
			return []int64{1, 2, 3}, nil
		},
		listActiveFn: func(_ context.Context, tenantID int64) ([]costdomain.BudgetRule, error) {
			visited = append(visited, tenantID)
			if tenantID == 1 {
				cancel()
			}
			return nil, nil
		},
	}
	svc := setupTestService(t, budgetDAO, &mockBillDAO{}, &mockAlertDAO{})

	err := svc.RunForAllTenants(ctx)

	require.NoError(t, err)
	assert.Equal(t, []int64{1}, visited)
}

// TestRunForAllTenants_EnumerateError 枚举本身失败：直接返回包装错误，不进入逐租户循环。
func TestRunForAllTenants_EnumerateError(t *testing.T) {
	var listActiveCalls int
	budgetDAO := &mockBudgetDAO{
		distinctTenantIDsFn: func(_ context.Context) ([]int64, error) {
			return nil, errors.New("distinct failed")
		},
		listActiveFn: func(_ context.Context, _ int64) ([]costdomain.BudgetRule, error) {
			listActiveCalls++
			return nil, nil
		},
	}
	svc := setupTestService(t, budgetDAO, &mockBillDAO{}, &mockAlertDAO{})

	err := svc.RunForAllTenants(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "distinct failed")
	assert.Zero(t, listActiveCalls)
}
