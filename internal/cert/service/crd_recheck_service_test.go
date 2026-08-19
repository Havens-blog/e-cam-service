package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------
// crd-recheck 消费者测试（任务 5.9 AC：延迟到期消费、通过/回写/读取失败、
// 复检次数固定 1（Hard Rule）、幂等、扫描集限定）
// ---------------------------------------------------------------------

const recheckTestNewFP = "abababababababababababababababababababababababababababababababab"

// recheckOutcome 单项复检脚本：pass=true 通过；否则按 err/reason 失败。
type recheckOutcome struct {
	pass   bool
	reason string
	err    error
}

// recheckFakeRechecker 复检端口假实现：记录 CRDRecheckItem 载荷；按 certID
// 脚本化结果；resolveErr 命中时期望值解析失败。
type recheckFakeRechecker struct {
	items      []deployer.CRDRecheckItem
	script     map[string]recheckOutcome
	resolveErr error
	resolved   map[string]string // fingerprint → cloudCertID（ResolveCloudCertID 应答）
}

func newRecheckFakeRechecker() *recheckFakeRechecker {
	return &recheckFakeRechecker{
		script:   map[string]recheckOutcome{},
		resolved: map[string]string{},
	}
}

func (f *recheckFakeRechecker) RecheckCRDField(_ context.Context, item deployer.CRDRecheckItem) (deployer.RecheckResult, error) {
	f.items = append(f.items, item)
	o, ok := f.script[item.NewCertID]
	if !ok {
		return deployer.RecheckResult{RecheckPassed: true, CurrentCertID: item.NewCertID}, nil
	}
	if o.err != nil {
		return deployer.RecheckResult{}, o.err
	}
	if o.pass {
		return deployer.RecheckResult{RecheckPassed: true, CurrentCertID: item.NewCertID}, nil
	}
	return deployer.RecheckResult{RecheckPassed: false, CurrentCertID: "reverted", Reason: o.reason}, nil
}

func (f *recheckFakeRechecker) ResolveCloudCertID(_ context.Context, fingerprint string) (string, error) {
	if f.resolveErr != nil {
		return "", f.resolveErr
	}
	id, ok := f.resolved[fingerprint]
	if !ok {
		return "", fmt.Errorf("no active cloud cert id mapping for %s", fingerprint)
	}
	return id, nil
}

// recheckHarness crd-recheck 测试依赖聚合。
type recheckHarness struct {
	svc       CrdRecheckService
	orders    *certtest.FakeChangeOrderRepo
	items     *certtest.FakeChangeItemRepo
	certs     *certtest.FakeCertificateRepo
	mappings  *certtest.FakeCloudCertMappingRepo
	alertCfg  *certtest.FakeAlertConfigRepo
	rechecker *recheckFakeRechecker
	publisher *InMemoryAlertPublisher
	now       time.Time
	orderID   string
}

func newRecheckHarness(t *testing.T) *recheckHarness {
	t.Helper()
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	h := &recheckHarness{
		orders:    certtest.NewFakeChangeOrderRepo(),
		items:     certtest.NewFakeChangeItemRepo(),
		certs:     certtest.NewFakeCertificateRepo(),
		mappings:  certtest.NewFakeCloudCertMappingRepo(),
		alertCfg:  certtest.NewFakeAlertConfigRepo(),
		rechecker: newRecheckFakeRechecker(),
		publisher: NewInMemoryAlertPublisher(),
		now:       now,
	}
	svc := NewCrdRecheckService(h.orders, h.items, h.certs, h.alertCfg, h.rechecker, h.publisher)
	impl := svc.(*crdRecheckService)
	impl.now = func() time.Time { return now }
	h.svc = impl

	// 订单 + 新证书台账（fp → 唯一 active 云证书 ID 映射）。
	newCert := &domain.Certificate{Fingerprint: recheckTestNewFP, HostingStatus: domain.HostingStatusComplete}
	require.NoError(t, h.certs.Create(context.Background(), newCert))
	orderID, err := h.orders.Create(context.Background(), &domain.ChangeOrder{
		OldCertFingerprint: "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd",
		NewCertID:          newCert.ID.Hex(),
		Status:             domain.ChangeStatusVerifying,
		ActiveMutex:        "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd",
	})
	require.NoError(t, err)
	h.orderID = orderID
	require.NoError(t, h.mappings.Upsert(context.Background(), &domain.CloudCertMapping{
		CertFingerprint: recheckTestNewFP,
		Cloud:           "aliyun",
		AccountKey:      "acc1",
		CloudCertID:     "cert-k8s-1",
		Status:          domain.MappingStatusActive,
	}))
	h.rechecker.resolved[recheckTestNewFP] = "cert-k8s-1"
	return h
}

