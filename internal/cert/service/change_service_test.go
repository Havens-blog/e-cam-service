package service

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
)

// changeHarness 变更单服务测试依赖聚合（内存假实现，无 DB 依赖）。
// snapshots/refs 为 5.2 清单生成依赖；probe 默认 nil（清单生成测试另行注入）。
type changeHarness struct {
	svc       ChangeService
	orders    *certtest.FakeChangeOrderRepo
	items     *certtest.FakeChangeItemRepo
	certs     *certtest.FakeCertificateRepo
	alertCfg  *certtest.FakeAlertConfigRepo
	snapshots *certtest.FakeScanSnapshotRepo
	refs      *certtest.FakeCertReferenceRepo
}

func newChangeHarness(t *testing.T) *changeHarness {
	t.Helper()
	h := &changeHarness{
		orders:    certtest.NewFakeChangeOrderRepo(),
		items:     certtest.NewFakeChangeItemRepo(),
		certs:     certtest.NewFakeCertificateRepo(),
		alertCfg:  certtest.NewFakeAlertConfigRepo(),
		snapshots: certtest.NewFakeScanSnapshotRepo(),
		refs:      certtest.NewFakeCertReferenceRepo(),
	}
	h.svc = NewChangeService(h.orders, h.items, h.certs, h.alertCfg, h.snapshots, h.refs, nil)
	return h
}

const changeTestFP = "aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11"

// seedOrder 写入指定状态订单（活跃态自动携带 activeMutex=fp），返回订单 ID。
func (h *changeHarness) seedOrder(t *testing.T, status domain.ChangeStatus, fp string, batch *domain.BatchInfo) string {
	t.Helper()
	o := &domain.ChangeOrder{
		OldCertFingerprint: fp,
		NewCertID:          "cert-new",
		Status:             status,
		SnapshotID:         "snap-1",
		BatchInfo:          batch,
		Creator:            "operator",
	}
	if status != domain.ChangeStatusDraft {
		o.ActiveMutex = fp
	}
	id, err := h.orders.Create(context.Background(), o)
	require.NoError(t, err)
	return id
}

// seedItems 写入指定状态变更项（同一订单），返回无。
func (h *changeHarness) seedItems(t *testing.T, orderID string, statuses ...domain.ChangeItemStatus) {
	t.Helper()
	items := make([]domain.ChangeItem, 0, len(statuses))
	for i, st := range statuses {
		items = append(items, domain.ChangeItem{
			OrderID: orderID,
			BatchNo: 1,
			Action:  domain.ActionUploadAndBind,
			ResourceRef: domain.ResourceRef{
				Channel: domain.ChannelCloudAPI, Cloud: "aliyun", Product: "cdn",
				AccountKey: "ak-1", ResourceID: orderID + "-" + time.Duration(i).String(),
			},
			Status: st,
		})
	}
	_, err := h.items.CreateMulti(context.Background(), items)
	require.NoError(t, err)
}

func itemStatuses(t *testing.T, h *changeHarness, orderID string) []domain.ChangeItemStatus {
	t.Helper()
	got, err := h.items.ListByOrder(context.Background(), orderID)
	require.NoError(t, err)
	out := make([]domain.ChangeItemStatus, 0, len(got))
	for _, it := range got {
		out = append(out, it.Status)
	}
	return out
}

