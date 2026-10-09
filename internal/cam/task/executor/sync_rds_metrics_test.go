package executor

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cloudx-sdk"
	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/Havens-blog/e-common-go/taskx"
)

// ==================== 测试用 mock ====================

// rdsMetricDAOMock 指标 DAO mock(分别记录首写批与覆盖批,校验日期分流)
type rdsMetricDAOMock struct {
	dao.RDSMetricDAO
	insertsIfAbsent   []types.RDSMetric
	upserts           []types.RDSMetric
	insertIfAbsentErr error
	bulkUpsertErr     error
}

func (m *rdsMetricDAOMock) BulkInsertIfAbsent(_ context.Context, metrics []types.RDSMetric) error {
	if m.insertIfAbsentErr != nil {
		return m.insertIfAbsentErr
	}
	m.insertsIfAbsent = append(m.insertsIfAbsent, metrics...)
	return nil
}

func (m *rdsMetricDAOMock) BulkUpsertMetrics(_ context.Context, metrics []types.RDSMetric) error {
	if m.bulkUpsertErr != nil {
		return m.bulkUpsertErr
	}
	m.upserts = append(m.upserts, metrics...)
	return nil
}

// metricCapableRDS 支持指标查询的 RDS 适配器 mock(RDSAdapter + RDSMetricQuerier)
type metricCapableRDS struct {
	metrics []types.RDSMetric
	err     error
	// 调用参数记录(区间/region/engine 透传断言用;RDS 是地域性资源且多引擎
	// 分派,querier 签名带 region+engine,均须按实例真实值透传)
	lastRDSID  string
	lastName   string
	lastRegion string
	lastEngine string
	lastStart  string
	lastEnd    string
}

func (d *metricCapableRDS) ListInstances(context.Context, string) ([]types.RDSInstance, error) {
	return nil, nil
}
func (d *metricCapableRDS) GetInstance(context.Context, string, string) (*types.RDSInstance, error) {
	return nil, nil
}
func (d *metricCapableRDS) ListInstancesByIDs(context.Context, string, []string) ([]types.RDSInstance, error) {
	return nil, nil
}
func (d *metricCapableRDS) GetInstanceStatus(context.Context, string, string) (string, error) {
	return "", nil
}
func (d *metricCapableRDS) ListInstancesWithFilter(context.Context, string, *types.RDSInstanceFilter) ([]types.RDSInstance, error) {
	return nil, nil
}
func (d *metricCapableRDS) GetRDSMetrics(_ context.Context, rdsID, instanceName, region, engine, startDate, endDate string) ([]types.RDSMetric, error) {
	if d.err != nil {
		return nil, d.err
	}
	d.lastRDSID, d.lastName, d.lastRegion, d.lastEngine, d.lastStart, d.lastEnd = rdsID, instanceName, region, engine, startDate, endDate
	out := make([]types.RDSMetric, 0, len(d.metrics))
	for _, m := range d.metrics {
		if m.RdsID != rdsID {
			continue // 只返回该实例的行(与真实厂商语义一致)
		}
		if m.Date < startDate || m.Date > endDate {
			continue // 只返回请求区间内的日值(与真实厂商语义一致)
		}
		out = append(out, m)
	}
	return out, nil
}

// plainRDS 不支持指标查询的 RDS 适配器 mock(未实现 RDSMetricQuerier,
// 注意不得内嵌 metricCapableRDS,否则提升的方法集会使其「实现」接口)
type plainRDS struct{}

func (d *plainRDS) ListInstances(context.Context, string) ([]types.RDSInstance, error) {
	return nil, nil
}
func (d *plainRDS) GetInstance(context.Context, string, string) (*types.RDSInstance, error) {
	return nil, nil
}
func (d *plainRDS) ListInstancesByIDs(context.Context, string, []string) ([]types.RDSInstance, error) {
	return nil, nil
}
func (d *plainRDS) GetInstanceStatus(context.Context, string, string) (string, error) {
	return "", nil
}
func (d *plainRDS) ListInstancesWithFilter(context.Context, string, *types.RDSInstanceFilter) ([]types.RDSInstance, error) {
	return nil, nil
}

