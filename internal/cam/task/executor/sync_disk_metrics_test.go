package executor

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
)

// ==================== 测试用 mock ====================

// diskMetricDAOMock 指标 DAO mock(分别记录首写批与覆盖批,校验日期分流)
type diskMetricDAOMock struct {
	dao.DiskMetricDAO
	insertsIfAbsent   []types.DiskMetric
	upserts           []types.DiskMetric
	insertIfAbsentErr error
	bulkUpsertErr     error
}

func (m *diskMetricDAOMock) BulkInsertIfAbsent(_ context.Context, metrics []types.DiskMetric) error {
	if m.insertIfAbsentErr != nil {
		return m.insertIfAbsentErr
	}
	m.insertsIfAbsent = append(m.insertsIfAbsent, metrics...)
	return nil
}

func (m *diskMetricDAOMock) BulkUpsertMetrics(_ context.Context, metrics []types.DiskMetric) error {
	if m.bulkUpsertErr != nil {
		return m.bulkUpsertErr
	}
	m.upserts = append(m.upserts, metrics...)
	return nil
}

// metricCapableDisk 支持指标查询的 Disk 适配器 mock(DiskAdapter + DiskMetricQuerier)
type metricCapableDisk struct {
	metrics []types.DiskMetric
	err     error
	// 调用参数记录(区间/region 透传断言用;Disk 是地域性资源,querier 签名带 region)
	lastDiskID string
	lastRegion string
	lastStart  string
	lastEnd    string
}

func (d *metricCapableDisk) ListInstances(context.Context, string) ([]types.DiskInstance, error) {
	return nil, nil
}
func (d *metricCapableDisk) GetInstance(context.Context, string, string) (*types.DiskInstance, error) {
	return nil, nil
}
func (d *metricCapableDisk) ListInstancesByIDs(context.Context, string, []string) ([]types.DiskInstance, error) {
	return nil, nil
}
func (d *metricCapableDisk) GetInstanceStatus(context.Context, string, string) (string, error) {
	return "", nil
}
func (d *metricCapableDisk) ListInstancesWithFilter(context.Context, string, *types.DiskFilter) ([]types.DiskInstance, error) {
	return nil, nil
}
func (d *metricCapableDisk) ListByInstanceID(context.Context, string, string) ([]types.DiskInstance, error) {
	return nil, nil
}
func (d *metricCapableDisk) GetDiskMetrics(_ context.Context, diskID, _ string, region, startDate, endDate string) ([]types.DiskMetric, error) {
	if d.err != nil {
		return nil, d.err
	}
	d.lastDiskID, d.lastRegion, d.lastStart, d.lastEnd = diskID, region, startDate, endDate
	out := make([]types.DiskMetric, 0, len(d.metrics))
	for _, m := range d.metrics {
		if m.Date < startDate || m.Date > endDate {
			continue // 只返回请求区间内的日值(与真实厂商语义一致)
		}
		out = append(out, m)
	}
	return out, nil
}

// plainDisk 不支持指标查询的 Disk 适配器 mock(未实现 DiskMetricQuerier,
// 注意不得内嵌 metricCapableDisk,否则提升的方法集会使其"实现"接口)
type plainDisk struct{}

func (d *plainDisk) ListInstances(context.Context, string) ([]types.DiskInstance, error) {
	return nil, nil
}
func (d *plainDisk) GetInstance(context.Context, string, string) (*types.DiskInstance, error) {
	return nil, nil
}
func (d *plainDisk) ListInstancesByIDs(context.Context, string, []string) ([]types.DiskInstance, error) {
	return nil, nil
}
func (d *plainDisk) GetInstanceStatus(context.Context, string, string) (string, error) {
	return "", nil
}
func (d *plainDisk) ListInstancesWithFilter(context.Context, string, *types.DiskFilter) ([]types.DiskInstance, error) {
	return nil, nil
}
func (d *plainDisk) ListByInstanceID(context.Context, string, string) ([]types.DiskInstance, error) {
	return nil, nil
}

