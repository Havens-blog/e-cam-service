// @feature oss-ops-insight @api-functional
//
// Shared journey harness for the oss-ops-insight API functional test suites
// ( tests/<journey>/ ). It wires the exported production surfaces —
// SyncOSSMetricsExecutor, PersistentDailyGate ( resource_type=oss key ),
// OSSQueryService and the gin asset handler OSS routes — to in-memory fakes
// with the same semantics as the real boundaries: unique key
// (account_id, bucket_name, date), first-write-wins for today rows,
// overwrite for past rows, [1MB, 1PB] storage_size QC gate with
// zero_exception passthrough.
//
// The OSS buckets fake ( BucketRepo ) mirrors the ecam_instance enumeration
// contract the collect executor relies on: active-account scope = >=1 OSS
// bucket under the account ( AssetTypes=["oss"] search ), bucket name taken
// from instance.AssetID.
//
// Account / task / gate-store / gate-alerter fakes are reused verbatim from
// tests/nastest ( same boundary semantics — OSS only adds the
// resource_type=oss gate key ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package osstest

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

// ==================== storage_size QC gate ( real DAO semantics ) ====================

// 落库数量级自检区间(与 dao.ossMetricQC 同口径):非零 storage_size 落在
// [1MB, 1PB](以 GB 二进制 GiB 计);storage_size=0 例外放行打 zero_exception。
const (
	ossStorageMinGB = float64(1) / float64(1024) // 1MiB = 1/1024 GiB
	ossStorageMaxGB = float64(1024 * 1024)       // 1PiB = 1024^2 GiB
)

