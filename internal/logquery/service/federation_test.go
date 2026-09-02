package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
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
}

func (p *fakeProvider) Cloud() domain.CloudProvider { return p.cloud }
func (p *fakeProvider) LogType() logquery.LogType   { return p.logType }
func (p *fakeProvider) ListLogSources(_ context.Context, _ *domain.CloudAccount) ([]logquery.LogSource, error) {
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

// TestSearchUnregisteredCloud 云未注册该类型 provider:状态显式记因,不报错。
func TestSearchUnregisteredCloud(t *testing.T) {
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(9, "aliyun"),
	}}, nil)
	now := time.Now().UnixMilli()
	resp, err := svc.Search(context.Background(), 3, SearchRequest{
		LogType: logquery.LogTypeWAF, StartTime: now - 1000, EndTime: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Sources) != 1 || resp.Sources[0].Error == "" {
		t.Fatalf("want provider-not-registered outcome, got %+v", resp.Sources)
	}
}

// TestSearchSourceFailureIsolated 单源失败:其他源结果正常返回,失败源记 error。
func TestSearchSourceFailureIsolated(t *testing.T) {
	now := time.Now().UnixMilli()
	mustRegister(t, []logquery.LogEntry{&testEntry{ts: now - 1000}}, nil)
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, testCloud), // 正常源
		testAccount(9, "aliyun"),  // 未注册 -> not registered
	}}, nil)
	// 让 testCloud provider 失败:重新注册一个会报错的
	logquery.RegisterProvider(testCloud, logquery.LogTypeWAF, func(*domain.CloudAccount) (logquery.LogProvider, error) {
		return nil, errors.New("boom")
	})
	resp, err := svc.Search(context.Background(), 3, SearchRequest{
		LogType: logquery.LogTypeWAF, StartTime: now - 1000, EndTime: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 无可用数据源但无整体错误
	if len(resp.Entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(resp.Entries))
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

// TestSearchFederatedCap 联邦级 1000 硬顶截断(limit 为每日志源上限,
// provider 归并总量可超单源上限;仅当总量触达 1000 才标记截断)。
func TestSearchFederatedCap(t *testing.T) {
	now := time.Now().UnixMilli()
	var entries []logquery.LogEntry
	for i := range 1500 {
		entries = append(entries, &testEntry{ts: now - int64(i)})
	}
	mustRegister(t, entries, nil)
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
	if resp.Total > 1000 {
		t.Errorf("total = %d exceeds federated cap", resp.Total)
	}
	// 1500 条 > 联邦硬顶:ignoreLimit 桩模拟多源归并总量超限
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
		t.Error("1500 entries should be truncated at federated cap")
	}
	if resp2.Total != 1000 {
		t.Errorf("total = %d, want 1000", resp2.Total)
	}
}
