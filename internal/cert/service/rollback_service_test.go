package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------
// 回滚服务测试（任务 5.8 AC：三判定分支 / 仅成功项范围 / 部分回滚失败 /
// 订单收敛 / K8s patch 恢复 / 孤儿标记 / 告警与审计）
// ---------------------------------------------------------------------

const (
	rollbackTestOldFP = "aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11"
	rollbackTestNewFP = "ff44ff44ff44ff44ff44ff44ff44ff44ff44ff44ff44ff44ff44ff44ff44"
)

// rollbackChannelCall 回滚通道调用记录（target + 旧引用断言依据）。
type rollbackChannelCall struct {
	Target deployer.DeployTarget
	OldRef domain.CertReference
}

// rollbackScriptedChannel 回滚脚本通道：按 resourceId 配置失败（值为错误码，
// 空串=未映射失败），成功路径恢复传入 oldRef（mock 通道，AC-6）。
type rollbackScriptedChannel struct {
	mu      sync.Mutex
	typ     deployer.ChannelType
	failFor map[string]string
	calls   []rollbackChannelCall
}

func newRollbackScriptedChannel(typ deployer.ChannelType) *rollbackScriptedChannel {
	return &rollbackScriptedChannel{typ: typ, failFor: map[string]string{}}
}

func (c *rollbackScriptedChannel) Type() deployer.ChannelType { return c.typ }

func (c *rollbackScriptedChannel) Discover(_ context.Context, _ deployer.Credential, _ deployer.DiscoverScope) ([]domain.CertReference, error) {
	return nil, nil
}

func (c *rollbackScriptedChannel) Deploy(_ context.Context, _ deployer.Credential, _ deployer.DeployTarget, _ string) (deployer.DeployResult, error) {
	return deployer.DeployResult{}, nil
}

func (c *rollbackScriptedChannel) Rollback(_ context.Context, _ deployer.Credential, target deployer.DeployTarget, oldRef domain.CertReference) (deployer.RollbackResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, rollbackChannelCall{Target: target, OldRef: oldRef})
	if code, ok := c.failFor[target.ResourceID]; ok {
		reason := "scripted rollback failure"
		return deployer.RollbackResult{Success: false, ErrCode: code, Reason: reason}, errors.New(reason)
	}
	return deployer.RollbackResult{Success: true, RestoredRef: oldRef, OrphanCleaned: []string{}}, nil
}

func (c *rollbackScriptedChannel) rollbackCalls() []rollbackChannelCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]rollbackChannelCall(nil), c.calls...)
}

// fakeTargetSource GetCert 三判定假数据源（未播种 ID 返回零值 Exists=false）。
type fakeTargetSource struct {
	mu    sync.Mutex
	infos map[string]deployer.CloudCertInfo
	err   error
	calls []string
}

func (f *fakeTargetSource) InspectCloudCert(_ context.Context, _ deployer.Credential, _, cloudCertID string) (deployer.CloudCertInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, cloudCertID)
	if f.err != nil {
		return deployer.CloudCertInfo{}, f.err
	}
	return f.infos[cloudCertID], nil
}

func (f *fakeTargetSource) recordedCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// fakeRollbackAuditor 审计假实现。
type fakeRollbackAuditor struct {
	mu     sync.Mutex
	events []RollbackAuditEvent
}

func (f *fakeRollbackAuditor) RecordRollback(_ context.Context, event RollbackAuditEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
	return nil
}

func (f *fakeRollbackAuditor) Events() []RollbackAuditEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]RollbackAuditEvent(nil), f.events...)
}

// rollbackFailingPublisher 告警发布失败注入（Hard Rule：不得静默断言依据）。
type rollbackFailingPublisher struct{}

func (rollbackFailingPublisher) PublishAlert(context.Context, CertAlertEvent) error {
	return errors.New("alert channel down")
}

// rollbackSeedItem 订单项种子。
type rollbackSeedItem struct {
	status   domain.ChangeItemStatus
	resource string
	k8s      bool   // true=patch_crd/k8s_api 项
	oldID    string // 覆盖 oldCloudCertId（默认 old-cert-<resource>）
	omitOld  bool   // true=不写 oldCloudCertId（缺失目标分支）
}

