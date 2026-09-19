package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// NAS 指标读取参数边界(规格:days 限 1~90;top 默认 10 最大 50;分页默认 10 最大 50)
const (
	nasDefaultDays  = 30
	nasMaxDays      = 90
	nasDefaultTop   = 10
	nasMaxTop       = 50
	nasDefaultPage  = 1
	nasDefaultPSize = 10
	nasMaxPageSize  = 50
)

// data_status 读取侧取值(qc_status 闭环 + 缺失日标注,规格「Proposed Solution」第 5 点)
const (
	// NASDataStatusOK 行存在且数据正常
	NASDataStatusOK = "ok"
	// NASDataStatusZeroException qc_status=zero_exception 映射:capacity=0 异常行
	// 原样暴露并映射进 data_status,前端据此渲染警示而非当正常零容量
	NASDataStatusZeroException = "zero_exception"
	// NASDataStatusMissing 窗口内缺失日:不填充假值,capacity/used/utilization 为 null
	NASDataStatusMissing = "missing"
)

// Top 排序键取值(sort 参数)
const (
	NASSortCapacity    = "capacity"
	NASSortUtilization = "utilization"
)

// ErrNASAccountNotInTenant 客户端传入的 account_id 不属于当前租户(越权)。
// handler 须映射 404 而非 403——不泄露账号存在性(规格 Non-Functional Requirements)。
var ErrNASAccountNotInTenant = errors.New("cloud account not found")

// NASMetricReader NAS 指标只读接口(service 层消费;dao.NASMetricDAO 天然满足)。
// 二期指标读取只查本地指标表,不走厂商 API。
type NASMetricReader interface {
	// ListByFs 取指定账号+fs_id 近 N 天单日指标,按 date 升序(趋势接口)
	ListByFs(ctx context.Context, accountID int64, fsID string, days int) ([]types.NASMetric, error)
	// ListByAccounts 取一组账号近 N 天全部指标行(Top 聚合,服务层按 fs_id 去重)
	ListByAccounts(ctx context.Context, accountIDs []int64, days int) ([]types.NASMetric, error)
}

// NASMetricPoint 趋势单日数据点。缺失日 capacity/used/utilization 为 null
// (不填充假值,以 data_status=missing 标注)。
type NASMetricPoint struct {
	Date        string   `json:"date"`        // YYYY-MM-DD(Asia/Shanghai)
	Capacity    *float64 `json:"capacity"`    // 总容量(GB);缺失日 null
	Used        *float64 `json:"used"`        // 已用容量(GB);缺失日 null
	Utilization *float64 `json:"utilization"` // used/capacity(0-1);capacity=0 或缺失日 null
	DataStatus  string   `json:"data_status"` // ok | zero_exception | missing
	QcStatus    string   `json:"qc_status"`   // 原样透出落库 qc_status(空=正常)
}

// NASMetricSummary 最新一天 / 近 N 天均值两类值。
// 无可用行(窗口内全缺失或全为 capacity=0 异常行)时字段为 null。
type NASMetricSummary struct {
	Date        string   `json:"date,omitempty"` // 仅「最新一天」携带
	Capacity    *float64 `json:"capacity"`
	Used        *float64 `json:"used"`
	Utilization *float64 `json:"utilization"`
}

// NASFsMetricsResp GET /assets/nas/metrics 响应
type NASFsMetricsResp struct {
	FsID    string            `json:"fs_id"`
	Days    []NASMetricPoint  `json:"days"` // 按日期升序
	Latest  *NASMetricSummary `json:"latest"`
	Average *NASMetricSummary `json:"average"`
}

// NASTopItem Top 单项:按 fs_id 去重后的代表行(最新一天)+ 近 N 天均值。
// 跨账号同 fs 并存时 AccountIDs 为去重后的账号列表(口径与 T11 前端对齐项,
// 见任务 Implementation Notes)。
type NASTopItem struct {
	FsID       string           `json:"fs_id"`
	FsName     string           `json:"fs_name"`
	Provider   string           `json:"provider"`
	AccountIDs []int64          `json:"account_id"`
	DataStatus string           `json:"data_status"`
	QcStatus   string           `json:"qc_status"`
	Latest     NASMetricSummary `json:"latest"`
	Average    NASMetricSummary `json:"average"`
}

// NASTopResp GET /assets/nas/top 响应
type NASTopResp struct {
	Total    int          `json:"total"` // 去重后的 fs 总数(分页分母)
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
	Items    []NASTopItem `json:"items"`
}

