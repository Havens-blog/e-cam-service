package service

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-cloudx-sdk/logquery"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
)

// ---------------------------------------------------------------------
// 测试桩:账号源 + provider
// ---------------------------------------------------------------------

type fakeAccountSource struct {
	accounts []domain.CloudAccount
}

func (f *fakeAccountSource) List(_ context.Context, filter domain.CloudAccountFilter) ([]domain.CloudAccount, int64, error) {
	var out []domain.CloudAccount
	for _, a := range f.accounts {
		if filter.Provider != "" && a.Provider != filter.Provider {
			continue
		}
		if filter.Status != "" && a.Status != filter.Status {
			continue
		}
		if filter.TenantID != 0 && a.TenantID != filter.TenantID {
			continue
		}
		out = append(out, a)
	}
	return out, int64(len(out)), nil
}

// fakeProvider 可编程 provider:返回固定条目或错误。
type fakeProvider struct {
	cloud       domain.CloudProvider
	logType     logquery.LogType
	entries     []logquery.LogEntry
	err         error
	listed      []logquery.LogSource
	listErr     error
	ignoreLimit bool // 模拟多内部源归并总量超限的 provider(不做每源截断)
	// onListSources 预热等场景的调用计数钩子(可 nil)。
	onListSources func()
}

func (p *fakeProvider) Cloud() domain.CloudProvider { return p.cloud }
func (p *fakeProvider) LogType() logquery.LogType   { return p.logType }
func (p *fakeProvider) ListLogSources(_ context.Context, _ *domain.CloudAccount) ([]logquery.LogSource, error) {
	if p.onListSources != nil {
		p.onListSources()
	}
	return p.listed, p.listErr
}
func (p *fakeProvider) Search(_ context.Context, _ *domain.CloudAccount, params logquery.SearchParams) ([]logquery.LogEntry, error) {
	if p.err != nil {
		return nil, p.err
	}
	// 模拟每日志源上限截断行为(ignoreLimit 除外)
	if !p.ignoreLimit && len(p.entries) > params.Limit {
		return p.entries[:params.Limit], nil
	}
	return p.entries, nil
}

// testEntry 最小 LogEntry 实现。
type testEntry struct {
	ts   int64
	meta logquery.LogMeta
}

func (e *testEntry) GetTimestamp() int64       { return e.ts }
func (e *testEntry) GetMeta() logquery.LogMeta { return e.meta }

// testCloud 测试专用云厂商标识(避免与真实注册冲突)。
const testCloud domain.CloudProvider = "testcloud"

func mustRegister(t *testing.T, entries []logquery.LogEntry, err error) {
	t.Helper()
	logquery.RegisterProvider(testCloud, logquery.LogTypeCDN, func(*domain.CloudAccount) (logquery.LogProvider, error) {
		return &fakeProvider{cloud: testCloud, logType: logquery.LogTypeCDN, entries: entries, err: err}, nil
	})
}

func testAccount(id int64, cloud domain.CloudProvider) domain.CloudAccount {
	return domain.CloudAccount{ID: id, Name: "test-acc", Provider: cloud,
		Status: domain.CloudAccountStatusActive, TenantID: 3}
}

// ---------------------------------------------------------------------
// 用例
// ---------------------------------------------------------------------

// TestSearchHappyPath 两账号联邦:归并倒序 + per-source 状态。
func TestSearchHappyPath(t *testing.T) {
	now := time.Now().UnixMilli()
	mustRegister(t, []logquery.LogEntry{
		&testEntry{ts: now - 1000},
		&testEntry{ts: now - 3000},
	}, nil)
	// 同云两账号(同一 provider 构造两份)
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, testCloud), testAccount(2, testCloud),
	}}, nil)
	resp, err := svc.Search(context.Background(), 3, SearchRequest{
		LogType:   logquery.LogTypeCDN,
		StartTime: now - 3600_000, EndTime: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Entries) != 4 {
		t.Fatalf("entries = %d, want 4", len(resp.Entries))
	}
	// 时间倒序校验
	for i := 1; i < len(resp.Entries); i++ {
		if resp.Entries[i-1].GetTimestamp() < resp.Entries[i].GetTimestamp() {
			t.Fatalf("not desc: %d < %d", resp.Entries[i-1].GetTimestamp(), resp.Entries[i].GetTimestamp())
		}
	}
	if len(resp.Sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(resp.Sources))
	}
	if resp.Truncated {
		t.Error("should not truncate below limit")
	}
}

