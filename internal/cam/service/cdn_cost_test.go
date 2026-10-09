package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/gotomicro/ego/core/elog"
)

// acctRangeCall 记录一次范围聚合调用的入参
type acctRangeCall struct {
	tenantID int64
	start    string
	end      string
}

// fakeCDNBillDAO 内存版 CDN 账单聚合 DAO,断言 service 层逻辑(不连库)。
// AggregateByServiceTypeName 会被调用多次(by_account 全量 + 域名分摊的最新月),
// 用调用历史而非单值记录。
type fakeCDNBillDAO struct {
	monthly      []repository.CDNMonthlyRow
	byField      []repository.AggregateResult
	monthlyCalls []acctRangeCall
	acctCalls    []acctRangeCall
}

func (f *fakeCDNBillDAO) AggregateByServiceTypeName(ctx context.Context, tenantID int64, field, startDate, endDate string) ([]repository.AggregateResult, error) {
	f.acctCalls = append(f.acctCalls, acctRangeCall{tenantID, startDate, endDate})
	return f.byField, nil
}

func (f *fakeCDNBillDAO) AggregateCDNMonthly(ctx context.Context, tenantID int64, startDate, endDate string) ([]repository.CDNMonthlyRow, error) {
	f.monthlyCalls = append(f.monthlyCalls, acctRangeCall{tenantID, startDate, endDate})
	return f.monthly, nil
}

func TestCDNCostService_GetCDNCost(t *testing.T) {
	// Arrange: 2026-08/2026-09 两个月,months=2 → 范围 [2026-08, 2026-09]
	fake := &fakeCDNBillDAO{
		monthly: []repository.CDNMonthlyRow{
			{Month: "2026-08", ServiceTypeName: "cdn", AmountCNY: 100},
			{Month: "2026-08", ServiceTypeName: "p_cdn", AmountCNY: 50},
			{Month: "2026-08", ServiceTypeName: "dcdn", AmountCNY: 30},
			{Month: "2026-09", ServiceTypeName: "dcdn", AmountCNY: 70},
			{Month: "2026-07", ServiceTypeName: "cdn", AmountCNY: 999}, // 范围外应被丢弃
		},
		byField: []repository.AggregateResult{
			{Key: "1", AmountCNY: 150},
			{Key: "2", AmountCNY: 100},
		},
	}
	svc := NewCDNCostService(fake, &fakeCDNMetricReader{}, elog.DefaultLogger)

	// Act
	view, err := svc.GetCDNCost(context.Background(), 7, "2026-09", 2, 0)
	if err != nil {
		t.Fatalf("GetCDNCost: %v", err)
	}

	// Assert: 日期范围 [2026-08-01, 2026-09-31](monthly 与首次 by_account 一致)
	if len(fake.monthlyCalls) != 1 {
		t.Fatalf("monthly calls = %d, want 1", len(fake.monthlyCalls))
	}
	if c := fake.monthlyCalls[0]; c.tenantID != 7 || c.start != "2026-08-01" || c.end != "2026-09-31" {
		t.Fatalf("monthly range = %+v, want tenant 7 [2026-08-01, 2026-09-31]", c)
	}
	if len(fake.acctCalls) < 1 || fake.acctCalls[0].start != "2026-08-01" || fake.acctCalls[0].end != "2026-09-31" {
		t.Fatalf("acct calls = %+v, want first [2026-08-01, 2026-09-31]", fake.acctCalls)
	}

	// monthly: 范围内每月都有行(无数据补零),p_cdn 计入 cdn_amount
	if len(view.Monthly) != 2 {
		t.Fatalf("monthly len = %d, want 2", len(view.Monthly))
	}
	if m := view.Monthly[0]; m.Month != "2026-08" || m.CDNAmount != 150 || m.DCDNAmount != 30 {
		t.Errorf("monthly[0] = %+v, want {2026-08, 150, 30}", m)
	}
	if m := view.Monthly[1]; m.Month != "2026-09" || m.CDNAmount != 0 || m.DCDNAmount != 70 {
		t.Errorf("monthly[1] = %+v, want {2026-09, 0, 70}", m)
	}

	// by_account: share = 账号金额 / 总金额(250)
	if len(view.ByAccount) != 2 {
		t.Fatalf("by_account len = %d, want 2", len(view.ByAccount))
	}
	if a := view.ByAccount[0]; a.AccountID != 1 || a.Amount != 150 || a.Share != 0.6 {
		t.Errorf("by_account[0] = %+v, want {1, 150, 0.6}", a)
	}
	if a := view.ByAccount[1]; a.AccountID != 2 || a.Amount != 100 || a.Share != 0.4 {
		t.Errorf("by_account[1] = %+v, want {2, 100, 0.4}", a)
	}

	// domain_cost: 指标表无数据时为空数组(非 null)
	if view.DomainCost == nil || len(view.DomainCost) != 0 {
		t.Errorf("domain_cost = %#v, want empty non-nil slice", view.DomainCost)
	}
}

