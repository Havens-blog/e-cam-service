package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
)

// probeRepoStub 探测结果仓储桩：可控 LatestPerDomain 返回与错误。
type probeRepoStub struct {
	latest []domain.ProbeResult
	err    error
}

func (p *probeRepoStub) Create(context.Context, *domain.ProbeResult) error { return nil }
func (p *probeRepoStub) LatestByDomain(context.Context, string) (domain.ProbeResult, error) {
	return domain.ProbeResult{}, nil
}
func (p *probeRepoStub) LatestPerDomain(context.Context) ([]domain.ProbeResult, error) {
	return p.latest, p.err
}
func (p *probeRepoStub) ListByDomains(context.Context, []string, int) ([]domain.ProbeResult, error) {
	return nil, nil
}

func (p *probeRepoStub) ListRecentByDomains(context.Context, []string, int) ([]domain.ProbeResult, error) {
	return nil, nil
}

// TestInternalDomainSet 决策口径：只有 status=internal 进入集合；大小写归一；
// 读失败返回空集 + error（调用方保守退化）。
func TestInternalDomainSet(t *testing.T) {
	t.Run("筛选 internal 并归一大小写", func(t *testing.T) {
		svc := &probeService{probes: &probeRepoStub{latest: []domain.ProbeResult{
			{Domain: "Dev-Cart.Example.com", Status: domain.ProbeStatusInternal},
			{Domain: "www.example.com", Status: domain.ProbeStatusConsistent},
			{Domain: "diff.example.com", Status: domain.ProbeStatusDiff},
			{Domain: "dev-app.example.com", Status: domain.ProbeStatusInternal},
		}}}
		set, err := svc.internalDomainSet(context.Background())
		assert.NoError(t, err)
		assert.Len(t, set, 2)
		assert.True(t, set["dev-cart.example.com"], "集合键统一小写")
		assert.True(t, set["dev-app.example.com"])
	})

	t.Run("仓储读失败：空集 + error", func(t *testing.T) {
		svc := &probeService{probes: &probeRepoStub{err: errors.New("mongo down")}}
		set, err := svc.internalDomainSet(context.Background())
		assert.Error(t, err)
		assert.Empty(t, set)
	})
}

// TestDropInternalTargets 日轮排除：只在集合内的目标被剔除；读取失败保守返回原集合。
func TestDropInternalTargets(t *testing.T) {
	targets := []DNSProbeTarget{
		{Hostname: "www.example.com"},
		{Hostname: "dev-cart.example.com"},
		{Hostname: "DEV-APP.example.com"}, // 大小写不同仍应剔除
	}

	t.Run("剔除 internal 目标", func(t *testing.T) {
		svc := &probeService{probes: &probeRepoStub{latest: []domain.ProbeResult{
			{Domain: "dev-cart.example.com", Status: domain.ProbeStatusInternal},
			{Domain: "dev-app.example.com", Status: domain.ProbeStatusInternal},
			{Domain: "www.example.com", Status: domain.ProbeStatusConsistent},
		}}}
		kept := svc.dropInternalTargets(context.Background(), targets)
		assert.Len(t, kept, 1)
		assert.Equal(t, "www.example.com", kept[0].Hostname)
	})

	t.Run("读取失败：保守返回全部目标（宁可多拨不漏拨）", func(t *testing.T) {
		svc := &probeService{probes: &probeRepoStub{err: errors.New("boom")}}
		kept := svc.dropInternalTargets(context.Background(), targets)
		assert.Len(t, kept, len(targets))
	})

	t.Run("无 internal：原样返回", func(t *testing.T) {
		svc := &probeService{probes: &probeRepoStub{latest: []domain.ProbeResult{
			{Domain: "www.example.com", Status: domain.ProbeStatusConsistent},
		}}}
		kept := svc.dropInternalTargets(context.Background(), targets)
		assert.Len(t, kept, len(targets))
	})

	t.Run("不改动入参切片底层数组", func(t *testing.T) {
		svc := &probeService{probes: &probeRepoStub{latest: []domain.ProbeResult{
			{Domain: "dev-cart.example.com", Status: domain.ProbeStatusInternal},
		}}}
		in := append([]DNSProbeTarget{}, targets...)
		_ = svc.dropInternalTargets(context.Background(), in)
		assert.Equal(t, "www.example.com", in[0].Hostname, "入参顺序不得被原地覆盖")
		assert.Len(t, in, 3)
	})
}

// TestProbeInternalDomains 周级轮：集合为空时不触发任何拨测。
func TestProbeInternalDomains(t *testing.T) {
	d := &fakeClassifyDialer{remoteIP: "10.25.1.10"}
	svc := &probeService{dialer: d, probes: &probeRepoStub{latest: []domain.ProbeResult{
		{Domain: "www.example.com", Status: domain.ProbeStatusConsistent},
	}}}
	results, err := svc.ProbeInternalDomains(context.Background())
	assert.NoError(t, err)
	assert.Empty(t, results)
	assert.Equal(t, 0, d.called, "无内网域时不得拨测")
}
