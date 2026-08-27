package scheduler

import (
	"context"
	"crypto/x509"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------
// 测试基础设施：注入时钟 + fake ProbeService（4.1 mock）+ 真实 4.2 引擎
// ---------------------------------------------------------------------

// jobBase 注入时钟基准（固定时点，避免挂钟抖动影响 lastInspectionAt 断言）。
// 注意：台账 notAfter 采用真实时钟相对值（4.2 引擎 now 不可注入），与 jobBase 解耦。
var jobBase = time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

// relNow 真实时钟相对时点（4.2 引擎与 2.1 解析使用真实 time.Now）。
func relNow(d time.Duration) time.Time { return time.Now().Add(d) }

// orderLog 跨 fake 记录子步调用顺序（步骤序断言）。
type orderLog struct {
	mu      sync.Mutex
	entries []string
}

func (o *orderLog) add(name string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.entries = append(o.entries, name)
}

func (o *orderLog) snapshot() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.entries...)
}

// fakeProbeService 4.1 ProbeService mock：目标域推导与真实实现同口径
// （台账全部 sans 展开去重），结果/错误可预置；block/entered 支撑并发守卫测试。
type fakeProbeService struct {
	certs   domain.CertificateRepository
	order   *orderLog
	results []domain.ProbeResult
	err     error
	block   chan struct{} // 非 nil 时进入后阻塞至关闭
	entered chan struct{} // 非 nil 时进入后发信号（并发守卫测试同步点）

	mu      sync.Mutex
	calls   int
	targets []string
}

func (f *fakeProbeService) ProbeDomains(_ context.Context, _ []string) ([]domain.ProbeResult, error) {
	return append([]domain.ProbeResult(nil), f.results...), f.err
}

// ProbeAllTenantDNS 模拟 DNS 源未装配：返回 ErrNoDNSSource，触发 stepProbe 回退
// ProbeLedgerDomains（与生产 dnsSource=nil 行为一致；测试 fake 不接入 DNS 源）。
func (f *fakeProbeService) ProbeAllTenantDNS(_ context.Context) ([]domain.ProbeResult, error) {
	return nil, service.ErrNoDNSSource
}

// ProbeTenantDNS 单租户 DNS 探测（fake 不接入 DNS 源，返回 ErrNoDNSSource）。
func (f *fakeProbeService) ProbeTenantDNS(_ context.Context, _ int64) ([]domain.ProbeResult, error) {
	return nil, service.ErrNoDNSSource
}

// TriggerProbeAsync fake：不接入 DNS 源，返回 ErrNoDNSSource（与生产 dnsSource=nil 一致）。
func (f *fakeProbeService) TriggerProbeAsync(_ context.Context) error {
	return service.ErrNoDNSSource
}

// ProbeLedgerDomains 模拟真实 4.1：目标 = 台账全部 sans 展开去重。
func (f *fakeProbeService) ProbeLedgerDomains(ctx context.Context) ([]domain.ProbeResult, error) {
	if f.order != nil {
		f.order.add("probe")
	}
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.block != nil {
		<-f.block
	}
	certs, err := f.certs.List(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	targets := make([]string, 0, len(certs)*2)
	for _, c := range certs {
		for _, san := range c.Sans {
			name := strings.TrimSpace(san)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			targets = append(targets, name)
		}
	}
	f.mu.Lock()
	f.targets = targets
	f.mu.Unlock()
	return append([]domain.ProbeResult(nil), f.results...), f.err
}

func (f *fakeProbeService) probeCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeProbeService) probedTargets() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.targets...)
}

// recordingInspectionService 包装真实 4.2 引擎：记录子步调用顺序后委托执行
// （保持分级/去重为真实行为，同时可断言"到期分级先于探测"）。
type recordingInspectionService struct {
	inner service.InspectionService
	order *orderLog
}

func (r *recordingInspectionService) InspectLedger(ctx context.Context) (service.InspectionSummary, error) {
	r.order.add("expiry")
	return r.inner.InspectLedger(ctx)
}

