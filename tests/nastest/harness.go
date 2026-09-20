// @feature nas-ops-insight @api-functional
//
// Shared journey harness for the nas-ops-insight API functional test
// suites ( tests/<journey>/ ). It wires the exported production surfaces —
// SyncNASMetricsExecutor, PersistentDailyGate, NASQueryService and the gin
// asset handler routes — to in-memory fakes with the same semantics as the
// real boundaries ( unique key (account_id, fs_id, date), first-write-wins
// for today rows, overwrite for past rows, [1MB, 1PB] capacity QC gate ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package nastest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	camshared "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"os"
)

// ==================== time vocabulary ====================

// CSTZone NAS 指标运营时区(Asia/Shanghai 自然日,与生产口径一致)。
var CSTZone = time.FixedZone("CST", 8*3600)

// Today 运营时区今日 YYYY-MM-DD。
func Today() string { return time.Now().In(CSTZone).Format("2006-01-02") }

// DayOffset 运营时区今日偏移 offset 天后的 YYYY-MM-DD(offset 为负即过去)。
func DayOffset(offset int) string {
	return time.Now().In(CSTZone).AddDate(0, 0, offset).Format("2006-01-02")
}

// ==================== capacity QC gate ( real DAO semantics ) ====================

// 落库数量级自检区间(与 dao.nasMetricQC 同口径):非零 capacity 落在 [1MB, 1PB]
// (以 GB 二进制 GiB 计);capacity=0 例外放行打 zero_exception。
const (
	nasCapacityMinGB = float64(1) / float64(1024) // 1MiB = 1/1024 GiB
	nasCapacityMaxGB = float64(1024 * 1024)       // 1PiB = 1024^2 GiB
)

// applyCapacityQC 复刻真实 DAO 写入门禁语义:capacity=0 → 强制 zero_exception
// 放行;非零越出 [1MB, 1PB] → 拒绝(错误携带 fs_id/date);其余原样放行。
func applyCapacityQC(m types.NASMetric) (types.NASMetric, error) {
	switch {
	case m.Capacity == 0:
		m.QcStatus = types.NASMetricQcZeroException
		return m, nil
	case m.Capacity < nasCapacityMinGB || m.Capacity > nasCapacityMaxGB:
		return types.NASMetric{}, fmt.Errorf(
			"nas metric capacity out of range [1MB,1PB]: fs_id=%s date=%s capacity=%g", m.FsID, m.Date, m.Capacity)
	default:
		return m, nil
	}
}

// ==================== metric DAO fake ====================

type nasMetricKey struct {
	accountID int64
	fsID      string
	date      string
}

// MetricDAO ecam_nas_metric 内存替身(唯一键 (account_id, fs_id, date);
// BulkInsertIfAbsent 首写生效 / BulkUpsertMetrics 覆盖更新 / 写入前数量级自检)。
type MetricDAO struct {
	dao.NASMetricDAO // 嵌入接口,仅实现被测路径

	mu   sync.Mutex
	rows map[nasMetricKey]types.NASMetric
}

// NewMetricDAO 创建空的内存指标 DAO。
func NewMetricDAO() *MetricDAO {
	return &MetricDAO{rows: make(map[nasMetricKey]types.NASMetric)}
}

// UpsertMetric 按 dao.NASMetricDAO 语义:唯一键幂等覆盖写入(先过门禁)。
func (d *MetricDAO) UpsertMetric(_ context.Context, m types.NASMetric) error {
	qc, err := applyCapacityQC(m)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rows[nasMetricKey{qc.AccountID, qc.FsID, qc.Date}] = qc
	return nil
}