// rollbackHarness 回滚服务测试依赖聚合。
type rollbackHarness struct {
	svc       *changeRollbackService
	orders    *certtest.FakeChangeOrderRepo
	items     *certtest.FakeChangeItemRepo
	certs     *certtest.FakeCertificateRepo
	alertCfg  *certtest.FakeAlertConfigRepo
	mappings  *certtest.FakeCloudCertMappingRepo
	channel   *rollbackScriptedChannel
	k8sChan   *rollbackScriptedChannel
	inspector *fakeTargetSource
	alerts    *InMemoryAlertPublisher
	auditor   *fakeRollbackAuditor
	now       time.Time
}

func newRollbackHarness(t *testing.T) *rollbackHarness {
	t.Helper()
	h := &rollbackHarness{
		orders:    certtest.NewFakeChangeOrderRepo(),
		items:     certtest.NewFakeChangeItemRepo(),
		certs:     certtest.NewFakeCertificateRepo(),
		alertCfg:  certtest.NewFakeAlertConfigRepo(),
		mappings:  certtest.NewFakeCloudCertMappingRepo(),
		channel:   newRollbackScriptedChannel(deployer.ChannelTypeCloudAPI),
		k8sChan:   newRollbackScriptedChannel(deployer.ChannelTypeK8sAPI),
		inspector: &fakeTargetSource{infos: map[string]deployer.CloudCertInfo{}},
		alerts:    NewInMemoryAlertPublisher(),
		auditor:   &fakeRollbackAuditor{},
	}
	h.now = time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	svc := NewChangeRollbackService(
		h.orders, h.items, h.certs, h.alertCfg, h.mappings,
		[]deployer.ExecutionChannel{h.channel, h.k8sChan},
		h.inspector,
		fakeCredentialSource{},
		h.alerts,
		h.auditor,
	).(*changeRollbackService)
	svc.now = func() time.Time { return h.now }
	h.svc = svc
	return h
}

// seedRollbackCert 台账写入旧证书（保护期固化断言依据）。
func (h *rollbackHarness) seedRollbackCert(t *testing.T) {
	t.Helper()
	require.NoError(t, h.certs.Create(context.Background(), &domain.Certificate{
		Fingerprint:   rollbackTestOldFP,
		HostingStatus: domain.HostingStatusComplete,
		CreatedAt:     h.now,
	}))
}

// seedOrder 写入订单与项，返回订单 ID。
func (h *rollbackHarness) seedOrder(t *testing.T, status domain.ChangeStatus, seeds ...rollbackSeedItem) string {
	t.Helper()
	h.seedRollbackCert(t)
	order := &domain.ChangeOrder{
		OldCertFingerprint: rollbackTestOldFP,
		NewCertID:          "new-cert-obj",
		Status:             status,
		SnapshotID:         "snap-rb",
		Creator:            "operator",
		CreatedAt:          h.now,
	}
	if domain.IsActiveChangeStatus(status) {
		order.ActiveMutex = rollbackTestOldFP
	}
	orderID, err := h.orders.Create(context.Background(), order)
	require.NoError(t, err)

	items := make([]domain.ChangeItem, 0, len(seeds))
	for _, sd := range seeds {
		action := domain.ActionUploadAndBind
		ref := domain.ResourceRef{
			Channel:    domain.ChannelCloudAPI,
			Cloud:      "aliyun",
			Product:    "cdn",
			AccountKey: "acct-main",
			ResourceID: sd.resource,
		}
		if sd.k8s {
			action = domain.ActionPatchCRD
			ref = domain.ResourceRef{
				Channel:    domain.ChannelK8sAPI,
				ClusterID:  "cluster-1",
				Namespace:  "default",
				Kind:       "Gateway",
				ResourceID: sd.resource,
			}
		}
		oldID := sd.oldID
		if oldID == "" && !sd.omitOld {
			oldID = "old-cert-" + sd.resource
		}
		newID := ""
		if !sd.k8s {
			newID = "new-cert-" + sd.resource
		}
		items = append(items, domain.ChangeItem{
			OrderID:        orderID,
			Action:         action,
			ResourceRef:    ref,
			OldCloudCertID: oldID,
			NewCloudCertID: newID,
			Status:         sd.status,
			BatchNo:        1,
		})
	}
	_, err = h.items.CreateMulti(context.Background(), items)
	require.NoError(t, err)
	return orderID
}