// jobHarness 巡检 Job 测试装置：真实 4.2 引擎 + fake 4.1 探测 + 内存发布器/运行记录。
type jobHarness struct {
	certs   *certtest.FakeCertificateRepo
	exempts *certtest.FakeExemptionRepo
	alert   *certtest.FakeAlertConfigRepo
	pub     *service.InMemoryAlertPublisher
	expiry  *recordingInspectionService
	probe   *fakeProbeService
	store   *MemoryInspectionRunStore
	job     *InspectionJob
	order   *orderLog
	now     time.Time
	seq     int
}

func newJobHarness(t *testing.T) *jobHarness {
	t.Helper()
	certs := certtest.NewFakeCertificateRepo()
	exempts := certtest.NewFakeExemptionRepo()
	alert := certtest.NewFakeAlertConfigRepo()
	pub := service.NewInMemoryAlertPublisher()
	ol := &orderLog{}
	probe := &fakeProbeService{certs: certs, order: ol}
	expiry := &recordingInspectionService{
		inner: service.NewInspectionService(certs, alert, pub),
		order: ol,
	}
	store := NewMemoryInspectionRunStore()
	job := NewInspectionJob(certs, exempts, expiry, probe, pub, store)
	h := &jobHarness{
		certs:   certs,
		exempts: exempts,
		alert:   alert,
		pub:     pub,
		expiry:  expiry,
		probe:   probe,
		store:   store,
		job:     job,
		order:   ol,
		now:     jobBase,
	}
	job.now = func() time.Time { return h.now }
	return h
}

// fp 生成唯一 64-hex 测试指纹（非 bundle 证书用）。
func (h *jobHarness) fp() string {
	h.seq++
	return fmt.Sprintf("%064x", h.seq)
}

// seed 写入一张台账证书（CertPEM 为导入时存储的证书束原文）。
func (h *jobHarness) seed(t *testing.T, fp, cn, certPEM string, sans []string, notAfter time.Time) {
	t.Helper()
	require.NoError(t, h.certs.Create(context.Background(), &domain.Certificate{
		Fingerprint:   fp,
		CommonName:    cn,
		Sans:          sans,
		NotAfter:      notAfter,
		HostingStatus: domain.HostingStatusComplete,
		CertPEM:       certPEM,
	}))
}

// seedBundle 写入一张现场生成完整链证书。
func (h *jobHarness) seedBundle(t *testing.T, b *certtest.CertBundle, sans []string, notAfter time.Time) {
	t.Helper()
	h.seed(t, b.Fingerprint, b.CN, string(b.CertPEM), sans, notAfter)
}

// seedExempt 登记探测豁免域名。
func (h *jobHarness) seedExempt(t *testing.T, domainName string) {
	t.Helper()
	require.NoError(t, h.exempts.Upsert(context.Background(), &domain.Exemption{Domain: domainName}))
}

// events 按类别过滤已发布事件。
func (h *jobHarness) events(cat service.AlertCategory) []service.CertAlertEvent {
	var out []service.CertAlertEvent
	for _, e := range h.pub.Events() {
		if e.Category == cat {
			out = append(out, e)
		}
	}
	return out
}

