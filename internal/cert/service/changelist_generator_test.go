package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
)

// ---------------------------------------------------------------------
// 测试依赖（5.2 清单生成：���存假实现 + 管理权探测 fake）
// ---------------------------------------------------------------------

// newTestFP 新证书测试指纹（与 changeTestFP 同口径 64 位 hex）。
const newTestFP = "ee55ee55ee55ee55ee55ee55ee55ee55ee55ee55ee55ee55ee55ee55ee55"

// fakeManagementProbe K8s 管理权探测假实现（5.6 前经接口注入解耦）。
// notManageable 按 resource key 命中即返回不可变更+原因；err 非 nil 时全部失败。
type fakeManagementProbe struct {
	notManageable map[string]string // cluster|ns|kind|name → 命中信号原因
	err           error
	probed        []string // 探测调用记录（resource key 序列）
}

func k8sProbeKey(ref domain.ResourceRef) string {
	return strings.Join([]string{ref.ClusterID, ref.Namespace, ref.Kind, ref.ResourceID}, "|")
}

// Probe 三信号探测假实现：默认可管理，命中 notManageable / err 时不可。
func (f *fakeManagementProbe) Probe(_ context.Context, ref domain.ResourceRef) (bool, string, error) {
	f.probed = append(f.probed, k8sProbeKey(ref))
	if f.err != nil {
		return false, "", f.err
	}
	if reason, hit := f.notManageable[k8sProbeKey(ref)]; hit {
		return false, reason, nil
	}
	return true, "", nil
}

// genHarness 清单生成测试依赖聚合（复用 changeHarness，注入管理权探测）。
type genHarness struct {
	*changeHarness
	probe *fakeManagementProbe
}

// newGenHarness 创建清单生成测试聚合；probe 为 nil 时按"探测通道未接入"装配
// （注意规避 typed-nil 接口非 nil 陷阱）。
func newGenHarness(t *testing.T, probe *fakeManagementProbe) *genHarness {
	t.Helper()
	base := newChangeHarness(t)
	g := &genHarness{changeHarness: base, probe: probe}
	var injected ManagementProbe
	if probe != nil {
		injected = probe
	}
	base.svc = NewChangeService(base.orders, base.items, base.certs, base.alertCfg,
		base.snapshots, base.refs, injected)
	return g
}

// seedCert 写入台账证书，返回文档 ID（hex）。
func (h *genHarness) seedCert(t *testing.T, fp string, sans []string, hosting domain.HostingStatus) string {
	t.Helper()
	c := &domain.Certificate{Fingerprint: fp, Sans: sans, HostingStatus: hosting}
	require.NoError(t, h.certs.Create(context.Background(), c))
	return c.ID.Hex()
}

// seedDoneSnapshot 写入成功快照（startedAt=now-age），返回快照 ID。
func (h *genHarness) seedDoneSnapshot(t *testing.T, age time.Duration, meta []domain.CoverageMeta, partials []domain.ScanChannelFailure) string {
	t.Helper()
	snap := &domain.ScanSnapshot{
		StartedAt:       time.Now().Add(-age),
		Status:          domain.ScanStatusDone,
		CoverageMeta:    meta,
		PartialFailures: partials,
	}
	id, err := h.snapshots.Create(context.Background(), snap)
	require.NoError(t, err)
	return id
}

// cloudRef 云引用构造（upload_and_bind 分支数据源）。
func cloudRef(fp string, cloud domain.Cloud, product domain.Product, accountKey, resourceID, cloudCertID string) domain.CertReference {
	return domain.CertReference{
		CertFingerprint: fp, Cloud: cloud, Product: product,
		AccountKey: accountKey, ResourceID: resourceID, ReferencedCloudCertID: cloudCertID,
	}
}

// k8sRef K8s CRD 引用构造（patch_crd 分支数据源）。
func k8sRef(fp, cluster, namespace, kind, name, cloudCertID string) domain.CertReference {
	return domain.CertReference{
		CertFingerprint: fp, Product: domain.ProductCRD,
		ClusterID: cluster, Namespace: namespace, Kind: kind,
		ResourceID: name, ReferencedCloudCertID: cloudCertID,
	}
}

// seedRefs 写入快照内引用（回写 snapshotId）。
func (h *genHarness) seedRefs(t *testing.T, snapshotID string, refs ...domain.CertReference) {
	t.Helper()
	for i := range refs {
		refs[i].SnapshotID = snapshotID
	}
	_, err := h.refs.CreateMulti(context.Background(), refs)
	require.NoError(t, err)
}

