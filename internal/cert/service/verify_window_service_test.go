package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
// 验证窗口服务测试（任务 5.10）：
// AC-1 固化 verifyExpected（豁免剔除/按批刷新/固化后不随台账变化）
// AC-2 提频探测 + 连续 verifyConfirmProbes 达标 + 提前达标关闭/批间暂停
// AC-3 window-expiry 终局判定（completed / partial_completed+UnmetDomains /
//      非终批转批间暂停）+ 恢复常规告警
// AC-4 change_linked 路由（窗口内变更关联通道 / 关闭后常规 diff）
// AC-5 豁免∩窗口不死锁（excludedDomains + 无 override 通配符计 skipped）
// AC-6 集成：5.7 执行引擎缝（EnterVerify 后固化、ConfirmBatch 门控联调、
//      分批 verifyExpected 刷新）
// ---------------------------------------------------------------------

// 验证窗口测试用指纹常量（64 位 hex，与执行引擎测试常量独立）。
const (
	vwOldFP  = "aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11"
	vwOldFP2 = "ee55ee55ee55ee55ee55ee55ee55ee55ee55ee55ee55ee55ee55ee55ee55"
	vwOldFP3 = "ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12"
	vwOldFP4 = "cd34cd34cd34cd34cd34cd34cd34cd34cd34cd34cd34cd34cd34cd34cd34"
	vwNewFP  = "dd44dd44dd44dd44dd44dd44dd44dd44dd44dd44dd44dd44dd44dd44dd44"
)

// fakeVerifyRecorder UnmetDomains 写入端口假实现（记录调用供断言）。
type fakeVerifyRecorder struct {
	mu    sync.Mutex
	calls []string // "orderID|d1,d2"
	err   error
}

func (r *fakeVerifyRecorder) RecordUnmetDomains(_ context.Context, orderID string, unmetDomains []string, _ time.Time) (bool, error) {
	if r.err != nil {
		return false, r.err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, orderID+"|"+strings.Join(unmetDomains, ","))
	return true, nil
}

func (r *fakeVerifyRecorder) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

// failingListRecentProbeRepo 注入 ListRecentByDomains 故障（其余语义继承共享假实现）。
type failingListRecentProbeRepo struct {
	*certtest.FakeProbeResultRepo
	err error
}

func (f *failingListRecentProbeRepo) ListRecentByDomains(context.Context, []string, int) ([]domain.ProbeResult, error) {
	return nil, f.err
}

// verifyHarness 验证窗口服务测试依赖聚合。
type verifyHarness struct {
	vw         *verifyWindowService
	orders     *certtest.FakeChangeOrderRepo
	certs      *certtest.FakeCertificateRepo
	exempts    *certtest.FakeExemptionRepo
	alertCfg   *certtest.FakeAlertConfigRepo
	probes     *certtest.FakeProbeResultRepo
	publisher  *InMemoryAlertPublisher
	recorder   *fakeVerifyRecorder
	prober     ProbeService
	newCertIDs map[string]string
	clock      time.Time
}

// newVerifyHarness 组装验证窗口服务。srv 非 nil 时提频探测以本地 SNI server
// 为拨测目标（复用 4.1 测试缝）；nil 时拨测端口不被触达（判定/终局类用例）。
func newVerifyHarness(t *testing.T, srv *sniServer) *verifyHarness {
	t.Helper()
	h := &verifyHarness{
		orders:     certtest.NewFakeChangeOrderRepo(),
		certs:      certtest.NewFakeCertificateRepo(),
		exempts:    certtest.NewFakeExemptionRepo(),
		alertCfg:   certtest.NewFakeAlertConfigRepo(),
		probes:     certtest.NewFakeProbeResultRepo(),
		publisher:  NewInMemoryAlertPublisher(),
		recorder:   &fakeVerifyRecorder{},
		newCertIDs: map[string]string{},
	}
	var dialer tlsDialer = &stdTLSDialer{timeout: 2 * time.Second}
	if srv != nil {
		dialer = &countingDialer{inner: &stdTLSDialer{timeout: 2 * time.Second, addrOverride: srv.addr}}
	}
	h.prober = NewProbeService(h.certs, h.probes, h.exempts, h.alertCfg, h.orders, dialer, ProbeOptions{})
	changes := NewChangeService(h.orders, certtest.NewFakeChangeItemRepo(), h.certs, h.alertCfg,
		certtest.NewFakeScanSnapshotRepo(), certtest.NewFakeCertReferenceRepo(), nil)
	h.vw = NewVerifyWindowService(
		h.orders, h.certs, h.exempts, h.alertCfg, h.probes, h.prober,
		changes, h.recorder, h.publisher,
	).(*verifyWindowService)
	h.clock = time.Now()
	h.vw.now = func() time.Time { return h.clock }
	return h
}

// seedOldCert 台账写入旧证书（目标域名基准 = 旧证书 SAN 集合）。
func (h *verifyHarness) seedOldCert(t *testing.T, oldFP string, sans []string) {
	t.Helper()
	require.NoError(t, h.certs.Create(context.Background(), &domain.Certificate{
		ID:            primitive.NewObjectID(),
		Fingerprint:   oldFP,
		CommonName:    "old.example.com",
		Sans:          sans,
		HostingStatus: domain.HostingStatusComplete,
	}))
}

// seedVerifyingOrder 写入验证中订单（expected 非 nil 时预固化快照；batch 非空
// 为分批单），返回订单 ID。新证书按指纹幂等种子。
func (h *verifyHarness) seedVerifyingOrder(
	t *testing.T, oldFP, newFP string, windowUntil time.Time, expected *domain.VerifyExpected, batch *domain.BatchInfo,
) string {
	t.Helper()
	newCertID, ok := h.newCertIDs[newFP]
	if !ok {
		cert := &domain.Certificate{
			ID:            primitive.NewObjectID(),
			Fingerprint:   newFP,
			CommonName:    "new.example.com",
			HostingStatus: domain.HostingStatusComplete,
		}
		require.NoError(t, h.certs.Create(context.Background(), cert))
		h.newCertIDs[newFP] = cert.ID.Hex()
		newCertID = h.newCertIDs[newFP]
	}
	order := &domain.ChangeOrder{
		OldCertFingerprint: oldFP,
		NewCertID:          newCertID,
		Status:             domain.ChangeStatusVerifying,
		VerifyWindowUntil:  &windowUntil,
		ActiveMutex:        oldFP,
		VerifyExpected:     expected,
		BatchInfo:          batch,
		Creator:            "operator",
	}
	orderID, err := h.orders.Create(context.Background(), order)
	require.NoError(t, err)
	return orderID
}

