package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dashHarness 看板服务测试装置。
type dashHarness struct {
	svc            DashboardService
	ledger         LedgerService
	certs          *certtest.FakeCertificateRepo
	refs           *certtest.FakeCertReferenceRepo
	snaps          *certtest.FakeScanSnapshotRepo
	probes         *certtest.FakeProbeResultRepo
	exempts        *certtest.FakeExemptionRepo
	alertCfg       *certtest.FakeAlertConfigRepo
	lastInspection *stubLastInspection
}

// stubLastInspection 最近巡检来源端口假实现。
type stubLastInspection struct {
	at  time.Time
	ok  bool
	err error
}

func (s *stubLastInspection) LastInspectionAt(context.Context) (time.Time, bool, error) {
	return s.at, s.ok, s.err
}

// newDashHarness 构造看板测试装置（默认无巡检来源）。
func newDashHarness(t *testing.T) *dashHarness {
	t.Helper()
	h := &dashHarness{
		certs:    certtest.NewFakeCertificateRepo(),
		refs:     certtest.NewFakeCertReferenceRepo(),
		snaps:    certtest.NewFakeScanSnapshotRepo(),
		probes:   certtest.NewFakeProbeResultRepo(),
		exempts:  certtest.NewFakeExemptionRepo(),
		alertCfg: certtest.NewFakeAlertConfigRepo(),
	}
	h.rebuild()
	return h
}

// rebuild 以当前依赖重建服务（alertCfg 阈值变更后调用）。
func (h *dashHarness) rebuild() {
	h.ledger = NewLedgerService(h.certs, h.refs, h.snaps)
	var src LastInspectionSource
	if h.lastInspection != nil {
		src = h.lastInspection
	}
	h.svc = NewDashboardService(h.certs, h.refs, h.snaps, h.probes, h.exempts, h.alertCfg, h.ledger, src)
}

// withLastInspection 换接巡检来源后重建服务。
func (h *dashHarness) withLastInspection(src LastInspectionSource) DashboardService {
	ledger := NewLedgerService(h.certs, h.refs, h.snaps)
	return NewDashboardService(h.certs, h.refs, h.snaps, h.probes, h.exempts, h.alertCfg, ledger, src)
}

// seedCert 落一张台账证书。
func (h *dashHarness) seedCert(t *testing.T, fp string, sans []string, notAfter time.Time, hosting domain.HostingStatus) {
	t.Helper()
	require.NoError(t, h.certs.Create(context.Background(), &domain.Certificate{
		Fingerprint:   fp,
		CommonName:    sans[0],
		Sans:          sans,
		NotAfter:      notAfter,
		HostingStatus: hosting,
	}))
}

// seedFreshSnapshot 落一张最新成功快照（startedAt=now，新鲜度阈值内）。
func (h *dashHarness) seedFreshSnapshot(t *testing.T) string {
	t.Helper()
	return h.seedDoneSnapshotAt(t, time.Now())
}

// seedDoneSnapshotAt 落一张成功快照并显式指定 startedAt（新鲜度控制）。
func (h *dashHarness) seedDoneSnapshotAt(t *testing.T, startedAt time.Time) string {
	t.Helper()
	id, err := h.snaps.Create(context.Background(), &domain.ScanSnapshot{
		Status:    domain.ScanStatusDone,
		StartedAt: startedAt,
	})
	require.NoError(t, err)
	require.NoError(t, h.snaps.MarkFinished(context.Background(), id, domain.ScanStatusDone, ""))
	return id
}

// itemByFingerprint 按证书指纹取行。
func itemByFingerprint(items []DashboardItem, fp string) (DashboardItem, bool) {
	for _, it := range items {
		if it.Fingerprint == fp {
			return it, true
		}
	}
	return DashboardItem{}, false
}

// hfp 互异指纹（服务层看板测试种子）。
func hfp(i int) string { return fmt.Sprintf("cc%04x%058x", i, i) }

// ---------------------------------------------------------------------
// 证书粒度行（AC1）
// ---------------------------------------------------------------------