// seedValid 写入清单生成通过态全部前置（2h 新鲜快照 + 新旧 complete 证书 +
// 新证书 SAN ⊇ 旧证书 SAN），返回 (newCertID, snapID)。refs 逐项写入快照。
func (h *genHarness) seedValid(t *testing.T, refs ...domain.CertReference) (string, string) {
	t.Helper()
	h.seedCert(t, changeTestFP, []string{"a.example.com", "b.example.com"}, domain.HostingStatusComplete)
	newCertID := h.seedCert(t, newTestFP, []string{"a.example.com", "b.example.com", "c.example.com"}, domain.HostingStatusComplete)
	snapID := h.seedDoneSnapshot(t, 2*time.Hour, nil, nil)
	if len(refs) > 0 {
		h.seedRefs(t, snapID, refs...)
	}
	return newCertID, snapID
}

// hasWarning warnings 中是否存在含 substr 的声明。
func hasWarning(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

// noActiveOrder 断言指纹无在途单（阻断路径未落库订单）。
func noActiveOrder(t *testing.T, h *genHarness, fp string) {
	t.Helper()
	_, err := h.orders.GetByMutexToken(context.Background(), fp)
	assert.ErrorIs(t, err, mongo.ErrNoDocuments, "阻断路径不得落库订单")
}

// requireBlockCode 断言错误命中指定 CERT_* 码。
func requireBlockCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	ce, ok := domain.AsCertError(err)
	require.True(t, ok, "应为 CertError：%v", err)
	assert.Equal(t, code, ce.Code())
}

// ---------------------------------------------------------------------
// AC-1/AC-5：指纹聚合 + 快照绑定 + 预生成订单/变更项 + SAN 预检结果
// ---------------------------------------------------------------------

// TestGenerateChangeList_HappyPath（AC-1/AC-5）
// 聚合最新成功快照内该指纹的引用（跨快照/异指纹/同资源重复项排除），
// 预生成 pending_confirm 订单（绑定 snapshotId + 条件写入 activeMutex），
// 持久化变更项，SAN 预检 Passed/NewSANs，ScanFreshnessHrs 固化。
func TestGenerateChangeList_HappyPath(t *testing.T) {
	ctx := context.Background()
	h := newGenHarness(t, &fakeManagementProbe{})

	newCertID, _ := h.seedValid(t,
		cloudRef(changeTestFP, domain.CloudAliyun, domain.ProductCDN, "ak-1", "res-cdn-1", "cloud-cert-old-1"),
		cloudRef(changeTestFP, domain.CloudAliyun, domain.ProductCDN, "ak-1", "res-cdn-1", "cloud-cert-old-1"), // 同资源重复 → 聚合去重
		cloudRef(changeTestFP, domain.CloudTencent, domain.ProductCLB, "ak-t", "res-clb-1", "cloud-cert-old-2"),
		cloudRef(newTestFP, domain.CloudAliyun, domain.ProductCDN, "ak-1", "res-cdn-9", "cloud-cert-new-9"), // 异指纹 → 排除
	)
	// 更早的 done 快照内同指纹引用不得混入（聚合源=最新成功快照）
	olderSnap := h.seedDoneSnapshot(t, 30*time.Hour, nil, nil)
	h.seedRefs(t, olderSnap, cloudRef(changeTestFP, domain.CloudAliyun, domain.ProductCDN, "ak-1", "res-stale", "cloud-cert-stale"))

	list, err := h.svc.GenerateChangeList(ctx, changeTestFP, newCertID)
	require.NoError(t, err)

	// 订单：pending_confirm + 快照绑定 + 互斥 token
	order, err := h.orders.GetByID(ctx, list.OrderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusPendingConfirm, order.Status)
	assert.Equal(t, list.SnapshotID, order.SnapshotID)
	assert.Equal(t, changeTestFP, order.OldCertFingerprint)
	assert.Equal(t, newCertID, order.NewCertID)
	assert.Equal(t, changeTestFP, order.ActiveMutex, "预生成订单即持有互斥 token")

	// 清单载荷：快照绑定 + 新鲜度 + 项数（去重/异指纹后 2 项）
	assert.Equal(t, order.ID.Hex(), list.OrderID)
	assert.Equal(t, 2, len(list.Items))
	assert.Equal(t, 2, list.ScanFreshnessHrs, "生成时扫描新鲜度（小时）")
	assert.Equal(t, changeTestFP, list.OldFingerprint)
	assert.Equal(t, newCertID, list.NewCertID)

	// 变更项持久化：与清单一一对应、pending、绑定订单
	items, err := h.items.ListByOrder(ctx, list.OrderID)
	require.NoError(t, err)
	require.Len(t, items, 2)
	ids := make(map[string]domain.ChangeItem, len(items))
	for _, it := range items {
		ids[it.ID.Hex()] = it
	}
	for _, li := range list.Items {
		persisted, ok := ids[li.ItemID]
		require.True(t, ok, "清单项 ID 与持久化变更项对应：%s", li.ItemID)
		assert.Equal(t, domain.ItemStatusPending, persisted.Status)
		assert.Equal(t, list.OrderID, persisted.OrderID)
		assert.Equal(t, li.Action, persisted.Action)
		assert.Equal(t, li.Target.ToResourceRef(), persisted.ResourceRef, "resourceRef 持久化完整 DeployTarget")
	}
	// oldCloudCertId 回滚依据写通
	assert.Equal(t, "cloud-cert-old-1", ids[list.Items[0].ItemID].OldCloudCertID)
	assert.Equal(t, "cloud-cert-old-2", ids[list.Items[1].ItemID].OldCloudCertID)

	// SAN 预检结果：通过 + 新增域名提示
	assert.True(t, list.SANCheck.Passed)
	assert.Empty(t, list.SANCheck.Missing)
	assert.Equal(t, []string{"c.example.com"}, list.SANCheck.NewSANs)

	// 盲区声明：覆盖边界恒定存在
	assert.True(t, hasWarning(list.Warnings, "VM Nginx"), "恒定覆盖边界声明")
}