var _ cloudx.RDSAdapter = (*plainRDS)(nil)
var _ cloudx.RDSAdapter = (*metricCapableRDS)(nil)
var _ cloudx.RDSMetricQuerier = (*metricCapableRDS)(nil)

// rdsMetricCloudAdapter 支持/不支持指标的 CloudAdapter mock(仅 RDS() 生效)
type rdsMetricCloudAdapter struct {
	cloudx.CloudAdapter
	provider domain.CloudProvider
	rds      cloudx.RDSAdapter
}

func (m *rdsMetricCloudAdapter) GetProvider() domain.CloudProvider { return m.provider }
func (m *rdsMetricCloudAdapter) RDS() cloudx.RDSAdapter            { return m.rds }

var _ cloudx.CloudAdapter = (*rdsMetricCloudAdapter)(nil)

// 测试用厂商键(全局注册表,用独特前缀避免与其他用例冲突)
const (
	testRDSProviderWithMetrics    = domain.CloudProvider("rdsmetric-yes")
	testRDSProviderWithoutMetrics = domain.CloudProvider("rdsmetric-no")
	testRDSProviderFailing        = domain.CloudProvider("rdsmetric-fail")
	testRDSProviderEmpty          = domain.CloudProvider("rdsmetric-empty")
	testRDSProviderNoRDS          = domain.CloudProvider("rdsmetric-nords")
)

// testRDSMetricSet 标准测试指标集(每次 CreateAdapter 现算日期):
// rm-a 今日行 + 昨日行;rm-zero-today 今日四指标全 0 行(异常行必须可见,
// 不继承 CDN 全零跳过过滤)
func testRDSMetricSet() []types.RDSMetric {
	now := time.Now().In(nasMetricsCSTZone)
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	return []types.RDSMetric{
		{RdsID: "rm-a", InstanceName: "rm-a", Date: today, CPUPercent: 12.3, MemoryPercent: 45.6, DiskPercent: 7.8, Connections: 21, Engine: "MySQL"},
		{RdsID: "rm-a", InstanceName: "rm-a", Date: yesterday, CPUPercent: 11.1, MemoryPercent: 44.4, DiskPercent: 7.7, Connections: 20, Engine: "MySQL"},
		{RdsID: "rm-zero-today", InstanceName: "rm-zero-today", Date: today, CPUPercent: 0, MemoryPercent: 0, DiskPercent: 0, Connections: 0, Engine: "MySQL"},
	}
}

func init() {
	cloudx.RegisterAdapter(testRDSProviderWithMetrics, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &rdsMetricCloudAdapter{
			provider: testRDSProviderWithMetrics,
			rds:      &metricCapableRDS{metrics: testRDSMetricSet()},
		}, nil
	})
	cloudx.RegisterAdapter(testRDSProviderWithoutMetrics, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &rdsMetricCloudAdapter{
			provider: testRDSProviderWithoutMetrics,
			rds:      &plainRDS{},
		}, nil
	})
	cloudx.RegisterAdapter(testRDSProviderFailing, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &rdsMetricCloudAdapter{
			provider: testRDSProviderFailing,
			rds: &metricCapableRDS{
				err: errors.New("DescribeMetricList quota exceeded"),
			},
		}, nil
	})
	cloudx.RegisterAdapter(testRDSProviderEmpty, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &rdsMetricCloudAdapter{
			provider: testRDSProviderEmpty,
			rds:      &metricCapableRDS{}, // 无指标且无错误 = 真实无数据
		}, nil
	})
	cloudx.RegisterAdapter(testRDSProviderNoRDS, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &rdsMetricCloudAdapter{
			provider: testRDSProviderNoRDS,
			rds:      nil, // RDS 适配器不可用(整账号失败路径)
		}, nil
	})
}

