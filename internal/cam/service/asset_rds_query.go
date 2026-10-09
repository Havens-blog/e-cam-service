// Package service RDS 指标读取服务(纯读 ecam_rds_metric 指标表,不走厂商 API)。
//
// 平移蓝本:internal/cam/service/asset_nas_query.go / asset_disk_query.go
// (租户校验、窗口逐日展开、缺失日 data_status 标注、latest/average 汇总
// 全部沿用同包模式)。与 POST 端契约(提议 locked contract)逐字对齐:
//   - days 默认 30,范围 1~90(服务端 normalizeNASDays 同口径;web 层 400);
//   - days[] 按日期升序,缺失日各指标 null + data_status=missing(不填充假值);
//   - latest = 最新有数日期行;average = 近 N 天均值(零异常行跳过不参与均值,
//     无可用行时各字段 null);窗口内无任何行时 latest/average 为 null;
//   - connections 为绝对值(int64),日期缺失时 null(不用 0 冒充)。
//
// data_status 甄别(RDSMetric 注释定案「结合实例 Status 甄别」的读取侧落点):
//   - 行存在且 qc_status=zero_exception(四指标全 0 异常行)且实例非停用/重启族
//     → zero_exception(口径缺失异常行暴露);
//   - 行存在且实例停用/重启中(attributes["status"] ∈ {stopped, restarting},
//     types.RDSStatusStopped/RDSStatusRestarting 归一值)→ 四 0 是「真停机」事实
//     → ok;
//   - 其余有数行 → ok;该日无行 → missing。
//     停用态以 ecam 资产当前 attributes["status"] 判定并适用于窗口内所有日期
//     (停机期间历史行同理真停机;不逐行回溯历史状态,注释留痕)。资产缺失或
//     查询失败视为非停用态(保守暴露异常行,不因读资产失败掩盖口径问题)。
package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/gotomicro/ego/core/elog"
)

// RDS 指标读取参数边界(与 NAS/OSS/Disk 读取同口径,常量独立命名避免跨资源
// 语义漂移;web 层参数校验同此上下界)
const (
	rdsDefaultDays = 30
	rdsMaxDays     = 90
)

// data_status 读取侧取值(qc_status 闭环 + 停用态甄别 + 缺失日标注,
// 提议 locked contract:data_status ∈ ok | zero_exception | missing)
const (
	// RDSDataStatusOK 行存在且数据正常(含停用/重启族实例的真停机四 0)
	RDSDataStatusOK = "ok"
	// RDSDataStatusZeroException qc_status=zero_exception 且实例非停用态:
	// 口径缺失的 0 值行原样暴露并映射 data_status,前端据此渲染警示
	RDSDataStatusZeroException = "zero_exception"
	// RDSDataStatusMissing 窗口内缺失日:不填充假值,各指标为 null
	RDSDataStatusMissing = "missing"
)

// ErrRDSAccountNotInTenant 客户端传入的 account_id 不属于当前租户(越权)。
// handler 须映射 404 而非 403——不泄露账号存在性(规格 Non-Functional
// Requirements,与 NAS/OSS/Disk 同语义)。
var ErrRDSAccountNotInTenant = errors.New("cloud account not found")

// RDSMetricReader RDS 指标只读接口(service 层消费;dao.RDSMetricDAO 天然满足)。
// 二期指标读取只查本地指标表,不走厂商 API。
type RDSMetricReader interface {
	// ListByRDS 取指定账号+rds_id 近 N 天单日指标,按 date 升序(趋势接口)
	ListByRDS(ctx context.Context, accountID int64, rdsID string, days int) ([]types.RDSMetric, error)
}

// RDSMetricPoint 趋势单日数据点。缺失日各指标为 null(不填充假值,以
// data_status=missing 标注);qc_status 原样透出落库标注,前端结合 data_status
// 甄别 0 值语义(口径缺失异常 vs 真停机)。
type RDSMetricPoint struct {
	Date          string   `json:"date"`           // YYYY-MM-DD(Asia/Shanghai)
	CPUPercent    *float64 `json:"cpu_percent"`    // CPU 使用率(%);缺失日 null
	MemoryPercent *float64 `json:"memory_percent"` // 内存使用率(%);缺失日 null
	DiskPercent   *float64 `json:"disk_percent"`   // 磁盘使用率(%);缺失日 null
	Connections   *int64   `json:"connections"`    // 连接数(绝对值,count);缺失日 null
	DataStatus    string   `json:"data_status"`    // ok | zero_exception | missing
	QcStatus      string   `json:"qc_status"`      // 原样透出落库 qc_status(空=正常)
}