// TestGenerateChangeList_ResourceRefBranches（AC-1）
// resourceRef 按 action 分支必填：cloud_api={channel,cloud,product,accountKey,
// resourceId}；patch_crd={channel,clusterId,namespace,kind,resourceId}。
func TestGenerateChangeList_ResourceRefBranches(t *testing.T) {
	ctx := context.Background()
	h := newGenHarness(t, &fakeManagementProbe{})

	newCertID, snapID := h.seedValid(t,
		cloudRef(changeTestFP, domain.CloudAliyun, domain.ProductDCDN, "ak-1", "res-dcdn-1", "cloud-cert-a"),
		k8sRef(changeTestFP, "c1", "team-a", "ALBConfig", "gw-1", "cert-k8s-a"),
	)

	list, err := h.svc.GenerateChangeList(ctx, changeTestFP, newCertID)
	require.NoError(t, err)
	require.Len(t, list.Items, 2)

	byResource := make(map[string]ChangeListItem, len(list.Items))
	for _, li := range list.Items {
		byResource[li.Target.ResourceID] = li
	}

	// cloud_api 分支
	cloud := byResource["res-dcdn-1"]
	assert.Equal(t, domain.ActionUploadAndBind, cloud.Action)
	assert.Equal(t, deployer.DeployTarget{
		Channel: "cloud_api", Cloud: "aliyun", Product: "dcdn", AccountKey: "ak-1", ResourceID: "res-dcdn-1",
	}, cloud.Target)
	assert.True(t, cloud.AutoChangeable, "部署器云引用可自动变更")

	// patch_crd 分支
	crd := byResource["gw-1"]
	assert.Equal(t, domain.ActionPatchCRD, crd.Action)
	assert.Equal(t, deployer.DeployTarget{
		Channel: "k8s_api", ClusterID: "c1", Namespace: "team-a", Kind: "ALBConfig", ResourceID: "gw-1",
	}, crd.Target)
	assert.True(t, crd.AutoChangeable, "探测可管理的 K8s 引用可自动变更")

	// 持久化形态 = 完整 DeployTarget 转换（5.7 子任务不回查台账/快照）
	items, err := h.items.ListByOrder(ctx, list.OrderID)
	require.NoError(t, err)
	for _, it := range items {
		require.NoError(t, deployer.DeployTargetFromResourceRef(it.ResourceRef).Validate(),
			"持久化 resourceRef 满足分支必填校验")
	}
	assert.Equal(t, snapID, list.SnapshotID)
}

// ---------------------------------------------------------------------
// AC-2：四项前置校验阻断
// ---------------------------------------------------------------------

