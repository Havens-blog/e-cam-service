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

// ==================== NAS 回填执行器单测 ====================
//
// 覆盖 AC:分片参数(≤5 实例 × ≤10 天)、CloudWatch 配额 30% 余量换算、
// 批间退避(5s 起指数退避至上限)、命中限流厂商挂起其余厂商续跑、
// 幂等去重(已成功批次零调用零写入、缺数只采缺数区间)、错峰窗口判定。

// 测试基准时刻:今日 02:00 Asia/Shanghai(回填窗口内)
func backfillTestNow() time.Time {
	return time.Date(2026, 9, 19, 2, 0, 0, 0, nasMetricsCSTZone)
}

// nasBackfillTestDates 以 yyyy-mm-dd 构造 [start, end] 的连续日期串(闭区间)
func nasBackfillTestDates(start, end string) []string {
	var out []string
	d, _ := time.ParseInLocation("2006-01-02", start, nasMetricsCSTZone)
	endT, _ := time.ParseInLocation("2006-01-02", end, nasMetricsCSTZone)
	for !d.After(endT) {
		out = append(out, d.Format("2006-01-02"))
		d = d.AddDate(0, 0, 1)
	}
	return out
}

// ---- mock:NAS 适配器(记录调用,可注入限流/普通错误) ----

var (
	errBackfillRateLimit = errors.New("ThrottlingException: rate exceeded")
	errBackfillOther     = errors.New("internal api error")
)

type backfillNAS struct {
	metrics []types.NASMetric
	calls   []string // "fsID|start|end"
	err     error    // 非 nil 时 GetNASMetrics 返回该错误
}