// RDSMetricSummary 最新一天 / 近 N 天均值两类值。
// 无可用行(窗口内全缺失或有行但均值无可用行)时字段为 null;latest 无行时为
// nil 指针(JSON null),average date 恒为空串(仅 latest 携带日期,契约对齐)。
type RDSMetricSummary struct {
	Date          string   `json:"date"`           // 仅「最新一天」携带;均值口径为空串
	CPUPercent    *float64 `json:"cpu_percent"`    // 无可用行为 null
	MemoryPercent *float64 `json:"memory_percent"` // 无可用行为 null
	DiskPercent   *float64 `json:"disk_percent"`   // 无可用行为 null
	Connections   *int64   `json:"connections"`    // 无可用行为 null
}

// RDSMetricsResp GET /assets/rds/metrics 响应(locked contract 字段名逐字对齐:
// rds_id / days / latest / average)
type RDSMetricsResp struct {
	RdsID   string            `json:"rds_id"`
	Days    []RDSMetricPoint  `json:"days"` // 按日期升序
	Latest  *RDSMetricSummary `json:"latest"`
	Average *RDSMetricSummary `json:"average"`
}

// RDSQueryService RDS 指标读取服务(纯读指标表;租户校验平移 OSS/Disk 模式,
// 停用态甄别依赖 InstanceRepository 读 ecam 资产 attributes["status"])。
type RDSQueryService struct {
	accountRepo  repository.CloudAccountRepository
	instanceRepo repository.InstanceRepository
	metrics      RDSMetricReader
	logger       *elog.Component
}

// NewRDSQueryService 创建 RDS 指标读取服务
func NewRDSQueryService(
	accountRepo repository.CloudAccountRepository,
	instanceRepo repository.InstanceRepository,
	metrics RDSMetricReader,
	logger *elog.Component,
) *RDSQueryService {
	return &RDSQueryService{accountRepo: accountRepo, instanceRepo: instanceRepo, metrics: metrics, logger: logger}
}

// rdsCSTZone 运营时区(与采集/DAO 侧一致,按 Asia/Shanghai 自然日切分)
var rdsCSTZone = nasCSTZone

func rdsToday() time.Time {
	return time.Now().In(rdsCSTZone)
}

// rdsStoppedLikeStatus 停用/重启族状态集合(attributes["status"] 归一值,
// types.RDSStatusStopped/RDSStatusRestarting):四指标全 0 + 该族状态 = 「真停机」
// 事实,data_status 映射 ok;反之 zero_exception。
var rdsStoppedLikeStatus = map[string]struct{}{
	types.RDSStatusStopped:    {},
	types.RDSStatusRestarting: {},
}

// rdsDataStatus 行级 data_status 映射(闭环:原样暴露 + 甄别)。
func rdsDataStatus(qcStatus string, stopped bool) string {
	if qcStatus == types.RDSMetricQcZeroException && !stopped {
		return RDSDataStatusZeroException
	}
	return RDSDataStatusOK
}

// rdsStoppedLike 判定实例当前是否停用/重启族。依据 ecam 资产 attributes["status"]
// (sync_rds.go 落库时 NormalizeRDSStatus 归一值);资产缺失/查询失败视为非停用态
// (保守暴露异常行,不因读资产失败掩盖口径问题,DEBUG 留痕)。
func (s *RDSQueryService) rdsStoppedLike(ctx context.Context, tenantID, accountID int64, rdsID string) bool {
	const pageSize = int64(500)
	var offset int64
	for {
		page, total, err := s.instanceRepo.Search(ctx, camdomain.SearchFilter{
			TenantID:   tenantID,
			AccountID:  accountID,
			AssetTypes: []string{"rds"},
			Offset:     offset,
			Limit:      pageSize,
		})
		if err != nil {
			s.logger.Warn("读取RDS资产状态失败,按非停用态处理(RDS指标读取)",
				elog.Int64("account_id", accountID),
				elog.String("rds_id", rdsID),
				elog.FieldErr(err))
			return false
		}
		for i := range page {
			if page[i].AssetID != rdsID {
				continue
			}
			status := page[i].GetStringAttribute("status")
			_, stopped := rdsStoppedLikeStatus[status]
			return stopped
		}
		if int64(len(page)) < pageSize || int64(len(page))+offset >= total {
			return false
		}
		offset += pageSize
	}
}

