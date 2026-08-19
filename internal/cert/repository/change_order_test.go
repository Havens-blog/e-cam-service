package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
)

func newActiveOrder(fp, mutex string) *domain.ChangeOrder {
	return &domain.ChangeOrder{
		OldCertFingerprint: fp,
		NewCertID:          "cert-new",
		Status:             domain.ChangeStatusPendingConfirm,
		SnapshotID:         "snap-1",
		ActiveMutex:        mutex,
		Creator:            "operator",
	}
}

// TestChangeOrderMutex_SameTokenSecondInsertFails（集成）
// uk_active_mutex 部分唯一索引：同 token 第二张活跃单 duplicate key → ErrChangeInFlight。
func TestChangeOrderMutex_SameTokenSecondInsertFails(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewChangeOrderRepository(db)

	fp := testFingerprint(3)
	first := newActiveOrder(fp, fp)
	id1, err := repo.Create(ctx, first)
	require.NoError(t, err)
	assert.NotEmpty(t, id1)

	// 同 token 第二张活跃单被索引拒绝
	_, err = repo.Create(ctx, newActiveOrder(fp, fp))
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrChangeInFlight)

	// 终态清除 token 后，同 token 可再建新单
	require.NoError(t, repo.TransitionTerminal(ctx, id1, domain.ChangeStatusCancelled))
	_, err = repo.Create(ctx, newActiveOrder(fp, fp))
	require.NoError(t, err, "终态 $unset 后同 token 应可再插")
}

// TestChangeOrderMutex_ConcurrentDoubleInsert（集成）
// 并发双 insert：恰一张成功，另一张 duplicate key（check-then-insert 竞态被索引关闭）。
func TestChangeOrderMutex_ConcurrentDoubleInsert(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewChangeOrderRepository(db)

	fp := testFingerprint(4)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	ids := make([]string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id, err := repo.Create(ctx, newActiveOrder(fp, fp))
			errs[i], ids[i] = err, id
		}(i)
	}
	wg.Wait()

	success := 0
	dup := 0
	for _, err := range errs {
		switch {
		case err == nil:
			success++
		case assert.ErrorIs(t, err, domain.ErrChangeInFlight) || mongo.IsDuplicateKeyError(err):
			dup++
		}
	}
	assert.Equal(t, 1, success, "并发双 insert 应恰一张成功")
	assert.Equal(t, 1, dup, "另一张应 duplicate key")
}

// TestChangeOrderRepo_Transitions（集成）活跃态/终态原子迁移原语。
func TestChangeOrderRepo_Transitions(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewChangeOrderRepository(db)

	fp := testFingerprint(5)

	// 创建时不带 token（draft 态）
	order := newActiveOrder(fp, "")
	order.Status = domain.ChangeStatusDraft
	id, err := repo.Create(ctx, order)
	require.NoError(t, err)

	created, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Empty(t, created.ActiveMutex)

	// 进入活跃态：状态与 token 同一原子 update 写入
	require.NoError(t, repo.TransitionActive(ctx, id, domain.ChangeStatusExecuting, fp))
	active, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusExecuting, active.Status)
	assert.Equal(t, fp, active.ActiveMutex)

	// 按互斥 token 查活跃单（应用层快速失败预检查）
	byToken, err := repo.GetByMutexToken(ctx, fp)
	require.NoError(t, err)
	assert.Equal(t, id, byToken.ID.Hex())

	// 进入终态：状态迁移与 token 清除同一原子 update
	require.NoError(t, repo.TransitionTerminal(ctx, id, domain.ChangeStatusCompleted))
	terminal, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusCompleted, terminal.Status)
	assert.Empty(t, terminal.ActiveMutex)

	_, err = repo.GetByMutexToken(ctx, fp)
	assert.ErrorIs(t, err, mongo.ErrNoDocuments)
}