// applyStorageQC 复刻真实 DAO 写入门禁语义:storage_size=0 → 强制
// zero_exception 放行;非零越出 [1MB, 1PB] → 拒绝(错误携带 bucket_name/date);
// 其余原样放行。错误文案与 dao/oss_metric.go 逐字同型(失败归因断言依赖)。
func applyStorageQC(m types.OSSMetric) (types.OSSMetric, error) {
	switch {
	case m.StorageSize == 0:
		m.QcStatus = types.OSSMetricQcZeroException
		return m, nil
	case m.StorageSize < ossStorageMinGB || m.StorageSize > ossStorageMaxGB:
		return types.OSSMetric{}, fmt.Errorf(
			"oss metric storage_size out of range [1MB,1PB]: bucket_name=%s date=%s storage_size=%g (自查单位换算:适配器须在采集边界完成字节→GB)",
			m.BucketName, m.Date, m.StorageSize)
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

type ossMetricKey struct {
	accountID  int64
	bucketName string
	date       string
}

// MetricDAO ecam_oss_metric 内存替身(唯一键 (account_id, bucket_name, date);
// BulkInsertIfAbsent 首写生效 / BulkUpsertMetrics 覆盖更新 / 写入前数量级自检 /
// storage_size=0 强制 zero_exception,与 dao.OSSMetricDAO 语义逐项对齐)。
type MetricDAO struct {
	dao.OSSMetricDAO // 嵌入接口,仅实现被测路径

	mu   sync.Mutex
	rows map[ossMetricKey]types.OSSMetric
}

// NewMetricDAO 创建空的内存 OSS 指标 DAO。
func NewMetricDAO() *MetricDAO {
	return &MetricDAO{rows: make(map[ossMetricKey]types.OSSMetric)}
}

// UpsertMetric 按 dao.OSSMetricDAO 语义:唯一键幂等覆盖写入(先过门禁)。
func (d *MetricDAO) UpsertMetric(_ context.Context, m types.OSSMetric) error {
	qc, err := applyStorageQC(m)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rows[ossMetricKey{qc.AccountID, qc.BucketName, qc.Date}] = qc
	return nil
}

// BulkUpsertMetrics 覆盖更新批写:任一行越界整批拒绝,不良行不得落库。
func (d *MetricDAO) BulkUpsertMetrics(_ context.Context, metrics []types.OSSMetric) error {
	qcRows := make([]types.OSSMetric, 0, len(metrics))
	for _, m := range metrics {
		qc, err := applyStorageQC(m)
		if err != nil {
			return err
		}
		qcRows = append(qcRows, qc)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, qc := range qcRows {
		d.rows[ossMetricKey{qc.AccountID, qc.BucketName, qc.Date}] = qc
	}
	return nil
}

// BulkInsertIfAbsent 首写生效批写:唯一键命中不修改任何字段,仅补缺失行;
// 任一行越界整批拒绝。
func (d *MetricDAO) BulkInsertIfAbsent(_ context.Context, metrics []types.OSSMetric) error {
	qcRows := make([]types.OSSMetric, 0, len(metrics))
	for _, m := range metrics {
		qc, err := applyStorageQC(m)
		if err != nil {
			return err
		}
		qcRows = append(qcRows, qc)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, qc := range qcRows {
		key := ossMetricKey{qc.AccountID, qc.BucketName, qc.Date}
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

// ListByBucket 指定账号 + bucket 近 N 天行,按 date 升序(趋势接口读取)。
func (d *MetricDAO) ListByBucket(_ context.Context, accountID int64, bucketName string, days int) ([]types.OSSMetric, error) {
	days = normalizeReadDays(days)
	cutoff := DayOffset(-(days - 1))
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]types.OSSMetric, 0)
	for _, row := range d.rows {
		if row.AccountID == accountID && row.BucketName == bucketName && row.Date >= cutoff {
			out = append(out, row)
		}
	}
	sortOSSMetrics(out, func(a, b types.OSSMetric) bool { return a.Date < b.Date })
	return out, nil
}

// ListByAccounts 一组账号近 N 天全部行(Top 聚合读取),按 bucket_name+date 升序
// (与真实 DAO 排序一致,服务层按 bucket 分组稳定)。
func (d *MetricDAO) ListByAccounts(_ context.Context, accountIDs []int64, days int) ([]types.OSSMetric, error) {
	if len(accountIDs) == 0 {
		return []types.OSSMetric{}, nil
	}
	days = normalizeReadDays(days)
	cutoff := DayOffset(-(days - 1))
	wanted := make(map[int64]struct{}, len(accountIDs))
	for _, id := range accountIDs {
		wanted[id] = struct{}{}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]types.OSSMetric, 0)
	for _, row := range d.rows {
		if _, ok := wanted[row.AccountID]; ok && row.Date >= cutoff {
			out = append(out, row)
		}
	}
	sortOSSMetrics(out, func(a, b types.OSSMetric) bool {
		if a.BucketName != b.BucketName {
			return a.BucketName < b.BucketName
		}
		return a.Date < b.Date
	})
	return out, nil
}

// Row 读取单行(不存在返回 false)。
func (d *MetricDAO) Row(accountID int64, bucketName, date string) (types.OSSMetric, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	row, ok := d.rows[ossMetricKey{accountID, bucketName, date}]
	return row, ok
}

// Count 当前行数。
func (d *MetricDAO) Count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.rows)
}

// BucketNames 返回当前去重后的 bucket_name 清单(断言用)。
func (d *MetricDAO) BucketNames() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, row := range d.rows {
		if _, ok := seen[row.BucketName]; !ok {
			seen[row.BucketName] = struct{}{}
			out = append(out, row.BucketName)
		}
	}
	sort.Strings(out)
	return out
}

func sortOSSMetrics(rows []types.OSSMetric, less func(a, b types.OSSMetric) bool) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && less(rows[j], rows[j-1]); j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

// ==================== bucket repo fake ( ecam_instance OSS enumeration ) ====================

// BucketRepo ecam_instance 中 OSS bucket 资产的内存替身(采集执行器以本地
// 资产枚举为准:AssetTypes=["oss"] + AccountID 分页枚举;健康监控以
// Provider + AssetTypes=["oss"] 计数)。bucket 名取 instance.AssetID。
type BucketRepo struct {
	repository.InstanceRepository // 形状借位:仅实现 Search

	mu         sync.Mutex
	byAccount  map[int64][]camdomain.Instance
	accountPri map[int64]camshared.CloudProvider
}