// seedExecutingOrder 写入执行中订单（进入验证窗口前形态）。
func (h *verifyHarness) seedExecutingOrder(t *testing.T, oldFP, newFP string, batch *domain.BatchInfo) string {
	t.Helper()
	newCertID, ok := h.newCertIDs[newFP]
	if !ok {
		cert := &domain.Certificate{
			ID: primitive.NewObjectID(), Fingerprint: newFP,
			CommonName: "new.example.com", HostingStatus: domain.HostingStatusComplete,
		}
		require.NoError(t, h.certs.Create(context.Background(), cert))
		h.newCertIDs[newFP] = cert.ID.Hex()
		newCertID = h.newCertIDs[newFP]
	}
	order := &domain.ChangeOrder{
		OldCertFingerprint: oldFP,
		NewCertID:          newCertID,
		Status:             domain.ChangeStatusExecuting,
		ActiveMutex:        oldFP,
		BatchInfo:          batch,
		Creator:            "operator",
	}
	orderID, err := h.orders.Create(context.Background(), order)
	require.NoError(t, err)
	return orderID
}

// seedExemptions 写入豁免清单。
func (h *verifyHarness) seedExemptions(t *testing.T, domains ...string) {
	t.Helper()
	for _, d := range domains {
		require.NoError(t, h.exempts.Upsert(context.Background(), &domain.Exemption{Domain: d, Reason: "fixture"}))
	}
}

// seedProbeRecord 写入一条探测历史（显式 probeAt 便于构造连续序列）。
func (h *verifyHarness) seedProbeRecord(t *testing.T, at time.Time, domainName, onlineFP string, status domain.ProbeStatus) {
	t.Helper()
	require.NoError(t, h.probes.Create(context.Background(), &domain.ProbeResult{
		Domain: domainName, OnlineFingerprint: onlineFP, Status: status, ProbeAt: at,
	}))
}

// ---------------------------------------------------------------------
// AC-1：固化 verifyExpected
// ---------------------------------------------------------------------

// TestVerifyWindow_SealBuildsExpected 固化快照构建（AC-1）：domains=旧证书 SAN
// 保序去重后剔除豁免命中项（记入 excludedDomains），newCertFingerprint=新证书
// 指纹，windowUntil=now+verifyWindowHours，verifyWindowUntil 同原子对齐；
// 非验证中单 CAS 未命中幂等返回 false。
func TestVerifyWindow_SealBuildsExpected(t *testing.T) {
	ctx := context.Background()
	h := newVerifyHarness(t, nil)
	h.seedOldCert(t, vwOldFP, []string{"d1.example.com", "exempt.example.com", "*.wild.example.com"})
	h.seedExemptions(t, "exempt.example.com")
	windowUntil := h.clock.Add(24 * time.Hour)

	orderID := h.seedVerifyingOrder(t, vwOldFP, vwNewFP, h.clock.Add(time.Hour), nil, nil)
	ok, err := h.vw.SealVerifyExpected(ctx, orderID)
	require.NoError(t, err)
	assert.True(t, ok)

	order, err := h.orders.GetByID(ctx, orderID)
	require.NoError(t, err)
	require.NotNil(t, order.VerifyExpected)
	assert.Equal(t, vwNewFP, order.VerifyExpected.NewCertFingerprint)
	assert.Equal(t, []string{"d1.example.com", "*.wild.example.com"}, order.VerifyExpected.Domains, "豁免域名剔除，次序保持")
	assert.Equal(t, []string{"exempt.example.com"}, order.VerifyExpected.ExcludedDomains)
	assert.True(t, order.VerifyExpected.WindowUntil.Equal(windowUntil), "windowUntil=now+verifyWindowHours(默认24h)")
	require.NotNil(t, order.VerifyWindowUntil)
	assert.True(t, order.VerifyWindowUntil.Equal(windowUntil), "verifyWindowUntil 与快照同原子对齐")

	// 非验证中单：CAS 未命中幂等（false, nil）
	execID := h.seedExecutingOrder(t, vwOldFP2, vwNewFP, nil)
	ok, err = h.vw.SealVerifyExpected(ctx, execID)
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestVerifyWindow_SealRefreshesPerBatch 分批单每批刷新（AC-1/AC-6）：续批进入
// verifying 后重新固化——windowUntil 按新批次时点刷新，豁免清单变化反映到
// excludedDomains（覆盖写非追加）。
func TestVerifyWindow_SealRefreshesPerBatch(t *testing.T) {
	ctx := context.Background()
	h := newVerifyHarness(t, nil)
	h.seedOldCert(t, vwOldFP, []string{"b1.example.com", "b2.example.com"})
	batch := &domain.BatchInfo{TotalBatches: 2, CurrentBatch: 1, BatchSize: 1}
	orderID := h.seedExecutingOrder(t, vwOldFP, vwNewFP, batch)

	// 批 1 进入验证窗口 → 固化 v1
	ok, err := h.orders.EnterVerify(ctx, orderID, h.clock.Add(24*time.Hour), h.clock)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = h.vw.SealVerifyExpected(ctx, orderID)
	require.NoError(t, err)
	require.True(t, ok)
	v1, err := h.orders.GetByID(ctx, orderID)
	require.NoError(t, err)
	require.NotNil(t, v1.VerifyExpected)
	assert.Equal(t, []string{"b1.example.com", "b2.example.com"}, v1.VerifyExpected.Domains)
	assert.Empty(t, v1.VerifyExpected.ExcludedDomains)

	// 续批放行 → 批间新增豁免 → 批 2 进入验证窗口再固化 v2
	advanced, err := h.orders.AdvanceBatch(ctx, orderID, domain.ChangeStatusVerifying, 2)
	require.NoError(t, err)
	require.True(t, advanced)
	h.seedExemptions(t, "b2.example.com")
	h.clock = h.clock.Add(time.Hour)
	ok, err = h.orders.EnterVerify(ctx, orderID, h.clock.Add(24*time.Hour), h.clock)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = h.vw.SealVerifyExpected(ctx, orderID)
	require.NoError(t, err)
	require.True(t, ok)

	v2, err := h.orders.GetByID(ctx, orderID)
	require.NoError(t, err)
	require.NotNil(t, v2.VerifyExpected)
	assert.True(t, v2.VerifyExpected.WindowUntil.After(v1.VerifyExpected.WindowUntil), "windowUntil 按批刷新")
	assert.Equal(t, []string{"b1.example.com"}, v2.VerifyExpected.Domains, "当前豁免清单命中项剔除（覆盖写）")
	assert.Equal(t, []string{"b2.example.com"}, v2.VerifyExpected.ExcludedDomains)
}

// ---------------------------------------------------------------------
// AC-5：豁免∩窗口不死锁（skipped 不阻塞达标）
// ---------------------------------------------------------------------

// TestVerifyWindow_ExemptAndWildcardDoNotBlock 豁免域名进 excludedDomains、
// 无 override 通配符计 skipped——全部验证项 skipped 时窗口仍可正常达标
// （Hard Rule：防"含豁免域名的窗口永不达标"死锁）。
func TestVerifyWindow_ExemptAndWildcardDoNotBlock(t *testing.T) {
	ctx := context.Background()
	h := newVerifyHarness(t, nil)
	h.seedOldCert(t, vwOldFP, []string{"exempt.example.com", "*.wild.example.com"})
	h.seedExemptions(t, "exempt.example.com")

	orderID := h.seedVerifyingOrder(t, vwOldFP, vwNewFP, h.clock.Add(time.Hour), nil, nil)
	ok, err := h.vw.SealVerifyExpected(ctx, orderID)
	require.NoError(t, err)
	require.True(t, ok)
	order, err := h.orders.GetByID(ctx, orderID)
	require.NoError(t, err)
	require.NotNil(t, order.VerifyExpected)
	assert.Equal(t, []string{"*.wild.example.com"}, order.VerifyExpected.Domains)
	assert.Equal(t, []string{"exempt.example.com"}, order.VerifyExpected.ExcludedDomains)

	// 提频探测轮：通配符无 override → wildcard_skipped（不拨测、不计差异、不告警）
	probed, err := h.vw.ProbeVerifyingWindows(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, probed)

	order, err = h.orders.GetByID(ctx, orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusCompleted, order.Status, "全部验证项 skipped：窗口正常达标关闭")
	assert.Empty(t, order.ActiveMutex, "终态清除互斥 token")
	require.NotNil(t, order.ProtectUntil, "completed 固化回滚保护期")
	assert.Empty(t, h.recorder.recorded(), "无未达标清单")
	assert.Empty(t, h.publisher.Events(), "skipped 项不产生告警")
	stored, err := h.probes.ListRecentByDomains(ctx, []string{"*.wild.example.com"}, 1)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, domain.ProbeStatusWildcardSkipped, stored[0].Status)
}