// TestChangeItemRepo_BatchAndHeartbeat（集成）变更项写入/取批/心跳。
func TestChangeItemRepo_BatchAndHeartbeat(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewChangeItemRepository(db)

	items := []domain.ChangeItem{
		{
			OrderID: "order-1", BatchNo: 1, Action: domain.ActionUploadAndBind,
			ResourceRef: domain.ResourceRef{
				Channel: domain.ChannelCloudAPI, Cloud: "aliyun", Product: "cdn",
				AccountKey: "ak-1", ResourceID: "res-1",
			},
			OldCloudCertID: "old-1",
		},
		{
			OrderID: "order-1", BatchNo: 2, Action: domain.ActionPatchCRD,
			ResourceRef: domain.ResourceRef{
				Channel: domain.ChannelK8sAPI, ClusterID: "c1", Namespace: "default",
				Kind: "Ingress", ResourceID: "gw",
			},
		},
	}
	n, err := repo.CreateMulti(ctx, items)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, domain.ItemStatusPending, items[0].Status, "DEFAULT status=pending")

	all, err := repo.ListByOrder(ctx, "order-1")
	require.NoError(t, err)
	assert.Len(t, all, 2)

	batch1, err := repo.ListByOrderAndBatch(ctx, "order-1", 1)
	require.NoError(t, err)
	require.Len(t, batch1, 1)
	assert.Equal(t, "res-1", batch1[0].ResourceRef.ResourceID)

	require.NoError(t, repo.UpdateHeartbeat(ctx, batch1[0].ID.Hex(), now()))
	refreshed, err := repo.ListByOrder(ctx, "order-1")
	require.NoError(t, err)
	for _, it := range refreshed {
		if it.ID == batch1[0].ID {
			require.NotNil(t, it.HeartbeatAt)
		}
	}
}

// TestChangeOrderRepo_ListVerifyingActive（集成）任务 4.1 change_linked_diff
// 数据源：status=verifying 且 verifyWindowUntil > after，createdAt 升序稳定返回。
func TestChangeOrderRepo_ListVerifyingActive(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewChangeOrderRepository(db)

	future := now().Add(time.Hour)
	past := now().Add(-time.Minute)

	// 活跃验证中单（窗口未过期）——两条，按 createdAt 先后稳定排序
	active1 := newActiveOrder(testFingerprint(6), testFingerprint(6))
	active1.Status = domain.ChangeStatusVerifying
	active1.VerifyWindowUntil = &future
	id1, err := repo.Create(ctx, active1)
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond) // 拉开 createdAt
	active2 := newActiveOrder(testFingerprint(7), testFingerprint(7))
	active2.Status = domain.ChangeStatusVerifying
	active2.VerifyWindowUntil = &future
	id2, err := repo.Create(ctx, active2)
	require.NoError(t, err)

	// 窗口已过期的验证中单——不返回
	expired := newActiveOrder(testFingerprint(8), testFingerprint(8))
	expired.Status = domain.ChangeStatusVerifying
	expired.VerifyWindowUntil = &past
	_, err = repo.Create(ctx, expired)
	require.NoError(t, err)

	// 非 verifying 态（verifyWindowUntil 未过期）——不返回
	executing := newActiveOrder(testFingerprint(9), testFingerprint(9))
	executing.Status = domain.ChangeStatusExecuting
	executing.VerifyWindowUntil = &future
	_, err = repo.Create(ctx, executing)
	require.NoError(t, err)

	orders, err := repo.ListVerifyingActive(ctx, now())
	require.NoError(t, err)
	require.Len(t, orders, 2, "仅窗口未过期的验证中单返回")
	assert.Equal(t, id1, orders[0].ID.Hex(), "createdAt 升序稳定返回")
	assert.Equal(t, id2, orders[1].ID.Hex())
	for _, o := range orders {
		require.NotNil(t, o.VerifyWindowUntil)
		assert.True(t, o.VerifyWindowUntil.After(now()))
	}
}

// TestChangeOrderRepo_TransitionActiveTokenConflict（集成，任务 5.1）
// update 路径互斥：draft 单迁入活跃态时 token 与在途活跃单冲突 →
// ErrChangeInFlight（uk_active_mutex 对 update 同样强制，
// check-then-update 竞态窗口被关闭）；释放后可重试成功。
func TestChangeOrderRepo_TransitionActiveTokenConflict(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewChangeOrderRepository(db)

	fp := testFingerprint(10)

	// 活跃单 A（pending_confirm，持 token）
	idA, err := repo.Create(ctx, newActiveOrder(fp, fp))
	require.NoError(t, err)

	// draft 单 B（同指纹，无 token）
	b := newActiveOrder(fp, "")
	b.Status = domain.ChangeStatusDraft
	idB, err := repo.Create(ctx, b)
	require.NoError(t, err)

	// B 迁入活跃态撞 uk_active_mutex → ErrChangeInFlight
	err = repo.TransitionActive(ctx, idB, domain.ChangeStatusPendingConfirm, fp)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrChangeInFlight)
	stuck, err := repo.GetByID(ctx, idB)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusDraft, stuck.Status, "冲突后状态不变")
	assert.Empty(t, stuck.ActiveMutex)

	// A 转终态释放 token 后，B 重试成功
	require.NoError(t, repo.TransitionTerminal(ctx, idA, domain.ChangeStatusCancelled))
	require.NoError(t, repo.TransitionActive(ctx, idB, domain.ChangeStatusPendingConfirm, fp))
	active, err := repo.GetByID(ctx, idB)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusPendingConfirm, active.Status)
	assert.Equal(t, fp, active.ActiveMutex)
}

