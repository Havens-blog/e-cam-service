package budget

import (
	"context"
	"errors"
	"testing"

	costdomain "github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
)

// 回归：预算不存在时**不得**返回包装后的存储错误。
//
// 实测缺陷（2026-10-31）：GET /api/v1/cam/budget/{id}/progress 对不存在的预算返回
//   500 {"code":500,"msg":"get budget: mongo: no documents in result"}
// —— 既不是业务语义（应 404），又把 mongo 原始错误当文案泄露给了调用方。
// 修复后：服务层把 mongo.ErrNoDocuments 归一为 ErrBudgetNotFound（404041）。
func TestGetBudgetProgress_NotFoundIsDomainError(t *testing.T) {
	dao := &mockBudgetDAO{
		getByIDFn: func(ctx context.Context, id int64) (costdomain.BudgetRule, error) {
			return costdomain.BudgetRule{}, mongo.ErrNoDocuments
		},
	}
	svc := setupTestService(t, dao, &mockBillDAO{}, &mockAlertDAO{})

	progress, err := svc.GetBudgetProgress(context.Background(), 999999)

	require.Error(t, err, "不存在的预算必须报错")
	assert.Nil(t, progress)
	assert.True(t, errors.Is(err, ErrBudgetNotFound), "应归一为 ErrBudgetNotFound，实际: %v", err)
	assert.Equal(t, int64(0), int64(ErrBudgetNotFound.Code)-int64(404041), "错误码应为 404041（预算不存在）")
	// 关键：不得把存储层细节暴露出去
	assert.NotContains(t, err.Error(), "mongo", "错误文案不得包含存储实现细节")
}

// 其他存储错误仍应被包装为普通错误（保持既有行为，交由 API 层回 500）。
func TestGetBudgetProgress_OtherErrorStillWrapped(t *testing.T) {
	boom := errors.New("connection reset by peer")
	dao := &mockBudgetDAO{
		getByIDFn: func(ctx context.Context, id int64) (costdomain.BudgetRule, error) {
			return costdomain.BudgetRule{}, boom
		},
	}
	svc := setupTestService(t, dao, &mockBillDAO{}, &mockAlertDAO{})

	_, err := svc.GetBudgetProgress(context.Background(), 1)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrBudgetNotFound), "非 not-found 不应被误判为不存在")
	assert.Contains(t, err.Error(), "get budget", "非 not-found 保持既有包装前缀，便于排障")
}