// ---------------------------------------------------------------------
// AC-2/AC-4：提频探测 + 连续达标 + 提前达标关闭 + change_linked 路由
// ---------------------------------------------------------------------

// TestVerifyWindow_ProbeRoundsEarlyClose 提频探测连续 verifyConfirmProbes（默认 2）
// 次一致 = 达标 → 终批窗口提前达标关闭（completed）；窗口内新转入 change_linked_diff
// 的域名经变更关联通道告警一次（附 orderId/预期指纹/达标计数），持续差异不重复告警。
func TestVerifyWindow_ProbeRoundsEarlyClose(t *testing.T) {
	ctx := context.Background()
	newCert := certtest.NewBundle(t, "new.example.com", []string{"d1.example.com"}, nil)
	srv := newSNIServer(t, map[string]*certtest.CertBundle{"d1.example.com": newCert})
	h := newVerifyHarness(t, srv)
	h.seedOldCert(t, vwOldFP, []string{"d1.example.com"}) // 台账仅旧证：线上新指纹 → diff → 变更关联

	windowUntil := h.clock.Add(2 * time.Hour)
	expected := &domain.VerifyExpected{
		NewCertFingerprint: newCert.Fingerprint,
		Domains:            []string{"d1.example.com"},
		WindowUntil:        windowUntil,
	}
	orderID := h.seedVerifyingOrder(t, vwOldFP, vwNewFP, windowUntil, expected, nil)

	// 第 1 轮：探测落库 change_linked_diff（连续一致 1/2），新转入域名告警一次
	probed, err := h.vw.ProbeVerifyingWindows(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, probed)
	order, err := h.orders.GetByID(ctx, orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusVerifying, order.Status, "连续一致不足阈值：窗口保持")
	events := h.publisher.Events()
	require.Len(t, events, 1, "新转入 change_linked_diff 告警一次")
	assert.Equal(t, AlertCategoryChangeLinked, events[0].Category)
	assert.Equal(t, orderID, events[0].OrderID)
	assert.Equal(t, "d1.example.com", events[0].Domain)
	require.NotNil(t, events[0].VerifyWindow)
	assert.True(t, events[0].VerifyWindow.Active, "窗口内走变更关联通道")
	assert.Equal(t, newCert.Fingerprint, events[0].VerifyWindow.ExpectedFingerprint)
	assert.Equal(t, 1, events[0].VerifyWindow.PassCount)

	// 第 2 轮：连续一致 2/2 = 达标 → 提前关闭（completed）
	probed, err = h.vw.ProbeVerifyingWindows(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, probed)
	order, err = h.orders.GetByID(ctx, orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusCompleted, order.Status, "终批窗口提前达标关闭")
	assert.Empty(t, order.ActiveMutex)
	require.NotNil(t, order.ProtectUntil)
	assert.Len(t, h.publisher.Events(), 1, "持续 change_linked_diff 不重复告警")
	stored, err := h.probes.ListRecentByDomains(ctx, []string{"d1.example.com"}, 10)
	require.NoError(t, err)
	require.Len(t, stored, 2, "两轮提频探测记录")
	for _, r := range stored {
		assert.Equal(t, domain.ProbeStatusChangeLinkedDiff, r.Status)
		assert.Equal(t, orderID, r.ChangeOrderID)
	}
	assert.Empty(t, h.recorder.recorded(), "达标关闭无未达标清单")
}