// BulkUpsertMetrics 覆盖更新批写:任一行越界整批拒绝,不良行不得落库。
func (d *MetricDAO) BulkUpsertMetrics(_ context.Context, metrics []types.NASMetric) error {
	qcRows := make([]types.NASMetric, 0, len(metrics))
	for _, m := range metrics {
		qc, err := applyCapacityQC(m)
		if err != nil {
			return err
		}
		qcRows = append(qcRows, qc)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, qc := range qcRows {
		d.rows[nasMetricKey{qc.AccountID, qc.FsID, qc.Date}] = qc
	}
	return nil
}

// BulkInsertIfAbsent 首写生效批写:唯一键命中不修改任何字段,仅补缺失行;
// 任一行越界整批拒绝。
func (d *MetricDAO) BulkInsertIfAbsent(_ context.Context, metrics []types.NASMetric) error {
	qcRows := make([]types.NASMetric, 0, len(metrics))
	for _, m := range metrics {
		qc, err := applyCapacityQC(m)
		if err != nil {
			return err
		}
		qcRows = append(qcRows, qc)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, qc := range qcRows {
		key := nasMetricKey{qc.AccountID, qc.FsID, qc.Date}
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

// ListByFs 指定账号 + fs 近 N 天行,按 date 升序。
func (d *MetricDAO) ListByFs(_ context.Context, accountID int64, fsID string, days int) ([]types.NASMetric, error) {
	cutoff := DayOffset(-(days - 1))
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]types.NASMetric, 0)
	for _, row := range d.rows {
		if row.AccountID == accountID && row.FsID == fsID && row.Date >= cutoff {
			out = append(out, row)
		}
	}
	sortNASMetricsByDate(out)
	return out, nil
}

// ListByAccounts 一组账号近 N 天全部行(趋势/Top 聚合读取)。
func (d *MetricDAO) ListByAccounts(_ context.Context, accountIDs []int64, days int) ([]types.NASMetric, error) {
	wanted := make(map[int64]struct{}, len(accountIDs))
	for _, id := range accountIDs {
		wanted[id] = struct{}{}
	}
	cutoff := DayOffset(-(days - 1))
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]types.NASMetric, 0)
	for _, row := range d.rows {
		if _, ok := wanted[row.AccountID]; ok && row.Date >= cutoff {
			out = append(out, row)
		}
	}
	sortNASMetricsByDate(out)
	return out, nil
}

// ListExistingMetricDates 回填幂等预检:返回指定账号 fs 集合在窗口内已落库日期。
func (d *MetricDAO) ListExistingMetricDates(_ context.Context, accountID int64, fsIDs []string, startDate, endDate string) (map[string]map[string]struct{}, error) {
	wanted := make(map[string]struct{}, len(fsIDs))
	for _, id := range fsIDs {
		wanted[id] = struct{}{}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[string]map[string]struct{})
	for _, row := range d.rows {
		if row.AccountID != accountID {
			continue
		}
		if _, ok := wanted[row.FsID]; !ok {
			continue
		}
		if row.Date < startDate || row.Date > endDate {
			continue
		}
		if out[row.FsID] == nil {
			out[row.FsID] = make(map[string]struct{})
		}
		out[row.FsID][row.Date] = struct{}{}
	}
	return out, nil
}

// Rows 返回当前全部行的副本快照(断言用)。
func (d *MetricDAO) Rows() []types.NASMetric {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]types.NASMetric, 0, len(d.rows))
	for _, row := range d.rows {
		out = append(out, row)
	}
	sortNASMetricsByDate(out)
	return out
}

// Row 读取单行(不存在返回 false)。
func (d *MetricDAO) Row(accountID int64, fsID, date string) (types.NASMetric, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	row, ok := d.rows[nasMetricKey{accountID, fsID, date}]
	return row, ok
}

// Count 当前行数。
func (d *MetricDAO) Count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.rows)
}

func sortNASMetricsByDate(rows []types.NASMetric) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].Date < rows[j-1].Date; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

// ==================== account / instance repo fakes ====================

// AccountRepo 云账号仓储内存替身(仅 List/GetByID 生效)。
type AccountRepo struct {
	repository.CloudAccountRepository // 形状借位:仅实现 List/GetByID

	mu       sync.Mutex
	accounts []camshared.CloudAccount
	listErr  error
}

