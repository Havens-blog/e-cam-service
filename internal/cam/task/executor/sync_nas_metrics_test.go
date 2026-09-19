package executor

import (
	"context"
	"fmt"
	"testing"
	"time"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	camrepository "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
)

// ==================== 测试用 mock ====================

// nasInstanceRepoMock 实例仓储 mock(仅 Search 生效,按账号返回 ecam_instance
// 中的 NAS 实例——采集执行器以 ecam_instance 枚举为准,不调云端 ListInstances)
type nasInstanceRepoMock struct {
	camrepository.InstanceRepository
	byAccount map[int64][]camdomain.Instance
}

func (m *nasInstanceRepoMock) Search(_ context.Context, f camdomain.SearchFilter) ([]camdomain.Instance, int64, error) {
	insts := m.byAccount[f.AccountID]
	start := int(f.Offset)
	if start > len(insts) {
		start = len(insts)
	}
	end := len(insts)
	if f.Limit > 0 && start+int(f.Limit) < end {
		end = start + int(f.Limit)
	}
	return insts[start:end], int64(len(insts)), nil
}

// nasMetricDAOMock 指标 DAO mock(分别记录首写批与覆盖批,校验日期分流)
type nasMetricDAOMock struct {
	dao.NASMetricDAO
	insertsIfAbsent   []types.NASMetric
	upserts           []types.NASMetric
	insertIfAbsentErr error
	bulkUpsertErr     error
}

func (m *nasMetricDAOMock) BulkInsertIfAbsent(_ context.Context, metrics []types.NASMetric) error {
	if m.insertIfAbsentErr != nil {
		return m.insertIfAbsentErr
	}
	m.insertsIfAbsent = append(m.insertsIfAbsent, metrics...)
	return nil
}

func (m *nasMetricDAOMock) BulkUpsertMetrics(_ context.Context, metrics []types.NASMetric) error {
	if m.bulkUpsertErr != nil {
		return m.bulkUpsertErr
	}
	m.upserts = append(m.upserts, metrics...)
	return nil
}

// metricCapableNAS 支持指标查询的 NAS 适配器 mock(NASAdapter + NASMetricQuerier)
type metricCapableNAS struct {
	metrics []types.NASMetric
	// 调用参数记录(区间/region 透传断言用)
	lastFsID   string
	lastFsName string
	lastRegion string
	lastStart  string
	lastEnd    string
}

func (n *metricCapableNAS) ListInstances(_ context.Context, _ string) ([]types.NASInstance, error) {
	return nil, nil
}
func (n *metricCapableNAS) GetInstance(_ context.Context, _, _ string) (*types.NASInstance, error) {
	return nil, nil
}
func (n *metricCapableNAS) ListInstancesByIDs(_ context.Context, _ string, _ []string) ([]types.NASInstance, error) {
	return nil, nil
}
func (n *metricCapableNAS) GetInstanceStatus(_ context.Context, _, _ string) (string, error) {
	return "Running", nil
}
func (n *metricCapableNAS) ListInstancesWithFilter(_ context.Context, _ string, _ *types.NASInstanceFilter) ([]types.NASInstance, error) {
	return nil, nil
}
func (n *metricCapableNAS) GetNASMetrics(_ context.Context, fsID, fsName, region, startDate, endDate string) ([]types.NASMetric, error) {
	n.lastFsID, n.lastFsName, n.lastRegion, n.lastStart, n.lastEnd = fsID, fsName, region, startDate, endDate
	out := make([]types.NASMetric, 0, len(n.metrics))
	for _, m := range n.metrics {
		if m.Date < startDate || m.Date > endDate {
			continue // 只返回请求区间内的日值(与真实厂商语义一致)
		}
		out = append(out, m)
	}
	return out, nil
}

// plainNAS 不支持指标查询的 NAS 适配器 mock(未实现 NASMetricQuerier,
// 注意不得内嵌 metricCapableNAS,否则提升的方法集会使其"实现"接口)
type plainNAS struct{}

func (n *plainNAS) ListInstances(_ context.Context, _ string) ([]types.NASInstance, error) {
	return nil, nil
}
func (n *plainNAS) GetInstance(_ context.Context, _, _ string) (*types.NASInstance, error) {
	return nil, nil
}
func (n *plainNAS) ListInstancesByIDs(_ context.Context, _ string, _ []string) ([]types.NASInstance, error) {
	return nil, nil
}
func (n *plainNAS) GetInstanceStatus(_ context.Context, _, _ string) (string, error) {
	return "Running", nil
}
func (n *plainNAS) ListInstancesWithFilter(_ context.Context, _ string, _ *types.NASInstanceFilter) ([]types.NASInstance, error) {
	return nil, nil
}