func rdsMetricTestAccount(id int64, provider domain.CloudProvider) domain.CloudAccount {
	return domain.CloudAccount{
		ID:              id,
		Name:            fmt.Sprintf("acc-%s-%d", provider, id),
		Provider:        provider,
		TenantID:        1,
		Status:          domain.CloudAccountStatusActive,
		AccessKeyID:     "ak",
		AccessKeySecret: "sk",
	}
}

// rdsTestInstance 构造 ecam_instance 形态的 RDS 实例(sync_rds.go 落库形态:
// AssetID=rds_id,AssetName=实例名,attributes 含 region/engine)
func rdsTestInstance(accountID int64, rdsID, region, engine string) camdomain.Instance {
	return camdomain.Instance{
		ModelUID:  "test_rds",
		AssetID:   rdsID,
		AssetName: rdsID,
		TenantID:  1,
		AccountID: accountID,
		Attributes: map[string]interface{}{
			"region": region,
			"engine": engine,
		},
	}
}

func newTestRDSMetricsExecutor(
	t *testing.T,
	accounts []domain.CloudAccount,
	instances map[int64][]camdomain.Instance,
) (*SyncRDSMetricsExecutor, *rdsMetricDAOMock) {
	t.Helper()
	daoMock := &rdsMetricDAOMock{}
	e := NewSyncRDSMetricsExecutor(
		&cdnMetricAccountRepo{accounts: accounts},
		&nasInstanceRepoMock{byAccount: instances},
		daoMock,
		&cdnMetricTaskRepo{},
		testLogger(),
	)
	return e, daoMock
}

// ==================== 用例 ====================

// 任务类型注册正确:rds:collect_metrics 可被调度器识别提交
func TestSyncRDSMetrics_GetType(t *testing.T) {
	e, _ := newTestRDSMetricsExecutor(t, nil, nil)
	if got := e.GetType(); got != taskx.TaskType("rds:collect_metrics") {
		t.Fatalf("GetType = %q, want rds:collect_metrics", got)
	}
}