var _ cloudx.DiskAdapter = (*plainDisk)(nil)
var _ cloudx.DiskAdapter = (*metricCapableDisk)(nil)
var _ cloudx.DiskMetricQuerier = (*metricCapableDisk)(nil)

// diskMetricCloudAdapter 支持/不支持指标的 CloudAdapter mock(仅 Disk() 生效)
type diskMetricCloudAdapter struct {
	cloudx.CloudAdapter
	provider domain.CloudProvider
	disk     cloudx.DiskAdapter
}

func (m *diskMetricCloudAdapter) GetProvider() domain.CloudProvider { return m.provider }
func (m *diskMetricCloudAdapter) Disk() cloudx.DiskAdapter          { return m.disk }

var _ cloudx.CloudAdapter = (*diskMetricCloudAdapter)(nil)

// 测试用厂商键(全局注册表,用独特前缀避免与其他用例冲突)
const (
	testDiskProviderWithMetrics    = domain.CloudProvider("diskmetric-yes")
	testDiskProviderWithoutMetrics = domain.CloudProvider("diskmetric-no")
	testDiskProviderFailing        = domain.CloudProvider("diskmetric-fail")
	testDiskProviderEmpty          = domain.CloudProvider("diskmetric-empty")
	testDiskProviderNoDisk         = domain.CloudProvider("diskmetric-nodisk")
)

// testDiskMetricSet 标准测试指标集(每次 CreateAdapter 现算日期):
// disk-a 今日行 + 昨日行(带 usage_scope 口径标注);disk-zero-today 今日零使用率行
func testDiskMetricSet() []types.DiskMetric {
	now := time.Now().In(nasMetricsCSTZone)
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	return []types.DiskMetric{
		{DiskID: "disk-a", DiskName: "disk-a", Date: today, UsagePercent: 42.5, UsageScope: types.DiskUsageScopeInstanceLevel, IOPS: 120, Throughput: 3.5},
		{DiskID: "disk-a", DiskName: "disk-a", Date: yesterday, UsagePercent: 41, UsageScope: types.DiskUsageScopeInstanceLevel, IOPS: 110, Throughput: 3.2},
		{DiskID: "disk-zero-today", DiskName: "disk-zero-today", Date: today, UsagePercent: 0, UsageScope: types.DiskUsageScopeBusyShare},
	}
}

func init() {
	cloudx.RegisterAdapter(testDiskProviderWithMetrics, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &diskMetricCloudAdapter{
			provider: testDiskProviderWithMetrics,
			disk:     &metricCapableDisk{metrics: testDiskMetricSet()},
		}, nil
	})
	cloudx.RegisterAdapter(testDiskProviderWithoutMetrics, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &diskMetricCloudAdapter{
			provider: testDiskProviderWithoutMetrics,
			disk:     &plainDisk{},
		}, nil
	})
	cloudx.RegisterAdapter(testDiskProviderFailing, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &diskMetricCloudAdapter{
			provider: testDiskProviderFailing,
			disk: &metricCapableDisk{
				err: errors.New("DescribeDiskMonitorData quota exceeded"),
			},
		}, nil
	})
	cloudx.RegisterAdapter(testDiskProviderEmpty, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &diskMetricCloudAdapter{
			provider: testDiskProviderEmpty,
			disk:     &metricCapableDisk{}, // 无指标且无错误 = 真实无数据
		}, nil
	})
	cloudx.RegisterAdapter(testDiskProviderNoDisk, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &diskMetricCloudAdapter{
			provider: testDiskProviderNoDisk,
			disk:     nil, // Disk 适配器不可用(整账号失败路径)
		}, nil
	})
}