// TestVerifyWindow_EarlyCloseNonFinalBatchPauses 非终批批级达标 → 批间暂停
// （verifying→executing + paused=true/pausedAt，当前批保持，activeMutex 持有），
// 等 ConfirmBatch 人工续批（门控 2 即刻可过）。
func TestVerifyWindow_EarlyCloseNonFinalBatchPauses(t *testing.T) {
	ctx := context.Background()
	newCert := certtest.NewBundle(t, "new.example.com", []string{"d1.example.com"}, nil)
	srv := newSNIServer(t, map[string]*certtest.CertBundle{"d1.example.com": newCert})
	h := newVerifyHarness(t, srv)
	h.seedOldCert(t, vwOldFP, []string{"d1.example.com"})
	windowUntil := h.clock.Add(2 * time.Hour)
	expected := &domain.VerifyExpected{
		NewCertFingerprint: newCert.Fingerprint,
		Domains:            []string{"d1.example.com"},
		WindowUntil:        windowUntil,
	}
	batch := &domain.BatchInfo{TotalBatches: 2, CurrentBatch: 1, BatchSize: 1}
	orderID := h.seedVerifyingOrder(t, vwOldFP, vwNewFP, windowUntil, expected, batch)

	for i := 0; i < 2; i++ {
		_, err := h.vw.ProbeVerifyingWindows(ctx)
		require.NoError(t, err)
	}
	order, err := h.orders.GetByID(ctx, orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusExecuting, order.Status, "批级达标 → 批间暂停态")
	require.NotNil(t, order.BatchInfo)
	assert.True(t, order.BatchInfo.Paused)
	require.NotNil(t, order.BatchInfo.PausedAt)
	assert.Equal(t, 1, order.BatchInfo.CurrentBatch, "当前批保持（不自动续批）")
	assert.Equal(t, vwOldFP, order.ActiveMutex, "批间循环互斥全程持有")

	// 门控 2 判定（5.7 ConfirmBatch 消费）：暂停态订单批级达标可续批
	verified, reason, err := h.vw.BatchVerified(ctx, order)
	require.NoError(t, err)
	assert.True(t, verified)
	assert.Empty(t, reason)
}

// TestVerifyWindow_LazySealWhenUnsealed verifyExpected 缺失（固化缝未接线）时
// 提频探测入口惰性补固化（仅缺失补写，不覆盖既有快照）。
func TestVerifyWindow_LazySealWhenUnsealed(t *testing.T) {
	ctx := context.Background()
	h := newVerifyHarness(t, nil)
	h.seedOldCert(t, vwOldFP, []string{"d1.example.com"})
	orderID := h.seedVerifyingOrder(t, vwOldFP, vwNewFP, h.clock.Add(time.Hour), nil, nil)

	probed, err := h.vw.ProbeVerifyingWindows(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, probed)
	order, err := h.orders.GetByID(ctx, orderID)
	require.NoError(t, err)
	require.NotNil(t, order.VerifyExpected, "惰性补固化")
	assert.Equal(t, []string{"d1.example.com"}, order.VerifyExpected.Domains)
	assert.Equal(t, vwNewFP, order.VerifyExpected.NewCertFingerprint)
	assert.Equal(t, domain.ChangeStatusVerifying, order.Status, "无探测记录：未达标，窗口保持")
}

// ---------------------------------------------------------------------
// AC-3：window-expiry 终局判定
// ---------------------------------------------------------------------

// TestVerifyWindow_FinalizeExpired_MetToCompleted 窗口到期全部达标 → completed，
// 不产生 UnmetDomains 与恢复告警；未到期单不进入扫描集（Hard Rule：scheduler
// 主动扫描，不依赖被动探测触发）。
func TestVerifyWindow_FinalizeExpired_MetToCompleted(t *testing.T) {
	ctx := context.Background()
	h := newVerifyHarness(t, nil)
	h.seedOldCert(t, vwOldFP, []string{"d1.example.com"})

	expired := h.clock.Add(-time.Minute)
	expected := &domain.VerifyExpected{
		NewCertFingerprint: vwNewFP,
		Domains:            []string{"d1.example.com"},
		WindowUntil:        expired,
	}
	orderID := h.seedVerifyingOrder(t, vwOldFP, vwNewFP, expired, expected, nil)
	h.seedProbeRecord(t, h.clock.Add(-2*time.Minute), "d1.example.com", vwNewFP, domain.ProbeStatusChangeLinkedDiff)
	h.seedProbeRecord(t, h.clock.Add(-1*time.Minute), "d1.example.com", vwNewFP, domain.ProbeStatusChangeLinkedDiff)

	// 未到期单：不在终局扫描集
	liveID := h.seedVerifyingOrder(t, vwOldFP2, vwNewFP, h.clock.Add(time.Hour), nil, nil)
	h.seedOldCert(t, vwOldFP2, []string{"live.example.com"})

	finalized, err := h.vw.FinalizeExpiredWindows(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{orderID}, finalized)

	order, err := h.orders.GetByID(ctx, orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusCompleted, order.Status)
	assert.Empty(t, order.ActiveMutex)
	require.NotNil(t, order.ProtectUntil)
	live, err := h.orders.GetByID(ctx, liveID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusVerifying, live.Status, "窗口未到期不被终局判定")
	assert.Empty(t, h.recorder.recorded())
	assert.Empty(t, h.publisher.Events())
}

