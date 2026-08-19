package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ---------------------------------------------------------------------
// 孤儿清理消费者测试（任务 5.9 AC：事件/批扫消费、映射状态流转、保护期与
// 在途门槛（Hard Rule）、失败告警、幂等去重）
// ---------------------------------------------------------------------

const (
	orphanTestOldFP = "0101010101010101010101010101010101010101010101010101010101010101" // 64 hex
	orphanTestNewFP = "fefefefefefefefefefefefefefefefefefefefefefefefefefefefefefefefe" // 64 hex
)

// orphanCleanerCall 清理调用记录。
type orphanCleanerCall struct {
	Cloud     string
	CertID    string
	CredsKind string
}

// orphanFakeCleaner 清理执行假实现（mock 通道）：failFor 命中的 certID 返回错误。
type orphanFakeCleaner struct {
	mu      sync.Mutex
	calls   []orphanCleanerCall
	failFor map[string]error
}

func newOrphanFakeCleaner() *orphanFakeCleaner {
	return &orphanFakeCleaner{failFor: map[string]error{}}
}

func (f *orphanFakeCleaner) CleanupOrphanCert(_ context.Context, creds deployer.Credential, cloud, cloudCertID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, orphanCleanerCall{Cloud: cloud, CertID: cloudCertID, CredsKind: creds.Kind})
	if err, ok := f.failFor[cloudCertID]; ok {
		return err
	}
	return nil
}

func (f *orphanFakeCleaner) recorded() []orphanCleanerCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]orphanCleanerCall(nil), f.calls...)
}

func (f *orphanFakeCleaner) callsFor(certID string) int {
	n := 0
	for _, c := range f.recorded() {
		if c.CertID == certID {
			n++
		}
	}
	return n
}

// orphanRecordedEntry 报告写入记录（orderID + 结果）。
type orphanRecordedEntry struct {
	OrderID string
	Result  OrphanCleanupResult
}

// orphanFakeRecorder 报告写入假实现：以 (orderID, cloudCertId, action, success)
// 去重（AC-4 幂等键——生产实现契约同口径）。
type orphanFakeRecorder struct {
	mu      sync.Mutex
	entries []orphanRecordedEntry
	seen    map[string]struct{}
}

func newOrphanFakeRecorder() *orphanFakeRecorder {
	return &orphanFakeRecorder{seen: map[string]struct{}{}}
}

func (f *orphanFakeRecorder) RecordOrphanCleanup(_ context.Context, orderID string, result OrphanCleanupResult) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fmt.Sprintf("%s|%s|%s|%t", orderID, result.CloudCertID, result.Action, result.Success)
	if _, dup := f.seen[key]; dup {
		return false, nil
	}
	f.seen[key] = struct{}{}
	f.entries = append(f.entries, orphanRecordedEntry{OrderID: orderID, Result: result})
	return true, nil
}

func (f *orphanFakeRecorder) recorded() []orphanRecordedEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]orphanRecordedEntry(nil), f.entries...)
}

// orphanFailingRecorder 注入报告写入失败（基础设施故障路径）。
type orphanFailingRecorder struct{}

func (orphanFailingRecorder) RecordOrphanCleanup(context.Context, string, OrphanCleanupResult) (bool, error) {
	return false, errors.New("report store down")
}

// orphanSeedMapping 映射种子。
type orphanSeedMapping struct {
	fp      string
	cloud   string
	account string
	certID  string
	orphan  bool
}

// orphanHarness 孤儿清理测试依赖聚合。
type orphanHarness struct {
	svc       OrphanCleanupService
	orders    *certtest.FakeChangeOrderRepo
	items     *certtest.FakeChangeItemRepo
	certs     *certtest.FakeCertificateRepo
	mappings  *certtest.FakeCloudCertMappingRepo
	cleaner   *orphanFakeCleaner
	recorder  *orphanFakeRecorder
	publisher *InMemoryAlertPublisher
	now       time.Time
	newCertID string
	orderID   string
}