var _ cloudx.NASAdapter = (*plainNAS)(nil)
var _ cloudx.NASAdapter = (*metricCapableNAS)(nil)
var _ cloudx.NASMetricQuerier = (*metricCapableNAS)(nil)

// nasMetricCloudAdapter 支持/不支持指标的 CloudAdapter mock(仅 NAS() 生效)
type nasMetricCloudAdapter struct {
	cloudx.CloudAdapter
	provider domain.CloudProvider
	nas      cloudx.NASAdapter
}

func (m *nasMetricCloudAdapter) GetProvider() domain.CloudProvider { return m.provider }
func (m *nasMetricCloudAdapter) NAS() cloudx.NASAdapter            { return m.nas }

var _ cloudx.CloudAdapter = (*nasMetricCloudAdapter)(nil)

// 测试用厂商键(全局注册表,用独特前缀避免与其他用例冲突)
const (
	testNASProviderWithMetrics    = domain.CloudProvider("nasmetric-yes")
	testNASProviderWithoutMetrics = domain.CloudProvider("nasmetric-no")
)

// testNASMetricSet 标准测试指标集(每次 CreateAdapter 现算日期):
// fs-a 今日行 + 昨日行;fs-zero-today 今日零容量行;fs-zero 昨日零容量行
func testNASMetricSet() []types.NASMetric {
	now := time.Now().In(nasMetricsCSTZone)
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	return []types.NASMetric{
		{FsID: "fs-a", FsName: "fs-a-name", Date: today, Capacity: 100, UsedCapacity: 10},
		{FsID: "fs-a", FsName: "fs-a-name", Date: yesterday, Capacity: 90, UsedCapacity: 9},
		{FsID: "fs-zero-today", FsName: "今日零容量fs", Date: today, Capacity: 0, UsedCapacity: 0},
		{FsID: "fs-zero", FsName: "零容量fs", Date: yesterday, Capacity: 0, UsedCapacity: 0},
	}
}

func init() {
	cloudx.RegisterAdapter(testNASProviderWithMetrics, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &nasMetricCloudAdapter{
			provider: testNASProviderWithMetrics,
			nas:      &metricCapableNAS{metrics: testNASMetricSet()},
		}, nil
	})
	cloudx.RegisterAdapter(testNASProviderWithoutMetrics, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &nasMetricCloudAdapter{
			provider: testNASProviderWithoutMetrics,
			nas:      &plainNAS{},
		}, nil
	})
}

