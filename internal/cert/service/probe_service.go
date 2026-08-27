package service

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/dns"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
)

// 探测节奏常量（Hard Rule：单域名拨测超时与整轮并发受控）。
const (
	// probeTLSPort 探测目标端口（TLS 服务端点约定端口）。
	probeTLSPort = "443"
	// probeDialTimeout 单域名拨测+握手超时（常量化，Hard Rule）。
	probeDialTimeout = 5 * time.Second
	// probeConcurrency 整轮批量探测并发上限（Hard Rule：整体批量并发受控）。
	probeConcurrency = 16 // DNS 源全量轮可达数千目标（fleet 实测 2.6k+），8 并发最坏 ~27min；16 折中
)

// ---------------------------------------------------------------------
// 拨测端口（crypto/tls 注入缝：生产 stdTLSDialer / 测试本地多证书 SNI server）
// ---------------------------------------------------------------------

// tlsDialer TLS 握手拨测端口：按 SNI 对目标域发起 TLS 握手并返回对端 leaf 证书。
// Hard Rule：仅做 TLS 握手读证书，不发送任何应用层请求。
type tlsDialer interface {
	// Dial 以 domainName 为 SNI（ServerName）完成 TLS 握手，返回对端 leaf 证书；
	// 拨号失败/DNS 失败/超时返回 error（调用方归类 unreachable）。
	Dial(ctx context.Context, domainName string) (*x509.Certificate, error)
}

// stdTLSDialer 标准库 crypto/tls 拨测实现。
// InsecureSkipVerify 仅用于读取对端证书——按 SNI 指定 ServerName，验证的是
// 目标域归属（该域名当前生效哪张证书），而非 PKI 链信任；单域名超时受控。
type stdTLSDialer struct {
	// timeout 单域名拨测+握手超时。
	timeout time.Duration
	// addrOverride 拨测地址覆盖（空 = domainName:443）；仅测试注入本地 TLS server。
	addrOverride string
}

// Dial 实现 tlsDialer：net.Dialer 受控超时 + tls.Client 握手（SNI=domainName），
// 读 PeerCertificates[0] 后立即关闭连接，不发应用层请求。
func (d *stdTLSDialer) Dial(ctx context.Context, domainName string) (*x509.Certificate, error) {
	timeout := d.timeout
	if timeout <= 0 {
		timeout = probeDialTimeout
	}
	addr := net.JoinHostPort(domainName, probeTLSPort)
	if d.addrOverride != "" {
		addr = d.addrOverride
	}
	raw, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("probe: dial %s: %w", addr, err)
	}
	defer raw.Close()
	if err := raw.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, fmt.Errorf("probe: set deadline %s: %w", addr, err)
	}
	//nolint:gosec // InsecureSkipVerify：仅读取对端证书判归属，不校验链信任（见类型注释）
	conn := tls.Client(raw, &tls.Config{
		ServerName:         domainName,
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	})
	defer conn.Close()
	if err := conn.HandshakeContext(ctx); err != nil {
		return nil, fmt.Errorf("probe: tls handshake %s: %w", domainName, err)
	}
	peers := conn.ConnectionState().PeerCertificates
	if len(peers) == 0 {
		return nil, fmt.Errorf("probe: no peer certificate from %s", domainName)
	}
	return peers[0], nil
}

// ---------------------------------------------------------------------
// ProbeService：TLS 主动探测（任务 4.1）
// ---------------------------------------------------------------------