// seedValidTargets 为云项播种有效回滚目标 + 新云证书 active 映射。
func (h *rollbackHarness) seedValidTargets(t *testing.T, orderID string) {
	t.Helper()
	items, err := h.items.ListByOrder(context.Background(), orderID)
	require.NoError(t, err)
	for _, it := range items {
		if it.ResourceRef.Channel != domain.ChannelCloudAPI || it.OldCloudCertID == "" {
			continue
		}
		h.inspector.infos[it.OldCloudCertID] = deployer.CloudCertInfo{
			Exists:      true,
			NotAfter:    h.now.Add(90 * 24 * time.Hour),
			Fingerprint: rollbackTestOldFP,
		}
		if it.NewCloudCertID != "" {
			require.NoError(t, h.mappings.Upsert(context.Background(), &domain.CloudCertMapping{
				CertFingerprint: rollbackTestNewFP,
				Cloud:           "aliyun",
				AccountKey:      "acct-main",
				CloudCertID:     it.NewCloudCertID,
				Status:          domain.MappingStatusActive,
			}))
		}
	}
}

// itemID 取资源对应的项 ID。
func (h *rollbackHarness) itemID(t *testing.T, orderID, resource string) string {
	t.Helper()
	it, ok := h.findItem(t, orderID, resource)
	require.True(t, ok, "item for resource %s not found", resource)
	return it.ID.Hex()
}

// findItem 按资源定位项。
func (h *rollbackHarness) findItem(t *testing.T, orderID, resource string) (domain.ChangeItem, bool) {
	t.Helper()
	items, err := h.items.ListByOrder(context.Background(), orderID)
	require.NoError(t, err)
	for _, it := range items {
		if it.ResourceRef.ResourceID == resource {
			return it, true
		}
	}
	return domain.ChangeItem{}, false
}

// order 取订单当前状态。
func (h *rollbackHarness) order(t *testing.T, orderID string) domain.ChangeOrder {
	t.Helper()
	order, err := h.orders.GetByID(context.Background(), orderID)
	require.NoError(t, err)
	return order
}

// AC-1：仅成功项参与回滚；失败/skipped 项引用未被改动不参与（状态不变），
// 全部成功项回滚成功 → 订单收敛 rolled_back。
func TestRollbackSuccessOnlyScope(t *testing.T) {
	h := newRollbackHarness(t)
	ctx := context.Background()
	orderID := h.seedOrder(t, domain.ChangeStatusPartialCompleted,
		rollbackSeedItem{status: domain.ItemStatusSuccess, resource: "res-ok"},
		rollbackSeedItem{status: domain.ItemStatusFailed, resource: "res-fail"},
		rollbackSeedItem{status: domain.ItemStatusSkipped, resource: "res-skip"},
	)
	h.seedValidTargets(t, orderID)

	require.NoError(t, h.svc.Rollback(ctx, orderID, []string{
		h.itemID(t, orderID, "res-ok"),
		h.itemID(t, orderID, "res-fail"),
		h.itemID(t, orderID, "res-skip"),
	}))

	calls := h.channel.rollbackCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "res-ok", calls[0].Target.ResourceID)
	assert.Equal(t, "old-cert-res-ok", calls[0].OldRef.ReferencedCloudCertID)

	ok, _ := h.findItem(t, orderID, "res-ok")
	assert.Equal(t, domain.ItemStatusRolledBack, ok.Status)
	failed, _ := h.findItem(t, orderID, "res-fail")
	assert.Equal(t, domain.ItemStatusFailed, failed.Status)
	skipped, _ := h.findItem(t, orderID, "res-skip")
	assert.Equal(t, domain.ItemStatusSkipped, skipped.Status)

	order := h.order(t, orderID)
	assert.Equal(t, domain.ChangeStatusRolledBack, order.Status)
	assert.Empty(t, order.ActiveMutex)
}