func nasMetricTestAccount(id int64, provider domain.CloudProvider) domain.CloudAccount {
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

// nasTestInstance 构造 ecam_instance 形态的 NAS 实例(region 在 attributes)
func nasTestInstance(accountID int64, fsID, name, region string) camdomain.Instance {
	return camdomain.Instance{
		ModelUID:   "test_nas",
		AssetID:    fsID,
		AssetName:  name,
		TenantID:   1,
		AccountID:  accountID,
		Attributes: map[string]interface{}{"region": region},
	}
}

func newTestNASMetricsExecutor(
	t *testing.T,
	accounts []domain.CloudAccount,
	instances map[int64][]camdomain.Instance,
) (*SyncNASMetricsExecutor, *nasMetricDAOMock) {
	t.Helper()
	daoMock := &nasMetricDAOMock{}
	e := NewSyncNASMetricsExecutor(
		&cdnMetricAccountRepo{accounts: accounts},
		&nasInstanceRepoMock{byAccount: instances},
		daoMock,
		&cdnMetricTaskRepo{},
		testLogger(),
	)
	return e, daoMock
}

// ==================== 用例 ====================

// 活跃账号筛选:ecam_instance 中无 NAS 实例的账号不采集,有实例的账号正常采集
func TestSyncNASMetrics_ActiveAccountFiltering(t *testing.T) {
	acc1 := nasMetricTestAccount(1, testNASProviderWithMetrics)
	acc2 := nasMetricTestAccount(2, testNASProviderWithMetrics)
	e, daoMock := newTestNASMetricsExecutor(t,
		[]domain.CloudAccount{acc1, acc2},
		map[int64][]camdomain.Instance{
			1: {nasTestInstance(1, "fs-a", "fs-a-name", "cn-hangzhou")},
			// 账号 2 无任何 NAS 实例 → 非活跃账号
		},
	)
	task := &taskx.Task{ID: "t1", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	// 仅账号 1 被采集:1 个实例 × 4 条指标(fs-a 今日/昨日 + 两个零容量行)
	written := len(daoMock.insertsIfAbsent) + len(daoMock.upserts)
	if written != 4 {
		t.Fatalf("written = %d, want 4 (无实例账号不得产生写入)", written)
	}
	for _, m := range daoMock.insertsIfAbsent {
		if m.AccountID != 1 {
			t.Fatalf("无 NAS 实例的账号 2 被采集: %+v", m)
		}
	}
	withoutNAS, _ := task.Result["accounts_without_nas"].([]string)
	if len(withoutNAS) != 1 || withoutNAS[0] != acc2.Name {
		t.Fatalf("accounts_without_nas = %v, want [%s]", withoutNAS, acc2.Name)
	}
}

// 首写生效 vs 昨日覆盖:今日行走 insert-if-absent(首写保护),昨日行走覆盖 upsert
func TestSyncNASMetrics_FirstWriteVsYesterdayOverwrite(t *testing.T) {
	e, daoMock := newTestNASMetricsExecutor(t,
		[]domain.CloudAccount{nasMetricTestAccount(3, testNASProviderWithMetrics)},
		map[int64][]camdomain.Instance{
			3: {nasTestInstance(3, "fs-a", "fs-a-name", "cn-hangzhou")},
		},
	)
	task := &taskx.Task{ID: "t2", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	today := time.Now().In(nasMetricsCSTZone).Format("2006-01-02")
	yesterday := time.Now().In(nasMetricsCSTZone).AddDate(0, 0, -1).Format("2006-01-02")

	// 今日行(fs-a + fs-zero-today)全部走首写生效批
	if len(daoMock.insertsIfAbsent) != 2 {
		t.Fatalf("insert-if-absent rows = %d, want 2: %+v", len(daoMock.insertsIfAbsent), daoMock.insertsIfAbsent)
	}
	for _, m := range daoMock.insertsIfAbsent {
		if m.Date != today {
			t.Fatalf("今日行未走首写生效: date=%s (want %s)", m.Date, today)
		}
		if m.AccountID != 3 || m.Provider != string(testNASProviderWithMetrics) {
			t.Fatalf("metric not enriched: %+v", m)
		}
	}
	// 昨日行(fs-a + fs-zero)全部走覆盖 upsert 批
	if len(daoMock.upserts) != 2 {
		t.Fatalf("upsert rows = %d, want 2: %+v", len(daoMock.upserts), daoMock.upserts)
	}
	for _, m := range daoMock.upserts {
		if m.Date != yesterday {
			t.Fatalf("昨日行未走覆盖更新: date=%s (want %s)", m.Date, yesterday)
		}
	}
	if got := task.Result["metrics_total"]; got != 4 {
		t.Fatalf("metrics_total = %v, want 4", got)
	}
}

// 不继承 CDN「全零跳过」过滤:capacity=0 异常行照常入批落库(首写路不丢)
func TestSyncNASMetrics_ZeroCapacityRowVisible(t *testing.T) {
	e, daoMock := newTestNASMetricsExecutor(t,
		[]domain.CloudAccount{nasMetricTestAccount(4, testNASProviderWithMetrics)},
		map[int64][]camdomain.Instance{
			4: {nasTestInstance(4, "fs-zero", "零容量fs", "cn-hangzhou")},
		},
	)
	task := &taskx.Task{ID: "t3", Params: map[string]any{"days": 1}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	// days=1 区间只有今日:fs-a(容量 100)+ fs-zero-today(容量 0)都走首写批
	if len(daoMock.insertsIfAbsent) != 2 {
		t.Fatalf("insert-if-absent rows = %d, want 2: %+v", len(daoMock.insertsIfAbsent), daoMock.insertsIfAbsent)
	}
	if len(daoMock.upserts) != 0 {
		t.Fatalf("upsert rows = %d, want 0 (days=1 区间无昨日行)", len(daoMock.upserts))
	}
	var zeroRow *types.NASMetric
	for i := range daoMock.insertsIfAbsent {
		row := daoMock.insertsIfAbsent[i]
		if row.Capacity == 0 {
			zeroRow = &daoMock.insertsIfAbsent[i]
		}
	}
	if zeroRow == nil {
		t.Fatalf("零容量行被过滤,批内: %+v", daoMock.insertsIfAbsent)
	}
	if zeroRow.FsID != "fs-zero-today" || zeroRow.UsedCapacity != 0 {
		t.Fatalf("零容量行被篡改: %+v", *zeroRow)
	}
	if got := task.Result["metrics_total"]; got != 2 {
		t.Fatalf("metrics_total = %v, want 2", got)
	}
}

// 账号级互斥:同账号并发采集只放行一个
func TestSyncNASMetrics_AccountMutex(t *testing.T) {
	e, _ := newTestNASMetricsExecutor(t, nil, nil)
	if !e.tryAcquireAccount(1, "task-a") {
		t.Fatal("first acquire should succeed")
	}
	if e.tryAcquireAccount(1, "task-b") {
		t.Fatal("second acquire should fail")
	}
	if !e.tryAcquireAccount(1, "task-a") {
		t.Fatal("re-entrant acquire by owner should succeed")
	}
	e.releaseAccount(1, "task-a")
	if !e.tryAcquireAccount(1, "task-c") {
		t.Fatal("acquire after release should succeed")
	}
	e.releaseAccount(1, "task-c")
	// 释放非持有者不误删
	e.tryAcquireAccount(2, "task-d")
	e.releaseAccount(2, "task-e")
	if _, busy := e.syncingNow[2]; !busy {
		t.Fatal("release by non-owner should not clear owner")
	}
}

// 跳过不实现 NASMetricQuerier 的厂商:不写入、不报错,Result 可见
func TestSyncNASMetrics_SkipsProviderWithoutMetricSupport(t *testing.T) {
	e, daoMock := newTestNASMetricsExecutor(t,
		[]domain.CloudAccount{nasMetricTestAccount(5, testNASProviderWithoutMetrics)},
		map[int64][]camdomain.Instance{
			5: {nasTestInstance(5, "fs-x", "fs-x", "cn-hangzhou")},
		},
	)
	task := &taskx.Task{ID: "t4", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(daoMock.insertsIfAbsent)+len(daoMock.upserts) != 0 {
		t.Fatalf("无指标支持厂商不得产生写入")
	}
	skipped, _ := task.Result["no_metric_support"].([]string)
	if len(skipped) != 1 || skipped[0] != string(testNASProviderWithoutMetrics) {
		t.Fatalf("no_metric_support = %v", task.Result["no_metric_support"])
	}
}

// region 与采集区间透传:GetNASMetrics 收到实例所在 region 与 [start, end]
func TestSyncNASMetrics_RegionAndDateRangePassthrough(t *testing.T) {
	e, _ := newTestNASMetricsExecutor(t,
		[]domain.CloudAccount{nasMetricTestAccount(6, testNASProviderWithMetrics)},
		map[int64][]camdomain.Instance{
			6: {nasTestInstance(6, "fs-a", "fs-a-name", "cn-hangzhou")},
		},
	)
	account := nasMetricTestAccount(6, testNASProviderWithMetrics)
	if _, err := e.collectAccount(context.Background(), &account, "2026-09-13", "2026-09-19"); err != nil {
		t.Fatalf("collectAccount: %v", err)
	}
	adapter, _ := e.cloudxFactory.CreateAdapter(&account)
	nas := adapter.NAS().(*metricCapableNAS)
	if nas.lastFsID != "fs-a" || nas.lastFsName != "fs-a-name" {
		t.Fatalf("fs 透传 = %s/%s", nas.lastFsID, nas.lastFsName)
	}
	if nas.lastRegion != "cn-hangzhou" {
		t.Fatalf("region = %s, want cn-hangzhou (实例真实 region)", nas.lastRegion)
	}
	if nas.lastStart != "2026-09-13" || nas.lastEnd != "2026-09-19" {
		t.Fatalf("date range = %s ~ %s", nas.lastStart, nas.lastEnd)
	}
}

// 无活跃账号:不报错,空跑结束
func TestSyncNASMetrics_NoAccounts(t *testing.T) {
	e, daoMock := newTestNASMetricsExecutor(t, nil, nil)
	task := &taskx.Task{ID: "t5", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(daoMock.insertsIfAbsent)+len(daoMock.upserts) != 0 {
		t.Fatal("无账号不得产生写入")
	}
}

// days 参数边界:默认 2 天([昨日, 今日])、上限收敛、区间按运营时区
func TestSyncNASMetrics_DaysBounds(t *testing.T) {
	if nasCollectDefaultDays != 2 {
		t.Fatalf("nasCollectDefaultDays = %d, want 2 ([昨日,今日] 采集窗口)", nasCollectDefaultDays)
	}
	if nasCollectMaxDays != 31 {
		t.Fatalf("nasCollectMaxDays = %d", nasCollectMaxDays)
	}
	today := time.Now().In(nasMetricsCSTZone).Format("2006-01-02")
	yesterday := time.Now().In(nasMetricsCSTZone).AddDate(0, 0, -1).Format("2006-01-02")
	start, end := nasMetricsDateRange(nasCollectDefaultDays)
	if start != yesterday || end != today {
		t.Fatalf("nasMetricsDateRange(2) = %s ~ %s, want %s ~ %s", start, end, yesterday, today)
	}
	monthAgo := time.Now().In(nasMetricsCSTZone).AddDate(0, 0, -30).Format("2006-01-02")
	start, end = nasMetricsDateRange(31)
	if start != monthAgo || end != today {
		t.Fatalf("nasMetricsDateRange(31) = %s ~ %s, want %s ~ %s", start, end, monthAgo, today)
	}
}
