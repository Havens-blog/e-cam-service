// Package analysis 成本分析引擎
package analysis

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/gotomicro/ego/core/elog"
	"github.com/redis/go-redis/v9"
)

const (
	// summaryCachePrefix 成本概览缓存 key 前缀
	summaryCachePrefix = "finops:cost:summary"
	// trendCachePrefix 趋势数据缓存 key 前缀
	trendCachePrefix = "finops:cost:trend"
	// summaryCacheTTL 概览缓存 TTL
	summaryCacheTTL = 5 * time.Minute
	// trendCacheTTL 趋势缓存 TTL
	trendCacheTTL = 30 * time.Minute
)

// CostFilter 成本查询筛选条件
type CostFilter struct {
	TenantID    string
	Provider    string
	AccountID   int64
	ServiceType string
	Region      string
	StartDate   string // YYYY-MM-DD
	EndDate     string // YYYY-MM-DD
}

// CostTrendFilter 成本趋势查询筛选条件
type CostTrendFilter struct {
	CostFilter
	Granularity string // "daily" | "weekly" | "monthly"
}

// CostSummary 成本概览
type CostSummary struct {
	CurrentMonthAmount float64 `json:"current_month_amount"`
	LastMonthAmount    float64 `json:"last_month_amount"`
	MoMChangePercent   float64 `json:"mom_change_percent"` // 环比变化百分比（与上月同期对比）
	ElapsedDays        int     `json:"elapsed_days"`       // 当月已过天数
}

// CostTrendPoint 成本趋势数据点
type CostTrendPoint struct {
	Date      string  `json:"date"`
	Amount    float64 `json:"amount"`
	AmountCNY float64 `json:"amount_cny"`
}

// CostDistItem 成本分布项
type CostDistItem struct {
	Key       string  `json:"key"`
	Amount    float64 `json:"amount"`
	AmountCNY float64 `json:"amount_cny"`
	Percent   float64 `json:"percent"` // 占比百分比
}

// ComparisonResult 同比/环比对比结果
type ComparisonResult struct {
	CurrentAmount  float64 `json:"current_amount"`
	PreviousAmount float64 `json:"previous_amount"`
	ChangePercent  float64 `json:"change_percent"`
	CurrentPeriod  string  `json:"current_period"`
	PreviousPeriod string  `json:"previous_period"`
}

// CostService 成本分析服务
type CostService struct {
	billDAO    repository.BillDAO
	redisCache redis.Cmdable
	logger     *elog.Component
}

// NewCostService 创建成本分析服务
func NewCostService(billDAO repository.BillDAO, redisClient redis.Cmdable, logger *elog.Component) *CostService {
	return &CostService{
		billDAO:    billDAO,
		redisCache: redisClient,
		logger:     logger,
	}
}

// toUnifiedBillFilter 将 CostFilter 转换为 UnifiedBillFilter
func toUnifiedBillFilter(f CostFilter) repository.UnifiedBillFilter {
	return repository.UnifiedBillFilter{
		TenantID:    f.TenantID,
		Provider:    f.Provider,
		AccountID:   f.AccountID,
		ServiceType: f.ServiceType,
		Region:      f.Region,
		StartDate:   f.StartDate,
		EndDate:     f.EndDate,
	}
}

// getCacheKey 生成缓存 key
func getCacheKey(prefix, tenantID string, filter interface{}) string {
	data, _ := json.Marshal(filter)
	hash := md5.Sum(data)
	return fmt.Sprintf("%s:%s:%x", prefix, tenantID, hash)
}

