package anomaly

import (
	"context"
	"errors"
	"testing"
	"time"

	costdomain "github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========== RunForAllTenants（每日异常检测的按租户枚举入口）==========

// recordingBillFn 造会让 DetectAnomalies 产生异常的聚合数据，并记录首次出现当前日
// 查询（startDate==endDate）的租户（每租户每维度一次当日查询，按首现顺序去重）：
// 当日 250 vs 基线 3000/30 天 = 100 → 偏离 150% > 默认阈值 50%。
func recordingBillFn(seen *[]int64, seenSet map[int64]bool, currentDate *string) func(context.Context, int64, string, string, string) ([]repository.AggregateResult, error) {
	return func(_ context.Context, tenantID int64, _ string, startDate, endDate string) ([]repository.AggregateResult, error) {
		if startDate == endDate {
			if !seenSet[tenantID] {
				seenSet[tenantID] = true
				*seen = append(*seen, tenantID)
			}
			if currentDate != nil {
				*currentDate = startDate
			}
			return []repository.AggregateResult{{Key: "res-a", AmountCNY: 250}}, nil
		}
		// 基线窗口聚合
		return []repository.AggregateResult{{Key: "res-a", AmountCNY: 3000}}, nil
	}
}

// TestRunForAllTenants_EachTenantDetectedOnce 多租户场景：DAO 枚举返回的每个租户
// 恰好执行一次异常检测（以当日聚合查询为观测点），并保持枚举顺序逐个执行；
// 检测日期为昨日（与原 job 闭包口径一致）。
func TestRunForAllTenants_EachTenantDetectedOnce(t *testing.T) {
	var (
		seen        []int64
		seenSet     = map[int64]bool{}
		currentDate string
	)
	billDAO := &mockBillDAO{
		distinctTenantIDsFn: func(_ context.Context) ([]int64, error) {
			return []int64{3, 1, 2}, nil
		},
		aggregateByFieldFn: recordingBillFn(&seen, seenSet, &currentDate),
	}
	svc := setupTestService(t, &mockAnomalyDAO{}, billDAO, &mockAlertDAO{})

	err := svc.RunForAllTenants(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []int64{3, 1, 2}, seen)
	assert.Equal(t, time.Now().AddDate(0, 0, -1).Format("2006-01-02"), currentDate)
}

// TestRunForAllTenants_SingleTenantFailureDoesNotBlockOthers 单租户失败不中断：
// 租户 2 的异常事件落库失败后，租户 3 仍被执行，错误经 errors.Join 聚合返回。
func TestRunForAllTenants_SingleTenantFailureDoesNotBlockOthers(t *testing.T) {
	var (
		seen      []int64
		seenSet   = map[int64]bool{}
		attempted []int64
	)
	billDAO := &mockBillDAO{
		distinctTenantIDsFn: func(_ context.Context) ([]int64, error) {
			return []int64{1, 2, 3}, nil
		},
		aggregateByFieldFn: recordingBillFn(&seen, seenSet, nil),
	}
	anomalyDAO := &mockAnomalyDAO{
		createBatchFn: func(_ context.Context, anomalies []costdomain.CostAnomaly) (int64, error) {
			if len(anomalies) == 0 {
				return 0, nil
			}
			tenantID := anomalies[0].TenantID
			attempted = append(attempted, tenantID)
			if tenantID == 2 {
				return 0, errors.New("create batch failed for tenant 2")
			}
			return int64(len(anomalies)), nil
		},
	}
	svc := setupTestService(t, anomalyDAO, billDAO, &mockAlertDAO{})

	err := svc.RunForAllTenants(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant 2")
	assert.Contains(t, err.Error(), "create batch failed for tenant 2")
	// 租户 2 失败后其余租户仍被检测
	assert.Equal(t, []int64{1, 2, 3}, attempted)
}

// TestRunForAllTenants_EmptyTenantListCleanReturn 空枚举列表：干净返回，不触发任何检测。
func TestRunForAllTenants_EmptyTenantListCleanReturn(t *testing.T) {
	var billCalls int
	billDAO := &mockBillDAO{
		distinctTenantIDsFn: func(_ context.Context) ([]int64, error) {
			return nil, nil
		},
		aggregateByFieldFn: func(context.Context, int64, string, string, string) ([]repository.AggregateResult, error) {
			billCalls++
			return nil, nil
		},
	}
	svc := setupTestService(t, &mockAnomalyDAO{}, billDAO, &mockAlertDAO{})

	err := svc.RunForAllTenants(context.Background())

	require.NoError(t, err)
	assert.Zero(t, billCalls)
}

// TestRunForAllTenants_ContextCancelStopsEarly ctx 取消时提前退出：租户 1 处理完
// 后取消 ctx，租户 2/3 不再执行，且取消不作为错误上抛。
func TestRunForAllTenants_ContextCancelStopsEarly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var (
		seen    []int64
		seenSet = map[int64]bool{}
	)
	billDAO := &mockBillDAO{
		distinctTenantIDsFn: func(_ context.Context) ([]int64, error) {
			return []int64{1, 2, 3}, nil
		},
		aggregateByFieldFn: func(_ context.Context, tenantID int64, _ string, startDate, endDate string) ([]repository.AggregateResult, error) {
			if startDate == endDate {
				if !seenSet[tenantID] {
					seenSet[tenantID] = true
					seen = append(seen, tenantID)
				}
				if tenantID == 1 {
					cancel()
				}
				return []repository.AggregateResult{{Key: "res-a", AmountCNY: 250}}, nil
			}
			return []repository.AggregateResult{{Key: "res-a", AmountCNY: 3000}}, nil
		},
	}
	svc := setupTestService(t, &mockAnomalyDAO{}, billDAO, &mockAlertDAO{})

	err := svc.RunForAllTenants(ctx)

	require.NoError(t, err)
	assert.Equal(t, []int64{1}, seen)
}

// TestRunForAllTenants_EnumerateError 枚举本身失败：直接返回包装错误，不进入逐租户循环。
func TestRunForAllTenants_EnumerateError(t *testing.T) {
	var billCalls int
	billDAO := &mockBillDAO{
		distinctTenantIDsFn: func(_ context.Context) ([]int64, error) {
			return nil, errors.New("distinct failed")
		},
		aggregateByFieldFn: func(context.Context, int64, string, string, string) ([]repository.AggregateResult, error) {
			billCalls++
			return nil, nil
		},
	}
	svc := setupTestService(t, &mockAnomalyDAO{}, billDAO, &mockAlertDAO{})

	err := svc.RunForAllTenants(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "distinct failed")
	assert.Zero(t, billCalls)
}
