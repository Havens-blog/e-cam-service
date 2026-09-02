package service

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/dns"
	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// ---------------------------------------------------------------------
// 测试基础设施：本地多证书 SNI TLS server + 仓储假实现
// ---------------------------------------------------------------------

// sniServer 本地 TLS server：按 ClientHello 的 SNI（ServerName）返回对应证书。
// 未登记 SNI 返回错误（握手失败 → 探测侧 unreachable 路径）。
type sniServer struct {
	listener net.Listener
	addr     string
}

// newSNIServer 启动本地多证书 SNI TLS server；certs 键为 SNI 域名。
func newSNIServer(t *testing.T, bundles map[string]*certtest.CertBundle) *sniServer {
	t.Helper()
	certMap := make(map[string]*tls.Certificate, len(bundles))
	for name, b := range bundles {
		cert, err := tls.X509KeyPair(b.CertPEM, b.KeyPEM)
		require.NoError(t, err, "certtest: 加载夹具证书 %s", name)
		certMap[name] = &cert
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := &sniServer{listener: ln, addr: ln.Addr().String()}
	cfg := &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			cert, ok := certMap[hello.ServerName]
			if !ok {
				return nil, fmt.Errorf("sniServer: no certificate for %q", hello.ServerName)
			}
			return &tls.Config{Certificates: []tls.Certificate{*cert}}, nil
		},
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			tlsConn := tls.Server(conn, cfg)
			// 仅完成握手读证书即关闭（不处理任何应用层请求，与探测语义对齐）
			_ = tlsConn.Handshake()
			_ = tlsConn.Close()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return srv
}

// leafNotAfter 解析夹具 leaf 证书的有效期截止（断言 onlineNotAfter 口径）。
func leafNotAfter(t *testing.T, b *certtest.CertBundle) time.Time {
	t.Helper()
	block, _ := pem.Decode(b.CertPEM)
	require.NotNil(t, block)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return cert.NotAfter
}

// leafFingerprint 与台账口径一致的 SHA256(leaf.Raw) 小写 hex。
func leafFingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// fakeProbeRepo 探测结果内存假实现（仅 Create/LatestByDomain，无删除——
// TTL 索引自然过期口径：探测服务不做主动清理）。
type fakeProbeRepo struct {
	mu      sync.Mutex
	created []domain.ProbeResult
}

func (f *fakeProbeRepo) Create(_ context.Context, r *domain.ProbeResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.ProbeAt.IsZero() {
		r.ProbeAt = time.Now()
	}
	stored := *r
	if stored.OnlineNotAfter != nil {
		na := *stored.OnlineNotAfter
		stored.OnlineNotAfter = &na
	}
	f.created = append(f.created, stored)
	return nil
}

func (f *fakeProbeRepo) LatestByDomain(_ context.Context, _ string) (domain.ProbeResult, error) {
	return domain.ProbeResult{}, mongo.ErrNoDocuments
}

// LatestPerDomain 每域最新探测（domain 字典序稳定；探测任务本身不消费，
// 仅为满足 4.5 扩展后的 ProbeResultRepository 接口）。
func (f *fakeProbeRepo) LatestPerDomain(_ context.Context) ([]domain.ProbeResult, error) {
	latest := map[string]domain.ProbeResult{}
	for _, r := range f.all() {
		if cur, ok := latest[r.Domain]; !ok || r.ProbeAt.After(cur.ProbeAt) {
			latest[r.Domain] = r
		}
	}
	names := make([]string, 0, len(latest))
	for name := range latest {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]domain.ProbeResult, 0, len(names))
	for _, name := range names {
		out = append(out, latest[name])
	}
	return out, nil
}

// ListRecentByDomains 各域名最近探测记录（任务 5.10 扩展后的接口方法；
// 探测任务本身不消费，提供真实语义供共享复用）：每域名 probeAt 降序至多
// limit 条，domain 字典序稳定返回。
func (f *fakeProbeRepo) ListRecentByDomains(_ context.Context, domains []string, limit int) ([]domain.ProbeResult, error) {
	if len(domains) == 0 || limit <= 0 {
		return []domain.ProbeResult{}, nil
	}
	set := make(map[string]bool, len(domains))
	for _, d := range domains {
		set[d] = true
	}
	grouped := map[string][]domain.ProbeResult{}
	for _, r := range f.all() {
		if set[r.Domain] {
			grouped[r.Domain] = append(grouped[r.Domain], r)
		}
	}
	names := make([]string, 0, len(grouped))
	for name := range grouped {
		names = append(names, name)
	}
	sort.Strings(names)
	out := []domain.ProbeResult{}
	for _, name := range names {
		rs := grouped[name]
		sort.SliceStable(rs, func(i, j int) bool { return rs[i].ProbeAt.After(rs[j].ProbeAt) })
		if len(rs) > limit {
			rs = rs[:limit]
		}
		out = append(out, rs...)
	}
	return out, nil
}