// 域名成本分摊:近 30 天字节占比 × 账号最新有账单月份成本;无指标账号跳过
func TestCDNCostService_GetCDNCost_DomainCostAllocation(t *testing.T) {
	// Arrange: 最新有账单月份为 2026-09;账号 1 成本 150,账号 2 成本 100
	fake := &fakeCDNBillDAO{
		monthly: []repository.CDNMonthlyRow{
			{Month: "2026-09", ServiceTypeName: "cdn", AmountCNY: 250},
		},
		byField: []repository.AggregateResult{
			{Key: "1", AmountCNY: 150},
			{Key: "2", AmountCNY: 100},
		},
	}
	reader := &fakeCDNMetricReader{topByAcct: map[int64][]types.CDNMetricTopRow{
		1: {
			{Domain: "a.example.com", Bytes: 700, Count: 30},
			{Domain: "b.example.com", Bytes: 300, Count: 30},
		},
		// 账号 2 无指标数据 → 跳过
	}}
	svc := NewCDNCostService(fake, reader, elog.DefaultLogger)

	// Act
	view, err := svc.GetCDNCost(context.Background(), 7, "2026-09", 1, 0)
	if err != nil {
		t.Fatalf("GetCDNCost: %v", err)
	}

	// Assert: a 150*0.7=105,b 150*0.3=45;按金额降序;is_estimate=true
	if len(view.DomainCost) != 2 {
		t.Fatalf("domain_cost len = %d, want 2: %#v", len(view.DomainCost), view.DomainCost)
	}
	first := view.DomainCost[0]
	if first.Domain != "a.example.com" || first.AmountEst != 105 || first.Bytes != 700 || !first.IsEstimate {
		t.Errorf("domain_cost[0] = %+v, want {a.example.com, 105, 700, true}", first)
	}
	second := view.DomainCost[1]
	if second.Domain != "b.example.com" || second.AmountEst != 45 || second.Bytes != 300 || !second.IsEstimate {
		t.Errorf("domain_cost[1] = %+v, want {b.example.com, 45, 300, true}", second)
	}
	// 分摊查询按最新有账单月份拉账号成本
	if c := fake.acctCalls[len(fake.acctCalls)-1]; c.start != "2026-09-01" || c.end != "2026-09-31" {
		t.Errorf("domain cost acct range = %+v, want [2026-09-01, 2026-09-31]", c)
	}
	if reader.gotTopDays != 30 {
		t.Errorf("TopByBytes days = %d, want 30", reader.gotTopDays)
	}
}

// account_id 过滤时域名分摊只保留该账号
func TestCDNCostService_GetCDNCost_DomainCostAccountFilter(t *testing.T) {
	fake := &fakeCDNBillDAO{
		monthly: []repository.CDNMonthlyRow{
			{Month: "2026-09", ServiceTypeName: "cdn", AmountCNY: 250},
		},
		byField: []repository.AggregateResult{
			{Key: "1", AmountCNY: 150},
			{Key: "2", AmountCNY: 100},
		},
	}
	reader := &fakeCDNMetricReader{topByAcct: map[int64][]types.CDNMetricTopRow{
		1: {{Domain: "a.example.com", Bytes: 1000, Count: 30}},
		2: {{Domain: "b.example.com", Bytes: 1000, Count: 30}},
	}}
	svc := NewCDNCostService(fake, reader, elog.DefaultLogger)

	view, err := svc.GetCDNCost(context.Background(), 7, "2026-09", 1, 2)
	if err != nil {
		t.Fatalf("GetCDNCost: %v", err)
	}
	if len(view.DomainCost) != 1 || view.DomainCost[0].Domain != "b.example.com" || view.DomainCost[0].AmountEst != 100 {
		t.Fatalf("domain_cost = %#v, want only b.example.com/100", view.DomainCost)
	}
}