// TestVerifyWindow_FinalizeExpired_UnmetPartialCompleted 窗口到期存在未达标 →
// partial_completed + 未达标域名写入 ChangeReport.UnmetDomains + 未达标域名恢复
// 常规 diff 告警（change_linked 事件 Active=false，4.3 路由恢复常规通道）；
// 窗口关闭后该域名探测恢复常规 diff 判定（不再关联订单）。
func TestVerifyWindow_FinalizeExpired_UnmetPartialCompleted(t *testing.T) {
	ctx := context.Background()
	newCert := certtest.NewBundle(t, "new.example.com", []string{"d1.example.com"}, nil)
	srv := newSNIServer(t, map[string]*certtest.CertBundle{"d1.example.com": newCert})
	h := newVerifyHarness(t, srv)
	h.seedOldCert(t, vwOldFP, []string{"d1.example.com", "d2.example.com"})

	expired := h.clock.Add(-time.Minute)
	expected := &domain.VerifyExpected{
		NewCertFingerprint: newCert.Fingerprint,
		Domains:            []string{"d1.example.com", "d2.example.com"},
		WindowUntil:        expired,
	}
	orderID := h.seedVerifyingOrder(t, vwOldFP, vwNewFP, expired, expected, nil)
	// d1 连续两次一致（达标）；d2 仅一次且为旧指纹（未达标）
	h.seedProbeRecord(t, h.clock.Add(-3*time.Minute), "d1.example.com", newCert.Fingerprint, domain.ProbeStatusChangeLinkedDiff)
	h.seedProbeRecord(t, h.clock.Add(-2*time.Minute), "d1.example.com", newCert.Fingerprint, domain.ProbeStatusChangeLinkedDiff)
	h.seedProbeRecord(t, h.clock.Add(-2*time.Minute), "d2.example.com", vwOldFP, domain.ProbeStatusDiff)

	finalized, err := h.vw.FinalizeExpiredWindows(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{orderID}, finalized)

	order, err := h.orders.GetByID(ctx, orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusPartialCompleted, order.Status)
	assert.Empty(t, order.ActiveMutex)
	require.NotNil(t, order.ProtectUntil)
	assert.Equal(t, []string{orderID + "|d2.example.com"}, h.recorder.recorded(), "未达标域名写入 UnmetDomains（Verify.Unmet 计数口径）")

	events := h.publisher.Events()
	require.Len(t, events, 1, "仅未达标域名恢复常规告警")
	assert.Equal(t, AlertCategoryChangeLinked, events[0].Category)
	assert.Equal(t, "d2.example.com", events[0].Domain)
	assert.Equal(t, orderID, events[0].OrderID)
	require.NotNil(t, events[0].VerifyWindow)
	assert.False(t, events[0].VerifyWindow.Active, "窗口关闭：4.3 路由恢复常规通道")
	assert.Equal(t, newCert.Fingerprint, events[0].VerifyWindow.ExpectedFingerprint)

	// 窗口关闭后恢复常规 diff 判定：d1 线上=新指纹但订单已终态 → 常规 diff，不关联
	results, err := h.prober.ProbeDomains(ctx, []string{"d1.example.com"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, domain.ProbeStatusDiff, results[0].Status, "窗口关闭后恢复常规 diff 判定")
	assert.Empty(t, results[0].ChangeOrderID)
}

// TestVerifyWindow_FinalizeExpired_NonFinalBatch 批级窗口到期：达标/未达标均回
// 批间暂停（转人工决策——回滚/Cancel，不自动续批）；未达标域名恢复常规跟踪，
// 且续批门控（BatchVerified）保持拒绝。
func TestVerifyWindow_FinalizeExpired_NonFinalBatch(t *testing.T) {
	ctx := context.Background()
	h := newVerifyHarness(t, nil)
	h.seedOldCert(t, vwOldFP, []string{"m1.example.com"})
	h.seedOldCert(t, vwOldFP2, []string{"u1.example.com"})
	expired := h.clock.Add(-time.Minute)
	batch := &domain.BatchInfo{TotalBatches: 2, CurrentBatch: 1, BatchSize: 1}

	// 达标的批级窗口 → 批间暂停等续批
	metExpected := &domain.VerifyExpected{NewCertFingerprint: vwNewFP, Domains: []string{"m1.example.com"}, WindowUntil: expired}
	metID := h.seedVerifyingOrder(t, vwOldFP, vwNewFP, expired, metExpected, batch)
	h.seedProbeRecord(t, h.clock.Add(-3*time.Minute), "m1.example.com", vwNewFP, domain.ProbeStatusChangeLinkedDiff)
	h.seedProbeRecord(t, h.clock.Add(-2*time.Minute), "m1.example.com", vwNewFP, domain.ProbeStatusChangeLinkedDiff)

	// 未达标的批级窗口 → 批间暂停转人工决策
	unmetExpected := &domain.VerifyExpected{NewCertFingerprint: vwNewFP, Domains: []string{"u1.example.com"}, WindowUntil: expired}
	unmetID := h.seedVerifyingOrder(t, vwOldFP2, vwNewFP, expired, unmetExpected, batch)
	h.seedProbeRecord(t, h.clock.Add(-2*time.Minute), "u1.example.com", vwOldFP2, domain.ProbeStatusDiff)

	finalized, err := h.vw.FinalizeExpiredWindows(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{metID, unmetID}, finalized)

	for _, tc := range []struct {
		id     string
		paused bool
	}{
		{metID, true},
		{unmetID, true},
	} {
		order, err := h.orders.GetByID(ctx, tc.id)
		require.NoError(t, err)
		assert.Equal(t, domain.ChangeStatusExecuting, order.Status, "批级窗口到期回批间暂停")
		require.NotNil(t, order.BatchInfo)
		assert.True(t, order.BatchInfo.Paused)
		assert.Equal(t, 1, order.BatchInfo.CurrentBatch)
		assert.NotEmpty(t, order.ActiveMutex, "人工决策期互斥保持")
	}

	// 未达标单：恢复常规告警 + 续批门控拒绝（附域名与阈值）
	events := h.publisher.Events()
	require.Len(t, events, 1)
	assert.Equal(t, "u1.example.com", events[0].Domain)
	require.NotNil(t, events[0].VerifyWindow)
	assert.False(t, events[0].VerifyWindow.Active)
	unmetOrder, err := h.orders.GetByID(ctx, unmetID)
	require.NoError(t, err)
	verified, reason, err := h.vw.BatchVerified(ctx, unmetOrder)
	require.NoError(t, err)
	assert.False(t, verified)
	assert.Contains(t, reason, "u1.example.com")
	assert.Contains(t, reason, "2")
}

// ---------------------------------------------------------------------
// 5.7 门控 2：BatchVerified 判定矩阵
// ---------------------------------------------------------------------

// TestVerifyWindow_BatchVerified 批级验证达标判定：无探测记录/不足阈值=未达标；
// verifyExpected 未固化=未达标；无 override 通配符计 skipped 不阻塞（Hard Rule）；
// 判定通道故障返回 error（调用方安全侧不放行）。
func TestVerifyWindow_BatchVerified(t *testing.T) {
	ctx := context.Background()
	h := newVerifyHarness(t, nil)
	h.seedOldCert(t, vwOldFP, []string{"d1.example.com", "*.wild.example.com"})

	t.Run("无探测记录未达标", func(t *testing.T) {
		expected := &domain.VerifyExpected{NewCertFingerprint: vwNewFP, Domains: []string{"d1.example.com"}, WindowUntil: h.clock.Add(time.Hour)}
		orderID := h.seedVerifyingOrder(t, vwOldFP, vwNewFP, h.clock.Add(time.Hour), expected, nil)
		order, err := h.orders.GetByID(ctx, orderID)
		require.NoError(t, err)
		verified, reason, err := h.vw.BatchVerified(ctx, order)
		require.NoError(t, err)
		assert.False(t, verified)
		assert.Contains(t, reason, "d1.example.com")
	})

	t.Run("verifyExpected 未固化未达标", func(t *testing.T) {
		orderID := h.seedVerifyingOrder(t, vwOldFP2, vwNewFP, h.clock.Add(time.Hour), nil, nil)
		order, err := h.orders.GetByID(ctx, orderID)
		require.NoError(t, err)
		verified, reason, err := h.vw.BatchVerified(ctx, order)
		require.NoError(t, err)
		assert.False(t, verified)
		assert.Contains(t, reason, "未固化")
	})

	t.Run("无 override 通配符计 skipped 不阻塞", func(t *testing.T) {
		expected := &domain.VerifyExpected{NewCertFingerprint: vwNewFP, Domains: []string{"*.wild.example.com"}, WindowUntil: h.clock.Add(time.Hour)}
		orderID := h.seedVerifyingOrder(t, vwOldFP3, vwNewFP, h.clock.Add(time.Hour), expected, nil)
		order, err := h.orders.GetByID(ctx, orderID)
		require.NoError(t, err)
		verified, reason, err := h.vw.BatchVerified(ctx, order)
		require.NoError(t, err)
		assert.True(t, verified, "通配符验证项 skipped：不阻塞达标")
		assert.Empty(t, reason)
	})

	t.Run("判定通道故障返回 error", func(t *testing.T) {
		probes := &failingListRecentProbeRepo{FakeProbeResultRepo: certtest.NewFakeProbeResultRepo(), err: errors.New("probe store down")}
		changes := NewChangeService(h.orders, certtest.NewFakeChangeItemRepo(), h.certs, h.alertCfg,
			certtest.NewFakeScanSnapshotRepo(), certtest.NewFakeCertReferenceRepo(), nil)
		vw := NewVerifyWindowService(h.orders, h.certs, h.exempts, h.alertCfg, probes, h.prober, changes, nil, nil).(*verifyWindowService)
		expected := &domain.VerifyExpected{NewCertFingerprint: vwNewFP, Domains: []string{"d1.example.com"}, WindowUntil: h.clock.Add(time.Hour)}
		orderID := h.seedVerifyingOrder(t, vwOldFP4, vwNewFP, h.clock.Add(time.Hour), expected, nil)
		order, err := h.orders.GetByID(ctx, orderID)
		require.NoError(t, err)
		verified, _, err := vw.BatchVerified(ctx, order)
		require.Error(t, err)
		assert.False(t, verified)
	})
}

// ---------------------------------------------------------------------
// AC-6 集成：5.7 执行引擎缝（EnterVerify 固化 + ConfirmBatch 门控 + 分批刷新）
// ---------------------------------------------------------------------

// TestVerifyWindow_ExecuteEngineIntegration 真实执行引擎驱动分批单：批 1 执行完成
// 进入 verifying 时经固化缝写入 verifyExpected；人工续批门控（门控 2=BatchVerified）
// 放行（判定基于固化快照——固化后不随台账变化）；批 2 执行完成按该批时点刷新快照。
func TestVerifyWindow_ExecuteEngineIntegration(t *testing.T) {
	ctx := context.Background()
	h := newExecuteHarness(t)
	exempts := certtest.NewFakeExemptionRepo()
	probes := certtest.NewFakeProbeResultRepo()
	// 台账补旧证（固化目标域名基准；seedNewCert 已写新证）
	require.NoError(t, h.certs.Create(ctx, &domain.Certificate{
		ID: primitive.NewObjectID(), Fingerprint: execTestOldFP,
		CommonName: "old.example.com", Sans: []string{"b1.example.com", "b2.example.com"},
		HostingStatus: domain.HostingStatusComplete,
	}))
	changes := NewChangeService(h.orders, h.items, h.certs, h.alertCfg, h.snaps, h.refs, nil)
	vw := NewVerifyWindowService(h.orders, h.certs, exempts, h.alertCfg, probes, h.proberForTest(t), changes, nil, NewInMemoryAlertPublisher()).(*verifyWindowService)
	clock := h.now()
	vw.now = func() time.Time { return clock }
	h.svc.now = vw.now
	h.svc.sealer = vw // 5.7 固化缝：EnterVerify 后立即固化
	h.svc.verify = vw // 5.7 门控 2：批级验证达标判定

	snapID := h.seedDoneSnapshot(t, time.Hour)
	orderID := h.seedPendingOrder(t, snapID,
		domain.ItemStatusPending, domain.ItemStatusPending,
		domain.ItemStatusPending, domain.ItemStatusPending)
	require.NoError(t, h.svc.Confirm(ctx, orderID, deployerBatchConf(2, 0.5)))
	h.bindDispatch()

	// 批 1 执行完成 → verifying + 固化 verifyExpected（该批窗口判定依据）
	require.NoError(t, h.svc.Execute(ctx, orderID))
	order, err := h.orders.GetByID(ctx, orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusVerifying, order.Status)
	require.NotNil(t, order.VerifyExpected, "批执行完成经固化缝写入 verifyExpected")
	assert.Equal(t, execTestNewFP, order.VerifyExpected.NewCertFingerprint)
	assert.Equal(t, []string{"b1.example.com", "b2.example.com"}, order.VerifyExpected.Domains)
	firstWindow := order.VerifyExpected.WindowUntil

	// 台账豁免在固化后新增——不改变既有快照（固化后不随台账变化）
	require.NoError(t, exempts.Upsert(ctx, &domain.Exemption{Domain: "b2.example.com", Reason: "fixture"}))

	// 批级验证达标：固化快照 domains（b1/b2）各连续 2 次一致
	for _, d := range []string{"b1.example.com", "b2.example.com"} {
		require.NoError(t, probes.Create(ctx, &domain.ProbeResult{
			Domain: d, OnlineFingerprint: execTestNewFP,
			Status: domain.ProbeStatusChangeLinkedDiff, ProbeAt: clock.Add(-2 * time.Minute),
		}))
		require.NoError(t, probes.Create(ctx, &domain.ProbeResult{
			Domain: d, OnlineFingerprint: execTestNewFP,
			Status: domain.ProbeStatusChangeLinkedDiff, ProbeAt: clock.Add(-1 * time.Minute),
		}))
	}
	require.NoError(t, h.svc.ConfirmBatch(ctx, orderID), "上一批全 success + 批级验证达标 → 放行续批")
	order, err = h.orders.GetByID(ctx, orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusExecuting, order.Status)
	assert.Equal(t, 2, order.BatchInfo.CurrentBatch)

	// 批 2（终批）执行完成 → 按该批时点刷新快照（豁免变化反映到 excludedDomains）
	clock = clock.Add(time.Hour)
	require.NoError(t, h.svc.Execute(ctx, orderID))
	order, err = h.orders.GetByID(ctx, orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusVerifying, order.Status)
	require.NotNil(t, order.VerifyExpected)
	assert.True(t, order.VerifyExpected.WindowUntil.After(firstWindow), "分批单每批按该批时点刷新 windowUntil")
	assert.Equal(t, []string{"b1.example.com"}, order.VerifyExpected.Domains, "批 2 固化剔除当前豁免命中项")
	assert.Equal(t, []string{"b2.example.com"}, order.VerifyExpected.ExcludedDomains)
}

// proberForTest 为集成用例构造真实探测服务（SNI 拨测不被触达——判定数据由
// 显式种子探测历史提供）。
func (h *executeHarness) proberForTest(t *testing.T) ProbeService {
	t.Helper()
	return NewProbeService(h.certs, certtest.NewFakeProbeResultRepo(), certtest.NewFakeExemptionRepo(),
		h.alertCfg, h.orders, &stdTLSDialer{timeout: time.Second}, ProbeOptions{})
}

// deployerBatchConf 测试便捷构造（BatchConf 字面量较长）。
func deployerBatchConf(size int, ratio float64) deployer.BatchConf {
	return deployer.BatchConf{Enabled: true, BatchSize: size, MaxBatchRatio: ratio}
}

// ---------------------------------------------------------------------
// 错误路径隔离（单笔故障不中断扫描，状态先落地）
// ---------------------------------------------------------------------

// failingProber 提频探测故障注入（ProbeService 端口）。
type failingProber struct{ err error }

func (f *failingProber) ProbeDomains(context.Context, []string) ([]domain.ProbeResult, error) {
	return nil, f.err
}

func (f *failingProber) ProbeLedgerDomains(context.Context) ([]domain.ProbeResult, error) {
	return nil, f.err
}

func (f *failingProber) ProbeAllTenantDNS(context.Context) ([]domain.ProbeResult, error) {
	return nil, f.err
}

func (f *failingProber) ProbeTenantDNS(context.Context, int64) ([]domain.ProbeResult, error) {
	return nil, f.err
}

func (f *failingProber) TriggerProbeAsync(context.Context) error {
	return f.err
}

func (f *failingProber) TriggerProbeRootAsync(context.Context, string) error {
	return f.err
}

// failingTransitions 终态迁移故障注入（VerifyWindowTransitions 端口）。
type failingTransitions struct{ err error }

func (f *failingTransitions) Transition(context.Context, string, domain.ChangeStatus) error {
	return f.err
}

// TestVerifyWindow_ErrorPathsAreIsolated 基础设施故障隔离：探测/判定/迁移/记录
// 失败以错误上抛（首批），单笔不中断扫描、已落地状态不回滚；未达标原因清单
// 超长截断。
func TestVerifyWindow_ErrorPathsAreIsolated(t *testing.T) {
	ctx := context.Background()

	changes := func() VerifyWindowTransitions {
		return NewChangeService(certtest.NewFakeChangeOrderRepo(), certtest.NewFakeChangeItemRepo(),
			certtest.NewFakeCertificateRepo(), certtest.NewFakeAlertConfigRepo(),
			certtest.NewFakeScanSnapshotRepo(), certtest.NewFakeCertReferenceRepo(), nil)
	}

	t.Run("探测故障上抛但订单仍参与判定", func(t *testing.T) {
		h := newVerifyHarness(t, nil)
		h.seedOldCert(t, vwOldFP, []string{"d1.example.com"})
		vw := NewVerifyWindowService(h.orders, h.certs, h.exempts, h.alertCfg, h.probes,
			&failingProber{err: errors.New("dial pool exhausted")}, changes(), nil, nil).(*verifyWindowService)
		vw.now = func() time.Time { return h.clock }
		orderID := h.seedVerifyingOrder(t, vwOldFP, vwNewFP, h.clock.Add(time.Hour),
			&domain.VerifyExpected{NewCertFingerprint: vwNewFP, Domains: []string{"d1.example.com"}, WindowUntil: h.clock.Add(time.Hour)}, nil)
		probed, err := vw.ProbeVerifyingWindows(ctx)
		require.Error(t, err)
		assert.Equal(t, 1, probed, "探测失败仍计为参与轮（既有数据继续判定）")
		order, err := h.orders.GetByID(ctx, orderID)
		require.NoError(t, err)
		assert.Equal(t, domain.ChangeStatusVerifying, order.Status)
	})

	t.Run("固化缝故障上抛（新证书不可达）", func(t *testing.T) {
		h := newVerifyHarness(t, nil)
		vw := NewVerifyWindowService(h.orders, h.certs, h.exempts, h.alertCfg, h.probes,
			h.prober, changes(), nil, nil).(*verifyWindowService)
		vw.now = func() time.Time { return h.clock }
		future := h.clock.Add(time.Hour)
		order := &domain.ChangeOrder{
			OldCertFingerprint: vwOldFP, NewCertID: "missing-cert",
			Status: domain.ChangeStatusVerifying, VerifyWindowUntil: &future,
			ActiveMutex: vwOldFP, Creator: "operator",
		}
		orderID, err := h.orders.Create(ctx, order)
		require.NoError(t, err)
		probed, err := vw.ProbeVerifyingWindows(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing-cert")
		assert.Zero(t, probed)
		// 窗口推到过期：终局判定补固化同样失败上抛
		past := h.clock.Add(-time.Minute)
		ok, err := h.orders.SetVerifyExpected(ctx, orderID, &domain.VerifyExpected{
			NewCertFingerprint: vwNewFP, Domains: []string{"d1.example.com"}, WindowUntil: past,
		})
		require.NoError(t, err)
		require.True(t, ok)
		_, err = vw.FinalizeExpiredWindows(ctx)
		require.Error(t, err, "终局判定补固化同样失败上抛")
	})

	t.Run("判定通道故障上抛且不误收敛", func(t *testing.T) {
		h := newVerifyHarness(t, nil)
		h.seedOldCert(t, vwOldFP, []string{"d1.example.com"})
		probes := &failingListRecentProbeRepo{FakeProbeResultRepo: certtest.NewFakeProbeResultRepo(), err: errors.New("probe store down")}
		vw := NewVerifyWindowService(h.orders, h.certs, h.exempts, h.alertCfg, probes,
			h.prober, changes(), nil, nil).(*verifyWindowService)
		vw.now = func() time.Time { return h.clock }
		expired := h.clock.Add(-time.Minute)
		orderID := h.seedVerifyingOrder(t, vwOldFP, vwNewFP, expired,
			&domain.VerifyExpected{NewCertFingerprint: vwNewFP, Domains: []string{"d1.example.com"}, WindowUntil: expired}, nil)
		finalized, err := vw.FinalizeExpiredWindows(ctx)
		require.Error(t, err)
		assert.Empty(t, finalized)
		order, err := h.orders.GetByID(ctx, orderID)
		require.NoError(t, err)
		assert.Equal(t, domain.ChangeStatusVerifying, order.Status, "判定失败不误收敛终态")
	})

	t.Run("终态迁移故障上抛", func(t *testing.T) {
		h := newVerifyHarness(t, nil)
		h.seedOldCert(t, vwOldFP, []string{"d1.example.com"})
		vw := NewVerifyWindowService(h.orders, h.certs, h.exempts, h.alertCfg, h.probes,
			h.prober, &failingTransitions{err: errors.New("mongo write concern timeout")}, nil, nil).(*verifyWindowService)
		vw.now = func() time.Time { return h.clock }
		expired := h.clock.Add(-time.Minute)
		orderID := h.seedVerifyingOrder(t, vwOldFP, vwNewFP, expired,
			&domain.VerifyExpected{NewCertFingerprint: vwNewFP, Domains: []string{"d1.example.com"}, WindowUntil: expired}, nil)
		_ = orderID
		h.seedProbeRecord(t, h.clock.Add(-2*time.Minute), "d1.example.com", vwNewFP, domain.ProbeStatusChangeLinkedDiff)
		h.seedProbeRecord(t, h.clock.Add(-1*time.Minute), "d1.example.com", vwNewFP, domain.ProbeStatusChangeLinkedDiff)
		finalized, err := vw.FinalizeExpiredWindows(ctx)
		require.Error(t, err)
		assert.Empty(t, finalized, "迁移失败不计入收敛清单（下轮重扫）")
	})

	t.Run("UnmetDomains 记录失败：终态已落地不回滚", func(t *testing.T) {
		h := newVerifyHarness(t, nil)
		h.recorder.err = errors.New("audit store down")
		h.seedOldCert(t, vwOldFP, []string{"d1.example.com"})
		expired := h.clock.Add(-time.Minute)
		orderID := h.seedVerifyingOrder(t, vwOldFP, vwNewFP, expired,
			&domain.VerifyExpected{NewCertFingerprint: vwNewFP, Domains: []string{"d1.example.com"}, WindowUntil: expired}, nil)
		_, err := h.vw.FinalizeExpiredWindows(ctx)
		require.Error(t, err)
		order, err := h.orders.GetByID(ctx, orderID)
		require.NoError(t, err)
		assert.Equal(t, domain.ChangeStatusPartialCompleted, order.Status, "终态先落地，记录失败不回滚")
	})

	t.Run("未达标域名清单超长截断", func(t *testing.T) {
		h := newVerifyHarness(t, nil)
		domains := make([]string, 6)
		for i := range domains {
			domains[i] = fmt.Sprintf("u%d.example.com", i)
		}
		h.seedOldCert(t, vwOldFP, domains)
		orderID := h.seedVerifyingOrder(t, vwOldFP, vwNewFP, h.clock.Add(time.Hour),
			&domain.VerifyExpected{NewCertFingerprint: vwNewFP, Domains: domains, WindowUntil: h.clock.Add(time.Hour)}, nil)
		order, err := h.orders.GetByID(ctx, orderID)
		require.NoError(t, err)
		verified, reason, err := h.vw.BatchVerified(ctx, order)
		require.NoError(t, err)
		assert.False(t, verified)
		assert.Contains(t, reason, "等", "超过 5 个域名截断列举")
		assert.NotContains(t, reason, "u5.example.com")
	})
}