func (f *fakeProbeRepo) all() []domain.ProbeResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.ProbeResult(nil), f.created...)
}

func (f *fakeProbeRepo) byDomain(name string) []domain.ProbeResult {
	var out []domain.ProbeResult
	for _, r := range f.all() {
		if r.Domain == name {
			out = append(out, r)
		}
	}
	return out
}

// fakeExemptionRepo 豁免清单内存假实现。
type fakeExemptionRepo struct{ domains []string }

func (f *fakeExemptionRepo) Upsert(_ context.Context, _ *domain.Exemption) error { return nil }
func (f *fakeExemptionRepo) List(_ context.Context) ([]domain.Exemption, error) {
	out := make([]domain.Exemption, 0, len(f.domains))
	for _, d := range f.domains {
		out = append(out, domain.Exemption{Domain: d})
	}
	return out, nil
}
func (f *fakeExemptionRepo) DeleteByDomain(_ context.Context, _ string) error { return nil }

// fakeChangeOrderRepo 变更单内存假实现（探测任务只消费 ListVerifyingActive）。
type fakeChangeOrderRepo struct{ orders []domain.ChangeOrder }

func (f *fakeChangeOrderRepo) Create(_ context.Context, _ *domain.ChangeOrder) (string, error) {
	return "", nil
}
func (f *fakeChangeOrderRepo) GetByID(_ context.Context, _ string) (domain.ChangeOrder, error) {
	return domain.ChangeOrder{}, mongo.ErrNoDocuments
}
func (f *fakeChangeOrderRepo) GetByMutexToken(_ context.Context, _ string) (domain.ChangeOrder, error) {
	return domain.ChangeOrder{}, mongo.ErrNoDocuments
}
func (f *fakeChangeOrderRepo) TransitionActive(_ context.Context, _ string, _ domain.ChangeStatus, _ string) error {
	return nil
}
func (f *fakeChangeOrderRepo) TransitionTerminal(_ context.Context, _ string, _ domain.ChangeStatus) error {
	return nil
}
func (f *fakeChangeOrderRepo) TransitionTerminalWithProtect(_ context.Context, _ string, _ domain.ChangeStatus, _ time.Time) error {
	return nil
}
func (f *fakeChangeOrderRepo) ListPausedBefore(_ context.Context, _ time.Time) ([]domain.ChangeOrder, error) {
	return nil, nil
}
func (f *fakeChangeOrderRepo) SetBatchInfo(_ context.Context, _ string, _ *domain.BatchInfo) (bool, error) {
	return false, nil
}
func (f *fakeChangeOrderRepo) EnterVerify(_ context.Context, _ string, _, _ time.Time) (bool, error) {
	return false, nil
}
func (f *fakeChangeOrderRepo) AdvanceBatch(_ context.Context, _ string, _ domain.ChangeStatus, _ int) (bool, error) {
	return false, nil
}
func (f *fakeChangeOrderRepo) SetVerifyExpected(_ context.Context, _ string, _ *domain.VerifyExpected) (bool, error) {
	return false, nil
}
func (f *fakeChangeOrderRepo) PauseAfterVerify(_ context.Context, _ string, _ time.Time) (bool, error) {
	return false, nil
}
func (f *fakeChangeOrderRepo) ListVerifyingExpired(_ context.Context, _ time.Time) ([]domain.ChangeOrder, error) {
	return nil, nil
}
func (f *fakeChangeOrderRepo) ListByNewCertID(_ context.Context, _ string) ([]domain.ChangeOrder, error) {
	return nil, nil
}

// ListPage 分页查询（任务 5.11 接口扩展：探测测试不消费，返回空页）。
func (f *fakeChangeOrderRepo) ListPage(_ context.Context, _ domain.ChangeStatus, _, _ int) ([]domain.ChangeOrder, int64, error) {
	return nil, 0, nil
}

// ListVerifyingActive 过滤 status=verifying 且 verifyWindowUntil > after。
func (f *fakeChangeOrderRepo) ListVerifyingActive(_ context.Context, after time.Time) ([]domain.ChangeOrder, error) {
	var out []domain.ChangeOrder
	for _, o := range f.orders {
		if o.Status != domain.ChangeStatusVerifying || o.VerifyWindowUntil == nil {
			continue
		}
		if o.VerifyWindowUntil.After(after) {
			out = append(out, o)
		}
	}
	return out, nil
}

// countingDialer 包装一层拨测端口，统计并发峰值与调用域名（wildcard 不拨测断言）。
type countingDialer struct {
	inner    tlsDialer
	inFlight atomic.Int64
	maxSeen  atomic.Int64
	called   sync.Map // domain → struct{}
}