// TestGenerateChangeList_BlockScanStale（AC-2）
// 扫描超期（now-startedAt > scanFreshnessHours）→ SCAN_STALE；
// 无成功快照同样阻断；阈值取全局配置（放行用例）。
func TestGenerateChangeList_BlockScanStale(t *testing.T) {
	ctx := context.Background()

	t.Run("快照超期阻断", func(t *testing.T) {
		h := newGenHarness(t, &fakeManagementProbe{})
		h.seedCert(t, changeTestFP, []string{"a.example.com"}, domain.HostingStatusComplete)
		newCertID := h.seedCert(t, newTestFP, []string{"a.example.com"}, domain.HostingStatusComplete)
		h.seedDoneSnapshot(t, 25*time.Hour, nil, nil) // 默认阈值 24h

		_, err := h.svc.GenerateChangeList(ctx, changeTestFP, newCertID)
		requireBlockCode(t, err, domain.CodeScanStale)
		assert.ErrorIs(t, err, domain.ErrScanStale)
		noActiveOrder(t, h, changeTestFP)
	})

	t.Run("无成功快照阻断", func(t *testing.T) {
		h := newGenHarness(t, &fakeManagementProbe{})
		h.seedCert(t, changeTestFP, []string{"a.example.com"}, domain.HostingStatusComplete)
		newCertID := h.seedCert(t, newTestFP, []string{"a.example.com"}, domain.HostingStatusComplete)
		// 仅 running 快照：无 done 快照，新鲜度无从建立
		_, err := h.snapshots.Create(ctx, &domain.ScanSnapshot{Status: domain.ScanStatusRunning})

		require.NoError(t, err)
		_, err = h.svc.GenerateChangeList(ctx, changeTestFP, newCertID)
		requireBlockCode(t, err, domain.CodeScanStale)
		noActiveOrder(t, h, changeTestFP)
	})

	t.Run("阈值取全局配置放行", func(t *testing.T) {
		h := newGenHarness(t, &fakeManagementProbe{})
		cfg, err := h.alertCfg.Get(ctx)
		require.NoError(t, err)
		cfg.Thresholds.ScanFreshnessHours = 48
		require.NoError(t, h.alertCfg.Save(ctx, &cfg))
		h.seedCert(t, changeTestFP, []string{"a.example.com"}, domain.HostingStatusComplete)
		newCertID := h.seedCert(t, newTestFP, []string{"a.example.com"}, domain.HostingStatusComplete)
		snapID := h.seedDoneSnapshot(t, 30*time.Hour, nil, nil) // 48h 阈值内
		h.seedRefs(t, snapID, cloudRef(changeTestFP, domain.CloudAliyun, domain.ProductCDN, "ak-1", "res-1", "cc-1"))

		list, err := h.svc.GenerateChangeList(ctx, changeTestFP, newCertID)
		require.NoError(t, err)
		assert.Equal(t, 30, list.ScanFreshnessHrs)
	})
}

// TestGenerateChangeList_BlockChangeInFlight（AC-2）
// 同 oldFingerprint 已有活跃单（activeMutex）→ CHANGE_IN_FLIGHT；
// 插入路径竞态（索引强制）同样映射 CHANGE_IN_FLIGHT。
func TestGenerateChangeList_BlockChangeInFlight(t *testing.T) {
	ctx := context.Background()

	t.Run("预检查命中", func(t *testing.T) {
		h := newGenHarness(t, &fakeManagementProbe{})
		newCertID, _ := h.seedValid(t)
		// 同指纹在途单（pending_confirm，持有互斥 token）
		_, err := h.orders.Create(ctx, &domain.ChangeOrder{
			OldCertFingerprint: changeTestFP, NewCertID: "cert-x",
			Status: domain.ChangeStatusPendingConfirm, SnapshotID: "snap-x",
			ActiveMutex: changeTestFP, Creator: "op",
		})
		require.NoError(t, err)

		_, err = h.svc.GenerateChangeList(ctx, changeTestFP, newCertID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrChangeInFlight, "CHANGE_IN_FLIGHT（仓储哨兵）")
	})

	t.Run("插入竞态由索引兜底", func(t *testing.T) {
		h := newGenHarness(t, &fakeManagementProbe{})
		newCertID, _ := h.seedValid(t)
		// 预检查放行（无 token 命中）但插入撞 uk_active_mutex → 同码阻断
		h.svc = NewChangeService(&failGenOrders{FakeChangeOrderRepo: h.orders, createErr: domain.ErrChangeInFlight},
			h.items, h.certs, h.alertCfg, h.snapshots, h.refs, nil)

		_, err := h.svc.GenerateChangeList(ctx, changeTestFP, newCertID)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrChangeInFlight)
	})
}

// TestGenerateChangeList_BlockNewCertFingerprintOnly（AC-2）
// 新证书 hostingStatus≠complete → NEW_CERT_FINGERPRINT_ONLY（无私钥无法上传云证书库）。
func TestGenerateChangeList_BlockNewCertFingerprintOnly(t *testing.T) {
	ctx := context.Background()
	h := newGenHarness(t, &fakeManagementProbe{})
	h.seedCert(t, changeTestFP, []string{"a.example.com"}, domain.HostingStatusComplete)
	newCertID := h.seedCert(t, newTestFP, []string{"a.example.com"}, domain.HostingStatusFingerprintOnly)
	h.seedDoneSnapshot(t, 2*time.Hour, nil, nil)

	_, err := h.svc.GenerateChangeList(ctx, changeTestFP, newCertID)
	requireBlockCode(t, err, domain.CodeNewCertFingerprintOnly)
	assert.ErrorIs(t, err, domain.ErrNewCertFingerprintOnly)
	noActiveOrder(t, h, changeTestFP)
}