// NewBucketRepo 创建 OSS bucket 仓储替身。
func NewBucketRepo() *BucketRepo {
	return &BucketRepo{
		byAccount:  make(map[int64][]camdomain.Instance),
		accountPri: make(map[int64]camshared.CloudProvider),
	}
}

// SeedBucket 给账号注册一个 OSS bucket 资产(asset_id 即 bucket_name,
// 与资产同步侧 convertOSSToInstance 落库形状一致)。Attributes 携带一个
// 资产表快照 storage_size(与指标表互为独立数据源,快照不一致用例用)。
func (r *BucketRepo) SeedBucket(accountID int64, providerName, bucketName string) camdomain.Instance {
	inst := camdomain.Instance{
		ModelUID:  "test_oss",
		AssetID:   bucketName,
		AssetName: bucketName,
		TenantID:  1,
		AccountID: accountID,
		Attributes: map[string]any{
			"storage_size": -1, // 资产表快照值:与任何指标表数值都不同
		},
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byAccount[accountID] = append(r.byAccount[accountID], inst)
	r.accountPri[accountID] = camshared.CloudProvider(providerName)
	return inst
}

// Search 支持两种口径:按账号枚举(采集链路)与按厂商计数(健康监控)。
// AssetTypes 不含 "oss" 时返回空(替身只存放 OSS 资产)。
func (r *BucketRepo) Search(_ context.Context, f camdomain.SearchFilter) ([]camdomain.Instance, int64, error) {
	ossWanted := len(f.AssetTypes) == 0
	for _, t := range f.AssetTypes {
		if t == "oss" {
			ossWanted = true
			break
		}
	}
	if !ossWanted {
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

// OSSCall 记录一次厂商指标查询的入参(区间透传断言用;OSS 查询签名无 region)。
type OSSCall struct {
	BucketName string
	Start      string
	End        string
}

// QuerierOSS 可配厂商 OSS 指标查询替身(支持/不支持探测、正常/失败注入)。
// 置 Err 非空模拟「调用失败」形态;MetricCapable=false 时装配 plainOSSAdapter
// (未实现 OSSMetricQuerier,探测不支持语义)。
type QuerierOSS struct {
	cloudx.OSSAdapter // 形状借位:仅 GetOSSMetrics 生效,由适配器桩显式装配

	MetricCapable bool

	mu         sync.Mutex
	err        error
	bucketErrs map[string]error
	metrics    []types.OSSMetric
	calls      []OSSCall
}

// SetMetrics 设置该厂商返回的日值集(按 bucket + 请求区间过滤回发)。
func (q *QuerierOSS) SetMetrics(metrics ...types.OSSMetric) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.metrics = append([]types.OSSMetric(nil), metrics...)
}

// Fail 注入调用失败(宕机/鉴权失效形态,区别于探测不支持)。
func (q *QuerierOSS) Fail(err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.err = err
}

// Recover 清除故障注入(次日恢复场景)。
func (q *QuerierOSS) Recover() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.err = nil
	q.bucketErrs = nil
}

// FailBucket 注入单 bucket 级调用失败(单 bucket 失败不阻塞同账号其余
// bucket 的隔离语义用)。
func (q *QuerierOSS) FailBucket(bucketName string, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.bucketErrs == nil {
		q.bucketErrs = make(map[string]error)
	}
	q.bucketErrs[bucketName] = err
}

// Calls 返回已发生的查询入参快照。
func (q *QuerierOSS) Calls() []OSSCall {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]OSSCall(nil), q.calls...)
}