func (c *countingDialer) Dial(ctx context.Context, domainName string) (dialResult, error) {
	cur := c.inFlight.Add(1)
	for {
		max := c.maxSeen.Load()
		if cur <= max || c.maxSeen.CompareAndSwap(max, cur) {
			break
		}
	}
	defer c.inFlight.Add(-1)
	c.called.Store(domainName, struct{}{})
	return c.inner.Dial(ctx, domainName)
}

func (c *countingDialer) dialed(name string) bool {
	_, ok := c.called.Load(name)
	return ok
}

func (c *countingDialer) calledCount() int64 {
	var n int64
	c.called.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}

// newVerifyingOrder 构造验证中变更单夹具（orderHex 为 24 位 hex 的订单 ID）。
func newVerifyingOrder(t *testing.T, orderHex string, windowUntil time.Time, newFP string, domains []string) domain.ChangeOrder {
	t.Helper()
	oid, err := primitive.ObjectIDFromHex(orderHex)
	require.NoError(t, err)
	oldFP := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaoldfp"
	return domain.ChangeOrder{
		ID:                 oid,
		OldCertFingerprint: oldFP,
		NewCertID:          "cert-new",
		Status:             domain.ChangeStatusVerifying,
		VerifyWindowUntil:  &windowUntil,
		ActiveMutex:        oldFP,
		VerifyExpected: &domain.VerifyExpected{
			NewCertFingerprint: newFP,
			Domains:            domains,
			WindowUntil:        windowUntil,
		},
		Creator:   "operator",
		CreatedAt: time.Now(),
	}
}

// probeHarness 聚合一轮探测测试的全部依赖。
type probeHarness struct {
	svc     ProbeService
	probes  *fakeProbeRepo
	certs   *certtest.FakeCertificateRepo
	orders  *fakeChangeOrderRepo
	exempts *fakeExemptionRepo
	alert   *fakeAlertCfgRepo
	dialer  *countingDialer
}

// newProbeHarness 组装探测服务：本地 SNI server 作为拨测目标。
func newProbeHarness(t *testing.T, srv *sniServer) *probeHarness {
	t.Helper()
	certs := certtest.NewFakeCertificateRepo()
	probes := &fakeProbeRepo{}
	exempts := &fakeExemptionRepo{}
	alert := &fakeAlertCfgRepo{cfg: domain.DefaultAlertConfig()}
	orders := &fakeChangeOrderRepo{}
	dialer := &countingDialer{inner: &stdTLSDialer{timeout: 2 * time.Second, addrOverride: srv.addr}}
	svc := NewProbeService(certs, probes, exempts, alert, orders, dialer, ProbeOptions{})
	return &probeHarness{svc: svc, probes: probes, certs: certs, orders: orders, exempts: exempts, alert: alert, dialer: dialer}
}

// seedCert 写入一张台账证书（sans 归属映射依据）。
func (h *probeHarness) seedCert(t *testing.T, b *certtest.CertBundle, sans []string) {
	t.Helper()
	require.NoError(t, h.certs.Create(context.Background(), &domain.Certificate{
		Fingerprint:   b.Fingerprint,
		CommonName:    b.CN,
		Sans:          sans,
		NotAfter:      leafNotAfter(t, b),
		HostingStatus: domain.HostingStatusComplete,
	}))
}

// ---------------------------------------------------------------------
// AC1：目标域来源 = 台账 sans 展开去重（expectedDomain 不参与）
// ---------------------------------------------------------------------

// TestProbeLedgerDomains_TargetsFromSANs 台账两证共享 SAN 去重、通配符入列、
// expectedDomain 不参与探测目标。
func TestProbeLedgerDomains_TargetsFromSANs(t *testing.T) {
	certA := certtest.NewBundle(t, "a.example.com", []string{"a.example.com", "shared.example.com"}, nil)
	certB := certtest.NewBundle(t, "b.example.com", []string{"shared.example.com", "*.wild.example.com"}, nil)
	srv := newSNIServer(t, map[string]*certtest.CertBundle{
		"a.example.com":      certA,
		"shared.example.com": certA,
	})
	h := newProbeHarness(t, srv)
	h.seedCert(t, certA, []string{"a.example.com", "shared.example.com"})
	// expectedDomain 仅提示性比对，不参与目标域
	require.NoError(t, h.certs.Create(context.Background(), &domain.Certificate{
		Fingerprint:    certB.Fingerprint,
		CommonName:     certB.CN,
		Sans:           []string{"shared.example.com", "*.wild.example.com"},
		NotAfter:       leafNotAfter(t, certB),
		HostingStatus:  domain.HostingStatusComplete,
		ExpectedDomain: "expected.example.com",
	}))

	results, err := h.svc.ProbeLedgerDomains(context.Background())
	require.NoError(t, err)

	domains := map[string]bool{}
	for _, r := range results {
		domains[r.Domain] = true
	}
	// sans 展开去重：a.example.com、shared.example.com、*.wild.example.com 恰三条
	assert.Len(t, results, 3, "目标域 = 台账全部 sans 展开去重")
	assert.Contains(t, domains, "a.example.com")
	assert.Contains(t, domains, "shared.example.com")
	assert.Contains(t, domains, "*.wild.example.com")
	assert.NotContains(t, domains, "expected.example.com", "expectedDomain 不参与探测目标")

	// shared.example.com 由 certA 提供线上证书 → consistent（归属任一证书一致）
	for _, r := range results {
		if r.Domain == "shared.example.com" {
			assert.Equal(t, domain.ProbeStatusConsistent, r.Status)
			assert.Equal(t, certA.Fingerprint, r.OnlineFingerprint)
		}
	}
	// 写穿透：仓储收到的记录与返回一致；probeAt 由仓储 DEFAULT 填充（TTL 基准）
	stored := h.probes.all()
	assert.Len(t, stored, 3)
	for _, r := range stored {
		assert.False(t, r.ProbeAt.IsZero(), "probeAt 需落库（TTL 索引过期基准，无主动清理）")
	}
}