// TestGenerateChangeList_BlockSanInsufficient（AC-2）
// 新证书 SAN ⊉ 目标域名（Missing 非空）→ SAN_INSUFFICIENT；域名大小写不敏感。
func TestGenerateChangeList_BlockSanInsufficient(t *testing.T) {
	ctx := context.Background()

	t.Run("缺失域名阻断", func(t *testing.T) {
		h := newGenHarness(t, &fakeManagementProbe{})
		h.seedCert(t, changeTestFP, []string{"a.example.com", "b.example.com", "c.example.com"}, domain.HostingStatusComplete)
		newCertID := h.seedCert(t, newTestFP, []string{"a.example.com"}, domain.HostingStatusComplete)
		h.seedDoneSnapshot(t, 2*time.Hour, nil, nil)

		_, err := h.svc.GenerateChangeList(ctx, changeTestFP, newCertID)
		requireBlockCode(t, err, domain.CodeSanInsufficient)
		assert.ErrorIs(t, err, domain.ErrSanInsufficient)
		assert.Contains(t, err.Error(), "b.example.com", "缺失域名入错文案")
		assert.Contains(t, err.Error(), "c.example.com")
		noActiveOrder(t, h, changeTestFP)
	})

	t.Run("域名大小写不敏感", func(t *testing.T) {
		h := newGenHarness(t, &fakeManagementProbe{})
		h.seedCert(t, changeTestFP, []string{"Example.COM"}, domain.HostingStatusComplete)
		newCertID := h.seedCert(t, newTestFP, []string{"example.com"}, domain.HostingStatusComplete)
		h.seedDoneSnapshot(t, 2*time.Hour, nil, nil)

		_, err := h.svc.GenerateChangeList(ctx, changeTestFP, newCertID)
		assert.NoError(t, err)
	})
}

// ---------------------------------------------------------------------
// AC-3：不可执行项分区
// ---------------------------------------------------------------------

// TestGenerateChangeList_NonExecutablePartition（AC-3）
// discovery-only 云（huawei/aws/azure）→ AutoChangeable=false+ERR_DISCOVERY_ONLY；
// K8s 三信号判定经 managementProbe 注入（5.6 实现）：不可管理项 false+原因；
// 不可执行项持久化即标 skipped（不计入执行成功率分母），可执行项 pending。
func TestGenerateChangeList_NonExecutablePartition(t *testing.T) {
	ctx := context.Background()
	probe := &fakeManagementProbe{notManageable: map[string]string{
		"c1|team-a|ALBConfig|gw-2": "GitOps label argocd.argoproj.io/instance=demo-app",
	}}
	h := newGenHarness(t, probe)
	newCertID, _ := h.seedValid(t,
		cloudRef(changeTestFP, domain.CloudHuawei, domain.ProductCDN, "ak-hw", "res-hw-1", "cc-hw"),
		cloudRef(changeTestFP, domain.CloudAWS, domain.ProductALB, "ak-aws", "res-aws-1", "cc-aws"),
		cloudRef(changeTestFP, domain.CloudAzure, domain.ProductCDN, "ak-az", "res-az-1", "cc-az"),
		cloudRef(changeTestFP, domain.CloudAliyun, domain.ProductCDN, "ak-1", "res-aliyun-1", "cc-1"),
		k8sRef(changeTestFP, "c1", "team-a", "ALBConfig", "gw-1", "cert-k8s-1"),
		k8sRef(changeTestFP, "c1", "team-a", "ALBConfig", "gw-2", "cert-k8s-2"),
	)

	list, err := h.svc.GenerateChangeList(ctx, changeTestFP, newCertID)
	require.NoError(t, err)
	require.Len(t, list.Items, 6)

	byResource := make(map[string]ChangeListItem, len(list.Items))
	for _, li := range list.Items {
		byResource[li.Target.ResourceID] = li
	}

	// discovery-only 三云：不可执行 + ERR_DISCOVERY_ONLY 原因
	for _, res := range []string{"res-hw-1", "res-aws-1", "res-az-1"} {
		li := byResource[res]
		assert.False(t, li.AutoChangeable, "%s 首期无部署器", res)
		assert.Contains(t, li.Reason, "ERR_DISCOVERY_ONLY")
	}
	// 部署器云：可执行
	assert.True(t, byResource["res-aliyun-1"].AutoChangeable)
	assert.Empty(t, byResource["res-aliyun-1"].Reason)
	// K8s：探测可管理 → 可执行；命中 GitOps 信号 → 不可执行+信号原因
	assert.True(t, byResource["gw-1"].AutoChangeable)
	assert.False(t, byResource["gw-2"].AutoChangeable)
	assert.Contains(t, byResource["gw-2"].Reason, "GitOps label argocd.argoproj.io/instance=demo-app")
	// 探测仅触达 K8s 项
	assert.Equal(t, []string{"c1|team-a|ALBConfig|gw-1", "c1|team-a|ALBConfig|gw-2"}, probe.probed)

	// 持久化分区：不可执行项 skipped+原因（不计成功率分母），可执行项 pending
	items, err := h.items.ListByOrder(ctx, list.OrderID)
	require.NoError(t, err)
	pending, skipped := 0, 0
	for _, it := range items {
		switch it.Status {
		case domain.ItemStatusPending:
			pending++
		case domain.ItemStatusSkipped:
			skipped++
			assert.NotEmpty(t, it.Error, "不可执行项持久化原因（不静默放行）")
		}
	}
	assert.Equal(t, 2, pending, "可执行项（aliyun+gw-1）计入分母")
	assert.Equal(t, 4, skipped, "不可执行项（三云+gw-2）标 skipped")

	// 分区汇总声明（Hard Rule：显式声明原因与出路）
	assert.True(t, hasWarning(list.Warnings, "不可执行项 4 项"), "分区汇总入 Warnings")
	assert.True(t, hasWarning(list.Warnings, "VM Nginx"))
}