// seedPatchItem patch_crd 成功项种子（executedAt 偏移控制到期判定）。
func (h *recheckHarness) seedPatchItem(t *testing.T, executedAgo time.Duration) string {
	t.Helper()
	executed := h.now.Add(-executedAgo)
	_, err := h.items.CreateMulti(context.Background(), []domain.ChangeItem{{
		OrderID: h.orderID,
		Action:  domain.ActionPatchCRD,
		ResourceRef: domain.ResourceRef{
			Channel: domain.ChannelK8sAPI, ClusterID: "cluster-1",
			Namespace: "default", Kind: "Ingress", ResourceID: "gw-1",
		},
		OldCloudCertID: "cert-old-k8s",
		Status:         domain.ItemStatusSuccess,
		ExecutedAt:     &executed,
	}})
	require.NoError(t, err)
	all, err := h.items.ListByOrder(context.Background(), h.orderID)
	require.NoError(t, err)
	require.Len(t, all, 1)
	return all[0].ID.Hex()
}

// ---- AC-3：到期消费 + 通过路径 ----

func TestCrdRecheckDueItemPasses(t *testing.T) {
	h := newRecheckHarness(t)
	itemID := h.seedPatchItem(t, 10*time.Minute) // 默认 delay=5min 已到期

	consumed, err := h.svc.RunDueRechecks(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, consumed)

	// 消费调 5.6 RecheckCRDField 单轮复检（CRDRecheckItem 载荷完整）。
	require.Len(t, h.rechecker.items, 1)
	got := h.rechecker.items[0]
	assert.Equal(t, itemID, got.ItemID)
	assert.Equal(t, h.orderID, got.OrderID)
	assert.Equal(t, "cert-k8s-1", got.NewCertID) // 期望终态经通道同口径解析
	assert.Equal(t, "cert-old-k8s", got.OldCertID)
	assert.Equal(t, "gw-1", got.Ref.ResourceID)
	assert.Equal(t, "Ingress", got.Ref.Kind)

	// 通过 → 项保持 success + recheckedAt 固化；不告警。
	item, err := h.items.GetByID(context.Background(), itemID)
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusSuccess, item.Status)
	require.NotNil(t, item.RecheckedAt)
	assert.Equal(t, h.now, *item.RecheckedAt)
	assert.Empty(t, h.publisher.Events())
}

// AC-3：延迟未到期不消费。
func TestCrdRecheckDelayNotElapsedSkips(t *testing.T) {
	h := newRecheckHarness(t)
	h.seedPatchItem(t, 2*time.Minute) // delay=5min 未到期

	consumed, err := h.svc.RunDueRechecks(context.Background())
	require.NoError(t, err)
	assert.Zero(t, consumed)
	assert.Empty(t, h.rechecker.items)
}

