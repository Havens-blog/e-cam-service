// @feature disk-ops-insight @api-functional
//
// Shared journey harness for the disk-ops-insight API functional test suites
// ( tests/<journey>/ ). It wires the exported production surfaces —
// SyncDiskMetricsExecutor, PersistentDailyGate ( resource_type=disk key ),
// DiskQueryService and the gin asset handler disk routes — to in-memory fakes
// with the same semantics as the real boundaries: unique key
// (account_id, disk_id, date), first-write-wins for today rows, overwrite for
// past rows, usage_percent ∈ [0,100] / non-negative iops+throughput QC gate
// with usage_percent=0 forced to qc_status=zero_exception.
//
// The disk instance fake ( DiskRepo ) mirrors the ecam_instance enumeration
// contract the collect executor relies on: active-account scope = >=1 disk
// instance under the account ( AssetTypes=["disk"] search ), disk_id taken
// from instance.AssetID, region taken from attributes["region"].
//
// Account / task / gate-store / gate-alerter fakes are reused verbatim from
// tests/nastest ( same boundary semantics — disk only adds the
// resource_type=disk gate key ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package disktest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"
	"github.com/Havens-blog/e-cloudx-sdk"
	"github.com/Havens-blog/e-cloudx-sdk/types"
	camshared "github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-common-go/mongox"
	"github.com/Havens-blog/e-common-go/taskx"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Today 运营时区今日 YYYY-MM-DD(Asia/Shanghai,与生产口径一致)。
func Today() string { return nastest.Today() }

// DayOffset 运营时区今日偏移 offset 天后的 YYYY-MM-DD(offset 为负即过去)。
func DayOffset(offset int) string { return nastest.DayOffset(offset) }

// ==================== usage/iops/throughput QC gate ( real DAO semantics ) ====================

// usagePercentMax usage_percent 归一口径上界(与 dao.diskMetricQC 同值 0~100)。
const usagePercentMax = 100.0

// applyDiskQC 复刻真实 DAO 写入门禁语义:usage_percent 越出 [0,100] → 拒绝
// (错误携带 disk_id/date,文案与 dao/disk_metric.go 逐字同型,失败归因断言依赖);
// iops/throughput 为负 → 拒绝;usage_percent=0 → 强制 zero_exception 例外放行;
// 其余原样放行(透传适配器 qc_status/usage_scope 标注)。
func applyDiskQC(m types.DiskMetric) (types.DiskMetric, error) {
	switch {
	case m.UsagePercent < 0 || m.UsagePercent > usagePercentMax:
		return types.DiskMetric{}, fmt.Errorf(
			"disk metric usage_percent out of range [0,100]: disk_id=%s date=%s usage_percent=%g (自查口径归一:厂商原始口径须在采集边界归一为 0~100)",
			m.DiskID, m.Date, m.UsagePercent)
	case m.IOPS < 0 || m.Throughput < 0:
		return types.DiskMetric{}, fmt.Errorf(
			"disk metric iops/throughput must be non-negative: disk_id=%s date=%s iops=%g throughput=%g",
			m.DiskID, m.Date, m.IOPS, m.Throughput)
	case m.UsagePercent == 0:
		m.QcStatus = types.DiskMetricQcZeroException
		return m, nil
	default:
		return m, nil
	}
}

// normalizeReadDays 复刻 DAO/服务侧读取窗口收敛:缺省 30,上限 90。
func normalizeReadDays(days int) int {
	switch {
	case days <= 0:
		return 30
	case days > 90:
		return 90
	default:
		return days
	}
}

// ==================== metric DAO fake ====================

type diskMetricKey struct {
	accountID int64
	diskID    string
	date      string
}

// MetricDAO ecam_disk_metric 内存替身(唯一键 (account_id, disk_id, date);
// BulkInsertIfAbsent 首写生效 / BulkUpsertMetrics 覆盖更新 / 写入前口径门禁 /
// usage_percent=0 强制 zero_exception,与 dao.DiskMetricDAO 语义逐项对齐)。
type MetricDAO struct {
	dao.DiskMetricDAO // 嵌入接口,仅实现被测路径

	mu   sync.Mutex
	rows map[diskMetricKey]types.DiskMetric
}