// TestGenerateChangeList_K8sProbeUnavailable（AC-3）
// 探测通道未注入（nil）→ K8s 项按不可执行分区+声明；探测失败（如集群不可达）
// 不阻断清单生成，同样按不可执行项分区。
func TestGenerateChangeList_K8sProbeUnavailable(t *testing.T) {
	ctx := context.Background()

	t.Run("探测通道未注入", func(t *testing.T) {
		h := newGenHarness(t, nil)
		newCertID, _ := h.seedValid(t, k8sRef(changeTestFP, "c1", "team-a", "ALBConfig", "gw-1", "cert-k8s-1"))

		list, err := h.svc.GenerateChangeList(ctx, changeTestFP, newCertID)
		require.NoError(t, err)
		require.Len(t, list.Items, 1)
		assert.False(t, list.Items[0].AutoChangeable, "未探测不默认放行")
		assert.Contains(t, list.Items[0].Reason, "K8S_MANAGEMENT_UNPROBED")
		assert.True(t, hasWarning(list.Warnings, "管理权未探测"), "未接入显式声明")

		items, err := h.items.ListByOrder(ctx, list.OrderID)
		require.NoError(t, err)
		assert.Equal(t, domain.ItemStatusSkipped, items[0].Status)
	})

	t.Run("探测失败不阻断", func(t *testing.T) {
		h := newGenHarness(t, &fakeManagementProbe{err: errors.New("k8s unreachable")})
		newCertID, _ := h.seedValid(t,
			k8sRef(changeTestFP, "c1", "team-a", "ALBConfig", "gw-1", "cert-k8s-1"),
			cloudRef(changeTestFP, domain.CloudAliyun, domain.ProductCDN, "ak-1", "res-1", "cc-1"),
		)

		list, err := h.svc.GenerateChangeList(ctx, changeTestFP, newCertID)
		require.NoError(t, err, "单点探测失败不阻塞其他目标")
		byResource := make(map[string]ChangeListItem)
		for _, li := range list.Items {
			byResource[li.Target.ResourceID] = li
		}
		assert.False(t, byResource["gw-1"].AutoChangeable)
		assert.Contains(t, byResource["gw-1"].Reason, "K8S_MANAGEMENT_PROBE_FAILED")
		assert.True(t, byResource["res-1"].AutoChangeable)
	})
}

// ---------------------------------------------------------------------
// AC-4：盲区声明
// ---------------------------------------------------------------------

// TestGenerateChangeList_Warnings（AC-4）
// 覆盖边界（不含 VM Nginx 配置级引用）恒定声明；扫描通道失败 → blind_spot
// 盲区提示；coverageMeta total=-1 → "分母不可用"；无可执行分区时不输出分区汇总。
func TestGenerateChangeList_Warnings(t *testing.T) {
	ctx := context.Background()
	h := newGenHarness(t, &fakeManagementProbe{})
	meta := []domain.CoverageMeta{
		{Cloud: "aliyun", Product: "cdn", Covered: 2, Total: 5},
		{Cloud: "huawei", Product: "cdn", Covered: 1, Total: -1},
		{Cloud: "", Product: "crd", Covered: 3, Total: -1},
	}
	partials := []domain.ScanChannelFailure{
		{Cloud: "tencent", Product: "clb", Account: "acc-x", Reason: "cloud api rate limited"},
	}
	h.seedCert(t, changeTestFP, []string{"a.example.com"}, domain.HostingStatusComplete)
	newCertID := h.seedCert(t, newTestFP, []string{"a.example.com"}, domain.HostingStatusComplete)
	// 覆盖 meta/partials 的快照（2h 新鲜）
	snapID := h.seedDoneSnapshot(t, 2*time.Hour, meta, partials)
	h.seedRefs(t, snapID, cloudRef(changeTestFP, domain.CloudAliyun, domain.ProductCDN, "ak-1", "res-1", "cc-1"))

	list, err := h.svc.GenerateChangeList(ctx, changeTestFP, newCertID)
	require.NoError(t, err)

	assert.Contains(t, list.Warnings[0], "VM Nginx", "覆盖边界声明居首")
	assert.True(t, hasWarning(list.Warnings, "盲区：tencent/clb（acc-x）"), "通道失败盲区提示")
	assert.True(t, hasWarning(list.Warnings, "cloud api rate limited"))
	assert.True(t, hasWarning(list.Warnings, "分母不可用：huawei/cdn"), "total=-1 分母不可用")
	assert.True(t, hasWarning(list.Warnings, "分母不可用：/crd"), "K8s crd 分母恒不可用")
	assert.False(t, hasWarning(list.Warnings, "不可执行项"), "全部可执行时不输出分区汇总")
	assert.False(t, hasWarning(list.Warnings, "管理权未探测"), "无 K8s 引用不输出未探测声明")
}