// AC-3：回写失败 → 项 failed + 告警（TLS 差异通道语义）+ 复检次数固定 1。
func TestCrdRecheckRevertedFailsAndAlerts(t *testing.T) {
	h := newRecheckHarness(t)
	itemID := h.seedPatchItem(t, 10*time.Minute)
	h.rechecker.script["cert-k8s-1"] = recheckOutcome{
		pass:   false,
		reason: "reconcile 回写旧值：期望 cert-k8s-1 实际 cert-old-k8s（orderId=x itemId=y）",
	}

	consumed, err := h.svc.RunDueRechecks(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, consumed)

	item, err := h.items.GetByID(context.Background(), itemID)
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusFailed, item.Status)
	assert.Contains(t, item.Error, crdRecheckErrFailed)
	assert.Contains(t, item.Error, "reconcile 回写")
	require.NotNil(t, item.RecheckedAt)

	// 告警：TLS 差异通道语义，附 orderId。
	alerts := h.publisher.Events()
	require.Len(t, alerts, 1)
	assert.Equal(t, AlertCategoryTLSDiff, alerts[0].Category)
	assert.Equal(t, h.orderID, alerts[0].OrderID)

	// Hard Rule：复检次数固定 1——已复检项不再命中扫描（失败不自动二次复检）。
	consumed2, err := h.svc.RunDueRechecks(context.Background())
	require.NoError(t, err)
	assert.Zero(t, consumed2)
	assert.Len(t, h.rechecker.items, 1)
	assert.Len(t, h.publisher.Events(), 1) // AC-4：不重复告警
}

// AC-3：读取失败（集群不可达等）→ failed + 告警，单轮承接不自动重试。
func TestCrdRecheckReadErrorFailsOnce(t *testing.T) {
	h := newRecheckHarness(t)
	itemID := h.seedPatchItem(t, 10*time.Minute)
	h.rechecker.script["cert-k8s-1"] = recheckOutcome{
		err: fmt.Errorf("k8s channel: get cluster-1: %w", domain.ErrK8sUnreachable),
	}

	consumed, err := h.svc.RunDueRechecks(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, consumed)

	item, err := h.items.GetByID(context.Background(), itemID)
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusFailed, item.Status)
	assert.Contains(t, item.Error, domain.CodeK8sUnreachable)
	require.Len(t, h.publisher.Events(), 1)

	// 单轮：不再重试（Hard Rule：失败不自动二次复检，转人工）。
	consumed2, err := h.svc.RunDueRechecks(context.Background())
	require.NoError(t, err)
	assert.Zero(t, consumed2)
	assert.Len(t, h.rechecker.items, 1)
}

// 期望终态解析失败（新证书映射已清理/无法消歧）按复检失败承接（fail closed）。
func TestCrdRecheckResolveFailureFailsItem(t *testing.T) {
	h := newRecheckHarness(t)
	h.rechecker.resolveErr = errors.New("no active mapping")
	itemID := h.seedPatchItem(t, 10*time.Minute)

	consumed, err := h.svc.RunDueRechecks(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, consumed)

	item, err := h.items.GetByID(context.Background(), itemID)
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusFailed, item.Status)
	assert.Contains(t, item.Error, crdRecheckErrFailed)
	assert.Contains(t, item.Error, "no active mapping")
	require.Len(t, h.publisher.Events(), 1)
}