// TestChangeService_CancelWholeOnNonExecuting（AC-3）
// draft / pending_confirm / executing+批间暂停：整单取消——未执行项标 skipped、
// 转 cancelled 终态、activeMutex 清除（同原子语义由仓储原语承接）。
func TestChangeService_CancelWholeOnNonExecuting(t *testing.T) {
	ctx := context.Background()
	pausedAt := time.Now().Add(-30 * time.Hour)
	cases := []struct {
		name   string
		status domain.ChangeStatus
		batch  *domain.BatchInfo
	}{
		{"draft 整单取消", domain.ChangeStatusDraft, nil},
		{"pending_confirm 整单取消", domain.ChangeStatusPendingConfirm, nil},
		{"executing 批间暂停整单取消", domain.ChangeStatusExecuting,
			&domain.BatchInfo{TotalBatches: 2, CurrentBatch: 2, BatchSize: 5, Paused: true, PausedAt: &pausedAt}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newChangeHarness(t)
			id := h.seedOrder(t, c.status, changeTestFP, c.batch)
			// 跨批次 pending + 已完结项：pending 全部标 skipped，已完结不动
			h.seedItems(t, id, domain.ItemStatusPending, domain.ItemStatusSuccess, domain.ItemStatusPending)

			require.NoError(t, h.svc.Cancel(ctx, id))

			order, err := h.orders.GetByID(ctx, id)
			require.NoError(t, err)
			assert.Equal(t, domain.ChangeStatusCancelled, order.Status)
			assert.Empty(t, order.ActiveMutex, "取消终态清除互斥 token")
			assert.Equal(t, []domain.ChangeItemStatus{
				domain.ItemStatusSkipped, domain.ItemStatusSuccess, domain.ItemStatusSkipped,
			}, itemStatuses(t, h, id), "未执行项标 skipped，已完结项保留")

			// token 释放后同指纹可再建活跃单（活性保障）
			_, err = h.orders.Create(ctx, &domain.ChangeOrder{
				OldCertFingerprint: changeTestFP, NewCertID: "cert-2",
				Status: domain.ChangeStatusPendingConfirm, SnapshotID: "snap-1",
				ActiveMutex: changeTestFP, Creator: "op",
			})
			assert.NoError(t, err, "取消释放 token 后同指纹可再建活跃单")
		})
	}
}

// TestChangeService_CancelExecutingAbortsPendingOnly（AC-3）
// executing（未暂停）中止路径：仅 pending 项标 skipped；running 不中断；
// 订单保持 executing、token 保留（待运行项完成后重算收敛，5.5/5.7）。
func TestChangeService_CancelExecutingAbortsPendingOnly(t *testing.T) {
	ctx := context.Background()
	h := newChangeHarness(t)
	id := h.seedOrder(t, domain.ChangeStatusExecuting, changeTestFP,
		&domain.BatchInfo{TotalBatches: 2, CurrentBatch: 1, BatchSize: 5})
	h.seedItems(t, id, domain.ItemStatusPending, domain.ItemStatusRunning, domain.ItemStatusPending)

	require.NoError(t, h.svc.Cancel(ctx, id))

	order, err := h.orders.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusExecuting, order.Status, "中止路径不立即迁移状态")
	assert.Equal(t, changeTestFP, order.ActiveMutex, "互斥 token 保留")
	assert.Equal(t, []domain.ChangeItemStatus{
		domain.ItemStatusSkipped, domain.ItemStatusRunning, domain.ItemStatusSkipped,
	}, itemStatuses(t, h, id), "仅 pending 标 skipped，running 不中断")

	// 幂等：重复 Cancel 不报错、状态不变
	require.NoError(t, h.svc.Cancel(ctx, id))
	order, err = h.orders.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusExecuting, order.Status)
}

// TestChangeService_CancelNotCancellable（AC-3/AC-6）
// verifying 与全部终态 → 409 CHANGE_NOT_CANCELLABLE；未执行项不受影响；
// 订单不存在时错误上抛。
func TestChangeService_CancelNotCancellable(t *testing.T) {
	ctx := context.Background()
	notCancellable := []domain.ChangeStatus{
		domain.ChangeStatusVerifying,
		domain.ChangeStatusCompleted,
		domain.ChangeStatusPartialCompleted,
		domain.ChangeStatusRolledBack,
		domain.ChangeStatusRollbackFailed,
		domain.ChangeStatusCancelled,
	}
	for _, st := range notCancellable {
		t.Run(string(st), func(t *testing.T) {
			h := newChangeHarness(t)
			id := h.seedOrder(t, st, changeTestFP, nil)
			h.seedItems(t, id, domain.ItemStatusPending)

			err := h.svc.Cancel(ctx, id)
			require.Error(t, err)
			ce, ok := domain.AsCertError(err)
			require.True(t, ok, "应为 CertError")
			assert.Equal(t, domain.CodeChangeNotCancellable, ce.Code())

			order, getErr := h.orders.GetByID(ctx, id)
			require.NoError(t, getErr)
			assert.Equal(t, st, order.Status, "状态不变")
			assert.Equal(t, []domain.ChangeItemStatus{domain.ItemStatusPending}, itemStatuses(t, h, id), "未执行项不受影响")
		})
	}

	t.Run("订单不存在", func(t *testing.T) {
		h := newChangeHarness(t)
		err := h.svc.Cancel(ctx, "000000000000000000000000")
		require.Error(t, err)
		assert.ErrorIs(t, err, mongo.ErrNoDocuments)
	})
}

