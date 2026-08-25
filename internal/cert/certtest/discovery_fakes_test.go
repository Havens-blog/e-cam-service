package certtest

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// TestFakeDiscoveryImportSessionRepo 假实现语义与真实仓储对齐
// （cert-cloud-discovery-import 任务 2；任务 4/5 服务层与端点测试注入前的行为锚定）：
// DEFAULT 填充/逐条目结果与进度递增/按失败计数收敛终态/未命中与非法 ID 哨兵/存储隔离。
func TestFakeDiscoveryImportSessionRepo(t *testing.T) {
	ctx := context.Background()
	f := NewFakeDiscoveryImportSessionRepo()

	s := &domain.DiscoveryImportSession{
		Items: []domain.DiscoveryImportItem{
			{Cloud: "aliyun", AccountKey: "acc-1", CloudCertID: "cert-1", Result: domain.DiscoveryItemPending},
			{Cloud: "tencent", AccountKey: "acc-2", CloudCertID: "cert-2", Result: domain.DiscoveryItemPending},
		},
		Progress: domain.DiscoveryImportProgress{Total: 2},
		Operator: "op-1",
	}
	id, err := f.Create(ctx, s)
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	got, err := f.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.DiscoveryImportRunning, got.Status, "DEFAULT status=running")
	assert.False(t, got.CreatedAt.IsZero(), "DEFAULT createdAt=now")
	assert.Equal(t, domain.DiscoveryItemPending, got.Items[0].Result, "未记录结果的条目保持 pending")

	// 逐条目结果与进度递增（与真实仓储原子语义一致）
	require.NoError(t, f.RecordItemResult(ctx, id, 0, domain.DiscoveryItemSuccess, testMappedCertID(1), ""))
	require.NoError(t, f.RecordItemResult(ctx, id, 1, domain.DiscoveryItemFailed, "", "CERT_GET_FAILED: 云侧已不存在"))
	got, err = f.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.DiscoveryItemSuccess, got.Items[0].Result)
	assert.Equal(t, testMappedCertID(1), got.Items[0].MappedCertID)
	assert.Equal(t, domain.DiscoveryItemFailed, got.Items[1].Result)
	assert.Equal(t, "CERT_GET_FAILED: 云侧已不存在", got.Items[1].ErrorReason)
	assert.Equal(t, 1, got.Progress.Succeeded)
	assert.Equal(t, 1, got.Progress.Failed)

	// 终态收敛（按失败计数）：failed>0 → partial_failed
	require.NoError(t, f.MarkFinished(ctx, id))
	got, err = f.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.DiscoveryImportPartialFailed, got.Status)
	require.NotNil(t, got.FinishedAt)

	// 全成功会话 → completed
	s2 := &domain.DiscoveryImportSession{
		Items:    []domain.DiscoveryImportItem{{Cloud: "aws", AccountKey: "acc-3", CloudCertID: "cert-3", Result: domain.DiscoveryItemPending}},
		Progress: domain.DiscoveryImportProgress{Total: 1},
		Operator: "op-1",
	}
	id2, err := f.Create(ctx, s2)
	require.NoError(t, err)
	require.NoError(t, f.RecordItemResult(ctx, id2, 0, domain.DiscoveryItemSuccess, testMappedCertID(2), ""))
	require.NoError(t, f.MarkFinished(ctx, id2))
	got2, err := f.GetByID(ctx, id2)
	require.NoError(t, err)
	assert.Equal(t, domain.DiscoveryImportCompleted, got2.Status)
	assert.NotNil(t, got2.FinishedAt)

	// 条目越界：返回错误（进度不受污染）
	err = f.RecordItemResult(ctx, id2, 5, domain.DiscoveryItemFailed, "", "x")
	require.Error(t, err)

	// 存储隔离：Create 后调用方修改切片不影响存储副本
	s.Items[0].CloudCertID = "mutated-after-create"
	got, err = f.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "cert-1", got.Items[0].CloudCertID)

	// 未命中/非法 hex 哨兵
	_, err = f.GetByID(ctx, primitive.NewObjectID().Hex())
	assert.ErrorIs(t, err, mongo.ErrNoDocuments)
	_, err = f.GetByID(ctx, "zzz")
	assert.ErrorIs(t, err, domain.ErrInvalidID)
	err = f.MarkFinished(ctx, "zzz")
	assert.ErrorIs(t, err, domain.ErrInvalidID)
	err = f.RecordItemResult(ctx, "zzz", 0, domain.DiscoveryItemSuccess, "", "")
	assert.ErrorIs(t, err, domain.ErrInvalidID)
}

// testMappedCertID 生成确定性台账证书 ID（仅测试数据用途）。
func testMappedCertID(seed byte) string {
	return "mapped-cert-" + string(rune('a'+seed))
}