// NewMetricDAO 创建空的内存 Disk 指标 DAO。
func NewMetricDAO() *MetricDAO {
	return &MetricDAO{rows: make(map[diskMetricKey]types.DiskMetric)}
}

// UpsertMetric 按 dao.DiskMetricDAO 语义:唯一键幂等覆盖写入(先过门禁)。
func (d *MetricDAO) UpsertMetric(_ context.Context, m types.DiskMetric) error {
	qc, err := applyDiskQC(m)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rows[diskMetricKey{qc.AccountID, qc.DiskID, qc.Date}] = qc
	return nil
}

// BulkUpsertMetrics 覆盖更新批写:任一行越界整批拒绝,不良行不得落库。
func (d *MetricDAO) BulkUpsertMetrics(_ context.Context, metrics []types.DiskMetric) error {
	qcRows := make([]types.DiskMetric, 0, len(metrics))
	for _, m := range metrics {
		qc, err := applyDiskQC(m)
		if err != nil {
			return err
		}
		qcRows = append(qcRows, qc)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, qc := range qcRows {
		d.rows[diskMetricKey{qc.AccountID, qc.DiskID, qc.Date}] = qc
	}
	return nil
}

// BulkInsertIfAbsent 首写生效批写:唯一键命中不修改任何字段,仅补缺失行;
// 任一行越界整批拒绝。
func (d *MetricDAO) BulkInsertIfAbsent(_ context.Context, metrics []types.DiskMetric) error {
	qcRows := make([]types.DiskMetric, 0, len(metrics))
	for _, m := range metrics {
		qc, err := applyDiskQC(m)
		if err != nil {
			return err
		}
		qcRows = append(qcRows, qc)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, qc := range qcRows {
		key := diskMetricKey{qc.AccountID, qc.DiskID, qc.Date}
		if _, exists := d.rows[key]; !exists {
			d.rows[key] = qc
		}
	}
	return nil
}

// CountMetricsByProviders 统计各必达厂商自 sinceDate(含)以来的行数。
func (d *MetricDAO) CountMetricsByProviders(_ context.Context, providers []string, sinceDate string) (map[string]int64, error) {
	out := make(map[string]int64, len(providers))
	for _, p := range providers {
		out[p] = 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, row := range d.rows {
		if row.Date >= sinceDate {
			if _, tracked := out[row.Provider]; tracked {
				out[row.Provider]++
			}
		}
	}
	return out, nil
}

// ListByDisk 指定账号 + disk_id 近 N 天行,按 date 升序(趋势接口读取)。
func (d *MetricDAO) ListByDisk(_ context.Context, accountID int64, diskID string, days int) ([]types.DiskMetric, error) {
	days = normalizeReadDays(days)
	cutoff := DayOffset(-(days - 1))
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]types.DiskMetric, 0)
	for _, row := range d.rows {
		if row.AccountID == accountID && row.DiskID == diskID && row.Date >= cutoff {
			out = append(out, row)
		}
	}
	sortDiskMetrics(out, func(a, b types.DiskMetric) bool { return a.Date < b.Date })
	return out, nil
}

// ListByAccounts 一组账号近 N 天全部行(Top 聚合读取),按 disk_id+date 升序
// (与真实 DAO 排序一致,服务层按 disk 分组稳定)。
func (d *MetricDAO) ListByAccounts(_ context.Context, accountIDs []int64, days int) ([]types.DiskMetric, error) {
	if len(accountIDs) == 0 {
		return []types.DiskMetric{}, nil
	}
	days = normalizeReadDays(days)
	cutoff := DayOffset(-(days - 1))
	wanted := make(map[int64]struct{}, len(accountIDs))
	for _, id := range accountIDs {
		wanted[id] = struct{}{}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]types.DiskMetric, 0)
	for _, row := range d.rows {
		if _, ok := wanted[row.AccountID]; ok && row.Date >= cutoff {
			out = append(out, row)
		}
	}
	sortDiskMetrics(out, func(a, b types.DiskMetric) bool {
		if a.DiskID != b.DiskID {
			return a.DiskID < b.DiskID
		}
		return a.Date < b.Date
	})
	return out, nil
}

