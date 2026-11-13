package service

import (
	"context"
	"crypto/x509"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
)

// fakeClassifyDialer 记录是否被调用，并返回带 RemoteIP 的握手产物。
type fakeClassifyDialer struct {
	remoteIP string
	called   int
	leaf     *x509.Certificate
}

func (f *fakeClassifyDialer) Dial(context.Context, string) (dialResult, error) {
	f.called++
	leaf := f.leaf
	if leaf == nil {
		leaf = &x509.Certificate{NotAfter: time.Now().Add(90 * 24 * time.Hour)}
	}
	return dialResult{Leaf: leaf, TLSVersion: "TLS 1.3", RemoteIP: f.remoteIP}, nil
}

// TestHasUnderscoreLabel 协议记录判定：任一标签下划线开头即命中。
func TestHasUnderscoreLabel(t *testing.T) {
	testCases := []struct {
		name     string
		hostname string
		want     bool
	}{
		{"首标签下划线（ACME DNS-01）", "_acme-challenge.example.com", true},
		{"中段下划线（DKIM，生产漏网形态）", "strong2._domainkey.jlcpcb.com", true},
		{"尾段下划线", "selector.dkim._spf.example.com", true},
		{"正常域名", "www.example.com", false},
		{"含下划线但非标签前缀", "under_score.example.com", false},
		{"空串", "", false},
		{"单标签下划线", "_dmarc", true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, hasUnderscoreLabel(tc.hostname))
		})
	}
}

// TestIsPrivateProbeIP 内网地址判定（RFC1918 / 100.64/10 / 链路本地 / 回环 / IPv6 私网）。
func TestIsPrivateProbeIP(t *testing.T) {
	testCases := []struct {
		name string
		ip   string
		want bool
	}{
		{"10/8", "10.25.1.10", true},
		{"172.16/12 下界", "172.16.0.1", true},
		{"172.16/12 上界", "172.31.255.254", true},
		{"172.32 不在私网段", "172.32.0.1", false},
		{"192.168/16", "192.168.2.39", true},
		{"100.64/10 运营商 NAT", "100.127.0.1", true},
		{"100.128 不在 100.64/10", "100.128.0.1", false},
		{"169.254 链路本地", "169.254.1.1", true},
		{"127 回环", "127.0.0.1", true},
		{"IPv6 唯一本地", "fd00::1", true},
		{"IPv6 链路本地", "fe80::1", true},
		{"IPv6 回环", "::1", true},
		{"公网 IPv4", "113.96.128.9", false},
		{"公网 IPv6", "2400:3200::1", false},
		{"空串保守归公网", "", false},
		{"非法 IP 保守归公网", "not-an-ip", false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isPrivateProbeIP(tc.ip))
		})
	}
}

// TestProbeOne_Classification 探测路径分类：
// 协议记录不拨测（protocol_skipped）；对端私网地址归 internal（不是 unreachable）。
func TestProbeOne_Classification(t *testing.T) {
	t.Run("中段下划线标签 → protocol_skipped 且不拨测", func(t *testing.T) {
		d := &fakeClassifyDialer{remoteIP: "113.96.128.9"}
		svc := &probeService{dialer: d}
		r := svc.probeOne("strong2._domainkey.example.com", nil, nil, nil, nil)
		assert.Equal(t, domain.ProbeStatusProtocolSkipped, r.Status)
		assert.Equal(t, 0, d.called, "协议记录不得触发拨测")
	})

	t.Run("对端私网 IP → internal（记录指纹但不判差异）", func(t *testing.T) {
		d := &fakeClassifyDialer{remoteIP: "10.25.1.10"}
		svc := &probeService{dialer: d}
		r := svc.probeOne("dev-app.example.com", nil, nil, nil, nil)
		assert.Equal(t, domain.ProbeStatusInternal, r.Status)
		assert.NotEmpty(t, r.OnlineFingerprint, "内网域仍记录线上指纹（到期监控用）")
	})

	t.Run("对端公网 IP → 走常规判定（无归属证书命中则 diff）", func(t *testing.T) {
		d := &fakeClassifyDialer{remoteIP: "113.96.128.9"}
		svc := &probeService{dialer: d}
		r := svc.probeOne("www.example.com", nil, map[string]map[string]bool{}, nil, nil)
		assert.Equal(t, domain.ProbeStatusDiff, r.Status)
	})

	t.Run("DNS 源：协议记录 → protocol_skipped", func(t *testing.T) {
		d := &fakeClassifyDialer{remoteIP: "113.96.128.9"}
		svc := &probeService{dialer: d}
		r := svc.probeOneDNS(DNSProbeTarget{Hostname: "_hash.example.com", RecordType: "A"}, nil, nil, nil, nil, nil)
		assert.Equal(t, domain.ProbeStatusProtocolSkipped, r.Status)
		assert.Equal(t, 0, d.called)
	})

	t.Run("DNS 源：对端私网 IP → internal 且保留链路字段", func(t *testing.T) {
		d := &fakeClassifyDialer{remoteIP: "192.168.2.39"}
		svc := &probeService{dialer: d}
		tgt := DNSProbeTarget{Hostname: "dev-cart.example.com", RecordType: "A", TenantID: 3}
		r := svc.probeOneDNS(tgt, nil, nil, nil, nil, nil)
		assert.Equal(t, domain.ProbeStatusInternal, r.Status)
		assert.Equal(t, int64(3), r.TenantID)
	})
}