// TestChangeOrderRepo_TransitionTerminalWithProtect（集成，任务 5.1）
// 完成类终态：状态迁移、protectUntil 固化与 token 清除同一原子 update。
func TestChangeOrderRepo_TransitionTerminalWithProtect(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewChangeOrderRepository(db)

	fp := testFingerprint(11)
	id, err := repo.Create(ctx, newActiveOrder(fp, fp))
	require.NoError(t, err)

	protectUntil := now().AddDate(0, 0, 7)
	require.NoError(t, repo.TransitionTerminalWithProtect(ctx, id, domain.ChangeStatusCompleted, protectUntil))

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusCompleted, got.Status)
	require.NotNil(t, got.ProtectUntil)
	// BSON date 毫秒精度：按秒级容差比较
	assert.WithinDuration(t, protectUntil, *got.ProtectUntil, time.Second, "protectUntil 随终态原子固化")
	assert.Empty(t, got.ActiveMutex, "token 同原子清除")

	_, err = repo.GetByMutexToken(ctx, fp)
	assert.ErrorIs(t, err, mongo.ErrNoDocuments)
}

// TestChangeOrderRepo_ListPausedBefore（集成，任务 5.1）
// CancelByTimeout 扫描集：batchInfo.paused=true 且 pausedAt < before。
func TestChangeOrderRepo_ListPausedBefore(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewChangeOrderRepository(db)

	staleAt := now().Add(-100 * time.Hour)
	freshAt := now().Add(-time.Hour)

	paused := func(fp string, at time.Time) *domain.ChangeOrder {
		o := newActiveOrder(fp, "")
		o.Status = domain.ChangeStatusExecuting
		o.BatchInfo = &domain.BatchInfo{TotalBatches: 2, CurrentBatch: 2, BatchSize: 5, Paused: true, PausedAt: &at}
		return o
	}

	// 超时暂停单（两条，按 createdAt 升序稳定返回）
	id1, err := repo.Create(ctx, paused(testFingerprint(12), staleAt))
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond) // 拉开 createdAt
	id2, err := repo.Create(ctx, paused(testFingerprint(13), staleAt))
	require.NoError(t, err)

	// 暂停未超时——不返回
	_, err = repo.Create(ctx, paused(testFingerprint(14), freshAt))
	require.NoError(t, err)

	// executing 未暂停（残留 pausedAt 也不算）——不返回
	running := newActiveOrder(testFingerprint(15), "")
	running.Status = domain.ChangeStatusExecuting
	running.BatchInfo = &domain.BatchInfo{TotalBatches: 2, CurrentBatch: 1, BatchSize: 5, Paused: false, PausedAt: &staleAt}
	_, err = repo.Create(ctx, running)
	require.NoError(t, err)

	// paused=true 但缺 pausedAt——不返回
	noAt := newActiveOrder(testFingerprint(16), "")
	noAt.Status = domain.ChangeStatusExecuting
	noAt.BatchInfo = &domain.BatchInfo{TotalBatches: 2, CurrentBatch: 2, BatchSize: 5, Paused: true}
	_, err = repo.Create(ctx, noAt)
	require.NoError(t, err)

	// 已取消但残留 paused 标记的超时暂停单——不返回（executing 限定，
	// 防止 CancelByTimeout 重复扫描终态单）
	cancelled := paused(testFingerprint(17), staleAt)
	cancelled.Status = domain.ChangeStatusCancelled
	cancelled.ActiveMutex = ""
	_, err = repo.Create(ctx, cancelled)
	require.NoError(t, err)

	orders, err := repo.ListPausedBefore(ctx, now().Add(-72*time.Hour))
	require.NoError(t, err)
	require.Len(t, orders, 2, "仅超时暂停单返回")
	assert.Equal(t, id1, orders[0].ID.Hex(), "createdAt 升序稳定返回")
	assert.Equal(t, id2, orders[1].ID.Hex())
}