func (n *backfillNAS) ListInstances(_ context.Context, _ string) ([]types.NASInstance, error) {
	return nil, nil
}
func (n *backfillNAS) GetInstance(_ context.Context, _, _ string) (*types.NASInstance, error) {
	return nil, nil
}
func (n *backfillNAS) ListInstancesByIDs(_ context.Context, _ string, _ []string) ([]types.NASInstance, error) {
	return nil, nil
}
func (n *backfillNAS) GetInstanceStatus(_ context.Context, _, _ string) (string, error) {
	return "Running", nil
}
func (n *backfillNAS) ListInstancesWithFilter(_ context.Context, _ string, _ *types.NASInstanceFilter) ([]types.NASInstance, error) {
	return nil, nil
}
func (n *backfillNAS) GetNASMetrics(_ context.Context, fsID, _, _, startDate, endDate string) ([]types.NASMetric, error) {
	n.calls = append(n.calls, fmt.Sprintf("%s|%s|%s", fsID, startDate, endDate))
	if n.err != nil {
		return nil, n.err
	}
	out := make([]types.NASMetric, 0, len(n.metrics))
	for _, m := range n.metrics {
		if m.Date < startDate || m.Date > endDate {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// 两个独立厂商的适配器实例(限流挂起只影响自身厂商的断言用)
var (
	backfillNASProviderA = &backfillNAS{}
	backfillNASProviderB = &backfillNAS{}
)

const (
	testNASBackfillProviderA = domain.CloudProvider("nasmetric-backfill-a")
	testNASBackfillProviderB = domain.CloudProvider("nasmetric-backfill-b")
)

func init() {
	cloudxRegisterBackfillTestAdapters()
}

// cloudxRegisterBackfillTestAdapters 注册回填测试适配器(独立函数便于 init 复用)
func cloudxRegisterBackfillTestAdapters() {
	cloudx.RegisterAdapter(testNASBackfillProviderA, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &nasMetricCloudAdapter{provider: testNASBackfillProviderA, nas: backfillNASProviderA}, nil
	})
	cloudx.RegisterAdapter(testNASBackfillProviderB, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &nasMetricCloudAdapter{provider: testNASBackfillProviderB, nas: backfillNASProviderB}, nil
	})
}

func resetBackfillNAS() {
	*backfillNASProviderA = backfillNAS{}
	*backfillNASProviderB = backfillNAS{}
}

// ---- mock:指标 DAO(支持预置已落库日期,记录 upsert 与预检调用) ----

type backfillDAOMock struct {
	dao.NASMetricDAO
	existing  map[int64]map[string]map[string]struct{} // account → fs → dates
	upserts   []types.NASMetric
	upsertErr error
	listErr   error
	listCalls int
}

func newBackfillDAOMock() *backfillDAOMock {
	return &backfillDAOMock{existing: make(map[int64]map[string]map[string]struct{})}
}

func (m *backfillDAOMock) seed(accountID int64, fsID string, dates ...string) {
	if m.existing[accountID] == nil {
		m.existing[accountID] = make(map[string]map[string]struct{})
	}
	if m.existing[accountID][fsID] == nil {
		m.existing[accountID][fsID] = make(map[string]struct{})
	}
	for _, d := range dates {
		m.existing[accountID][fsID][d] = struct{}{}
	}
}

func (m *backfillDAOMock) seedRange(accountID int64, fsID, start, end string) {
	m.seed(accountID, fsID, nasBackfillTestDates(start, end)...)
}

func (m *backfillDAOMock) ListExistingMetricDates(_ context.Context, accountID int64, fsIDs []string, startDate, endDate string) (map[string]map[string]struct{}, error) {
	m.listCalls++
	if m.listErr != nil {
		return nil, m.listErr
	}
	out := make(map[string]map[string]struct{})
	for _, fs := range fsIDs {
		for d := range m.existing[accountID][fs] {
			if d >= startDate && d <= endDate {
				if out[fs] == nil {
					out[fs] = make(map[string]struct{})
				}
				out[fs][d] = struct{}{}
			}
		}
	}
	return out, nil
}

func (m *backfillDAOMock) BulkUpsertMetrics(_ context.Context, metrics []types.NASMetric) error {
	if m.upsertErr != nil {
		return m.upsertErr
	}
	m.upserts = append(m.upserts, metrics...)
	return nil
}

// ---- 构造执行器(注入固定时钟 + 空 sleep) ----

func newTestBackfillExecutor(
	t *testing.T,
	accounts []domain.CloudAccount,
	instances map[int64][]camdomain.Instance,
) (*SyncNASBackfillExecutor, *backfillDAOMock) {
	t.Helper()
	daoMock := newBackfillDAOMock()
	e := NewSyncNASBackfillExecutor(
		&cdnMetricAccountRepo{accounts: accounts},
		&nasInstanceRepoMock{byAccount: instances},
		daoMock,
		&cdnMetricTaskRepo{},
		testLogger(),
	)
	e.nowFn = backfillTestNow
	e.sleepFn = func(time.Duration) {}
	return e, daoMock
}

func backfillTestAccount(id int64, provider domain.CloudProvider) domain.CloudAccount {
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

// ==================== 错峰窗口判定 ====================

// 窗口边界:01:30(含)~06:00(不含),Asia/Shanghai
func TestNASBackfill_WindowBoundaries(t *testing.T) {
	cases := []struct {
		hhmm string
		want bool
	}{
		{"01:29", false},
		{"01:30", true},
		{"03:00", true},
		{"05:59", true},
		{"06:00", false},
		{"00:10", false}, // 每日自动采集窗口,不得回填
		{"12:00", false},
	}
	for _, c := range cases {
		hh, mm := 0, 0
		if _, err := fmt.Sscanf(c.hhmm, "%d:%d", &hh, &mm); err != nil {
			t.Fatalf("parse %s: %v", c.hhmm, err)
		}
		now := time.Date(2026, 9, 19, hh, mm, 0, 0, nasMetricsCSTZone)
		if got := NASBackfillWindowActive(now); got != c.want {
			t.Fatalf("NASBackfillWindowActive(%s) = %v, want %v", c.hhmm, got, c.want)
		}
	}
	// 时区换算:同一 UTC 时刻在 CST 下判定(18:30 UTC = 次日 02:30 CST → 窗口内)
	utcNow := time.Date(2026, 9, 18, 18, 30, 0, 0, time.UTC)
	if !NASBackfillWindowActive(utcNow) {
		t.Fatal("18:30 UTC 应换算为 02:30 CST 并判定在窗口内")
	}
}

// 窗口外触发:不产生任何调用与写入,任务正常结束并标记 window_skipped
func TestNASBackfill_WindowInactiveSkips(t *testing.T) {
	resetBackfillNAS()
	acc := backfillTestAccount(1, testNASBackfillProviderA)
	e, daoMock := newTestBackfillExecutor(t,
		[]domain.CloudAccount{acc},
		map[int64][]camdomain.Instance{1: {nasTestInstance(1, "fs-1", "fs-1", "cn-hangzhou")}},
	)
	e.nowFn = func() time.Time { return time.Date(2026, 9, 19, 12, 0, 0, 0, nasMetricsCSTZone) }

	task := &taskx.Task{ID: "t-w", Params: map[string]any{"days": 14}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(backfillNASProviderA.calls) != 0 || len(daoMock.upserts) != 0 {
		t.Fatalf("窗口外不得产生调用/写入: calls=%d upserts=%d", len(backfillNASProviderA.calls), len(daoMock.upserts))
	}
	if v, _ := task.Result["window_skipped"].(bool); !v {
		t.Fatalf("window_skipped = %v, want true", task.Result["window_skipped"])
	}
}

// ==================== 天数口径(14~90,默认 30,止于昨日) ====================

func TestNASBackfill_DaysClamp(t *testing.T) {
	now := backfillTestNow()
	yesterday := now.AddDate(0, 0, -1)
	format := func(t time.Time) string { return t.Format("2006-01-02") }

	// 默认 30 天
	clamped, start, end := nasBackfillDateRange(0, now)
	if clamped != 30 || end != format(yesterday) {
		t.Fatalf("days=0 → (%d, %s, %s), want (30, …, %s)", clamped, start, end, format(yesterday))
	}
	if start != format(yesterday.AddDate(0, 0, -29)) {
		t.Fatalf("start = %s, want %s", start, format(yesterday.AddDate(0, 0, -29)))
	}
	// 下限 14
	if clamped, _, _ = nasBackfillDateRange(7, now); clamped != 14 {
		t.Fatalf("days=7 → %d, want 14(下限)", clamped)
	}
	// 上限 90
	if clamped, _, _ = nasBackfillDateRange(200, now); clamped != 90 {
		t.Fatalf("days=200 → %d, want 90(上限)", clamped)
	}
	// 14 天区间
	clamped, start, end = nasBackfillDateRange(14, now)
	if clamped != 14 || start != format(yesterday.AddDate(0, 0, -13)) || end != format(yesterday) {
		t.Fatalf("days=14 → (%d, %s, %s) 区间错误", clamped, start, end)
	}
}

// ==================== 分片参数(≤5 实例 × ≤10 天) ====================

func TestNASBackfill_Chunking(t *testing.T) {
	// 实例分片:12 个 → 5/5/2
	idx := nasBackfillChunkIndexes(12, nasBackfillBatchInstances)
	if len(idx) != 3 || len(idx[0]) != 5 || len(idx[1]) != 5 || len(idx[2]) != 2 {
		t.Fatalf("实例分片 = %v, want [[0..4],[5..9],[10,11]]", idx)
	}
	if got := nasBackfillChunkIndexes(5, nasBackfillBatchInstances); len(got) != 1 || len(got[0]) != 5 {
		t.Fatalf("5 实例应单批: %v", got)
	}
	if got := nasBackfillChunkIndexes(0, nasBackfillBatchInstances); len(got) != 0 {
		t.Fatalf("0 实例应无批: %v", got)
	}

	// 日期分片:30 天 → 3 个连续 10 天块;14 天 → 10+4
	now := backfillTestNow()
	_, start, end := nasBackfillDateRange(30, now)
	chunks := nasBackfillChunkDates(start, end, nasBackfillBatchDays)
	if len(chunks) != 3 {
		t.Fatalf("30 天应分 3 块, got %d: %v", len(chunks), chunks)
	}
	for i, c := range chunks {
		dates := nasBackfillTestDates(c.start, c.end)
		if len(dates) > nasBackfillBatchDays {
			t.Fatalf("块 %d 超过 %d 天: %v", i, nasBackfillBatchDays, c)
		}
	}
	// 闭区间连续:下一块 start 为上一块 end 的次日
	parseDay := func(s string) time.Time {
		tm, _ := time.ParseInLocation("2006-01-02", s, nasMetricsCSTZone)
		return tm
	}
	for i := 0; i < len(chunks)-1; i++ {
		if !parseDay(chunks[i+1].start).Equal(parseDay(chunks[i].end).AddDate(0, 0, 1)) {
			t.Fatalf("日期块须首尾相接连续: %v", chunks)
		}
	}
	if chunks[len(chunks)-1].end != end || chunks[0].start != start {
		t.Fatalf("日期块须覆盖完整区间 [%s, %s]: %v", start, end, chunks)
	}
	_, start, end = nasBackfillDateRange(14, now)
	chunks = nasBackfillChunkDates(start, end, nasBackfillBatchDays)
	if len(chunks) != 2 || len(nasBackfillTestDates(chunks[0].start, chunks[0].end)) != 10 ||
		len(nasBackfillTestDates(chunks[1].start, chunks[1].end)) != 4 {
		t.Fatalf("14 天应分 10+4: %v", chunks)
	}
}

// ==================== CloudWatch 配额换算(30% 余量) ====================

func TestNASBackfill_QuotaMargin(t *testing.T) {
	budget := nasBackfillQuotaBudgetPointsPerMin()
	if budget != nasBackfillCloudWatchQuotaPointsPerMin*(100-nasBackfillQuotaMarginPercent)/100 {
		t.Fatalf("配额预算 = %d, 未按 %d%% 余量换算", budget, nasBackfillQuotaMarginPercent)
	}
	if budget != 35000 {
		t.Fatalf("CloudWatch GetMetricData 50000 点/60s 留 30%% 余量应为 35000, got %d", budget)
	}
	// 默认批(5 实例 × 10 天 = 50 指标点,1 实例·天 ≈ 1 点)须在预算内
	batchPoints := nasBackfillBatchInstances * nasBackfillBatchDays
	if !nasBackfillBatchWithinQuota(batchPoints) {
		t.Fatalf("默认批 %d 点超配额预算 %d", batchPoints, budget)
	}
	if nasBackfillBatchWithinQuota(budget + 1) {
		t.Fatal("超预算批次须判定为不在配额内")
	}
}

// ==================== 批间退避(5s 起,指数至上限) ====================

func TestNASBackfill_BackoffCurve(t *testing.T) {
	cases := []struct {
		prev time.Duration
		want time.Duration
	}{
		{0, nasBackfillBackoffInitial},                 // 5s 起
		{5 * time.Second, 10 * time.Second},            // 指数
		{10 * time.Second, 20 * time.Second},           // 指数
		{4 * time.Minute, nasBackfillBackoffMax},       // 封顶
		{nasBackfillBackoffMax, nasBackfillBackoffMax}, // 保持在上限
	}
	for _, c := range cases {
		if got := nextNASBackfillBackoff(c.prev); got != c.want {
			t.Fatalf("nextNASBackfillBackoff(%v) = %v, want %v", c.prev, got, c.want)
		}
	}
}

// ==================== 幂等去重 ====================

// 已成功批次不重试:整批日期全已落库 → 不调厂商 API、不产生写入
func TestNASBackfill_DedupSkipsFullyCoveredBatches(t *testing.T) {
	resetBackfillNAS()
	acc := backfillTestAccount(1, testNASBackfillProviderA)
	e, daoMock := newTestBackfillExecutor(t,
		[]domain.CloudAccount{acc},
		map[int64][]camdomain.Instance{1: {nasTestInstance(1, "fs-1", "fs-1", "cn-hangzhou")}},
	)
	_, start, end := nasBackfillDateRange(14, backfillTestNow())
	daoMock.seedRange(1, "fs-1", start, end) // 全区间已落库

	task := &taskx.Task{ID: "t-dedup", Params: map[string]any{"days": 14}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(backfillNASProviderA.calls) != 0 {
		t.Fatalf("已成功批次不得重试, got 调用 %v", backfillNASProviderA.calls)
	}
	if len(daoMock.upserts) != 0 {
		t.Fatalf("重跑不得产生写入: %d", len(daoMock.upserts))
	}
	if skipped, _ := task.Result["batches_skipped"].(int); skipped != 2 { // 2 个日期块均整批跳过
		t.Fatalf("batches_skipped = %v, want 2", task.Result["batches_skipped"])
	}
}

// 缺数只采缺数:已落库块零调用,缺失块精确按缺失连续区间调用并写入
func TestNASBackfill_PartialMissingFetchesOnlyMissing(t *testing.T) {
	resetBackfillNAS()
	acc := backfillTestAccount(1, testNASBackfillProviderA)
	e, daoMock := newTestBackfillExecutor(t,
		[]domain.CloudAccount{acc},
		map[int64][]camdomain.Instance{1: {nasTestInstance(1, "fs-1", "fs-1", "cn-hangzhou")}},
	)
	_, start, end := nasBackfillDateRange(14, backfillTestNow())
	chunks := nasBackfillChunkDates(start, end, nasBackfillBatchDays)
	daoMock.seedRange(1, "fs-1", chunks[0].start, chunks[0].end) // 第一块已成功

	// 厂商对第二块区间返回完整数据
	secondChunk := chunks[1]
	var seeded []types.NASMetric
	for _, d := range nasBackfillTestDates(secondChunk.start, secondChunk.end) {
		seeded = append(seeded, types.NASMetric{FsID: "fs-1", FsName: "fs-1", Date: d, Capacity: 100, UsedCapacity: 10})
	}
	backfillNASProviderA.metrics = seeded

	task := &taskx.Task{ID: "t-partial", Params: map[string]any{"days": 14}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	// 仅第二块区间被调用一次,且区间精确
	want := fmt.Sprintf("fs-1|%s|%s", secondChunk.start, secondChunk.end)
	if len(backfillNASProviderA.calls) != 1 || backfillNASProviderA.calls[0] != want {
		t.Fatalf("调用 = %v, want [%s]", backfillNASProviderA.calls, want)
	}
	if len(daoMock.upserts) != len(nasBackfillTestDates(secondChunk.start, secondChunk.end)) {
		t.Fatalf("写入 %d 行, want %d(仅缺失块)", len(daoMock.upserts), len(nasBackfillTestDates(secondChunk.start, secondChunk.end)))
	}
	for _, m := range daoMock.upserts {
		if m.Date < secondChunk.start || m.Date > secondChunk.end {
			t.Fatalf("写入了缺失块之外/已成功批次的行: %+v", m)
		}
	}
}

// 重跑幂等:上一步写入后重跑 → 零调用零写入(模拟以唯一键幂等去重)
func TestNASBackfill_RerunNoDirtyRows(t *testing.T) {
	resetBackfillNAS()
	acc := backfillTestAccount(1, testNASBackfillProviderA)
	e, daoMock := newTestBackfillExecutor(t,
		[]domain.CloudAccount{acc},
		map[int64][]camdomain.Instance{1: {nasTestInstance(1, "fs-1", "fs-1", "cn-hangzhou")}},
	)
	_, start, end := nasBackfillDateRange(14, backfillTestNow())
	var seeded []types.NASMetric
	for _, d := range nasBackfillTestDates(start, end) {
		seeded = append(seeded, types.NASMetric{FsID: "fs-1", FsName: "fs-1", Date: d, Capacity: 100, UsedCapacity: 10})
	}
	backfillNASProviderA.metrics = seeded

	task := &taskx.Task{ID: "t-rerun1", Params: map[string]any{"days": 14}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("first execute: %v", err)
	}
	firstWrites := len(daoMock.upserts)
	if firstWrites == 0 {
		t.Fatal("首跑应有写入")
	}
	callsAfterFirst := len(backfillNASProviderA.calls)

	// 重跑:DAO 已含全部行(由 mock 的 existing 反映落库结果)
	for _, m := range daoMock.upserts {
		daoMock.seed(1, m.FsID, m.Date)
	}
	daoMock.upserts = nil
	task2 := &taskx.Task{ID: "t-rerun2", Params: map[string]any{"days": 14}}
	if err := e.Execute(context.Background(), task2); err != nil {
		t.Fatalf("second execute: %v", err)
	}
	if len(backfillNASProviderA.calls) != callsAfterFirst || len(daoMock.upserts) != 0 {
		t.Fatalf("重跑应零调用零写入: calls=%d(首跑 %d) upserts=%d", len(backfillNASProviderA.calls), callsAfterFirst, len(daoMock.upserts))
	}
}

// ==================== 限流退避与厂商挂起 ====================

// 命中限流:指数退避 5s→10s→20s 后耗尽重试 → 该厂商挂起;其余厂商不受累
func TestNASBackfill_RateLimitBackoffAndSuspend(t *testing.T) {
	resetBackfillNAS()
	accA := backfillTestAccount(1, testNASBackfillProviderA)
	accB := backfillTestAccount(2, testNASBackfillProviderB)
	e, daoMock := newTestBackfillExecutor(t,
		[]domain.CloudAccount{accA, accB},
		map[int64][]camdomain.Instance{
			1: {nasTestInstance(1, "fs-a", "fs-a", "cn-hangzhou")},
			2: {nasTestInstance(2, "fs-b", "fs-b", "cn-hangzhou")},
		},
	)
	backfillNASProviderA.err = errBackfillRateLimit
	var backfillNASProviderBSeeded []types.NASMetric
	_, start, end := nasBackfillDateRange(14, backfillTestNow())
	for _, d := range nasBackfillTestDates(start, end) {
		backfillNASProviderBSeeded = append(backfillNASProviderBSeeded, types.NASMetric{FsID: "fs-b", Date: d, Capacity: 100, UsedCapacity: 10})
	}
	backfillNASProviderB.metrics = backfillNASProviderBSeeded

	var sleeps []time.Duration
	e.sleepFn = func(d time.Duration) { sleeps = append(sleeps, d) }

	task := &taskx.Task{ID: "t-rl", Params: map[string]any{"days": 14}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	// 退避序列 5s→10s→20s(3 次重试后挂起);其后可能有其余厂商的批间退避 5s
	if len(sleeps) < 3 || sleeps[0] != 5*time.Second || sleeps[1] != 10*time.Second || sleeps[2] != 20*time.Second {
		t.Fatalf("退避序列前三次 = %v, want [5s 10s 20s]", sleeps)
	}
	for _, d := range sleeps[3:] {
		if d != nasBackfillBackoffInitial {
			t.Fatalf("挂起厂商之后的等待只允许批间退避 %v, got %v", nasBackfillBackoffInitial, d)
		}
	}
	// A 厂商仅 1 次调用尝试/区间?退避重试间不重复调用之外……A 首个区间共 4 次尝试(1 原始 + 3 重试)
	aCalls := len(backfillNASProviderA.calls)
	if aCalls != nasBackfillRateLimitMaxRetries+1 {
		t.Fatalf("限流厂商调用次数 = %d, want %d(原始+3 重试)", aCalls, nasBackfillRateLimitMaxRetries+1)
	}
	suspended, _ := task.Result["suspended_providers"].([]string)
	if len(suspended) != 1 || suspended[0] != string(testNASBackfillProviderA) {
		t.Fatalf("suspended_providers = %v, want [%s]", suspended, testNASBackfillProviderA)
	}
	// B 厂商不受累,正常完成
	if len(backfillNASProviderB.calls) == 0 || len(daoMock.upserts) == 0 {
		t.Fatal("未挂起厂商应正常采集")
	}
	for _, m := range daoMock.upserts {
		if m.Provider != string(testNASBackfillProviderB) {
			t.Fatalf("挂起厂商不应有写入: %+v", m)
		}
	}
}

// 非限流错误:记失败不挂起,该区间跳过后其余区间/实例继续
func TestNASBackfill_OtherErrorContinues(t *testing.T) {
	resetBackfillNAS()
	acc := backfillTestAccount(1, testNASBackfillProviderA)
	e, _ := newTestBackfillExecutor(t,
		[]domain.CloudAccount{acc},
		map[int64][]camdomain.Instance{1: {nasTestInstance(1, "fs-a", "fs-a", "cn-hangzhou")}},
	)
	backfillNASProviderA.err = errBackfillOther
	var sleeps []time.Duration
	e.sleepFn = func(d time.Duration) { sleeps = append(sleeps, d) }

	task := &taskx.Task{ID: "t-err", Params: map[string]any{"days": 14}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	// 非限流错误不得触发指数退避重试;批间退避(5s)仍允许
	for _, d := range sleeps {
		if d != nasBackfillBackoffInitial {
			t.Fatalf("非限流错误不得退避重试: %v", sleeps)
		}
	}
	if suspended, _ := task.Result["suspended_providers"].([]string); len(suspended) != 0 {
		t.Fatalf("非限流错误不得挂起厂商: %v", suspended)
	}
	failures, _ := task.Result["failures"].([]nasProviderFailure)
	if len(failures) == 0 || failures[0].ErrorCount == 0 {
		t.Fatalf("失败明细缺失: %v", task.Result["failures"])
	}
}

// ==================== 窗口中途到期(挂起续跑) ====================

func TestNASBackfill_WindowExpiryMidRun(t *testing.T) {
	resetBackfillNAS()
	acc := backfillTestAccount(1, testNASBackfillProviderA)
	// 6 个实例 → 2 个实例批(5+1),窗口在第 2 批前到期
	insts := make([]camdomain.Instance, 0, 6)
	for i := 0; i < 6; i++ {
		insts = append(insts, nasTestInstance(1, fmt.Sprintf("fs-%d", i), fmt.Sprintf("fs-%d", i), "cn-hangzhou"))
	}
	e, daoMock := newTestBackfillExecutor(t, []domain.CloudAccount{acc}, map[int64][]camdomain.Instance{1: insts})
	var seeded []types.NASMetric
	_, start, end := nasBackfillDateRange(14, backfillTestNow())
	for _, d := range nasBackfillTestDates(start, end) {
		seeded = append(seeded, types.NASMetric{Date: d, Capacity: 100, UsedCapacity: 10})
	}
	backfillNASProviderA.metrics = seeded

	// 时钟脚本:启动(区间+窗口各一次,窗口内)→ 第 1 批(窗口内)→ 第 2 批(窗口已过)
	calls := 0
	e.nowFn = func() time.Time {
		calls++
		if calls <= 3 {
			return backfillTestNow()
		}
		return time.Date(2026, 9, 19, 6, 0, 0, 0, nasMetricsCSTZone)
	}

	task := &taskx.Task{ID: "t-exp", Params: map[string]any{"days": 14}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if v, _ := task.Result["window_suspended"].(bool); !v {
		t.Fatalf("window_suspended = %v, want true", task.Result["window_suspended"])
	}
	// 仅第 1 个实例批(5 实例 × 2 日期块)被采集,第 2 批(fs-5)未触碰
	for _, m := range daoMock.upserts {
		if m.FsID == "fs-5" {
			t.Fatalf("窗口到期后不得继续采集: %+v", m)
		}
	}
	if len(backfillNASProviderA.calls) == 0 {
		t.Fatal("窗口内批次应正常采集")
	}
}

// ==================== 写入语义(历史行走覆盖 upsert,非首写) ====================

func TestNASBackfill_WritesPastRowsViaUpsert(t *testing.T) {
	resetBackfillNAS()
	acc := backfillTestAccount(1, testNASBackfillProviderA)
	e, daoMock := newTestBackfillExecutor(t,
		[]domain.CloudAccount{acc},
		map[int64][]camdomain.Instance{1: {nasTestInstance(1, "fs-1", "fs-1-name", "cn-hangzhou")}},
	)
	var seeded []types.NASMetric
	_, start, end := nasBackfillDateRange(14, backfillTestNow())
	for _, d := range nasBackfillTestDates(start, end) {
		seeded = append(seeded, types.NASMetric{FsID: "fs-1", FsName: "fs-1-name", Date: d, Capacity: 100, UsedCapacity: 10})
	}
	backfillNASProviderA.metrics = seeded

	task := &taskx.Task{ID: "t-write", Params: map[string]any{"days": 14}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	today := backfillTestNow().Format("2006-01-02")
	for _, m := range daoMock.upserts {
		if m.AccountID != 1 {
			t.Fatalf("account_id 未回填: %+v", m)
		}
		if m.Provider != string(testNASBackfillProviderA) {
			t.Fatalf("provider 未回填: %+v", m)
		}
		if m.Date >= today {
			t.Fatalf("回填不得触碰今日行: %+v", m)
		}
	}
	if written, _ := task.Result["metrics_written"].(int); written != len(nasBackfillTestDates(start, end)) {
		t.Fatalf("metrics_written = %v, want %d", task.Result["metrics_written"], len(nasBackfillTestDates(start, end)))
	}
}

// ==================== 限定范围参数 ====================

func TestNASBackfill_ScopeParams(t *testing.T) {
	resetBackfillNAS()
	accA := backfillTestAccount(1, testNASBackfillProviderA)
	accB := backfillTestAccount(2, testNASBackfillProviderB)
	e, _ := newTestBackfillExecutor(t,
		[]domain.CloudAccount{accA, accB},
		map[int64][]camdomain.Instance{
			1: {nasTestInstance(1, "fs-a", "fs-a", "cn-hangzhou")},
			2: {nasTestInstance(2, "fs-b", "fs-b", "cn-hangzhou")},
		},
	)
	var seeded []types.NASMetric
	_, start, end := nasBackfillDateRange(14, backfillTestNow())
	for _, d := range nasBackfillTestDates(start, end) {
		seeded = append(seeded, types.NASMetric{Date: d, Capacity: 100, UsedCapacity: 10})
	}
	backfillNASProviderB.metrics = seeded

	// provider 限定:仅 B 厂商
	task := &taskx.Task{ID: "t-scope", Params: map[string]any{
		"days": 14, "provider": string(testNASBackfillProviderB),
	}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(backfillNASProviderA.calls) != 0 {
		t.Fatalf("provider 限定后 A 厂商不得被采集: %v", backfillNASProviderA.calls)
	}
	if len(backfillNASProviderB.calls) == 0 {
		t.Fatal("provider 限定的 B 厂商应被采集")
	}
}

// 厂商未实现指标查询:INFO 语义跳过,不计失败不挂起
func TestNASBackfill_ProviderWithoutMetricSupport(t *testing.T) {
	resetBackfillNAS()
	acc := backfillTestAccount(1, testNASProviderWithoutMetrics)
	e, daoMock := newTestBackfillExecutor(t,
		[]domain.CloudAccount{acc},
		map[int64][]camdomain.Instance{1: {nasTestInstance(1, "fs-x", "fs-x", "cn-hangzhou")}},
	)
	task := &taskx.Task{ID: "t-nosup", Params: map[string]any{"days": 14}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	noSupport, _ := task.Result["no_metric_support"].([]string)
	if len(noSupport) != 1 || noSupport[0] != string(testNASProviderWithoutMetrics) {
		t.Fatalf("no_metric_support = %v", noSupport)
	}
	if failures, _ := task.Result["failures"].([]nasProviderFailure); len(failures) != 0 {
		t.Fatalf("不支持指标查询不得计失败: %v", failures)
	}
	if len(daoMock.upserts) != 0 {
		t.Fatalf("不支持指标查询不得有写入: %v", daoMock.upserts)
	}
}

// 任务类型锚:注册类型名冻结(调度/手动触发依赖该字符串)
func TestNASBackfill_GetType(t *testing.T) {
	e := &SyncNASBackfillExecutor{}
	if e.GetType() != taskx.TaskType("nas:backfill_metrics") {
		t.Fatalf("GetType = %s, want nas:backfill_metrics", e.GetType())
	}
}

// 写库失败:记失败明细、不挂起厂商、其余段继续(下次窗口凭幂等续跑)
func TestNASBackfill_UpsertErrorRecordsFailure(t *testing.T) {
	resetBackfillNAS()
	acc := backfillTestAccount(1, testNASBackfillProviderA)
	e, daoMock := newTestBackfillExecutor(t,
		[]domain.CloudAccount{acc},
		map[int64][]camdomain.Instance{1: {nasTestInstance(1, "fs-a", "fs-a", "cn-hangzhou")}},
	)
	var seeded []types.NASMetric
	_, start, end := nasBackfillDateRange(14, backfillTestNow())
	for _, d := range nasBackfillTestDates(start, end) {
		seeded = append(seeded, types.NASMetric{FsID: "fs-a", Date: d, Capacity: 100, UsedCapacity: 10})
	}
	backfillNASProviderA.metrics = seeded
	daoMock.upsertErr = errors.New("mongo bulk write failed")

	task := &taskx.Task{ID: "t-upsert-err", Params: map[string]any{"days": 14}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if suspended, _ := task.Result["suspended_providers"].([]string); len(suspended) != 0 {
		t.Fatalf("写库失败不得挂起厂商: %v", suspended)
	}
	failures, _ := task.Result["failures"].([]nasProviderFailure)
	if len(failures) == 0 || failures[0].ErrorCount == 0 {
		t.Fatalf("写库失败应记失败明细: %v", task.Result["failures"])
	}
	if written, _ := task.Result["metrics_written"].(int); written != 0 {
		t.Fatalf("写库失败不得计写入: %v", task.Result["metrics_written"])
	}
}

// account_id 限定:仅该账号被回填(GetByID 单账号路径)
func TestNASBackfill_AccountIDScope(t *testing.T) {
	resetBackfillNAS()
	accA := backfillTestAccount(1, testNASBackfillProviderA)
	accB := backfillTestAccount(2, testNASBackfillProviderA)
	e, _ := newTestBackfillExecutor(t,
		[]domain.CloudAccount{accA, accB},
		map[int64][]camdomain.Instance{
			1: {nasTestInstance(1, "fs-a", "fs-a", "cn-hangzhou")},
			2: {nasTestInstance(2, "fs-b", "fs-b", "cn-hangzhou")},
		},
	)
	var seeded []types.NASMetric
	_, start, end := nasBackfillDateRange(14, backfillTestNow())
	for _, d := range nasBackfillTestDates(start, end) {
		seeded = append(seeded, types.NASMetric{Date: d, Capacity: 100, UsedCapacity: 10})
	}
	backfillNASProviderA.metrics = seeded

	task := &taskx.Task{ID: "t-acc", Params: map[string]any{"days": 14, "account_id": int64(2)}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, c := range backfillNASProviderA.calls {
		if len(c) > 0 && c[:4] == "fs-a" {
			t.Fatalf("account_id 限定后账号 1 不得被采集: %v", backfillNASProviderA.calls)
		}
	}
	found := false
	for _, c := range backfillNASProviderA.calls {
		if len(c) > 0 && c[:4] == "fs-b" {
			found = true
		}
	}
	if !found {
		t.Fatalf("account_id 限定的账号 2 应被采集: %v", backfillNASProviderA.calls)
	}
}
