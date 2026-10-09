// Package executor CDN 指标采集执行器域名有界并发 + 攒批批量写用例
//
// 文件：internal/cam/task/executor/sync_cdn_metrics_concurrent_test.go
//
// 既有 sync_cdn_metrics_test.go 零改动锁定(行为等价回归)。本文件职责：
//  1. 为既有 cdnMetricDAOMock 补充 BulkUpsertMetrics 方法(executor 攒批改造
//     后走批量路径;mock 内嵌接口原方法集无批量,追加共享 upserts 记录并加锁,
//     既有断言对批量结果同样成立,原测试文件不动)
//  2. 验证域名循环 semaphore 有界并发:并发数不超上限常量,首波顶满上限
//  3. 验证每域名攒批一次 BulkUpsertMetrics:AccountID/Provider 注入、
//     无数据日过滤、逐条 UpsertMetric 调用清零
//  4. 验证单域名查询失败/批量写失败记日志 continue,不影响其它域名
package executor

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cloudx-sdk"
	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/Havens-blog/e-common-go/taskx"
)

// ==================== 既有 mock 的批量路径扩展(不改原文件) ====================

// cdnMetricDAOMockMu 保护 cdnMetricDAOMock.upserts:域名并发后批量写会从多个
// goroutine 追加(原 mock 字段无锁,原文件零改动,锁在本文件补充)。
var cdnMetricDAOMockMu sync.Mutex

// BulkUpsertMetrics 让既有 mock 支持批量路径:整批追加进同一 upserts 记录,
// 既有断言(条数/回填字段/离线域名跳过)对批量结果同样成立。
func (m *cdnMetricDAOMock) BulkUpsertMetrics(_ context.Context, metrics []types.CDNMetric) error {
	cdnMetricDAOMockMu.Lock()
	defer cdnMetricDAOMockMu.Unlock()
	m.upserts = append(m.upserts, metrics...)
	return nil
}

// ==================== 攒批/并发用例 mock ====================

// 测试用厂商键(独立前缀,不与其他用例冲突)
const (
	testProviderBatch    = domain.CloudProvider("cdnmetric-batch")
	testProviderBatchErr = domain.CloudProvider("cdnmetric-batch-err")
	testProviderConc     = domain.CloudProvider("cdnmetric-conc")
)

// cdnBatchCDN 攒批用 CDN 适配器 mock:每域名返回 3 日指标(2 有效 + 1 无数据),
// errDomain 非空时该域名查询失败;nodata.example.com 整域无数据。
type cdnBatchCDN struct {
	domains   []string
	errDomain string
}

func (c *cdnBatchCDN) ListInstances(_ context.Context, _ string) ([]types.CDNInstance, error) {
	out := make([]types.CDNInstance, 0, len(c.domains))
	for _, d := range c.domains {
		out = append(out, types.CDNInstance{DomainName: d, Status: "online"})
	}
	return out, nil
}
func (c *cdnBatchCDN) GetInstance(_ context.Context, _, _ string) (*types.CDNInstance, error) {
	return nil, nil
}
func (c *cdnBatchCDN) ListInstancesByIDs(_ context.Context, _ string, _ []string) ([]types.CDNInstance, error) {
	return nil, nil
}
func (c *cdnBatchCDN) GetInstanceStatus(_ context.Context, _, _ string) (string, error) {
	return "online", nil
}
func (c *cdnBatchCDN) ListInstancesWithFilter(_ context.Context, _ string, _ *types.CDNInstanceFilter) ([]types.CDNInstance, error) {
	return nil, nil
}
func (c *cdnBatchCDN) GetDomainMetrics(_ context.Context, domainName, _ string, _, _ string) ([]types.CDNMetric, error) {
	if domainName == c.errDomain {
		return nil, fmt.Errorf("mock query fail: %s", domainName)
	}
	var out []types.CDNMetric
	if domainName == "nodata.example.com" {
		out = []types.CDNMetric{
			{Date: "2026-09-18", Bytes: 0, Bandwidth: 0, HitRate: -1},
			{Date: "", Bytes: 100, Bandwidth: 10, HitRate: 0.5},
		}
	} else {
		out = []types.CDNMetric{
			{Date: "2026-09-17", Bytes: 100, Bandwidth: 10, HitRate: 0.5},
			{Date: "2026-09-18", Bytes: 200, Bandwidth: 20, HitRate: 0.6},
			{Date: "2026-09-19", Bytes: 0, Bandwidth: 0, HitRate: -1}, // 无数据日,应过滤
		}
	}
	// 与真实适配器一致:回填 Domain(DAO 唯一键 account_id+domain+date 之一)
	for i := range out {
		out[i].Domain = domainName
	}
	return out, nil
}

var _ cloudx.CDNAdapter = (*cdnBatchCDN)(nil)
var _ cloudx.CDNMetricQuerier = (*cdnBatchCDN)(nil)

