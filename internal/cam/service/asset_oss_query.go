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

// OSS 指标读取参数边界(规格:days 限 1~90;top 默认 10 最大 50;分页默认 10 最大 50;
// 与 NAS 读取同口径,常量独立命名避免跨资源语义漂移)
const (
	ossDefaultDays  = 30
	ossMaxDays      = 90
	ossDefaultTop   = 10
	ossMaxTop       = 50
	ossDefaultPage  = 1
	ossDefaultPSize = 10
	ossMaxPageSize  = 50
)

// data_status 读取侧取值(qc_status 闭环 + 缺失日标注,规格「Proposed Solution」第 5 点)
const (
	// OSSDataStatusOK 行存在且数据正常
	OSSDataStatusOK = "ok"
	// OSSDataStatusZeroException qc_status=zero_exception 映射:storage_size=0 异常行
	// 原样暴露并映射进 data_status,前端据此渲染警示而非当正常空桶
	OSSDataStatusZeroException = "zero_exception"
	// OSSDataStatusMissing 窗口内缺失日:不填充假值,storage_size/object_count 为 null
	OSSDataStatusMissing = "missing"
)

// Top 排序键取值(sort 参数,排序值用近 N 天均值口径)
const (
	OSSSortStorageSize = "storage_size"
	OSSSortObjectCount = "object_count"
)

// ErrOSSAccountNotInTenant 客户端传入的 account_id 不属于当前租户(越权)。
// handler 须映射 404 而非 403——不泄露账号存在性(规格 Non-Functional Requirements)。
var ErrOSSAccountNotInTenant = errors.New("cloud account not found")

// OSSMetricReader OSS 指标只读接口(service 层消费;dao.OSSMetricDAO 天然满足)。
// 二期指标读取只查本地指标表,不走厂商 API。
type OSSMetricReader interface {
	// ListByBucket 取指定账号+bucket_name 近 N 天单日指标,按 date 升序(趋势接口)
	ListByBucket(ctx context.Context, accountID int64, bucketName string, days int) ([]types.OSSMetric, error)
	// ListByAccounts 取一组账号近 N 天全部指标行(Top 聚合,服务层按 bucket_name 去重)
	ListByAccounts(ctx context.Context, accountIDs []int64, days int) ([]types.OSSMetric, error)
}

// OSSMetricPoint 趋势单日数据点。缺失日 storage_size/object_count 为 null
// (不填充假值,以 data_status=missing 标注);zero_exception 日 storage_size=0
// 原样暴露(前端可分辨异常而非当正常空桶)。
type OSSMetricPoint struct {
	Date        string   `json:"date"`         // YYYY-MM-DD(Asia/Shanghai)
	StorageSize *float64 `json:"storage_size"` // 存储量(GB);缺失日 null;zero_exception 日为 0
	ObjectCount *int64   `json:"object_count"` // 对象数量;缺失日 null
	DataStatus  string   `json:"data_status"`  // ok | zero_exception | missing
	QcStatus    string   `json:"qc_status"`    // 原样透出落库 qc_status(空=正常)
}

// OSSMetricSummary 最新一天 / 近 N 天均值两类值。
// 无可用行(窗口内全缺失或全为 storage_size=0 异常行)时字段为 null。
type OSSMetricSummary struct {
	Date        string   `json:"date,omitempty"` // 仅「最新一天」携带
	StorageSize *float64 `json:"storage_size"`   // 存储量(GB);均值口径时无可用行为 null
	ObjectCount *float64 `json:"object_count"`   // 对象数量;均值口径时无可用行为 null
}

// OSSBucketMetricsResp GET /assets/oss/metrics 响应
type OSSBucketMetricsResp struct {
	BucketName string            `json:"bucket_name"`
	Days       []OSSMetricPoint  `json:"days"` // 按日期升序
	Latest     *OSSMetricSummary `json:"latest"`
	Average    *OSSMetricSummary `json:"average"`
}