// stepByName 从运行记录取指定子步指标。
func stepByName(t *testing.T, run InspectionRun, name string) StepMetrics {
	t.Helper()
	for _, s := range run.Steps {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("step %q not present in run: %+v", name, run.Steps)
	return StepMetrics{}
}

// day 天单位（4.2 daysLeft 口径）。
const day = 24 * time.Hour

// ---------------------------------------------------------------------
// AC1/AC3/AC5：完整一轮巡检（mock 台帐+探测）四步顺序、全量探测、告警、记录
// ---------------------------------------------------------------------

// TestRunInspection_FullRoundIntegration 完整一轮：四步依序执行（到期先于探测）、
// probe 目标覆盖台账全部 sans、豁免域探测记 exempt 不告警、diff 域发 tls_diff、
// 到期升级告警一次、lastInspectionAt 更新。
func TestRunInspection_FullRoundIntegration(t *testing.T) {
	h := newJobHarness(t)

	// 主证（远期，sans 含豁免域与差异域）+ 临期证（25d → L30 首次触发）
	main := certtest.NewBundle(t, "main.example.com", []string{"main.example.com", "exempt.example.com", "diff.example.com"}, nil)
	near := certtest.NewBundle(t, "near.example.com", []string{"near.example.com"}, nil)
	mainSans := []string{"main.example.com", "exempt.example.com", "diff.example.com"}
	h.seedBundle(t, main, mainSans, relNow(200*day))
	h.seedBundle(t, near, []string{"near.example.com"}, relNow(25*day))
	h.seedExempt(t, "exempt.example.com")

	// fake 探测结果：主证域 consistent / 豁免 exempt / 差异 diff / 临期 consistent
	h.probe.results = []domain.ProbeResult{
		{Domain: "main.example.com", Status: domain.ProbeStatusConsistent},
		{Domain: "exempt.example.com", Status: domain.ProbeStatusExempt},
		{Domain: "diff.example.com", Status: domain.ProbeStatusDiff, OnlineFingerprint: "ff00"},
		{Domain: "near.example.com", Status: domain.ProbeStatusConsistent},
	}

	run, err := h.job.RunInspection(context.Background())
	require.NoError(t, err)

	// 四步依序（AC1）
	names := make([]string, 0, len(run.Steps))
	for _, s := range run.Steps {
		names = append(names, s.Name)
	}
	assert.Equal(t, []string{StepIntegrity, StepExpiry, StepProbe, StepExemption}, names)
	assert.True(t, run.Ok(), "全部子步完成")
	assert.Equal(t, h.now, run.At, "运行时点取注入时钟")

	// 完整性复检：两证均可解析/未过期/链完整
	integrity := stepByName(t, run, StepIntegrity)
	assert.True(t, integrity.Ok)
	assert.Equal(t, 2, integrity.Total)
	assert.Equal(t, 0, integrity.Failed)

	// 到期分级：2 证计级、临期证 L30 升级触发一次（真实 4.2 引擎）
	expiry := stepByName(t, run, StepExpiry)
	assert.True(t, expiry.Ok)
	assert.Equal(t, 2, expiry.Total)
	assert.Equal(t, 1, expiry.Extra["triggered"])

	// 探测调度：fake 以台账全部 sans 为目标（AC5 probe 全量域名）
	probe := stepByName(t, run, StepProbe)
	assert.True(t, probe.Ok)
	assert.Equal(t, 4, probe.Total)
	targets := h.probe.probedTargets()
	sort.Strings(targets)
	assert.Equal(t, []string{"diff.example.com", "exempt.example.com", "main.example.com", "near.example.com"}, targets)
	assert.Equal(t, []string{"expiry", "probe"}, h.order.snapshot(), "到期分级先于探测执行")

	// 豁免过滤：豁免域探测记 exempt 不告警；唯一 diff 域告警一次（AC1/AC5）
	exemption := stepByName(t, run, StepExemption)
	assert.True(t, exemption.Ok)
	assert.Equal(t, 1, exemption.Extra["exemptProbed"])
	assert.Equal(t, 1, exemption.Extra["diffAlerted"])

	// 事件面：到期 L30 ×1 + tls_diff ×1；无 ops 异常事件
	expiryEvents := h.events(service.AlertCategoryExpiry)
	require.Len(t, expiryEvents, 1)
	assert.Equal(t, domain.ExpiryAlertL30, expiryEvents[0].Level)
	diffEvents := h.events(service.AlertCategoryTLSDiff)
	require.Len(t, diffEvents, 1)
	assert.Equal(t, "diff.example.com", diffEvents[0].Domain)
	assert.Empty(t, h.events(service.AlertCategoryOps))

	// lastInspectionAt 更新（AC3/AC5）
	at, ok, err := h.store.LastInspectionAt(context.Background())
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, h.now, at)
}

