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
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
)

// 探测节奏常量（Hard Rule：单域名拨测超时与整轮并发受控）。
const (
	// probeTLSPort 探测目标端口（TLS 服务端点约定端口）。
	probeTLSPort = "443"
	// probeDialTimeout 单域名拨测+握手超时（常量化，Hard Rule）。
	probeDialTimeout = 5 * time.Second
	// probeConcurrency 整轮批量探测并发上限（Hard Rule：整体批量并发受控）。
	probeConcurrency = 8
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
}

type probeService struct {
	certs    domain.CertificateRepository
	probes   domain.ProbeResultRepository
	exempts  domain.ExemptionRepository
	alertCfg domain.AlertConfigRepository
	orders   domain.ChangeOrderRepository
	dialer   tlsDialer
}

// NewProbeService 创建探测服务。deps 说明：
//   - certs：台账（目标域来源 + 域名→归属证书指纹映射）
//   - probes：探测结果落库
//   - exempts：豁免清单（命中域名仍探测但标 exempt）
//   - alertCfg：wildcardProbeOverrides（通配符→具体探测子域名）
//   - orders：验证中变更单查询（change_linked_diff 关联判定）
//   - dialer：TLS 拨测端口；nil 时使用标准库实现（probeDialTimeout 常量超时）
func NewProbeService(
	certs domain.CertificateRepository,
	probes domain.ProbeResultRepository,
	exempts domain.ExemptionRepository,
	alertCfg domain.AlertConfigRepository,
	orders domain.ChangeOrderRepository,
	dialer tlsDialer,
) ProbeService {
	if dialer == nil {
		dialer = &stdTLSDialer{timeout: probeDialTimeout}
	}
	return &probeService{
		certs:    certs,
		probes:   probes,
		exempts:  exempts,
		alertCfg: alertCfg,
		orders:   orders,
		dialer:   dialer,
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