// NASQueryService NAS 指标读取服务(纯读指标表,租户校验平移 CDN 模式)。
type NASQueryService struct {
	accountRepo repository.CloudAccountRepository
	metrics     NASMetricReader
	logger      *elog.Component
}

// NewNASQueryService 创建 NAS 指标读取服务
func NewNASQueryService(
	accountRepo repository.CloudAccountRepository,
	metrics NASMetricReader,
	logger *elog.Component,
) *NASQueryService {
	return &NASQueryService{accountRepo: accountRepo, metrics: metrics, logger: logger}
}

// nasCSTZone 运营时区(与采集/DAO 侧一致,按 Asia/Shanghai 自然日切分)
var nasCSTZone = time.FixedZone("CST", 8*60*60)

func nasToday() time.Time {
	return time.Now().In(nasCSTZone)
}

// normalizeNASDays 缺省 30 天,上限收敛 90(规格 days 限 1~90)
func normalizeNASDays(days int) int {
	if days <= 0 {
		return nasDefaultDays
	}
	if days > nasMaxDays {
		return nasMaxDays
	}
	return days
}

// nasUtilization 读取时由 capacity/used 派生(不读落库字段,Hard Rule):
//   - capacity=0 → null(不 panic、不写 NaN,异常行经 qc_status 可分辨);
//   - used>capacity → 按 min(used, capacity) 收敛(厂商刷新时点不一致),
//     由调用方打 warn 日志;
//   - used=0, capacity>0 → 0。
func nasUtilization(capacity, used float64) *float64 {
	if capacity == 0 {
		return nil
	}
	eff := used
	if eff > capacity {
		eff = capacity
	}
	v := eff / capacity
	return &v
}

func floatPtr(v float64) *float64 { return &v }

// nasDataStatus qc_status → data_status 映射(闭环:原样暴露 + 映射)
func nasDataStatus(qcStatus string) string {
	if qcStatus == types.NASMetricQcZeroException {
		return NASDataStatusZeroException
	}
	return NASDataStatusOK
}

// GetFsMetrics 查询租户内指定账号下某 NAS 文件系统近 N 天单日指标趋势。
// days[] 按日期升序,缺失日以 data_status=missing 标注不填充假值;
// 同时返回「最新一天」与「近 N 天均值」两类值(utilization 均为读取时派生)。
func (s *NASQueryService) GetFsMetrics(
	ctx context.Context,
	tenantID, accountID int64,
	fsID string,
	days int,
) (*NASFsMetricsResp, error) {
	if fsID == "" {
		return nil, fmt.Errorf("缺少 fs_id 参数")
	}
	if accountID <= 0 {
		return nil, fmt.Errorf("缺少 account_id 参数")
	}
	days = normalizeNASDays(days)

	// 租户校验:account_id ∉ 租户账号集合 → 越权(handler 映射 404)
	if _, err := s.resolveAccountScope(ctx, tenantID, accountID); err != nil {
		return nil, err
	}

	rows, err := s.metrics.ListByFs(ctx, accountID, fsID, days)
	if err != nil {
		return nil, fmt.Errorf("查询 NAS 指标失败: %w", err)
	}

	resp := &NASFsMetricsResp{FsID: fsID, Days: []NASMetricPoint{}}
	rowsByDate := make(map[string]types.NASMetric, len(rows))
	for _, m := range rows {
		rowsByDate[m.Date] = m
	}

	// 窗口逐日展开(升序),缺失日不填充假值
	today := nasToday()
	for i := days - 1; i >= 0; i-- {
		date := today.AddDate(0, 0, -i).Format("2006-01-02")
		m, ok := rowsByDate[date]
		if !ok {
			resp.Days = append(resp.Days, NASMetricPoint{Date: date, DataStatus: NASDataStatusMissing})
			continue
		}
		s.warnIfUsedExceedsCapacity(m)
		resp.Days = append(resp.Days, NASMetricPoint{
			Date:        date,
			Capacity:    floatPtr(m.Capacity),
			Used:        floatPtr(m.UsedCapacity),
			Utilization: nasUtilization(m.Capacity, m.UsedCapacity),
			DataStatus:  nasDataStatus(m.QcStatus),
			QcStatus:    m.QcStatus,
		})
	}

	resp.Latest = nasLatestSummary(rows)
	resp.Average = nasAverageSummary(rows)
	return resp, nil
}