// ProbeService TLS 主动探测服务：对目标域做 SNI 拨测抓取线上生效证书，
// 与台账比对得出六态 status（consistent/diff/change_linked_diff/unreachable/
// exempt/wildcard_skipped）并逐域写 ProbeResult（TTL 索引自然过期，不主动清理）。
type ProbeService interface {
	// ProbeDomains 单轮探测指定域名清单（供天级任务与验证窗口提频共用，5.10 调度）。
	// 目标清单内的通配符 SAN 按 wildcardProbeOverrides 处置；单域名失败不中断整轮。
	ProbeDomains(ctx context.Context, domains []string) ([]domain.ProbeResult, error)
	// ProbeLedgerDomains 以台账全部 Certificate.sans 展开去重为目标域执行一轮探测
	// （expectedDomain 仅提示性比对，不参与目标域；豁免清单命中域名仍探测但标 exempt）。
	ProbeLedgerDomains(ctx context.Context) ([]domain.ProbeResult, error)
	// ProbeAllTenantDNS 以多云 DNS 记录为探测目标源，按租户轮：逐租户拉其全部
	// A/AAAA/CNAME 记录导出的子域名，SNI 拨测抓线上证书。覆盖通配符证书的实际子域名
	// 部署（DNS 记录枚举具体 hostname，不再 wildcard_skipped）；ProbeResult 带
	// tenantId/linkedResource（cdn/waf/external 链路分层）。dnsSource 未装配时返回 ErrNoDNSSource。
	ProbeAllTenantDNS(ctx context.Context) ([]domain.ProbeResult, error)
	// ProbeTenantDNS 单租户 DNS 探测（按租户轮的调度单元；ProbeAllTenantDNS 内部逐租户调用）。
	ProbeTenantDNS(ctx context.Context, tenantID int64) ([]domain.ProbeResult, error)
	// TriggerProbeAsync 立即触发一轮 DNS 源探测（后台 goroutine，立即返回）；
	// 防重：已有探测在跑返回 ErrProbeRunning。前端轮询 GET /certs/probes 看新结果。
	TriggerProbeAsync(ctx context.Context) error
}

// DNSRecordSource DNS 记录只读端口（cam/dns 模块 RecordReadPort 的 cert 侧投影）：
// cert 只依赖此接口，不碰 dns DAO/linker。dns.RecordReadPort 结构性满足本接口，
// 装配层直接注入；nil = 未装配，probe 回退 ProbeLedgerDomains 台账 SAN 路径。
type DNSRecordSource interface {
	ListTenantsWithRecords(ctx context.Context) ([]int64, error)
	ListProbeTargets(ctx context.Context, tenantID int64) ([]dns.ProbeTarget, error)
}

// ErrNoDNSSource DNS 记录源未装配（probe DNS 路径不可用，调用方应回退台账 SAN 路径）。
var ErrNoDNSSource = errors.New("probe: dns record source not configured")

// ErrProbeRunning 探测已在运行（防重：手动触发与巡检并发时拒绝重复）。
var ErrProbeRunning = errors.New("probe: already running")

type probeService struct {
	certs     domain.CertificateRepository
	probes    domain.ProbeResultRepository
	exempts   domain.ExemptionRepository
	alertCfg  domain.AlertConfigRepository
	orders    domain.ChangeOrderRepository
	dialer    tlsDialer
	dnsSource DNSRecordSource // 可空：未装配则 ProbeAllTenantDNS 返回 ErrNoDNSSource
	refs      domain.CertReferenceRepository // 可空：Phase 3 expected 侧（引用扫描指纹）
	snapshots domain.ScanSnapshotRepository    // 可空：配合 refs 取 latest done 快照建引用索引
	probeRunning atomic.Bool                  // 手动触发防重（CompareAndSwap）
}

// ProbeOptions 探测可选依赖（均可空；零值=回退纯台账 SAN 路径）。
type ProbeOptions struct {
	DNS       DNSRecordSource                // DNS 记录源：DNS 源探测
	Refs      domain.CertReferenceRepository // 引用扫描仓储：Phase 3 expected 侧（资源绑定证书指纹）
	Snapshots domain.ScanSnapshotRepository  // 快照仓储：配合 Refs 取 latest done 建引用索引
}