// ---------------------------------------------------------------------
// 错误传播（仓储/配置错误经 %w 包装上抛）
// ---------------------------------------------------------------------

// failSnapshots 包装假快照仓储，LatestDone 注入错误。
type failSnapshots struct {
	*certtest.FakeScanSnapshotRepo
	latestDoneErr error
}

func (f *failSnapshots) LatestDone(ctx context.Context) (domain.ScanSnapshot, error) {
	if f.latestDoneErr != nil {
		return domain.ScanSnapshot{}, f.latestDoneErr
	}
	return f.FakeScanSnapshotRepo.LatestDone(ctx)
}

// failRefs 包装假引用仓储，ListBySnapshotID 注入错误。
type failRefs struct {
	*certtest.FakeCertReferenceRepo
	listErr error
}

func (f *failRefs) ListBySnapshotID(ctx context.Context, snapshotID string) ([]domain.CertReference, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.FakeCertReferenceRepo.ListBySnapshotID(ctx, snapshotID)
}

// failGenCerts 包装假台账仓储，GetByID/GetByFingerprint 注入错误。
type failGenCerts struct {
	*certtest.FakeCertificateRepo
	getByIDErr error
	getByFPErr error
}

func (f *failGenCerts) GetByID(ctx context.Context, id string) (domain.Certificate, error) {
	if f.getByIDErr != nil {
		return domain.Certificate{}, f.getByIDErr
	}
	return f.FakeCertificateRepo.GetByID(ctx, id)
}

func (f *failGenCerts) GetByFingerprint(ctx context.Context, fingerprint string) (domain.Certificate, error) {
	if f.getByFPErr != nil {
		return domain.Certificate{}, f.getByFPErr
	}
	return f.FakeCertificateRepo.GetByFingerprint(ctx, fingerprint)
}

// failGenOrders 包装假变更单仓储，GetByMutexToken/Create 注入错误。
type failGenOrders struct {
	*certtest.FakeChangeOrderRepo
	mutexErr  error
	createErr error
}

func (f *failGenOrders) GetByMutexToken(ctx context.Context, token string) (domain.ChangeOrder, error) {
	if f.mutexErr != nil {
		return domain.ChangeOrder{}, f.mutexErr
	}
	return f.FakeChangeOrderRepo.GetByMutexToken(ctx, token)
}

func (f *failGenOrders) Create(ctx context.Context, order *domain.ChangeOrder) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	return f.FakeChangeOrderRepo.Create(ctx, order)
}

// failGenItems 包装假变更项仓储，CreateMulti 注入错误。
type failGenItems struct {
	*certtest.FakeChangeItemRepo
	createErr error
}

func (f *failGenItems) CreateMulti(ctx context.Context, items []domain.ChangeItem) (int, error) {
	if f.createErr != nil {
		return 0, f.createErr
	}
	return f.FakeChangeItemRepo.CreateMulti(ctx, items)
}