// GetTop 查询租户内近 N 天 NAS 容量/使用率 Top(账号视角)。
// Top 按 fs_id 去重:多账号并存按「日期 desc,再容量 desc」取第一行(代表行,
// 不跨账号求和/平均,避免共享容量双计);utilization 排序用近 N 天均值口径;
// 均值对 capacity=0 行/无数据日跳过不参与。
func (s *NASQueryService) GetTop(
	ctx context.Context,
	tenantID, accountID int64,
	days int,
	sortBy string,
	top, page, pageSize int,
) (*NASTopResp, error) {
	days = normalizeNASDays(days)
	top = normalizeBound(top, nasDefaultTop, nasMaxTop)
	page = normalizeBound(page, nasDefaultPage, 1<<30)
	pageSize = normalizeBound(pageSize, nasDefaultPSize, nasMaxPageSize)
	switch sortBy {
	case NASSortCapacity, NASSortUtilization:
	default:
		sortBy = NASSortCapacity
	}

	// account_id 可选:空 = 全部租户账号;非空则校验归属(越权 404)
	scope, err := s.resolveAccountScope(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	if len(scope) == 0 {
		return &NASTopResp{Page: page, PageSize: pageSize, Items: []NASTopItem{}}, nil
	}

	rows, err := s.metrics.ListByAccounts(ctx, scope, days)
	if err != nil {
		return nil, fmt.Errorf("查询 NAS Top 指标失败: %w", err)
	}

	items := nasAggregateTop(rows)
	total := len(items)
	sort.SliceStable(items, func(i, j int) bool {
		if sortBy == NASSortUtilization {
			return nasSummaryAvgUtil(items[i].Average) > nasSummaryAvgUtil(items[j].Average)
		}
		return deref(items[i].Latest.Capacity) > deref(items[j].Latest.Capacity)
	})

	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return &NASTopResp{
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		Items:    items[start:end],
	}, nil
}

// resolveAccountScope 解析查询的账号范围:account_id>0 时校验 ∈ 租户账号集合
// (越权返回 ErrNASAccountNotInTenant,handler 映射 404);0 表示全部租户账号。
func (s *NASQueryService) resolveAccountScope(ctx context.Context, tenantID, accountID int64) ([]int64, error) {
	accountIDs, err := s.tenantAccountIDs(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if accountID <= 0 {
		return accountIDs, nil
	}
	for _, id := range accountIDs {
		if id == accountID {
			return []int64{accountID}, nil
		}
	}
	return nil, ErrNASAccountNotInTenant
}

// tenantAccountIDs 取租户全部云账号 ID(逐账号隔离模式,平移 CDN 查询服务)
func (s *NASQueryService) tenantAccountIDs(ctx context.Context, tenantID int64) ([]int64, error) {
	accounts, _, err := s.accountRepo.List(ctx, domain.CloudAccountFilter{TenantID: tenantID})
	if err != nil {
		return nil, fmt.Errorf("获取租户云账号失败: %w", err)
	}
	ids := make([]int64, 0, len(accounts))
	for _, a := range accounts {
		ids = append(ids, a.ID)
	}
	return ids, nil
}

// nasAggregateTop 按物理 fs 聚合去重:同 fs 多账号同日多行时,同日内取容量最大
// 行(「日期 desc,再容量 desc」取第一行的等价实现),均值亦按「每日代表行」
// 计算避免跨账号双计;capacity=0 异常行不参与均值。
func nasAggregateTop(rows []types.NASMetric) []NASTopItem {
	type fsAgg struct {
		item    NASTopItem
		acctSet map[int64]struct{}
		byDate  map[string]types.NASMetric
	}
	aggs := make(map[string]*fsAgg)
	order := make([]string, 0, len(aggs))
	for _, m := range rows {
		agg, ok := aggs[m.FsID]
		if !ok {
			agg = &fsAgg{
				item: NASTopItem{
					FsID:       m.FsID,
					FsName:     m.FsName,
					Provider:   m.Provider,
					AccountIDs: []int64{},
					DataStatus: nasDataStatus(m.QcStatus),
					QcStatus:   m.QcStatus,
				},
				acctSet: map[int64]struct{}{},
				byDate:  map[string]types.NASMetric{},
			}
			aggs[m.FsID] = agg
			order = append(order, m.FsID)
		}
		if _, seen := agg.acctSet[m.AccountID]; !seen {
			agg.acctSet[m.AccountID] = struct{}{}
			agg.item.AccountIDs = append(agg.item.AccountIDs, m.AccountID)
		}
		// 同日代表行:容量最大(规格「同日多账号行内再取容量最大」);
		// 代表行的 qc_status 随之携带(同日异常行容量为 0,仅在该日只有异常行时胜出)
		if cur, ok := agg.byDate[m.Date]; !ok || m.Capacity > cur.Capacity {
			agg.byDate[m.Date] = m
		}
	}

	items := make([]NASTopItem, 0, len(order))
	for _, fsID := range order {
		agg := aggs[fsID]
		sort.Slice(agg.item.AccountIDs, func(i, j int) bool { return agg.item.AccountIDs[i] < agg.item.AccountIDs[j] })

		// 代表行 = 最新日期的同日代表行
		dates := make([]string, 0, len(agg.byDate))
		for d := range agg.byDate {
			dates = append(dates, d)
		}
		sort.Sort(sort.Reverse(sort.StringSlice(dates)))
		rep := agg.byDate[dates[0]]
		agg.item.Latest = NASMetricSummary{
			Date:        rep.Date,
			Capacity:    floatPtr(rep.Capacity),
			Used:        floatPtr(rep.UsedCapacity),
			Utilization: nasUtilization(rep.Capacity, rep.UsedCapacity),
		}
		agg.item.Average = *nasAverageSummary(dailyReps(agg.byDate))
		items = append(items, agg.item)
	}
	return items
}

// dailyReps 按日期升序展开每日代表行
func dailyReps(byDate map[string]types.NASMetric) []types.NASMetric {
	dates := make([]string, 0, len(byDate))
	for d := range byDate {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	rows := make([]types.NASMetric, 0, len(dates))
	for _, d := range dates {
		rows = append(rows, byDate[d])
	}
	return rows
}

// nasLatestSummary 最新一天的值(rows 按 date 升序时即末行;空则 nil)
func nasLatestSummary(rows []types.NASMetric) *NASMetricSummary {
	if len(rows) == 0 {
		return nil
	}
	m := rows[len(rows)-1]
	return &NASMetricSummary{
		Date:        m.Date,
		Capacity:    floatPtr(m.Capacity),
		Used:        floatPtr(m.UsedCapacity),
		Utilization: nasUtilization(m.Capacity, m.UsedCapacity),
	}
}

// nasAverageSummary 近 N 天均值:capacity=0 异常行与缺失日跳过不参与
// (不记 0 拉低均值);无可用行时各字段为 null。
func nasAverageSummary(rows []types.NASMetric) *NASMetricSummary {
	var sumCap, sumUsed, sumUtil float64
	var n, nUtil int
	for _, m := range rows {
		if m.Capacity == 0 {
			continue
		}
		sumCap += m.Capacity
		sumUsed += m.UsedCapacity
		n++
		if u := nasUtilization(m.Capacity, m.UsedCapacity); u != nil {
			sumUtil += *u
			nUtil++
		}
	}
	if n == 0 {
		return &NASMetricSummary{}
	}
	summary := &NASMetricSummary{
		Capacity: floatPtr(sumCap / float64(n)),
		Used:     floatPtr(sumUsed / float64(n)),
	}
	if nUtil > 0 {
		summary.Utilization = floatPtr(sumUtil / float64(nUtil))
	}
	return summary
}

// nasSummaryAvgUtil Top 排序键:均值使用率(nil 视为 -1,排尾部)
func nasSummaryAvgUtil(s NASMetricSummary) float64 {
	if s.Utilization == nil {
		return -1
	}
	return *s.Utilization
}

func deref(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

// normalizeBound 边界收敛:v<=0 回默认值,v>max 收敛到 max
func normalizeBound(v, def, max int) int {
	if v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}

// warnIfUsedExceedsCapacity used>capacity 时按 min(used,capacity) 口径收敛使用率,
// 原始 used 仍原样返回(规格「单位归一化与字段语义」),打 warn 供排查。
func (s *NASQueryService) warnIfUsedExceedsCapacity(m types.NASMetric) {
	if m.Capacity > 0 && m.UsedCapacity > m.Capacity {
		s.logger.Warn("NAS 指标 used>capacity,使用率按 min(used,capacity) 收敛",
			elog.String("fs_id", m.FsID),
			elog.String("date", m.Date),
			elog.Any("capacity", m.Capacity),
			elog.Any("used", m.UsedCapacity))
	}
}