// cdnConcCDN 有界并发验证适配器:进入 GetDomainMetrics 即计数并发发到达信号,
// 等测试关闭闸门后放行(保证能稳定观察到首波并发恰好顶满上限)。
type cdnConcCDN struct {
	domains  []string
	mu       sync.Mutex
	inFlight int
	maxSeen  int
	arrive   chan struct{} // 每次进入查询(计数后)发一个到达信号
	gate     chan struct{} // 关闭后放行
}

func (c *cdnConcCDN) GetDomainMetrics(_ context.Context, _, _ string, _, _ string) ([]types.CDNMetric, error) {
	c.mu.Lock()
	c.inFlight++
	if c.inFlight > c.maxSeen {
		c.maxSeen = c.inFlight
	}
	c.mu.Unlock()
	c.arrive <- struct{}{}
	<-c.gate
	c.mu.Lock()
	c.inFlight--
	c.mu.Unlock()
	return []types.CDNMetric{{Date: "2026-09-19", Bytes: 1, Bandwidth: 1, HitRate: 0}}, nil
}
func (c *cdnConcCDN) ListInstances(_ context.Context, _ string) ([]types.CDNInstance, error) {
	out := make([]types.CDNInstance, 0, len(c.domains))
	for _, d := range c.domains {
		out = append(out, types.CDNInstance{DomainName: d, Status: "online"})
	}
	return out, nil
}
func (c *cdnConcCDN) GetInstance(_ context.Context, _, _ string) (*types.CDNInstance, error) {
	return nil, nil
}
func (c *cdnConcCDN) ListInstancesByIDs(_ context.Context, _ string, _ []string) ([]types.CDNInstance, error) {
	return nil, nil
}
func (c *cdnConcCDN) GetInstanceStatus(_ context.Context, _, _ string) (string, error) {
	return "online", nil
}
func (c *cdnConcCDN) ListInstancesWithFilter(_ context.Context, _ string, _ *types.CDNInstanceFilter) ([]types.CDNInstance, error) {
	return nil, nil
}

func (c *cdnConcCDN) maxSeenValue() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.maxSeen
}

var _ cloudx.CDNAdapter = (*cdnConcCDN)(nil)
var _ cloudx.CDNMetricQuerier = (*cdnConcCDN)(nil)

// cdnMetricBatchDAOMock 批量 DAO mock:按批记录、可注入按域名的批量写失败、
// 统计逐条 UpsertMetric 调用次数(断言攒批改造后逐条调用清零)。
type cdnMetricBatchDAOMock struct {
	dao.CDNMetricDAO
	mu          sync.Mutex
	batches     [][]types.CDNMetric
	singleCalls int
	failDomain  string // 批内含该域名则批量写失败(失败批不记录)
}

func (m *cdnMetricBatchDAOMock) BulkUpsertMetrics(_ context.Context, metrics []types.CDNMetric) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failDomain != "" {
		for _, mm := range metrics {
			if mm.Domain == m.failDomain {
				return fmt.Errorf("mock bulk write fail: %s", m.failDomain)
			}
		}
	}
	m.batches = append(m.batches, append([]types.CDNMetric(nil), metrics...))
	return nil
}

func (m *cdnMetricBatchDAOMock) UpsertMetric(_ context.Context, _ types.CDNMetric) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.singleCalls++
	return nil
}

func (m *cdnMetricBatchDAOMock) batchCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.batches)
}

func (m *cdnMetricBatchDAOMock) allMetrics() []types.CDNMetric {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]types.CDNMetric, 0, len(m.batches))
	for _, b := range m.batches {
		out = append(out, b...)
	}
	return out
}

func init() {
	cloudx.RegisterAdapter(testProviderBatch, func(*domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &metricCloudAdapter{
			provider: testProviderBatch,
			cdn: &cdnBatchCDN{domains: []string{
				"a.example.com", "b.example.com", "nodata.example.com",
			}},
		}, nil
	})
	cloudx.RegisterAdapter(testProviderBatchErr, func(*domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &metricCloudAdapter{
			provider: testProviderBatchErr,
			cdn: &cdnBatchCDN{domains: []string{
				"bad.example.com", "good.example.com",
			}, errDomain: "bad.example.com"},
		}, nil
	})
}

func newBatchTestExecutor(_ *testing.T, provider domain.CloudProvider, metricDAO dao.CDNMetricDAO) *SyncCDNMetricsExecutor {
	return NewSyncCDNMetricsExecutor(
		&cdnMetricAccountRepo{accounts: []domain.CloudAccount{cdnMetricTestAccount(7, provider)}},
		metricDAO,
		&cdnMetricTaskRepo{},
		testLogger(),
	)
}