// TestChangeItemRepo_MarkPendingSkipped（集成，任务 5.1）
// Cancel 标记原语：订单 pending 项（含未到期批次）→ skipped；
// running 与已完结项不受影响；返回标记条数。
func TestChangeItemRepo_MarkPendingSkipped(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewChangeItemRepository(db)

	mk := func(orderID string, batchNo int, status domain.ChangeItemStatus) domain.ChangeItem {
		return domain.ChangeItem{
			OrderID: orderID,
			BatchNo: batchNo,
			Action:  domain.ActionUploadAndBind,
			ResourceRef: domain.ResourceRef{
				Channel: domain.ChannelCloudAPI, Cloud: "aliyun", Product: "cdn",
				AccountKey: "ak-1", ResourceID: orderID + "-" + fmt.Sprintf("%d-%s", batchNo, status),
			},
			Status: status,
		}
	}
	items := []domain.ChangeItem{
		mk("order-1", 1, domain.ItemStatusPending),
		mk("order-1", 1, domain.ItemStatusRunning),
		mk("order-1", 1, domain.ItemStatusSuccess),
		mk("order-1", 2, domain.ItemStatusPending), // 未到期批次同样标记
		mk("order-2", 1, domain.ItemStatusPending), // 其他订单不受影响
	}
	_, err := repo.CreateMulti(ctx, items)
	require.NoError(t, err)

	n, err := repo.MarkPendingSkipped(ctx, "order-1")
	require.NoError(t, err)
	assert.Equal(t, int64(2), n, "order-1 的两条 pending 被标记")

	all, err := repo.ListByOrder(ctx, "order-1")
	require.NoError(t, err)
	byResource := map[string]domain.ChangeItemStatus{}
	for _, it := range all {
		byResource[it.ResourceRef.ResourceID] = it.Status
	}
	assert.Equal(t, domain.ItemStatusSkipped, byResource["order-1-1-pending"])
	assert.Equal(t, domain.ItemStatusSkipped, byResource["order-1-2-pending"])
	assert.Equal(t, domain.ItemStatusRunning, byResource["order-1-1-running"], "running 不受影响")
	assert.Equal(t, domain.ItemStatusSuccess, byResource["order-1-1-success"], "已完结项不受影响")

	other, err := repo.ListByOrder(ctx, "order-2")
	require.NoError(t, err)
	require.Len(t, other, 1)
	assert.Equal(t, domain.ItemStatusPending, other[0].Status, "其他订单不受影响")

	// 幂等：无剩余 pending 时返回 0
	n, err = repo.MarkPendingSkipped(ctx, "order-1")
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
}