// TestChangeService_TransitionActiveSetsMutex（AC-1/AC-2）
// draft→pending_confirm 经状态机校验后走 TransitionActive：
// token=oldCertFingerprint 与状态同一原子 update；同指纹在途单 → ErrChangeInFlight。
func TestChangeService_TransitionActiveSetsMutex(t *testing.T) {
	ctx := context.Background()
	h := newChangeHarness(t)

	// 正常路径：draft → pending_confirm
	id := h.seedOrder(t, domain.ChangeStatusDraft, changeTestFP, nil)
	require.NoError(t, h.svc.Transition(ctx, id, domain.ChangeStatusPendingConfirm))
	order, err := h.orders.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusPendingConfirm, order.Status)
	assert.Equal(t, changeTestFP, order.ActiveMutex, "活跃态 token=oldCertFingerprint")

	// 互斥冲突：同指纹另一张 draft 单迁入活跃态 → ErrChangeInFlight
	other := h.seedOrder(t, domain.ChangeStatusDraft, changeTestFP, nil)
	err = h.svc.Transition(ctx, other, domain.ChangeStatusPendingConfirm)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrChangeInFlight)
	stuck, err := h.orders.GetByID(ctx, other)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusDraft, stuck.Status, "冲突后状态不变")

	// 分批循环：executing ↔ verifying token 全程持有
	require.NoError(t, h.svc.Transition(ctx, id, domain.ChangeStatusExecuting))
	require.NoError(t, h.svc.Transition(ctx, id, domain.ChangeStatusVerifying))
	require.NoError(t, h.svc.Transition(ctx, id, domain.ChangeStatusExecuting))
	order, err = h.orders.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusExecuting, order.Status)
	assert.Equal(t, changeTestFP, order.ActiveMutex, "批间循环 token 全程持有")
}

// TestChangeService_TransitionCompletedProtects（AC-4）
// 进入 completed/partial_completed：protectUntil=now+rollbackProtectDays
// （订单原子固化 + 旧证书保护期写入），token 清除。
func TestChangeService_TransitionCompletedProtects(t *testing.T) {
	ctx := context.Background()
	for _, target := range []domain.ChangeStatus{domain.ChangeStatusCompleted, domain.ChangeStatusPartialCompleted} {
		t.Run(string(target), func(t *testing.T) {
			h := newChangeHarness(t)
			require.NoError(t, h.certs.Create(ctx, &domain.Certificate{
				Fingerprint: changeTestFP, HostingStatus: domain.HostingStatusComplete,
			}))
			id := h.seedOrder(t, domain.ChangeStatusVerifying, changeTestFP, nil)

			t0 := time.Now()
			require.NoError(t, h.svc.Transition(ctx, id, target))
			t1 := time.Now()

			order, err := h.orders.GetByID(ctx, id)
			require.NoError(t, err)
			assert.Equal(t, target, order.Status)
			assert.Empty(t, order.ActiveMutex, "终态清除 token")
			require.NotNil(t, order.ProtectUntil)
			// 默认 rollbackProtectDays=7（schema DEFAULT）
			assert.WithinDuration(t, t0.AddDate(0, 0, 7), *order.ProtectUntil, t1.Sub(t0)+time.Second,
				"protectUntil=进入终态时点+rollbackProtectDays")

			cert, err := h.certs.GetByFingerprint(ctx, changeTestFP)
			require.NoError(t, err)
			require.NotNil(t, cert.ProtectUntil, "旧证书保护期已固化（2.3 删除拦截依据）")
			assert.WithinDuration(t, *order.ProtectUntil, *cert.ProtectUntil, time.Second)
		})
	}
}

// TestChangeService_TransitionCompletedCustomProtectDays（AC-4）
// rollbackProtectDays 取全局配置（7~14 可配）。
func TestChangeService_TransitionCompletedCustomProtectDays(t *testing.T) {
	ctx := context.Background()
	h := newChangeHarness(t)
	cfg, err := h.alertCfg.Get(ctx)
	require.NoError(t, err)
	cfg.Thresholds.RollbackProtectDays = 14
	require.NoError(t, h.alertCfg.Save(ctx, &cfg))

	id := h.seedOrder(t, domain.ChangeStatusVerifying, changeTestFP, nil)
	t0 := time.Now()
	require.NoError(t, h.svc.Transition(ctx, id, domain.ChangeStatusCompleted))
	t1 := time.Now()

	order, err := h.orders.GetByID(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, order.ProtectUntil)
	assert.WithinDuration(t, t0.AddDate(0, 0, 14), *order.ProtectUntil, t1.Sub(t0)+time.Second)
}