// ---------------------------------------------------------------------
// AC2 + AC6：六态一轮集成（本地多证书 SNI server）
// ---------------------------------------------------------------------

// TestProbe_SixStatesRound 一轮覆盖六态：consistent / diff / unreachable（server
// 拒绝该 SNI）/ exempt / wildcard_skipped / change_linked_diff（验证窗口关联）；
// 单域名失败不中断整轮。
func TestProbe_SixStatesRound(t *testing.T) {
	owner := certtest.NewBundle(t, "ok.example.com", []string{
		"ok.example.com", "mismatch.example.com", "exempt.example.com", "linked.example.com",
	}, nil)
	other := certtest.NewBundle(t, "other.example.com", []string{"other.example.com"}, nil)
	srv := newSNIServer(t, map[string]*certtest.CertBundle{
		"ok.example.com":       owner, // consistent
		"mismatch.example.com": other, // diff（线上=other，台账归属=owner）
		"exempt.example.com":   other, // exempt（仍探测，指纹不一致也不计 diff）
		"linked.example.com":   other, // change_linked_diff（other 指纹=订单预期新指纹）
		// down.example.com 未登记 → server 拒绝握手 → unreachable
	})
	h := newProbeHarness(t, srv)
	h.seedCert(t, owner, []string{
		"ok.example.com", "mismatch.example.com", "exempt.example.com", "linked.example.com",
	})
	h.exempts.domains = []string{"exempt.example.com"}
	window := time.Now().Add(time.Hour)
	h.orders.orders = []domain.ChangeOrder{
		newVerifyingOrder(t, "aaaaaaaaaaaaaaaaaaaaaa01", window, other.Fingerprint, []string{"linked.example.com"}),
	}

	results, err := h.svc.ProbeDomains(context.Background(), []string{
		"ok.example.com", "mismatch.example.com", "down.example.com",
		"exempt.example.com", "*.wild.example.com", "linked.example.com",
	})
	require.NoError(t, err)
	byDomain := map[string]domain.ProbeResult{}
	for _, r := range results {
		byDomain[r.Domain] = r
	}
	require.Len(t, results, 6, "单域名失败不中断整轮：全部目标均有结果")

	// consistent：线上指纹=台账归属证书
	r := byDomain["ok.example.com"]
	assert.Equal(t, domain.ProbeStatusConsistent, r.Status)
	assert.Equal(t, owner.Fingerprint, r.OnlineFingerprint)
	assert.NotNil(t, r.OnlineNotAfter)
	assert.True(t, r.OnlineNotAfter.Equal(leafNotAfter(t, owner)), "onlineNotAfter = 对端 leaf NotAfter")

	// diff：线上指纹≠���账该域名归属证书
	r = byDomain["mismatch.example.com"]
	assert.Equal(t, domain.ProbeStatusDiff, r.Status)
	assert.Equal(t, other.Fingerprint, r.OnlineFingerprint)
	assert.Empty(t, r.ChangeOrderID)

	// unreachable：握手失败（server 未登记 SNI），指纹缺省，不参与差异告警
	r = byDomain["down.example.com"]
	assert.Equal(t, domain.ProbeStatusUnreachable, r.Status)
	assert.Empty(t, r.OnlineFingerprint)
	assert.Nil(t, r.OnlineNotAfter)

	// exempt：仍探测但 status=exempt，不按差异判定
	r = byDomain["exempt.example.com"]
	assert.Equal(t, domain.ProbeStatusExempt, r.Status)
	assert.NotEmpty(t, r.OnlineFingerprint, "豁免域名仍探测（记录线上指纹）")

	// wildcard_skipped：通配符不拨测
	r = byDomain["*.wild.example.com"]
	assert.Equal(t, domain.ProbeStatusWildcardSkipped, r.Status)
	assert.Empty(t, r.OnlineFingerprint)
	assert.False(t, h.dialer.dialed("*.wild.example.com"), "通配符 SAN 不得发起拨测")

	// change_linked_diff：diff 且命中验证窗口预期指纹 → 关联订单
	r = byDomain["linked.example.com"]
	assert.Equal(t, domain.ProbeStatusChangeLinkedDiff, r.Status)
	assert.Equal(t, other.Fingerprint, r.OnlineFingerprint)
	assert.Equal(t, "aaaaaaaaaaaaaaaaaaaaaa01", r.ChangeOrderID, "change_linked_diff 需记录关联 changeOrderId")
}