// GetCostSummary 获取成本概览（当月/上月/环比）
func (s *CostService) GetCostSummary(ctx context.Context, filter CostFilter) (*CostSummary, error) {
	// 尝试从缓存获取
	cacheKey := getCacheKey(summaryCachePrefix, filter.TenantID, filter)
	if cached, err := s.getFromCache(ctx, cacheKey); err == nil {
		var summary CostSummary
		if json.Unmarshal(cached, &summary) == nil {
			return &summary, nil
		}
	}

	now := time.Now()

	// 当月日期范围：当月1号 ~ 今天
	currentStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	currentEnd := now
	elapsedDays := now.Day()

	// 上月同期范围：上月1号 ~ 上月同一天（用于环比对比）
	lastMonthSameDay := currentStart.AddDate(0, -1, 0)
	lastMonthSameDayEnd := time.Date(lastMonthSameDay.Year(), lastMonthSameDay.Month(), elapsedDays, 0, 0, 0, 0, now.Location())
	// 防止上月天数不够（如3月31日对比2月只有28天）
	lastMonthLastDay := currentStart.AddDate(0, 0, -1)
	if lastMonthSameDayEnd.After(lastMonthLastDay) {
		lastMonthSameDayEnd = lastMonthLastDay
	}

	// 上月整月范围（用于"上月总成本"卡片）
	lastMonthStart := currentStart.AddDate(0, -1, 0)
	lastMonthEnd := currentStart.AddDate(0, 0, -1)

	// 查询当月总成本（截至今日）
	currentFilter := toUnifiedBillFilter(filter)
	currentFilter.StartDate = currentStart.Format("2006-01-02")
	currentFilter.EndDate = currentEnd.Format("2006-01-02")
	currentAmount, err := s.billDAO.SumAmount(ctx, currentFilter)
	if err != nil {
		s.logger.Error("failed to sum current month amount", elog.FieldErr(err))
		return nil, fmt.Errorf("sum current month amount: %w", err)
	}

	// 查询上月整月总成本
	lastFilter := toUnifiedBillFilter(filter)
	lastFilter.StartDate = lastMonthStart.Format("2006-01-02")
	lastFilter.EndDate = lastMonthEnd.Format("2006-01-02")
	lastAmount, err := s.billDAO.SumAmount(ctx, lastFilter)
	if err != nil {
		s.logger.Error("failed to sum last month amount", elog.FieldErr(err))
		return nil, fmt.Errorf("sum last month amount: %w", err)
	}

	// 查询上月同期成本（上月1号 ~ 上月同一天），用于环比
	lastSamePeriodFilter := toUnifiedBillFilter(filter)
	lastSamePeriodFilter.StartDate = lastMonthSameDay.Format("2006-01-02")
	lastSamePeriodFilter.EndDate = lastMonthSameDayEnd.Format("2006-01-02")
	lastSamePeriodAmount, err := s.billDAO.SumAmount(ctx, lastSamePeriodFilter)
	if err != nil {
		s.logger.Error("failed to sum last month same period amount", elog.FieldErr(err))
		// 降级：用上月整月对比
		lastSamePeriodAmount = lastAmount
	}

	// 环比变化：当月截至今日 vs 上月同期
	var momChange float64
	if lastSamePeriodAmount > 0 {
		momChange = (currentAmount - lastSamePeriodAmount) / lastSamePeriodAmount * 100
	}

	summary := &CostSummary{
		CurrentMonthAmount: currentAmount,
		LastMonthAmount:    lastAmount,
		MoMChangePercent:   momChange,
		ElapsedDays:        elapsedDays,
	}

	// 写入缓存（忽略错误）
	s.setCache(ctx, cacheKey, summary, summaryCacheTTL)

	return summary, nil
}

// GetCostTrend 获取成本趋势数据
func (s *CostService) GetCostTrend(ctx context.Context, filter CostTrendFilter) ([]CostTrendPoint, error) {
	// 如果没有指定日期范围，默认近3个月
	if filter.StartDate == "" || filter.EndDate == "" {
		now := time.Now()
		filter.EndDate = now.Format("2006-01-02")
		start := time.Date(now.Year(), now.Month()-2, 1, 0, 0, 0, 0, now.Location())
		filter.StartDate = start.Format("2006-01-02")
	}

	// 尝试从缓存获取
	cacheKey := getCacheKey(trendCachePrefix, filter.TenantID, filter)
	if cached, err := s.getFromCache(ctx, cacheKey); err == nil {
		var points []CostTrendPoint
		if json.Unmarshal(cached, &points) == nil {
			return points, nil
		}
	}

	// 查询每日金额
	ubf := toUnifiedBillFilter(filter.CostFilter)
	dailyAmounts, err := s.billDAO.AggregateDailyAmount(ctx, filter.TenantID, filter.StartDate, filter.EndDate, ubf)
	if err != nil {
		s.logger.Error("failed to aggregate daily amount", elog.FieldErr(err))
		return nil, fmt.Errorf("aggregate daily amount: %w", err)
	}

	// 转换为趋势数据点
	var points []CostTrendPoint
	switch filter.Granularity {
	case "weekly":
		points = aggregateWeekly(dailyAmounts)
	case "monthly":
		points = aggregateMonthly(dailyAmounts)
	default: // "daily"
		points = convertDailyToPoints(dailyAmounts)
	}

	// 写入缓存
	s.setCache(ctx, cacheKey, points, trendCacheTTL)

	return points, nil
}