// Row 读取单行(不存在返回 false)。
func (d *MetricDAO) Row(accountID int64, diskID, date string) (types.DiskMetric, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	row, ok := d.rows[diskMetricKey{accountID, diskID, date}]
	return row, ok
}

// Count 当前行数。
func (d *MetricDAO) Count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.rows)
}

// DiskIDs 返回当前去重后的 disk_id 清单(断言用)。
func (d *MetricDAO) DiskIDs() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, row := range d.rows {
		if _, ok := seen[row.DiskID]; !ok {
			seen[row.DiskID] = struct{}{}
			out = append(out, row.DiskID)
		}
	}
	sort.Strings(out)
	return out
}

func sortDiskMetrics(rows []types.DiskMetric, less func(a, b types.DiskMetric) bool) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && less(rows[j], rows[j-1]); j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

// ==================== disk instance repo fake ( ecam_instance enumeration ) ====================

// DiskRepo ecam_instance 中 disk 资产的内存替身(采集执行器以本地资产枚举为准:
// AssetTypes=["disk"] + AccountID 分页枚举;健康监控以 Provider + AssetTypes=
// ["disk"] 计数)。disk_id 取 instance.AssetID,region 取 attributes["region"]。
type DiskRepo struct {
	repository.InstanceRepository // 形状借位:仅实现 Search

	mu         sync.Mutex
	byAccount  map[int64][]camdomain.Instance
	accountPri map[int64]camshared.CloudProvider
}

// NewDiskRepo 创建 disk 实例仓储替身。
func NewDiskRepo() *DiskRepo {
	return &DiskRepo{
		byAccount:  make(map[int64][]camdomain.Instance),
		accountPri: make(map[int64]camshared.CloudProvider),
	}
}

// SeedDisk 给账号注册一个 disk 资产(asset_id 即 disk_id,与资产同步侧
// convertDiskToInstance 落库形状一致;attributes 携带 region 与一个资产表
// 快照 size 哨兵值 -1 —— 与任何指标表数值都不同,数据来源唯一性断言用)。
func (r *DiskRepo) SeedDisk(accountID int64, providerName, diskID, region string) camdomain.Instance {
	inst := camdomain.Instance{
		ModelUID:  "test_disk",
		AssetID:   diskID,
		AssetName: diskID,
		TenantID:  1,
		AccountID: accountID,
		Attributes: map[string]any{
			"region": region,
			"size":   -1, // 资产表快照哨兵值:指标界面不得展示快照数值
		},
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byAccount[accountID] = append(r.byAccount[accountID], inst)
	r.accountPri[accountID] = camshared.CloudProvider(providerName)
	return inst
}

// Search 支持两种口径:按账号枚举(采集链路)与按厂商计数(健康监控)。
// AssetTypes 不含 "disk" 时返回空(替身只存放 disk 资产)。
func (r *DiskRepo) Search(_ context.Context, f camdomain.SearchFilter) ([]camdomain.Instance, int64, error) {
	diskWanted := len(f.AssetTypes) == 0
	for _, t := range f.AssetTypes {
		if t == "disk" {
			diskWanted = true
			break
		}
	}
	if !diskWanted {
		return []camdomain.Instance{}, 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []camdomain.Instance
	if f.AccountID > 0 {
		out = append(out, r.byAccount[f.AccountID]...)
	} else {
		for accountID, insts := range r.byAccount {
			if f.Provider != "" && r.accountPri[accountID] != camshared.CloudProvider(f.Provider) {
				continue
			}
			out = append(out, insts...)
		}
	}
	total := int64(len(out))
	start := int(f.Offset)
	if start > len(out) {
		start = len(out)
	}
	end := len(out)
	if f.Limit > 0 && start+int(f.Limit) < end {
		end = start + int(f.Limit)
	}
	return out[start:end], total, nil
}

// ==================== cloudx adapter fakes ====================

// DiskCall 记录一次厂商指标查询的入参(region 透传断言用——云硬盘是地域性资源)。
type DiskCall struct {
	DiskID string
	Region string
	Start  string
	End    string
}

// QuerierDisk 可配厂商 disk 指标查询替身(支持/不支持探测、正常/失败注入)。
// 置 Err 非空模拟「调用失败」形态;MetricCapable=false 时装配 plainDiskAdapter
// (未实现 DiskMetricQuerier,探测不支持语义)。
type QuerierDisk struct {
	cloudx.DiskAdapter // 形状借位:仅 GetDiskMetrics 生效,由适配器桩显式装配

	MetricCapable bool

	mu       sync.Mutex
	err      error
	diskErrs map[string]error
	metrics  []types.DiskMetric
	calls    []DiskCall
}

// SetMetrics 设置该厂商返回的日值集(按 disk + 请求区间过滤回发)。
func (q *QuerierDisk) SetMetrics(metrics ...types.DiskMetric) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.metrics = append([]types.DiskMetric(nil), metrics...)
}

// Fail 注入调用失败(宕机/鉴权失效形态,区别于探测不支持)。
func (q *QuerierDisk) Fail(err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.err = err
}

// Recover 清除故障注入(次日恢复场景)。
func (q *QuerierDisk) Recover() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.err = nil
	q.diskErrs = nil
}