// TestChangeItemRepo_FinishRollback（集成）任务 5.8：CAS status=success →
// rolled_back/rollback_failed（同原子写 error）；非 success 幂等返回 false；
// 非法终态入参拒绝。经真实集合校验器验证 rollback_failed 枚举可写入。
func TestChangeItemRepo_FinishRollback(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewChangeItemRepository(db)

	items := []domain.ChangeItem{
		{
			OrderID: "order-rb", BatchNo: 1, Action: domain.ActionUploadAndBind,
			ResourceRef: domain.ResourceRef{
				Channel: domain.ChannelCloudAPI, Cloud: "aliyun", Product: "cdn",
				AccountKey: "ak-1", ResourceID: "res-ok",
			},
			OldCloudCertID: "old-1", Status: domain.ItemStatusSuccess,
		},
		{
			OrderID: "order-rb", BatchNo: 1, Action: domain.ActionUploadAndBind,
			ResourceRef: domain.ResourceRef{
				Channel: domain.ChannelCloudAPI, Cloud: "aliyun", Product: "cdn",
				AccountKey: "ak-1", ResourceID: "res-fail",
			},
			OldCloudCertID: "old-2", Status: domain.ItemStatusSuccess,
		},
		{
			OrderID: "order-rb", BatchNo: 1, Action: domain.ActionUploadAndBind,
			ResourceRef: domain.ResourceRef{
				Channel: domain.ChannelCloudAPI, Cloud: "aliyun", Product: "cdn",
				AccountKey: "ak-1", ResourceID: "res-deploy-failed",
			},
			OldCloudCertID: "old-3", Status: domain.ItemStatusFailed,
		},
	}
	_, err := repo.CreateMulti(ctx, items)
	require.NoError(t, err)
	all, err := repo.ListByOrder(ctx, "order-rb")
	require.NoError(t, err)
	ids := map[string]string{}
	for _, it := range all {
		ids[it.ResourceRef.ResourceID] = it.ID.Hex()
	}

	// 成功项 → rolled_back
	ok, err := repo.FinishRollback(ctx, ids["res-ok"], domain.ItemStatusRolledBack, "")
	require.NoError(t, err)
	assert.True(t, ok)
	// 成功项 → rollback_failed（error 写入）
	ok, err = repo.FinishRollback(ctx, ids["res-fail"], domain.ItemStatusRollbackFailed, "CLOUD_API_RATELIMITED: scripted")
	require.NoError(t, err)
	assert.True(t, ok)
	// 非 success（failed）→ CAS 未命中幂等 false
	ok, err = repo.FinishRollback(ctx, ids["res-deploy-failed"], domain.ItemStatusRolledBack, "")
	require.NoError(t, err)
	assert.False(t, ok)
	// 非法终态入参拒绝
	_, err = repo.FinishRollback(ctx, ids["res-ok"], domain.ItemStatusFailed, "")
	require.Error(t, err)

	refreshed, err := repo.ListByOrder(ctx, "order-rb")
	require.NoError(t, err)
	for _, it := range refreshed {
		switch it.ResourceRef.ResourceID {
		case "res-ok":
			assert.Equal(t, domain.ItemStatusRolledBack, it.Status)
		case "res-fail":
			assert.Equal(t, domain.ItemStatusRollbackFailed, it.Status)
			assert.Contains(t, it.Error, "CLOUD_API_RATELIMITED")
		case "res-deploy-failed":
			assert.Equal(t, domain.ItemStatusFailed, it.Status, "部署失败项不被回滚覆盖")
		}
	}

	// 幂等：已 rolled_back 的项再次回滚返回 false
	ok, err = repo.FinishRollback(ctx, ids["res-ok"], domain.ItemStatusRolledBack, "")
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestChangeOrderRepo_VerifyWindowPrimitives（集成，任务 5.10）
// SetVerifyExpected/PauseAfterVerify CAS 语义与 ListVerifyingExpired 扫描集。
func TestChangeOrderRepo_VerifyWindowPrimitives(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewChangeOrderRepository(db)

	fp := testFingerprint(11)
	order := newActiveOrder(fp, fp)
	order.Status = domain.ChangeStatusExecuting
	order.BatchInfo = &domain.BatchInfo{TotalBatches: 2, CurrentBatch: 1, BatchSize: 1}
	id, err := repo.Create(ctx, order)
	require.NoError(t, err)

	// 非 verifying 态固化未命中（CAS）
	until := now().Add(2 * time.Hour).Truncate(time.Millisecond) // Mongo date 毫秒精度：夹具预截断保证精确断言
	ok, err := repo.SetVerifyExpected(ctx, id, &domain.VerifyExpected{
		NewCertFingerprint: testFingerprint(12), Domains: []string{"d1.example.com"}, WindowUntil: until,
	})
	require.NoError(t, err)
	assert.False(t, ok, "executing 态固化 CAS 未命中")

	// 进入验证窗口 → 固化（verifyExpected 与 verifyWindowUntil 同原子对齐）
	okEnter, err := repo.EnterVerify(ctx, id, until, now())
	require.NoError(t, err)
	require.True(t, okEnter)
	ok, err = repo.SetVerifyExpected(ctx, id, &domain.VerifyExpected{
		NewCertFingerprint: testFingerprint(12), Domains: []string{"d1.example.com"},
		ExcludedDomains: []string{"ex.example.com"}, WindowUntil: until,
	})
	require.NoError(t, err)
	assert.True(t, ok)
	sealed, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, sealed.VerifyExpected)
	assert.Equal(t, []string{"d1.example.com"}, sealed.VerifyExpected.Domains)
	assert.Equal(t, []string{"ex.example.com"}, sealed.VerifyExpected.ExcludedDomains)
	require.NotNil(t, sealed.VerifyWindowUntil)
	assert.True(t, sealed.VerifyWindowUntil.Equal(until), "verifyWindowUntil 与快照 windowUntil 对齐")

	// ListVerifyingExpired：窗口已过期命中；未过期/非 verifying 不命中
	expiredAt := now().Add(-time.Minute).Truncate(time.Millisecond)
	ok, err = repo.SetVerifyExpected(ctx, id, sealed.VerifyExpected) // 覆盖写幂等
	require.NoError(t, err)
	assert.True(t, ok)
	other := newActiveOrder(testFingerprint(13), testFingerprint(13))
	other.Status = domain.ChangeStatusVerifying
	future := now().Add(time.Hour)
	other.VerifyWindowUntil = &future
	otherID, err := repo.Create(ctx, other)
	require.NoError(t, err)

	// 先把 id 的窗口推到过去：直接以过期窗口固化
	past := now().Add(-2 * time.Minute).Truncate(time.Millisecond)
	ok, err = repo.SetVerifyExpected(ctx, id, &domain.VerifyExpected{
		NewCertFingerprint: testFingerprint(12), Domains: []string{"d1.example.com"}, WindowUntil: past,
	})
	require.NoError(t, err)
	require.True(t, ok)
	expired, err := repo.ListVerifyingExpired(ctx, expiredAt)
	require.NoError(t, err)
	ids := make([]string, 0, len(expired))
	for _, o := range expired {
		ids = append(ids, o.ID.Hex())
	}
	assert.ElementsMatch(t, []string{id}, ids, "仅窗口已过期的验证中单命中")
	assert.NotContains(t, ids, otherID, "窗口未过期不命中")

	// PauseAfterVerify：verifying→executing + paused 标记（当前批保持）
	pausedAt := now()
	ok, err = repo.PauseAfterVerify(ctx, id, pausedAt)
	require.NoError(t, err)
	assert.True(t, ok)
	paused, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusExecuting, paused.Status)
	require.NotNil(t, paused.BatchInfo)
	assert.True(t, paused.BatchInfo.Paused)
	require.NotNil(t, paused.BatchInfo.PausedAt)
	assert.Equal(t, 1, paused.BatchInfo.CurrentBatch)
	assert.Equal(t, fp, paused.ActiveMutex, "批间暂停互斥保持")

	// 再次收敛（已非 verifying）CAS 未命中
	ok, err = repo.PauseAfterVerify(ctx, id, now())
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestChangeOrderRepo_ListPage（集成，任务 5.11）
// 列表分页：状态筛选、createdAt 降序、总数独立统计、limit<=0 空页。
func TestChangeOrderRepo_ListPage(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))
	repo := NewChangeOrderRepository(db)

	// 三张 completed + 一张 verifying（createdAt 递增写入）
	ids := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		o := newActiveOrder(testFingerprint(byte(20+i)), "")
		o.Status = domain.ChangeStatusCompleted
		id, err := repo.Create(ctx, o)
		require.NoError(t, err)
		ids = append(ids, id)
		time.Sleep(10 * time.Millisecond) // 拉开 createdAt
	}
	verifying := newActiveOrder(testFingerprint(30), testFingerprint(30))
	verifying.Status = domain.ChangeStatusVerifying
	_, err := repo.Create(ctx, verifying)
	require.NoError(t, err)

	// 全量：createdAt 降序（verifying 最后写入 → 首位）
	orders, total, err := repo.ListPage(ctx, "", 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 4, total)
	require.Len(t, orders, 4)

	// 状态 Tab 筛选：仅 completed，降序（后写在前）
	orders, total, err = repo.ListPage(ctx, domain.ChangeStatusCompleted, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Len(t, orders, 3)
	assert.Equal(t, ids[2], orders[0].ID.Hex(), "createdAt 降序稳定返回")
	assert.Equal(t, ids[0], orders[2].ID.Hex())

	// 分页：第 2 页每页 2 条（总数不变）
	orders, total, err = repo.ListPage(ctx, domain.ChangeStatusCompleted, 2, 2)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Len(t, orders, 1)
	assert.Equal(t, ids[0], orders[0].ID.Hex())

	// limit<=0：空页 + 总数
	orders, total, err = repo.ListPage(ctx, "", 5, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 4, total)
	assert.Empty(t, orders)

	// 未命中状态：空页 + 0
	orders, total, err = repo.ListPage(ctx, domain.ChangeStatusRolledBack, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)
	assert.Empty(t, orders)
}
