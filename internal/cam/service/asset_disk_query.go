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

// Disk 指标读取参数边界(规格:days 限 1~90;top 默认 10 最大 50;分页默认 10
// 最大 50;与 NAS/OSS 读取同口径,常量独立命名避免跨资源语义漂移)
const (
	diskDefaultDays  = 30
	diskMaxDays      = 90
	diskDefaultTop   = 10
	diskMaxTop       = 50
	diskDefaultPage  = 1
	diskDefaultPSize = 10
	diskMaxPageSize  = 50
)

// data_status 读取侧取值(qc_status 闭环 + 缺失日标注,规格「Proposed Solution」第 5 点)
const (
	// DiskDataStatusOK 行存在且数据正常(含 busy_share 合法闲盘 0)
	DiskDataStatusOK = "ok"
	// DiskDataStatusZeroException qc_status=zero_exception 且口径缺失的 0 值行:
	// 原样暴露并映射进 data_status,前端据此渲染警示而非当正常空盘
	DiskDataStatusZeroException = "zero_exception"
	// DiskDataStatusMissing 窗口内缺失日:不填充假值,各指标为 null
	DiskDataStatusMissing = "missing"
)

// Top 排序键取值(sort 参数,排序值统一用近 N 天均值口径,规格明示)
const (
	DiskSortUsagePercent = "usage_percent"
	DiskSortIOPS         = "iops"
	DiskSortThroughput   = "throughput"
)

// ErrDiskAccountNotInTenant 客户端传入的 account_id 不属于当前租户(越权)。
// handler 须映射 404 而非 403——不泄露账号存在性(规格 Non-Functional Requirements)。
var ErrDiskAccountNotInTenant = errors.New("cloud account not found")

// DiskMetricReader Disk 指标只读接口(service 层消费;dao.DiskMetricDAO 天然满足)。
// 二期指标读取只查本地指标表,不走厂商 API。
type DiskMetricReader interface {
	// ListByDisk 取指定账号+disk_id 近 N 天单日指标,按 date 升序(趋势接口)
	ListByDisk(ctx context.Context, accountID int64, diskID string, days int) ([]types.DiskMetric, error)
	// ListByAccounts 取一组账号近 N 天全部指标行(Top 聚合,服务层按 disk_id 去重)
	ListByAccounts(ctx context.Context, accountIDs []int64, days int) ([]types.DiskMetric, error)
}

// DiskMetricPoint 趋势单日数据点。缺失日各指标为 null(不填充假值,以
// data_status=missing 标注);qc_status 原样透出落库标注,前端结合 usage_scope
// 甄别 0 值语义(口径缺失异常 vs busy_share 合法闲盘)。
type DiskMetricPoint struct {
	Date         string   `json:"date"`          // YYYY-MM-DD(Asia/Shanghai)
	UsagePercent *float64 `json:"usage_percent"` // 使用率(百分比 0~100,口径见 usage_scope);缺失日 null
	UsageScope   string   `json:"usage_scope"`   // 口径标注:instance_level / busy_share / cloud_disk_level(空=厂商未提供使用率)
	IOPS         *float64 `json:"iops"`          // IOPS(次/秒,当日均值);缺失日 null
	Throughput   *float64 `json:"throughput"`    // 吞吐(MB/s,当日均值);缺失日 null
	DataStatus   string   `json:"data_status"`   // ok | zero_exception | missing
	QcStatus     string   `json:"qc_status"`     // 原样透出落库 qc_status(空=正常)
}

// DiskMetricSummary 最新一天 / 近 N 天均值两类值。
// 无可用行(窗口内全缺失或全为口径缺失 0 异常行)时字段为 null。
type DiskMetricSummary struct {
	Date         string   `json:"date,omitempty"` // 仅「最新一天」携带
	UsagePercent *float64 `json:"usage_percent"`  // 使用率(百分比);均值口径时无可用行为 null
	IOPS         *float64 `json:"iops"`           // 均值口径时无可用行为 null
	Throughput   *float64 `json:"throughput"`     // 均值口径时无可用行为 null
}

// DiskMetricsResp GET /assets/disk/metrics 响应
type DiskMetricsResp struct {
	DiskID  string             `json:"disk_id"`
	Days    []DiskMetricPoint  `json:"days"` // 按日期升序
	Latest  *DiskMetricSummary `json:"latest"`
	Average *DiskMetricSummary `json:"average"`
}