// OSSTopItem Top 单项:按 bucket_name 去重后的代表行(最新一天)+ 近 N 天均值。
// 跨账号同 bucket_name 并存时 AccountIDs 为去重后的账号列表
// (口径与 T9 前端对齐项,见任务 Implementation Notes)。
type OSSTopItem struct {
	BucketName string           `json:"bucket_name"`
	Provider   string           `json:"provider"`
	AccountIDs []int64          `json:"account_id"`
	DataStatus string           `json:"data_status"`
	QcStatus   string           `json:"qc_status"`
	Latest     OSSMetricSummary `json:"latest"`
	Average    OSSMetricSummary `json:"average"`
}

// OSSTopResp GET /assets/oss/top 响应
type OSSTopResp struct {
	Total    int          `json:"total"` // 去重后的 bucket 总数(分页分母)
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
	Items    []OSSTopItem `json:"items"`
}

// OSSQueryService OSS 指标读取服务(纯读指标表,租户校验平移 NAS 模式)。
type OSSQueryService struct {
	accountRepo repository.CloudAccountRepository
	metrics     OSSMetricReader
	logger      *elog.Component
}

// NewOSSQueryService 创建 OSS 指标读取服务
func NewOSSQueryService(
	accountRepo repository.CloudAccountRepository,
	metrics OSSMetricReader,
	logger *elog.Component,
) *OSSQueryService {
	return &OSSQueryService{accountRepo: accountRepo, metrics: metrics, logger: logger}
}

// ossCSTZone 运营时区(与采集/DAO 侧一致,按 Asia/Shanghai 自然日切分)
var ossCSTZone = nasCSTZone

func ossToday() time.Time {
	return time.Now().In(ossCSTZone)
}

// ossDataStatus qc_status → data_status 映射(闭环:原样暴露 + 映射)
func ossDataStatus(qcStatus string) string {
	if qcStatus == types.OSSMetricQcZeroException {
		return OSSDataStatusZeroException
	}
	return OSSDataStatusOK
}

// ossSummaryAvgStorage Top storage_size 排序键:均值存储量(nil 视为 -1,排尾部)
func ossSummaryAvgStorage(s OSSMetricSummary) float64 {
	if s.StorageSize == nil {
		return -1
	}
	return *s.StorageSize
}

// ossSummaryAvgObjects Top object_count 排序键:均值对象数(nil 视为 -1,排尾部)
func ossSummaryAvgObjects(s OSSMetricSummary) float64 {
	if s.ObjectCount == nil {
		return -1
	}
	return *s.ObjectCount
}

// GetBucketMetrics 查询租户内指定账号下某 OSS bucket 近 N 天单日指标趋势。
// days[] 按日期升序,缺失日以 data_status=missing 标注不填充假值;
// 同时返回「最新一天」与「近 N 天均值」两类值。
func (s *OSSQueryService) GetBucketMetrics(
	ctx context.Context,
	tenantID, accountID int64,
	bucketName string,
	days int,
) (*OSSBucketMetricsResp, error) {
	if bucketName == "" {
		return nil, fmt.Errorf("缺少 bucket_name 参数")
	}
	if accountID <= 0 {
		return nil, fmt.Errorf("缺少 account_id 参数")
	}
	days = normalizeNASDays(days)

	// 租户校验:account_id ∉ 租户账号集合 → 越权(handler 映射 404)
	if _, err := s.resolveAccountScope(ctx, tenantID, accountID); err != nil {
		return nil, err
	}

	rows, err := s.metrics.ListByBucket(ctx, accountID, bucketName, days)
	if err != nil {
		return nil, fmt.Errorf("查询 OSS 指标失败: %w", err)
	}

	resp := &OSSBucketMetricsResp{BucketName: bucketName, Days: []OSSMetricPoint{}}
	rowsByDate := make(map[string]types.OSSMetric, len(rows))
	for _, m := range rows {
		rowsByDate[m.Date] = m
	}

	// 窗口逐日展开(升序),缺失日不填充假值
	today := ossToday()
	for i := days - 1; i >= 0; i-- {
		date := today.AddDate(0, 0, -i).Format("2006-01-02")
		m, ok := rowsByDate[date]
		if !ok {
			resp.Days = append(resp.Days, OSSMetricPoint{Date: date, DataStatus: OSSDataStatusMissing})
			continue
		}
		resp.Days = append(resp.Days, OSSMetricPoint{
			Date:        date,
			StorageSize: floatPtr(m.StorageSize),
			ObjectCount: int64Ptr(m.ObjectCount),
			DataStatus:  ossDataStatus(m.QcStatus),
			QcStatus:    m.QcStatus,
		})
	}

	resp.Latest = ossLatestSummary(rows)
	resp.Average = ossAverageSummary(rows)
	return resp, nil
}