// NewAccountRepo 创建账号仓储替身。
func NewAccountRepo() *AccountRepo { return &AccountRepo{} }

// SeedAccount 注册一个云账号(默认活跃状态)。
func (r *AccountRepo) SeedAccount(account camshared.CloudAccount) camshared.CloudAccount {
	if account.Status == "" {
		account.Status = camshared.CloudAccountStatusActive
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accounts = append(r.accounts, account)
	return account
}

// FailList 注入 List 错误(故障注入用)。
func (r *AccountRepo) FailList(err error) { r.listErr = err }

// List 按 filter 收口(TenantID/Status/Provider)。
func (r *AccountRepo) List(_ context.Context, filter camshared.CloudAccountFilter) ([]camshared.CloudAccount, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	out := make([]camshared.CloudAccount, 0)
	for _, a := range r.accounts {
		if filter.TenantID > 0 && a.TenantID != filter.TenantID {
			continue
		}
		if filter.Status != "" && a.Status != filter.Status {
			continue
		}
		if filter.Provider != "" && a.Provider != filter.Provider {
			continue
		}
		out = append(out, a)
	}
	return out, int64(len(out)), nil
}

// GetByID 按 ID 取账号。
func (r *AccountRepo) GetByID(_ context.Context, id int64) (camshared.CloudAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.accounts {
		if a.ID == id {
			return a, nil
		}
	}
	return camshared.CloudAccount{}, errors.New("cloud account not found")
}

// InstanceRepo ecam_instance 内存替身(采集执行器以本地实例枚举为准)。
type InstanceRepo struct {
	repository.InstanceRepository // 形状借位:仅实现 Search

	mu         sync.Mutex
	byAccount  map[int64][]camdomain.Instance
	accountPri map[int64]camshared.CloudProvider
}

// NewInstanceRepo 创建实例仓储替身。
func NewInstanceRepo() *InstanceRepo {
	return &InstanceRepo{
		byAccount:  make(map[int64][]camdomain.Instance),
		accountPri: make(map[int64]camshared.CloudProvider),
	}
}

// SeedInstance 注册一个 NAS 实例(region 在 attributes,与生产枚举形状一致)。
func (r *InstanceRepo) SeedInstance(accountID int64, provider camshared.CloudProvider, fsID, name, region string) camdomain.Instance {
	inst := camdomain.Instance{
		ModelUID:   "test_nas",
		AssetID:    fsID,
		AssetName:  name,
		TenantID:   1,
		AccountID:  accountID,
		Attributes: map[string]any{"region": region},
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byAccount[accountID] = append(r.byAccount[accountID], inst)
	r.accountPri[accountID] = provider
	return inst
}

// Search 支持两种口径:按账号枚举(采集链路)与按厂商计数(健康监控)。
func (r *InstanceRepo) Search(_ context.Context, f camdomain.SearchFilter) ([]camdomain.Instance, int64, error) {
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

// MetricCall 记录一次厂商指标查询的入参(区间/region 透传断言用)。
type MetricCall struct {
	FsID   string
	FsName string
	Region string
	Start  string
	End    string
}

// QuerierNAS 可配厂商指标查询替身(支持/不支持探测、正常/失败注入)。
// 置 Err 非空模拟「调用失败」形态;MetricCapable=false 时退化为 plainNAS
// (未实现 NASMetricQuerier,探测不支持语义)。
type QuerierNAS struct {
	cloudx.NASAdapter // 形状借位:仅 GetNASMetrics 生效,由适配器桩显式装配

	MetricCapable bool

	mu      sync.Mutex
	err     error
	metrics []types.NASMetric
	calls   []MetricCall
}

// SetMetrics 设置该厂商返回的日值集(按请求区间过滤回发)。
func (q *QuerierNAS) SetMetrics(metrics ...types.NASMetric) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.metrics = append([]types.NASMetric(nil), metrics...)
}

// Fail 注入调用失败(宕机/鉴权失效形态,区别于探测不支持)。
func (q *QuerierNAS) Fail(err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.err = err
}

// Recover 清除故障注入(次日恢复场景)。
func (q *QuerierNAS) Recover() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.err = nil
}

// Calls 返回已发生的查询入参快照。
func (q *QuerierNAS) Calls() []MetricCall {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]MetricCall(nil), q.calls...)
}