// DiskTopItem Top 单项:按 disk_id 去重后的代表行(最新一天)+ 近 N 天均值。
// 跨账号同盘(共享盘)并存时 AccountIDs 为去重后的账号列表(口径与 T9 前端
// 对齐项,见任务 Implementation Notes)。
type DiskTopItem struct {
	DiskID     string            `json:"disk_id"`
	DiskName   string            `json:"disk_name"`
	Provider   string            `json:"provider"`
	UsageScope string            `json:"usage_scope"`
	AccountIDs []int64           `json:"account_id"`
	DataStatus string            `json:"data_status"`
	QcStatus   string            `json:"qc_status"`
	Latest     DiskMetricSummary `json:"latest"`
	Average    DiskMetricSummary `json:"average"`
}

// DiskTopResp GET /assets/disk/top 响应
type DiskTopResp struct {
	Total    int           `json:"total"` // 去重后的磁盘总数(分页分母)
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
	Items    []DiskTopItem `json:"items"`
}

// DiskQueryService Disk 指标读取服务(纯读指标表,租户校验平移 OSS 模式)。
type DiskQueryService struct {
	accountRepo repository.CloudAccountRepository
	metrics     DiskMetricReader
	logger      *elog.Component
}

// NewDiskQueryService 创建 Disk 指标读取服务
func NewDiskQueryService(
	accountRepo repository.CloudAccountRepository,
	metrics DiskMetricReader,
	logger *elog.Component,
) *DiskQueryService {
	return &DiskQueryService{accountRepo: accountRepo, metrics: metrics, logger: logger}
}

// diskCSTZone 运营时区(与采集/DAO 侧一致,按 Asia/Shanghai 自然日切分)
var diskCSTZone = nasCSTZone

func diskToday() time.Time {
	return time.Now().In(diskCSTZone)
}

// diskIsMissingScopeZero 口径缺失 0 判定(T2 记录 + DiskMetricQcZeroException 注释):
// usage_percent=0 异常行需结合 usage_scope 甄别——
//   - usage_scope=busy_share(AWS 全闲盘派生 0.00%)→ 合法闲盘,是真数据;
//   - 其余(口径为空=厂商未提供使用率,或 instance_level 口径缺失)→ 异常行。
func diskIsMissingScopeZero(m types.DiskMetric) bool {
	return m.QcStatus == types.DiskMetricQcZeroException && m.UsageScope != types.DiskUsageScopeBusyShare
}

// diskDataStatus qc_status → data_status 映射(闭环:原样暴露 + 映射)。
// busy_share 合法闲盘 0 映射 ok(是真数据不是异常),口径缺失 0 映射 zero_exception。
func diskDataStatus(qcStatus, usageScope string) string {
	if qcStatus == types.DiskMetricQcZeroException && usageScope != types.DiskUsageScopeBusyShare {
		return DiskDataStatusZeroException
	}
	return DiskDataStatusOK
}

// diskSummaryAvgUsage Top usage_percent 排序键:均值使用率(nil 视为 -1,排尾部)
func diskSummaryAvgUsage(s DiskMetricSummary) float64 {
	if s.UsagePercent == nil {
		return -1
	}
	return *s.UsagePercent
}

// diskSummaryAvgIOPS Top iops 排序键:均值 IOPS(nil 视为 -1,排尾部)
func diskSummaryAvgIOPS(s DiskMetricSummary) float64 {
	if s.IOPS == nil {
		return -1
	}
	return *s.IOPS
}

// diskSummaryAvgThroughput Top throughput 排序键:均值吞吐(nil 视为 -1,排尾部)
func diskSummaryAvgThroughput(s DiskMetricSummary) float64 {
	if s.Throughput == nil {
		return -1
	}
	return *s.Throughput
}