// NewProbeService 创建探测服务。deps 说明：
//   - certs：台账（目标域来源 + 域名→归属证书指纹映射）
//   - probes：探测结果落库
//   - exempts：豁免清单（命中域名仍探测但标 exempt）
//   - alertCfg：wildcardProbeOverrides（通配符→具体探测子域名）
//   - orders：验证中变更单查询（change_linked_diff 关联判定）
//   - dialer：TLS 拨测端口；nil 时使用标准库实现（probeDialTimeout 常量超时）
//   - opts：可选依赖（DNS 源 + 引用扫描 expected 侧）；零值=回退台账 SAN 路径
func NewProbeService(
	certs domain.CertificateRepository,
	probes domain.ProbeResultRepository,
	exempts domain.ExemptionRepository,
	alertCfg domain.AlertConfigRepository,
	orders domain.ChangeOrderRepository,
	dialer tlsDialer,
	opts ProbeOptions,
) ProbeService {
	if dialer == nil {
		dialer = &stdTLSDialer{timeout: probeDialTimeout}
	}
	return &probeService{
		certs:     certs,
		probes:    probes,
		exempts:   exempts,
		alertCfg:  alertCfg,
		orders:    orders,
		dialer:    dialer,
		dnsSource: opts.DNS,
		refs:      opts.Refs,
		snapshots: opts.Snapshots,
	}
}

// ProbeLedgerDomains 目标域 = 台账全部 sans 展开去重（expectedDomain 不参与）。
func (s *probeService) ProbeLedgerDomains(ctx context.Context) ([]domain.ProbeResult, error) {
	certs, err := s.certs.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("probe: list ledger certificates: %w", err)
	}
	seen := make(map[string]bool)
	targets := make([]string, 0, len(certs)*2)
	for _, cert := range certs {
		for _, san := range cert.Sans {
			name := strings.TrimSpace(san)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			targets = append(targets, name)
		}
	}
	return s.ProbeDomains(ctx, targets)
}

// ProbeDomains 单轮探测：目标解析（去重 + 通配符 override 替换）→ 受控并发拨测
// → 六态判定 → 逐域写 ProbeResult。单域名失败（拨测/写库）不中断整轮。
func (s *probeService) ProbeDomains(ctx context.Context, domains []string) ([]domain.ProbeResult, error) {
	// 逐轮一次性读取判定上下文：豁免清单、通配符 override、验证中订单
	exemptions, err := s.exempts.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("probe: list exemptions: %w", err)
	}
	exemptSet := make(map[string]bool, len(exemptions))
	for _, e := range exemptions {
		exemptSet[e.Domain] = true
	}
	cfg, err := s.alertCfg.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("probe: get alert config: %w", err)
	}
	verifying, err := s.orders.ListVerifyingActive(ctx, time.Now())
	if err != nil {
		return nil, fmt.Errorf("probe: list verifying orders: %w", err)
	}
	ownership, err := s.buildOwnership(ctx)
	if err != nil {
		return nil, err
	}

	// 目标解析：通配符 SAN 有 concreteSubdomainOverride → 以子域名替代拨测
	// （结果记于子域名、按常规状态判定）；无 override → 保留通配符（记 wildcard_skipped，不拨测）
	seen := make(map[string]bool, len(domains))
	targets := make([]string, 0, len(domains))
	for _, d := range domains {
		name := strings.TrimSpace(d)
		if name == "" {
			continue
		}
		if isWildcardSAN(name) {
			if sub, ok := cfg.WildcardProbeOverrides[name]; ok {
				name = strings.TrimSpace(sub)
				if name == "" {
					continue
				}
			}
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		targets = append(targets, name)
	}

	// 受控并发拨测（Hard Rule：并发上限常量、单域名失败不中断整轮）
	results := make([]domain.ProbeResult, len(targets))
	sem := make(chan struct{}, probeConcurrency)
	var wg sync.WaitGroup
	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			break
		}
		wg.Add(1)
		go func(idx int, name string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			results[idx] = s.probeOne(name, exemptSet, ownership, verifying)
		}(i, target)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("probe: round cancelled: %w", err)
	}

	// 逐域写库；写失败聚合返回但不中断其余域（单域失败不中断整轮）
	var writeErrs []error
	for i := range results {
		r := results[i]
		if err := s.probes.Create(ctx, &r); err != nil {
			writeErrs = append(writeErrs, fmt.Errorf("probe: persist result for %s: %w", r.Domain, err))
		}
	}
	return results, errors.Join(writeErrs...)
}

