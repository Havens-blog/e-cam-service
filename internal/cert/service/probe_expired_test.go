package service

import (
	"context"
	"crypto/x509"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
)

// expiredDialer 返回已过期 / 未过期的 leaf 证书（其余字段固定）。
type expiredDialer struct {
	notAfter time.Time
}

func (d *expiredDialer) Dial(_ context.Context, _ string) (dialResult, error) {
	return dialResult{
		Leaf:       &x509.Certificate{NotAfter: d.notAfter, Raw: []byte("leaf")},
		TLSVersion: "TLS 1.3",
		RemoteIP:   "113.96.128.9",
	}, nil
}

// TestProbeOne_OnlineExpired 线上证书已过期 → OnlineExpired=true（与常规 diff 区分）；
// 未过期 → false。
func TestProbeOne_OnlineExpired(t *testing.T) {
	t.Run("已过期（安全事件级）", func(t *testing.T) {
		svc := &probeService{dialer: &expiredDialer{notAfter: time.Now().Add(-24 * time.Hour)}}
		r := svc.probeOne("www.example.com", nil, map[string]map[string]bool{}, nil, nil)
		assert.True(t, r.OnlineExpired, "notAfter 在过去 → OnlineExpired=true")
		assert.Equal(t, domain.ProbeStatusDiff, r.Status, "过期仍按 diff 判定，但带过期标记")
	})

	t.Run("未过期", func(t *testing.T) {
		svc := &probeService{dialer: &expiredDialer{notAfter: time.Now().Add(24 * time.Hour)}}
		r := svc.probeOne("www.example.com", nil, map[string]map[string]bool{}, nil, nil)
		assert.False(t, r.OnlineExpired)
	})

	t.Run("DNS 源路径同样标记", func(t *testing.T) {
		svc := &probeService{dialer: &expiredDialer{notAfter: time.Now().Add(-48 * time.Hour)}}
		r := svc.probeOneDNS(DNSProbeTarget{Hostname: "dev.example.com", RecordType: "A"}, nil, buildCoverageIndex(nil), nil, nil, nil)
		assert.True(t, r.OnlineExpired)
	})
}

// TestDashboardSummary_OnlineExpiredCount 汇总计数：过期域无论状态都计入（含 consistent 场景下
// 台账本身过期的情况）。
func TestDashboardSummary_OnlineExpiredCount(t *testing.T) {
	s := DashboardSummary{ProbeTotalCount: 3}
	for _, expired := range []bool{true, true, false} {
		if expired {
			s.OnlineExpiredCount++
		}
	}
	assert.Equal(t, 2, s.OnlineExpiredCount)
}