// TestDashboard_CertGranularRows 证书粒度主表：同域新旧两证同时成行、
// 独立分档，每行携带 referenceStatus；includeHidden=true 下 items 长度
// == 全部证书数。
func TestDashboard_CertGranularRows(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	// 同域两张证：旧证（fingerprint 小）已过期，新证 >30 天
	h.seedCert(t, hfp(1), []string{"shared.example.com"}, now.Add(-24*time.Hour), domain.HostingStatusFingerprintOnly)
	h.seedCert(t, hfp(2), []string{"shared.example.com"}, now.Add(50*24*time.Hour), domain.HostingStatusComplete)
	h.seedFreshSnapshot(t)

	view, err := h.svc.Dashboard(context.Background(), true)
	require.NoError(t, err)
	require.Len(t, view.Items, 2, "同域新旧两证同时成行（不再域名去重）")

	old, ok := itemByFingerprint(view.Items, hfp(1))
	require.True(t, ok)
	fresh, ok := itemByFingerprint(view.Items, hfp(2))
	require.True(t, ok)
	assert.Equal(t, DaysLeftExpired, old.Level, "旧证独立分档（不被新证掩盖）")
	assert.Equal(t, DaysLeftGT30, fresh.Level, "新证独立分档")
	assert.Equal(t, "shared.example.com", old.CommonName)
	assert.Equal(t, domain.HostingStatusFingerprintOnly, old.HostingType)
	assert.Equal(t, domain.HostingStatusComplete, fresh.HostingType)
	assert.NotEmpty(t, old.ReferenceStatus, "每行携带 referenceStatus")
	assert.NotEmpty(t, fresh.ReferenceStatus)
	// notAfter asc：旧证在前
	assert.Less(t, old.DaysLeft, fresh.DaysLeft)
}

// TestDashboard_IncludeHiddenEqualsAllCerts includeHidden=true 时 items 长度
// == 全部证书数（含多 SAN 证不重复成行、单证单行）。
func TestDashboard_IncludeHiddenEqualsAllCerts(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	h.seedCert(t, hfp(1), []string{"a.example.com", "b.example.com"}, now.Add(100*24*time.Hour), domain.HostingStatusComplete)
	h.seedCert(t, hfp(2), []string{"c.example.com"}, now.Add(3*24*time.Hour), domain.HostingStatusComplete)

	view, err := h.svc.Dashboard(context.Background(), true)
	require.NoError(t, err)
	assert.Len(t, view.Items, 2, "每证书一行：多 SAN 单证仍只一行")
}

// ---------------------------------------------------------------------
// 孤儿隐藏（AC2/AC3）
// ---------------------------------------------------------------------

// TestDashboard_OrphanHiddenByDefault 默认视图隐藏「过期且 no_refs_scanned」
// 孤儿；includeHidden=true 时以 hidden=true 行下发（referenceStatus 保留）。
func TestDashboard_OrphanHiddenByDefault(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	// 孤儿：过期、无任何引用（历史空 → no_refs_scanned）、快照新鲜
	h.seedCert(t, hfp(1), []string{"orphan.example.com"}, now.Add(-24*time.Hour), domain.HostingStatusComplete)
	// 非过期对照
	h.seedCert(t, hfp(2), []string{"live.example.com"}, now.Add(50*24*time.Hour), domain.HostingStatusComplete)
	h.seedFreshSnapshot(t)

	defView, err := h.svc.Dashboard(context.Background(), false)
	require.NoError(t, err)
	require.Len(t, defView.Items, 1, "默认视图不含孤儿行")
	assert.Equal(t, hfp(2), defView.Items[0].Fingerprint)
	assert.Equal(t, 1, defView.Summary.HiddenCount, "全局 hiddenCount")
	assert.Equal(t, 1, defView.Summary.CountsByLevel[levelIdxExpired].Hidden, "expired 档 hidden 分量")
	assert.Equal(t, 0, defView.Summary.CountsByLevel[levelIdxExpired].Visible)
	assert.Equal(t, 1, defView.Summary.CountsByLevel[levelIdxExpired].Total, "total=visible+hidden")

	fullView, err := h.svc.Dashboard(context.Background(), true)
	require.NoError(t, err)
	require.Len(t, fullView.Items, 2)
	orphan, ok := itemByFingerprint(fullView.Items, hfp(1))
	require.True(t, ok)
	assert.True(t, orphan.Hidden)
	assert.Equal(t, domain.RefStatusNoRefsScanned, orphan.ReferenceStatus)
	assert.False(t, defView.Items[0].Hidden)
}