// probeOne 单域探测与六态判定（结果不落库，由调用方统一写入）。
func (s *probeService) probeOne(
	name string,
	exemptSet map[string]bool,
	ownership map[string]map[string]bool,
	verifying []domain.ChangeOrder,
) domain.ProbeResult {
	// 通配符 SAN 无法直接 DNS 解析与 SNI 拨测 → 不拨测，记 wildcard_skipped
	//（计数可见、不告警、不计差异；override 替换已在目标解析阶段完成）
	if isWildcardSAN(name) {
		return domain.ProbeResult{Domain: name, Status: domain.ProbeStatusWildcardSkipped}
	}
	leaf, err := s.dialer.Dial(context.Background(), name)
	if err != nil {
		// 拨号失败/DNS 失败/超时 → unreachable（不参与差异告警）
		return domain.ProbeResult{Domain: name, Status: domain.ProbeStatusUnreachable}
	}
	onlineFP := leafFingerprintHex(leaf)
	notAfter := leaf.NotAfter
	result := domain.ProbeResult{
		Domain:            name,
		OnlineFingerprint: onlineFP,
		OnlineNotAfter:    &notAfter,
	}
	switch {
	// 豁免清单命中：仍探测（线上指纹已记录）但不做差异判定、不告警
	case exemptSet[name]:
		result.Status = domain.ProbeStatusExempt
	// 线上指纹与台账该域名任一归属证书一致 → consistent
	//（多证书同域名并存语义按"任一归属证书一致"固定，见测试 TestProbe_MultiCertOwnershipSameDomain）
	case ownership[name][onlineFP]:
		result.Status = domain.ProbeStatusConsistent
	default:
		// 常规 diff → 验证窗口关联判定：活跃验证中订单（verifyWindowUntil > now）
		// 的 verifyExpected 覆盖该域名且预期指纹一致 → change_linked_diff + 关联订单
		if orderID, ok := matchVerifyingOrder(verifying, name, onlineFP); ok {
			result.Status = domain.ProbeStatusChangeLinkedDiff
			result.ChangeOrderID = orderID
		} else {
			result.Status = domain.ProbeStatusDiff
		}
	}
	return result
}

// buildOwnership 台账域名→归属证书指纹集合映射（sans 反查；多证书同域名并存时
// 集合含全部归属指纹）。
func (s *probeService) buildOwnership(ctx context.Context) (map[string]map[string]bool, error) {
	certs, err := s.certs.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("probe: list ledger certificates: %w", err)
	}
	ownership := make(map[string]map[string]bool)
	for _, cert := range certs {
		for _, san := range cert.Sans {
			name := strings.TrimSpace(san)
			if name == "" {
				continue
			}
			if ownership[name] == nil {
				ownership[name] = make(map[string]bool)
			}
			ownership[name][cert.Fingerprint] = true
		}
	}
	return ownership, nil
}

// matchVerifyingOrder change_linked_diff 判定：domain ∈ verifyExpected.domains
// 且 onlineFingerprint == newCertFingerprint 时返回订单 ID（hex）。
func matchVerifyingOrder(orders []domain.ChangeOrder, domainName, onlineFingerprint string) (string, bool) {
	for _, order := range orders {
		expected := order.VerifyExpected
		if expected == nil {
			continue
		}
		if onlineFingerprint != expected.NewCertFingerprint {
			continue
		}
		for _, d := range expected.Domains {
			if d == domainName {
				return order.ID.Hex(), true
			}
		}
	}
	return "", false
}

// isWildcardSAN 通配符 SAN 判定（`*.` 前缀，无法直接 SNI 拨测）。
func isWildcardSAN(name string) bool {
	return strings.HasPrefix(name, "*.")
}

