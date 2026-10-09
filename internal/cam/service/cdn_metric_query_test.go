package service

import (
	"context"
	"errors"
	"testing"

	camrepository "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/gotomicro/ego/core/elog"
)

// ==================== 测试用 mock ====================

// metricQueryAccountRepo 账号仓储 mock(仅 List 生效,按租户过滤)
type metricQueryAccountRepo struct {
	camrepository.CloudAccountRepository
	accounts []domain.CloudAccount
	gotTid   int64
}

func (m *metricQueryAccountRepo) List(_ context.Context, filter domain.CloudAccountFilter) ([]domain.CloudAccount, int64, error) {
	m.gotTid = filter.TenantID
	return m.accounts, int64(len(m.accounts)), nil
}

// fakeCDNMetricReader 指标读取 mock(记录调用入参,按账号回发数据)
type fakeCDNMetricReader struct {
	byDomain  map[int64][]types.CDNMetric
	topByAcct map[int64][]types.CDNMetricTopRow

	gotDomainDomain  string
	gotDomainDays    int
	gotDomainAccount int64
	gotTopDays       int
	gotTopLimit      int
	gotTopAccounts   []int64
}

func (f *fakeCDNMetricReader) ListByDomain(_ context.Context, domainName string, days int, accountID int64) ([]types.CDNMetric, error) {
	f.gotDomainDomain = domainName
	f.gotDomainDays = days
	f.gotDomainAccount = accountID
	return f.byDomain[accountID], nil
}

func (f *fakeCDNMetricReader) TopByBytes(_ context.Context, days, limit int, accountID int64) ([]types.CDNMetricTopRow, error) {
	f.gotTopDays = days
	f.gotTopLimit = limit
	f.gotTopAccounts = append(f.gotTopAccounts, accountID)
	return f.topByAcct[accountID], nil
}

// errMetricReader 注入错误的 mock
type errMetricReader struct{}

func (errMetricReader) ListByDomain(context.Context, string, int, int64) ([]types.CDNMetric, error) {
	return nil, errors.New("boom")
}
func (errMetricReader) TopByBytes(context.Context, int, int, int64) ([]types.CDNMetricTopRow, error) {
	return nil, errors.New("boom")
}

// ==================== GetDomainMetrics ====================

// 租户无云账号 → 空数组且不查指标表
func TestGetDomainMetrics_TenantWithoutAccounts(t *testing.T) {
	repo := &metricQueryAccountRepo{}
	reader := &fakeCDNMetricReader{}
	svc := NewCDNQueryService(repo, nil, reader, elog.DefaultLogger)

	items, err := svc.GetDomainMetrics(context.Background(), 7, "a.example.com", 30)
	if err != nil {
		t.Fatalf("GetDomainMetrics: %v", err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("items = %#v, want empty non-nil slice", items)
	}
	if reader.gotDomainDomain != "" {
		t.Fatal("无账号时不应查询指标表")
	}
}

// 多账号合并 + 按 date 降序
func TestGetDomainMetrics_MergesAccountsSortedByDateDesc(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{
		{ID: 1}, {ID: 2},
	}}
	reader := &fakeCDNMetricReader{byDomain: map[int64][]types.CDNMetric{
		1: {
			{Domain: "a.example.com", Date: "2026-09-12", Bytes: 1},
			{Domain: "a.example.com", Date: "2026-09-14", Bytes: 3},
		},
		2: {
			{Domain: "a.example.com", Date: "2026-09-13", Bytes: 2},
		},
	}}
	svc := NewCDNQueryService(repo, nil, reader, elog.DefaultLogger)

	items, err := svc.GetDomainMetrics(context.Background(), 7, "a.example.com", 30)
	if err != nil {
		t.Fatalf("GetDomainMetrics: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("items len = %d, want 3", len(items))
	}
	for i, want := range []string{"2026-09-14", "2026-09-13", "2026-09-12"} {
		if items[i].Date != want {
			t.Fatalf("items[%d].Date = %s, want %s", i, items[i].Date, want)
		}
	}
	if repo.gotTid != 7 {
		t.Fatalf("account filter tenant = %d, want 7", repo.gotTid)
	}
	if reader.gotDomainAccount != 2 {
		t.Fatalf("last query account = %d, want 2 (逐账号隔离查询)", reader.gotDomainAccount)
	}
}

// DAO 报错向上透传
func TestGetDomainMetrics_PropagatesError(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	svc := NewCDNQueryService(repo, nil, errMetricReader{}, elog.DefaultLogger)

	if _, err := svc.GetDomainMetrics(context.Background(), 7, "a.example.com", 30); err == nil {
		t.Fatal("DAO 错误应向上透传")
	}
}

// ==================== GetTopDomains ====================

// 多账号聚合行合并后按字节降序,并截断到 limit
func TestGetTopDomains_MergesAndTruncates(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}, {ID: 2}}}
	reader := &fakeCDNMetricReader{topByAcct: map[int64][]types.CDNMetricTopRow{
		1: {
			{Domain: "big.example.com", Bytes: 900},
			{Domain: "small.example.com", Bytes: 100},
		},
		2: {
			{Domain: "mid.example.com", Bytes: 500},
		},
	}}
	svc := NewCDNQueryService(repo, nil, reader, elog.DefaultLogger)

	items, err := svc.GetTopDomains(context.Background(), 7, 7, 2)
	if err != nil {
		t.Fatalf("GetTopDomains: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2 (截断到 limit)", len(items))
	}
	if items[0].Domain != "big.example.com" || items[1].Domain != "mid.example.com" {
		t.Fatalf("order = [%s, %s], want [big, mid]", items[0].Domain, items[1].Domain)
	}
	if reader.gotTopDays != 7 || reader.gotTopLimit != 2 {
		t.Fatalf("top args days=%d limit=%d, want 7/2", reader.gotTopDays, reader.gotTopLimit)
	}
}

// 租户无账号 → 空数组
func TestGetTopDomains_TenantWithoutAccounts(t *testing.T) {
	repo := &metricQueryAccountRepo{}
	reader := &fakeCDNMetricReader{}
	svc := NewCDNQueryService(repo, nil, reader, elog.DefaultLogger)

	items, err := svc.GetTopDomains(context.Background(), 7, 7, 10)
	if err != nil {
		t.Fatalf("GetTopDomains: %v", err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("items = %#v, want empty non-nil slice", items)
	}
	if len(reader.gotTopAccounts) != 0 {
		t.Fatal("无账号时不应查询指标表")
	}
}