func newOrphanHarness(t *testing.T) *orphanHarness {
	t.Helper()
	h := &orphanHarness{
		orders:    certtest.NewFakeChangeOrderRepo(),
		items:     certtest.NewFakeChangeItemRepo(),
		certs:     certtest.NewFakeCertificateRepo(),
		mappings:  certtest.NewFakeCloudCertMappingRepo(),
		cleaner:   newOrphanFakeCleaner(),
		recorder:  newOrphanFakeRecorder(),
		publisher: NewInMemoryAlertPublisher(),
		now:       time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC),
	}
	h.rewire(NewOrphanCleanupService(
		h.orders, h.items, h.certs, h.mappings,
		h.cleaner, fakeCredentialSource{}, h.recorder, h.publisher,
	))
	// 新证书台账（order.NewCertID 指向）。
	newCert := &domain.Certificate{Fingerprint: orphanTestNewFP, HostingStatus: domain.HostingStatusComplete}
	require.NoError(t, h.certs.Create(context.Background(), newCert))
	h.newCertID = newCert.ID.Hex()
	return h
}

// rewire 装配服务实现并注入固定时钟。
func (h *orphanHarness) rewire(svc OrphanCleanupService) {
	impl := svc.(*orphanCleanupService)
	impl.now = func() time.Time { return h.now }
	h.svc = impl
}

// seedOldCert 旧证书台账（保护期可选）。
func (h *orphanHarness) seedOldCert(t *testing.T, protectUntil *time.Time) {
	t.Helper()
	require.NoError(t, h.certs.Create(context.Background(), &domain.Certificate{
		Fingerprint:   orphanTestOldFP,
		HostingStatus: domain.HostingStatusComplete,
		ProtectUntil:  protectUntil,
	}))
}

// seedTerminalOrder 终态单（completed）+ 一个成功云项（旧证书替换完成）。
func (h *orphanHarness) seedTerminalOrder(t *testing.T, itemOldCertID string) {
	t.Helper()
	orderID, err := h.orders.Create(context.Background(), &domain.ChangeOrder{
		OldCertFingerprint: orphanTestOldFP,
		NewCertID:          h.newCertID,
		Status:             domain.ChangeStatusPendingConfirm,
		ActiveMutex:        orphanTestOldFP,
	})
	require.NoError(t, err)
	require.NoError(t, h.orders.TransitionTerminal(context.Background(), orderID, domain.ChangeStatusCompleted))
	h.orderID = orderID
	_, err = h.items.CreateMulti(context.Background(), []domain.ChangeItem{{
		OrderID: orderID,
		Action:  domain.ActionUploadAndBind,
		ResourceRef: domain.ResourceRef{
			Channel: domain.ChannelCloudAPI, Cloud: "aliyun", Product: "cdn",
			AccountKey: "acc1", ResourceID: "res-1",
		},
		OldCloudCertID: itemOldCertID,
		Status:         domain.ItemStatusSuccess,
	}})
	require.NoError(t, err)
}

// seedMapping 写入映射种子，返回映射 ID。
func (h *orphanHarness) seedMapping(t *testing.T, s orphanSeedMapping) string {
	t.Helper()
	status := domain.MappingStatusActive
	if s.orphan {
		status = domain.MappingStatusOrphan
	}
	require.NoError(t, h.mappings.Upsert(context.Background(), &domain.CloudCertMapping{
		CertFingerprint: s.fp,
		Cloud:           s.cloud,
		AccountKey:      s.account,
		CloudCertID:     s.certID,
		Status:          status,
	}))
	m, err := h.mappings.FindByCloudCertID(context.Background(), s.cloud, s.account, s.certID)
	require.NoError(t, err)
	return m.ID.Hex()
}

func (h *orphanHarness) opsAlerts() []CertAlertEvent {
	var out []CertAlertEvent
	for _, e := range h.publisher.Events() {
		if e.Category == AlertCategoryOps {
			out = append(out, e)
		}
	}
	return out
}

// ---- AC-1：事件触发消费（终态单清理队列 + 成功项旧证书入队 + 结果入报告） ----