// GetCostDistribution 获取成本分布（按云厂商/服务类型/地域）
func (s *CostService) GetCostDistribution(ctx context.Context, filter CostFilter, dimension string) ([]CostDistItem, error) {
	// 如果没有指定日期范围，默认近3个月
	if filter.StartDate == "" || filter.EndDate == "" {
		now := time.Now()
		filter.EndDate = now.Format("2006-01-02")
		start := time.Date(now.Year(), now.Month()-2, 1, 0, 0, 0, 0, now.Location())
		filter.StartDate = start.Format("2006-01-02")
	}

	results, err := s.billDAO.AggregateByField(ctx, filter.TenantID, dimension, filter.StartDate, filter.EndDate, repository.UnifiedBillFilter{
		Provider:    filter.Provider,
		AccountID:   filter.AccountID,
		ServiceType: filter.ServiceType,
		Region:      filter.Region,
	})
	if err != nil {
		s.logger.Error("failed to aggregate by field",
			elog.String("dimension", dimension),
			elog.FieldErr(err),
		)
		return nil, fmt.Errorf("aggregate by field %s: %w", dimension, err)
	}

	// 计算总金额
	var totalCNY float64
	for _, r := range results {
		totalCNY += r.AmountCNY
	}

	// 构建分布项并计算百分比
	items := make([]CostDistItem, 0, len(results))
	for _, r := range results {
		var pct float64
		if totalCNY > 0 {
			pct = r.AmountCNY / totalCNY * 100
		}
		items = append(items, CostDistItem{
			Key:       r.Key,
			Amount:    r.Amount,
			AmountCNY: r.AmountCNY,
			Percent:   pct,
		})
	}

	return items, nil
}

// GetYoYComparison 获取同比对比数据
func (s *CostService) GetYoYComparison(ctx context.Context, filter CostFilter) (*ComparisonResult, error) {
	now := time.Now()

	// 如果未指定日期范围，默认使用当月
	if filter.StartDate == "" {
		filter.StartDate = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	}
	if filter.EndDate == "" {
		filter.EndDate = now.Format("2006-01-02")
	}

	// 解析当前周期
	startDate, err := time.Parse("2006-01-02", filter.StartDate)
	if err != nil {
		return nil, fmt.Errorf("parse start date: %w", err)
	}
	endDate, err := time.Parse("2006-01-02", filter.EndDate)
	if err != nil {
		return nil, fmt.Errorf("parse end date: %w", err)
	}

	// 计算去年同期
	prevStart := startDate.AddDate(-1, 0, 0)
	prevEnd := endDate.AddDate(-1, 0, 0)

	// 查询当期金额
	currentFilter := toUnifiedBillFilter(filter)
	currentAmount, err := s.billDAO.SumAmount(ctx, currentFilter)
	if err != nil {
		return nil, fmt.Errorf("sum current period amount: %w", err)
	}

	// 查询去年同期金额
	prevFilter := toUnifiedBillFilter(filter)
	prevFilter.StartDate = prevStart.Format("2006-01-02")
	prevFilter.EndDate = prevEnd.Format("2006-01-02")
	prevAmount, err := s.billDAO.SumAmount(ctx, prevFilter)
	if err != nil {
		return nil, fmt.Errorf("sum previous period amount: %w", err)
	}

	// 计算同比变化百分比
	var changePct float64
	if prevAmount > 0 {
		changePct = (currentAmount - prevAmount) / prevAmount * 100
	}

	return &ComparisonResult{
		CurrentAmount:  currentAmount,
		PreviousAmount: prevAmount,
		ChangePercent:  changePct,
		CurrentPeriod:  fmt.Sprintf("%s ~ %s", filter.StartDate, filter.EndDate),
		PreviousPeriod: fmt.Sprintf("%s ~ %s", prevStart.Format("2006-01-02"), prevEnd.Format("2006-01-02")),
	}, nil
}