// TestRunInspection_IdempotentSameDayNoDuplicateExpiryAlert 幂等（AC4）：
// 同日重复执行整轮巡检——到期告警只发一次（依赖 4.2 升级去重）；
// lastInspectionAt 更新为最近一轮时点。
func TestRunInspection_IdempotentSameDayNoDuplicateExpiryAlert(t *testing.T) {
	h := newJobHarness(t)
	near := certtest.NewBundle(t, "near.example.com", []string{"near.example.com"}, nil)
	h.seedBundle(t, near, []string{"near.example.com"}, relNow(25*day))
	h.probe.results = []domain.ProbeResult{
		{Domain: "near.example.com", Status: domain.ProbeStatusConsistent},
	}

	_, err := h.job.RunInspection(context.Background())
	require.NoError(t, err)
	require.Len(t, h.events(service.AlertCategoryExpiry), 1)

	// 同日第二轮（推进 1h，仍为同一天）
	h.now = h.now.Add(1 * time.Hour)
	run, err := h.job.RunInspection(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, stepByName(t, run, StepExpiry).Extra["triggered"], "同级不得重复触发")
	assert.Len(t, h.events(service.AlertCategoryExpiry), 1, "同日两轮到期告警只发一次")
	assert.Empty(t, h.events(service.AlertCategoryTLSDiff), "无 diff 结果不产生差异告警")

	// lastInspectionAt 为第二轮时点
	at, ok, err := h.store.LastInspectionAt(context.Background())
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, h.now, at)
}

// ---------------------------------------------------------------------
// AC2：完整性复检——复跑 2.1 校验（可解析/未过期/链完整），异常项产 ops 事件
// ---------------------------------------------------------------------

// TestRunInspection_IntegrityAnomaliesEmitOpsEvents 四类异常（PEM 损坏/已过期/
// 链缺失/指纹不一致）各产一条 ops 异常事件；健康证不产事件；异常不阻塞分级与探测。
func TestRunInspection_IntegrityAnomaliesEmitOpsEvents(t *testing.T) {
	h := newJobHarness(t)

	healthy := certtest.NewBundle(t, "healthy.example.com", []string{"healthy.example.com"}, nil)
	h.seedBundle(t, healthy, []string{"healthy.example.com"}, relNow(200*day))

	corrupt := certtest.NewBundle(t, "corrupt.example.com", []string{"corrupt.example.com"}, nil)
	h.seed(t, corrupt.Fingerprint, corrupt.CN, "not a pem certificate", []string{"corrupt.example.com"}, relNow(200*day))

	// 已过期：存储 PEM 本身过期（mutate NotBefore/NotAfter），完整性复检命中"未过期"校验
	expired := certtest.NewBundle(t, "expired.example.com", []string{"expired.example.com"}, func(c *x509.Certificate) {
		c.NotBefore = time.Now().Add(-48 * time.Hour)
		c.NotAfter = time.Now().Add(-time.Hour)
	})
	h.seedBundle(t, expired, []string{"expired.example.com"}, relNow(-day))

	leafOnly := certtest.NewBundle(t, "leaf.example.com", []string{"leaf.example.com"}, nil)
	h.seed(t, leafOnly.Fingerprint, leafOnly.CN, string(leafOnly.LeafOnlyPEM()), []string{"leaf.example.com"}, relNow(200*day))

	fpMismatch := certtest.NewBundle(t, "mismatch.example.com", []string{"mismatch.example.com"}, nil)
	h.seed(t, h.fp(), "mismatch.example.com", string(fpMismatch.CertPEM), []string{"mismatch.example.com"}, relNow(200*day))

	run, err := h.job.RunInspection(context.Background())
	require.NoError(t, err, "异常项不产生步骤级错误，整轮照常完成")

	integrity := stepByName(t, run, StepIntegrity)
	assert.True(t, integrity.Ok)
	assert.Equal(t, 5, integrity.Total)
	assert.Equal(t, 4, integrity.Failed, "corrupt/expired/leafOnly/fpMismatch 四项异常")

	opsEvents := h.events(service.AlertCategoryOps)
	require.Len(t, opsEvents, 4)
	fps := map[string]bool{}
	for _, e := range opsEvents {
		fps[e.Fingerprint] = true
		assert.NotEmpty(t, e.Detail)
	}
	assert.True(t, fps[corrupt.Fingerprint])
	assert.True(t, fps[expired.Fingerprint])
	assert.True(t, fps[leafOnly.Fingerprint])
	assert.False(t, fps[healthy.Fingerprint], "健康证不产异常事件")

	// 不阻塞：分级与探测照常执行（AC2 处置策略）
	assert.True(t, stepByName(t, run, StepExpiry).Ok)
	assert.Equal(t, 1, h.probe.probeCalls(), "探测仍执行")
}