func TestOrphanCleanupConsumeOrderQueueCleansAndRecords(t *testing.T) {
	h := newOrphanHarness(t)
	h.seedOldCert(t, nil) // 无保护期
	h.seedTerminalOrder(t, "cert-old-1")
	// 旧证书映射仍 active：ConsumeOrderQueue 先入队（orphan）再清理（AC-1 入队语义）。
	h.seedMapping(t, orphanSeedMapping{fp: orphanTestOldFP, cloud: "aliyun", account: "acc1", certID: "cert-old-1"})
	// 新证书孤儿映射（5.8 回滚标记产物，已 orphan）。
	h.seedMapping(t, orphanSeedMapping{fp: orphanTestNewFP, cloud: "tencent", account: "acc2", certID: "cert-new-9", orphan: true})

	consumed, err := h.svc.ConsumeOrderQueue(context.Background(), h.orderID)
	require.NoError(t, err)
	assert.Equal(t, 2, consumed)

	// 逐项调 CleanupOrphan（cloud + certID 路由正确）。
	calls := h.cleaner.recorded()
	assert.Len(t, calls, 2)
	byCert := map[string]orphanCleanerCall{}
	for _, c := range calls {
		byCert[c.CertID] = c
	}
	assert.Equal(t, "aliyun", byCert["cert-old-1"].Cloud)
	assert.Equal(t, "tencent", byCert["cert-new-9"].Cloud)
	assert.Equal(t, deployer.CredentialKindCloudAK, byCert["cert-old-1"].CredsKind)

	// AC-2：清理成功后映射删除（orphan→cleaned 以删除承载）。
	orphans, err := h.mappings.ListByStatus(context.Background(), domain.MappingStatusOrphan)
	require.NoError(t, err)
	assert.Empty(t, orphans)

	// 结果 OrphanCleanupResult{cloud,cloudCertId,action,success,at} 写入变更报告。
	entries := h.recorder.recorded()
	require.Len(t, entries, 2)
	for _, e := range entries {
		assert.Equal(t, h.orderID, e.OrderID)
		assert.Equal(t, OrphanActionCleanup, e.Result.Action)
		assert.True(t, e.Result.Success)
		assert.Equal(t, h.now, e.Result.At)
	}
	assert.Empty(t, h.opsAlerts()) // 成功不告警
}