// GetDiskMetrics 查询租户内指定账号下某云盘近 N 天单日指标趋势。
// days[] 按日期升序,缺失日以 data_status=missing 标注不填充假值;
// 同时返回「最新一天」与「近 N 天均值」两类值。
func (s *DiskQueryService) GetDiskMetrics(
	ctx context.Context,
	tenantID, accountID int64,
	diskID string,
	days int,
) (*DiskMetricsResp, error) {
	if diskID == "" {
		return nil, fmt.Errorf("缺少 disk_id 参数")
	}
	if accountID <= 0 {
		return nil, fmt.Errorf("缺少 account_id 参数")
	}
	days = normalizeNASDays(days)

	// 租户校验:account_id ∉ 租户账号集合 → 越权(handler 映射 404)
	if _, err := s.resolveAccountScope(ctx, tenantID, accountID); err != nil {
		return nil, err
	}

	rows, err := s.metrics.ListByDisk(ctx, accountID, diskID, days)
	if err != nil {
		return nil, fmt.Errorf("查询 Disk 指标失败: %w", err)
	}

	resp := &DiskMetricsResp{DiskID: diskID, Days: []DiskMetricPoint{}}
	rowsByDate := make(map[string]types.DiskMetric, len(rows))
	for _, m := range rows {
		rowsByDate[m.Date] = m
	}

	// 窗口逐日展开(升序),缺失日不填充假值
	today := diskToday()
	for i := days - 1; i >= 0; i-- {
		date := today.AddDate(0, 0, -i).Format("2006-01-02")
		m, ok := rowsByDate[date]
		if !ok {
			resp.Days = append(resp.Days, DiskMetricPoint{Date: date, DataStatus: DiskDataStatusMissing})
			continue
		}
		resp.Days = append(resp.Days, DiskMetricPoint{
			Date:         date,
			UsagePercent: floatPtr(m.UsagePercent),
			UsageScope:   m.UsageScope,
			IOPS:         floatPtr(m.IOPS),
			Throughput:   floatPtr(m.Throughput),
			DataStatus:   diskDataStatus(m.QcStatus, m.UsageScope),
			QcStatus:     m.QcStatus,
		})
	}

	resp.Latest = diskLatestSummary(rows)
	resp.Average = diskAverageSummary(rows)
	return resp, nil
}