// ---------------------------------------------------------------------
// AC1 步骤四：豁免过滤——exempt 不告警，仅常规 diff 发 tls_diff
// ---------------------------------------------------------------------

// TestRunInspection_ExemptionFilterSkipsAlertsForNonDiff 六态过滤：
// 仅 diff 发 tls_diff；exempt/unreachable/wildcard_skipped/change_linked_diff
// 均不告警（unreachable 不参与差异告警；change_linked 由 5.10 验证窗口通道触达）。
func TestRunInspection_ExemptionFilterSkipsAlertsForNonDiff(t *testing.T) {
	h := newJobHarness(t)
	sans := []string{
		"ok.example.com", "exempt.example.com", "diff.example.com",
		"unreachable.example.com", "wild.example.com", "linked.example.com",
	}
	far := certtest.NewBundle(t, "far.example.com", sans, nil)
	h.seedBundle(t, far, sans, relNow(200*day))
	h.seedExempt(t, "exempt.example.com")
	h.seedExempt(t, "unreachable.example.com")

	h.probe.results = []domain.ProbeResult{
		{Domain: "ok.example.com", Status: domain.ProbeStatusConsistent},
		{Domain: "exempt.example.com", Status: domain.ProbeStatusExempt},
		{Domain: "diff.example.com", Status: domain.ProbeStatusDiff, OnlineFingerprint: "aa"},
		{Domain: "unreachable.example.com", Status: domain.ProbeStatusUnreachable},
		{Domain: "wild.example.com", Status: domain.ProbeStatusWildcardSkipped},
		{Domain: "linked.example.com", Status: domain.ProbeStatusChangeLinkedDiff, ChangeOrderID: "507f1f77bcf86cd799439011"},
	}

	run, err := h.job.RunInspection(context.Background())
	require.NoError(t, err)

	exemption := stepByName(t, run, StepExemption)
	assert.True(t, exemption.Ok)
	assert.Equal(t, 6, exemption.Total)
	assert.Equal(t, 1, exemption.Extra["exemptProbed"])
	assert.Equal(t, 1, exemption.Extra["diffAlerted"])
	assert.Equal(t, 1, exemption.Extra["exemptUnreachable"], "豁免域拨测失败记 unreachable（真可达性）")

	diffEvents := h.events(service.AlertCategoryTLSDiff)
	require.Len(t, diffEvents, 1)
	assert.Equal(t, "diff.example.com", diffEvents[0].Domain)
	assert.Empty(t, h.events(service.AlertCategoryOps))
	assert.Empty(t, h.events(service.AlertCategoryExpiry), "远期证不触发到期告警")
}

// ---------------------------------------------------------------------
// AC4：单项失败不中断整轮；错误聚合返回；运行记录仍落
// ---------------------------------------------------------------------

// expiryFailCertRepo 包装台账假实现：指定指纹的 UpdateExpiryAlertLevel 注入失败
// （4.2 at-least-once：发布成功、状态未落 → 下轮重发）。
type expiryFailCertRepo struct {
	*certtest.FakeCertificateRepo
	failFP string
}

func (r *expiryFailCertRepo) UpdateExpiryAlertLevel(ctx context.Context, fp string, level domain.ExpiryAlertLevel) error {
	if fp == r.failFP {
		return fmt.Errorf("injected persist failure for %s", fp)
	}
	return r.FakeCertificateRepo.UpdateExpiryAlertLevel(ctx, fp, level)
}