// TestDashboard_HasRefsAndBlindSpotAlwaysVisible 风险信号恒可见：
// has_refs（快照计数>0）与 blind_spot（历史引用超出扫描范围、裸 refCount=0）
// 的过期证默认视图均可见——判定走 referenceStatus，禁用裸 refCount 谓词。
func TestDashboard_HasRefsAndBlindSpotAlwaysVisible(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	h.seedCert(t, hfp(1), []string{"hasrefs.example.com"}, now.Add(-24*time.Hour), domain.HostingStatusComplete)
	h.seedCert(t, hfp(2), []string{"blind.example.com"}, now.Add(-48*time.Hour), domain.HostingStatusComplete)
	h.seedCert(t, hfp(3), []string{"orphan.example.com"}, now.Add(-24*time.Hour), domain.HostingStatusComplete)
	snapID := h.seedFreshSnapshot(t)
	_, err := h.refs.CreateMulti(context.Background(), []domain.CertReference{
		// has_refs：快照内引用
		{CertFingerprint: hfp(1), Cloud: domain.CloudAliyun, Product: "cdn", ResourceID: "r1", SnapshotID: snapID},
		// blind_spot：历史引用（跨快照）的云/产品不在快照覆盖范围内 → refCount=0
		// 但状态为 blind_spot（裸 refCount=0 反例锁点）
		{CertFingerprint: hfp(2), Cloud: domain.CloudHuawei, Product: "waf", ResourceID: "r2", SnapshotID: "hist-snap"},
	})
	require.NoError(t, err)

	view, err := h.svc.Dashboard(context.Background(), false)
	require.NoError(t, err)
	fps := map[string]bool{}
	for _, it := range view.Items {
		fps[it.Fingerprint] = true
	}
	assert.True(t, fps[hfp(1)], "has_refs 过期证恒可见")
	assert.True(t, fps[hfp(2)], "blind_spot（refCount=0）过期证恒可见——不得用裸 refCount 判定")
	assert.False(t, fps[hfp(3)], "no_refs_scanned 过期证默认隐藏")

	blind, ok := itemByFingerprint(h.mustFull(t).Items, hfp(2))
	require.True(t, ok)
	assert.Equal(t, domain.RefStatusBlindSpot, blind.ReferenceStatus)
}

// mustFull includeHidden=true 视图快捷获取。
func (h *dashHarness) mustFull(t *testing.T) DashboardView {
	t.Helper()
	view, err := h.svc.Dashboard(context.Background(), true)
	require.NoError(t, err)
	return view
}

// TestDashboard_StaleSnapshotConservativeDisplay 快照超新鲜度阈值 → 降级保守
// 显示（不隐藏），并下发最近扫描时间。
func TestDashboard_StaleSnapshotConservativeDisplay(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	h.seedCert(t, hfp(1), []string{"orphan.example.com"}, now.Add(-24*time.Hour), domain.HostingStatusComplete)
	// 默认阈值 24h；快照 48h 前 → 陈旧
	staleAt := now.Add(-48 * time.Hour)
	h.seedDoneSnapshotAt(t, staleAt)

	view, err := h.svc.Dashboard(context.Background(), false)
	require.NoError(t, err)
	require.Len(t, view.Items, 1, "超阈值降级为保守显示（不隐藏）")
	assert.Equal(t, domain.RefStatusNoRefsScanned, view.Items[0].ReferenceStatus)
	assert.Equal(t, 0, view.Summary.HiddenCount)
	require.NotNil(t, view.Items[0].LastScanAt, "下发最近扫描时间")
	assert.WithinDuration(t, staleAt, *view.Items[0].LastScanAt, time.Second)
}