// GetTop 查询租户内近 N 天 Disk 使用 Top(账号视角)。
// Top 按 disk_id 去重:多账号(共享盘)并存按「日期 desc,再使用率 desc」取第一行
// (代表行,不跨账号求和/平均,避免共享盘双计);三个排序键统一用近 N 天均值
// 口径;口径缺失 0 异常行不参与均值,busy_share 合法闲盘 0 参与平均;无数据
// 磁盘自然跳过(不记 0)。
func (s *DiskQueryService) GetTop(
	ctx context.Context,
	tenantID, accountID int64,
	days int,
	sortBy string,
	top, page, pageSize int,
) (*DiskTopResp, error) {
	days = normalizeNASDays(days)
	top = normalizeBound(top, diskDefaultTop, diskMaxTop)
	page = normalizeBound(page, diskDefaultPage, 1<<30)
	pageSize = normalizeBound(pageSize, diskDefaultPSize, diskMaxPageSize)
	switch sortBy {
	case DiskSortUsagePercent, DiskSortIOPS, DiskSortThroughput:
	default:
		sortBy = DiskSortUsagePercent
	}

	// account_id 可选:空 = 全部租户账号;非空则校验归属(越权 404)
	scope, err := s.resolveAccountScope(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	if len(scope) == 0 {
		return &DiskTopResp{Page: page, PageSize: pageSize, Items: []DiskTopItem{}}, nil
	}

	rows, err := s.metrics.ListByAccounts(ctx, scope, days)
	if err != nil {
		return nil, fmt.Errorf("查询 Disk Top 指标失败: %w", err)
	}

	items := diskAggregateTop(rows)
	total := len(items)
	sort.SliceStable(items, func(i, j int) bool {
		switch sortBy {
		case DiskSortIOPS:
			return diskSummaryAvgIOPS(items[i].Average) > diskSummaryAvgIOPS(items[j].Average)
		case DiskSortThroughput:
			return diskSummaryAvgThroughput(items[i].Average) > diskSummaryAvgThroughput(items[j].Average)
		default:
			return diskSummaryAvgUsage(items[i].Average) > diskSummaryAvgUsage(items[j].Average)
		}
	})

	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return &DiskTopResp{
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		Items:    items[start:end],
	}, nil
}

// resolveAccountScope 解析查询的账号范围:account_id>0 时校验 ∈ 租户账号集合
// (越权返回 ErrDiskAccountNotInTenant,handler 映射 404);0 表示全部租户账号。
func (s *DiskQueryService) resolveAccountScope(ctx context.Context, tenantID, accountID int64) ([]int64, error) {
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
	return nil, ErrDiskAccountNotInTenant
}

// tenantAccountIDs 取租户全部云账号 ID(逐账号隔离模式,平移 OSS 查询服务)
func (s *DiskQueryService) tenantAccountIDs(ctx context.Context, tenantID int64) ([]int64, error) {
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

// diskAggregateTop 按物理磁盘聚合去重:同盘多账号(共享盘)同日多行时,同日内取
// usage_percent 最大行(「日期 desc,再使用率 desc」取第一行的等价实现),均值亦
// 按「每日代表行」计算避免跨账号双计;item 级 qc_status/data_status/usage_scope
// 随代表行携带。
func diskAggregateTop(rows []types.DiskMetric) []DiskTopItem {
	type diskAgg struct {
		item    DiskTopItem
		acctSet map[int64]struct{}
		byDate  map[string]types.DiskMetric
	}
	aggs := make(map[string]*diskAgg)
	order := make([]string, 0, len(rows))
	for _, m := range rows {
		agg, ok := aggs[m.DiskID]
		if !ok {
			agg = &diskAgg{
				item: DiskTopItem{
					DiskID:     m.DiskID,
					DiskName:   m.DiskName,
					Provider:   m.Provider,
					AccountIDs: []int64{},
				},
				acctSet: map[int64]struct{}{},
				byDate:  map[string]types.DiskMetric{},
			}
			aggs[m.DiskID] = agg
			order = append(order, m.DiskID)
		}
		if _, seen := agg.acctSet[m.AccountID]; !seen {
			agg.acctSet[m.AccountID] = struct{}{}
			agg.item.AccountIDs = append(agg.item.AccountIDs, m.AccountID)
		}
		// 同日代表行:usage_percent 最大;代表行的 qc_status/usage_scope 随之携带
		// (同日异常行使用率为 0,仅在该日只有异常行时胜出)
		if cur, ok := agg.byDate[m.Date]; !ok || m.UsagePercent > cur.UsagePercent {
			agg.byDate[m.Date] = m
		}
	}

	items := make([]DiskTopItem, 0, len(order))
	for _, diskID := range order {
		agg := aggs[diskID]
		sort.Slice(agg.item.AccountIDs, func(i, j int) bool { return agg.item.AccountIDs[i] < agg.item.AccountIDs[j] })

		// 代表行 = 最新日期的同日代表行(dailyDiskReps 按日期升序展开,取末行)
		reps := dailyDiskReps(agg.byDate)
		rep := reps[len(reps)-1]
		agg.item.Latest = DiskMetricSummary{
			Date:         rep.Date,
			UsagePercent: floatPtr(rep.UsagePercent),
			IOPS:         floatPtr(rep.IOPS),
			Throughput:   floatPtr(rep.Throughput),
		}
		agg.item.DataStatus = diskDataStatus(rep.QcStatus, rep.UsageScope)
		agg.item.QcStatus = rep.QcStatus
		agg.item.UsageScope = rep.UsageScope
		agg.item.Average = *diskAverageSummary(reps)
		items = append(items, agg.item)
	}
	return items
}

// dailyDiskReps 按日期升序展开每日代表行
func dailyDiskReps(byDate map[string]types.DiskMetric) []types.DiskMetric {
	dates := make([]string, 0, len(byDate))
	for d := range byDate {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	rows := make([]types.DiskMetric, 0, len(dates))
	for _, d := range dates {
		rows = append(rows, byDate[d])
	}
	return rows
}

// diskLatestSummary 最新一天的值(rows 按 date 升序时即末行;空则 nil)
func diskLatestSummary(rows []types.DiskMetric) *DiskMetricSummary {
	if len(rows) == 0 {
		return nil
	}
	m := rows[len(rows)-1]
	return &DiskMetricSummary{
		Date:         m.Date,
		UsagePercent: floatPtr(m.UsagePercent),
		IOPS:         floatPtr(m.IOPS),
		Throughput:   floatPtr(m.Throughput),
	}
}

// diskAverageSummary 近 N 天均值:口径缺失 0 异常行与缺失日跳过不参与(不记 0
// 拉低均值);busy_share 合法闲盘 0 是真数据,参与平均;无可用行时各字段为 null。
func diskAverageSummary(rows []types.DiskMetric) *DiskMetricSummary {
	var sumUsage, sumIOPS, sumTP float64
	var n int
	for _, m := range rows {
		if diskIsMissingScopeZero(m) {
			continue
		}
		sumUsage += m.UsagePercent
		sumIOPS += m.IOPS
		sumTP += m.Throughput
		n++
	}
	if n == 0 {
		return &DiskMetricSummary{}
	}
	return &DiskMetricSummary{
		UsagePercent: floatPtr(sumUsage / float64(n)),
		IOPS:         floatPtr(sumIOPS / float64(n)),
		Throughput:   floatPtr(sumTP / float64(n)),
	}
}