// GetNASMetrics 实现厂商指标查询(过滤请求区间;注入失败时返回错误)。
func (q *QuerierNAS) GetNASMetrics(_ context.Context, fsID, fsName, region, startDate, endDate string) ([]types.NASMetric, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.calls = append(q.calls, MetricCall{FsID: fsID, FsName: fsName, Region: region, Start: startDate, End: endDate})
	if q.err != nil {
		return nil, q.err
	}
	out := make([]types.NASMetric, 0, len(q.metrics))
	for _, m := range q.metrics {
		if m.Date < startDate || m.Date > endDate {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// CloudAdapterStub 仅 NAS()/GetProvider() 生效的云适配器桩。
type CloudAdapterStub struct {
	cloudx.CloudAdapter
	provider camshared.CloudProvider
	nas      cloudx.NASAdapter
}

// GetProvider 返回桩厂商。
func (s *CloudAdapterStub) GetProvider() camshared.CloudProvider { return s.provider }

// NAS 返回 NAS 适配器(可能未实现指标查询)。
func (s *CloudAdapterStub) NAS() cloudx.NASAdapter { return s.nas }

var (
	_ cloudx.CloudAdapter     = (*CloudAdapterStub)(nil)
	_ cloudx.NASMetricQuerier = (*QuerierNAS)(nil)
)

// ==================== task repo / alerters / gate store ====================

// TaskRepo 任务仓储内存替身(记录 Create 的任务,供 Result 断言)。
type TaskRepo struct {
	taskx.TaskRepository

	mu    sync.Mutex
	tasks []taskx.Task
}

// Create 记录任务。
func (r *TaskRepo) Create(_ context.Context, t taskx.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tasks = append(r.tasks, t)
	return nil
}

// UpdateProgress 进度留痕 no-op。
func (r *TaskRepo) UpdateProgress(_ context.Context, _ string, _ int, _ string) error { return nil }

// UpdateStatus 状态留痕 no-op。
func (r *TaskRepo) UpdateStatus(_ context.Context, _ string, _ taskx.TaskStatus, _ string) error {
	return nil
}

// Tasks 返回已创建任务快照。
func (r *TaskRepo) Tasks() []taskx.Task {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]taskx.Task(nil), r.tasks...)
}

// GateAlert 日闸故障告警调用记录。
type GateAlert struct {
	ResourceType string
	Operation    string
	Failures     int
	Err          error
}

// GateAlerter 日闸故障告警通道替身(与 NAS 健康告警分属两个接口)。
type GateAlerter struct {
	mu    sync.Mutex
	calls []GateAlert
}

// AlertDailyGateFailure 记录一次日闸故障告警。
func (a *GateAlerter) AlertDailyGateFailure(_ context.Context, resourceType, operation string, consecutiveFailures int, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, GateAlert{ResourceType: resourceType, Operation: operation, Failures: consecutiveFailures, Err: err})
}

// Calls 返回告警记录快照。
func (a *GateAlerter) Calls() []GateAlert {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]GateAlert(nil), a.calls...)
}

// HealthAlert 零成功升级告警调用记录。
type HealthAlert struct {
	Provider      string
	WindowDays    int
	InstanceCount int64
}

// HealthAlerter NAS 自我健康监控告警通道替身。
type HealthAlerter struct {
	mu    sync.Mutex
	calls []HealthAlert
}

// AlertNASZeroSuccess 记录一次零成功升级告警。
func (a *HealthAlerter) AlertNASZeroSuccess(_ context.Context, provider string, windowDays int, instanceCount int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, HealthAlert{Provider: provider, WindowDays: windowDays, InstanceCount: instanceCount})
}