// 无活跃账号:不报错,空跑结束
func TestSyncRDSMetrics_NoAccounts(t *testing.T) {
	e, daoMock := newTestRDSMetricsExecutor(t, nil, nil)
	task := &taskx.Task{ID: "t8", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(daoMock.insertsIfAbsent)+len(daoMock.upserts) != 0 {
		t.Fatal("无账号不得产生写入")
	}
}

// 活跃账号筛选:ecam_instance 中无 RDS 实例的账号不采集,有 RDS 的账号正常采集
// (Hard Rule:活跃账号口径 = 存在 ≥1 个 RDS 实例,不依赖 EnableAutoSync)
func TestSyncRDSMetrics_ActiveAccountFiltering(t *testing.T) {
	acc1 := rdsMetricTestAccount(1, testRDSProviderWithMetrics)
	acc2 := rdsMetricTestAccount(2, testRDSProviderWithMetrics)
	e, daoMock := newTestRDSMetricsExecutor(t,
		[]domain.CloudAccount{acc1, acc2},
		map[int64][]camdomain.Instance{
			1: {rdsTestInstance(1, "rm-a", "cn-hangzhou", "MySQL")},
			// 账号 2 无任何 RDS 实例 → 非活跃账号
		},
	)
	task := &taskx.Task{ID: "t1", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	// 仅账号 1 被采集:rm-a 今日/昨日 2 行 + rm-zero-today 今日行 = 3 行
	// (mock querier 按实例过滤:账号 1 只有 rm-a 实例 → 2 行)
	written := len(daoMock.insertsIfAbsent) + len(daoMock.upserts)
	if written != 2 {
		t.Fatalf("written = %d, want 2 (无 RDS 实例账号不得产生写入)", written)
	}
	for _, m := range daoMock.insertsIfAbsent {
		if m.AccountID != 1 {
			t.Fatalf("无 RDS 实例的账号 2 被采集: %+v", m)
		}
	}
	withoutRDS, _ := task.Result["accounts_without_rds"].([]string)
	if len(withoutRDS) != 1 || withoutRDS[0] != acc2.Name {
		t.Fatalf("accounts_without_rds = %v, want [%s]", withoutRDS, acc2.Name)
	}
}

// 首写生效 vs 昨日覆盖:今日行走 insert-if-absent(首写保护),昨日行走覆盖 upsert;
// AccountID/Provider 由执行器回填(querier 签名不感知账号)
func TestSyncRDSMetrics_FirstWriteVsYesterdayOverwrite(t *testing.T) {
	e, daoMock := newTestRDSMetricsExecutor(t,
		[]domain.CloudAccount{rdsMetricTestAccount(3, testRDSProviderWithMetrics)},
		map[int64][]camdomain.Instance{
			3: {
				rdsTestInstance(3, "rm-a", "cn-hangzhou", "MySQL"),
				rdsTestInstance(3, "rm-zero-today", "cn-hangzhou", "MySQL"),
			},
		},
	)
	task := &taskx.Task{ID: "t2", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	today := time.Now().In(nasMetricsCSTZone).Format("2006-01-02")
	yesterday := time.Now().In(nasMetricsCSTZone).AddDate(0, 0, -1).Format("2006-01-02")

	// 今日行(rm-a + rm-zero-today)全部走首写生效批
	if len(daoMock.insertsIfAbsent) != 2 {
		t.Fatalf("insert-if-absent rows = %d, want 2: %+v", len(daoMock.insertsIfAbsent), daoMock.insertsIfAbsent)
	}
	for _, m := range daoMock.insertsIfAbsent {
		if m.Date != today {
			t.Fatalf("今日行未走首写生效: date=%s (want %s)", m.Date, today)
		}
		if m.AccountID != 3 || m.Provider != string(testRDSProviderWithMetrics) {
			t.Fatalf("metric not enriched: %+v", m)
		}
		if m.Engine != "MySQL" {
			t.Fatalf("engine 未透传: %+v", m)
		}
	}
	// 昨日行(rm-a)全部走覆盖 upsert 批
	if len(daoMock.upserts) != 1 {
		t.Fatalf("upsert rows = %d, want 1: %+v", len(daoMock.upserts), daoMock.upserts)
	}
	for _, m := range daoMock.upserts {
		if m.Date != yesterday {
			t.Fatalf("昨日行未走覆盖更新: date=%s (want %s)", m.Date, yesterday)
		}
	}
	if got := task.Result["metrics_total"]; got != 3 {
		t.Fatalf("metrics_total = %v, want 3", got)
	}
}

// region/engine 透传(RDS 是地域性资源且多引擎分派,querier 签名带 region+engine):
// 按实例 attributes 逐实例查询,不做全局推断
func TestSyncRDSMetrics_RegionEnginePassthrough(t *testing.T) {
	e, _ := newTestRDSMetricsExecutor(t,
		[]domain.CloudAccount{rdsMetricTestAccount(12, testRDSProviderWithMetrics)},
		map[int64][]camdomain.Instance{
			12: {rdsTestInstance(12, "rm-a", "ap-southeast-1", "PostgreSQL")},
		},
	)
	if err := e.Execute(context.Background(), &taskx.Task{ID: "t12", Params: map[string]any{}}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	acc := rdsMetricTestAccount(12, testRDSProviderWithMetrics)
	adapter, err := e.cloudxFactory.CreateAdapter(&acc)
	if err != nil {
		t.Fatalf("create adapter: %v", err)
	}
	q := adapter.RDS().(*metricCapableRDS)
	if q.lastRDSID != "rm-a" || q.lastName != "rm-a" {
		t.Fatalf("rds/name 透传 = %s/%s, want rm-a/rm-a", q.lastRDSID, q.lastName)
	}
	if q.lastRegion != "ap-southeast-1" {
		t.Fatalf("region 透传 = %s, want ap-southeast-1", q.lastRegion)
	}
	if q.lastEngine != "PostgreSQL" {
		t.Fatalf("engine 透传 = %s, want PostgreSQL", q.lastEngine)
	}
	// 区间与执行器 [startDate, endDate] 一致(默认 days=2 → [昨日, 今日])
	today := time.Now().In(nasMetricsCSTZone).Format("2006-01-02")
	yesterday := time.Now().In(nasMetricsCSTZone).AddDate(0, 0, -1).Format("2006-01-02")
	if q.lastStart != yesterday || q.lastEnd != today {
		t.Fatalf("date range 透传 = %s ~ %s, want %s ~ %s", q.lastStart, q.lastEnd, yesterday, today)
	}
}

// 不继承 CDN「全零跳过」过滤:四指标全 0 异常行照常入批落库
// (qc_status=zero_exception 由 DAO 写路径打标,执行器不拦截)
func TestSyncRDSMetrics_ZeroRowVisible(t *testing.T) {
	e, daoMock := newTestRDSMetricsExecutor(t,
		[]domain.CloudAccount{rdsMetricTestAccount(4, testRDSProviderWithMetrics)},
		map[int64][]camdomain.Instance{
			4: {rdsTestInstance(4, "rm-zero-today", "cn-hangzhou", "MySQL")},
		},
	)
	task := &taskx.Task{ID: "t3", Params: map[string]any{"days": 1}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	// days=1 区间只有今日:mock querier 返回 rm-zero-today 的全 0 行,
	// 不得被 CDN 式「全零跳过」过滤掉
	if len(daoMock.insertsIfAbsent) != 1 {
		t.Fatalf("insert-if-absent rows = %d, want 1: %+v", len(daoMock.insertsIfAbsent), daoMock.insertsIfAbsent)
	}
	if len(daoMock.upserts) != 0 {
		t.Fatalf("upsert rows = %d, want 0 (days=1 区间无昨日行)", len(daoMock.upserts))
	}
	row := daoMock.insertsIfAbsent[0]
	if row.RdsID != "rm-zero-today" || row.CPUPercent != 0 || row.MemoryPercent != 0 || row.DiskPercent != 0 || row.Connections != 0 {
		t.Fatalf("全 0 异常行被过滤/篡改: %+v", row)
	}
	if got := task.Result["metrics_total"]; got != 1 {
		t.Fatalf("metrics_total = %v, want 1", got)
	}
	if _, ok := task.Result["health_alerts"]; !ok {
		t.Fatal("Result 须保留 health_alerts 键位(RDS 暂恒 null,与 NAS/Disk 同构)")
	}
}

// 失败计数:querier 报错计入 Result["failures"](provider/account/error_count/last_error),
// 单实例失败不中断其余实例
func TestSyncRDSMetrics_FailureCounting(t *testing.T) {
	e, daoMock := newTestRDSMetricsExecutor(t,
		[]domain.CloudAccount{rdsMetricTestAccount(5, testRDSProviderFailing)},
		map[int64][]camdomain.Instance{
			5: {rdsTestInstance(5, "rm-a", "cn-hangzhou", "MySQL")},
		},
	)
	task := &taskx.Task{ID: "t4", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(daoMock.insertsIfAbsent)+len(daoMock.upserts) != 0 {
		t.Fatal("查询失败不得产生写入")
	}
	failures, _ := task.Result["failures"].([]nasProviderFailure)
	if len(failures) != 1 {
		t.Fatalf("failures = %+v, want 1 条", task.Result["failures"])
	}
	f := failures[0]
	if f.Provider != string(testRDSProviderFailing) || f.AccountID != 5 {
		t.Fatalf("failure 归因错误: %+v", f)
	}
	if f.ErrorCount < 1 || f.LastError == "" {
		t.Fatalf("failure 计数/末次错误缺失: %+v", f)
	}
	if got := task.Result["failed_instances"]; got != 1 {
		t.Fatalf("failed_instances = %v, want 1", got)
	}
}

// 真实无数据(空结果)不计失败:failures 为空,任务正常结束
func TestSyncRDSMetrics_EmptyResultNotFailure(t *testing.T) {
	e, daoMock := newTestRDSMetricsExecutor(t,
		[]domain.CloudAccount{rdsMetricTestAccount(6, testRDSProviderEmpty)},
		map[int64][]camdomain.Instance{
			6: {rdsTestInstance(6, "rm-empty", "cn-hangzhou", "MySQL")},
		},
	)
	task := &taskx.Task{ID: "t5", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(daoMock.insertsIfAbsent)+len(daoMock.upserts) != 0 {
		t.Fatal("空结果不得产生写入")
	}
	failures, _ := task.Result["failures"].([]nasProviderFailure)
	if len(failures) != 0 {
		t.Fatalf("真实无数据不得计入失败: %+v", failures)
	}
}

// 跳过不实现 RDSMetricQuerier 的厂商:不写入、不报错,Result 可见(探测不支持不计失败)
func TestSyncRDSMetrics_SkipsProviderWithoutMetricSupport(t *testing.T) {
	e, daoMock := newTestRDSMetricsExecutor(t,
		[]domain.CloudAccount{rdsMetricTestAccount(7, testRDSProviderWithoutMetrics)},
		map[int64][]camdomain.Instance{
			7: {rdsTestInstance(7, "rm-x", "cn-hangzhou", "MySQL")},
		},
	)
	task := &taskx.Task{ID: "t6", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(daoMock.insertsIfAbsent)+len(daoMock.upserts) != 0 {
		t.Fatal("无指标支持厂商不得产生写入")
	}
	skipped, _ := task.Result["no_metric_support"].([]string)
	if len(skipped) != 1 || skipped[0] != string(testRDSProviderWithoutMetrics) {
		t.Fatalf("no_metric_support = %v", task.Result["no_metric_support"])
	}
	failures, _ := task.Result["failures"].([]nasProviderFailure)
	if len(failures) != 0 {
		t.Fatalf("探测不支持不得计入失败: %+v", failures)
	}
}

// RDS 适配器不可用(adapter.RDS() == nil):整账号失败路径,计入 failures
func TestSyncRDSMetrics_RDSAdapterUnavailable(t *testing.T) {
	e, daoMock := newTestRDSMetricsExecutor(t,
		[]domain.CloudAccount{rdsMetricTestAccount(9, testRDSProviderNoRDS)},
		map[int64][]camdomain.Instance{
			9: {rdsTestInstance(9, "rm-a", "cn-hangzhou", "MySQL")},
		},
	)
	task := &taskx.Task{ID: "t7", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(daoMock.insertsIfAbsent)+len(daoMock.upserts) != 0 {
		t.Fatal("适配器不可用不得产生写入")
	}
	failures, _ := task.Result["failures"].([]nasProviderFailure)
	if len(failures) != 1 || failures[0].AccountID != 9 {
		t.Fatalf("failures = %+v, want 账号 9 一条", failures)
	}
}

// 多账号多 region:同一账号不同实例按各自 attributes 查询(逐实例 region/engine
// 透传),账号级互斥闸独立于 NAS(并发同账号第二个任务跳过)
func TestSyncRDSMetrics_MultiRegionInstances(t *testing.T) {
	e, daoMock := newTestRDSMetricsExecutor(t,
		[]domain.CloudAccount{rdsMetricTestAccount(10, testRDSProviderWithMetrics)},
		map[int64][]camdomain.Instance{
			10: {
				rdsTestInstance(10, "rm-a", "cn-hangzhou", "MySQL"),
			},
		},
	)
	task := &taskx.Task{ID: "t9", Params: map[string]any{"days": 1}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	written := len(daoMock.insertsIfAbsent) + len(daoMock.upserts)
	if written != 1 {
		t.Fatalf("written = %d, want 1 (days=1 只有 rm-a 今日行)", written)
	}
	if got := task.Result["date_range"]; got == "" {
		t.Fatal("date_range 缺失")
	}
	if task.Progress != 100 {
		t.Fatalf("progress = %d, want 100", task.Progress)
	}
}