// TestGenerateChangeList_ErrorPropagation 各仓储/配置错误经 %w 包装上抛；
// 旧证书未登记台账时 ErrNoDocuments 透传（发起更换的前提是台账在册）。
func TestGenerateChangeList_ErrorPropagation(t *testing.T) {
	ctx := context.Background()

	newSvc := func(h *genHarness, orders domain.ChangeOrderRepository, items domain.ChangeItemRepository,
		certs domain.CertificateRepository, snaps domain.ScanSnapshotRepository, refs domain.CertReferenceRepository,
		cfg domain.AlertConfigRepository) {
		h.svc = NewChangeService(orders, items, certs, cfg, snaps, refs, nil)
	}

	t.Run("快照读取失败", func(t *testing.T) {
		h := newGenHarness(t, &fakeManagementProbe{})
		newSvc(h, h.orders, h.items, h.certs, &failSnapshots{FakeScanSnapshotRepo: h.snapshots, latestDoneErr: errInjected}, h.refs, h.alertCfg)
		_, err := h.svc.GenerateChangeList(ctx, changeTestFP, "000000000000000000000000")
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("配置读取失败", func(t *testing.T) {
		h := newGenHarness(t, &fakeManagementProbe{})
		h.seedDoneSnapshot(t, time.Hour, nil, nil)
		newSvc(h, h.orders, h.items, h.certs, h.snapshots, h.refs, &failingAlertCfg{FakeAlertConfigRepo: h.alertCfg, getErr: errInjected})
		_, err := h.svc.GenerateChangeList(ctx, changeTestFP, "000000000000000000000000")
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("在途单预检查失败", func(t *testing.T) {
		h := newGenHarness(t, &fakeManagementProbe{})
		h.seedDoneSnapshot(t, time.Hour, nil, nil)
		newSvc(h, &failGenOrders{FakeChangeOrderRepo: h.orders, mutexErr: errInjected}, h.items, h.certs, h.snapshots, h.refs, h.alertCfg)
		_, err := h.svc.GenerateChangeList(ctx, changeTestFP, "000000000000000000000000")
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("新证书读取失败", func(t *testing.T) {
		h := newGenHarness(t, &fakeManagementProbe{})
		h.seedDoneSnapshot(t, time.Hour, nil, nil)
		newSvc(h, h.orders, h.items, &failGenCerts{FakeCertificateRepo: h.certs, getByIDErr: errInjected}, h.snapshots, h.refs, h.alertCfg)
		_, err := h.svc.GenerateChangeList(ctx, changeTestFP, "000000000000000000000000")
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("旧证书读取失败", func(t *testing.T) {
		h := newGenHarness(t, &fakeManagementProbe{})
		newCertID := h.seedCert(t, newTestFP, nil, domain.HostingStatusComplete)
		h.seedDoneSnapshot(t, time.Hour, nil, nil)
		newSvc(h, h.orders, h.items, &failGenCerts{FakeCertificateRepo: h.certs, getByFPErr: errInjected}, h.snapshots, h.refs, h.alertCfg)
		_, err := h.svc.GenerateChangeList(ctx, changeTestFP, newCertID)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("旧证书未登记", func(t *testing.T) {
		h := newGenHarness(t, &fakeManagementProbe{})
		newCertID := h.seedCert(t, newTestFP, []string{"a.example.com"}, domain.HostingStatusComplete)
		h.seedDoneSnapshot(t, time.Hour, nil, nil)
		_, err := h.svc.GenerateChangeList(ctx, changeTestFP, newCertID)
		assert.ErrorIs(t, err, mongo.ErrNoDocuments, "SAN 预检基准（旧证书 SAN）不可缺失")
	})

	t.Run("引用读取失败", func(t *testing.T) {
		h := newGenHarness(t, &fakeManagementProbe{})
		h.seedCert(t, changeTestFP, nil, domain.HostingStatusComplete)
		newCertID := h.seedCert(t, newTestFP, nil, domain.HostingStatusComplete)
		h.seedDoneSnapshot(t, time.Hour, nil, nil)
		newSvc(h, h.orders, h.items, h.certs, h.snapshots, &failRefs{FakeCertReferenceRepo: h.refs, listErr: errInjected}, h.alertCfg)
		_, err := h.svc.GenerateChangeList(ctx, changeTestFP, newCertID)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("订单写入失败", func(t *testing.T) {
		h := newGenHarness(t, &fakeManagementProbe{})
		newCertID, _ := h.seedValid(t)
		newSvc(h, &failGenOrders{FakeChangeOrderRepo: h.orders, createErr: errInjected}, h.items, h.certs, h.snapshots, h.refs, h.alertCfg)
		_, err := h.svc.GenerateChangeList(ctx, changeTestFP, newCertID)
		assert.ErrorIs(t, err, errInjected)
	})

	t.Run("变更项写入失败", func(t *testing.T) {
		h := newGenHarness(t, &fakeManagementProbe{})
		newCertID, _ := h.seedValid(t, cloudRef(changeTestFP, domain.CloudAliyun, domain.ProductCDN, "ak-1", "res-1", "cc-1"))
		newSvc(h, h.orders, &failGenItems{FakeChangeItemRepo: h.items, createErr: errInjected}, h.certs, h.snapshots, h.refs, h.alertCfg)
		_, err := h.svc.GenerateChangeList(ctx, changeTestFP, newCertID)
		assert.ErrorIs(t, err, errInjected)
	})
}