// AC-1：入口状态门控（执行中（失败项后）/验证中/部分完成之外拒绝）。
func TestRollbackEntryStates(t *testing.T) {
	cases := []struct {
		name   string
		status domain.ChangeStatus
		ok     bool
	}{
		{"draft 拒绝", domain.ChangeStatusDraft, false},
		{"pending_confirm 拒绝", domain.ChangeStatusPendingConfirm, false},
		{"completed 非入口拒绝", domain.ChangeStatusCompleted, false},
		{"rolled_back 终态拒绝", domain.ChangeStatusRolledBack, false},
		{"cancelled 终态拒绝", domain.ChangeStatusCancelled, false},
		{"verifying 允许", domain.ChangeStatusVerifying, true},
		{"partial_completed 允许", domain.ChangeStatusPartialCompleted, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newRollbackHarness(t)
			ctx := context.Background()
			orderID := h.seedOrder(t, tc.status,
				rollbackSeedItem{status: domain.ItemStatusSuccess, resource: "res-ok"})
			h.seedValidTargets(t, orderID)

			err := h.svc.Rollback(ctx, orderID, []string{h.itemID(t, orderID, "res-ok")})
			if !tc.ok {
				require.Error(t, err)
				var ite *domain.InvalidTransitionError
				assert.ErrorAs(t, err, &ite)
				assert.Equal(t, tc.status, h.order(t, orderID).Status)
				assert.Empty(t, h.channel.rollbackCalls())
				return
			}
			require.NoError(t, err)
			assert.Len(t, h.channel.rollbackCalls(), 1)
		})
	}
}

// AC-1：executing 入口条件（出现失败项后；在途项须先收敛）与
// 未执行项 skipped 标记 + 订单收敛。
func TestRollbackExecutingEntryGates(t *testing.T) {
	ctx := context.Background()
	t.Run("无失败项拒绝", func(t *testing.T) {
		h := newRollbackHarness(t)
		orderID := h.seedOrder(t, domain.ChangeStatusExecuting,
			rollbackSeedItem{status: domain.ItemStatusSuccess, resource: "res-a"})
		h.seedValidTargets(t, orderID)
		err := h.svc.Rollback(ctx, orderID, []string{h.itemID(t, orderID, "res-a")})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "without failed items")
		assert.Empty(t, h.channel.rollbackCalls())
	})
	t.Run("在途项拒绝", func(t *testing.T) {
		h := newRollbackHarness(t)
		orderID := h.seedOrder(t, domain.ChangeStatusExecuting,
			rollbackSeedItem{status: domain.ItemStatusFailed, resource: "res-f"},
			rollbackSeedItem{status: domain.ItemStatusRunning, resource: "res-r"})
		h.seedValidTargets(t, orderID)
		err := h.svc.Rollback(ctx, orderID, []string{h.itemID(t, orderID, "res-f")})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "in-flight")
		assert.Empty(t, h.channel.rollbackCalls())
	})
	t.Run("失败项后放行：未执行项标 skipped 订单收敛 rolled_back", func(t *testing.T) {
		h := newRollbackHarness(t)
		orderID := h.seedOrder(t, domain.ChangeStatusExecuting,
			rollbackSeedItem{status: domain.ItemStatusSuccess, resource: "res-ok"},
			rollbackSeedItem{status: domain.ItemStatusFailed, resource: "res-f"},
			rollbackSeedItem{status: domain.ItemStatusPending, resource: "res-p"})
		h.seedValidTargets(t, orderID)

		require.NoError(t, h.svc.Rollback(ctx, orderID, []string{h.itemID(t, orderID, "res-ok")}))

		pending, _ := h.findItem(t, orderID, "res-p")
		assert.Equal(t, domain.ItemStatusSkipped, pending.Status)
		ok, _ := h.findItem(t, orderID, "res-ok")
		assert.Equal(t, domain.ItemStatusRolledBack, ok.Status)
		order := h.order(t, orderID)
		assert.Equal(t, domain.ChangeStatusRolledBack, order.Status)
		assert.Empty(t, order.ActiveMutex)
	})
}

// AC-1：范围解析输入校验（空清单/未知项/仅非成功项）。
func TestRollbackScopeValidation(t *testing.T) {
	h := newRollbackHarness(t)
	ctx := context.Background()
	orderID := h.seedOrder(t, domain.ChangeStatusPartialCompleted,
		rollbackSeedItem{status: domain.ItemStatusSuccess, resource: "res-ok"},
		rollbackSeedItem{status: domain.ItemStatusFailed, resource: "res-f"})
	h.seedValidTargets(t, orderID)

	require.Error(t, h.svc.Rollback(ctx, orderID, nil))
	require.Error(t, h.svc.Rollback(ctx, orderID, []string{"012345678901234567890123"}))
	require.Error(t, h.svc.Rollback(ctx, orderID, []string{h.itemID(t, orderID, "res-f")}))
	assert.Empty(t, h.channel.rollbackCalls())
}