// --- Redis cache helpers ---

func (s *CostService) getFromCache(ctx context.Context, key string) ([]byte, error) {
	if s.redisCache == nil {
		return nil, fmt.Errorf("redis not configured")
	}
	val, err := s.redisCache.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}
	return val, nil
}

func (s *CostService) setCache(ctx context.Context, key string, value interface{}, ttl time.Duration) {
	if s.redisCache == nil {
		return
	}
	data, err := json.Marshal(value)
	if err != nil {
		s.logger.Warn("failed to marshal cache value", elog.String("key", key), elog.FieldErr(err))
		return
	}
	if err := s.redisCache.Set(ctx, key, data, ttl).Err(); err != nil {
		s.logger.Warn("failed to set cache", elog.String("key", key), elog.FieldErr(err))
	}
}

// --- Aggregation helpers ---

// convertDailyToPoints 将 DailyAmount 转换为 CostTrendPoint
func convertDailyToPoints(daily []repository.DailyAmount) []CostTrendPoint {
	points := make([]CostTrendPoint, 0, len(daily))
	for _, d := range daily {
		points = append(points, CostTrendPoint{
			Date:      d.Date,
			Amount:    d.Amount,
			AmountCNY: d.AmountCNY,
		})
	}
	sort.Slice(points, func(i, j int) bool {
		return points[i].Date < points[j].Date
	})
	return points
}

// aggregateWeekly 将每日数据聚合为每周数据
// 以每周一为周的起始日
func aggregateWeekly(daily []repository.DailyAmount) []CostTrendPoint {
	weekMap := make(map[string]*CostTrendPoint)
	var weekKeys []string

	for _, d := range daily {
		t, err := time.Parse("2006-01-02", d.Date)
		if err != nil {
			continue
		}
		// 计算该日期所在周的周一
		weekday := t.Weekday()
		offset := int(weekday - time.Monday)
		if offset < 0 {
			offset += 7
		}
		monday := t.AddDate(0, 0, -offset)
		weekKey := monday.Format("2006-01-02")

		if _, exists := weekMap[weekKey]; !exists {
			weekMap[weekKey] = &CostTrendPoint{Date: weekKey}
			weekKeys = append(weekKeys, weekKey)
		}
		weekMap[weekKey].Amount += d.Amount
		weekMap[weekKey].AmountCNY += d.AmountCNY
	}

	sort.Strings(weekKeys)
	points := make([]CostTrendPoint, 0, len(weekKeys))
	for _, k := range weekKeys {
		points = append(points, *weekMap[k])
	}
	return points
}

// aggregateMonthly 将每日数据聚合为每月数据
func aggregateMonthly(daily []repository.DailyAmount) []CostTrendPoint {
	monthMap := make(map[string]*CostTrendPoint)
	var monthKeys []string

	for _, d := range daily {
		t, err := time.Parse("2006-01-02", d.Date)
		if err != nil {
			continue
		}
		monthKey := t.Format("2006-01")

		if _, exists := monthMap[monthKey]; !exists {
			monthMap[monthKey] = &CostTrendPoint{Date: monthKey}
			monthKeys = append(monthKeys, monthKey)
		}
		monthMap[monthKey].Amount += d.Amount
		monthMap[monthKey].AmountCNY += d.AmountCNY
	}

	sort.Strings(monthKeys)
	points := make([]CostTrendPoint, 0, len(monthKeys))
	for _, k := range monthKeys {
		points = append(points, *monthMap[k])
	}
	return points
}
