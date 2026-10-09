package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Havens-blog/e-cloudx-sdk/logquery"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
)

// ---------------------------------------------------------------------
// Search/Aggregate 结果缓存(热查询 <300ms;SWR:新鲜窗直返,过期宽限内
// 供旧+后台刷新,见 proposal «Selected» 与任务实现注记 3)
// ---------------------------------------------------------------------

// countingProvider 带 Search 调用计数的 provider(验证缓存命中不重复触发
// 真实查询);countingAggregator 增加 Aggregate 计数。
type countingProvider struct {
	fakeProvider
	searchCalls atomic.Int64
}

func (p *countingProvider) Search(ctx context.Context, acc *domain.CloudAccount, params logquery.SearchParams) ([]logquery.LogEntry, error) {
	p.searchCalls.Add(1)
	return p.fakeProvider.Search(ctx, acc, params)
}

type countingAggregator struct {
	fakeAggregator
	searchCalls    atomic.Int64
	aggregateCalls atomic.Int64
}

func (p *countingAggregator) Search(ctx context.Context, acc *domain.CloudAccount, params logquery.SearchParams) ([]logquery.LogEntry, error) {
	p.searchCalls.Add(1)
	return p.fakeProvider.Search(ctx, acc, params)
}

func (p *countingAggregator) Aggregate(ctx context.Context, acc *domain.CloudAccount, params logquery.AggregateParams) (*logquery.AggregateResult, error) {
	p.aggregateCalls.Add(1)
	return p.fakeAggregator.Aggregate(ctx, acc, params)
}

func newCountingCloud(t *testing.T, name domain.CloudProvider, entries []logquery.LogEntry) *countingProvider {
	t.Helper()
	p := &countingProvider{fakeProvider: fakeProvider{
		cloud: name, logType: logquery.LogTypeCDN, entries: entries, ignoreLimit: true,
	}}
	logquery.RegisterProvider(name, logquery.LogTypeCDN, func(*domain.CloudAccount) (logquery.LogProvider, error) {
		return p, nil
	})
	return p
}

func searchReq() SearchRequest {
	return SearchRequest{LogType: logquery.LogTypeCDN, StartTime: 1000, EndTime: 2000}
}

// TestSearchResultCacheFreshHit 相同请求第二次命中结果缓存:不再触发 provider
// 查询,响应标注 Cached。
func TestSearchResultCacheFreshHit(t *testing.T) {
	const cloud domain.CloudProvider = "cache-srch-1"
	p := newCountingCloud(t, cloud, []logquery.LogEntry{&testEntry{ts: 1500}})
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, cloud),
	}}, nil)
	r1, err := svc.Search(context.Background(), 3, searchReq())
	if err != nil {
		t.Fatal(err)
	}
	if r1.Cached {
		t.Error("first search must be a cache miss")
	}
	r2, err := svc.Search(context.Background(), 3, searchReq())
	if err != nil {
		t.Fatal(err)
	}
	if p.searchCalls.Load() != 1 {
		t.Errorf("provider calls=%d, want 1(缓存命中不再查询)", p.searchCalls.Load())
	}
	if !r2.Cached || r2.CacheStale {
		t.Errorf("second search: cached=%v stale=%v, want true/false", r2.Cached, r2.CacheStale)
	}
	if r2.Total != r1.Total || len(r2.Entries) != len(r1.Entries) {
		t.Errorf("cached response diverges: total %d vs %d", r2.Total, r1.Total)
	}
	// 不同请求参数 = 不同键,必须 miss
	other := searchReq()
	other.StartTime = 1100
	if _, err := svc.Search(context.Background(), 3, other); err != nil {
		t.Fatal(err)
	}
	if p.searchCalls.Load() != 2 {
		t.Errorf("distinct request must miss cache, calls=%d", p.searchCalls.Load())
	}
}

// TestSearchResultCacheStaleServe 宽限期内供旧结果+后台刷新,刷新落定后
// 恢复新鲜命中(不阻塞请求)。
func TestSearchResultCacheStaleServe(t *testing.T) {
	const cloud domain.CloudProvider = "cache-srch-2"
	p := newCountingCloud(t, cloud, []logquery.LogEntry{&testEntry{ts: 1500}})
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, cloud),
	}}, nil)
	if _, err := svc.Search(context.Background(), 3, searchReq()); err != nil {
		t.Fatal(err)
	}
	key := cacheKey("search", 3, searchReq())
	svc.cache.forceStale(key)

	r2, err := svc.Search(context.Background(), 3, searchReq())
	if err != nil {
		t.Fatal(err)
	}
	if !r2.Cached || !r2.CacheStale {
		t.Errorf("stale serve: cached=%v stale=%v", r2.Cached, r2.CacheStale)
	}
	if p.searchCalls.Load() != 1 {
		t.Errorf("stale serve must not block on compute, calls=%d", p.searchCalls.Load())
	}
	// 等后台刷新落定
	deadline := time.Now().Add(2 * time.Second)
	for p.searchCalls.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("background refresh did not run")
		}
		time.Sleep(10 * time.Millisecond)
	}
	r3, err := svc.Search(context.Background(), 3, searchReq())
	if err != nil {
		t.Fatal(err)
	}
	if !r3.Cached || r3.CacheStale {
		t.Errorf("after refresh: cached=%v stale=%v, want true/false", r3.Cached, r3.CacheStale)
	}
	if p.searchCalls.Load() != 2 {
		t.Errorf("calls=%d, want 2", p.searchCalls.Load())
	}
}

// TestAggregateResultCacheFreshHit 聚合同参重复命中结果缓存。
func TestAggregateResultCacheFreshHit(t *testing.T) {
	const cloud domain.CloudProvider = "cache-agg-1"
	p := &countingAggregator{fakeAggregator: fakeAggregator{fakeProvider: fakeProvider{cloud: cloud, logType: logquery.LogTypeCDN}}}
	logquery.RegisterProvider(cloud, logquery.LogTypeCDN, func(*domain.CloudAccount) (logquery.LogProvider, error) {
		return p, nil
	})
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, cloud),
	}}, nil)
	req := AggregateRequest{LogType: logquery.LogTypeCDN, StartTime: 1000, EndTime: 2000}
	r1, err := svc.Aggregate(context.Background(), 3, req)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Cached {
		t.Error("first aggregate must be a cache miss")
	}
	r2, err := svc.Aggregate(context.Background(), 3, req)
	if err != nil {
		t.Fatal(err)
	}
	if p.aggregateCalls.Load() != 1 {
		t.Errorf("aggregate calls=%d, want 1", p.aggregateCalls.Load())
	}
	if !r2.Cached || r2.CacheStale {
		t.Errorf("second aggregate: cached=%v stale=%v", r2.Cached, r2.CacheStale)
	}
	if r2.Total != r1.Total {
		t.Errorf("cached total %d != %d", r2.Total, r1.Total)
	}
}

// TestResultCacheTenantIsolated 缓存键必须包含租户(相同请求参数不同租户
// 不得互串;账号源按租户过滤,租户不同账号集不同)。
func TestResultCacheTenantIsolated(t *testing.T) {
	if cacheKey("search", 3, searchReq()) == cacheKey("search", 7, searchReq()) {
		t.Error("cache key must differ across tenants")
	}
	if cacheKey("search", 3, searchReq()) == cacheKey("aggregate", 3, AggregateRequest{}) {
		t.Error("cache key must differ across endpoints")
	}
}