// TestSearchInvalidParams 参数校验。
func TestSearchInvalidParams(t *testing.T) {
	svc := NewFederationService(&fakeAccountSource{}, nil)
	now := time.Now().UnixMilli()
	if _, err := svc.Search(context.Background(), 3, SearchRequest{LogType: "bogus", StartTime: now, EndTime: now + 1}); err == nil {
		t.Error("invalid log type should fail")
	}
	if _, err := svc.Search(context.Background(), 3, SearchRequest{LogType: logquery.LogTypeCDN, StartTime: now, EndTime: now}); err == nil {
		t.Error("empty window should fail")
	}
}

// TestSearchUnregisteredCloud 云未注册该类型 provider:全源失败时显式报错,
// 不返回可被结果缓存回放的空 200(瞬时全失败曾以脏 0 回放)。
func TestSearchUnregisteredCloud(t *testing.T) {
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(9, "aliyun"),
	}}, nil)
	now := time.Now().UnixMilli()
	_, err := svc.Search(context.Background(), 3, SearchRequest{
		LogType: logquery.LogTypeWAF, StartTime: now - 1000, EndTime: now,
	})
	if err == nil || !strings.Contains(err.Error(), "所有源查询失败") {
		t.Fatalf("want all-sources-failed error, got %v", err)
	}
}

// TestSearchSourceFailureIsolated 单源失败:其他源结果正常返回,失败源记 error。
func TestSearchSourceFailureIsolated(t *testing.T) {
	now := time.Now().UnixMilli()
	// 显式注册 testCloud WAF 为健康 provider(注册表是全局的,防其他测试
	// 的注册泄漏到本用例;mustRegister 注册的是 CDN,与本用例无关)
	logquery.RegisterProvider(testCloud, logquery.LogTypeWAF, func(*domain.CloudAccount) (logquery.LogProvider, error) {
		return &fakeProvider{cloud: testCloud, logType: logquery.LogTypeWAF,
			entries: []logquery.LogEntry{&testEntry{ts: now - 1000}}}, nil
	})
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, testCloud), // 正常源:上面注册的数据可查
		testAccount(9, "aliyun"),  // 未注册 -> 单源失败隔离,仅注解
	}}, nil)
	resp, err := svc.Search(context.Background(), 3, SearchRequest{
		LogType: logquery.LogTypeWAF, StartTime: now - 1000, EndTime: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 单源失败不中断整体:健康源条目照常返回,失败源记 error(源顺序是
	// 并发完成序,不断言位置)
	if len(resp.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(resp.Entries))
	}
	failed := 0
	for _, oc := range resp.Sources {
		if oc.Error != "" {
			failed++
		}
	}
	if failed != 1 || len(resp.Sources) != 2 {
		t.Fatalf("want 1 failed + 1 ok source, got %+v", resp.Sources)
	}
}

// TestSearchTenantIsolation 租户过滤:他租账号不可见。
func TestSearchTenantIsolation(t *testing.T) {
	now := time.Now().UnixMilli()
	mustRegister(t, []logquery.LogEntry{&testEntry{ts: now - 1000}}, nil)
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		{ID: 1, Provider: testCloud, Status: domain.CloudAccountStatusActive, TenantID: 99, Name: "other"},
	}}, nil)
	resp, err := svc.Search(context.Background(), 3, SearchRequest{
		LogType: logquery.LogTypeCDN, StartTime: now - 1000, EndTime: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Sources) != 0 {
		t.Fatalf("other tenant accounts leaked: %+v", resp.Sources)
	}
}