// Calls 返回告警记录快照。
func (a *HealthAlerter) Calls() []HealthAlert {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]HealthAlert(nil), a.calls...)
}

// GateStore scheduler_state 内存替身(原子认领语义:last_date < today 才推进)。
type GateStore struct {
	scheduler.DailyGateStore

	mu            sync.Mutex
	lastDates     map[string]string
	getErr        map[string]error
	claimErr      map[string]error
	claimFailLeft map[string]int
	getCalls      int
	claimCalls    int
}

// NewGateStore 创建空日闸存储。
func NewGateStore() *GateStore {
	return &GateStore{
		lastDates:     make(map[string]string),
		getErr:        make(map[string]error),
		claimErr:      make(map[string]error),
		claimFailLeft: make(map[string]int),
	}
}

// GetLastDate 读某资源类型的 last_date(无记录返回空串)。
func (s *GateStore) GetLastDate(_ context.Context, resourceType string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getCalls++
	if err := s.getErr[resourceType]; err != nil {
		return "", err
	}
	return s.lastDates[resourceType], nil
}

// TryClaimDaily 原子认领:仅 last_date 早于 date 时推进并返回真。
func (s *GateStore) TryClaimDaily(_ context.Context, resourceType, date string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimCalls++
	if s.claimFailLeft[resourceType] > 0 {
		s.claimFailLeft[resourceType]--
		err := s.claimErr[resourceType]
		if s.claimFailLeft[resourceType] == 0 {
			delete(s.claimErr, resourceType)
		}
		return false, err
	}
	if err := s.claimErr[resourceType]; err != nil {
		return false, err
	}
	if s.lastDates[resourceType] >= date {
		return false, nil
	}
	s.lastDates[resourceType] = date
	return true, nil
}

// FailGet 注入读失败。
func (s *GateStore) FailGet(resourceType string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getErr[resourceType] = err
}

// FailClaim 注入随后 n 次(而非 n 秒)写失败,之后恢复(确定性故障注入)。
func (s *GateStore) FailClaim(resourceType string, n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimFailLeft[resourceType] = n
	s.claimErr[resourceType] = err
}

// LastDate 读取某资源类型当前 last_date(断言用)。
func (s *GateStore) LastDate(resourceType string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastDates[resourceType]
}

// SnapshotClaimCalls 返回至今为止的认领写调用次数(断言用)。
func (s *GateStore) SnapshotClaimCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.claimCalls
}

// ==================== harness ====================

// Provider 一个已注册的测试厂商(名字全局唯一,避免跨 harness 串号)。
type Provider struct {
	Name    string
	Querier *QuerierNAS
}

// Harness nas-ops-insight journey 世界:账号 + 实例 + 指标表 + 任务仓储 +
// 日闸存储 + 告警通道,并暴露生产执行器/查询服务/路由的装配入口。
type Harness struct {
	TenantID int64

	AccountRepo   *AccountRepo
	InstanceRepo  *InstanceRepo
	MetricDAO     *MetricDAO
	TaskRepo      *TaskRepo
	GateStore     *GateStore
	GateAlerter   *GateAlerter
	HealthAlerter *HealthAlerter

	providerSeq int
}

// NewHarness 创建 harness(默认租户 1)。
func NewHarness() *Harness {
	return &Harness{
		TenantID:      1,
		AccountRepo:   NewAccountRepo(),
		InstanceRepo:  NewInstanceRepo(),
		MetricDAO:     NewMetricDAO(),
		TaskRepo:      &TaskRepo{},
		GateStore:     NewGateStore(),
		GateAlerter:   &GateAlerter{},
		HealthAlerter: &HealthAlerter{},
	}
}

// providerNameSeq 进程级厂商名序号:同一测试二进制内全局唯一,避免多个
// harness 复用同名厂商时撞上 cloudx 包级适配器缓存(provider_accountID 键)。
var providerNameSeq int64