// TestProbe_DialRefused 拨号失败（连接拒绝/DNS 失败类）→ unreachable，不影响其余域名。
func TestProbe_DialRefused(t *testing.T) {
	// 申请一个端口后立即关闭 → 连接拒绝
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closedAddr := ln.Addr().String()
	require.NoError(t, ln.Close())

	certA := certtest.NewBundle(t, "a.example.com", []string{"a.example.com"}, nil)
	srv := newSNIServer(t, map[string]*certtest.CertBundle{"a.example.com": certA})
	h := newProbeHarness(t, srv)
	// 将拨测地址替换为已关闭端口，模拟 DNS 失败/超时/拒绝类不可达
	h.dialer.inner = &stdTLSDialer{timeout: 2 * time.Second, addrOverride: closedAddr}
	h.seedCert(t, certA, []string{"a.example.com", "dead.example.com"})

	results, err := h.svc.ProbeDomains(context.Background(), []string{"a.example.com", "dead.example.com"})
	require.NoError(t, err)
	byDomain := map[string]domain.ProbeResult{}
	for _, r := range results {
		byDomain[r.Domain] = r
	}
	assert.Equal(t, domain.ProbeStatusUnreachable, byDomain["a.example.com"].Status)
	assert.Equal(t, domain.ProbeStatusUnreachable, byDomain["dead.example.com"].Status)
}

// TestProbe_HandshakeTimeout 握手超时受控（单域名 dial timeout 常量化生效）→ unreachable。
func TestProbe_HandshakeTimeout(t *testing.T) {
	// 接受连接但不响应握手 → 客户端握手超时
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			time.Sleep(3 * time.Second) // 挂起连接，触发对端握手超时
			_ = conn.Close()
		}
	}()

	certA := certtest.NewBundle(t, "a.example.com", []string{"a.example.com"}, nil)
	srv := newSNIServer(t, map[string]*certtest.CertBundle{"a.example.com": certA})
	h := newProbeHarness(t, srv)
	h.dialer.inner = &stdTLSDialer{timeout: 150 * time.Millisecond, addrOverride: ln.Addr().String()}
	h.seedCert(t, certA, []string{"a.example.com"})

	start := time.Now()
	results, err := h.svc.ProbeDomains(context.Background(), []string{"a.example.com"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, domain.ProbeStatusUnreachable, results[0].Status)
	assert.Less(t, time.Since(start), 2*time.Second, "超时受控：受 dial timeout 常量约束")
}