// TestDashboard_HideConsistentWithLedger 看板与台账三态判定单点一致性：
// 同一证书集合，看板 hiddenCount/可见集与台账 ListCerts 双口径一致
// （共享 deriveRefStatusFor，无看板侧另写判定）。
func TestDashboard_HideConsistentWithLedger(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	h.seedCert(t, hfp(1), []string{"orphan.example.com"}, now.Add(-24*time.Hour), domain.HostingStatusComplete)
	h.seedCert(t, hfp(2), []string{"hasrefs.example.com"}, now.Add(-48*time.Hour), domain.HostingStatusComplete)
	h.seedCert(t, hfp(3), []string{"blind.example.com"}, now.Add(-24*time.Hour), domain.HostingStatusComplete)
	snapID := h.seedFreshSnapshot(t)
	_, err := h.refs.CreateMulti(context.Background(), []domain.CertReference{
		{CertFingerprint: hfp(2), Cloud: domain.CloudAliyun, Product: "cdn", ResourceID: "r1", SnapshotID: snapID},
		{CertFingerprint: hfp(3), Cloud: domain.CloudHuawei, Product: "waf", ResourceID: "r2", SnapshotID: "hist-snap"},
	})
	require.NoError(t, err)

	dash, err := h.svc.Dashboard(context.Background(), false)
	require.NoError(t, err)
	ledger, err := h.ledger.ListCerts(context.Background(), ListCertsQuery{})
	require.NoError(t, err)

	assert.Equal(t, int64(dash.Summary.HiddenCount), ledger.HiddenCount, "hiddenCount 单点一致")
	assert.Equal(t, int(len(dash.Items)), int(ledger.Total), "可见行数单点一致")
}

// ---------------------------------------------------------------------
// countsByLevel 双口径（AC4）
// ---------------------------------------------------------------------

// TestDashboard_CountsSameSnapshot countsByLevel 与 items 同一快照收敛：
// includeHidden=true 下各档 total 之和 == items.length。
func TestDashboard_CountsSameSnapshot(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	h.seedCert(t, hfp(1), []string{"orphan.example.com"}, now.Add(-24*time.Hour), domain.HostingStatusComplete)
	h.seedCert(t, hfp(2), []string{"le7.example.com"}, now.Add(3*24*time.Hour), domain.HostingStatusComplete)
	h.seedCert(t, hfp(3), []string{"gt30.example.com"}, now.Add(100*24*time.Hour), domain.HostingStatusComplete)
	h.seedFreshSnapshot(t)

	view, err := h.svc.Dashboard(context.Background(), true)
	require.NoError(t, err)
	sum := 0
	for _, lc := range view.Summary.CountsByLevel {
		assert.Equal(t, lc.Total, lc.Visible+lc.Hidden, "每档 total=visible+hidden")
		sum += lc.Total
	}
	assert.Equal(t, len(view.Items), sum, "各档 total 之和 == items.length（同快照）")
}

// TestDashboard_CountsByLevelExclusiveBuckets 证书粒度互斥分桶（无快照全可见）。
func TestDashboard_CountsByLevelExclusiveBuckets(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	h.seedCert(t, hfp(1), []string{"a1.example.com"}, now.Add(100*24*time.Hour), domain.HostingStatusComplete)
	h.seedCert(t, hfp(2), []string{"a2.example.com"}, now.Add(20*24*time.Hour), domain.HostingStatusComplete)
	h.seedCert(t, hfp(3), []string{"a3.example.com"}, now.Add(10*24*time.Hour), domain.HostingStatusComplete)
	h.seedCert(t, hfp(4), []string{"a4.example.com"}, now.Add(5*24*time.Hour), domain.HostingStatusComplete)
	h.seedCert(t, hfp(5), []string{"a5.example.com"}, now.Add(-2*time.Hour), domain.HostingStatusComplete)

	view, err := h.svc.Dashboard(context.Background(), false)
	require.NoError(t, err)
	for i, want := range []int{1, 1, 1, 1, 1} {
		assert.Equal(t, want, view.Summary.CountsByLevel[i].Total)
		assert.Equal(t, want, view.Summary.CountsByLevel[i].Visible)
		assert.Equal(t, 0, view.Summary.CountsByLevel[i].Hidden)
	}
	assert.Equal(t, 0, view.Summary.HiddenCount)
}