// TestListSources 日志源清单聚合。
func TestListSources(t *testing.T) {
	logquery.RegisterProvider(testCloud, logquery.LogTypeCDN, func(*domain.CloudAccount) (logquery.LogProvider, error) {
		return &fakeProvider{
			cloud: testCloud, logType: logquery.LogTypeCDN,
			listed: []logquery.LogSource{{Cloud: testCloud, ResourceID: "api.example.com", Enabled: true}},
		}, nil
	})
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, testCloud),
	}}, nil)
	sources, err := svc.ListSources(context.Background(), 3, logquery.LogTypeCDN, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].ResourceID != "api.example.com" {
		t.Fatalf("sources = %+v", sources)
	}
	// 未注册类型:空清单不报错
	if _, err := svc.ListSources(context.Background(), 3, logquery.LogTypeSLB, nil, nil); err != nil {
		t.Fatal(err)
	}
}

// fakeAggregator 可编程聚合 provider:实现 Search(复用 fakeProvider 语义)+ Aggregate。
type fakeAggregator struct {
	fakeProvider
	result *logquery.AggregateResult
	err    error
	// seenBucketSec 记录服务层下发的分桶秒数(对齐校验)
	seenBucketSec int64
}

func (p *fakeAggregator) Aggregate(_ context.Context, _ *domain.CloudAccount, params logquery.AggregateParams) (*logquery.AggregateResult, error) {
	p.seenBucketSec = params.BucketSec
	return p.result, p.err
}

// TestAggregateHappyPath 两账号联邦聚合:分桶求和、TopN 归并、Total 精确总数。
func TestAggregateHappyPath(t *testing.T) {
	logquery.RegisterProvider(testCloud, logquery.LogTypeCDN, func(*domain.CloudAccount) (logquery.LogProvider, error) {
		return &fakeAggregator{
			fakeProvider: fakeProvider{cloud: testCloud, logType: logquery.LogTypeCDN},
			result: &logquery.AggregateResult{
				Total: 300,
				Buckets: []logquery.AggregateBucket{
					{Timestamp: 1000, Count: 100}, {Timestamp: 2000, Count: 200},
				},
				TopN: []logquery.TopNItem{{Name: "a.com", Count: 150}},
			},
		}, nil
	})
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, testCloud), testAccount(2, testCloud),
	}}, nil)
	start := time.Now().Add(-6 * time.Hour)
	resp, err := svc.Aggregate(context.Background(), 3, AggregateRequest{
		LogType: logquery.LogTypeCDN, StartTime: start.UnixMilli(), EndTime: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 600 {
		t.Errorf("total = %d, want 600(两源求和)", resp.Total)
	}
	if len(resp.Buckets) != 2 || resp.Buckets[0].Count != 200 || resp.Buckets[1].Count != 400 {
		t.Errorf("buckets not summed: %+v", resp.Buckets)
	}
	if len(resp.TopN) != 1 || resp.TopN[0].Count != 300 {
		t.Errorf("topn not merged: %+v", resp.TopN)
	}
	// 分桶秒数服务层统一计算并下发(6h 窗口 -> 300s)
	if resp.Sources[0].Error != "" {
		t.Fatalf("unexpected source error: %+v", resp.Sources)
	}
}

// TestAggregateUnsupportedIsolated 未实现 Aggregator 的源显式标注不支持,
// 其他源正常归并(不静默缺失)。
func TestAggregateUnsupportedIsolated(t *testing.T) {
	logquery.RegisterProvider(testCloud, logquery.LogTypeWAF, func(*domain.CloudAccount) (logquery.LogProvider, error) {
		return &fakeAggregator{
			fakeProvider: fakeProvider{cloud: testCloud, logType: logquery.LogTypeWAF},
			result: &logquery.AggregateResult{
				Total:   42,
				Buckets: []logquery.AggregateBucket{{Timestamp: 1000, Count: 42}},
			},
		}, nil
	})
	// aliyun provider 已注册(Search 用)但未实现 Aggregator? 实际上已实现——
	// 用一个只含 Search 的 provider 模拟:注册到独立测试云
	const cloud2 domain.CloudProvider = "testcloud2"
	logquery.RegisterProvider(cloud2, logquery.LogTypeWAF, func(*domain.CloudAccount) (logquery.LogProvider, error) {
		return &fakeProvider{cloud: cloud2, logType: logquery.LogTypeWAF}, nil
	})
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, testCloud), testAccount(2, cloud2),
	}}, nil)
	now := time.Now()
	resp, err := svc.Aggregate(context.Background(), 3, AggregateRequest{
		LogType: logquery.LogTypeWAF, StartTime: now.Add(-time.Hour).UnixMilli(), EndTime: now.UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 42 {
		t.Errorf("total = %d, want 42(仅支持源计入)", resp.Total)
	}
	var unsupported []AggregateSourceOutcome
	for _, s := range resp.Sources {
		if s.Error != "" {
			unsupported = append(unsupported, s)
		}
	}
	if len(unsupported) != 1 || unsupported[0].AccountID != "2" {
		t.Errorf("want account 2 marked unsupported, got %+v", resp.Sources)
	}
}

// TestAggregateBucketSecAligned 分桶秒数由服务层按窗口统一计算并下发(全源对齐)。
func TestAggregateBucketSecAligned(t *testing.T) {
	var mu sync.Mutex
	var seen []int64
	logquery.RegisterProvider(testCloud, logquery.LogTypeSLB, func(*domain.CloudAccount) (logquery.LogProvider, error) {
		return &fakeAggregator{
			fakeProvider: fakeProvider{cloud: testCloud, logType: logquery.LogTypeSLB},
			result:       &logquery.AggregateResult{},
		}, nil
	})
	// 捕获型:每次构造的 Aggregate 都会记下下发参数
	logquery.RegisterProvider(testCloud, logquery.LogTypeWAF, func(*domain.CloudAccount) (logquery.LogProvider, error) {
		return &capturingAggregator{onAggregate: func(sec int64) {
			mu.Lock()
			seen = append(seen, sec)
			mu.Unlock()
		}}, nil
	})
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, testCloud), testAccount(2, testCloud),
	}}, nil)
	now := time.Now()
	if _, err := svc.Aggregate(context.Background(), 3, AggregateRequest{
		LogType: logquery.LogTypeWAF, StartTime: now.Add(-2 * time.Hour).UnixMilli(), EndTime: now.UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("aggregate calls = %d, want 2", len(seen))
	}
	if seen[0] != seen[1] || seen[0] != 300 {
		t.Errorf("bucket sec not aligned to 300(2h/300s=24 桶 ≤100): %v", seen)
	}
}