// AC-1：非终态单拒绝消费（验证未达标不清理）。
func TestOrphanCleanupConsumeRequiresTerminalOrder(t *testing.T) {
	h := newOrphanHarness(t)
	h.seedOldCert(t, nil)
	orderID, err := h.orders.Create(context.Background(), &domain.ChangeOrder{
		OldCertFingerprint: orphanTestOldFP,
		NewCertID:          h.newCertID,
		Status:             domain.ChangeStatusVerifying,
		ActiveMutex:        orphanTestOldFP,
	})
	require.NoError(t, err)
	h.seedMapping(t, orphanSeedMapping{fp: orphanTestOldFP, cloud: "aliyun", account: "acc1", certID: "cert-old-1", orphan: true})

	consumed, err := h.svc.ConsumeOrderQueue(context.Background(), orderID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not terminal")
	assert.Zero(t, consumed)
	assert.Empty(t, h.cleaner.recorded()) // Hard Rule：验证未达标不清理
}

// ---- AC-2：清理失败保留 orphan + 运维处置告警 ----

func TestOrphanCleanupFailureKeepsOrphanAndAlertsOps(t *testing.T) {
	h := newOrphanHarness(t)
	h.seedOldCert(t, nil)
	h.seedTerminalOrder(t, "cert-old-1")
	h.seedMapping(t, orphanSeedMapping{fp: orphanTestOldFP, cloud: "aliyun", account: "acc1", certID: "cert-old-1"})
	h.cleaner.failFor["cert-old-1"] = errors.New("cloud api error")

	consumed, err := h.svc.ConsumeOrderQueue(context.Background(), h.orderID)
	require.NoError(t, err) // 清理失败是项级结果，不以 error 呈现
	assert.Equal(t, 1, consumed)

	// 失败项保留 orphan（供重试）。
	orphans, err := h.mappings.ListByStatus(context.Background(), domain.MappingStatusOrphan)
	require.NoError(t, err)
	require.Len(t, orphans, 1)
	assert.Equal(t, "cert-old-1", orphans[0].CloudCertID)

	// 结果 success=false + 运维处置告警（ops 类，不计入四类业务告警）。
	entries := h.recorder.recorded()
	require.Len(t, entries, 1)
	assert.Equal(t, OrphanActionCleanup, entries[0].Result.Action)
	assert.False(t, entries[0].Result.Success)
	alerts := h.opsAlerts()
	require.Len(t, alerts, 1)
	assert.Contains(t, alerts[0].Detail, "cert-old-1")
	assert.Equal(t, h.orderID, alerts[0].OrderID)
}

// ---- AC-2/Hard Rule：保护期内 skip_keep 暂留 ----

func TestOrphanCleanupProtectedCertSkipsKeep(t *testing.T) {
	h := newOrphanHarness(t)
	protect := h.now.Add(3 * 24 * time.Hour)
	h.seedOldCert(t, &protect)
	h.seedTerminalOrder(t, "cert-old-1")
	h.seedMapping(t, orphanSeedMapping{fp: orphanTestOldFP, cloud: "aliyun", account: "acc1", certID: "cert-old-1"})

	consumed, err := h.svc.ConsumeOrderQueue(context.Background(), h.orderID)
	require.NoError(t, err)
	assert.Equal(t, 1, consumed)

	assert.Empty(t, h.cleaner.recorded()) // 不清理
	orphans, err := h.mappings.ListByStatus(context.Background(), domain.MappingStatusOrphan)
	require.NoError(t, err)
	assert.Len(t, orphans, 1) // 映射保留 orphan
	entries := h.recorder.recorded()
	require.Len(t, entries, 1)
	assert.Equal(t, OrphanActionSkipKeep, entries[0].Result.Action)
	assert.True(t, entries[0].Result.Success)
	assert.Empty(t, h.opsAlerts()) // 暂留非失败，不告警
}

// ---- Hard Rule：仍在 active（未替换完成）/归属单在途的映射不清理 ----

func TestOrphanCleanupSkipsActiveAndInFlight(t *testing.T) {
	h := newOrphanHarness(t)
	h.seedOldCert(t, nil)
	h.seedTerminalOrder(t, "cert-old-1")
	// ① 非 orphan（active）映射：未替换完成，不构成清理队列成员（不归属任何
	// 成功项——成功项旧证书映射才入队）。
	h.seedMapping(t, orphanSeedMapping{fp: orphanTestNewFP, cloud: "aliyun", account: "acc1", certID: "cert-new-active"})
	// ② orphan 但证书是另一张活跃单的旧证书（同一证书二次变更在途——回滚目标
	// 保护，替换未完成不清理）。
	activeOrderID, err := h.orders.Create(context.Background(), &domain.ChangeOrder{
		OldCertFingerprint: orphanTestOldFP,
		NewCertID:          h.newCertID,
		Status:             domain.ChangeStatusExecuting,
		ActiveMutex:        orphanTestOldFP,
	})
	require.NoError(t, err)
	h.seedMapping(t, orphanSeedMapping{fp: orphanTestOldFP, cloud: "aliyun", account: "acc1", certID: "cert-old-1", orphan: true})

	// 终态单消费：active 新证书映射静默跳过；旧证书 orphan 映射因活跃单 token 在途跳过。
	consumed, err := h.svc.ConsumeOrderQueue(context.Background(), h.orderID)
	require.NoError(t, err)
	assert.Zero(t, consumed)
	assert.Empty(t, h.cleaner.recorded())
	assert.Empty(t, h.recorder.recorded())

	// 活跃单保持 executing（未被消费动作影响）。
	activeOrder, err := h.orders.GetByID(context.Background(), activeOrderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusExecuting, activeOrder.Status)
}

// Hard Rule：第二段失败孤儿（新证书映射 orphan）在归属单在途期间天级批扫不清理。
func TestOrphanCleanupSweepSkipsInFlightNewCertOrphan(t *testing.T) {
	h := newOrphanHarness(t)
	h.seedOldCert(t, nil)
	// 归属单 executing：NewCertID 指向 newCert（orphanTestNewFP）。
	_, err := h.orders.Create(context.Background(), &domain.ChangeOrder{
		OldCertFingerprint: orphanTestOldFP,
		NewCertID:          h.newCertID,
		Status:             domain.ChangeStatusExecuting,
		ActiveMutex:        orphanTestOldFP,
	})
	require.NoError(t, err)
	h.seedMapping(t, orphanSeedMapping{fp: orphanTestNewFP, cloud: "aliyun", account: "acc1", certID: "cert-new-9", orphan: true})

	consumed, err := h.svc.SweepOrphans(context.Background())
	require.NoError(t, err)
	assert.Zero(t, consumed)
	assert.Empty(t, h.cleaner.recorded())
}

// ---- AC-1：天级批扫（ListByStatus 全量孤儿，事件遗漏兜底） ----

func TestOrphanCleanupSweepOrphans(t *testing.T) {
	h := newOrphanHarness(t)
	h.seedOldCert(t, nil)
	// 新证书处于保护期：其孤儿映射（5.8 回滚产物）批扫 skip_keep。
	protect := h.now.Add(2 * 24 * time.Hour)
	require.NoError(t, h.certs.SetProtectUntil(context.Background(), orphanTestNewFP, protect))
	// rolled_back 终态单（NewCertID → newCert：新证书孤儿的归属单，报告承载）。
	orderID, err := h.orders.Create(context.Background(), &domain.ChangeOrder{
		OldCertFingerprint: orphanTestOldFP,
		NewCertID:          h.newCertID,
		Status:             domain.ChangeStatusPendingConfirm,
		ActiveMutex:        orphanTestOldFP,
	})
	require.NoError(t, err)
	require.NoError(t, h.orders.TransitionTerminal(context.Background(), orderID, domain.ChangeStatusRolledBack))
	// 可清理孤儿（旧证书，归属单已终态、无保护）。
	h.seedMapping(t, orphanSeedMapping{fp: orphanTestOldFP, cloud: "aliyun", account: "acc1", certID: "cert-old-1", orphan: true})
	// 受保护孤儿（新证书，归属 rolled_back 单）。
	h.seedMapping(t, orphanSeedMapping{fp: orphanTestNewFP, cloud: "tencent", account: "acc2", certID: "cert-prot", orphan: true})

	consumed, err := h.svc.SweepOrphans(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, consumed)

	// 可清理项删除；受保护项 skip_keep 保留。
	orphans, err := h.mappings.ListByStatus(context.Background(), domain.MappingStatusOrphan)
	require.NoError(t, err)
	require.Len(t, orphans, 1)
	assert.Equal(t, "cert-prot", orphans[0].CloudCertID)
	entries := h.recorder.recorded()
	require.Len(t, entries, 2)
	byCert := map[string]orphanRecordedEntry{}
	for _, e := range entries {
		byCert[e.Result.CloudCertID] = e
	}
	assert.Equal(t, OrphanActionCleanup, byCert["cert-old-1"].Result.Action)
	assert.True(t, byCert["cert-old-1"].Result.Success)
	// 新证书孤儿归属单可解析（rolled_back 单承载报告）；旧证书孤儿无 NewCertID
	// 语境归属单（天级兜底口径，orderID 空）。
	assert.Equal(t, OrphanActionSkipKeep, byCert["cert-prot"].Result.Action)
	assert.Equal(t, orderID, byCert["cert-prot"].OrderID)
}

// ---- AC-4：消费幂等（重复消费不产生重复结果/重复告警） ----

func TestOrphanCleanupIdempotentDoubleConsumption(t *testing.T) {
	h := newOrphanHarness(t)
	h.seedOldCert(t, nil)
	h.seedTerminalOrder(t, "cert-old-1")
	h.seedMapping(t, orphanSeedMapping{fp: orphanTestOldFP, cloud: "aliyun", account: "acc1", certID: "cert-old-1"})
	h.seedMapping(t, orphanSeedMapping{fp: orphanTestNewFP, cloud: "tencent", account: "acc2", certID: "cert-new-9", orphan: true})
	h.cleaner.failFor["cert-new-9"] = errors.New("cloud api error")

	_, err := h.svc.ConsumeOrderQueue(context.Background(), h.orderID)
	require.NoError(t, err)
	require.Len(t, h.recorder.recorded(), 2)
	require.Len(t, h.opsAlerts(), 1)

	// 第二次消费：成功项映射已删除不再命中；失败项保留 orphan 但 recorder
	// 以 (orderID, certID, action, success) 去重——无新增结果/告警。
	consumed, err := h.svc.ConsumeOrderQueue(context.Background(), h.orderID)
	require.NoError(t, err)
	assert.Equal(t, 1, consumed) // 仅失败项重试
	assert.Equal(t, 1, h.cleaner.callsFor("cert-old-1"))
	assert.Equal(t, 2, h.cleaner.callsFor("cert-new-9")) // 重试动作本身执行
	assert.Len(t, h.recorder.recorded(), 2)              // 结果不重复
	assert.Len(t, h.opsAlerts(), 1)                      // 告警不重复
}

// 基础设施故障（报告写入失败）逐项隔离：清理动作已落地，错误随计数返回。
func TestOrphanCleanupRecorderFailureIsolates(t *testing.T) {
	h := newOrphanHarness(t)
	h.seedOldCert(t, nil)
	h.seedTerminalOrder(t, "cert-old-1")
	h.seedMapping(t, orphanSeedMapping{fp: orphanTestOldFP, cloud: "aliyun", account: "acc1", certID: "cert-old-1"})
	h.rewire(NewOrphanCleanupService(h.orders, h.items, h.certs, h.mappings,
		h.cleaner, fakeCredentialSource{}, orphanFailingRecorder{}, h.publisher))

	consumed, err := h.svc.ConsumeOrderQueue(context.Background(), h.orderID)
	require.Error(t, err)
	assert.Equal(t, 1, consumed) // 清理动作已执行（映射删除），报告写入失败上抛
}

// 台账缺失容错：映射指纹无台账证书（外部/历史数据）视为无保护期、无新证书
// 语境归属单，可正常清理；订单新证书台账缺失仅收窄指纹集合不阻断消费。
func TestOrphanCleanupToleratesMissingLedgerEntries(t *testing.T) {
	h := newOrphanHarness(t)
	h.seedOldCert(t, nil)
	// 订单 NewCertID 指向的证书已被删除（GetByID ErrNoDocuments 容忍）。
	orderID, err := h.orders.Create(context.Background(), &domain.ChangeOrder{
		OldCertFingerprint: orphanTestOldFP,
		NewCertID:          primitive.NewObjectID().Hex(), // 台账不存在的证书 ID
		Status:             domain.ChangeStatusPendingConfirm,
		ActiveMutex:        orphanTestOldFP,
	})
	require.NoError(t, err)
	require.NoError(t, h.orders.TransitionTerminal(context.Background(), orderID, domain.ChangeStatusCancelled))
	// 台账无此指纹的孤儿映射：无保护/无归属单 → 天级批扫直接清理。
	h.seedMapping(t, orphanSeedMapping{fp: "9999999999999999999999999999999999999999999999999999999999999999",
		cloud: "aliyun", account: "acc9", certID: "cert-ghost", orphan: true})

	// 订单消费（新证书台账缺失容错：无错误、无候选——该单无成功项/无 old 孤儿）。
	consumed, err := h.svc.ConsumeOrderQueue(context.Background(), orderID)
	require.NoError(t, err)
	assert.Zero(t, consumed)

	consumed, err = h.svc.SweepOrphans(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, consumed)
	assert.Equal(t, 1, h.cleaner.callsFor("cert-ghost"))
	entries := h.recorder.recorded()
	require.Len(t, entries, 1)
	assert.Equal(t, OrphanActionCleanup, entries[0].Result.Action)
	assert.True(t, entries[0].Result.Success)

	// nil publisher/recorder 装配弹性（回退日志实现/no-op）。
	assert.NotNil(t, NewOrphanCleanupService(h.orders, h.items, h.certs, h.mappings,
		h.cleaner, fakeCredentialSource{}, nil, nil))
}