// GetRdsMetrics 查询租户内指定账号下某 RDS 实例近 N 天单日指标趋势。
// days[] 按日期升序,缺失日以 data_status=missing 标注不填充假值;
// 同时返回「最新一天」与「近 N 天均值」两类值。
func (s *RDSQueryService) GetRdsMetrics(
	ctx context.Context,
	tenantID, accountID int64,
	rdsID string,
	days int,
) (*RDSMetricsResp, error) {
	if rdsID == "" {
		return nil, fmt.Errorf("缺少 rds_id 参数")
	}
	if accountID <= 0 {
		return nil, fmt.Errorf("缺少 account_id 参数")
	}
	days = normalizeNASDays(days)

	// 租户校验:account_id ∉ 租户账号集合 → 越权(handler 映射 404)
	if _, err := s.resolveAccountScope(ctx, tenantID, accountID); err != nil {
		return nil, err
	}

	stopped := s.rdsStoppedLike(ctx, tenantID, accountID, rdsID)

	rows, err := s.metrics.ListByRDS(ctx, accountID, rdsID, days)
	if err != nil {
		return nil, fmt.Errorf("查询 RDS 指标失败: %w", err)
	}

	resp := &RDSMetricsResp{RdsID: rdsID, Days: []RDSMetricPoint{}}
	rowsByDate := make(map[string]types.RDSMetric, len(rows))
	for _, m := range rows {
		rowsByDate[m.Date] = m
	}

	// 窗口逐日展开(升序),缺失日不填充假值
	today := rdsToday()
	for i := days - 1; i >= 0; i-- {
		date := today.AddDate(0, 0, -i).Format("2006-01-02")
		m, ok := rowsByDate[date]
		if !ok {
			resp.Days = append(resp.Days, RDSMetricPoint{Date: date, DataStatus: RDSDataStatusMissing})
			continue
		}
		resp.Days = append(resp.Days, RDSMetricPoint{
			Date:          date,
			CPUPercent:    floatPtr(m.CPUPercent),
			MemoryPercent: floatPtr(m.MemoryPercent),
			DiskPercent:   floatPtr(m.DiskPercent),
			Connections:   rdsIntPtr(m.Connections),
			DataStatus:    rdsDataStatus(m.QcStatus, stopped),
			QcStatus:      m.QcStatus,
		})
	}

	// 窗口内无任何行:latest/average 均为 null(契约:无可用行 null);
	// 有行时 latest 取最后一行,average 汇总(全零异常行时为空对象——契约形态)
	if len(rows) > 0 {
		resp.Latest = rdsLatestSummary(rows)
		resp.Average = rdsAverageSummary(rows)
	}
	return resp, nil
}

// resolveAccountScope 解析查询的账号范围:accountID>0 时校验 ∈ 租户账号集合
// (越权返回 ErrRDSAccountNotInTenant,handler 映射 404;RDS 只读接口必填
// account_id,不复用 0=全租户语义)。
func (s *RDSQueryService) resolveAccountScope(ctx context.Context, tenantID, accountID int64) ([]int64, error) {
	accountIDs, err := s.tenantAccountIDs(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for _, id := range accountIDs {
		if id == accountID {
			return []int64{accountID}, nil
		}
	}
	return nil, ErrRDSAccountNotInTenant
}

// tenantAccountIDs 取租户全部云账号 ID(逐账号隔离模式,平移 OSS/Disk 查询服务)
func (s *RDSQueryService) tenantAccountIDs(ctx context.Context, tenantID int64) ([]int64, error) {
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

// ==================== 纯汇总函数(单测覆盖) ====================

// rdsIntPtr 连接数(int64)→ 指针(缺失日 null 用)
func rdsIntPtr(v int64) *int64 { return &v }

// rdsLatestSummary 「最新一天」汇总:取窗口内最后一行(DAO 按 date 升序返回)。
// 无行返回 nil(JSON null,契约:无可用行 latest 为 null)。
func rdsLatestSummary(rows []types.RDSMetric) *RDSMetricSummary {
	if len(rows) == 0 {
		return nil
	}
	m := rows[len(rows)-1]
	return &RDSMetricSummary{
		Date:          m.Date,
		CPUPercent:    floatPtr(m.CPUPercent),
		MemoryPercent: floatPtr(m.MemoryPercent),
		DiskPercent:   floatPtr(m.DiskPercent),
		Connections:   rdsIntPtr(m.Connections),
	}
}

// rdsAverageSummary 近 N 天均值:qc_status=zero_exception 的零异常行跳过不参与
// (不记 0 拉低均值,与 NAS/Disk 同口径);有行但均值无可用行时返回非 nil 的空
// 汇总(各字段 null,date 空串——契约形态);窗口内无行由调用方置 nil。
func rdsAverageSummary(rows []types.RDSMetric) *RDSMetricSummary {
	var sumCPU, sumMem, sumDisk float64
	var sumConn int64
	var n int
	for _, m := range rows {
		if m.QcStatus == types.RDSMetricQcZeroException {
			continue
		}
		sumCPU += m.CPUPercent
		sumMem += m.MemoryPercent
		sumDisk += m.DiskPercent
		sumConn += m.Connections
		n++
	}
	if n == 0 {
		return &RDSMetricSummary{}
	}
	return &RDSMetricSummary{
		CPUPercent:    floatPtr(sumCPU / float64(n)),
		MemoryPercent: floatPtr(sumMem / float64(n)),
		DiskPercent:   floatPtr(sumDisk / float64(n)),
		Connections:   rdsIntPtr(int64(math.Round(float64(sumConn) / float64(n)))),
	}
}