// AC-2：前置三判定分支（已删除/已过期/指纹被替换）——整体阻断、无自动回滚、
// 项保持 success、订单保持入口状态、审计记录转人工。
func TestRollbackTargetInvalidBranches(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(h *rollbackHarness)
	}{
		{"云侧已删除", func(h *rollbackHarness) {
			h.inspector.infos["old-cert-res-ok"] = deployer.CloudCertInfo{}
		}},
		{"已过期", func(h *rollbackHarness) {
			info := h.inspector.infos["old-cert-res-ok"]
			info.NotAfter = h.now.Add(-time.Hour)
			h.inspector.infos["old-cert-res-ok"] = info
		}},
		{"指纹被替换", func(h *rollbackHarness) {
			info := h.inspector.infos["old-cert-res-ok"]
			info.Fingerprint = rollbackTestNewFP
			h.inspector.infos["old-cert-res-ok"] = info
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newRollbackHarness(t)
			ctx := context.Background()
			orderID := h.seedOrder(t, domain.ChangeStatusPartialCompleted,
				rollbackSeedItem{status: domain.ItemStatusSuccess, resource: "res-ok"})
			h.seedValidTargets(t, orderID)
			tc.mutate(h)

			err := h.svc.Rollback(ctx, orderID, []string{h.itemID(t, orderID, "res-ok")})
			require.Error(t, err)
			ce, ok := domain.AsCertError(err)
			require.True(t, ok)
			assert.Equal(t, domain.CodeRollbackTargetInvalid, ce.Code())

			// 无效目标绝不自动回滚：通道未触达、项保持 success、订单保持入口状态
			assert.Empty(t, h.channel.rollbackCalls())
			item, found := h.findItem(t, orderID, "res-ok")
			require.True(t, found)
			assert.Equal(t, domain.ItemStatusSuccess, item.Status)
			assert.Equal(t, domain.ChangeStatusPartialCompleted, h.order(t, orderID).Status)

			// 审计记录（转人工决策）
			events := h.auditor.Events()
			require.Len(t, events, 1)
			assert.Equal(t, RollbackAuditTargetInvalid, events[0].Outcome)
			assert.Equal(t, h.itemID(t, orderID, "res-ok"), events[0].ItemID)
		})
	}
	t.Run("oldCloudCertId 缺失", func(t *testing.T) {
		h := newRollbackHarness(t)
		ctx := context.Background()
		orderID := h.seedOrder(t, domain.ChangeStatusPartialCompleted,
			rollbackSeedItem{status: domain.ItemStatusSuccess, resource: "res-ok", omitOld: true})
		h.seedValidTargets(t, orderID)

		err := h.svc.Rollback(ctx, orderID, []string{h.itemID(t, orderID, "res-ok")})
		require.Error(t, err)
		ce, ok := domain.AsCertError(err)
		require.True(t, ok)
		assert.Equal(t, domain.CodeRollbackTargetInvalid, ce.Code())
		assert.Empty(t, h.channel.rollbackCalls())
		assert.Len(t, h.auditor.Events(), 1)
	})
}

// AC-2：GetCert 基础设施故障 fail closed（非"无效目标"语义，可重试）。
func TestRollbackGetCertErrorFailsClosed(t *testing.T) {
	h := newRollbackHarness(t)
	ctx := context.Background()
	orderID := h.seedOrder(t, domain.ChangeStatusPartialCompleted,
		rollbackSeedItem{status: domain.ItemStatusSuccess, resource: "res-ok"})
	h.seedValidTargets(t, orderID)
	h.inspector.err = errors.New("cloud api down")

	err := h.svc.Rollback(ctx, orderID, []string{h.itemID(t, orderID, "res-ok")})
	require.Error(t, err)
	_, isCertErr := domain.AsCertError(err)
	assert.False(t, isCertErr)
	assert.Contains(t, err.Error(), "inspect cloud cert")
	assert.Empty(t, h.channel.rollbackCalls())
	assert.Equal(t, domain.ChangeStatusPartialCompleted, h.order(t, orderID).Status)
}