// GetTop 查询租户内近 N 天 OSS 存储 Top(账号视角)。
// Top 按 bucket_name 去重:多账号并存按「日期 desc,再容量 desc」取第一行(代表行,
// 不跨账号求和/平均,避免共享存储双计);排序值用近 N 天均值口径(storage_size|
// object_count);均值对 storage_size=0 异常行跳过不参与;无数据 bucket 自然跳过。
func (s *OSSQueryService) GetTop(
	ctx context.Context,
	tenantID, accountID int64,
	days int,
	sortBy string,
	top, page, pageSize int,
) (*OSSTopResp, error) {
	days = normalizeNASDays(days)
	top = normalizeBound(top, ossDefaultTop, ossMaxTop)
	page = normalizeBound(page, ossDefaultPage, 1<<30)
	pageSize = normalizeBound(pageSize, ossDefaultPSize, ossMaxPageSize)
	switch sortBy {
	case OSSSortStorageSize, OSSSortObjectCount:
	default:
		sortBy = OSSSortStorageSize
	}

	// account_id 可选:空 = 全部租户账号;非空则校验归属(越权 404)
	scope, err := s.resolveAccountScope(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	if len(scope) == 0 {
		return &OSSTopResp{Page: page, PageSize: pageSize, Items: []OSSTopItem{}}, nil
	}

	rows, err := s.metrics.ListByAccounts(ctx, scope, days)
	if err != nil {
		return nil, fmt.Errorf("查询 OSS Top 指标失败: %w", err)
	}

	items := ossAggregateTop(rows)
	total := len(items)
	sort.SliceStable(items, func(i, j int) bool {
		if sortBy == OSSSortObjectCount {
			return ossSummaryAvgObjects(items[i].Average) > ossSummaryAvgObjects(items[j].Average)
		}
		return ossSummaryAvgStorage(items[i].Average) > ossSummaryAvgStorage(items[j].Average)
	})

	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return &OSSTopResp{
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		Items:    items[start:end],
	}, nil
}

// resolveAccountScope 解析查询的账号范围:account_id>0 时校验 ∈ 租户账号集合
// (越权返回 ErrOSSAccountNotInTenant,handler 映射 404);0 表示全部租户账号。
func (s *OSSQueryService) resolveAccountScope(ctx context.Context, tenantID, accountID int64) ([]int64, error) {
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
	return nil, ErrOSSAccountNotInTenant
}

// tenantAccountIDs 取租户全部云账号 ID(逐账号隔离模式,平移 NAS 查询服务)
func (s *OSSQueryService) tenantAccountIDs(ctx context.Context, tenantID int64) ([]int64, error) {
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

// ossAggregateTop 按物理 bucket 聚合去重:同 bucket 多账号同日多行时,同日内取
// storage_size 最大行(「日期 desc,再容量 desc」取第一行的等价实现),均值亦按
// 「每日代表行」计算避免跨账号双计;storage_size=0 异常行不参与均值。
func ossAggregateTop(rows []types.OSSMetric) []OSSTopItem {
	type bucketAgg struct {
		item    OSSTopItem
		acctSet map[int64]struct{}
		byDate  map[string]types.OSSMetric
	}
	aggs := make(map[string]*bucketAgg)
	order := make([]string, 0, len(rows))
	for _, m := range rows {
		agg, ok := aggs[m.BucketName]
		if !ok {
			agg = &bucketAgg{
				item: OSSTopItem{
					BucketName: m.BucketName,
					Provider:   m.Provider,
					AccountIDs: []int64{},
					DataStatus: ossDataStatus(m.QcStatus),
					QcStatus:   m.QcStatus,
				},
				acctSet: map[int64]struct{}{},
				byDate:  map[string]types.OSSMetric{},
			}
			aggs[m.BucketName] = agg
			order = append(order, m.BucketName)
		}
		if _, seen := agg.acctSet[m.AccountID]; !seen {
			agg.acctSet[m.AccountID] = struct{}{}
			agg.item.AccountIDs = append(agg.item.AccountIDs, m.AccountID)
		}
		// 同日代表行:storage_size 最大(规格「同日多账号行内再取容量最大」);
		// 代表行的 qc_status 随之携带(同日异常行容量为 0,仅在该日只有异常行时胜出)
		if cur, ok := agg.byDate[m.Date]; !ok || m.StorageSize > cur.StorageSize {
			agg.byDate[m.Date] = m
		}
	}

	items := make([]OSSTopItem, 0, len(order))
	for _, name := range order {
		agg := aggs[name]
		sort.Slice(agg.item.AccountIDs, func(i, j int) bool { return agg.item.AccountIDs[i] < agg.item.AccountIDs[j] })

		// 代表行 = 最新日期的同日代表行(dailyOSSReps 按日期升序展开,取末行)
		reps := dailyOSSReps(agg.byDate)
		rep := reps[len(reps)-1]
		agg.item.Latest = OSSMetricSummary{
			Date:        rep.Date,
			StorageSize: floatPtr(rep.StorageSize),
			ObjectCount: floatPtr(float64(rep.ObjectCount)),
		}
		agg.item.Average = *ossAverageSummary(dailyOSSReps(agg.byDate))
		items = append(items, agg.item)
	}
	return items
}

// dailyOSSReps 按日期升序展开每日代表行
func dailyOSSReps(byDate map[string]types.OSSMetric) []types.OSSMetric {
	dates := make([]string, 0, len(byDate))
	for d := range byDate {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	rows := make([]types.OSSMetric, 0, len(dates))
	for _, d := range dates {
		rows = append(rows, byDate[d])
	}
	return rows
}

// ossLatestSummary 最新一天的值(rows 按 date 升序时即末行;空则 nil)
func ossLatestSummary(rows []types.OSSMetric) *OSSMetricSummary {
	if len(rows) == 0 {
		return nil
	}
	m := rows[len(rows)-1]
	return &OSSMetricSummary{
		Date:        m.Date,
		StorageSize: floatPtr(m.StorageSize),
		ObjectCount: floatPtr(float64(m.ObjectCount)),
	}
}

// ossAverageSummary 近 N 天均值:storage_size=0 异常行与缺失日跳过不参与
// (不记 0 拉低均值);无可用行时各字段为 null。
func ossAverageSummary(rows []types.OSSMetric) *OSSMetricSummary {
	var sumSize, sumObjects float64
	var n int
	for _, m := range rows {
		if m.StorageSize == 0 {
			continue
		}
		sumSize += m.StorageSize
		sumObjects += float64(m.ObjectCount)
		n++
	}
	if n == 0 {
		return &OSSMetricSummary{}
	}
	return &OSSMetricSummary{
		StorageSize: floatPtr(sumSize / float64(n)),
		ObjectCount: floatPtr(sumObjects / float64(n)),
	}
}

func int64Ptr(v int64) *int64 { return &v }