// ---------------------------------------------------------------------
// 探测徽标聚合（AC5）
// ---------------------------------------------------------------------

// TestDashboard_ProbeBadgeWorstFirst 行徽标按最差优先序聚合：
// diff > change_linked_diff > unreachable > wildcard_skipped > exempt > 健康，
// 未探测 SAN 排序尾（并列同态不细分）。
func TestDashboard_ProbeBadgeWorstFirst(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	h.seedCert(t, hfp(1), []string{
		"m1.example.com", "m2.example.com", "m3.example.com",
		"m4.example.com", "m5.example.com", "m6.example.com",
	}, now.Add(100*24*time.Hour), domain.HostingStatusComplete)
	for name, status := range map[string]domain.ProbeStatus{
		"m1.example.com": domain.ProbeStatusDiff,
		"m2.example.com": domain.ProbeStatusChangeLinkedDiff,
		"m3.example.com": domain.ProbeStatusUnreachable,
		"m4.example.com": domain.ProbeStatusWildcardSkipped,
		"m5.example.com": domain.ProbeStatusExempt,
		"m6.example.com": domain.ProbeStatusConsistent,
	} {
		require.NoError(t, h.probes.Create(context.Background(), &domain.ProbeResult{Domain: name, Status: status}))
	}

	view, err := h.svc.Dashboard(context.Background(), false)
	require.NoError(t, err)
	require.Len(t, view.Items, 1)
	assert.Equal(t, domain.ProbeStatusDiff, view.Items[0].ProbeStatus, "最差优先：diff 胜出")
}

// TestDashboard_ProbeBadgeHealthAndUnprobed 全健康 → consistent；无探测 → 空串
// （未探测排序尾）；lastProbeAt 取 SAN 内最近时点。
func TestDashboard_ProbeBadgeHealthAndUnprobed(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	h.seedCert(t, hfp(1), []string{"ok1.example.com", "ok2.example.com"}, now.Add(100*24*time.Hour), domain.HostingStatusComplete)
	h.seedCert(t, hfp(2), []string{"noprobe.example.com"}, now.Add(100*24*time.Hour), domain.HostingStatusComplete)
	earlier := now.Add(-2 * time.Hour)
	later := now.Add(-1 * time.Hour)
	require.NoError(t, h.probes.Create(context.Background(), &domain.ProbeResult{
		Domain: "ok1.example.com", Status: domain.ProbeStatusConsistent, ProbeAt: earlier,
	}))
	require.NoError(t, h.probes.Create(context.Background(), &domain.ProbeResult{
		Domain: "ok2.example.com", Status: domain.ProbeStatusConsistent, ProbeAt: later,
	}))

	view, err := h.svc.Dashboard(context.Background(), false)
	require.NoError(t, err)
	require.Len(t, view.Items, 2)
	byFP := map[string]DashboardItem{}
	for _, it := range view.Items {
		byFP[it.Fingerprint] = it
	}
	assert.Equal(t, domain.ProbeStatusConsistent, byFP[hfp(1)].ProbeStatus, "并列同态不细分")
	require.NotNil(t, byFP[hfp(1)].LastProbeAt)
	assert.WithinDuration(t, later, *byFP[hfp(1)].LastProbeAt, time.Second, "lastProbeAt=SAN 内最近时点")
	assert.Empty(t, byFP[hfp(2)].ProbeStatus, "未探测=空串（排序尾）")
	assert.Nil(t, byFP[hfp(2)].LastProbeAt)
}