// GetOSSMetrics 实现厂商 OSS 指标查询(过滤 bucket + 请求区间;注入失败时返回错误)。
func (q *QuerierOSS) GetOSSMetrics(_ context.Context, bucketName, startDate, endDate string) ([]types.OSSMetric, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.calls = append(q.calls, OSSCall{BucketName: bucketName, Start: startDate, End: endDate})
	if q.err != nil {
		return nil, q.err
	}
	if err, ok := q.bucketErrs[bucketName]; ok {
		return nil, err
	}
	out := make([]types.OSSMetric, 0)
	for _, m := range q.metrics {
		if m.BucketName != bucketName {
			continue
		}
		if m.Date < startDate || m.Date > endDate {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// CloudAdapterStub 仅 OSS()/GetProvider() 生效的云适配器桩。
type CloudAdapterStub struct {
	cloudx.CloudAdapter
	provider camshared.CloudProvider
	oss      cloudx.OSSAdapter
}

// GetProvider 返回桩厂商。
func (s *CloudAdapterStub) GetProvider() camshared.CloudProvider { return s.provider }

// OSS 返回 OSS 适配器(可能未实现指标查询)。
func (s *CloudAdapterStub) OSS() cloudx.OSSAdapter { return s.oss }

// plainOSSAdapter 未实现 OSSMetricQuerier 的 OSS 适配器(探测不支持语义)。
type plainOSSAdapter struct{}

func (p *plainOSSAdapter) ListBuckets(_ context.Context, _ string) ([]types.OSSBucket, error) {
	return nil, nil
}
func (p *plainOSSAdapter) GetBucket(_ context.Context, _ string) (*types.OSSBucket, error) {
	return nil, nil
}
func (p *plainOSSAdapter) GetBucketStats(_ context.Context, _ string) (*types.OSSBucketStats, error) {
	return nil, nil
}
func (p *plainOSSAdapter) ListBucketsWithFilter(_ context.Context, _ string, _ *types.OSSBucketFilter) ([]types.OSSBucket, error) {
	return nil, nil
}

var (
	_ cloudx.CloudAdapter     = (*CloudAdapterStub)(nil)
	_ cloudx.OSSAdapter       = (*plainOSSAdapter)(nil)
	_ cloudx.OSSMetricQuerier = (*QuerierOSS)(nil)
)

// ==================== health alerter fake ====================

// OSSHealthAlert 零成功升级告警调用记录。
type OSSHealthAlert struct {
	Provider    string
	WindowDays  int
	BucketCount int64
}

// OSSHealthAlerterFake OSS 自我健康监控告警通道替身。
type OSSHealthAlerterFake struct {
	mu    sync.Mutex
	calls []OSSHealthAlert
}

// AlertOSSZeroSuccess 记录一次零成功升级告警(实现 executor.OSSHealthAlerter)。
func (a *OSSHealthAlerterFake) AlertOSSZeroSuccess(_ context.Context, provider string, windowDays int, bucketCount int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, OSSHealthAlert{Provider: provider, WindowDays: windowDays, BucketCount: bucketCount})
}

// Calls 返回告警记录快照。
func (a *OSSHealthAlerterFake) Calls() []OSSHealthAlert {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]OSSHealthAlert(nil), a.calls...)
}

// ==================== harness ====================

// Provider 一个已注册的测试厂商(名字全局唯一,避免跨 harness 串号)。
type Provider struct {
	Name    string
	Querier *QuerierOSS
}

// PerAccountProvider 按账号返回独立适配器实例的测试厂商:适配器缓存键为
// provider_accountID,每个账号各持一个 querier(账号级故障注入/各自口径用)。
type PerAccountProvider struct {
	Name string

	mu       sync.Mutex
	queriers map[int64]*QuerierOSS
}

// QuerierFor 返回指定账号的 querier(懒创建,同一账号恒定)。
func (p *PerAccountProvider) QuerierFor(accountID int64) *QuerierOSS {
	p.mu.Lock()
	defer p.mu.Unlock()
	q, ok := p.queriers[accountID]
	if !ok {
		q = &QuerierOSS{MetricCapable: true}
		p.queriers[accountID] = q
	}
	return q
}

