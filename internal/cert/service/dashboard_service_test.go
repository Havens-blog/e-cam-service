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
	ledger := NewLedgerService(h.certs, h.refs, h.snaps)
	h.svc = NewDashboardService(h.certs, h.refs, h.snaps, h.probes, h.exempts, h.alertCfg, ledger, nil)
	return h
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

// hfp 互异指纹（服务层看板测试种子）。
func hfp(i int) string { return fmt.Sprintf("cc%04x%058x", i, i) }

// TestDashboard_CountsByLevelExclusiveBuckets 证书粒度互斥分桶。
func TestDashboard_CountsByLevelExclusiveBuckets(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	h.seedCert(t, hfp(1), []string{"a1.example.com"}, now.Add(100*24*time.Hour), domain.HostingStatusComplete)
	h.seedCert(t, hfp(2), []string{"a2.example.com"}, now.Add(20*24*time.Hour), domain.HostingStatusComplete)
	h.seedCert(t, hfp(3), []string{"a3.example.com"}, now.Add(10*24*time.Hour), domain.HostingStatusComplete)
	h.seedCert(t, hfp(4), []string{"a4.example.com"}, now.Add(5*24*time.Hour), domain.HostingStatusComplete)
	h.seedCert(t, hfp(5), []string{"a5.example.com"}, now.Add(-2*time.Hour), domain.HostingStatusComplete)

	view, err := h.svc.Dashboard(context.Background())
	require.NoError(t, err)
	assert.Equal(t, DashboardLevelCounts{1, 1, 1, 1, 1}, view.Summary.CountsByLevel)
}

// TestDashboard_DiffAlertOnlyRegularDiff 仅常规 diff 计差异（其余五态不计）。
func TestDashboard_DiffAlertOnlyRegularDiff(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	h.seedCert(t, hfp(1), []string{
		"d1.example.com", "d2.example.com", "d3.example.com",
		"d4.example.com", "d5.example.com", "d6.example.com",
	}, now.Add(100*24*time.Hour), domain.HostingStatusComplete)
	for name, status := range map[string]domain.ProbeStatus{
		"d1.example.com": domain.ProbeStatusDiff,
		"d2.example.com": domain.ProbeStatusChangeLinkedDiff,
		"d3.example.com": domain.ProbeStatusUnreachable,
		"d4.example.com": domain.ProbeStatusExempt,
		"d5.example.com": domain.ProbeStatusWildcardSkipped,
		"d6.example.com": domain.ProbeStatusConsistent,
	} {
		require.NoError(t, h.probes.Create(context.Background(), &domain.ProbeResult{Domain: name, Status: status}))
	}

	view, err := h.svc.Dashboard(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, view.Summary.DiffAlertCount)

	byDomain := map[string]domain.ProbeStatus{}
	for _, it := range view.Items {
		byDomain[it.Domain] = it.ProbeStatus
	}
	assert.Equal(t, domain.ProbeStatusChangeLinkedDiff, byDomain["d2.example.com"])
}

// TestDashboard_DiffCountsLatestPerDomain 同域多次探测仅取最新一次判定。
func TestDashboard_DiffCountsLatestPerDomain(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	h.seedCert(t, hfp(1), []string{"d1.example.com"}, now.Add(100*24*time.Hour), domain.HostingStatusComplete)
	// 先 diff 后 consistent（最新）→ 不计差异；probeStatus 取最新
	require.NoError(t, h.probes.Create(context.Background(), &domain.ProbeResult{
		Domain: "d1.example.com", Status: domain.ProbeStatusDiff, ProbeAt: now.Add(-2 * time.Hour),
	}))
	require.NoError(t, h.probes.Create(context.Background(), &domain.ProbeResult{
		Domain: "d1.example.com", Status: domain.ProbeStatusConsistent, ProbeAt: now.Add(-1 * time.Hour),
	}))

	view, err := h.svc.Dashboard(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, view.Summary.DiffAlertCount)
	require.Len(t, view.Items, 1)
	assert.Equal(t, domain.ProbeStatusConsistent, view.Items[0].ProbeStatus)
}