// TestRunInspection_ItemFailuresIsolated 单证/单步失败不中断整轮：
// 完整性异常（ops 处置不阻塞）+ 到期状态落库失败 + 探测写库失败聚合返回；
// 四步全部执行、豁免过滤照常、lastInspectionAt 仍记录。
func TestRunInspection_ItemFailuresIsolated(t *testing.T) {
	h := newJobHarness(t)

	corrupt := certtest.NewBundle(t, "corrupt.example.com", []string{"corrupt.example.com"}, nil)
	near := certtest.NewBundle(t, "near.example.com", []string{"near.example.com"}, nil)

	// 以失败注入仓储重建 job（完整性读取与到期状态写同仓储）
	repo := &expiryFailCertRepo{FakeCertificateRepo: h.certs, failFP: near.Fingerprint}
	expiry := service.NewInspectionService(repo, h.alert, h.pub)
	job := NewInspectionJob(repo, h.exempts, expiry, h.probe, h.pub, h.store)
	fixedNow := h.now
	job.now = func() time.Time { return fixedNow }

	require.NoError(t, h.certs.Create(context.Background(), &domain.Certificate{
		Fingerprint:   corrupt.Fingerprint,
		CommonName:    corrupt.CN,
		Sans:          []string{"corrupt.example.com"},
		NotAfter:      relNow(200 * day),
		HostingStatus: domain.HostingStatusComplete,
		CertPEM:       "broken pem",
		EncryptedPrivateKey: &domain.EncryptedSecret{
			Ciphertext: "LEAK-MARKER-CIPHERTEXT",
			KeyVersion: 1,
			Algo:       "AES-256-GCM",
		},
	}))
	h.seedBundle(t, near, []string{"near.example.com"}, relNow(25*day))

	h.probe.err = fmt.Errorf("probe: persist result injected failure")

	run, err := job.RunInspection(context.Background())
	require.Error(t, err, "失败聚合返回")
	assert.Contains(t, err.Error(), "injected persist failure")
	assert.Contains(t, err.Error(), "persist result injected failure")
	assert.False(t, run.Ok())

	// 四步全部执行；单项失败仅影响所在子步指标
	integrity := stepByName(t, run, StepIntegrity)
	assert.True(t, integrity.Ok)
	assert.Equal(t, 2, integrity.Total)
	assert.Equal(t, 1, integrity.Failed)
	assert.False(t, stepByName(t, run, StepExpiry).Ok, "到期状态落库失败 → 步骤级失败")
	assert.False(t, stepByName(t, run, StepProbe).Ok, "探测写库失败 → 步骤级失败")
	assert.True(t, stepByName(t, run, StepExemption).Ok, "空结果集豁免过滤照常")

	// at-least-once：到期事件已发布（状态未落）；完整性 ops 事件已发布且不含密文片段
	assert.Len(t, h.events(service.AlertCategoryExpiry), 1)
	opsEvents := h.events(service.AlertCategoryOps)
	require.Len(t, opsEvents, 1)
	assert.NotContains(t, fmt.Sprintf("%+v", opsEvents[0]), "LEAK-MARKER")

	// 运行记录仍落（平台自身监控可见失败）
	at, ok, err := h.store.LastInspectionAt(context.Background())
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, fixedNow, at)
}

// ---------------------------------------------------------------------
// 幂等守卫：同进程单飞，并发第二轮直接拒绝
// ---------------------------------------------------------------------