// NewProvider 注册一个测试厂商并返回句柄;metricCapable=false 时为
// 「探测不支持」形态(未实现 NASMetricQuerier)。
func (h *Harness) NewProvider(metricCapable bool) *Provider {
	n := atomic.AddInt64(&providerNameSeq, 1)
	name := fmt.Sprintf("nasjt-%d-%s", n, map[bool]string{true: "metric", false: "plain"}[metricCapable])
	return h.NewNamedProvider(name, metricCapable)
}

// NewNamedProvider 以指定名字注册测试厂商(需要命中生产厂商清单的字面量时用,
// 如自我健康监控的必达厂商 aliyun/huawei/aws)。仅测试二进制内生效;注册前
// 清空 cloudx 包级适配器缓存,确保 Execute 一定拿到最新 creator。
func (h *Harness) NewNamedProvider(name string, metricCapable bool) *Provider {
	// 包级适配器缓存失效(全局生效):防止先前测试留下的同名缓存适配器
	cloudx.NewAdapterFactory(elog.DefaultLogger).ClearCache()
	querier := &QuerierNAS{MetricCapable: metricCapable}
	var nas cloudx.NASAdapter
	if metricCapable {
		// *QuerierNAS 经内嵌 NASAdapter 形状借位满足 NASAdapter,且实现
		// NASMetricQuerier(可指标查询形态)
		nas = querier
	} else {
		nas = &plainNASAdapter{}
	}
	cloudx.RegisterAdapter(camshared.CloudProvider(name), func(_ *camshared.CloudAccount) (cloudx.CloudAdapter, error) {
		return &CloudAdapterStub{provider: camshared.CloudProvider(name), nas: nas}, nil
	})
	return &Provider{Name: name, Querier: querier}
}

// PerAccountProvider 按账号返回独立适配器实例的测试厂商:适配器缓存键为
// provider_accountID,每个账号各持一个 querier(账号级故障注入/各自口径用)。
type PerAccountProvider struct {
	Name string

	mu       sync.Mutex
	queriers map[int64]*QuerierNAS
}

// QuerierFor 返回指定账号的 querier(懒创建,同一账号恒定)。
func (p *PerAccountProvider) QuerierFor(accountID int64) *QuerierNAS {
	p.mu.Lock()
	defer p.mu.Unlock()
	q, ok := p.queriers[accountID]
	if !ok {
		q = &QuerierNAS{MetricCapable: true}
		p.queriers[accountID] = q
	}
	return q
}

// NewPerAccountProvider 注册按账号独立的测试厂商。
func (h *Harness) NewPerAccountProvider() *PerAccountProvider {
	n := atomic.AddInt64(&providerNameSeq, 1)
	name := fmt.Sprintf("nasjt-%d-peracct", n)
	cloudx.NewAdapterFactory(elog.DefaultLogger).ClearCache()
	p := &PerAccountProvider{Name: name, queriers: make(map[int64]*QuerierNAS)}
	cloudx.RegisterAdapter(camshared.CloudProvider(name), func(account *camshared.CloudAccount) (cloudx.CloudAdapter, error) {
		return &CloudAdapterStub{provider: camshared.CloudProvider(name), nas: p.QuerierFor(account.ID)}, nil
	})
	return p
}

// plainNASAdapter 未实现 NASMetricQuerier 的 NAS 适配器(探测不支持语义)。
type plainNASAdapter struct{}

func (p *plainNASAdapter) ListInstances(_ context.Context, _ string) ([]types.NASInstance, error) {
	return nil, nil
}
func (p *plainNASAdapter) GetInstance(_ context.Context, _, _ string) (*types.NASInstance, error) {
	return nil, nil
}
func (p *plainNASAdapter) ListInstancesByIDs(_ context.Context, _ string, _ []string) ([]types.NASInstance, error) {
	return nil, nil
}
func (p *plainNASAdapter) GetInstanceStatus(_ context.Context, _, _ string) (string, error) {
	return "Running", nil
}
func (p *plainNASAdapter) ListInstancesWithFilter(_ context.Context, _ string, _ *types.NASInstanceFilter) ([]types.NASInstance, error) {
	return nil, nil
}