// leafFingerprintHex 线上证书指纹：SHA256(leaf.Raw) 小写 hex，与台账指纹口径一致。
func leafFingerprintHex(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// ==================== DNS 源探测（按租户轮，覆盖通配符证书实际子域名） ====================

// ProbeAllTenantDNS 按租户轮：枚举有 DNS 记录的全部租户，逐租户 ProbeTenantDNS，
// 聚合结果。单租户失败不中断其他租户（记入返回的 err）。
func (s *probeService) ProbeAllTenantDNS(ctx context.Context) ([]domain.ProbeResult, error) {
	if s.dnsSource == nil {
		return nil, ErrNoDNSSource
	}
	tenants, err := s.dnsSource.ListTenantsWithRecords(ctx)
	if err != nil {
		return nil, fmt.Errorf("probe: list dns tenants: %w", err)
	}
	var (
		all     []domain.ProbeResult
		errs    []error
	)
	for _, tid := range tenants {
		if err := ctx.Err(); err != nil {
			break
		}
		results, e := s.ProbeTenantDNS(ctx, tid)
		if e != nil {
			errs = append(errs, fmt.Errorf("probe: tenant %d: %w", tid, e))
			continue
		}
		all = append(all, results...)
	}
	return all, errors.Join(errs...)
}

// TriggerProbeAsync 立即触发一轮 DNS 源探测：后台 goroutine 跑 ProbeAllTenantDNS
// （context.Background，不受请求生命周期影响），立即返回。防重：已在跑返回
// ErrProbeRunning。前端据返回 202 轮询 GET /certs/probes 看新结果（probeAt 推进）。
// dnsSource 未装配返回 ErrNoDNSSource（调用方引导先同步 DNS）。
func (s *probeService) TriggerProbeAsync(ctx context.Context) error {
	if s.dnsSource == nil {
		return ErrNoDNSSource
	}
	if !s.probeRunning.CompareAndSwap(false, true) {
		return ErrProbeRunning
	}
	go func() {
		defer s.probeRunning.Store(false)
		_, _ = s.ProbeAllTenantDNS(context.Background())
	}()
	return nil
}

// ProbeTenantDNS 单租户 DNS 探测：拉该租户全部 TLS-relevant DNS 记录导出的子域名，
// 受控并发 SNI 拨测，通配符感知的台账覆盖判定六态，逐域写 ProbeResult（带
// tenantId/linkedResource）。单域名拨测/写库失败不中断整轮。
func (s *probeService) ProbeTenantDNS(ctx context.Context, tenantID int64) ([]domain.ProbeResult, error) {
	if s.dnsSource == nil {
		return nil, ErrNoDNSSource
	}
	targets, err := s.dnsSource.ListProbeTargets(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("probe: list dns targets for tenant %d: %w", tenantID, err)
	}
	if len(targets) == 0 {
		return nil, nil
	}
	// 逐轮一次性读取判定上下文（同 ProbeDomains）
	exemptions, err := s.exempts.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("probe: list exemptions: %w", err)
	}
	exemptSet := make(map[string]bool, len(exemptions))
	for _, e := range exemptions {
		exemptSet[e.Domain] = true
	}
	verifying, err := s.orders.ListVerifyingActive(ctx, time.Now())
	if err != nil {
		return nil, fmt.Errorf("probe: list verifying orders: %w", err)
	}
	certs, err := s.certs.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("probe: list ledger certificates: %w", err)
	}
	coverage := buildCoverageIndex(certs) // hostname→归属证书指纹索引（精确 + 通配符单标签覆盖）
	// Phase 3 expected 侧：最新成功快照引用扫描解析的"资源→绑定证书指纹"索引。
	// CDN/DCDN/WAF 记录的 hostname == cert_reference.resourceId，按 (product,hostname)
	// 查到引用即用其解析指纹做权威 expected；未装配/无快照/无引用则回退 coverage。
	refIndex := s.buildReferenceIndex(ctx)

	results := make([]domain.ProbeResult, len(targets))
	sem := make(chan struct{}, probeConcurrency)
	var wg sync.WaitGroup
	for i, t := range targets {
		if err := ctx.Err(); err != nil {
			break
		}
		wg.Add(1)
		go func(idx int, tgt dns.ProbeTarget) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			results[idx] = s.probeOneDNS(tgt, exemptSet, coverage, refIndex, verifying)
		}(i, t)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("probe: dns round cancelled: %w", err)
	}

	var writeErrs []error
	for i := range results {
		r := results[i]
		if err := s.probes.Create(ctx, &r); err != nil {
			writeErrs = append(writeErrs, fmt.Errorf("probe: persist dns result for %s: %w", r.Domain, err))
		}
	}
	return results, errors.Join(writeErrs...)
}