// TestProbe_MultiCertOwnershipSameDomain 多证书同域名并存行为固定：
// 与任一归属证书一致 → consistent（Implementation Notes 固定语义：
// 换证并存期线上命中任一在册证书不误报差异）。
func TestProbe_MultiCertOwnershipSameDomain(t *testing.T) {
	certA := certtest.NewBundle(t, "legacy.example.com", []string{"multi.example.com"}, nil)
	certB := certtest.NewBundle(t, "fresh.example.com", []string{"multi.example.com"}, nil)
	srv := newSNIServer(t, map[string]*certtest.CertBundle{"multi.example.com": certB})
	h := newProbeHarness(t, srv)
	h.seedCert(t, certA, []string{"multi.example.com"})
	h.seedCert(t, certB, []string{"multi.example.com"})

	results, err := h.svc.ProbeDomains(context.Background(), []string{"multi.example.com"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, domain.ProbeStatusConsistent, results[0].Status,
		"线上指纹与任一归属证书一致即 consistent")
}

// ---------------------------------------------------------------------
// AC3：通配符 wildcard_skipped + concreteSubdomainOverride
// ---------------------------------------------------------------------

// TestProbe_WildcardSkipped_NoOverride 无 override：写 ProbeResult{domain=通配符
// SAN, status=wildcard_skipped}，不拨测。
func TestProbe_WildcardSkipped_NoOverride(t *testing.T) {
	certA := certtest.NewBundle(t, "wild.example.com", []string{"*.wild.example.com"}, nil)
	srv := newSNIServer(t, map[string]*certtest.CertBundle{})
	h := newProbeHarness(t, srv)
	h.seedCert(t, certA, []string{"*.wild.example.com"})

	results, err := h.svc.ProbeDomains(context.Background(), []string{"*.wild.example.com"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "*.wild.example.com", results[0].Domain)
	assert.Equal(t, domain.ProbeStatusWildcardSkipped, results[0].Status)
	assert.Empty(t, results[0].OnlineFingerprint)
	assert.False(t, h.dialer.dialed("*.wild.example.com"), "通配符 SAN 不得发起拨测")

	stored := h.probes.byDomain("*.wild.example.com")
	require.Len(t, stored, 1)
	assert.Equal(t, domain.ProbeStatusWildcardSkipped, stored[0].Status)
}

// TestProbe_WildcardOverride 配置 concreteSubdomainOverride 后：对子域名正常拨测，
// 结果记于子域名、按常规状态判定；通配符本体不写 wildcard_skipped。
func TestProbe_WildcardOverride(t *testing.T) {
	certA := certtest.NewBundle(t, "wild.example.com", []string{"*.wild.example.com"}, nil)
	certNew := certtest.NewBundle(t, "probe-wild.example.com", []string{"probe.wild.example.com"}, nil)
	srv := newSNIServer(t, map[string]*certtest.CertBundle{
		"probe.wild.example.com": certNew, // 线上为新证书（非台账归属）→ diff
	})
	h := newProbeHarness(t, srv)
	h.seedCert(t, certA, []string{"*.wild.example.com"})
	h.alert.cfg.WildcardProbeOverrides = map[string]string{
		"*.wild.example.com": "probe.wild.example.com",
	}

	results, err := h.svc.ProbeDomains(context.Background(), []string{"*.wild.example.com"})
	require.NoError(t, err)
	require.Len(t, results, 1, "override 后结果记于具体子域名，通配符本体不重复记录")
	assert.Equal(t, "probe.wild.example.com", results[0].Domain)
	assert.Equal(t, domain.ProbeStatusDiff, results[0].Status, "override 子域名按常规状态判定")
	assert.Equal(t, certNew.Fingerprint, results[0].OnlineFingerprint)
	assert.True(t, h.dialer.dialed("probe.wild.example.com"), "对 override 子域名正常拨测")
	assert.False(t, h.dialer.dialed("*.wild.example.com"), "通配符本体不拨测")
	assert.Empty(t, h.probes.byDomain("*.wild.example.com"), "通配���本体不产生 wildcard_skipped 记录")

	// 豁免 + override 组合：子域名豁免 → exempt
	h.exempts.domains = []string{"probe.wild.example.com"}
	results, err = h.svc.ProbeDomains(context.Background(), []string{"*.wild.example.com"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, domain.ProbeStatusExempt, results[0].Status)
}

// ---------------------------------------------------------------------
// AC5：change_linked_diff 关联判定边界
// ---------------------------------------------------------------------

// TestProbe_ChangeLinkedDiff_AssociatesOrderID 命中窗口 → status=change_linked_diff
// 且 changeOrderId = 订单 ID（hex）。
func TestProbe_ChangeLinkedDiff_AssociatesOrderID(t *testing.T) {
	owner := certtest.NewBundle(t, "owner.example.com", []string{"app.example.com"}, nil)
	newCert := certtest.NewBundle(t, "new.example.com", []string{"app.example.com"}, nil)
	srv := newSNIServer(t, map[string]*certtest.CertBundle{"app.example.com": newCert})
	h := newProbeHarness(t, srv)
	h.seedCert(t, owner, []string{"app.example.com"})

	order := newVerifyingOrder(t, "abcdef0123456789abcdef01", time.Now().Add(2*time.Hour), newCert.Fingerprint, []string{"app.example.com"})
	h.orders.orders = []domain.ChangeOrder{order}

	results, err := h.svc.ProbeDomains(context.Background(), []string{"app.example.com"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, domain.ProbeStatusChangeLinkedDiff, results[0].Status)
	assert.Equal(t, order.ID.Hex(), results[0].ChangeOrderID)

	stored := h.probes.byDomain("app.example.com")
	require.Len(t, stored, 1)
	assert.Equal(t, domain.ProbeStatusChangeLinkedDiff, stored[0].Status)
	assert.Equal(t, order.ID.Hex(), stored[0].ChangeOrderID)
}

// TestProbe_ChangeLinkedDiff_Boundaries 窗口过期 / 域名不在 domains /
// 指纹不匹配 → 维持常规 diff，不关联订单。
func TestProbe_ChangeLinkedDiff_Boundaries(t *testing.T) {
	owner := certtest.NewBundle(t, "owner.example.com", []string{"d1.example.com", "d2.example.com", "d3.example.com"}, nil)
	newCert := certtest.NewBundle(t, "new.example.com", []string{"d1.example.com", "d2.example.com", "d3.example.com"}, nil)
	unrelated := certtest.NewBundle(t, "unrelated.example.com", []string{"x.example.com"}, nil)
	srv := newSNIServer(t, map[string]*certtest.CertBundle{
		"d1.example.com": newCert,
		"d2.example.com": newCert,
		"d3.example.com": newCert,
	})
	h := newProbeHarness(t, srv)
	h.seedCert(t, owner, []string{"d1.example.com", "d2.example.com", "d3.example.com"})

	expired := time.Now().Add(-time.Minute)
	active := time.Now().Add(time.Hour)
	h.orders.orders = []domain.ChangeOrder{
		// d1：窗口已过期（verifyWindowUntil <= now）
		newVerifyingOrder(t, "aaaaaaaaaaaaaaaaaaaaaa02", expired, newCert.Fingerprint, []string{"d1.example.com"}),
		// d2：窗口活跃但预期指纹与线上不一致
		newVerifyingOrder(t, "aaaaaaaaaaaaaaaaaaaaaa03", active, unrelated.Fingerprint, []string{"d2.example.com"}),
		// d3：窗口活跃、指纹匹配，但域名不在 verifyExpected.domains
		newVerifyingOrder(t, "aaaaaaaaaaaaaaaaaaaaaa04", active, newCert.Fingerprint, []string{"other.example.com"}),
	}

	results, err := h.svc.ProbeDomains(context.Background(), []string{"d1.example.com", "d2.example.com", "d3.example.com"})
	require.NoError(t, err)
	byDomain := map[string]domain.ProbeStatus{}
	for _, r := range results {
		byDomain[r.Domain] = r.Status
	}
	assert.Equal(t, domain.ProbeStatusDiff, byDomain["d1.example.com"], "窗口过期（verifyWindowUntil <= now）不关联")
	assert.Equal(t, domain.ProbeStatusDiff, byDomain["d2.example.com"], "指纹不匹配维持 diff")
	assert.Equal(t, domain.ProbeStatusDiff, byDomain["d3.example.com"], "域名不在 verifyExpected.domains 维持 diff")
	for _, r := range results {
		assert.Empty(t, r.ChangeOrderID)
	}
}

// ---------------------------------------------------------------------
// Hard Rules：并发受控 + 仅 TLS 握手（生产拨测端口直测）
// ---------------------------------------------------------------------

// TestProbe_BoundedConcurrency 整体批量并发受控（probeConcurrency 常量上限），
// 且全部域名均完成拨测。
func TestProbe_BoundedConcurrency(t *testing.T) {
	const total = 16
	certA := certtest.NewBundle(t, "a.example.com", []string{"a.example.com"}, nil)
	srv := newSNIServer(t, map[string]*certtest.CertBundle{"a.example.com": certA})
	h := newProbeHarness(t, srv)

	// 慢速拨测端口：每次调用阻塞 60ms，统计并发峰值
	h.dialer.inner = &blockingDialer{delay: 60 * time.Millisecond}
	domains := make([]string, 0, total)
	for i := 0; i < total; i++ {
		domains = append(domains, fmt.Sprintf("d%d.example.com", i))
	}

	results, err := h.svc.ProbeDomains(context.Background(), domains)
	require.NoError(t, err)
	assert.Len(t, results, total)
	assert.LessOrEqual(t, int(h.dialer.maxSeen.Load()), probeConcurrency,
		"并发峰值不得超过 probeConcurrency 常量")
	assert.Equal(t, int64(total), h.dialer.calledCount(), "全部域名均已拨测")
}

// blockingDialer 固定延迟的拨测假实现（并发计数用途）。
type blockingDialer struct {
	delay time.Duration
}

func (b *blockingDialer) Dial(_ context.Context, _ string) (dialResult, error) {
	time.Sleep(b.delay)
	return dialResult{}, fmt.Errorf("blockingDialer: simulated failure")
}

// TestStdTLSDialer_HandshakeOnly 生产拨测端口验证：真实 crypto/tls 握手读对端
// leaf 证书（SNI 生效、指纹口径一致），不发送应用层请求。
func TestStdTLSDialer_HandshakeOnly(t *testing.T) {
	certA := certtest.NewBundle(t, "a.example.com", []string{"a.example.com"}, nil)
	srv := newSNIServer(t, map[string]*certtest.CertBundle{"a.example.com": certA})

	d := &stdTLSDialer{timeout: 2 * time.Second, addrOverride: srv.addr}
	dr, err := d.Dial(context.Background(), "a.example.com")
	require.NoError(t, err)
	require.NotNil(t, dr.Leaf)
	assert.Equal(t, certA.Fingerprint, leafFingerprint(dr.Leaf))
	assert.True(t, dr.Leaf.NotAfter.Equal(leafNotAfter(t, certA)))
	// 协商版本以 "TLS 1.x" 名称透出（本地测试 server 默认协商 TLS 1.3）
	assert.Equal(t, "TLS 1.3", dr.TLSVersion)

	// 未登记 SNI → 握手失败
	_, err = d.Dial(context.Background(), "unknown.example.com")
	assert.Error(t, err)
}

// TestNewProbeService_DefaultDialer dialer 传 nil 时使用标准库实现
// （probeDialTimeout 常量超时、拨测 443 端口），供 5.10 天级/提频调度接线。
func TestNewProbeService_DefaultDialer(t *testing.T) {
	h := &probeHarness{
		probes:  &fakeProbeRepo{},
		certs:   certtest.NewFakeCertificateRepo(),
		orders:  &fakeChangeOrderRepo{},
		exempts: &fakeExemptionRepo{},
		alert:   &fakeAlertCfgRepo{cfg: domain.DefaultAlertConfig()},
	}
	h.svc = NewProbeService(h.certs, h.probes, h.exempts, h.alert, h.orders, nil, ProbeOptions{})
	// 默认 dialer 拨真实 443，测试用不存在域名 → unreachable（单域失败不中断整轮）
	results, err := h.svc.ProbeDomains(context.Background(), []string{"nonexistent-6f2a1b.example.com"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, domain.ProbeStatusUnreachable, results[0].Status)
}

// ==================== TriggerProbeRootAsync（根域定向探测） ====================

// rootTargetSource 单租户固定目标集的 DNS 源（定向探测预检/过滤测试用）。
type rootTargetSource struct {
	targets []dns.ProbeTarget
}

func (f *rootTargetSource) ListTenantsWithRecords(context.Context) ([]int64, error) {
	return []int64{3}, nil
}

func (f *rootTargetSource) ListProbeTargets(context.Context, int64) ([]dns.ProbeTarget, error) {
	return f.targets, nil
}

// newRootProbeHarness 构造带 DNS 源的 probe 服务（dialer 失败即可，不关心结果内容）。
func newRootProbeHarness(t *testing.T, targets []dns.ProbeTarget) (*probeService, *fakeProbeRepo) {
	t.Helper()
	probes := &fakeProbeRepo{}
	svc := &probeService{
		certs:    certtest.NewFakeCertificateRepo(),
		probes:   probes,
		exempts:  &fakeExemptionRepo{},
		alertCfg: &fakeAlertCfgRepo{cfg: domain.DefaultAlertConfig()},
		orders:   &fakeChangeOrderRepo{},
		dialer:   &blockingDialer{delay: time.Millisecond},
		dnsSource: &rootTargetSource{targets: targets},
	}
	return svc, probes
}

func TestTriggerProbeRootAsyncFiltersByRoot(t *testing.T) {
	svc, probes := newRootProbeHarness(t, []dns.ProbeTarget{
		{Hostname: "www.easyeda.com", RecordType: "CNAME", TenantID: 3},
		{Hostname: "easyeda.com", RecordType: "A", TenantID: 3},
		{Hostname: "api.jlcerp.com", RecordType: "A", TenantID: 3},
	})
	require.NoError(t, svc.TriggerProbeRootAsync(context.Background(), " EASYEDA.com "))
	// 轮询等待后台轮完成（dialer 立即失败 → 两条 unreachable 全部落库）
	deadline := time.Now().Add(2 * time.Second)
	for len(probes.created) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("expected 2 persisted results, got %d", len(probes.created))
		}
		time.Sleep(5 * time.Millisecond)
	}
	domains := map[string]bool{}
	for _, r := range probes.created {
		domains[r.Domain] = true
	}
	assert.True(t, domains["www.easyeda.com"], "根域子域名应命中")
	assert.True(t, domains["easyeda.com"], "根域本身应命中")
	assert.False(t, domains["api.jlcerp.com"], "其他根域不应被拨测")
}

func TestTriggerProbeRootAsyncNoTargets(t *testing.T) {
	svc, probes := newRootProbeHarness(t, []dns.ProbeTarget{
		{Hostname: "api.jlcerp.com", RecordType: "A", TenantID: 3},
	})
	err := svc.TriggerProbeRootAsync(context.Background(), "easyeda.com")
	require.ErrorIs(t, err, ErrNoProbeTargets)
	assert.Empty(t, probes.created, "无目标不应起后台轮")
}

func TestTriggerProbeRootAsyncEmptyEqualsFull(t *testing.T) {
	svc, _ := newRootProbeHarness(t, []dns.ProbeTarget{
		{Hostname: "www.easyeda.com", RecordType: "CNAME", TenantID: 3},
	})
	// 空白参数等价全量触发：占住防重锁后再次触发返回 ErrProbeRunning
	require.NoError(t, svc.TriggerProbeRootAsync(context.Background(), "  "))
	deadline := time.Now().Add(2 * time.Second)
	for !svc.probeRunning.Load() {
		if time.Now().After(deadline) {
			t.Fatal("background probe goroutine should be running")
		}
		time.Sleep(1 * time.Millisecond)
	}
	err := svc.TriggerProbeRootAsync(context.Background(), "")
	assert.ErrorIs(t, err, ErrProbeRunning)
}
