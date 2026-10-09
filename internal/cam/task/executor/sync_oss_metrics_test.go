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

// ossMetricDAOMock 指标 DAO mock(分别记录首写批与覆盖批,校验日期分流)
type ossMetricDAOMock struct {
	dao.OSSMetricDAO
	insertsIfAbsent   []types.OSSMetric
	upserts           []types.OSSMetric
	insertIfAbsentErr error
	bulkUpsertErr     error
}

func (m *ossMetricDAOMock) BulkInsertIfAbsent(_ context.Context, metrics []types.OSSMetric) error {
	if m.insertIfAbsentErr != nil {
		return m.insertIfAbsentErr
	}
	m.insertsIfAbsent = append(m.insertsIfAbsent, metrics...)
	return nil
}

func (m *ossMetricDAOMock) BulkUpsertMetrics(_ context.Context, metrics []types.OSSMetric) error {
	if m.bulkUpsertErr != nil {
		return m.bulkUpsertErr
	}
	m.upserts = append(m.upserts, metrics...)
	return nil
}

// metricCapableOSS 支持指标查询的 OSS 适配器 mock(OSSAdapter + OSSMetricQuerier)
type metricCapableOSS struct {
	metrics []types.OSSMetric
	err     error
	// 调用参数记录(区间透传断言用;OSS querier 无 region 签名)
	lastBucket string
	lastStart  string
	lastEnd    string
}

func (o *metricCapableOSS) ListBuckets(context.Context, string) ([]types.OSSBucket, error) {
	return nil, nil
}
func (o *metricCapableOSS) GetBucket(context.Context, string) (*types.OSSBucket, error) {
	return nil, nil
}
func (o *metricCapableOSS) GetBucketStats(context.Context, string) (*types.OSSBucketStats, error) {
	return nil, nil
}
func (o *metricCapableOSS) ListBucketsWithFilter(context.Context, string, *types.OSSBucketFilter) ([]types.OSSBucket, error) {
	return nil, nil
}
func (o *metricCapableOSS) GetOSSMetrics(_ context.Context, bucketName, startDate, endDate string) ([]types.OSSMetric, error) {
	if o.err != nil {
		return nil, o.err
	}
	o.lastBucket, o.lastStart, o.lastEnd = bucketName, startDate, endDate
	out := make([]types.OSSMetric, 0, len(o.metrics))
	for _, m := range o.metrics {
		if m.Date < startDate || m.Date > endDate {
			continue // 只返回请求区间内的日值(与真实厂商语义一致)
		}
		out = append(out, m)
	}
	return out, nil
}

// plainOSS 不支持指标查询的 OSS 适配器 mock(未实现 OSSMetricQuerier,
// 注意不得内嵌 metricCapableOSS,否则提升的方法集会使其"实现"接口)
type plainOSS struct{}

func (o *plainOSS) ListBuckets(context.Context, string) ([]types.OSSBucket, error) {
	return nil, nil
}
func (o *plainOSS) GetBucket(context.Context, string) (*types.OSSBucket, error) {
	return nil, nil
}
func (o *plainOSS) GetBucketStats(context.Context, string) (*types.OSSBucketStats, error) {
	return nil, nil
}
func (o *plainOSS) ListBucketsWithFilter(context.Context, string, *types.OSSBucketFilter) ([]types.OSSBucket, error) {
	return nil, nil
}

var _ cloudx.OSSAdapter = (*plainOSS)(nil)
var _ cloudx.OSSAdapter = (*metricCapableOSS)(nil)
var _ cloudx.OSSMetricQuerier = (*metricCapableOSS)(nil)

// ossMetricCloudAdapter 支持/不支持指标的 CloudAdapter mock(仅 OSS() 生效)
type ossMetricCloudAdapter struct {
	cloudx.CloudAdapter
	provider domain.CloudProvider
	oss      cloudx.OSSAdapter
}

func (m *ossMetricCloudAdapter) GetProvider() domain.CloudProvider { return m.provider }
func (m *ossMetricCloudAdapter) OSS() cloudx.OSSAdapter            { return m.oss }