var _ cloudx.NASAdapter = (*plainNASAdapter)(nil)

// SeedAccount 注册一个活跃云账号(默认属 harness 租户)。
func (h *Harness) SeedAccount(id int64, provider *Provider) camshared.CloudAccount {
	return h.SeedAccountTenant(id, provider.Name, h.TenantID)
}

// SeedAccountNamed 以厂商名注册活跃云账号(PerAccountProvider 场景用)。
func (h *Harness) SeedAccountNamed(id int64, providerName string) camshared.CloudAccount {
	account := camshared.CloudAccount{
		ID:              id,
		Name:            fmt.Sprintf("acc-%s-%d", providerName, id),
		Provider:        camshared.CloudProvider(providerName),
		TenantID:        h.TenantID,
		Status:          camshared.CloudAccountStatusActive,
		AccessKeyID:     "test-ak",
		AccessKeySecret: "test-sk",
	}
	h.AccountRepo.SeedAccount(account)
	return account
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

// SeedInstance 给账号注册一个 NAS 实例(采集链路以本地实例枚举为准)。
func (h *Harness) SeedInstance(accountID int64, provider *Provider, fsID, name, region string) camdomain.Instance {
	return h.SeedInstanceNamed(accountID, provider.Name, fsID, name, region)
}

// SeedInstanceNamed 以厂商名注册 NAS 实例(PerAccountProvider 场景用)。
func (h *Harness) SeedInstanceNamed(accountID int64, providerName, fsID, name, region string) camdomain.Instance {
	return h.InstanceRepo.SeedInstance(accountID, camshared.CloudProvider(providerName), fsID, name, region)
}

// SeedMetric 直写一行指标(经写入门禁,零容量行自动打 zero_exception)。
func (h *Harness) SeedMetric(t *testing.T, accountID int64, providerName string, m types.NASMetric) {
	t.Helper()
	m.AccountID = accountID
	m.Provider = providerName
	require.NoError(t, h.MetricDAO.UpsertMetric(context.Background(), m))
}

// NewCollectExecutor 装配生产采集执行器(挂自我健康监控告警桥)。
func (h *Harness) NewCollectExecutor() *executor.SyncNASMetricsExecutor {
	e := executor.NewSyncNASMetricsExecutor(
		h.AccountRepo,
		h.InstanceRepo,
		h.MetricDAO,
		h.TaskRepo,
		elog.DefaultLogger,
	)
	e.SetNASHealthAlerter(h.HealthAlerter)
	return e
}

// RunCollect 以指定参数执行一次采集任务,返回任务(Result 已填充)。
func (h *Harness) RunCollect(t *testing.T, params map[string]any) (*taskx.Task, error) {
	t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	task := &taskx.Task{
		ID:        fmt.Sprintf("task-nasjt-%d", time.Now().UnixNano()),
		Type:      executor.TaskTypeNASCollectMetrics,
		Status:    taskx.TaskStatusPending,
		Params:    params,
		CreatedBy: "nasjt",
	}
	err := h.NewCollectExecutor().Execute(context.Background(), task)
	return task, err
}

// NewQueryService 装配生产 NAS 读取服务(读同一内存指标表)。
func (h *Harness) NewQueryService() *service.NASQueryService {
	return service.NewNASQueryService(h.AccountRepo, h.MetricDAO, elog.DefaultLogger)
}

// NewNASRouter 装配 /assets/nas/metrics 与 /assets/nas/top 路由(注入租户)。
func (h *Harness) NewNASRouter(tenantID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	tenant := func(c *gin.Context) {
		c.Set(middleware.TenantIDKey, tenantID)
		c.Next()
	}
	// RegisterRoutesWithGroup 期望收到的已是 assets 路由组(内部直接注册
	// /nas/metrics 等),故组前缀为 /assets,与生产 /cam/assets/* 对齐
	group := router.Group("/assets", tenant)
	web.NewAssetHandler(nil, nil, nil, h.NewQueryService(), nil).RegisterRoutesWithGroup(group)
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

// ResultField 读取采集任务 Result 中指定键。
func ResultField(t *testing.T, task *taskx.Task, key string) any {
	t.Helper()
	require.NotNil(t, task.Result, "任务 Result 应已填充")
	v, ok := task.Result[key]
	require.True(t, ok, "Result 应含键 %s, 实际 keys=%v", key, resultKeys(task.Result))
	return v
}

// ResultStrings 读取 Result 中字符串切片键(skipped_accounts 等形态)。
func ResultStrings(t *testing.T, task *taskx.Task, key string) []string {
	t.Helper()
	switch v := ResultField(t, task, key).(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, fmt.Sprint(item))
		}
		return out
	default:
		t.Fatalf("Result[%s] 应为字符串切片, 实际 %T", key, v)
		return nil
	}
}