// AC-3/AC-4/AC-5：有效目标回滚成功——通道恢复旧引用、新云证书映射转 orphan
// （5.9 队列 + OrphanCleaned 审计载荷）、订单收敛 rolled_back、保护期固化。
func TestRollbackHappyPathRecords(t *testing.T) {
	h := newRollbackHarness(t)
	ctx := context.Background()
	orderID := h.seedOrder(t, domain.ChangeStatusVerifying,
		rollbackSeedItem{status: domain.ItemStatusSuccess, resource: "res-ok"})
	h.seedValidTargets(t, orderID)

	require.NoError(t, h.svc.Rollback(ctx, orderID, []string{h.itemID(t, orderID, "res-ok")}))

	calls := h.channel.rollbackCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "old-cert-res-ok", calls[0].OldRef.ReferencedCloudCertID)
	assert.Equal(t, "res-ok", calls[0].Target.ResourceID)

	// 新云证书映射 active→orphan（5.9 清理队列入口）
	m, err := h.mappings.FindByCloudCertID(ctx, "aliyun", "acct-main", "new-cert-res-ok")
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusOrphan, m.Status)

	// verifying 入口先收敛 partial_completed（白名单前置终态）再 rolled_back；
	// 保护期随终态固化（默认 rollbackProtectDays=7）
	order := h.order(t, orderID)
	assert.Equal(t, domain.ChangeStatusRolledBack, order.Status)
	require.NotNil(t, order.ProtectUntil)
	assert.Equal(t, h.now.AddDate(0, 0, 7), *order.ProtectUntil)
	cert, err := h.certs.GetByFingerprint(ctx, rollbackTestOldFP)
	require.NoError(t, err)
	require.NotNil(t, cert.ProtectUntil)
	assert.Equal(t, h.now.AddDate(0, 0, 7), *cert.ProtectUntil)

	// 审计：项 rolled_back（含 OrphanCleaned 载荷）+ 订单 rolled_back
	events := h.auditor.Events()
	require.Len(t, events, 2)
	assert.Equal(t, RollbackAuditItemRolledBack, events[0].Outcome)
	assert.Contains(t, events[0].Detail, "orphanCleaned=new-cert-res-ok")
	assert.Equal(t, RollbackAuditOrderRolledBack, events[1].Outcome)

	// 回滚成功不产生四类告警
	assert.Empty(t, h.alerts.Events())
}

// AC-3/AC-4：部分回滚失败 → 失败项 rollback_failed + 立即告警（四类之
// "回滚失���"）转人工，成功项不受影响，订单收敛 rollback_failed 终态。
func TestRollbackPartialFailureConvergesRollbackFailed(t *testing.T) {
	h := newRollbackHarness(t)
	ctx := context.Background()
	orderID := h.seedOrder(t, domain.ChangeStatusPartialCompleted,
		rollbackSeedItem{status: domain.ItemStatusSuccess, resource: "res-a"},
		rollbackSeedItem{status: domain.ItemStatusSuccess, resource: "res-b"})
	h.seedValidTargets(t, orderID)
	h.channel.failFor["res-b"] = "CLOUD_API_RATELIMITED"

	// 项级失败不以同步错误返回（异步子任务状态口径）
	require.NoError(t, h.svc.Rollback(ctx, orderID, []string{
		h.itemID(t, orderID, "res-a"), h.itemID(t, orderID, "res-b"),
	}))

	a, _ := h.findItem(t, orderID, "res-a")
	assert.Equal(t, domain.ItemStatusRolledBack, a.Status)
	b, _ := h.findItem(t, orderID, "res-b")
	assert.Equal(t, domain.ItemStatusRollbackFailed, b.Status)
	assert.Contains(t, b.Error, "CLOUD_API_RATELIMITED")

	// 立即告警（Hard Rule：不得静默）
	alerts := h.alerts.Events()
	require.Len(t, alerts, 1)
	assert.Equal(t, AlertCategoryRollbackFailed, alerts[0].Category)
	assert.Equal(t, orderID, alerts[0].OrderID)
	assert.Equal(t, rollbackTestOldFP, alerts[0].Fingerprint)

	order := h.order(t, orderID)
	assert.Equal(t, domain.ChangeStatusRollbackFailed, order.Status)
	assert.Empty(t, order.ActiveMutex)

	// 审计：两项级事件 + 订单 rollback_failed
	events := h.auditor.Events()
	require.Len(t, events, 3)
	assert.Equal(t, RollbackAuditItemRolledBack, events[0].Outcome)
	assert.Equal(t, RollbackAuditItemFailed, events[1].Outcome)
	assert.Equal(t, RollbackAuditOrderFailed, events[2].Outcome)
}

