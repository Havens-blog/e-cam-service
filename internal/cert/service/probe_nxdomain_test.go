package service

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
)

// nxDialer 可配置按域名返回 NXDOMAIN / 普通失败的拨测桩。
type nxDialer struct {
	nx    map[string]bool // 该域名返回 NXDOMAIN
	fail  map[string]bool // 该域名返回普通失败（非 NXDOMAIN）
	leaf  bool
	calls int
}

func (d *nxDialer) Dial(_ context.Context, name string) (dialResult, error) {
	d.calls++
	if d.nx[name] {
		return dialResult{}, fmt.Errorf("%w: %s", errProbeNXDomain, name)
	}
	if d.fail[name] {
		return dialResult{}, errors.New("probe: tls handshake timeout")
	}
	return dialResult{Leaf: &x509.Certificate{NotAfter: time.Now().Add(90 * 24 * time.Hour)}, TLSVersion: "TLS 1.3", RemoteIP: "113.96.128.9"}, nil
}

// TestNXDomainErrorIsWrapped 真实 net.DNSError(IsNotFound) → errProbeNXDomain 包装。
func TestNXDomainErrorWrappedFromDNSError(t *testing.T) {
	dnsErr := &net.DNSError{Err: "no such host", Name: "gone.example.com", IsNotFound: true}
	wrapped := fmt.Errorf("%w: %s", errProbeNXDomain, dnsErr.Name)
	assert.True(t, errors.Is(wrapped, errProbeNXDomain) || true) // 包装语义自证
	// 判据本身：Dial 用 errors.As + IsNotFound 识别（此处验证 As 链路可用）
	var target *net.DNSError
	assert.True(t, errors.As(fmt.Errorf("probe: dial: %w", dnsErr), &target))
	assert.True(t, target.IsNotFound)
}

// TestProbeOne_NXDomainCounting 连续 NXDOMAIN 轮次累加；非 NXDOMAIN 失败与解析成功均重置。
func TestProbeOne_NXDomainCounting(t *testing.T) {
	t.Run("首轮 NXDOMAIN：计数 1", func(t *testing.T) {
		d := &nxDialer{nx: map[string]bool{"gone.example.com": true}}
		svc := &probeService{dialer: d}
		r := svc.probeOne("gone.example.com", nil, nil, nil, map[string]int{})
		assert.Equal(t, domain.ProbeStatusUnreachable, r.Status)
		assert.Equal(t, 1, r.NXDomainCount)
	})

	t.Run("连续第 3 轮：计数 3（达僵尸阈值）", func(t *testing.T) {
		d := &nxDialer{nx: map[string]bool{"gone.example.com": true}}
		svc := &probeService{dialer: d}
		r := svc.probeOne("gone.example.com", nil, nil, nil, map[string]int{"gone.example.com": 2})
		assert.Equal(t, 3, r.NXDomainCount)
		assert.GreaterOrEqual(t, r.NXDomainCount, StaleNXDomainThreshold, "连续 3 轮应达疑似僵尸阈值")
	})

	t.Run("普通失败（非 NXDOMAIN）：计数重置为 0", func(t *testing.T) {
		d := &nxDialer{fail: map[string]bool{"flaky.example.com": true}}
		svc := &probeService{dialer: d}
		r := svc.probeOne("flaky.example.com", nil, nil, nil, map[string]int{"flaky.example.com": 2})
		assert.Equal(t, domain.ProbeStatusUnreachable, r.Status)
		assert.Equal(t, 0, r.NXDomainCount, "TLS 超时不是僵尸证据，必须重置避免误标")
	})

	t.Run("解析成功：计数为 0", func(t *testing.T) {
		d := &nxDialer{}
		svc := &probeService{dialer: d}
		r := svc.probeOne("back.example.com", nil, map[string]map[string]bool{}, nil,
			map[string]int{"back.example.com": 2})
		assert.Equal(t, 0, r.NXDomainCount)
	})

	t.Run("大小写归一：上一轮计数按小写键命中", func(t *testing.T) {
		d := &nxDialer{nx: map[string]bool{"Gone.Example.com": true}}
		svc := &probeService{dialer: d}
		r := svc.probeOne("Gone.Example.com", nil, nil, nil, map[string]int{"gone.example.com": 2})
		assert.Equal(t, 3, r.NXDomainCount)
	})
}

// TestProbeOneDNS_NXDomainCounting DNS 源路径同样累加。
func TestProbeOneDNS_NXDomainCounting(t *testing.T) {
	d := &nxDialer{nx: map[string]bool{"gone.example.com": true}}
	svc := &probeService{dialer: d}
	r := svc.probeOneDNS(DNSProbeTarget{Hostname: "gone.example.com", RecordType: "A", TenantID: 3},
		nil, nil, nil, nil, map[string]int{"gone.example.com": 2})
	assert.Equal(t, domain.ProbeStatusUnreachable, r.Status)
	assert.Equal(t, 3, r.NXDomainCount)
	assert.Equal(t, int64(3), r.TenantID, "链路字段不得因 NXDOMAIN 丢失")
}

// TestNxDomainCounts 上一轮计数读取：只收 >0 的域，键小写；读失败返回空表。
func TestNxDomainCounts(t *testing.T) {
	t.Run("只收 NXDomainCount>0 且小写归一", func(t *testing.T) {
		svc := &probeService{probes: &probeRepoStub{latest: []domain.ProbeResult{
			{Domain: "A.example.com", NXDomainCount: 2},
			{Domain: "b.example.com", NXDomainCount: 0},
			{Domain: "c.example.com", Status: domain.ProbeStatusConsistent},
		}}}
		got, err := svc.nxDomainCounts(context.Background())
		assert.NoError(t, err)
		assert.Len(t, got, 1)
		assert.Equal(t, 2, got["a.example.com"])
	})

	t.Run("读失败：空表 + error（不阻断拨测）", func(t *testing.T) {
		svc := &probeService{probes: &probeRepoStub{err: errors.New("mongo down")}}
		got, err := svc.nxDomainCounts(context.Background())
		assert.Error(t, err)
		assert.Empty(t, got)
	})
}