// ProviderFailure failures 数组单项的形状镜像(executor.nasProviderFailure
// 为非导出类型,经 JSON 往返提取字段)。
type ProviderFailure struct {
	Provider   string `json:"provider"`
	AccountID  int64  `json:"account_id"`
	ErrorCount int    `json:"error_count"`
	LastError  string `json:"last_error"`
}

// ResultFailures 读取 Result 的 failures 数组(厂商/账号维度失败明细)。
func ResultFailures(t *testing.T, task *taskx.Task) []ProviderFailure {
	t.Helper()
	v, ok := task.Result["failures"]
	require.True(t, ok, "Result 应含 failures 键")
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	var failures []ProviderFailure
	require.NoError(t, json.Unmarshal(raw, &failures))
	return failures
}

// FailureFor 在 failures 中定位指定厂商的失败明细(不存在返回 false)。
func FailureFor(failures []ProviderFailure, provider string) (ProviderFailure, bool) {
	for _, f := range failures {
		if f.Provider == provider {
			return f, true
		}
	}
	return ProviderFailure{}, false
}

// ResultAlerts 读取 Result 的 health_alerts 厂商清单。
func ResultAlerts(t *testing.T, task *taskx.Task) []string {
	return ResultStrings(t, task, "health_alerts")
}

func resultKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// Metric 构造一行厂商日指标(GB 口径)。
func Metric(fsID, fsName, date string, capacityGB, usedGB float64) types.NASMetric {
	return types.NASMetric{FsID: fsID, FsName: fsName, Date: date, Capacity: capacityGB, UsedCapacity: usedGB}
}

// ==================== real DAO live access ( MONGO_DSN gated ) ====================

// LiveMetricDAO 构造真实 NASMetricDAO(独立测试库 ecam_dao_test + Drop 清理)。
// 未设置 MONGO_DSN 时跳过;DSN 指向的实例 5 秒内不可达同样跳过
// (与 internal/cam/repository/dao live 测试同约定,仅门控语义更宽容)。
func LiveMetricDAO(t *testing.T) dao.NASMetricDAO {
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
	return dao.NewNASMetricDAO(mongox.NewMongo(client, "ecam_dao_test"))
}

// RequireNoCrossAccountLoss 断言同 fs 同日多账号各行独立存在且值各归其主。
func RequireNoCrossAccountLoss(t *testing.T, dao *MetricDAO, fsID, date string, expect map[int64]float64) {
	t.Helper()
	for accountID, capacity := range expect {
		row, ok := dao.Row(accountID, fsID, date)
		require.True(t, ok, "账号 %d 在 %s/%s 应保留自己的指标行", accountID, fsID, date)
		require.Equal(t, capacity, row.Capacity, "账号 %d 的容量口径不得被其他账号写入覆盖", accountID)
	}
}

// NormalizeFSSort 稳定排序 fs 标识(断言辅助,避免 map 遍历序噪声)。
func NormalizeFSSort(items []string) string {
	sorted := append([]string(nil), items...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return strings.Join(sorted, ",")
}