// Harness oss-ops-insight journey 世界:账号 + OSS bucket 资产 + 指标表 +
// 任务仓储 + 日闸存储 + 日闸告警通道 + 健康告警通道,并暴露生产执行器/
// 查询服务/路由的装配入口。账号/任务/日闸替身直接复用 nastest(同一套
// 边界语义,OSS 仅分键 resource_type=oss)。
type Harness struct {
	TenantID int64

	AccountRepo   *nastest.AccountRepo
	BucketRepo    *BucketRepo
	MetricDAO     *MetricDAO
	TaskRepo      *nastest.TaskRepo
	GateStore     *nastest.GateStore
	GateAlerter   *nastest.GateAlerter
	HealthAlerter *OSSHealthAlerterFake
}

// NewHarness 创建 harness(默认租户 1)。
func NewHarness() *Harness {
	return &Harness{
		TenantID:      1,
		AccountRepo:   nastest.NewAccountRepo(),
		BucketRepo:    NewBucketRepo(),
		MetricDAO:     NewMetricDAO(),
		TaskRepo:      &nastest.TaskRepo{},
		GateStore:     nastest.NewGateStore(),
		GateAlerter:   &nastest.GateAlerter{},
		HealthAlerter: &OSSHealthAlerterFake{},
	}
}

// providerNameSeq 进程级厂商名序号:同一测试二进制内全局唯一,避免多个
// harness 复用同名厂商时撞上 cloudx 包级适配器缓存(provider_accountID 键)。
var providerNameSeq int64

// NewProvider 注册一个测试厂商并返回句柄;metricCapable=false 时为
// 「探测不支持」形态(未实现 OSSMetricQuerier)。
func (h *Harness) NewProvider(metricCapable bool) *Provider {
	n := atomic.AddInt64(&providerNameSeq, 1)
	name := fmt.Sprintf("ossjt-%d-%s", n, map[bool]string{true: "metric", false: "plain"}[metricCapable])
	return h.NewNamedProvider(name, metricCapable)
}

// NewNamedProvider 以指定名字注册测试厂商(需要命中生产厂商清单的字面量时用,
// 如自我健康监控的必达厂商 aliyun/huawei/aws 与尽力而为厂商 tencent)。
// 仅测试二进制内生效;注册前清空 cloudx 包级适配器缓存,确保 Execute 一定
// 拿到最新 creator。
func (h *Harness) NewNamedProvider(name string, metricCapable bool) *Provider {
	// 包级适配器缓存失效(全局生效):防止先前测试留下的同名缓存适配器
	cloudx.NewAdapterFactory(elog.DefaultLogger).ClearCache()
	querier := &QuerierOSS{MetricCapable: metricCapable}
	var oss cloudx.OSSAdapter
	if metricCapable {
		// *QuerierOSS 经内嵌 OSSAdapter 形状借位满足 OSSAdapter,且实现
		// OSSMetricQuerier(可指标查询形态)
		oss = querier
	} else {
		oss = &plainOSSAdapter{}
	}
	cloudx.RegisterAdapter(camshared.CloudProvider(name), func(_ *camshared.CloudAccount) (cloudx.CloudAdapter, error) {
		return &CloudAdapterStub{provider: camshared.CloudProvider(name), oss: oss}, nil
	})
	return &Provider{Name: name, Querier: querier}
}