// TestChangeService_TransitionTerminalWithoutProtect（AC-1）
// rolled_back/rollback_failed/cancelled 终态：无保护期写入，token 清除。
func TestChangeService_TransitionTerminalWithoutProtect(t *testing.T) {
	ctx := context.Background()
	h := newChangeHarness(t)
	id := h.seedOrder(t, domain.ChangeStatusPartialCompleted, changeTestFP, nil)

	require.NoError(t, h.svc.Transition(ctx, id, domain.ChangeStatusRolledBack))
	order, err := h.orders.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusRolledBack, order.Status)
	assert.Empty(t, order.ActiveMutex)
	assert.Nil(t, order.ProtectUntil, "非完成类终态不写保护期")
}

// TestChangeService_TransitionIllegal（AC-1）
// 白名单外迁移（跨级跳跃/自迁移/终态出边）→ *InvalidTransitionError，无写入。
func TestChangeService_TransitionIllegal(t *testing.T) {
	ctx := context.Background()
	illegal := []struct {
		from, to domain.ChangeStatus
	}{
		{domain.ChangeStatusDraft, domain.ChangeStatusExecuting},          // 跨级
		{domain.ChangeStatusDraft, domain.ChangeStatusCompleted},          // 跨级
		{domain.ChangeStatusPendingConfirm, domain.ChangeStatusVerifying}, // 跨级
		{domain.ChangeStatusVerifying, domain.ChangeStatusCancelled},      // verifying 不可取消
		{domain.ChangeStatusVerifying, domain.ChangeStatusRolledBack},     // 回滚终链仅自完成类终态
		{domain.ChangeStatusExecuting, domain.ChangeStatusDraft},          // 逆向
		{domain.ChangeStatusCancelled, domain.ChangeStatusPendingConfirm}, // 终态出边
		{domain.ChangeStatusCompleted, domain.ChangeStatusCompleted},      // 自迁移
	}
	for _, c := range illegal {
		t.Run(string(c.from)+"->"+string(c.to), func(t *testing.T) {
			h := newChangeHarness(t)
			id := h.seedOrder(t, c.from, changeTestFP, nil)
			err := h.svc.Transition(ctx, id, c.to)
			var ite *domain.InvalidTransitionError
			require.ErrorAs(t, err, &ite)
			order, getErr := h.orders.GetByID(ctx, id)
			require.NoError(t, getErr)
			assert.Equal(t, c.from, order.Status, "非法迁移不落库")
		})
	}
}

// TestChangeService_CancelByTimeout（AC-3/Implementation Notes）
// 暂停超时自动取消：batchInfo.paused=true 且 pausedAt+pauseTimeoutHours<now
// 的订单整单取消（未执行项标 skipped、token 清除）；未超时/未暂停单不受影响；
// 返回取消订单 ID 清单。阈值取全局配置。
func TestChangeService_CancelByTimeout(t *testing.T) {
	ctx := context.Background()
	h := newChangeHarness(t)

	stale := time.Now().Add(-100 * time.Hour) // 默认 72h 已超
	fresh := time.Now().Add(-time.Hour)       // 未超时
	pausedAt := func(at time.Time) *time.Time { return &at }
	batch := func(at *time.Time, paused bool) *domain.BatchInfo {
		return &domain.BatchInfo{TotalBatches: 2, CurrentBatch: 2, BatchSize: 5, Paused: paused, PausedAt: at}
	}

	idStale := h.seedOrder(t, domain.ChangeStatusExecuting, changeTestFP, batch(pausedAt(stale), true))
	h.seedItems(t, idStale, domain.ItemStatusPending, domain.ItemStatusPending)

	idFresh := h.seedOrder(t, domain.ChangeStatusExecuting, "bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22", batch(pausedAt(fresh), true))
	h.seedItems(t, idFresh, domain.ItemStatusPending)

	idRunning := h.seedOrder(t, domain.ChangeStatusExecuting, "cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33", batch(pausedAt(stale), false))
	h.seedItems(t, idRunning, domain.ItemStatusPending)

	cancelled, err := h.svc.CancelByTimeout(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{idStale}, cancelled, "仅超时暂停单被取消")

	order, err := h.orders.GetByID(ctx, idStale)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusCancelled, order.Status)
	assert.Empty(t, order.ActiveMutex)
	assert.Equal(t, []domain.ChangeItemStatus{domain.ItemStatusSkipped, domain.ItemStatusSkipped}, itemStatuses(t, h, idStale))

	for _, id := range []string{idFresh, idRunning} {
		order, err := h.orders.GetByID(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, domain.ChangeStatusExecuting, order.Status, "未超时/未暂停单不受影响")
	}
	assert.Equal(t, []domain.ChangeItemStatus{domain.ItemStatusPending}, itemStatuses(t, h, idFresh))
	assert.Equal(t, []domain.ChangeItemStatus{domain.ItemStatusPending}, itemStatuses(t, h, idRunning))

	// 幂等：再次扫描无超时单
	again, err := h.svc.CancelByTimeout(ctx)
	require.NoError(t, err)
	assert.Empty(t, again)
}

