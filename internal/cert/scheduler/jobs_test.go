package scheduler

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/robfig/cron/v3"
)

// ---------------------------------------------------------------------
// 测试 fake（窄端口，仅调度接线断言用）
// ---------------------------------------------------------------------

// fakeScanRunner 记录调用并可注入返回。
type fakeScanRunner struct {
	mu          sync.Mutex
	startCalls  int
	recoverHits int
	startErr    error
}

func (f *fakeScanRunner) StartScan(context.Context) (service.ScanResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalls++
	return service.ScanResult{}, f.startErr
}

func (f *fakeScanRunner) RecoverTimedOutScans(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recoverHits++
	return 0, nil
}

func (f *fakeScanRunner) RecoverOrphanedScans(context.Context) (int, error) {
	return 0, nil
}

// fakeInspectionRunner 单方法巡检入口。
type fakeInspectionRunner struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeInspectionRunner) RunInspection(context.Context) (InspectionRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return InspectionRun{}, nil
}

// fakeWindowRunner 提频探测 + 终局判定（终局返回固定订单集）。
type fakeWindowRunner struct {
	mu        sync.Mutex
	probeHits int
	finalized []string
}

func (f *fakeWindowRunner) ProbeVerifyingWindows(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probeHits++
	return 0, nil
}

func (f *fakeWindowRunner) FinalizeExpiredWindows(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.finalized == nil {
		return nil, nil
	}
	return f.finalized, nil
}

// fakePauseCanceller 暂停超时取消（返回固定取消清单）。
type fakePauseCanceller struct {
	mu        sync.Mutex
	calls     int
	cancelled []string
}

func (f *fakePauseCanceller) CancelByTimeout(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.cancelled, nil
}

// fakeExecutingRecoverer executing-timeout 恢复。
type fakeExecutingRecoverer struct{ hits int }

func (f *fakeExecutingRecoverer) RecoverTimedOutItems(context.Context) (int, error) {
	f.hits++
	return 0, nil
}

// fakeOrphanConsumer 孤儿清理双入口。
type fakeOrphanConsumer struct {
	mu       sync.Mutex
	sweepHit int
	consumed []string
}

func (f *fakeOrphanConsumer) SweepOrphans(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sweepHit++
	return 0, nil
}

func (f *fakeOrphanConsumer) ConsumeOrderQueue(_ context.Context, orderID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.consumed = append(f.consumed, orderID)
	return 1, nil
}

// fakeRecheckRunner crd-recheck 消费。
type fakeRecheckRunner struct{ hits int }

func (f *fakeRecheckRunner) RunDueRechecks(context.Context) (int, error) {
	f.hits++
	return 0, nil
}

// recordingPublisher 记录发布事件（含 ops 处置通知）。
type recordingPublisher struct {
	mu     sync.Mutex
	events []service.CertAlertEvent
}

func (p *recordingPublisher) PublishAlert(_ context.Context, evt service.CertAlertEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, evt)
	return nil
}

func (p *recordingPublisher) snapshot() []service.CertAlertEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]service.CertAlertEvent(nil), p.events...)
}

// newTestCertJobs 组装带 fake 的调度依赖集。
func newTestCertJobs() (*CertJobs, *fakeScanRunner, *fakeWindowRunner, *fakePauseCanceller, *fakeOrphanConsumer, *recordingPublisher) {
	scan := &fakeScanRunner{}
	windows := &fakeWindowRunner{}
	pause := &fakePauseCanceller{}
	orphan := &fakeOrphanConsumer{}
	pub := &recordingPublisher{}
	jobs := &CertJobs{
		Scan:       scan,
		Inspection: &fakeInspectionRunner{},
		Windows:    windows,
		Changes:    pause,
		Execute:    &fakeExecutingRecoverer{},
		Orphan:     orphan,
		Recheck:    &fakeRecheckRunner{},
		Publisher:  pub,
	}
	return jobs, scan, windows, pause, orphan, pub
}