func diskMetricTestAccount(id int64, provider domain.CloudProvider) domain.CloudAccount {
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

// diskTestInstance 构造 ecam_instance 形态的 Disk 实例(sync_disk.go 落库形态:
// AssetID=disk_id,AssetName=disk_name,attributes 含 region)
func diskTestInstance(accountID int64, diskID, region string) camdomain.Instance {
	return camdomain.Instance{
		ModelUID:  "test_disk",
		AssetID:   diskID,
		AssetName: diskID,
		TenantID:  1,
		AccountID: accountID,
		Attributes: map[string]interface{}{
			"region": region,
		},
	}
}

func newTestDiskMetricsExecutor(
	t *testing.T,
	accounts []domain.CloudAccount,
	instances map[int64][]camdomain.Instance,
) (*SyncDiskMetricsExecutor, *diskMetricDAOMock) {
	t.Helper()
	daoMock := &diskMetricDAOMock{}
	e := NewSyncDiskMetricsExecutor(
		&cdnMetricAccountRepo{accounts: accounts},
		&nasInstanceRepoMock{byAccount: instances},
		daoMock,
		&cdnMetricTaskRepo{},
		testLogger(),
	)
	return e, daoMock
}

// ==================== 用例 ====================

// 活跃账号筛选:ecam_instance 中无 Disk 实例的账号不采集,有 Disk 的账号正常采集
// (Hard Rule:活跃账号口径 = 存在 ≥1 个 Disk 实例,不依赖 EnableAutoSync)
func TestSyncDiskMetrics_ActiveAccountFiltering(t *testing.T) {
	acc1 := diskMetricTestAccount(1, testDiskProviderWithMetrics)
	acc2 := diskMetricTestAccount(2, testDiskProviderWithMetrics)
	e, daoMock := newTestDiskMetricsExecutor(t,
		[]domain.CloudAccount{acc1, acc2},
		map[int64][]camdomain.Instance{
			1: {diskTestInstance(1, "disk-a", "cn-hangzhou")},
			// 账号 2 无任何 Disk 实例 → 非活跃账号
		},
	)
	task := &taskx.Task{ID: "t1", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	// 仅账号 1 被采集:1 个 disk × 3 条指标(disk-a 今日/昨日 + disk-zero-today)
	written := len(daoMock.insertsIfAbsent) + len(daoMock.upserts)
	if written != 3 {
		t.Fatalf("written = %d, want 3 (无 Disk 实例账号不得产生写入)", written)
	}
	for _, m := range daoMock.insertsIfAbsent {
		if m.AccountID != 1 {
			t.Fatalf("无 Disk 实例的账号 2 被采集: %+v", m)
		}
	}
	withoutDisk, _ := task.Result["accounts_without_disk"].([]string)
	if len(withoutDisk) != 1 || withoutDisk[0] != acc2.Name {
		t.Fatalf("accounts_without_disk = %v, want [%s]", withoutDisk, acc2.Name)
	}
}

// 首写生效 vs 昨日覆盖:今日行走 insert-if-absent(首写保护),昨日行走覆盖 upsert;
// AccountID/Provider 由执行器回填(querier 签名不感知账号)
func TestSyncDiskMetrics_FirstWriteVsYesterdayOverwrite(t *testing.T) {
	e, daoMock := newTestDiskMetricsExecutor(t,
		[]domain.CloudAccount{diskMetricTestAccount(3, testDiskProviderWithMetrics)},
		map[int64][]camdomain.Instance{
			3: {diskTestInstance(3, "disk-a", "cn-hangzhou")},
		},
	)
	task := &taskx.Task{ID: "t2", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	today := time.Now().In(nasMetricsCSTZone).Format("2006-01-02")
	yesterday := time.Now().In(nasMetricsCSTZone).AddDate(0, 0, -1).Format("2006-01-02")

	// 今日行(disk-a + disk-zero-today)全部走首写生效批
	if len(daoMock.insertsIfAbsent) != 2 {
		t.Fatalf("insert-if-absent rows = %d, want 2: %+v", len(daoMock.insertsIfAbsent), daoMock.insertsIfAbsent)
	}
	for _, m := range daoMock.insertsIfAbsent {
		if m.Date != today {
			t.Fatalf("今日行未走首写生效: date=%s (want %s)", m.Date, today)
		}
		if m.AccountID != 3 || m.Provider != string(testDiskProviderWithMetrics) {
			t.Fatalf("metric not enriched: %+v", m)
		}
	}
	// 昨日行(disk-a)全部走覆盖 upsert 批
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

// usage_scope 口径标注透传(T1 探测定案:五厂商无云盘级容量使用率,必达三家为
// instance_level/busy_share):执行器原样透传适配器标注,不篡改不改写
func TestSyncDiskMetrics_UsageScopePassthrough(t *testing.T) {
	e, daoMock := newTestDiskMetricsExecutor(t,
		[]domain.CloudAccount{diskMetricTestAccount(11, testDiskProviderWithMetrics)},
		map[int64][]camdomain.Instance{
			11: {diskTestInstance(11, "disk-a", "cn-hangzhou")},
		},
	)
	if err := e.Execute(context.Background(), &taskx.Task{ID: "t11", Params: map[string]any{}}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, m := range append(daoMock.insertsIfAbsent, daoMock.upserts...) {
		switch m.DiskID {
		case "disk-a":
			if m.UsageScope != types.DiskUsageScopeInstanceLevel {
				t.Fatalf("usage_scope 被篡改: disk_id=%s usage_scope=%q", m.DiskID, m.UsageScope)
			}
		case "disk-zero-today":
			if m.UsageScope != types.DiskUsageScopeBusyShare {
				t.Fatalf("usage_scope 被篡改: disk_id=%s usage_scope=%q", m.DiskID, m.UsageScope)
			}
		}
	}
}

// 不继承 CDN「全零跳过」过滤:usage_percent=0 异常行照常入批落库(qc_status 打标
// zero_exception 由 DAO 写路径落实,执行器透传适配器标注不篡改)
func TestSyncDiskMetrics_ZeroUsageRowVisible(t *testing.T) {
	e, daoMock := newTestDiskMetricsExecutor(t,
		[]domain.CloudAccount{diskMetricTestAccount(4, testDiskProviderWithMetrics)},
		map[int64][]camdomain.Instance{
			4: {diskTestInstance(4, "disk-zero-today", "cn-hangzhou")},
		},
	)
	task := &taskx.Task{ID: "t3", Params: map[string]any{"days": 1}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	// days=1 区间只有今日:mock querier 返回区间内全部日值(disk-a + disk-zero-today),
	// 零使用率行不得被 CDN 式「全零跳过」过滤掉
	if len(daoMock.insertsIfAbsent) != 2 {
		t.Fatalf("insert-if-absent rows = %d, want 2: %+v", len(daoMock.insertsIfAbsent), daoMock.insertsIfAbsent)
	}
	if len(daoMock.upserts) != 0 {
		t.Fatalf("upsert rows = %d, want 0 (days=1 区间无昨日行)", len(daoMock.upserts))
	}
	var zeroRow *types.DiskMetric
	for i := range daoMock.insertsIfAbsent {
		row := daoMock.insertsIfAbsent[i]
		if row.UsagePercent == 0 {
			zeroRow = &daoMock.insertsIfAbsent[i]
		}
	}
	if zeroRow == nil {
		t.Fatalf("零使用率行被过滤,批内: %+v", daoMock.insertsIfAbsent)
	}
	if zeroRow.DiskID != "disk-zero-today" || zeroRow.UsageScope != types.DiskUsageScopeBusyShare {
		t.Fatalf("零使用率行被篡改: %+v", *zeroRow)
	}
	if got := task.Result["metrics_total"]; got != 2 {
		t.Fatalf("metrics_total = %v, want 2", got)
	}
}

// region 透传(Disk 是地域性资源,querier 签名带 region):按实例 attributes["region"]
// 逐盘查询,不做全局 region 推断
func TestSyncDiskMetrics_RegionPassthrough(t *testing.T) {
	e, _ := newTestDiskMetricsExecutor(t,
		[]domain.CloudAccount{diskMetricTestAccount(12, testDiskProviderWithMetrics)},
		map[int64][]camdomain.Instance{
			12: {diskTestInstance(12, "disk-a", "ap-southeast-1")},
		},
	)
	if err := e.Execute(context.Background(), &taskx.Task{ID: "t12", Params: map[string]any{}}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	acc := diskMetricTestAccount(12, testDiskProviderWithMetrics)
	adapter, err := e.cloudxFactory.CreateAdapter(&acc)
	if err != nil {
		t.Fatalf("create adapter: %v", err)
	}
	q := adapter.Disk().(*metricCapableDisk)
	if q.lastDiskID != "disk-a" {
		t.Fatalf("disk 透传 = %s, want disk-a", q.lastDiskID)
	}
	if q.lastRegion != "ap-southeast-1" {
		t.Fatalf("region 透传 = %s, want ap-southeast-1", q.lastRegion)
	}
}

// 失败计数:querier 报错计入 Result["failures"](provider/account/error_count/last_error),
// 单 disk 失败不中断其余 disk
func TestSyncDiskMetrics_FailureCounting(t *testing.T) {
	e, daoMock := newTestDiskMetricsExecutor(t,
		[]domain.CloudAccount{diskMetricTestAccount(5, testDiskProviderFailing)},
		map[int64][]camdomain.Instance{
			5: {diskTestInstance(5, "disk-a", "cn-hangzhou")},
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
	if f.Provider != string(testDiskProviderFailing) || f.AccountID != 5 {
		t.Fatalf("failure 归因错误: %+v", f)
	}
	if f.ErrorCount < 1 || f.LastError == "" {
		t.Fatalf("failure 计数/末次错误缺失: %+v", f)
	}
	if got := task.Result["failed_disks"]; got != 1 {
		t.Fatalf("failed_disks = %v, want 1", got)
	}
}

// 真实无数据(空结果)不计失败:failures 为空,任务正常结束
func TestSyncDiskMetrics_EmptyResultNotFailure(t *testing.T) {
	e, daoMock := newTestDiskMetricsExecutor(t,
		[]domain.CloudAccount{diskMetricTestAccount(6, testDiskProviderEmpty)},
		map[int64][]camdomain.Instance{
			6: {diskTestInstance(6, "disk-empty", "cn-hangzhou")},
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

// 跳过不实现 DiskMetricQuerier 的厂商:不写入、不报错,Result 可见(探测不支持不计失败)
func TestSyncDiskMetrics_SkipsProviderWithoutMetricSupport(t *testing.T) {
	e, daoMock := newTestDiskMetricsExecutor(t,
		[]domain.CloudAccount{diskMetricTestAccount(7, testDiskProviderWithoutMetrics)},
		map[int64][]camdomain.Instance{
			7: {diskTestInstance(7, "disk-x", "cn-hangzhou")},
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
	if len(skipped) != 1 || skipped[0] != string(testDiskProviderWithoutMetrics) {
		t.Fatalf("no_metric_support = %v", task.Result["no_metric_support"])
	}
	failures, _ := task.Result["failures"].([]nasProviderFailure)
	if len(failures) != 0 {
		t.Fatalf("探测不支持不得计入失败: %+v", failures)
	}
}

// 账号级互斥:复用共享账号互斥闸(nasAccountGate),同账号并发采集只放行一个
func TestSyncDiskMetrics_AccountMutex(t *testing.T) {
	e, _ := newTestDiskMetricsExecutor(t, nil, nil)
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
}

// 采集区间透传与 31 天补采窗口:GetDiskMetrics 收到 [start, end];days 超限收敛到 31
func TestSyncDiskMetrics_DateRangeAndDaysBounds(t *testing.T) {
	if diskCollectDefaultDays != 2 {
		t.Fatalf("diskCollectDefaultDays = %d, want 2 ([昨日,今日] 采集窗口)", diskCollectDefaultDays)
	}
	if diskCollectMaxDays != 31 {
		t.Fatalf("diskCollectMaxDays = %d, want 31 (补采窗口上限)", diskCollectMaxDays)
	}
	e, _ := newTestDiskMetricsExecutor(t,
		[]domain.CloudAccount{diskMetricTestAccount(8, testDiskProviderWithMetrics)},
		map[int64][]camdomain.Instance{
			8: {diskTestInstance(8, "disk-a", "cn-hangzhou")},
		},
	)
	// days=999 → 收敛到 31 天窗口
	task := &taskx.Task{ID: "t7", Params: map[string]any{"days": 999}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	acc := diskMetricTestAccount(8, testDiskProviderWithMetrics)
	adapter, err := e.cloudxFactory.CreateAdapter(&acc)
	if err != nil {
		t.Fatalf("create adapter: %v", err)
	}
	q := adapter.Disk().(*metricCapableDisk)
	today := time.Now().In(nasMetricsCSTZone).Format("2006-01-02")
	monthAgo := time.Now().In(nasMetricsCSTZone).AddDate(0, 0, -30).Format("2006-01-02")
	if q.lastStart != monthAgo || q.lastEnd != today {
		t.Fatalf("date range = %s ~ %s, want %s ~ %s (31 天窗口收敛)", q.lastStart, q.lastEnd, monthAgo, today)
	}
}

// Disk 适配器不可用:计入失败(Disk适配器不可用属 ERROR 语义),不产生写入
func TestSyncDiskMetrics_UnavailableDiskAdapter(t *testing.T) {
	e, daoMock := newTestDiskMetricsExecutor(t,
		[]domain.CloudAccount{diskMetricTestAccount(9, testDiskProviderNoDisk)},
		map[int64][]camdomain.Instance{
			9: {diskTestInstance(9, "disk-a", "cn-hangzhou")},
		},
	)
	task := &taskx.Task{ID: "t9", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(daoMock.insertsIfAbsent)+len(daoMock.upserts) != 0 {
		t.Fatal("适配器不可用不得产生写入")
	}
	failures, _ := task.Result["failures"].([]nasProviderFailure)
	if len(failures) != 1 {
		t.Fatalf("适配器不可用须计入失败: %+v", task.Result["failures"])
	}
}

// 写库失败:计入失败明细与 failed_disks,不误报成功
func TestSyncDiskMetrics_DAOWriteFailure(t *testing.T) {
	e, daoMock := newTestDiskMetricsExecutor(t,
		[]domain.CloudAccount{diskMetricTestAccount(10, testDiskProviderWithMetrics)},
		map[int64][]camdomain.Instance{
			10: {diskTestInstance(10, "disk-a", "cn-hangzhou")},
		},
	)
	daoMock.insertIfAbsentErr = errors.New("bulk write timeout")
	task := &taskx.Task{ID: "t10", Params: map[string]any{"days": 1}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	failures, _ := task.Result["failures"].([]nasProviderFailure)
	if len(failures) != 1 || failures[0].LastError != "bulk write timeout" {
		t.Fatalf("写库失败须计入 failures: %+v", task.Result["failures"])
	}
	if got := task.Result["failed_disks"]; got != 1 {
		t.Fatalf("failed_disks = %v, want 1", got)
	}
	if got := task.Result["metrics_total"]; got != 0 {
		t.Fatalf("写库失败不得计入 metrics_total: %v", got)
	}
}

// 任务类型注册正确:disk:collect_metrics 可被调度器识别提交
func TestSyncDiskMetrics_GetType(t *testing.T) {
	e, _ := newTestDiskMetricsExecutor(t, nil, nil)
	if got := e.GetType(); got != taskx.TaskType("disk:collect_metrics") {
		t.Fatalf("GetType = %q, want disk:collect_metrics", got)
	}
}

// 无活跃账号:不报错,空跑结束
func TestSyncDiskMetrics_NoAccounts(t *testing.T) {
	e, daoMock := newTestDiskMetricsExecutor(t, nil, nil)
	task := &taskx.Task{ID: "t8", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(daoMock.insertsIfAbsent)+len(daoMock.upserts) != 0 {
		t.Fatal("无账号不得产生写入")
	}
}

// diskMetricTestAccountPtr 取指针形态账号(CreateAdapter 断言用)
func diskMetricTestAccountPtr(id int64, provider domain.CloudProvider) *domain.CloudAccount {
	acc := diskMetricTestAccount(id, provider)
	return &acc
}