// probeOneDNS 单域 DNS 源探测与六态判定（结果不落库，由调用方统一写入）。
// expected 侧分层（Phase 3）：CDN/DCDN/WAF 记录的 hostname 在引用索引命中时，用引用
// 扫描解析的"该资源绑定证书指纹"做权威 expected（资源级精度）；无引用/external/ALB
// 回退 coverageIndex（台账 SAN 通配符感知覆盖）。
func (s *probeService) probeOneDNS(
	tgt dns.ProbeTarget,
	exemptSet map[string]bool,
	coverage *coverageIndex,
	refIndex map[string]map[string]bool,
	verifying []domain.ChangeOrder,
) domain.ProbeResult {
	result := domain.ProbeResult{
		Domain:         tgt.Hostname,
		TenantID:       tgt.TenantID,
		LinkedResource: linkedResourceType(tgt.LinkedResource),
	}
	leaf, err := s.dialer.Dial(context.Background(), tgt.Hostname)
	if err != nil {
		result.Status = domain.ProbeStatusUnreachable
		return result
	}
	onlineFP := leafFingerprintHex(leaf)
	notAfter := leaf.NotAfter
	result.OnlineFingerprint = onlineFP
	result.OnlineNotAfter = &notAfter
	// expected 判定：豁免 > 引用索引（资源级权威）> 台账覆盖（SAN 通配符感知）
	switch {
	case exemptSet[tgt.Hostname]:
		result.Status = domain.ProbeStatusExempt
	case refIndexMatches(refIndex, tgt.LinkedResource, tgt.Hostname, onlineFP):
		result.Status = domain.ProbeStatusConsistent
	case coverage.covers(tgt.Hostname, onlineFP):
		result.Status = domain.ProbeStatusConsistent
	default:
		if orderID, ok := matchVerifyingOrder(verifying, tgt.Hostname, onlineFP); ok {
			result.Status = domain.ProbeStatusChangeLinkedDiff
			result.ChangeOrderID = orderID
		} else {
			result.Status = domain.ProbeStatusDiff
		}
	}
	return result
}

// refIndexMatches 引用索引是否命中并指纹一致。仅 CDN/DCDN/WAF 类 linked_resource
// 的 hostname 能与 cert_reference.resourceId 对齐（== hostname）；key=product|hostname。
// 若索引含该 key 但 onlineFP 不在其指纹集合 → 返回 false（由调用方落 diff，
// 即"该资源绑了别的证书"——资源级漂移）；索引无该 key → 返回 false 走 coverage 回退。
func refIndexMatches(refIndex map[string]map[string]bool, lr *dns.LinkedResource, hostname, onlineFP string) bool {
	if refIndex == nil || lr == nil {
		return false
	}
	product := ""
	switch lr.Type {
	case "cdn", "dcdn", "waf":
		product = lr.Type
	case "external":
		// external A 记录可能指向 ALB/NLB：查 alb/nlb 两个 product 的 served domain 索引
		for _, p := range []string{"alb", "nlb"} {
			if fps := refIndex[p+"|"+hostname]; len(fps) > 0 && fps[onlineFP] {
				return true
			}
		}
		return false
	default:
		return false // nil/未知：回退 coverage
	}
	fps := refIndex[product+"|"+hostname]
	return len(fps) > 0 && fps[onlineFP]
}

