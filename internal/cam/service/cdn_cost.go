package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/gotomicro/ego/core/elog"
)

// ErrInvalidStartMonth start_month 不是合法的 YYYY-MM 格式(客户端参数错误,
// handler 据此返回 400)
var ErrInvalidStartMonth = errors.New("invalid start_month")

const (
	// defaultCDNMonths start_month/months 缺省时的回看月数
	defaultCDNMonths = 6
	// maxCDNMonths months 上限,防止恶意大范围聚合
	maxCDNMonths = 24
)

// CDNMonthCost CDN 月度成本（cdn/p_cdn 与 dcdn 分列）
type CDNMonthCost struct {
	Month      string  `json:"month"`
	CDNAmount  float64 `json:"cdn_amount"`
	DCDNAmount float64 `json:"dcdn_amount"`
}

// CDNAccountCost 按账号维度的 CDN 成本
type CDNAccountCost struct {
	AccountID   int64   `json:"account_id"`
	AccountName string  `json:"account_name"` // 一期置空,展示层补充
	Amount      float64 `json:"amount"`
	Share       float64 `json:"share"` // 该账号金额 / CDN 总金额
}

// CDNDomainCost 域名成本分摊项（二期接入 ecam_cdn_metric 流量指标后生效）
type CDNDomainCost struct {
	Domain     string  `json:"domain"`
	AmountEst  float64 `json:"amount_est"`
	Bytes      uint64  `json:"bytes"` // 二期接指标表,一期恒为 0
	IsEstimate bool    `json:"is_estimate"`
}

// CDNCostView GET /api/v1/cam/cost/cdn 的响应体
type CDNCostView struct {
	Monthly    []CDNMonthCost   `json:"monthly"`
	ByAccount  []CDNAccountCost `json:"by_account"`
	DomainCost []CDNDomainCost  `json:"domain_cost"`
}

// CDNCostService 多云 CDN 经营成本服务。
// 一期: monthly / by_account 为真实聚合;domain_cost 为空数组（is_estimate:false）。
type CDNCostService struct {
	bills  repository.CDNBillQuerier
	logger *elog.Component
}

// NewCDNCostService 创建 CDN 经营成本服务
func NewCDNCostService(bills repository.CDNBillQuerier, logger *elog.Component) *CDNCostService {
	return &CDNCostService{bills: bills, logger: logger}
}

// GetCDNCost 查询 CDN 经营成本。
// startMonth 为空取当前月;范围 = [startMonth-(months-1), startMonth]。
// accountID > 0 时 by_account 仅返回该账号（share 仍按全量总额计算）。
func (s *CDNCostService) GetCDNCost(ctx context.Context, tenantID int64, startMonth string, months int, accountID int64) (*CDNCostView, error) {
	if months <= 0 {
		months = defaultCDNMonths
	}
	if months > maxCDNMonths {
		months = maxCDNMonths
	}
	endMonth := startMonth
	if endMonth == "" {
		endMonth = time.Now().Format("2006-01")
	}
	firstMonth, err := shiftMonth(endMonth, -(months - 1))
	if err != nil {
		return nil, err
	}
	// billing_date 为 YYYY-MM-DD,字符串比较;end 取 "-31" 可覆盖任意月份的全部日期
	startDate := firstMonth + "-01"
	endDate := endMonth + "-31"

	monthly, err := s.buildMonthly(ctx, tenantID, startDate, endDate, firstMonth, endMonth)
	if err != nil {
		return nil, err
	}
	byAccount, err := s.buildByAccount(ctx, tenantID, startDate, endDate, accountID)
	if err != nil {
		return nil, err
	}

	return &CDNCostView{
		Monthly:    monthly,
		ByAccount:  byAccount,
		DomainCost: []CDNDomainCost{},
	}, nil
}

// buildMonthly 月度序列补零后按 service_type_name 分类填充
func (s *CDNCostService) buildMonthly(ctx context.Context, tenantID int64, startDate, endDate, firstMonth, endMonth string) ([]CDNMonthCost, error) {
	months := monthRange(firstMonth, endMonth)
	monthly := make([]CDNMonthCost, len(months))
	index := make(map[string]int, len(months))
	for i, m := range months {
		monthly[i] = CDNMonthCost{Month: m}
		index[m] = i
	}

	rows, err := s.bills.AggregateCDNMonthly(ctx, tenantID, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("aggregate cdn monthly: %w", err)
	}
	for _, r := range rows {
		i, ok := index[r.Month]
		if !ok {
			continue
		}
		// 精确 'dcdn' 计 dcdn_amount;其余被正则圈定的值（cdn/p_cdn 等）计 cdn_amount
		if r.ServiceTypeName == "dcdn" {
			monthly[i].DCDNAmount += r.AmountCNY
		} else {
			monthly[i].CDNAmount += r.AmountCNY
		}
	}
	return monthly, nil
}

// buildByAccount 按账号聚合;accountID>0 时仅保留该账号,share 按全量总额
func (s *CDNCostService) buildByAccount(ctx context.Context, tenantID int64, startDate, endDate string, accountID int64) ([]CDNAccountCost, error) {
	rows, err := s.bills.AggregateByServiceTypeName(ctx, tenantID, "account_id", startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("aggregate cdn by account: %w", err)
	}

	items := make([]CDNAccountCost, 0, len(rows))
	var total float64
	for _, r := range rows {
		aid, convErr := strconv.ParseInt(r.Key, 10, 64)
		if convErr != nil {
			continue
		}
		items = append(items, CDNAccountCost{AccountID: aid, Amount: r.AmountCNY})
		total += r.AmountCNY
	}
	if total > 0 {
		for i := range items {
			items[i].Share = items[i].Amount / total
		}
	}
	if accountID <= 0 {
		return items, nil
	}

	filtered := make([]CDNAccountCost, 0, 1)
	for _, it := range items {
		if it.AccountID == accountID {
			filtered = append(filtered, it)
		}
	}
	return filtered, nil
}

// shiftMonth 月份平移: "2026-09" + (-3) → "2026-06"
func shiftMonth(month string, offset int) (string, error) {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return "", fmt.Errorf("%w: %q", ErrInvalidStartMonth, month)
	}
	return t.AddDate(0, offset, 0).Format("2006-01"), nil
}

// monthRange 闭区间 [start, end] 的月份序列（升序）。
// 防御性上限 240 个月,避免异常输入导致死循环。
func monthRange(start, end string) []string {
	months := []string{start}
	for cur := start; cur != end && len(months) < 240; {
		next, err := shiftMonth(cur, 1)
		if err != nil {
			return months
		}
		months = append(months, next)
		cur = next
	}
	return months
}
