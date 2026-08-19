// 变更单/变更项仓储内存假实现（任务 5.1 起供 service/web 层测试共享）。
// 纯存储语义：模拟 uk_active_mutex 部分唯一索引与各原子原语的字段效果，
// 不做状态机合法性校验（业务校验属 domain/service 层）。
package certtest

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// FakeChangeOrderRepo 变更单内存假实现。
// Create/TransitionActive 写入的 activeMutex token 与其他活跃单冲突时返回
// domain.ErrChangeInFlight（同 token 仅一张活跃单）；终态迁移清除 token。
type FakeChangeOrderRepo struct {
	mu     sync.Mutex
	byID   map[string]*domain.ChangeOrder
	tokens map[string]string // activeMutex token → 持有订单 ID（hex）
}

// NewFakeChangeOrderRepo 创建空变更单假实现。
func NewFakeChangeOrderRepo() *FakeChangeOrderRepo {
	return &FakeChangeOrderRepo{
		byID:   map[string]*domain.ChangeOrder{},
		tokens: map[string]string{},
	}
}

// Create 写入变更单（模拟 DEFAULT 填充：createdAt=now）并返回 ID（hex）；
// token 冲突返回 ErrChangeInFlight。
func (f *FakeChangeOrderRepo) Create(_ context.Context, order *domain.ChangeOrder) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if order.CreatedAt.IsZero() {
		order.CreatedAt = time.Now()
	}
	if order.ID.IsZero() {
		order.ID = primitive.NewObjectID()
	}
	if order.ActiveMutex != "" {
		if holder, ok := f.tokens[order.ActiveMutex]; ok && holder != order.ID.Hex() {
			return "", domain.ErrChangeInFlight
		}
	}
	stored := cloneChangeOrder(*order)
	f.byID[stored.ID.Hex()] = &stored
	if stored.ActiveMutex != "" {
		f.tokens[stored.ActiveMutex] = stored.ID.Hex()
	}
	return stored.ID.Hex(), nil
}