// 每域名攒批一次批量写:AccountID/Provider 注入、无数据日过滤、逐条调用清零
func TestSyncCDNMetrics_BatchesPerDomain(t *testing.T) {
	daoMock := &cdnMetricBatchDAOMock{}
	e := newBatchTestExecutor(t, testProviderBatch, daoMock)
	task := &taskx.Task{ID: "t-batch", Params: map[string]any{"days": 1}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	// 3 域名中 nodata 域整域无数据不产生批量写 → 2 批,每批 2 条(无数据日已滤)
	if got := daoMock.batchCount(); got != 2 {
		t.Fatalf("bulk batches = %d, want 2", got)
	}
	all := daoMock.allMetrics()
	if len(all) != 4 {
		t.Fatalf("total metrics = %d, want 4: %+v", len(all), all)
	}
	for _, m := range all {
		if m.AccountID != 7 || m.Provider != string(testProviderBatch) {
			t.Fatalf("metric not enriched: %+v", m)
		}
		if m.Date == "" || (m.Bytes == 0 && m.Bandwidth == 0 && m.HitRate < 0) {
			t.Fatalf("no-data metric should be filtered: %+v", m)
		}
	}
	if got := task.Result["metrics_total"]; got != 4 {
		t.Fatalf("metrics_total = %v, want 4", got)
	}
	if daoMock.singleCalls != 0 {
		t.Fatalf("UpsertMetric calls = %d, want 0 (攒批后逐条调用清零)", daoMock.singleCalls)
	}
}

// 单域名查询失败:记日志 continue,不影响其它域名采集与写入
func TestSyncCDNMetrics_DomainQueryErrorContinues(t *testing.T) {
	daoMock := &cdnMetricBatchDAOMock{}
	e := newBatchTestExecutor(t, testProviderBatchErr, daoMock)
	task := &taskx.Task{ID: "t-qerr", Params: map[string]any{"days": 1}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := daoMock.batchCount(); got != 1 {
		t.Fatalf("bulk batches = %d, want 1", got)
	}
	for _, m := range daoMock.allMetrics() {
		if m.Domain != "good.example.com" {
			t.Fatalf("failed domain should not be written: %+v", m)
		}
	}
	if got := task.Result["metrics_total"]; got != 2 {
		t.Fatalf("metrics_total = %v, want 2", got)
	}
}

// 单域名批量写失败:记日志 continue,其它域名不受影响,失败域不计数
func TestSyncCDNMetrics_BulkWriteErrorContinues(t *testing.T) {
	daoMock := &cdnMetricBatchDAOMock{failDomain: "a.example.com"}
	e := newBatchTestExecutor(t, testProviderBatch, daoMock)
	task := &taskx.Task{ID: "t-werr", Params: map[string]any{"days": 1}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := daoMock.batchCount(); got != 1 {
		t.Fatalf("bulk batches = %d, want 1 (失败域整批不记录)", got)
	}
	for _, m := range daoMock.allMetrics() {
		if m.Domain != "b.example.com" {
			t.Fatalf("failed domain should not be written: %+v", m)
		}
	}
	if got := task.Result["metrics_total"]; got != 2 {
		t.Fatalf("metrics_total = %v, want 2 (失败域不计 written)", got)
	}
}

// 域名循环经 semaphore 有界并发:并发数不超上限常量,且首波顶满上限(默认 5)
func TestSyncCDNMetrics_BoundedDomainConcurrency(t *testing.T) {
	if cdnMetricDomainConcurrency != 5 {
		t.Fatalf("cdnMetricDomainConcurrency = %d, want 5 (默认上限)", cdnMetricDomainConcurrency)
	}
	const domainCount = 12
	domains := make([]string, 0, domainCount)
	for i := 0; i < domainCount; i++ {
		domains = append(domains, fmt.Sprintf("conc-%02d.example.com", i))
	}
	cdn := &cdnConcCDN{
		domains: domains,
		arrive:  make(chan struct{}, domainCount),
		gate:    make(chan struct{}),
	}
	cloudx.RegisterAdapter(testProviderConc, func(*domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &metricCloudAdapter{provider: testProviderConc, cdn: cdn}, nil
	})
	daoMock := &cdnMetricBatchDAOMock{}
	e := newBatchTestExecutor(t, testProviderConc, daoMock)
	task := &taskx.Task{ID: "t-conc", Params: map[string]any{"days": 1}}

	done := make(chan error, 1)
	go func() { done <- e.Execute(context.Background(), task) }()

	// 首波:到达信号在并发计数之后发出,收到上限个信号时并发数恰为上限
	for i := 0; i < cdnMetricDomainConcurrency; i++ {
		select {
		case <-cdn.arrive:
		case <-time.After(10 * time.Second):
			t.Fatal("等待并发到达超时:域名循环疑似串行")
		}
	}
	if got := cdn.maxSeenValue(); got != cdnMetricDomainConcurrency {
		t.Fatalf("first-wave concurrency = %d, want %d", got, cdnMetricDomainConcurrency)
	}
	close(cdn.gate) // 放行全部域名
	if err := <-done; err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := cdn.maxSeenValue(); got > cdnMetricDomainConcurrency {
		t.Fatalf("max concurrency = %d, exceeds limit %d", got, cdnMetricDomainConcurrency)
	}
	if got := task.Result["metrics_total"]; got != domainCount {
		t.Fatalf("metrics_total = %v, want %d", got, domainCount)
	}
}