// TestRunInspection_ConcurrentSecondRunRejected 单飞守卫：第一轮执行中并发触发
// 返回 ErrInspectionInProgress；结束后守卫释放可再次执行。
func TestRunInspection_ConcurrentSecondRunRejected(t *testing.T) {
	h := newJobHarness(t)
	far := certtest.NewBundle(t, "far.example.com", []string{"far.example.com"}, nil)
	h.seedBundle(t, far, []string{"far.example.com"}, relNow(200*day))

	block := make(chan struct{})
	entered := make(chan struct{}, 1)
	h.probe.block = block
	h.probe.entered = entered

	done := make(chan error, 1)
	go func() {
		_, err := h.job.RunInspection(context.Background())
		done <- err
	}()
	<-entered // 等第一轮进入探测步

	_, err := h.job.RunInspection(context.Background())
	require.ErrorIs(t, err, ErrInspectionInProgress)
	_, ok, _ := h.store.LastInspectionAt(context.Background())
	assert.False(t, ok, "被拒绝的一轮不产生运行记录")

	close(block)
	require.NoError(t, <-done)

	// 守卫释放后可再次执行
	h.probe.block = nil
	_, err = h.job.RunInspection(context.Background())
	require.NoError(t, err)
}

// ---------------------------------------------------------------------
// AC3：运行记录存储与成功率指标
// ---------------------------------------------------------------------

// TestMemoryInspectionRunStore_LastInspectionSource 内存运行记录实现
// service.LastInspectionSource：无记录 ok=false；记录后返回最近一轮时点。
func TestMemoryInspectionRunStore_LastInspectionSource(t *testing.T) {
	var src service.LastInspectionSource = NewMemoryInspectionRunStore()
	_, ok, err := src.LastInspectionAt(context.Background())
	require.NoError(t, err)
	assert.False(t, ok)

	store := NewMemoryInspectionRunStore()
	t1 := jobBase
	t2 := t1.Add(time.Hour)
	require.NoError(t, store.RecordRun(context.Background(), InspectionRun{At: t1}))
	require.NoError(t, store.RecordRun(context.Background(), InspectionRun{At: t2}))
	at, ok, err := store.LastInspectionAt(context.Background())
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, t2, at, "最近一轮时点生效")
}

// TestStepMetrics_SuccessRate 子步成功率口径：(Total-Failed)/Total；Total=0 视为 1。
func TestStepMetrics_SuccessRate(t *testing.T) {
	cases := []struct {
		name string
		m    StepMetrics
		want float64
	}{
		{"全部成功", StepMetrics{Ok: true, Total: 10, Failed: 0}, 1},
		{"部分失败", StepMetrics{Ok: true, Total: 10, Failed: 2}, 0.8},
		{"全部失败", StepMetrics{Ok: true, Total: 4, Failed: 4}, 0},
		{"空集视为满率", StepMetrics{Ok: true}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.InDelta(t, tc.want, tc.m.SuccessRate(), 1e-9)
		})
	}
}

// TestRunInspection_RecordsStepMetricsForMonitoring AC3 各子步指标随运行记录落库
// （供平台自身监控），四步指标齐备且含步骤专属计数。
func TestRunInspection_RecordsStepMetricsForMonitoring(t *testing.T) {
	h := newJobHarness(t)
	far := certtest.NewBundle(t, "far.example.com", []string{"far.example.com"}, nil)
	h.seedBundle(t, far, []string{"far.example.com"}, relNow(200*day))
	h.probe.results = []domain.ProbeResult{
		{Domain: "far.example.com", Status: domain.ProbeStatusConsistent},
	}

	run, err := h.job.RunInspection(context.Background())
	require.NoError(t, err)
	require.Len(t, run.Steps, 4)
	for _, s := range run.Steps {
		assert.NotEmpty(t, s.Name)
		assert.True(t, s.Ok)
		assert.GreaterOrEqual(t, s.SuccessRate(), 0.0)
	}
	assert.Equal(t, 1, stepByName(t, run, StepProbe).Extra["consistent"])
	assert.Equal(t, 0, stepByName(t, run, StepExpiry).Extra["triggered"])
}

// ---------------------------------------------------------------------
// 装配默认值与步骤级错误分支（7.1 接线前的健壮性）
// ---------------------------------------------------------------------