// 扫描集限定：仅 patch_crd + success + 未复检项（upload_and_bind 成功项、
// failed 项、已复检项均不命中）。
func TestCrdRecheckScanScopeExcludesNonCandidates(t *testing.T) {
	h := newRecheckHarness(t)
	ago := h.now.Add(-10 * time.Minute)
	rechecked := h.now.Add(-time.Minute)
	_, err := h.items.CreateMulti(context.Background(), []domain.ChangeItem{
		{ // 云通道成功项：不参与复检
			OrderID: h.orderID, Action: domain.ActionUploadAndBind,
			ResourceRef: domain.ResourceRef{Channel: domain.ChannelCloudAPI, Cloud: "aliyun",
				Product: "cdn", AccountKey: "acc1", ResourceID: "res-1"},
			Status: domain.ItemStatusSuccess, ExecutedAt: &ago,
		},
		{ // failed 项：无复检语义
			OrderID: h.orderID, Action: domain.ActionPatchCRD,
			ResourceRef: domain.ResourceRef{Channel: domain.ChannelK8sAPI, ClusterID: "c",
				Namespace: "default", Kind: "Ingress", ResourceID: "gw-failed"},
			Status: domain.ItemStatusFailed, ExecutedAt: &ago,
		},
		{ // 已复检通过项：幂等标记命中，不再消费
			OrderID: h.orderID, Action: domain.ActionPatchCRD,
			ResourceRef: domain.ResourceRef{Channel: domain.ChannelK8sAPI, ClusterID: "c",
				Namespace: "default", Kind: "Ingress", ResourceID: "gw-done"},
			Status: domain.ItemStatusSuccess, ExecutedAt: &ago, RecheckedAt: &rechecked,
		},
	})
	require.NoError(t, err)

	consumed, err := h.svc.RunDueRechecks(context.Background())
	require.NoError(t, err)
	assert.Zero(t, consumed)
	assert.Empty(t, h.rechecker.items)
}

// AC-4：并发竞争 CAS 落败方幂等跳过——项在扫描后、回填前被并发回滚时
// MarkRechecked 未命中，不重复告警（MarkRechecked CAS status=success 语义）。
func TestCrdRecheckConcurrentLoserSkipsAlert(t *testing.T) {
	h := newRecheckHarness(t)
	itemID := h.seedPatchItem(t, 10*time.Minute)
	// 预置项已被并发迁移为 rolled_back（CAS 落败）——直接调单项消费走
	// MarkRechecked 落败分支（扫描入口会先按 status 过滤，CAS 分支由消费
	// 单体承载）。
	_, err := h.items.FinishRollback(context.Background(), itemID, domain.ItemStatusRolledBack, "")
	require.NoError(t, err)
	item, err := h.items.GetByID(context.Background(), itemID)
	require.NoError(t, err)

	handled, err := h.svc.(*crdRecheckService).recheckItem(context.Background(), item)
	require.NoError(t, err)
	assert.False(t, handled) // CAS 未命中：幂等跳过
	assert.Len(t, h.rechecker.items, 1)
	assert.Empty(t, h.publisher.Events()) // 不重复告警
}

// 告警发布失败聚合上抛（不得静默）：项级 failed 已落地，仅通道故障以错误返回。
func TestCrdRecheckAlertFailureSurfacesError(t *testing.T) {
	h := newRecheckHarness(t)
	itemID := h.seedPatchItem(t, 10*time.Minute)
	h.rechecker.script["cert-k8s-1"] = recheckOutcome{
		pass:   false,
		reason: "reconcile 回写旧值（orderId=x itemId=y）",
	}
	svc := NewCrdRecheckService(h.orders, h.items, h.certs, h.alertCfg, h.rechecker, rollbackFailingPublisher{})
	impl := svc.(*crdRecheckService)
	impl.now = func() time.Time { return h.now }

	consumed, err := impl.RunDueRechecks(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "publish recheck-failed alert")
	assert.Equal(t, 1, consumed) // 复检动作已发生，项级失败已落
	item, err := h.items.GetByID(context.Background(), itemID)
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusFailed, item.Status)

	// nil publisher 回退日志实现（装配弹性）。
	assert.NotNil(t, NewCrdRecheckService(h.orders, h.items, h.certs, h.alertCfg, h.rechecker, nil))
}

// ctx 取消中断：未消费项保持 due（扫描幂等重入）。
func TestCrdRecheckCancelledContextBreaks(t *testing.T) {
	h := newRecheckHarness(t)
	h.seedPatchItem(t, 10*time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	consumed, err := h.svc.RunDueRechecks(ctx)
	require.NoError(t, err)
	assert.Zero(t, consumed)
	assert.Empty(t, h.rechecker.items)

	// 重入（未取消）正常消费。
	consumed, err = h.svc.RunDueRechecks(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, consumed)
}