var _ cloudx.CloudAdapter = (*ossMetricCloudAdapter)(nil)

// 测试用厂商键(全局注册表,用独特前缀避免与其他用例冲突)
const (
	testOSSProviderWithMetrics    = domain.CloudProvider("ossmetric-yes")
	testOSSProviderWithoutMetrics = domain.CloudProvider("ossmetric-no")
	testOSSProviderFailing        = domain.CloudProvider("ossmetric-fail")
	testOSSProviderEmpty          = domain.CloudProvider("ossmetric-empty")
	testOSSProviderNoOSS          = domain.CloudProvider("ossmetric-nooss")
)

// testOSSMetricSet 标准测试指标集(每次 CreateAdapter 现算日期):
// bucket-a 今日行 + 昨日行;bucket-zero-today 今日零容量行;bucket-zero 昨日零容量行
func testOSSMetricSet() []types.OSSMetric {
	now := time.Now().In(nasMetricsCSTZone)
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	return []types.OSSMetric{
		{BucketName: "bucket-a", Date: today, StorageSize: 100, ObjectCount: 10},
		{BucketName: "bucket-a", Date: yesterday, StorageSize: 90, ObjectCount: 9},
		{BucketName: "bucket-zero-today", Date: today, StorageSize: 0, ObjectCount: 0},
		{BucketName: "bucket-zero", Date: yesterday, StorageSize: 0, ObjectCount: 0},
	}
}

func init() {
	cloudx.RegisterAdapter(testOSSProviderWithMetrics, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &ossMetricCloudAdapter{
			provider: testOSSProviderWithMetrics,
			oss:      &metricCapableOSS{metrics: testOSSMetricSet()},
		}, nil
	})
	cloudx.RegisterAdapter(testOSSProviderWithoutMetrics, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &ossMetricCloudAdapter{
			provider: testOSSProviderWithoutMetrics,
			oss:      &plainOSS{},
		}, nil
	})
	cloudx.RegisterAdapter(testOSSProviderFailing, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &ossMetricCloudAdapter{
			provider: testOSSProviderFailing,
			oss: &metricCapableOSS{
				err: errors.New("DescribeBucketStat quota exceeded"),
			},
		}, nil
	})
	cloudx.RegisterAdapter(testOSSProviderEmpty, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &ossMetricCloudAdapter{
			provider: testOSSProviderEmpty,
			oss:      &metricCapableOSS{}, // 无指标且无错误 = 真实无数据
		}, nil
	})
	cloudx.RegisterAdapter(testOSSProviderNoOSS, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &ossMetricCloudAdapter{
			provider: testOSSProviderNoOSS,
			oss:      nil, // OSS 适配器不可用(整账号失败路径)
		}, nil
	})
}