// TestChangeService_CancelByTimeoutCustomThreshold（AC-3）
// pauseTimeoutHours 取全局配置：24h 阈值下 30h 前暂停的单被取消。
func TestChangeService_CancelByTimeoutCustomThreshold(t *testing.T) {
	ctx := context.Background()
	h := newChangeHarness(t)
	cfg, err := h.alertCfg.Get(ctx)
	require.NoError(t, err)
	cfg.Thresholds.PauseTimeoutHours = 24
	require.NoError(t, h.alertCfg.Save(ctx, &cfg))

	at := time.Now().Add(-30 * time.Hour)
	id := h.seedOrder(t, domain.ChangeStatusExecuting, changeTestFP,
		&domain.BatchInfo{TotalBatches: 2, CurrentBatch: 2, BatchSize: 5, Paused: true, PausedAt: &at})

	cancelled, err := h.svc.CancelByTimeout(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{id}, cancelled)
}

// ---- 错误传播（错误注入假实现：包装 certtest 假实现按开关返回错误） ----

// failingOrders 包装假变更单仓储，按开关注入错误。
type failingOrders struct {
	*certtest.FakeChangeOrderRepo
	getByIDErr    error
	transitionA   error
	terminalErr   error
	terminalPErr  error
	listPausedErr error
}

func (f *failingOrders) GetByID(ctx context.Context, id string) (domain.ChangeOrder, error) {
	if f.getByIDErr != nil {
		return domain.ChangeOrder{}, f.getByIDErr
	}
	return f.FakeChangeOrderRepo.GetByID(ctx, id)
}

func (f *failingOrders) TransitionActive(ctx context.Context, id string, to domain.ChangeStatus, token string) error {
	if f.transitionA != nil {
		return f.transitionA
	}
	return f.FakeChangeOrderRepo.TransitionActive(ctx, id, to, token)
}

func (f *failingOrders) TransitionTerminal(ctx context.Context, id string, to domain.ChangeStatus) error {
	if f.terminalErr != nil {
		return f.terminalErr
	}
	return f.FakeChangeOrderRepo.TransitionTerminal(ctx, id, to)
}

func (f *failingOrders) TransitionTerminalWithProtect(ctx context.Context, id string, to domain.ChangeStatus, protectUntil time.Time) error {
	if f.terminalPErr != nil {
		return f.terminalPErr
	}
	return f.FakeChangeOrderRepo.TransitionTerminalWithProtect(ctx, id, to, protectUntil)
}

func (f *failingOrders) ListPausedBefore(ctx context.Context, before time.Time) ([]domain.ChangeOrder, error) {
	if f.listPausedErr != nil {
		return nil, f.listPausedErr
	}
	return f.FakeChangeOrderRepo.ListPausedBefore(ctx, before)
}

// failingItems 包装假变更项仓储，MarkPendingSkipped 注入错误。
type failingItems struct {
	*certtest.FakeChangeItemRepo
	markErr error
}

func (f *failingItems) MarkPendingSkipped(ctx context.Context, orderID string) (int64, error) {
	if f.markErr != nil {
		return 0, f.markErr
	}
	return f.FakeChangeItemRepo.MarkPendingSkipped(ctx, orderID)
}