// specByName 从清单中按名取调度点（缺失即失败）。
func specByName(t *testing.T, specs []CertJobSpec, name string) CertJobSpec {
	t.Helper()
	for _, s := range specs {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("调度点 %s 未注册", name)
	return CertJobSpec{}
}

// ---------------------------------------------------------------------
// AC-1：9 类任务按 Scheduler Tasks 表频率注册
// ---------------------------------------------------------------------

// TestCertJobs_JobSpecs_CoversSchedulerTasks 8 个调度点承载 9 类任务
// （probe 天级由 cert:inspection 第三步承载，见 jobs.go 头注释），频率逐项对表。
func TestCertJobs_JobSpecs_CoversSchedulerTasks(t *testing.T) {
	jobs, scan, _, _, _, _ := newTestCertJobs()
	specs := jobs.JobSpecs(10)
	if len(specs) != 8 {
		t.Fatalf("期望 8 个调度点（9 类任务，probe 天级折叠进 inspection），实际 %d", len(specs))
	}

	expect := []struct{ name, spec string }{
		{JobScan, "0 2 * * *"},               // scan 天级
		{JobScanTimeout, "*/15 * * * *"},     // scan-timeout 每 15 分钟
		{JobInspection, "0 5 * * *"},         // inspection 天级（含 probe 天级）
		{JobVerifyWindow, "*/10 * * * *"},    // window-expiry/probe 提频 = verifyProbeIntervalMinutes
		{JobPauseTimeout, "0 * * * *"},       // pause-timeout 每小时
		{JobOrphanSweep, "0 3 * * *"},        // orphan-cleanup 天级批扫
		{JobCrdRecheck, "* * * * *"},         // crd-recheck 分钟级扫描（recheckDelayMinutes 默认 5）
		{JobExecutingTimeout, "*/5 * * * *"}, // executing-timeout 每 5 分钟
	}
	for _, e := range expect {
		s := specByName(t, specs, e.name)
		if s.Spec != e.spec {
			t.Errorf("%s 频率期望 %q 实际 %q", e.name, e.spec, s.Spec)
		}
		if s.Run == nil {
			t.Errorf("%s 入口函数缺失", e.name)
		}
	}

	// scan 调度点实际触发 StartScan。
	if err := specByName(t, specs, JobScan).Run(context.Background()); err != nil {
		t.Fatalf("scan 调度点执行失败: %v", err)
	}
	if scan.startCalls != 1 {
		t.Fatalf("StartScan 期望调用 1 次，实际 %d", scan.startCalls)
	}
	// scan-timeout 调度点触发 RecoverTimedOutScans。
	if err := specByName(t, specs, JobScanTimeout).Run(context.Background()); err != nil {
		t.Fatalf("scan-timeout 调度点执行失败: %v", err)
	}
	if scan.recoverHits != 1 {
		t.Fatalf("RecoverTimedOutScans 期望调用 1 次，实际 %d", scan.recoverHits)
	}
}

// TestVerifyWindowSpec_IntervalFromThresholds 窗口周期分钟数 → cron 表达式
// （整除取 */n；非整除取 minute%n==0 集合；>=60 整点；非法回退默认 10）。
func TestVerifyWindowSpec_IntervalFromThresholds(t *testing.T) {
	cases := []struct {
		minutes int
		want    string
	}{
		{10, "*/10 * * * *"},
		{5, "*/5 * * * *"},
		{30, "*/30 * * * *"},
		{45, "*/45 * * * *"},
		{60, "0 * * * *"},
		{90, "0 * * * *"},
		{0, "*/10 * * * *"},  // 非法回退默认
		{-3, "*/10 * * * *"}, // 非法回退默认
	}
	for _, c := range cases {
		if got := VerifyWindowSpec(c.minutes); got != c.want {
			t.Errorf("VerifyWindowSpec(%d) 期望 %q 实际 %q", c.minutes, c.want, got)
		}
	}
}

// TestResolveVerifyProbeIntervalMinutes AC-6：窗口周期从 AlertConfig.thresholds
// 读取；读取失败/未配置回退默认（10 分钟）。
func TestResolveVerifyProbeIntervalMinutes(t *testing.T) {
	// 未写入文档 → 默认 10。
	if got := ResolveVerifyProbeIntervalMinutes(context.Background(), certtest.NewFakeAlertConfigRepo()); got != 10 {
		t.Fatalf("缺省配置期望默认 10，实际 %d", got)
	}
	// 显式阈值。
	cfg := certtest.NewFakeAlertConfigRepo()
	base := domain.DefaultAlertConfig()
	base.Thresholds.VerifyProbeIntervalMinutes = 20
	if err := cfg.Save(context.Background(), &base); err != nil {
		t.Fatal(err)
	}
	if got := ResolveVerifyProbeIntervalMinutes(context.Background(), cfg); got != 20 {
		t.Fatalf("配置 20 期望 20，实际 %d", got)
	}
	// nil 仓储安全回退。
	if got := ResolveVerifyProbeIntervalMinutes(context.Background(), nil); got != 10 {
		t.Fatalf("nil 仓储期望默认 10，实际 %d", got)
	}
}

// ---------------------------------------------------------------------
// AC-3：防重/幂等/失败隔离
// ---------------------------------------------------------------------

// TestCertJobs_ScanDedupSwallowsInProgress scan 防重：已有 running 快照
// （ErrScanInProgress）按"跳过本轮"处理，不作为调度错误上抛——scan-timeout
// 恢复释放锁后下一轮可正常触发（联动语义由 3.5 仓储状态承载）。
func TestCertJobs_ScanDedupSwallowsInProgress(t *testing.T) {
	jobs, scan, _, _, _, _ := newTestCertJobs()
	scan.startErr = domain.ErrScanInProgress
	specs := jobs.JobSpecs(10)
	if err := specByName(t, specs, JobScan).Run(context.Background()); err != nil {
		t.Fatalf("防重应跳过而非报错: %v", err)
	}
	if scan.startCalls != 1 {
		t.Fatalf("StartScan 期望调用 1 次，实际 %d", scan.startCalls)
	}
	// 其余错误正常上抛（调度层可见）。
	scan.startErr = errors.New("boom")
	if err := specByName(t, specs, JobScan).Run(context.Background()); err == nil {
		t.Fatal("真实错误应上抛")
	}
}

// TestCertJobs_VerifyWindowJob_OrphanEventTrigger window 调度点：提频探测 →
// 终局判定 → 每个终局订单即时消费孤儿清理队列（orphan-cleanup 事件触发路径）。
func TestCertJobs_VerifyWindowJob_OrphanEventTrigger(t *testing.T) {
	jobs, _, windows, _, orphan, _ := newTestCertJobs()
	windows.finalized = []string{"order-a", "order-b"}
	specs := jobs.JobSpecs(10)
	if err := specByName(t, specs, JobVerifyWindow).Run(context.Background()); err != nil {
		t.Fatalf("窗口调度点执行失败: %v", err)
	}
	if windows.probeHits != 1 {
		t.Fatalf("ProbeVerifyingWindows 期望调用 1 次，实际 %d", windows.probeHits)
	}
	if len(orphan.consumed) != 2 ||
		orphan.consumed[0] != "order-a" || orphan.consumed[1] != "order-b" {
		t.Fatalf("终局订单应逐一入队消费，实际 %v", orphan.consumed)
	}
}

// TestCertJobs_PauseTimeoutNotifiesCancelledOrders pause-timeout 调度点：
// 每笔超时取消的订单经 ops 通道发布"变更单超时取消"处置通知（不计四类业务告警）。
func TestCertJobs_PauseTimeoutNotifiesCancelledOrders(t *testing.T) {
	jobs, _, _, pause, _, pub := newTestCertJobs()
	pause.cancelled = []string{"order-x", "order-y"}
	specs := jobs.JobSpecs(10)
	if err := specByName(t, specs, JobPauseTimeout).Run(context.Background()); err != nil {
		t.Fatalf("pause-timeout 调度点执行失败: %v", err)
	}
	events := pub.snapshot()
	if len(events) != 2 {
		t.Fatalf("期望 2 条超时取消通知，实际 %d", len(events))
	}
	for i, want := range []string{"order-x", "order-y"} {
		if events[i].Category != service.AlertCategoryOps {
			t.Errorf("通知 %d 类别期望 ops，实际 %s", i, events[i].Category)
		}
		if events[i].OrderID != want {
			t.Errorf("通知 %d 订单期望 %s，实际 %s", i, want, events[i].OrderID)
		}
		if !strings.Contains(events[i].Title, "超时") {
			t.Errorf("通知 %d 标题应含超时语义: %q", i, events[i].Title)
		}
	}
}

// TestCertJobs_RunRecoversPanic 任务入口 panic 不击穿调度框架（recover 转 error）。
func TestCertJobs_RunRecoversPanic(t *testing.T) {
	jobs, _, _, _, _, _ := newTestCertJobs()
	// 用会 panic 的 scan fake 验证包装层 recover。
	jobs.Scan = &panicScanRunner{}
	specs := jobs.JobSpecs(10)
	err := specByName(t, specs, JobScan).Run(context.Background())
	if err == nil {
		t.Fatal("panic 应转为 error 返回")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Fatalf("错误应标注 panic 来源: %v", err)
	}
}

type panicScanRunner struct{}

func (p *panicScanRunner) StartScan(context.Context) (service.ScanResult, error) {
	panic("scan exploded")
}
func (p *panicScanRunner) RecoverTimedOutScans(context.Context) (int, error) { return 0, nil }
func (p *panicScanRunner) RecoverOrphanedScans(context.Context) (int, error) { return 0, nil }

// TestCertJobs_SingleFlightSkipsOverlap 同一任务不重复并发执行：上一轮
// 未结束时下一轮触发直接跳过（框架级互斥，AC-3）。
func TestCertJobs_SingleFlightSkipsOverlap(t *testing.T) {
	jobs, _, _, _, _, _ := newTestCertJobs()
	started := make(chan struct{})
	release := make(chan struct{})
	jobs.Scan = &blockingScanRunner{started: started, release: release}
	specs := jobs.JobSpecs(10)
	run := specByName(t, specs, JobScan).Run

	done := make(chan error, 1)
	go func() { done <- run(context.Background()) }()
	<-started // 第一轮进入执行

	// 第二轮并发触发：应立即跳过返回 nil，不再进入 StartScan。
	skipDone := make(chan error, 1)
	go func() { skipDone <- run(context.Background()) }()
	select {
	case err := <-skipDone:
		if err != nil {
			t.Fatalf("并发触发应跳过而非报错: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("并发触发被阻塞——单飞守卫未生效")
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("首轮执行失败: %v", err)
	}
	if b := jobs.Scan.(*blockingScanRunner); b.entered != 1 {
		t.Fatalf("并发期应仅 1 次进入 StartScan，实际 %d", b.entered)
	}
	// 释放后可再次执行。
	if err := run(context.Background()); err != nil {
		t.Fatalf("释放后应可重新执行: %v", err)
	}
	if b := jobs.Scan.(*blockingScanRunner); b.entered != 2 {
		t.Fatalf("释放后应可再次进入，实际 %d", b.entered)
	}
}

type blockingScanRunner struct {
	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
	entered   int
	mu        sync.Mutex
}

func (b *blockingScanRunner) StartScan(context.Context) (service.ScanResult, error) {
	b.mu.Lock()
	b.entered++
	b.mu.Unlock()
	// started 仅首次进入关闭：并发重叠进入会同时等待 release，由 entered 计数暴露。
	b.startOnce.Do(func() { close(b.started) })
	<-b.release
	return service.ScanResult{}, nil
}

func (b *blockingScanRunner) RecoverTimedOutScans(context.Context) (int, error) {
	return 0, nil
}

func (b *blockingScanRunner) RecoverOrphanedScans(context.Context) (int, error) {
	return 0, nil
}

// ---------------------------------------------------------------------
// 装配回归：9 个调度点全部可执行一次（fake 驱动，无 panic 无死锁）
// ---------------------------------------------------------------------

func TestCertJobs_AllSpecsRunnable(t *testing.T) {
	jobs, _, windows, pause, orphan, _ := newTestCertJobs()
	windows.finalized = []string{"o1"}
	pause.cancelled = []string{"o1"}
	specs := jobs.JobSpecs(10)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, s := range specs {
		if err := s.Run(ctx); err != nil {
			t.Fatalf("调度点 %s 执行失败: %v", s.Name, err)
		}
	}
	if orphan.sweepHit != 1 {
		t.Fatalf("SweepOrphans 期望 1 次，实际 %d", orphan.sweepHit)
	}
}

// TestCertJobs_AllSpecsParseable 全部 cron 表达式可被标准 5 字段解析器解析
// （ego ecron 默认解析器；坏表达式会在启动期 panic——装配前置校验）。
func TestCertJobs_AllSpecsParseable(t *testing.T) {
	jobs, _, _, _, _, _ := newTestCertJobs()
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	for _, s := range jobs.JobSpecs(10) {
		if _, err := parser.Parse(s.Spec); err != nil {
			t.Errorf("调度点 %s 表达式 %q 解析失败: %v", s.Name, s.Spec, err)
		}
	}
	// 界值边界（5~60）全部可解析。
	for _, m := range []int{5, 7, 30, 45, 60} {
		if _, err := parser.Parse(VerifyWindowSpec(m)); err != nil {
			t.Errorf("VerifyWindowSpec(%d) = %q 解析失败: %v", m, VerifyWindowSpec(m), err)
		}
	}
}