func ossMetricTestAccount(id int64, provider domain.CloudProvider) domain.CloudAccount {
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

// ossTestBucket 构造 ecam_instance 形态的 OSS 存储桶(sync_oss.go 落库形态:
// AssetID/AssetName 均为 bucket_name)
func ossTestBucket(accountID int64, bucketName string) camdomain.Instance {
	return camdomain.Instance{
		ModelUID:  "test_oss",
		AssetID:   bucketName,
		AssetName: bucketName,
		TenantID:  1,
		AccountID: accountID,
		Attributes: map[string]interface{}{
			"bucket_name": bucketName,
			"region":      "cn-hangzhou",
		},
	}
}

func newTestOSSMetricsExecutor(
	t *testing.T,
	accounts []domain.CloudAccount,
	instances map[int64][]camdomain.Instance,
) (*SyncOSSMetricsExecutor, *ossMetricDAOMock) {
	t.Helper()
	daoMock := &ossMetricDAOMock{}
	e := NewSyncOSSMetricsExecutor(
		&cdnMetricAccountRepo{accounts: accounts},
		&nasInstanceRepoMock{byAccount: instances},
		daoMock,
		&cdnMetricTaskRepo{},
		testLogger(),
	)
	return e, daoMock
}

// ==================== 用例 ====================

// 活跃账号筛选:ecam_instance 中无 OSS bucket 的账号不采集,有 bucket 的账号正常采集
// (Hard Rule:活跃账号口径 = 存在 ≥1 个 OSS bucket,不依赖 EnableAutoSync)
func TestSyncOSSMetrics_ActiveAccountFiltering(t *testing.T) {
	acc1 := ossMetricTestAccount(1, testOSSProviderWithMetrics)
	acc2 := ossMetricTestAccount(2, testOSSProviderWithMetrics)
	e, daoMock := newTestOSSMetricsExecutor(t,
		[]domain.CloudAccount{acc1, acc2},
		map[int64][]camdomain.Instance{
			1: {ossTestBucket(1, "bucket-a")},
			// 账号 2 无任何 OSS bucket → 非活跃账号
		},
	)
	task := &taskx.Task{ID: "t1", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	// 仅账号 1 被采集:1 个 bucket × 4 条指标(bucket-a 今日/昨日 + 两个零容量行)
	written := len(daoMock.insertsIfAbsent) + len(daoMock.upserts)
	if written != 4 {
		t.Fatalf("written = %d, want 4 (无 bucket 账号不得产生写入)", written)
	}
	for _, m := range daoMock.insertsIfAbsent {
		if m.AccountID != 1 {
			t.Fatalf("无 OSS bucket 的账号 2 被采集: %+v", m)
		}
	}
	withoutOSS, _ := task.Result["accounts_without_oss"].([]string)
	if len(withoutOSS) != 1 || withoutOSS[0] != acc2.Name {
		t.Fatalf("accounts_without_oss = %v, want [%s]", withoutOSS, acc2.Name)
	}
}

// 首写生效 vs 昨日覆盖:今日行走 insert-if-absent(首写保护),昨日行走覆盖 upsert;
// AccountID/Provider 由执行器回填(querier 签名不感知账号)
func TestSyncOSSMetrics_FirstWriteVsYesterdayOverwrite(t *testing.T) {
	e, daoMock := newTestOSSMetricsExecutor(t,
		[]domain.CloudAccount{ossMetricTestAccount(3, testOSSProviderWithMetrics)},
		map[int64][]camdomain.Instance{
			3: {ossTestBucket(3, "bucket-a")},
		},
	)
	task := &taskx.Task{ID: "t2", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	today := time.Now().In(nasMetricsCSTZone).Format("2006-01-02")
	yesterday := time.Now().In(nasMetricsCSTZone).AddDate(0, 0, -1).Format("2006-01-02")

	// 今日行(bucket-a + bucket-zero-today)全部走首写生效批
	if len(daoMock.insertsIfAbsent) != 2 {
		t.Fatalf("insert-if-absent rows = %d, want 2: %+v", len(daoMock.insertsIfAbsent), daoMock.insertsIfAbsent)
	}
	for _, m := range daoMock.insertsIfAbsent {
		if m.Date != today {
			t.Fatalf("今日行未走首写生效: date=%s (want %s)", m.Date, today)
		}
		if m.AccountID != 3 || m.Provider != string(testOSSProviderWithMetrics) {
			t.Fatalf("metric not enriched: %+v", m)
		}
	}
	// 昨日行(bucket-a + bucket-zero)全部走覆盖 upsert 批
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

// 不继承 CDN「全零跳过」过滤:storage_size=0 异常行照常入批落库(qc_status 打标
// 由 DAO 写路径落实,执行器透传适配器标注不篡改)
func TestSyncOSSMetrics_ZeroCapacityRowVisible(t *testing.T) {
	e, daoMock := newTestOSSMetricsExecutor(t,
		[]domain.CloudAccount{ossMetricTestAccount(4, testOSSProviderWithMetrics)},
		map[int64][]camdomain.Instance{
			4: {ossTestBucket(4, "bucket-zero-today")},
		},
	)
	task := &taskx.Task{ID: "t3", Params: map[string]any{"days": 1}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	// days=1 区间只有今日:mock querier 返回区间内全部日值(bucket-a + bucket-zero-today),
	// 零容量行不得被 CDN 式「全零跳过」过滤掉
	if len(daoMock.insertsIfAbsent) != 2 {
		t.Fatalf("insert-if-absent rows = %d, want 2: %+v", len(daoMock.insertsIfAbsent), daoMock.insertsIfAbsent)
	}
	if len(daoMock.upserts) != 0 {
		t.Fatalf("upsert rows = %d, want 0 (days=1 区间无昨日行)", len(daoMock.upserts))
	}
	var zeroRow *types.OSSMetric
	for i := range daoMock.insertsIfAbsent {
		row := daoMock.insertsIfAbsent[i]
		if row.StorageSize == 0 {
			zeroRow = &daoMock.insertsIfAbsent[i]
		}
	}
	if zeroRow == nil {
		t.Fatalf("零容量行被过滤,批内: %+v", daoMock.insertsIfAbsent)
	}
	if zeroRow.BucketName != "bucket-zero-today" || zeroRow.ObjectCount != 0 {
		t.Fatalf("零容量行被篡改: %+v", *zeroRow)
	}
	if got := task.Result["metrics_total"]; got != 2 {
		t.Fatalf("metrics_total = %v, want 2", got)
	}
}

// 失败计数:querier 报错计入 Result["failures"](provider/account/error_count/last_error),
// 单 bucket 失败不中断其余 bucket
func TestSyncOSSMetrics_FailureCounting(t *testing.T) {
	e, daoMock := newTestOSSMetricsExecutor(t,
		[]domain.CloudAccount{ossMetricTestAccount(5, testOSSProviderFailing)},
		map[int64][]camdomain.Instance{
			5: {ossTestBucket(5, "bucket-a")},
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
	if f.Provider != string(testOSSProviderFailing) || f.AccountID != 5 {
		t.Fatalf("failure 归因错误: %+v", f)
	}
	if f.ErrorCount < 1 || f.LastError == "" {
		t.Fatalf("failure 计数/末次错误缺失: %+v", f)
	}
	if got := task.Result["failed_buckets"]; got != 1 {
		t.Fatalf("failed_buckets = %v, want 1", got)
	}
}

// 真实无数据(空结果)不计失败:failures 为空,任务正常结束
func TestSyncOSSMetrics_EmptyResultNotFailure(t *testing.T) {
	e, daoMock := newTestOSSMetricsExecutor(t,
		[]domain.CloudAccount{ossMetricTestAccount(6, testOSSProviderEmpty)},
		map[int64][]camdomain.Instance{
			6: {ossTestBucket(6, "bucket-empty")},
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

// 跳过不实现 OSSMetricQuerier 的厂商:不写入、不报错,Result 可见(探测不支持不计失败)
func TestSyncOSSMetrics_SkipsProviderWithoutMetricSupport(t *testing.T) {
	e, daoMock := newTestOSSMetricsExecutor(t,
		[]domain.CloudAccount{ossMetricTestAccount(7, testOSSProviderWithoutMetrics)},
		map[int64][]camdomain.Instance{
			7: {ossTestBucket(7, "bucket-x")},
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
	if len(skipped) != 1 || skipped[0] != string(testOSSProviderWithoutMetrics) {
		t.Fatalf("no_metric_support = %v", task.Result["no_metric_support"])
	}
	failures, _ := task.Result["failures"].([]nasProviderFailure)
	if len(failures) != 0 {
		t.Fatalf("探测不支持不得计入失败: %+v", failures)
	}
}

// 账号级互斥:复用共享账号互斥闸(nasAccountGate),同账号并发采集只放行一个
func TestSyncOSSMetrics_AccountMutex(t *testing.T) {
	e, _ := newTestOSSMetricsExecutor(t, nil, nil)
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

// 采集区间透传与 31 天补采窗口:GetOSSMetrics 收到 [start, end];days 超限收敛到 31
func TestSyncOSSMetrics_DateRangeAndDaysBounds(t *testing.T) {
	if ossCollectDefaultDays != 2 {
		t.Fatalf("ossCollectDefaultDays = %d, want 2 ([昨日,今日] 采集窗口)", ossCollectDefaultDays)
	}
	if ossCollectMaxDays != 31 {
		t.Fatalf("ossCollectMaxDays = %d, want 31 (补采窗口上限)", ossCollectMaxDays)
	}
	e, _ := newTestOSSMetricsExecutor(t,
		[]domain.CloudAccount{ossMetricTestAccount(8, testOSSProviderWithMetrics)},
		map[int64][]camdomain.Instance{
			8: {ossTestBucket(8, "bucket-a")},
		},
	)
	// days=999 → 收敛到 31 天窗口
	task := &taskx.Task{ID: "t7", Params: map[string]any{"days": 999}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	adapter, err := e.cloudxFactory.CreateAdapter(ossMetricTestAccountPtr(8, testOSSProviderWithMetrics))
	if err != nil {
		t.Fatalf("create adapter: %v", err)
	}
	oss := adapter.OSS().(*metricCapableOSS)
	today := time.Now().In(nasMetricsCSTZone).Format("2006-01-02")
	monthAgo := time.Now().In(nasMetricsCSTZone).AddDate(0, 0, -30).Format("2006-01-02")
	if oss.lastBucket != "bucket-a" {
		t.Fatalf("bucket 透传 = %s, want bucket-a", oss.lastBucket)
	}
	if oss.lastStart != monthAgo || oss.lastEnd != today {
		t.Fatalf("date range = %s ~ %s, want %s ~ %s (31 天窗口收敛)", oss.lastStart, oss.lastEnd, monthAgo, today)
	}
}

// OSS 适配器不可用:计入失败(OSS适配器不可用属 ERROR 语义),不产生写入
func TestSyncOSSMetrics_UnavailableOSSAdapter(t *testing.T) {
	e, daoMock := newTestOSSMetricsExecutor(t,
		[]domain.CloudAccount{ossMetricTestAccount(9, testOSSProviderNoOSS)},
		map[int64][]camdomain.Instance{
			9: {ossTestBucket(9, "bucket-a")},
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

// 写库失败:计入失败明细与 failed_buckets,不误报成功
func TestSyncOSSMetrics_DAOWriteFailure(t *testing.T) {
	e, daoMock := newTestOSSMetricsExecutor(t,
		[]domain.CloudAccount{ossMetricTestAccount(10, testOSSProviderWithMetrics)},
		map[int64][]camdomain.Instance{
			10: {ossTestBucket(10, "bucket-a")},
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
	if got := task.Result["failed_buckets"]; got != 1 {
		t.Fatalf("failed_buckets = %v, want 1", got)
	}
	if got := task.Result["metrics_total"]; got != 0 {
		t.Fatalf("写库失败不得计入 metrics_total: %v", got)
	}
}

// 任务类型注册正确:oss:collect_metrics 可被调度器识别提交
func TestSyncOSSMetrics_GetType(t *testing.T) {
	e, _ := newTestOSSMetricsExecutor(t, nil, nil)
	if got := e.GetType(); got != taskx.TaskType("oss:collect_metrics") {
		t.Fatalf("GetType = %q, want oss:collect_metrics", got)
	}
}

// 无活跃账号:不报错,空跑结束
func TestSyncOSSMetrics_NoAccounts(t *testing.T) {
	e, daoMock := newTestOSSMetricsExecutor(t, nil, nil)
	task := &taskx.Task{ID: "t8", Params: map[string]any{}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(daoMock.insertsIfAbsent)+len(daoMock.upserts) != 0 {
		t.Fatal("无账号不得产生写入")
	}
}

// ossMetricTestAccountPtr 取指针形态账号(CreateAdapter 断言用)
func ossMetricTestAccountPtr(id int64, provider domain.CloudProvider) *domain.CloudAccount {
	acc := ossMetricTestAccount(id, provider)
	return &acc
}