// TestNewInspectionJob_NilDependenciesDefaults publisher/store 传 nil 回退默认实现
// （日志发布器/内存运行记录），不 panic 且整轮可执行。
func TestNewInspectionJob_NilDependenciesDefaults(t *testing.T) {
	certs := certtest.NewFakeCertificateRepo()
	exempts := certtest.NewFakeExemptionRepo()
	alert := certtest.NewFakeAlertConfigRepo()
	probe := &fakeProbeService{certs: certs}
	job := NewInspectionJob(certs, exempts, service.NewInspectionService(certs, alert, nil), probe, nil, nil)
	require.NotNil(t, job)

	run, err := job.RunInspection(context.Background())
	require.NoError(t, err)
	assert.True(t, run.Ok())
}

// listFailCertRepo 包装台账假实现：List 注入失败（仓储不可用场景）。
type listFailCertRepo struct {
	*certtest.FakeCertificateRepo
	err error
}

func (r *listFailCertRepo) List(ctx context.Context) ([]domain.Certificate, error) {
	return nil, r.err
}

// TestRunInspection_IntegrityListFailureFailsStepNotRound 台账 List 失败 →
// 完整性步骤级失败（Ok=false），整轮仍执行探测与豁免过滤、运行记录仍落。
func TestRunInspection_IntegrityListFailureFailsStepNotRound(t *testing.T) {
	h := newJobHarness(t)
	far := certtest.NewBundle(t, "far.example.com", []string{"far.example.com"}, nil)
	h.seedBundle(t, far, []string{"far.example.com"}, relNow(200*day))

	repo := &listFailCertRepo{FakeCertificateRepo: h.certs, err: fmt.Errorf("db down")}
	// 4.2 引擎同仓储（List 同样失败→到期步骤级失败，探测 fake 独立读取不受影响）
	job := NewInspectionJob(repo, h.exempts, service.NewInspectionService(repo, h.alert, h.pub), h.probe, h.pub, h.store)
	fixedNow := h.now
	job.now = func() time.Time { return fixedNow }

	run, err := job.RunInspection(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")
	assert.False(t, stepByName(t, run, StepIntegrity).Ok)
	assert.False(t, stepByName(t, run, StepExpiry).Ok)
	assert.True(t, stepByName(t, run, StepProbe).Ok, "探测不依赖台账读取失败")
	_, ok, _ := h.store.LastInspectionAt(context.Background())
	assert.True(t, ok)
}

// failingPublisher 发布器注入失败（通道故障场景）。
type failingPublisher struct {
	err error
}

func (p *failingPublisher) PublishAlert(context.Context, service.CertAlertEvent) error { return p.err }

// TestRunInspection_PublishFailuresAggregated 完整性异常与 diff 事件发布失败：
// 聚合返回、子步 Ok=false，巡检不阻塞（at-least-once 语义由下轮巡检承接）。
func TestRunInspection_PublishFailuresAggregated(t *testing.T) {
	h := newJobHarness(t)
	far := certtest.NewBundle(t, "far.example.com", []string{"far.example.com", "diff.example.com"}, nil)
	h.seedBundle(t, far, []string{"far.example.com", "diff.example.com"}, relNow(200*day))
	h.seed(t, h.fp(), "broken.example.com", "not a pem", []string{"broken.example.com"}, relNow(200*day))
	h.probe.results = []domain.ProbeResult{
		{Domain: "far.example.com", Status: domain.ProbeStatusConsistent},
		{Domain: "diff.example.com", Status: domain.ProbeStatusDiff, OnlineFingerprint: "aa"},
	}

	pub := &failingPublisher{err: fmt.Errorf("channel down")}
	job := NewInspectionJob(h.certs, h.exempts, h.expiry, h.probe, pub, h.store)
	fixedNow := h.now
	job.now = func() time.Time { return fixedNow }

	run, err := job.RunInspection(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "channel down")
	assert.False(t, stepByName(t, run, StepIntegrity).Ok, "完整性异常发布失败 → 步骤级失败")
	assert.Equal(t, 1, stepByName(t, run, StepIntegrity).Failed, "异常计数不受发布失败影响")
	assert.False(t, stepByName(t, run, StepExemption).Ok, "diff 发布失败 → 步骤级失败")
	assert.True(t, stepByName(t, run, StepExpiry).Ok, "到期引擎（内存发布器）不受影响")
}