// buildReferenceIndex 从最新成功快照的引用扫描结果建 "product|hostname → 指纹集合" 索引：
//   - CDN/DCDN/WAF：resourceId 即域名，直接入键
//   - ALB/NLB：resourceId 为监听复合 ID（非域名），改按 ServedDomains（监听规则提取的
//     served hostname）展开入键——external DNS 记录（A→ALB IP）的 hostname 经此对齐
//
// 跳过占位指纹（certscan-unresolved: 前缀，未解析为台账指纹，不作权威 expected）。
// refs/snapshots 未装配或无成功快照时返回 nil（调用方回退 coverage）。
func (s *probeService) buildReferenceIndex(ctx context.Context) map[string]map[string]bool {
	if s.refs == nil || s.snapshots == nil {
		return nil
	}
	snap, err := s.snapshots.LatestDone(ctx)
	if err != nil {
		return nil // 无成功快照（含 mongo.ErrNoDocuments）→ 无引用可参考
	}
	refs, err := s.refs.ListBySnapshotID(ctx, snap.ID.Hex())
	if err != nil || len(refs) == 0 {
		return nil
	}
	idx := make(map[string]map[string]bool)
	register := func(product, hostname, fp string) {
		if hostname == "" {
			return
		}
		key := product + "|" + hostname
		if idx[key] == nil {
			idx[key] = make(map[string]bool)
		}
		idx[key][fp] = true
	}
	for _, r := range refs {
		if r.CertFingerprint == "" || isPlaceholderFingerprint(r.CertFingerprint) {
			continue
		}
		product := string(r.Product)
		switch product {
		case "cdn", "dcdn", "waf":
			register(product, r.ResourceID, r.CertFingerprint)
		case "alb", "nlb":
			// resourceId 为监听复合 ID（非域名），按 ServedDomains 展开
			for _, h := range r.ServedDomains {
				register(product, h, r.CertFingerprint)
			}
		}
	}
	return idx
}

// isPlaceholderFingerprint 占位指纹判定（扫描侧确定性占位公式，未解析为台账指纹）。
func isPlaceholderFingerprint(fp string) bool {
	return strings.HasPrefix(fp, "certscan-unresolved:")
}

// coverageIndex 台账域名→归属证书指纹的通配符感知索引：
//   - exact：精确 SAN hostname → 指纹集合（O(1) 查）
//   - wildcards：通配符 SAN (base, fp) 列表，按单标签覆盖匹配（O(W) 查）
//
// DNS 源 hostname（如 www.example.com）即使不是任何精确 SAN，仍能经 wildcards
// 匹配通配符证书 *.example.com 的归属，避免通配符子域名被误判 diff。
type coverageIndex struct {
	exact     map[string]map[string]bool
	wildcards []sanWildcard
}

type sanWildcard struct {
	base string // *.example.com → example.com
	fp   string
}

func buildCoverageIndex(certs []domain.Certificate) *coverageIndex {
	ci := &coverageIndex{exact: make(map[string]map[string]bool)}
	for _, c := range certs {
		for _, san := range c.Sans {
			name := strings.TrimSpace(san)
			if name == "" {
				continue
			}
			if rest, ok := strings.CutPrefix(name, "*."); ok && rest != "" {
				ci.wildcards = append(ci.wildcards, sanWildcard{base: rest, fp: c.Fingerprint})
				continue
			}
			if ci.exact[name] == nil {
				ci.exact[name] = make(map[string]bool)
			}
			ci.exact[name][c.Fingerprint] = true
		}
	}
	return ci
}

// covers hostname 是否被台账某归属证书覆盖且指纹为 fp（精确命中优先，通配符单标签覆盖次之）。
func (ci *coverageIndex) covers(hostname, fp string) bool {
	if fps := ci.exact[hostname]; fps[fp] {
		return true
	}
	for _, w := range ci.wildcards {
		if w.fp == fp && coversSingleLabel(hostname, w.base) {
			return true
		}
	}
	return false
}

// coversSingleLabel hostname 是否为 `<label>.<base>` 且 label 不含 "."（通配符单标签覆盖）。
func coversSingleLabel(hostname, base string) bool {
	tail := "." + strings.ToLower(base)
	lower := strings.ToLower(hostname)
	if !strings.HasSuffix(lower, tail) {
		return false
	}
	label := strings.TrimSuffix(lower, tail)
	return label != "" && !strings.Contains(label, ".")
}

// linkedResourceType 提取 dns.LinkedResource.Type（cdn/waf/external）；nil 时空串。
func linkedResourceType(lr *dns.LinkedResource) string {
	if lr == nil {
		return ""
	}
	return lr.Type
}