// NewPerAccountProvider 注册按账号独立的测试厂商。
func (h *Harness) NewPerAccountProvider() *PerAccountProvider {
	n := atomic.AddInt64(&providerNameSeq, 1)
	name := fmt.Sprintf("ossjt-%d-peracct", n)
	cloudx.NewAdapterFactory(elog.DefaultLogger).ClearCache()
	p := &PerAccountProvider{Name: name, queriers: make(map[int64]*QuerierOSS)}
	cloudx.RegisterAdapter(camshared.CloudProvider(name), func(account *camshared.CloudAccount) (cloudx.CloudAdapter, error) {
		return &CloudAdapterStub{provider: camshared.CloudProvider(name), oss: p.QuerierFor(account.ID)}, nil
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

// SeedBucket 给账号注册一个 OSS bucket 资产(采集链路以本地资产枚举为准)。
func (h *Harness) SeedBucket(accountID int64, providerName, bucketName string) camdomain.Instance {
	return h.BucketRepo.SeedBucket(accountID, providerName, bucketName)
}

// SeedMetric 直写一行指标(经写入门禁,零容量行自动打 zero_exception)。
func (h *Harness) SeedMetric(t *testing.T, accountID int64, providerName string, m types.OSSMetric) {
	t.Helper()
	m.AccountID = accountID
	m.Provider = providerName
	require.NoError(t, h.MetricDAO.UpsertMetric(context.Background(), m))
}

// NewCollectExecutor 装配生产采集执行器(挂自我健康监控告警桥)。
func (h *Harness) NewCollectExecutor() *executor.SyncOSSMetricsExecutor {
	e := executor.NewSyncOSSMetricsExecutor(
		h.AccountRepo,
		h.BucketRepo,
		h.MetricDAO,
		h.TaskRepo,
		elog.DefaultLogger,
	)
	e.SetOSSHealthAlerter(h.HealthAlerter)
	return e
}

// RunCollect 以指定参数执行一次采集任务,返回任务(Result 已填充)。
func (h *Harness) RunCollect(t *testing.T, params map[string]any) (*taskx.Task, error) {
	t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	task := &taskx.Task{
		ID:        fmt.Sprintf("task-ossjt-%d", time.Now().UnixNano()),
		Type:      executor.TaskTypeOSSCollectMetrics,
		Status:    taskx.TaskStatusPending,
		Params:    params,
		CreatedBy: "ossjt",
	}
	err := h.NewCollectExecutor().Execute(context.Background(), task)
	return task, err
}

// NewQueryService 装配生产 OSS 读取服务(读同一内存指标表)。
func (h *Harness) NewQueryService() *service.OSSQueryService {
	return service.NewOSSQueryService(h.AccountRepo, h.MetricDAO, elog.DefaultLogger)
}

// NewOSSRouter 装配 /assets/oss/metrics 与 /assets/oss/top 路由(注入租户)。
func (h *Harness) NewOSSRouter(tenantID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	tenant := func(c *gin.Context) {
		c.Set(middleware.TenantIDKey, tenantID)
		c.Next()
	}
	// RegisterRoutesWithGroup 期望收到的已是 assets 路由组(内部直接注册
	// /oss/metrics 等),故组前缀为 /assets,与生产 /cam/assets/* 对齐
	group := router.Group("/assets", tenant)
	web.NewAssetHandler(nil, nil, nil, nil, h.NewQueryService(), nil).RegisterRoutesWithGroup(group)
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

// Metric 构造一行厂商日指标(GB 口径)。
func Metric(bucketName, date string, storageGB float64, objectCount int64) types.OSSMetric {
	return types.OSSMetric{BucketName: bucketName, Date: date, StorageSize: storageGB, ObjectCount: objectCount}
}

// ==================== real DAO live access ( MONGO_DSN gated ) ====================

// LiveMetricDAO 构造真实 OSSMetricDAO(独立测试库 ecam_dao_test + Drop 清理)。
// 未设置 MONGO_DSN 时跳过;DSN 指向的实例 5 秒内不可达同样跳过(与
// internal/cam/repository/dao live 测试同约定)。
func LiveMetricDAO(t *testing.T) dao.OSSMetricDAO {
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
	testDB := client.Database("ecam_dao_test")
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer dropCancel()
		_ = testDB.Drop(dropCtx)
		_ = client.Disconnect(dropCtx)
	})
	return dao.NewOSSMetricDAO(mongox.NewMongo(client, "ecam_dao_test"))
}