// TestDashboard_WildcardSkippedCount 无 override 的通配符 SAN 计 skip；
// 有 override 不计；override 指向子域名正常呈现。
func TestDashboard_WildcardSkippedCount(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	h.seedCert(t, hfp(1), []string{"*.skip.example.com", "*.probe.example.com"}, now.Add(100*24*time.Hour), domain.HostingStatusComplete)
	require.NoError(t, h.alertCfg.Save(context.Background(), &domain.AlertConfig{
		WildcardProbeOverrides: map[string]string{"*.probe.example.com": "concrete.probe.example.com"},
	}))

	view, err := h.svc.Dashboard(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, view.Summary.WildcardSkippedCount)
	require.Len(t, view.Items, 2, "通配符 SAN 本身仍为看板行")

	levels := map[string]DaysLeftTier{}
	for _, it := range view.Items {
		levels[it.Domain] = it.Level
	}
	assert.Equal(t, DaysLeftGT30, levels["*.skip.example.com"])
}

// TestDashboard_MultiCertDomainOwnership 同域名多证书并存：归属 notAfter 最新证书
// （daysLeft/hostingType/referencedClouds 随归属证）。
func TestDashboard_MultiCertDomainOwnership(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	// 旧证（fingerprint 小）快到期；新证（fingerprint 大）notAfter 更新 → 归属新证
	h.seedCert(t, hfp(1), []string{"shared.example.com"}, now.Add(5*24*time.Hour), domain.HostingStatusFingerprintOnly)
	h.seedCert(t, hfp(2), []string{"shared.example.com"}, now.Add(50*24*time.Hour), domain.HostingStatusComplete)

	view, err := h.svc.Dashboard(context.Background())
	require.NoError(t, err)
	require.Len(t, view.Items, 1)
	assert.Equal(t, domain.HostingStatusComplete, view.Items[0].HostingType)
	assert.Equal(t, DaysLeftGT30, view.Items[0].Level)
}

// TestDashboard_ReferencedClouds 归属证引用云去重（K8s 记 k8s）。
func TestDashboard_ReferencedClouds(t *testing.T) {
	h := newDashHarness(t)
	now := time.Now()
	h.seedCert(t, hfp(1), []string{"web.example.com"}, now.Add(40*24*time.Hour), domain.HostingStatusComplete)
	snapID, err := h.snaps.Create(context.Background(), &domain.ScanSnapshot{Status: domain.ScanStatusDone})
	require.NoError(t, err)
	require.NoError(t, h.snaps.MarkFinished(context.Background(), snapID, domain.ScanStatusDone, ""))
	_, err = h.refs.CreateMulti(context.Background(), []domain.CertReference{
		{CertFingerprint: hfp(1), Cloud: domain.CloudAliyun, Product: "cdn", ResourceID: "r1", SnapshotID: snapID},
		{CertFingerprint: hfp(1), Cloud: domain.CloudAliyun, Product: "waf", ResourceID: "r2", SnapshotID: snapID},
		{CertFingerprint: hfp(1), Cloud: "", Product: "crd", ClusterID: "c1", ResourceID: "gw", SnapshotID: snapID},
	})
	require.NoError(t, err)

	view, err := h.svc.Dashboard(context.Background())
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
	snapID, err := h.snaps.Create(context.Background(), &domain.ScanSnapshot{Status: domain.ScanStatusDone})
	require.NoError(t, err)
	require.NoError(t, h.snaps.MarkFinished(context.Background(), snapID, domain.ScanStatusDone, ""))
	_, err = h.refs.CreateMulti(context.Background(), []domain.CertReference{
		{CertFingerprint: hfp(9), Cloud: domain.CloudAliyun, Product: "cdn", ResourceID: "r9", SnapshotID: snapID},
	})
	require.NoError(t, err)
	require.NoError(t, h.exempts.Upsert(context.Background(), &domain.Exemption{Domain: "ex.example.com"}))

	view, err := h.svc.Dashboard(context.Background())
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

	view, err := h.withLastInspection(&stubLastInspection{at: at, ok: true}).Dashboard(context.Background())
	require.NoError(t, err)
	require.NotNil(t, view.LastInspectionAt)
	assert.WithinDuration(t, at, *view.LastInspectionAt, time.Second)

	view, err = h.withLastInspection(&stubLastInspection{}).Dashboard(context.Background())
	require.NoError(t, err)
	assert.Nil(t, view.LastInspectionAt)

	_, err = h.withLastInspection(&stubLastInspection{err: errors.New("boom")}).Dashboard(context.Background())
	require.Error(t, err)
}

// TestDashboard_NoDataEmptyShape 空台账：counts 全 0、items 空、rates 0。
func TestDashboard_NoDataEmptyShape(t *testing.T) {
	h := newDashHarness(t)
	view, err := h.svc.Dashboard(context.Background())
	require.NoError(t, err)
	assert.Equal(t, DashboardLevelCounts{}, view.Summary.CountsByLevel)
	assert.Empty(t, view.Items)
	assert.Zero(t, view.Summary.RegistrationRate)
}