// 无任何账单(latestMonth 为空)→ 域名分摊为空数组且不查指标表
func TestCDNCostService_GetCDNCost_DomainCostNoBills(t *testing.T) {
	fake := &fakeCDNBillDAO{}
	reader := &fakeCDNMetricReader{}
	svc := NewCDNCostService(fake, reader, elog.DefaultLogger)

	view, err := svc.GetCDNCost(context.Background(), 7, "", 0, 0)
	if err != nil {
		t.Fatalf("GetCDNCost: %v", err)
	}
	if view.DomainCost == nil || len(view.DomainCost) != 0 {
		t.Fatalf("domain_cost = %#v, want empty non-nil slice", view.DomainCost)
	}
	if len(reader.gotTopAccounts) != 0 {
		t.Fatal("无账单时不应查询指标表")
	}
}

func TestCDNCostService_GetCDNCost_Defaults(t *testing.T) {
	// Arrange
	fake := &fakeCDNBillDAO{}
	svc := NewCDNCostService(fake, &fakeCDNMetricReader{}, elog.DefaultLogger)

	// Act: 不传 start_month、months=0 → 默认 6 个月,止于当前月
	view, err := svc.GetCDNCost(context.Background(), 7, "", 0, 0)
	if err != nil {
		t.Fatalf("GetCDNCost: %v", err)
	}

	// Assert: monthly 有 6 个连续月份,最后一个月是当前月
	if len(view.Monthly) != 6 {
		t.Fatalf("monthly len = %d, want 6 (默认 months)", len(view.Monthly))
	}
	last := view.Monthly[len(view.Monthly)-1].Month
	if last != fake.monthlyCalls[0].end[:7] {
		t.Errorf("last month %s 与 endDate %s 不一致", last, fake.monthlyCalls[0].end)
	}
}

func TestCDNCostService_GetCDNCost_InvalidMonth(t *testing.T) {
	// Arrange
	svc := NewCDNCostService(&fakeCDNBillDAO{}, &fakeCDNMetricReader{}, elog.DefaultLogger)

	// Act
	_, err := svc.GetCDNCost(context.Background(), 7, "2026/09", 6, 0)

	// Assert: 必须是可判别的参数错误(handler 据此返回 400)
	if err == nil {
		t.Fatal("非法月份格式应返回错误")
	}
	if !errors.Is(err, ErrInvalidStartMonth) {
		t.Fatalf("err = %v, want errors.Is(err, ErrInvalidStartMonth)", err)
	}
}

func TestCDNCostService_GetCDNCost_AccountFilter(t *testing.T) {
	// Arrange: account_id=1 过滤后只保留该账号,但 share 仍按全量总额计算
	fake := &fakeCDNBillDAO{
		byField: []repository.AggregateResult{
			{Key: "1", AmountCNY: 150},
			{Key: "2", AmountCNY: 100},
		},
	}
	svc := NewCDNCostService(fake, &fakeCDNMetricReader{}, elog.DefaultLogger)

	// Act
	view, err := svc.GetCDNCost(context.Background(), 7, "2026-09", 1, 1)
	if err != nil {
		t.Fatalf("GetCDNCost: %v", err)
	}

	// Assert
	if len(view.ByAccount) != 1 {
		t.Fatalf("by_account len = %d, want 1", len(view.ByAccount))
	}
	a := view.ByAccount[0]
	if a.AccountID != 1 || a.Share != 0.6 {
		t.Errorf("by_account[0] = %+v, want {1, share 0.6 (按全量总额 250)}", a)
	}
}