// FailDisk 注入单盘级调用失败(单盘失败不阻塞同账号其余盘的隔离语义用)。
func (q *QuerierDisk) FailDisk(diskID string, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.diskErrs == nil {
		q.diskErrs = make(map[string]error)
	}
	q.diskErrs[diskID] = err
}

// Calls 返回已发生的查询入参快照。
func (q *QuerierDisk) Calls() []DiskCall {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]DiskCall(nil), q.calls...)
}

// GetDiskMetrics 实现厂商 disk 指标查询(过滤 disk + 请求区间;注入失败时返回错误)。
func (q *QuerierDisk) GetDiskMetrics(_ context.Context, diskID, _, region, startDate, endDate string) ([]types.DiskMetric, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.calls = append(q.calls, DiskCall{DiskID: diskID, Region: region, Start: startDate, End: endDate})
	if q.err != nil {
		return nil, q.err
	}
	if err, ok := q.diskErrs[diskID]; ok {
		return nil, err
	}
	out := make([]types.DiskMetric, 0)
	for _, m := range q.metrics {
		if m.DiskID != diskID {
			continue
		}
		if m.Date < startDate || m.Date > endDate {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// CloudAdapterStub 仅 Disk()/GetProvider() 生效的云适配器桩。
type CloudAdapterStub struct {
	cloudx.CloudAdapter
	provider camshared.CloudProvider
	disk     cloudx.DiskAdapter
}

// GetProvider 返回桩厂商。
func (s *CloudAdapterStub) GetProvider() camshared.CloudProvider { return s.provider }

// Disk 返回 disk 适配器(可能未实现指标查询)。
func (s *CloudAdapterStub) Disk() cloudx.DiskAdapter { return s.disk }

// plainDiskAdapter 未实现 DiskMetricQuerier 的 disk 适配器(探测不支持语义)。
type plainDiskAdapter struct{}

func (p *plainDiskAdapter) ListInstances(_ context.Context, _ string) ([]types.DiskInstance, error) {
	return nil, nil
}
func (p *plainDiskAdapter) GetInstance(_ context.Context, _, _ string) (*types.DiskInstance, error) {
	return nil, nil
}
func (p *plainDiskAdapter) ListInstancesByIDs(_ context.Context, _ string, _ []string) ([]types.DiskInstance, error) {
	return nil, nil
}
func (p *plainDiskAdapter) GetInstanceStatus(_ context.Context, _, _ string) (string, error) {
	return "", nil
}
func (p *plainDiskAdapter) ListInstancesWithFilter(_ context.Context, _ string, _ *types.DiskFilter) ([]types.DiskInstance, error) {
	return nil, nil
}
func (p *plainDiskAdapter) ListByInstanceID(_ context.Context, _, _ string) ([]types.DiskInstance, error) {
	return nil, nil
}

var (
	_ cloudx.CloudAdapter      = (*CloudAdapterStub)(nil)
	_ cloudx.DiskAdapter       = (*plainDiskAdapter)(nil)
	_ cloudx.DiskMetricQuerier = (*QuerierDisk)(nil)
)

// ==================== health alerter fake ====================

// DiskHealthAlert 零成功升级告警调用记录。
type DiskHealthAlert struct {
	Provider      string
	WindowDays    int
	InstanceCount int64
}

// DiskHealthAlerterFake disk 自我健康监控告警通道替身。
type DiskHealthAlerterFake struct {
	mu    sync.Mutex
	calls []DiskHealthAlert
}

// AlertDiskZeroSuccess 记录一次零成功升级告警(实现 executor.DiskHealthAlerter)。
func (a *DiskHealthAlerterFake) AlertDiskZeroSuccess(_ context.Context, provider string, windowDays int, instanceCount int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, DiskHealthAlert{Provider: provider, WindowDays: windowDays, InstanceCount: instanceCount})
}

// Calls 返回告警记录快照。
func (a *DiskHealthAlerterFake) Calls() []DiskHealthAlert {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]DiskHealthAlert(nil), a.calls...)
}