// TestDashboard_DiffCountsLatestPerDomain 同域多次探测仅取最新一次判定。
func TestDashboard_DiffCountsLatestPerDomain(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	h.seedCert(t, hfp(1), []string{"d1.example.com"}, now.Add(100*24*time.Hour), domain.HostingStatusComplete)
	// 先 diff 后 consistent（最新）→ 不计差异；行状态取最新
	require.NoError(t, h.probes.Create(context.Background(), &domain.ProbeResult{
		Domain: "d1.example.com", Status: domain.ProbeStatusDiff, ProbeAt: now.Add(-2 * time.Hour),
	}))
	require.NoError(t, h.probes.Create(context.Background(), &domain.ProbeResult{
		Domain: "d1.example.com", Status: domain.ProbeStatusConsistent, ProbeAt: now.Add(-1 * time.Hour),
	}))

	view, err := h.svc.Dashboard(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, 0, view.Summary.DiffAlertCount)
	require.Len(t, view.Items, 1)
	assert.Equal(t, domain.ProbeStatusConsistent, view.Items[0].ProbeStatus)
}

// ---------------------------------------------------------------------
// wildcardSkippedCount 域名去重（AC5）
// ---------------------------------------------------------------------

// TestDashboard_WildcardSkippedCountDomainDedup 共享通配符 SAN 的两证计数 1
// （按 domain 去重）；有 override 不计。
func TestDashboard_WildcardSkippedCountDomainDedup(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	// 两证共享 *.skip.example.com + 各自独立 wildcard
	h.seedCert(t, hfp(1), []string{"*.skip.example.com", "*.one.example.com"}, now.Add(100*24*time.Hour), domain.HostingStatusComplete)
	h.seedCert(t, hfp(2), []string{"*.skip.example.com", "*.two.example.com", "*.probe.example.com"}, now.Add(100*24*time.Hour), domain.HostingStatusComplete)
	require.NoError(t, h.alertCfg.Save(context.Background(), &domain.AlertConfig{
		WildcardProbeOverrides: map[string]string{"*.probe.example.com": "concrete.probe.example.com"},
	}))

	view, err := h.svc.Dashboard(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, 3, view.Summary.WildcardSkippedCount,
		"共享 *.skip 去重计 1 + *.one + *.two（*.probe 有 override 不计）")
	assert.Len(t, view.Items, 2, "通配符 SAN 所在证仍为看板行")
}

// ---------------------------------------------------------------------
// 排序（AC6）
// ---------------------------------------------------------------------

// TestDashboard_SortedNotAfterAscFingerprintTiebreak items 按 notAfter asc、
// 并列按证书指纹字典序稳定取序。
func TestDashboard_SortedNotAfterAscFingerprintTiebreak(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	same := now.Add(10 * 24 * time.Hour)
	h.seedCert(t, hfp(9), []string{"z.example.com"}, same, domain.HostingStatusComplete)
	h.seedCert(t, hfp(2), []string{"b.example.com"}, same, domain.HostingStatusComplete)
	h.seedCert(t, hfp(5), []string{"m.example.com"}, now.Add(40*24*time.Hour), domain.HostingStatusComplete)
	h.seedCert(t, hfp(1), []string{"expired.example.com"}, now.Add(-24*time.Hour), domain.HostingStatusComplete)
	h.seedFreshSnapshot(t)

	view, err := h.svc.Dashboard(context.Background(), true)
	require.NoError(t, err)
	require.Len(t, view.Items, 4)
	got := []string{}
	for _, it := range view.Items {
		got = append(got, it.Fingerprint)
	}
	// notAfter asc：expired(hfp1) → 10d 并列按指纹字典序 hfp2 < hfp9 → 40d(hfp5)
	assert.Equal(t, []string{hfp(1), hfp(2), hfp(9), hfp(5)}, got)
}

// ---------------------------------------------------------------------
// 既有口径回归
// ---------------------------------------------------------------------

