package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/gotomicro/ego/core/elog"
)

// fakeCDNBillDAO 内存版 CDN 账单聚合 DAO,断言 service 层逻辑(不连库)
type fakeCDNBillDAO struct {
	monthly   []repository.CDNMonthlyRow
	byField   []repository.AggregateResult
	gotTenant int64
	gotStart  string
	gotEnd    string
}

func (f *fakeCDNBillDAO) AggregateByServiceTypeName(ctx context.Context, tenantID int64, field, startDate, endDate string) ([]repository.AggregateResult, error) {
	f.gotTenant = tenantID
	f.gotStart = startDate
	f.gotEnd = endDate
	return f.byField, nil
}

func (f *fakeCDNBillDAO) AggregateCDNMonthly(ctx context.Context, tenantID int64, startDate, endDate string) ([]repository.CDNMonthlyRow, error) {
	f.gotTenant = tenantID
	f.gotStart = startDate
	f.gotEnd = endDate
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
	svc := NewCDNCostService(fake, elog.DefaultLogger)

	// Act
	view, err := svc.GetCDNCost(context.Background(), 7, "2026-09", 2, 0)
	if err != nil {
		t.Fatalf("GetCDNCost: %v", err)
	}

	// Assert: 日期范围 [2026-08-01, 2026-09-31]
	if fake.gotTenant != 7 || fake.gotStart != "2026-08-01" || fake.gotEnd != "2026-09-31" {
		t.Fatalf("date range = [%s, %s], tenant=%d, want [2026-08-01, 2026-09-31], 7",
			fake.gotStart, fake.gotEnd, fake.gotTenant)
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

	// domain_cost: 一期恒为空数组(非 null)
	if view.DomainCost == nil || len(view.DomainCost) != 0 {
		t.Errorf("domain_cost = %#v, want empty non-nil slice", view.DomainCost)
	}
}

func TestCDNCostService_GetCDNCost_Defaults(t *testing.T) {
	// Arrange
	fake := &fakeCDNBillDAO{}
	svc := NewCDNCostService(fake, elog.DefaultLogger)

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
	if last != fake.gotEnd[:7] {
		t.Errorf("last month %s 与 endDate %s 不一致", last, fake.gotEnd)
	}
}

func TestCDNCostService_GetCDNCost_InvalidMonth(t *testing.T) {
	// Arrange
	svc := NewCDNCostService(&fakeCDNBillDAO{}, elog.DefaultLogger)

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
	svc := NewCDNCostService(fake, elog.DefaultLogger)

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