// ==================== harness ====================

// Provider 一个已注册的测试厂商(名字全局唯一,避免跨 harness 串号)。
type Provider struct {
	Name    string
	Querier *QuerierDisk
}

// PerAccountProvider 按账号返回独立适配器实例的测试厂商:适配器缓存键为
// provider_accountID,每个账号各持一个 querier(账号级故障注入/各自口径用)。
type PerAccountProvider struct {
	Name string

	mu       sync.Mutex
	queriers map[int64]*QuerierDisk
}

// QuerierFor 返回指定账号的 querier(懒创建,同一账号恒定)。
func (p *PerAccountProvider) QuerierFor(accountID int64) *QuerierDisk {
	p.mu.Lock()
	defer p.mu.Unlock()
	q, ok := p.queriers[accountID]
	if !ok {
		q = &QuerierDisk{MetricCapable: true}
		p.queriers[accountID] = q
	}
	return q
}

// Harness disk-ops-insight journey 世界:账号 + disk 资产 + 指标表 + 任务仓储 +
// 日闸存储 + 日闸告警通道 + 健康告警通道,并暴露生产执行器/查询服务/路由的
// 装配入口。账号/任务/日闸替身直接复用 nastest(同一套边界语义,disk 仅分键
// resource_type=disk)。
type Harness struct {
	TenantID int64

	AccountRepo   *nastest.AccountRepo
	DiskRepo      *DiskRepo
	MetricDAO     *MetricDAO
	TaskRepo      *nastest.TaskRepo
	GateStore     *nastest.GateStore
	GateAlerter   *nastest.GateAlerter
	HealthAlerter *DiskHealthAlerterFake
}

// NewHarness 创建 harness(默认租户 1)。
func NewHarness() *Harness {
	return &Harness{
		TenantID:      1,
		AccountRepo:   nastest.NewAccountRepo(),
		DiskRepo:      NewDiskRepo(),
		MetricDAO:     NewMetricDAO(),
		TaskRepo:      &nastest.TaskRepo{},
		GateStore:     nastest.NewGateStore(),
		GateAlerter:   &nastest.GateAlerter{},
		HealthAlerter: &DiskHealthAlerterFake{},
	}
}

// providerNameSeq 进程级厂商名序号:同一测试二进制内全局唯一,避免多个
// harness 复用同名厂商时撞上 cloudx 包级适配器缓存(provider_accountID 键)。
var providerNameSeq int64

// NewProvider 注册一个测试厂商并返回句柄;metricCapable=false 时为
// 「探测不支持」形态(未实现 DiskMetricQuerier)。
func (h *Harness) NewProvider(metricCapable bool) *Provider {
	n := atomic.AddInt64(&providerNameSeq, 1)
	name := fmt.Sprintf("diskjt-%d-%s", n, map[bool]string{true: "metric", false: "plain"}[metricCapable])
	return h.NewNamedProvider(name, metricCapable)
}