// GetByID 查询订单；非法 hex 返回 ErrInvalidID，未命中返回 mongo.ErrNoDocuments。
func (f *FakeChangeOrderRepo) GetByID(_ context.Context, id string) (domain.ChangeOrder, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return domain.ChangeOrder{}, fmt.Errorf("%w: %q", domain.ErrInvalidID, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.byID[oid.Hex()]
	if !ok {
		return domain.ChangeOrder{}, mongo.ErrNoDocuments
	}
	return cloneChangeOrder(*o), nil
}

// GetByMutexToken 按互斥 token 查活跃单；未命中返回 mongo.ErrNoDocuments。
func (f *FakeChangeOrderRepo) GetByMutexToken(_ context.Context, token string) (domain.ChangeOrder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.tokens[token]
	if !ok {
		return domain.ChangeOrder{}, mongo.ErrNoDocuments
	}
	return cloneChangeOrder(*f.byID[id]), nil
}

// ListVerifyingActive 过滤 status=verifying 且 verifyWindowUntil > after，createdAt 升序。
func (f *FakeChangeOrderRepo) ListVerifyingActive(_ context.Context, after time.Time) ([]domain.ChangeOrder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.ChangeOrder{}
	for _, o := range f.byID {
		if o.Status != domain.ChangeStatusVerifying || o.VerifyWindowUntil == nil {
			continue
		}
		if o.VerifyWindowUntil.After(after) {
			out = append(out, cloneChangeOrder(*o))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// TransitionActive 状态迁移与 token 写入同步生效；token 冲突返回 ErrChangeInFlight。
func (f *FakeChangeOrderRepo) TransitionActive(_ context.Context, id string, to domain.ChangeStatus, mutexToken string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, err := f.orderLocked(id)
	if err != nil {
		return err
	}
	if mutexToken != "" {
		if holder, ok := f.tokens[mutexToken]; ok && holder != id {
			return domain.ErrChangeInFlight
		}
	}
	f.releaseTokenLocked(id, o.ActiveMutex)
	o.Status = to
	o.ActiveMutex = mutexToken
	if mutexToken != "" {
		f.tokens[mutexToken] = id
	}
	return nil
}

// TransitionTerminal 状态迁移与 token 清除同步生效。
func (f *FakeChangeOrderRepo) TransitionTerminal(_ context.Context, id string, to domain.ChangeStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, err := f.orderLocked(id)
	if err != nil {
		return err
	}
	f.releaseTokenLocked(id, o.ActiveMutex)
	o.Status = to
	o.ActiveMutex = ""
	return nil
}

// TransitionTerminalWithProtect 完成类终态：状态迁移、protectUntil 固化与
// token 清除同步生效。
func (f *FakeChangeOrderRepo) TransitionTerminalWithProtect(_ context.Context, id string, to domain.ChangeStatus, protectUntil time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, err := f.orderLocked(id)
	if err != nil {
		return err
	}
	f.releaseTokenLocked(id, o.ActiveMutex)
	o.Status = to
	o.ActiveMutex = ""
	u := protectUntil
	o.ProtectUntil = &u
	return nil
}

// ListPausedBefore 批间暂停超时扫描集：status=executing 且 paused=true 且
// pausedAt 早于 before（终态单不重复扫描），createdAt 升序。
func (f *FakeChangeOrderRepo) ListPausedBefore(_ context.Context, before time.Time) ([]domain.ChangeOrder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.ChangeOrder{}
	for _, o := range f.byID {
		if o.Status != domain.ChangeStatusExecuting || o.BatchInfo == nil || !o.BatchInfo.Paused || o.BatchInfo.PausedAt == nil {
			continue
		}
		if o.BatchInfo.PausedAt.Before(before) {
			out = append(out, cloneChangeOrder(*o))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// SetBatchInfo Confirm 固化批次分配（任务 5.7）：CAS status=pending_confirm。
func (f *FakeChangeOrderRepo) SetBatchInfo(_ context.Context, id string, batch *domain.BatchInfo) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, err := f.orderLocked(id)
	if err != nil {
		return false, err
	}
	if o.Status != domain.ChangeStatusPendingConfirm {
		return false, nil
	}
	b := *batch
	if batch.PausedAt != nil {
		at := *batch.PausedAt
		b.PausedAt = &at
	}
	o.BatchInfo = &b
	return true, nil
}

// EnterVerify 进入验证窗口（任务 5.7）：CAS executing→verifying + verifyWindowUntil；
// 分批单（batchInfo 存在）一并固化批间暂停标记；未分批单保持 batchInfo 缺失。
func (f *FakeChangeOrderRepo) EnterVerify(_ context.Context, id string, verifyWindowUntil, pausedAt time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, err := f.orderLocked(id)
	if err != nil {
		return false, err
	}
	if o.Status != domain.ChangeStatusExecuting {
		return false, nil
	}
	o.Status = domain.ChangeStatusVerifying
	w := verifyWindowUntil
	o.VerifyWindowUntil = &w
	if o.BatchInfo != nil {
		o.BatchInfo.Paused = true
		at := pausedAt
		o.BatchInfo.PausedAt = &at
	}
	return true, nil
}

// AdvanceBatch 人工续批放行（任务 5.7）：CAS fromStatus→executing +
// currentBatch=nextBatch + paused=false + pausedAt 清除。
func (f *FakeChangeOrderRepo) AdvanceBatch(_ context.Context, id string, fromStatus domain.ChangeStatus, nextBatch int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, err := f.orderLocked(id)
	if err != nil {
		return false, err
	}
	if o.Status != fromStatus {
		return false, nil
	}
	if o.BatchInfo == nil {
		return false, fmt.Errorf("certtest: advance batch on unbatched order %s", id)
	}
	o.Status = domain.ChangeStatusExecuting
	o.BatchInfo.CurrentBatch = nextBatch
	o.BatchInfo.Paused = false
	o.BatchInfo.PausedAt = nil
	return true, nil
}

// SetVerifyExpected 固化验证窗口预期终态快照（任务 5.10）：CAS status=verifying，
// verifyExpected 与 verifyWindowUntil 同原子写入（对齐真实仓储两口径一致语义）。
func (f *FakeChangeOrderRepo) SetVerifyExpected(_ context.Context, id string, expected *domain.VerifyExpected) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, err := f.orderLocked(id)
	if err != nil {
		return false, err
	}
	if o.Status != domain.ChangeStatusVerifying {
		return false, nil
	}
	if expected != nil {
		e := *expected
		e.Domains = append([]string(nil), expected.Domains...)
		e.ExcludedDomains = append([]string(nil), expected.ExcludedDomains...)
		o.VerifyExpected = &e
		w := expected.WindowUntil
		o.VerifyWindowUntil = &w
	} else {
		o.VerifyExpected = nil
	}
	return true, nil
}

// PauseAfterVerify 批级窗口收敛回批间暂停（任务 5.10）：CAS verifying→executing +
// paused=true/pausedAt（当前批保持；仅分批单命中——batchInfo 缺失时 CAS 未命中）。
func (f *FakeChangeOrderRepo) PauseAfterVerify(_ context.Context, id string, pausedAt time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, err := f.orderLocked(id)
	if err != nil {
		return false, err
	}
	if o.Status != domain.ChangeStatusVerifying || o.BatchInfo == nil {
		return false, nil
	}
	o.Status = domain.ChangeStatusExecuting
	o.BatchInfo.Paused = true
	at := pausedAt
	o.BatchInfo.PausedAt = &at
	return true, nil
}

// ListVerifyingExpired 窗口到期扫描集（任务 5.10 window-expiry）：status=verifying
// 且 verifyWindowUntil 非空且 <= before，createdAt 升序。
func (f *FakeChangeOrderRepo) ListVerifyingExpired(_ context.Context, before time.Time) ([]domain.ChangeOrder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.ChangeOrder{}
	for _, o := range f.byID {
		if o.Status != domain.ChangeStatusVerifying || o.VerifyWindowUntil == nil {
			continue
		}
		if !o.VerifyWindowUntil.After(before) {
			out = append(out, cloneChangeOrder(*o))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// ListByNewCertID 按新证书 ID 查询（任务 5.9 orphan-cleanup 归属单判定），
// createdAt 降序稳定返回。
func (f *FakeChangeOrderRepo) ListByNewCertID(_ context.Context, newCertID string) ([]domain.ChangeOrder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.ChangeOrder{}
	for _, o := range f.byID {
		if o.NewCertID == newCertID {
			out = append(out, cloneChangeOrder(*o))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID.Hex() > out[j].ID.Hex()
	})
	return out, nil
}

// ListPage 分页查询（任务 5.11 状态 Tab 筛选）：status 非空时过滤；
// createdAt 降序 + ID 降序 tie-breaker 稳定返回；limit<=0 返回空切片；
// 总数独立统计（同一筛选）。
func (f *FakeChangeOrderRepo) ListPage(_ context.Context, status domain.ChangeStatus, skip, limit int) ([]domain.ChangeOrder, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	matched := []domain.ChangeOrder{}
	for _, o := range f.byID {
		if status != "" && o.Status != status {
			continue
		}
		matched = append(matched, cloneChangeOrder(*o))
	}
	sort.Slice(matched, func(i, j int) bool {
		if !matched[i].CreatedAt.Equal(matched[j].CreatedAt) {
			return matched[i].CreatedAt.After(matched[j].CreatedAt)
		}
		return matched[i].ID.Hex() > matched[j].ID.Hex()
	})
	total := int64(len(matched))
	if limit <= 0 {
		return []domain.ChangeOrder{}, total, nil
	}
	if skip < 0 {
		skip = 0
	}
	if skip >= len(matched) {
		return []domain.ChangeOrder{}, total, nil
	}
	end := skip + limit
	if end > len(matched) {
		end = len(matched)
	}
	return matched[skip:end], total, nil
}

// orderLocked 按 hex ID 取订单（调用方持锁）；未命中返回 mongo.ErrNoDocuments。
func (f *FakeChangeOrderRepo) orderLocked(id string) (*domain.ChangeOrder, error) {
	o, ok := f.byID[id]
	if !ok {
		return nil, mongo.ErrNoDocuments
	}
	return o, nil
}

// releaseTokenLocked 清除订单持有的 token（仅当仍由本订单持有时）。
func (f *FakeChangeOrderRepo) releaseTokenLocked(id, token string) {
	if token != "" && f.tokens[token] == id {
		delete(f.tokens, token)
	}
}

// cloneChangeOrder 深拷贝（隔离指针字段）。
func cloneChangeOrder(o domain.ChangeOrder) domain.ChangeOrder {
	out := o
	if o.BatchInfo != nil {
		b := *o.BatchInfo
		if o.BatchInfo.PausedAt != nil {
			at := *o.BatchInfo.PausedAt
			b.PausedAt = &at
		}
		out.BatchInfo = &b
	}
	if o.VerifyWindowUntil != nil {
		v := *o.VerifyWindowUntil
		out.VerifyWindowUntil = &v
	}
	if o.ProtectUntil != nil {
		p := *o.ProtectUntil
		out.ProtectUntil = &p
	}
	if o.VerifyExpected != nil {
		ve := *o.VerifyExpected
		ve.Domains = append([]string(nil), o.VerifyExpected.Domains...)
		ve.ExcludedDomains = append([]string(nil), o.VerifyExpected.ExcludedDomains...)
		out.VerifyExpected = &ve
	}
	return out
}

// FakeChangeItemRepo 变更项内存假实现。
type FakeChangeItemRepo struct {
	mu    sync.Mutex
	items []*domain.ChangeItem
}

// NewFakeChangeItemRepo 创建空变更项假实现。
func NewFakeChangeItemRepo() *FakeChangeItemRepo {
	return &FakeChangeItemRepo{}
}

// CreateMulti 批量写入（模拟 DEFAULT 填充：status=pending），返回写入条数。
func (f *FakeChangeItemRepo) CreateMulti(_ context.Context, items []domain.ChangeItem) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range items {
		if items[i].Status == "" {
			items[i].Status = domain.ItemStatusPending
		}
		if items[i].ID.IsZero() {
			items[i].ID = primitive.NewObjectID()
		}
		stored := items[i]
		f.items = append(f.items, &stored)
	}
	return len(items), nil
}

// ListByOrder 订单全部变更项（写入序稳定返回）。
func (f *FakeChangeItemRepo) ListByOrder(_ context.Context, orderID string) ([]domain.ChangeItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.ChangeItem{}
	for _, it := range f.items {
		if it.OrderID == orderID {
			out = append(out, *it)
		}
	}
	return out, nil
}

// ListByOrderAndBatch 指定批次变更项。
func (f *FakeChangeItemRepo) ListByOrderAndBatch(_ context.Context, orderID string, batchNo int) ([]domain.ChangeItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.ChangeItem{}
	for _, it := range f.items {
		if it.OrderID == orderID && it.BatchNo == batchNo {
			out = append(out, *it)
		}
	}
	return out, nil
}

// GetByID 按文档 ID 查询单个变更项；非法 hex 返回 ErrInvalidID，未命中返回
// mongo.ErrNoDocuments（任务 5.7 子任务按项取载）。
func (f *FakeChangeItemRepo) GetByID(_ context.Context, itemID string) (domain.ChangeItem, error) {
	oid, err := primitive.ObjectIDFromHex(itemID)
	if err != nil {
		return domain.ChangeItem{}, fmt.Errorf("%w: %q", domain.ErrInvalidID, itemID)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, it := range f.items {
		if it.ID == oid {
			return *it, nil
		}
	}
	return domain.ChangeItem{}, mongo.ErrNoDocuments
}

// AssignBatches Confirm 固化批次归属（任务 5.7）：逐项写入 batchNo
// （orderId 双重限定，模拟仓储 UpdateOne 过滤条件）。
func (f *FakeChangeItemRepo) AssignBatches(_ context.Context, orderID string, assignments []domain.ItemBatchAssignment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range assignments {
		oid, err := primitive.ObjectIDFromHex(a.ItemID)
		if err != nil {
			return fmt.Errorf("%w: %q", domain.ErrInvalidID, a.ItemID)
		}
		for _, it := range f.items {
			if it.ID == oid && it.OrderID == orderID {
				it.BatchNo = a.BatchNo
				break
			}
		}
	}
	return nil
}

// ClaimForExecution 子任务领取执行权（任务 5.7）：CAS pending→running，
// 同原子写 heartbeatAt/executedAt；未命中（已领取/终态）返回 false。
func (f *FakeChangeItemRepo) ClaimForExecution(_ context.Context, itemID string, at time.Time) (bool, error) {
	oid, err := primitive.ObjectIDFromHex(itemID)
	if err != nil {
		return false, fmt.Errorf("%w: %q", domain.ErrInvalidID, itemID)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, it := range f.items {
		if it.ID == oid {
			if it.Status != domain.ItemStatusPending {
				return false, nil
			}
			it.Status = domain.ItemStatusRunning
			hb := at
			it.HeartbeatAt = &hb
			ex := at
			it.ExecutedAt = &ex
			return true, nil
		}
	}
	return false, mongo.ErrNoDocuments
}

// MarkRateLimited 云 API 限流退避标记（任务 5.7）：→rate_limited + heartbeatAt
// 刷新（退避期间保活）。
func (f *FakeChangeItemRepo) MarkRateLimited(_ context.Context, itemID string, at time.Time) error {
	oid, err := primitive.ObjectIDFromHex(itemID)
	if err != nil {
		return fmt.Errorf("%w: %q", domain.ErrInvalidID, itemID)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, it := range f.items {
		if it.ID == oid {
			if it.Status != domain.ItemStatusRunning && it.Status != domain.ItemStatusRateLimited {
				return nil // 与真实仓储 UpdateOne 过滤一致：非运行态静默未命中
			}
			it.Status = domain.ItemStatusRateLimited
			hb := at
			it.HeartbeatAt = &hb
			return nil
		}
	}
	return mongo.ErrNoDocuments
}

// FinishItem 项级终态收敛（任务 5.7）：CAS running/rate_limited→终态；
// 未命中（超时恢复竞争落败/已终态）返回 false。
func (f *FakeChangeItemRepo) FinishItem(_ context.Context, itemID string, status domain.ChangeItemStatus, errMsg, newCloudCertID string, at time.Time) (bool, error) {
	oid, err := primitive.ObjectIDFromHex(itemID)
	if err != nil {
		return false, fmt.Errorf("%w: %q", domain.ErrInvalidID, itemID)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, it := range f.items {
		if it.ID == oid {
			if it.Status != domain.ItemStatusRunning && it.Status != domain.ItemStatusRateLimited {
				return false, nil
			}
			it.Status = status
			if errMsg != "" {
				it.Error = errMsg
			}
			if newCloudCertID != "" {
				it.NewCloudCertID = newCloudCertID
			}
			hb := at
			it.HeartbeatAt = &hb
			return true, nil
		}
	}
	return false, mongo.ErrNoDocuments
}

// FinishRollback 回滚项级终态（任务 5.8）：CAS status=success → rolled_back |
// rollback_failed（errMsg 非空时写入 error）；非 success 幂等返回 false；
// status 非两回滚终态之一返回错误。
func (f *FakeChangeItemRepo) FinishRollback(_ context.Context, itemID string, status domain.ChangeItemStatus, errMsg string) (bool, error) {
	if status != domain.ItemStatusRolledBack && status != domain.ItemStatusRollbackFailed {
		return false, fmt.Errorf("fake change item repo: finish rollback only accepts rolled_back/rollback_failed, got %s", status)
	}
	oid, err := primitive.ObjectIDFromHex(itemID)
	if err != nil {
		return false, fmt.Errorf("%w: %q", domain.ErrInvalidID, itemID)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, it := range f.items {
		if it.ID == oid {
			if it.Status != domain.ItemStatusSuccess {
				return false, nil
			}
			it.Status = status
			if errMsg != "" {
				it.Error = errMsg
			}
			return true, nil
		}
	}
	return false, mongo.ErrNoDocuments
}

// ListRunningBefore running 且心跳早于 before（或缺失）的变更项
// （executing-timeout 扫描集，任务 5.7）。
func (f *FakeChangeItemRepo) ListRunningBefore(_ context.Context, before time.Time) ([]domain.ChangeItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.ChangeItem{}
	for _, it := range f.items {
		if it.Status != domain.ItemStatusRunning {
			continue
		}
		if it.HeartbeatAt == nil || it.HeartbeatAt.Before(before) {
			out = append(out, *it)
		}
	}
	return out, nil
}

// UpdateHeartbeat 刷新执行心跳；非法 hex 返回 ErrInvalidID。
func (f *FakeChangeItemRepo) UpdateHeartbeat(_ context.Context, itemID string, at time.Time) error {
	oid, err := primitive.ObjectIDFromHex(itemID)
	if err != nil {
		return fmt.Errorf("%w: %q", domain.ErrInvalidID, itemID)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, it := range f.items {
		if it.ID == oid {
			hb := at
			it.HeartbeatAt = &hb
			return nil
		}
	}
	return mongo.ErrNoDocuments
}

// MarkPendingSkipped 将订单 pending 项标 skipped，返回标记条数。
func (f *FakeChangeItemRepo) MarkPendingSkipped(_ context.Context, orderID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, it := range f.items {
		if it.OrderID == orderID && it.Status == domain.ItemStatusPending {
			it.Status = domain.ItemStatusSkipped
			n++
		}
	}
	return n, nil
}

// ListPatchCRDDueRecheck patch_crd 项到期复检扫描集（任务 5.9）：success 且
// recheckedAt 缺失且 executedAt <= before；executedAt 缺失不构成候选，
// executedAt 升序稳定返回。
func (f *FakeChangeItemRepo) ListPatchCRDDueRecheck(_ context.Context, before time.Time) ([]domain.ChangeItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.ChangeItem{}
	for _, it := range f.items {
		if it.Action != domain.ActionPatchCRD || it.Status != domain.ItemStatusSuccess ||
			it.RecheckedAt != nil || it.ExecutedAt == nil || it.ExecutedAt.After(before) {
			continue
		}
		out = append(out, *it)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ExecutedAt.Before(*out[j].ExecutedAt)
	})
	return out, nil
}

// MarkRechecked 复检结果回填（任务 5.9）：CAS status=success——passed 保持
// success 写 recheckedAt；failed 转 failed 写 error + recheckedAt；未命中
// （已迁移/已复检）返回 false。
func (f *FakeChangeItemRepo) MarkRechecked(_ context.Context, itemID string, passed bool, errMsg string, at time.Time) (bool, error) {
	oid, err := primitive.ObjectIDFromHex(itemID)
	if err != nil {
		return false, fmt.Errorf("%w: %q", domain.ErrInvalidID, itemID)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, it := range f.items {
		if it.ID == oid {
			if it.Status != domain.ItemStatusSuccess {
				return false, nil
			}
			if !passed {
				it.Status = domain.ItemStatusFailed
				if errMsg != "" {
					it.Error = errMsg
				}
			}
			atCopy := at
			it.RecheckedAt = &atCopy
			return true, nil
		}
	}
	return false, mongo.ErrNoDocuments
}

// FakeCloudCertMappingRepo 平台证书↔云证书映射内存假实现（任务 5.3 起供
// deployer/service 层测试共享）。模拟 uk_fp_cloud_account 唯一约束的 Upsert
// 语义（同 certFingerprint+cloud+accountKey 覆盖字段并保留 _id）与
// FindByCloudCertID 的 uploadedAt 降序取首条。
type FakeCloudCertMappingRepo struct {
	mu       sync.Mutex
	mappings []*domain.CloudCertMapping
}

// NewFakeCloudCertMappingRepo 创建空映射假实现。
func NewFakeCloudCertMappingRepo() *FakeCloudCertMappingRepo {
	return &FakeCloudCertMappingRepo{}
}

// Upsert 按 uk_fp_cloud_account 去重写入（模拟 DEFAULT 填充：
// uploadedAt=now、status=active；命中唯一键时保留 _id 仅覆盖字段）。
func (f *FakeCloudCertMappingRepo) Upsert(_ context.Context, m *domain.CloudCertMapping) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m.UploadedAt.IsZero() {
		m.UploadedAt = time.Now()
	}
	if m.Status == "" {
		m.Status = domain.MappingStatusActive
	}
	for _, existing := range f.mappings {
		if existing.CertFingerprint == m.CertFingerprint &&
			existing.Cloud == m.Cloud && existing.AccountKey == m.AccountKey {
			stored := *m
			stored.ID = existing.ID // UpdateOne upsert 命中时保留 _id
			*existing = stored
			return nil
		}
	}
	if m.ID.IsZero() {
		m.ID = primitive.NewObjectID()
	}
	stored := *m
	f.mappings = append(f.mappings, &stored)
	return nil
}

// ListByFingerprint 按指纹查询全部映射（写入序稳定返回）。
func (f *FakeCloudCertMappingRepo) ListByFingerprint(_ context.Context, fingerprint string) ([]domain.CloudCertMapping, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.CloudCertMapping{}
	for _, m := range f.mappings {
		if m.CertFingerprint == fingerprint {
			out = append(out, *m)
		}
	}
	return out, nil
}

// FindByCloudCertID 反查映射；cloud/accountKey 空串=通配；多条命中按
// uploadedAt 降序取首条；无命中返回 mongo.ErrNoDocuments。
func (f *FakeCloudCertMappingRepo) FindByCloudCertID(_ context.Context, cloud, accountKey, cloudCertID string) (domain.CloudCertMapping, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best *domain.CloudCertMapping
	for _, m := range f.mappings {
		if m.CloudCertID != cloudCertID {
			continue
		}
		if cloud != "" && m.Cloud != cloud {
			continue
		}
		if accountKey != "" && m.AccountKey != accountKey {
			continue
		}
		if best == nil || m.UploadedAt.After(best.UploadedAt) {
			best = m
		}
	}
	if best == nil {
		return domain.CloudCertMapping{}, mongo.ErrNoDocuments
	}
	return *best, nil
}

// UpdateStatus 映射状态迁移（active→orphan）；非法 hex 返回 ErrInvalidID，
// 未命中返回 mongo.ErrNoDocuments。
func (f *FakeCloudCertMappingRepo) UpdateStatus(_ context.Context, id string, status domain.MappingStatus) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("%w: %q", domain.ErrInvalidID, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.mappings {
		if m.ID == oid {
			m.Status = status
			return nil
		}
	}
	return mongo.ErrNoDocuments
}

// ListByStatus 按状态查询全部映射（任务 5.9 orphan-cleanup 天级批扫扫描集），
// uploadedAt 升序稳定返回。
func (f *FakeCloudCertMappingRepo) ListByStatus(_ context.Context, status domain.MappingStatus) ([]domain.CloudCertMapping, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.CloudCertMapping{}
	for _, m := range f.mappings {
		if m.Status == status {
			out = append(out, *m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UploadedAt.Equal(out[j].UploadedAt) {
			return out[i].UploadedAt.Before(out[j].UploadedAt)
		}
		return out[i].ID.Hex() < out[j].ID.Hex()
	})
	return out, nil
}

// DeleteByID 清理成功后删除映射（任务 5.9：orphan→清理成功即删除）；
// 非法 hex 返回 ErrInvalidID，未命中返回 mongo.ErrNoDocuments。
func (f *FakeCloudCertMappingRepo) DeleteByID(_ context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("%w: %q", domain.ErrInvalidID, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, m := range f.mappings {
		if m.ID == oid {
			f.mappings = append(f.mappings[:i], f.mappings[i+1:]...)
			return nil
		}
	}
	return mongo.ErrNoDocuments
}