// failingCerts 包装假台账仓储，SetProtectUntil 注入错误。
type failingCerts struct {
	*certtest.FakeCertificateRepo
	protectErr error
}

func (f *failingCerts) SetProtectUntil(ctx context.Context, fp string, until time.Time) error {
	if f.protectErr != nil {
		return f.protectErr
	}
	return f.FakeCertificateRepo.SetProtectUntil(ctx, fp, until)
}

// failingAlertCfg 包装假配置仓储，Get 注入错误。
type failingAlertCfg struct {
	*certtest.FakeAlertConfigRepo
	getErr error
}

func (f *failingAlertCfg) Get(ctx context.Context) (domain.AlertConfig, error) {
	if f.getErr != nil {
		return domain.AlertConfig{}, f.getErr
	}
	return f.FakeAlertConfigRepo.Get(ctx)
}

var errInjected = mongo.ErrClientDisconnected // 任意非哨兵错误，验证包装传播

// TestChangeService_ErrorPropagation 仓储/配置错误经 %w 包装上抛；
// 整单取消标记失败时不迁移终态；CancelByTimeout 单笔失败不中断扫描。
func TestChangeService_ErrorPropagation(t *testing.T) {
	ctx := context.Background()

	t.Run("Transition GetByID 失败", func(t *testing.T) {
		h := newChangeHarness(t)
		h.svc = NewChangeService(&failingOrders{FakeChangeOrderRepo: h.orders, getByIDErr: errInjected}, h.items, h.certs, h.alertCfg, h.snapshots, h.refs, nil)
		err := h.svc.Transition(ctx, "000000000000000000000000", domain.ChangeStatusPendingConfirm)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("Transition 活跃态迁移失败", func(t *testing.T) {
		h := newChangeHarness(t)
		id := h.seedOrder(t, domain.ChangeStatusDraft, changeTestFP, nil)
		h.svc = NewChangeService(&failingOrders{FakeChangeOrderRepo: h.orders, transitionA: errInjected}, h.items, h.certs, h.alertCfg, h.snapshots, h.refs, nil)
		err := h.svc.Transition(ctx, id, domain.ChangeStatusPendingConfirm)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("Transition 完成终态 配置读取失败", func(t *testing.T) {
		h := newChangeHarness(t)
		id := h.seedOrder(t, domain.ChangeStatusVerifying, changeTestFP, nil)
		h.svc = NewChangeService(h.orders, h.items, h.certs, &failingAlertCfg{FakeAlertConfigRepo: h.alertCfg, getErr: errInjected}, h.snapshots, h.refs, nil)
		err := h.svc.Transition(ctx, id, domain.ChangeStatusCompleted)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("Transition 完成终态 保护终态迁移失败", func(t *testing.T) {
		h := newChangeHarness(t)
		id := h.seedOrder(t, domain.ChangeStatusVerifying, changeTestFP, nil)
		h.svc = NewChangeService(&failingOrders{FakeChangeOrderRepo: h.orders, terminalPErr: errInjected}, h.items, h.certs, h.alertCfg, h.snapshots, h.refs, nil)
		err := h.svc.Transition(ctx, id, domain.ChangeStatusCompleted)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("Transition 完成终态 旧证书保护期写入失败上抛不回滚", func(t *testing.T) {
		h := newChangeHarness(t)
		id := h.seedOrder(t, domain.ChangeStatusVerifying, changeTestFP, nil)
		h.svc = NewChangeService(h.orders, h.items, &failingCerts{FakeCertificateRepo: h.certs, protectErr: errInjected}, h.alertCfg, h.snapshots, h.refs, nil)
		err := h.svc.Transition(ctx, id, domain.ChangeStatusCompleted)
		assert.ErrorIs(t, err, errInjected)
		order, getErr := h.orders.GetByID(ctx, id)
		require.NoError(t, getErr)
		assert.Equal(t, domain.ChangeStatusCompleted, order.Status, "订单终态不因证书保护写入失败回滚")
	})

	t.Run("Transition 普通终态迁移失败", func(t *testing.T) {
		h := newChangeHarness(t)
		id := h.seedOrder(t, domain.ChangeStatusPartialCompleted, changeTestFP, nil)
		h.svc = NewChangeService(&failingOrders{FakeChangeOrderRepo: h.orders, terminalErr: errInjected}, h.items, h.certs, h.alertCfg, h.snapshots, h.refs, nil)
		err := h.svc.Transition(ctx, id, domain.ChangeStatusRolledBack)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("Cancel GetByID 失败", func(t *testing.T) {
		h := newChangeHarness(t)
		h.svc = NewChangeService(&failingOrders{FakeChangeOrderRepo: h.orders, getByIDErr: errInjected}, h.items, h.certs, h.alertCfg, h.snapshots, h.refs, nil)
		err := h.svc.Cancel(ctx, "000000000000000000000000")
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("Cancel 整单 标记失败不迁移终态", func(t *testing.T) {
		h := newChangeHarness(t)
		id := h.seedOrder(t, domain.ChangeStatusDraft, changeTestFP, nil)
		h.svc = NewChangeService(h.orders, &failingItems{FakeChangeItemRepo: h.items, markErr: errInjected}, h.certs, h.alertCfg, h.snapshots, h.refs, nil)
		err := h.svc.Cancel(ctx, id)
		assert.ErrorIs(t, err, errInjected)
		order, getErr := h.orders.GetByID(ctx, id)
		require.NoError(t, getErr)
		assert.Equal(t, domain.ChangeStatusDraft, order.Status, "标记失败时不迁移终态（可重试）")
	})

	t.Run("Cancel 中止路径 标记失败", func(t *testing.T) {
		h := newChangeHarness(t)
		id := h.seedOrder(t, domain.ChangeStatusExecuting, changeTestFP, nil)
		h.svc = NewChangeService(h.orders, &failingItems{FakeChangeItemRepo: h.items, markErr: errInjected}, h.certs, h.alertCfg, h.snapshots, h.refs, nil)
		err := h.svc.Cancel(ctx, id)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("Cancel 整单 终态迁移失败", func(t *testing.T) {
		h := newChangeHarness(t)
		id := h.seedOrder(t, domain.ChangeStatusDraft, changeTestFP, nil)
		h.svc = NewChangeService(&failingOrders{FakeChangeOrderRepo: h.orders, terminalErr: errInjected}, h.items, h.certs, h.alertCfg, h.snapshots, h.refs, nil)
		err := h.svc.Cancel(ctx, id)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("CancelByTimeout 配置读取失败", func(t *testing.T) {
		h := newChangeHarness(t)
		h.svc = NewChangeService(h.orders, h.items, h.certs, &failingAlertCfg{FakeAlertConfigRepo: h.alertCfg, getErr: errInjected}, h.snapshots, h.refs, nil)
		_, err := h.svc.CancelByTimeout(ctx)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("CancelByTimeout 扫描失败", func(t *testing.T) {
		h := newChangeHarness(t)
		h.svc = NewChangeService(&failingOrders{FakeChangeOrderRepo: h.orders, listPausedErr: errInjected}, h.items, h.certs, h.alertCfg, h.snapshots, h.refs, nil)
		_, err := h.svc.CancelByTimeout(ctx)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("CancelByTimeout 单笔失败不中断扫描", func(t *testing.T) {
		h := newChangeHarness(t)
		stale := time.Now().Add(-100 * time.Hour)
		batch := &domain.BatchInfo{TotalBatches: 2, CurrentBatch: 2, BatchSize: 5, Paused: true, PausedAt: &stale}
		id1 := h.seedOrder(t, domain.ChangeStatusExecuting, changeTestFP, batch)
		id2 := h.seedOrder(t, domain.ChangeStatusExecuting, "bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22", batch)
		// 终态迁移注入失败：首批单取消失败
		h.svc = NewChangeService(&failingOrders{FakeChangeOrderRepo: h.orders, terminalErr: errInjected}, h.items, h.certs, h.alertCfg, h.snapshots, h.refs, nil)
		cancelled, err := h.svc.CancelByTimeout(ctx)
		assert.ErrorIs(t, err, errInjected, "首批错误上抛")
		assert.Empty(t, cancelled, "失败单不计入取消清单")

		// 恢复后重试：两单均可取消（含此前失败单）
		h.svc = NewChangeService(h.orders, h.items, h.certs, h.alertCfg, h.snapshots, h.refs, nil)
		cancelled, err = h.svc.CancelByTimeout(ctx)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{id1, id2}, cancelled)
	})
}