// TestDashboard_ReferencedClouds 归属证引用云去重（K8s 记 k8s）。
func TestDashboard_ReferencedClouds(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	h.seedCert(t, hfp(1), []string{"web.example.com"}, now.Add(40*24*time.Hour), domain.HostingStatusComplete)
	snapID := h.seedFreshSnapshot(t)
	_, err := h.refs.CreateMulti(context.Background(), []domain.CertReference{
		{CertFingerprint: hfp(1), Cloud: domain.CloudAliyun, Product: "cdn", ResourceID: "r1", SnapshotID: snapID},
		{CertFingerprint: hfp(1), Cloud: domain.CloudAliyun, Product: "waf", ResourceID: "r2", SnapshotID: snapID},
		{CertFingerprint: hfp(1), Cloud: "", Product: "crd", ClusterID: "c1", ResourceID: "gw", SnapshotID: snapID},
	})
	require.NoError(t, err)

	view, err := h.svc.Dashboard(context.Background(), false)
	require.NoError(t, err)
	require.Len(t, view.Items, 1)
	assert.Equal(t, []string{"aliyun", "k8s"}, view.Items[0].ReferencedClouds)
}

// TestDashboard_ExemptCountAndRates 豁免计数与三 rate 口径（同 Stats）。
func TestDashboard_ExemptCountAndRates(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	h.seedCert(t, hfp(1), []string{"a.example.com"}, now.Add(100*24*time.Hour), domain.HostingStatusComplete)
	h.seedCert(t, hfp(2), []string{"b.example.com"}, now.Add(100*24*time.Hour), domain.HostingStatusFingerprintOnly)
	// 扫描缺口：未登记指纹 → 分母 3
	snapID := h.seedFreshSnapshot(t)
	_, err := h.refs.CreateMulti(context.Background(), []domain.CertReference{
		{CertFingerprint: hfp(9), Cloud: domain.CloudAliyun, Product: "cdn", ResourceID: "r9", SnapshotID: snapID},
	})
	require.NoError(t, err)
	require.NoError(t, h.exempts.Upsert(context.Background(), &domain.Exemption{Domain: "ex.example.com"}))

	view, err := h.svc.Dashboard(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, 1, view.Summary.ExemptCount)
	// ratio 万分位四舍五入（同 Stats 展示口径）
	assert.InDelta(t, 2.0/3.0, view.Summary.RegistrationRate, 0.0001)
	assert.InDelta(t, 1.0/3.0, view.Summary.ReplaceableRate, 0.0001)
	assert.InDelta(t, 0.5, view.Summary.FingerprintOnlyRate, 1e-9)
}

// TestDashboard_LastInspectionSource 巡检来源接线：ok=true 携值；ok=false 为 nil；
// 来源错误向上传播。
func TestDashboard_LastInspectionSource(t *testing.T) {
	h := newDashHarness(t)
	at := time.Now().Add(-3 * time.Hour)

	view, err := h.withLastInspection(&stubLastInspection{at: at, ok: true}).Dashboard(context.Background(), false)
	require.NoError(t, err)
	require.NotNil(t, view.LastInspectionAt)
	assert.WithinDuration(t, at, *view.LastInspectionAt, time.Second)

	view, err = h.withLastInspection(&stubLastInspection{}).Dashboard(context.Background(), false)
	require.NoError(t, err)
	assert.Nil(t, view.LastInspectionAt)

	_, err = h.withLastInspection(&stubLastInspection{err: errors.New("boom")}).Dashboard(context.Background(), false)
	require.Error(t, err)
}

// TestDashboard_NoDataEmptyShape 空台账：counts 全 0、items 空、rates 0。
func TestDashboard_NoDataEmptyShape(t *testing.T) {
	h := newDashHarness(t)
	view, err := h.svc.Dashboard(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, DashboardLevelCounts{}, view.Summary.CountsByLevel)
	assert.Empty(t, view.Items)
	assert.Equal(t, 0, view.Summary.HiddenCount)
	assert.Zero(t, view.Summary.RegistrationRate)
}