// NewNamedProvider 以指定名字注册测试厂商(需要命中生产厂商清单的字面量时用,
// 如自我健康监控的必达厂商 aliyun/huawei/aws 与尽力而为厂商 tencent)。
// 仅测试二进制内生效;注册前清空 cloudx 包级适配器缓存,确保 Execute 一定
// 拿到最新 creator。
func (h *Harness) NewNamedProvider(name string, metricCapable bool) *Provider {
	// 包级适配器缓存失效(全局生效):防止先前测试留下的同名缓存适配器
	cloudx.NewAdapterFactory(elog.DefaultLogger).ClearCache()
	querier := &QuerierDisk{MetricCapable: metricCapable}
	var disk cloudx.DiskAdapter
	if metricCapable {
		// *QuerierDisk 经内嵌 DiskAdapter 形状借位满足 DiskAdapter,且实现
		// DiskMetricQuerier(可指标查询形态)
		disk = querier
	} else {
		disk = &plainDiskAdapter{}
	}
	cloudx.RegisterAdapter(camshared.CloudProvider(name), func(_ *camshared.CloudAccount) (cloudx.CloudAdapter, error) {
		return &CloudAdapterStub{provider: camshared.CloudProvider(name), disk: disk}, nil
	})
	return &Provider{Name: name, Querier: querier}
}

// NewPerAccountProvider 注册按账号独立的测试厂商。
func (h *Harness) NewPerAccountProvider() *PerAccountProvider {
	n := atomic.AddInt64(&providerNameSeq, 1)
	name := fmt.Sprintf("diskjt-%d-peracct", n)
	cloudx.NewAdapterFactory(elog.DefaultLogger).ClearCache()
	p := &PerAccountProvider{Name: name, queriers: make(map[int64]*QuerierDisk)}
	cloudx.RegisterAdapter(camshared.CloudProvider(name), func(account *camshared.CloudAccount) (cloudx.CloudAdapter, error) {
		return &CloudAdapterStub{provider: camshared.CloudProvider(name), disk: p.QuerierFor(account.ID)}, nil
	})
	return p
}

// SeedAccount 注册一个活跃云账号(默认属 harness 租户)。
func (h *Harness) SeedAccount(id int64, provider *Provider) camshared.CloudAccount {
	return h.SeedAccountTenant(id, provider.Name, h.TenantID)
}

// SeedAccountNamed 以厂商名注册活跃云账号(PerAccountProvider 场景用)。
func (h *Harness) SeedAccountNamed(id int64, providerName string) camshared.CloudAccount {
	return h.SeedAccountTenant(id, providerName, h.TenantID)
}

// SeedAccountTenant 注册指定租户的活跃云账号(跨租户越权用例用)。
func (h *Harness) SeedAccountTenant(id int64, providerName string, tenantID int64) camshared.CloudAccount {
	account := camshared.CloudAccount{
		ID:              id,
		Name:            fmt.Sprintf("acc-%s-%d", providerName, id),
		Provider:        camshared.CloudProvider(providerName),
		TenantID:        tenantID,
		Status:          camshared.CloudAccountStatusActive,
		AccessKeyID:     "test-ak",
		AccessKeySecret: "test-sk",
	}
	h.AccountRepo.SeedAccount(account)
	return account
}

// SeedDisk 给账号注册一个 disk 资产(采集链路以本地资产枚举为准)。
func (h *Harness) SeedDisk(accountID int64, providerName, diskID, region string) camdomain.Instance {
	return h.DiskRepo.SeedDisk(accountID, providerName, diskID, region)
}

// SeedMetric 直写一行指标(经写入门禁,零使用率行自动打 zero_exception)。
func (h *Harness) SeedMetric(t *testing.T, accountID int64, providerName string, m types.DiskMetric) {
	t.Helper()
	m.AccountID = accountID
	m.Provider = providerName
	require.NoError(t, h.MetricDAO.UpsertMetric(context.Background(), m))
}