// capturingAggregator 记录 AggregateParams.BucketSec(对齐校验用)。
type capturingAggregator struct {
	onAggregate func(sec int64)
}

func (p *capturingAggregator) Cloud() domain.CloudProvider { return testCloud }
func (p *capturingAggregator) LogType() logquery.LogType   { return logquery.LogTypeWAF }
func (p *capturingAggregator) ListLogSources(context.Context, *domain.CloudAccount) ([]logquery.LogSource, error) {
	return nil, nil
}
func (p *capturingAggregator) Search(context.Context, *domain.CloudAccount, logquery.SearchParams) ([]logquery.LogEntry, error) {
	return nil, nil
}
func (p *capturingAggregator) Aggregate(_ context.Context, _ *domain.CloudAccount, params logquery.AggregateParams) (*logquery.AggregateResult, error) {
	p.onAggregate(params.BucketSec)
	return &logquery.AggregateResult{}, nil
}

// TestSearchFederatedCap 联邦级 1000 硬顶截断(limit 为每日志源上限,
// provider 归并总量可超单源上限;仅当总量触达 1000 才标记截断)。
func TestSearchFederatedCap(t *testing.T) {
	now := time.Now().UnixMilli()
	var entries []logquery.LogEntry
	for i := range 3500 {
		entries = append(entries, &testEntry{ts: now - int64(i)})
	}
	mustRegister(t, entries[:600], nil)
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, testCloud),
	}}, nil)
	// 600 条 < 联邦硬顶:limit 600 被钳到单源硬顶 500,fake 源返回 600
	resp, err := svc.Search(context.Background(), 3, SearchRequest{
		LogType: logquery.LogTypeCDN, StartTime: now - 7200_000, EndTime: now,
		Limit: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Truncated {
		t.Error("600 entries below federated cap should not be truncated")
	}
	if resp.Total > 3000 {
		t.Errorf("total = %d exceeds federated cap", resp.Total)
	}
	// 3500 条 > 联邦硬顶 3000:ignoreLimit 桩模拟多源归并总量超限
	logquery.RegisterProvider(testCloud, logquery.LogTypeWAF, func(*domain.CloudAccount) (logquery.LogProvider, error) {
		return &fakeProvider{cloud: testCloud, logType: logquery.LogTypeWAF, entries: entries, ignoreLimit: true}, nil
	})
	resp2, err := svc.Search(context.Background(), 3, SearchRequest{
		LogType: logquery.LogTypeWAF, StartTime: now - 7200_000, EndTime: now,
		Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp2.Truncated {
		t.Error("3500 entries should be truncated at federated cap")
	}
	if resp2.Total != 3000 {
		t.Errorf("total = %d, want 3000", resp2.Total)
	}
}

// TestNormalizeWindowMs 毫秒/秒窗口自适配:秒纪元值([1e9,1e12))按秒×1000
// 纠正;毫秒纪元原样;更小值报错不静默(1970 空窗曾静默返回 0)。
func TestNormalizeWindowMs(t *testing.T) {
	now := time.Now().UnixMilli()
	s, e, err := normalizeWindowMs(now-60_000, now)
	if err != nil || s != now-60_000 || e != now {
		t.Fatalf("ms passthrough: %d,%d,%v", s, e, err)
	}
	nowSec := time.Now().Unix()
	s, e, err = normalizeWindowMs(nowSec-60, nowSec)
	if err != nil || s != (nowSec-60)*1000 || e != nowSec*1000 {
		t.Fatalf("sec auto-correct: %d,%d,%v", s, e, err)
	}
	if _, _, err := normalizeWindowMs(1000, 2000); err == nil {
		t.Fatal("1e3 级窗口应报错")
	}
}

// TestAllTransientFailed 能力不支持的确定性空不算瞬时失败。
func TestAllTransientFailed(t *testing.T) {
	if allTransientFailed(nil) {
		t.Fatal("空源不算全失败")
	}
	ok := []AggregateSourceOutcome{{Error: ""}}
	if allTransientFailed(ok) {
		t.Fatal("有成功源不算全失败")
	}
	transient := []AggregateSourceOutcome{{Error: "boom"}, {Error: "timeout"}}
	if !allTransientFailed(transient) {
		t.Fatal("全瞬时失败应 true")
	}
	mixed := []AggregateSourceOutcome{{Error: "boom"}, {Error: "aggregate not supported for this provider", NotSupported: true}}
	if allTransientFailed(mixed) {
		t.Fatal("含不支持源不算全失败")
	}
}

// TestWarmupOnce 预热遍历活跃账号 × 已注册类型调源清单;未注册组合跳过,
// 至少完成一个 provider 记成功。
func TestWarmupOnce(t *testing.T) {
	now := time.Now().UnixMilli()
	calls := 0
	logquery.RegisterProvider(testCloud, logquery.LogTypeCDN, func(*domain.CloudAccount) (logquery.LogProvider, error) {
		return &fakeProvider{cloud: testCloud, logType: logquery.LogTypeCDN,
			entries: []logquery.LogEntry{&testEntry{ts: now}}, onListSources: func() { calls++ }}, nil
	})
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, testCloud),
		testAccount(9, "aliyun"), // 未注册组合:静默跳过
	}}, nil)
	if !svc.warmupOnce(context.Background()) {
		t.Fatal("warmupOnce 应记成功")
	}
	if calls != 1 {
		t.Fatalf("ListLogSources 调用 = %d, want 1", calls)
	}
	// 无账号:不算成功(触发短重试)
	empty := NewFederationService(&fakeAccountSource{}, nil)
	if empty.warmupOnce(context.Background()) {
		t.Fatal("零 provider 预热不应记成功")
	}
}