// AC-3：K8s 项按 patch 恢复旧值（不经云证书库 GetCert 三判定）。
func TestRollbackK8sItemPatchRestore(t *testing.T) {
	h := newRollbackHarness(t)
	ctx := context.Background()
	orderID := h.seedOrder(t, domain.ChangeStatusPartialCompleted,
		rollbackSeedItem{status: domain.ItemStatusSuccess, resource: "gw-1", k8s: true},
		rollbackSeedItem{status: domain.ItemStatusSuccess, resource: "res-a"})
	h.seedValidTargets(t, orderID)

	require.NoError(t, h.svc.Rollback(ctx, orderID, []string{
		h.itemID(t, orderID, "gw-1"), h.itemID(t, orderID, "res-a"),
	}))

	// GetCert 仅触达云项
	assert.Equal(t, []string{"old-cert-res-a"}, h.inspector.recordedCalls())

	kcalls := h.k8sChan.rollbackCalls()
	require.Len(t, kcalls, 1)
	assert.Equal(t, "old-cert-gw-1", kcalls[0].OldRef.ReferencedCloudCertID)
	assert.Equal(t, domain.ProductCRD, kcalls[0].OldRef.Product)
	assert.Equal(t, "cluster-1", kcalls[0].Target.ClusterID)

	gw, _ := h.findItem(t, orderID, "gw-1")
	assert.Equal(t, domain.ItemStatusRolledBack, gw.Status)
	assert.Equal(t, domain.ChangeStatusRolledBack, h.order(t, orderID).Status)
}

// AC-4：部分成功项回滚（清单仍有 success 项）→ 订单保持 partial_completed
// 可续作；剩余项回滚完成后收敛 rolled_back。
func TestRollbackSubsetKeepsOrderOpen(t *testing.T) {
	h := newRollbackHarness(t)
	ctx := context.Background()
	orderID := h.seedOrder(t, domain.ChangeStatusPartialCompleted,
		rollbackSeedItem{status: domain.ItemStatusSuccess, resource: "res-a"},
		rollbackSeedItem{status: domain.ItemStatusSuccess, resource: "res-b"})
	h.seedValidTargets(t, orderID)

	require.NoError(t, h.svc.Rollback(ctx, orderID, []string{h.itemID(t, orderID, "res-a")}))
	a, _ := h.findItem(t, orderID, "res-a")
	assert.Equal(t, domain.ItemStatusRolledBack, a.Status)
	b, _ := h.findItem(t, orderID, "res-b")
	assert.Equal(t, domain.ItemStatusSuccess, b.Status)
	assert.Equal(t, domain.ChangeStatusPartialCompleted, h.order(t, orderID).Status)
	assert.Len(t, h.auditor.Events(), 2) // 项 rolled_back + order_held

	require.NoError(t, h.svc.Rollback(ctx, orderID, []string{h.itemID(t, orderID, "res-b")}))
	assert.Equal(t, domain.ChangeStatusRolledBack, h.order(t, orderID).Status)
}

// AC-3/Hard Rule：告警发布失败不得静默（错误上抛），项级 rollback_failed
// 状态与订单终态不受影响。
func TestRollbackAlertPublishFailureSurfaced(t *testing.T) {
	h := newRollbackHarness(t)
	ctx := context.Background()
	orderID := h.seedOrder(t, domain.ChangeStatusPartialCompleted,
		rollbackSeedItem{status: domain.ItemStatusSuccess, resource: "res-a"})
	h.seedValidTargets(t, orderID)
	h.channel.failFor["res-a"] = ""
	h.svc.publisher = rollbackFailingPublisher{}

	err := h.svc.Rollback(ctx, orderID, []string{h.itemID(t, orderID, "res-a")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "publish rollback-failed alert")

	a, _ := h.findItem(t, orderID, "res-a")
	assert.Equal(t, domain.ItemStatusRollbackFailed, a.Status)
	assert.Contains(t, a.Error, rollbackErrGeneric)
	assert.Equal(t, domain.ChangeStatusRollbackFailed, h.order(t, orderID).Status)
}