// NewCollectExecutor 装配生产采集执行器(挂自我健康监控告警桥)。
func (h *Harness) NewCollectExecutor() *executor.SyncDiskMetricsExecutor {
	e := executor.NewSyncDiskMetricsExecutor(
		h.AccountRepo,
		h.DiskRepo,
		h.MetricDAO,
		h.TaskRepo,
		elog.DefaultLogger,
	)
	e.SetDiskHealthAlerter(h.HealthAlerter)
	return e
}

// RunCollect 以指定参数执行一次采集任务,返回任务(Result 已填充)。
func (h *Harness) RunCollect(t *testing.T, params map[string]any) (*taskx.Task, error) {
	t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	task := &taskx.Task{
		ID:        fmt.Sprintf("task-diskjt-%d", time.Now().UnixNano()),
		Type:      executor.TaskTypeDiskCollectMetrics,
		Status:    taskx.TaskStatusPending,
		Params:    params,
		CreatedBy: "diskjt",
	}
	err := h.NewCollectExecutor().Execute(context.Background(), task)
	return task, err
}

// NewQueryService 装配生产 disk 读取服务(读同一内存指标表)。
func (h *Harness) NewQueryService() *service.DiskQueryService {
	return service.NewDiskQueryService(h.AccountRepo, h.MetricDAO, elog.DefaultLogger)
}

// NewDiskRouter 装配 /assets/disk/metrics 与 /assets/disk/top 路由(注入租户)。
func (h *Harness) NewDiskRouter(tenantID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	tenant := func(c *gin.Context) {
		c.Set(middleware.TenantIDKey, tenantID)
		c.Next()
	}
	// RegisterRoutesWithGroup 期望收到的已是 assets 路由组(内部直接注册
	// /disk/metrics 等),故组前缀为 /assets,与生产 /cam/assets/* 对齐
	group := router.Group("/assets", tenant)
	web.NewAssetHandler(nil, nil, nil, nil, nil, h.NewQueryService()).RegisterRoutesWithGroup(group)
	return router
}

// ==================== HTTP / Result helpers ====================

// Envelope 统一响应信封(pkg/ginx.Result 形状)。
type Envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// GetJSON 发起 GET 并解析统一信封。
func GetJSON(t *testing.T, router *gin.Engine, path string) (int, Envelope) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var env Envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env), "响应体应为统一信封 JSON: %s", rec.Body.String())
	return rec.Code, env
}

// Metric 构造一行厂商日指标(归一口径:usage_percent 0~100,iops 次/秒,
// throughput MB/s)。
func Metric(diskID, date string, usagePercent, iops, throughput float64) types.DiskMetric {
	return types.DiskMetric{
		DiskID:       diskID,
		Date:         date,
		UsagePercent: usagePercent,
		UsageScope:   types.DiskUsageScopeInstanceLevel,
		IOPS:         iops,
		Throughput:   throughput,
	}
}

// ==================== real DAO live access ( MONGO_DSN gated ) ====================

// LiveMetricDAO 构造真实 DiskMetricDAO(独立测试库 ecam_dao_disk_test + Drop 清理;
// 与 NAS ecam_dao_test / OSS ecam_dao_oss_test 分库,Drop 互不清场)。
// 未设置 MONGO_DSN 时跳过;DSN 指向的实例 5 秒内不可达同样跳过(与
// internal/cam/repository/dao live 测试同约定)。
func LiveMetricDAO(t *testing.T) dao.DiskMetricDAO {
	t.Helper()
	dsn := os.Getenv("MONGO_DSN")
	if dsn == "" {
		t.Skip("set MONGO_DSN to run live check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx,
		options.Client().ApplyURI(dsn).
			SetConnectTimeout(5*time.Second).
			SetServerSelectionTimeout(5*time.Second))
	if err != nil {
		t.Skipf("mongo connect failed, skip live check: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		t.Skipf("mongo unreachable, skip live check: %v", err)
	}
	testDB := client.Database("ecam_dao_disk_test")
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer dropCancel()
		_ = testDB.Drop(dropCtx)
		_ = client.Disconnect(dropCtx)
	})
	return dao.NewDiskMetricDAO(mongox.NewMongo(client, "ecam_dao_disk_test"))
}
