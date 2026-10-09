package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-common-go/taskx"
)

// T7(rds-ops-insight) RDS 每日指标采集接入持久化日闸(spec「持久化日闸复用」,
// 平移蓝本 auto_sync_disk_metrics_test.go):NAS/OSS/Disk 已实现的 scheduler_state
// 日闸加 resource_type=rds 分支,原子认领/写失败退避/读失败退避/特性开关回滚
// 全部沿用,不改既有 nas/cdn/oss/disk 键行为(Hard Rule:分键独立互不覆盖;
// 原子认领是唯一提交入口)。

// dummyRDSExecutor 让队列允许提交 rds:collect_metrics(测试不启动 worker)
type dummyRDSExecutor struct{}

func (d *dummyRDSExecutor) Execute(_ context.Context, _ *taskx.Task) error { return nil }
func (d *dummyRDSExecutor) GetType() taskx.TaskType {
	return executor.TaskTypeRDSCollectMetrics
}

// newRDSTestScheduler 建调度器(持久化闸可注入开关状态),注册 RDS 执行器
func newRDSTestScheduler(t *testing.T, gate *PersistentDailyGate, persistentEnabled bool) (*AutoSyncScheduler, *mockTaskRepo) {
	t.Helper()
	repo := &mockTaskRepo{}
	queue := taskx.NewQueue(repo, testLogger(), taskx.Config{WorkerNum: 1, BufferSize: 10})
	queue.RegisterExecutor(&dummyRDSExecutor{})
	s := NewAutoSyncScheduler(nil, queue, testLogger(), gate, persistentEnabled)
	return s, repo
}

// AC1/AC2:rds 键认领成功才提交,恰好 1 条 rds:collect_metrics days=2;
// 同日不重复;认领持久化在 rds 键(资源分键,不与 nas/cdn/oss/disk 混淆)
func TestCheckRDSMetricsCollection_PersistentGateClaimSubmits(t *testing.T) {
	g := newTestGate(newFakeGateStore(), &recordingAlerter{})
	s, repo := newRDSTestScheduler(t, g, true)

	cst := time.FixedZone("CST", 8*3600)
	clock := time.Date(2026, 9, 21, 10, 0, 0, 0, cst)
	s.nowFn = func() time.Time { return clock }

	s.checkRDSMetricsCollection()

	if n := countTasks(t, repo); n != 1 {
		t.Fatalf("认领成功应恰好提交 1 条任务,实际 %d", n)
	}
	tasks, _ := repo.List(context.Background(), taskx.TaskFilter{})
	task := tasks[0]
	if task.Type != executor.TaskTypeRDSCollectMetrics {
		t.Fatalf("任务类型应为 rds:collect_metrics,实际 %s", task.Type)
	}
	if days, _ := task.Params["days"].(int); days != 2 {
		t.Fatalf("days 语义应为 2(补昨日完整行+今日初态),实际 %v", task.Params["days"])
	}
	// rds 键已持久化认领(资源分键,不与 nas/cdn/oss/disk 混淆)
	d, _ := g.store.(*fakeGateStore)
	if d.lastDates[GateResourceRDS] != "2026-09-21" {
		t.Fatalf("rds 键应记录认领日期 2026-09-21,实际 %q", d.lastDates[GateResourceRDS])
	}
	// nas/cdn/oss/disk 键不得被 rds 认领触碰(Hard Rule:互不覆盖)
	for _, k := range []string{GateResourceNAS, GateResourceCDN, GateResourceOSS, GateResourceDisk} {
		if d.lastDates[k] != "" {
			t.Fatalf("rds 认领不得改写 %s 键: %q", k, d.lastDates[k])
		}
	}

	// 同日第二轮:不重复提交
	s.checkRDSMetricsCollection()
	if n := countTasks(t, repo); n != 1 {
		t.Fatalf("同日不应重复提交,实际 %d 条", n)
	}
}

// AC1:首次无 scheduler_state rds 记录视为首次认领——认领后触发一次当日提交,
// 不回溯补采历史;此后同日不重复、跨日每日一次
func TestCheckRDSMetricsCollection_FirstDeployTransition(t *testing.T) {
	g := newTestGate(newFakeGateStore(), &recordingAlerter{})
	s, repo := newRDSTestScheduler(t, g, true)

	cst := time.FixedZone("CST", 8*3600)
	clock := time.Date(2026, 9, 21, 10, 0, 0, 0, cst)
	s.nowFn = func() time.Time { return clock }

	// 首部署:无记录 → 首次认领 → 恰好 1 次当日提交(不回溯补采)
	s.checkRDSMetricsCollection()
	if n := countTasks(t, repo); n != 1 {
		t.Fatalf("首部署首次认领应触发一次当日提交,实际 %d", n)
	}

	// 同日多轮:不再提交
	s.checkRDSMetricsCollection()
	s.checkRDSMetricsCollection()
	if n := countTasks(t, repo); n != 1 {
		t.Fatalf("首部署同日不应重复提交,实际 %d 条", n)
	}

	// 跨日:每日一次
	clock = time.Date(2026, 9, 22, 10, 0, 0, 0, cst)
	s.checkRDSMetricsCollection()
	if n := countTasks(t, repo); n != 2 {
		t.Fatalf("跨日应提交 1 次(累计 2 条),实际 %d", n)
	}
}

// AC1/AC5:rds 键原子认领并发——多 goroutine 同触发只一个胜出(Hard Rule:
// 认领成功才提交)
func TestPersistentDailyGate_RDSKeyConcurrentClaimOnlyOneWins(t *testing.T) {
	store := newFakeGateStore()
	g := newTestGate(store, &recordingAlerter{})
	ctx := context.Background()

	const n = 32
	var winCount int64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, err := g.TryClaim(ctx, GateResourceRDS, "2026-09-21")
			if err != nil {
				t.Errorf("并发认领不应报错: %v", err)
				return
			}
			if claimed {
				winCount++
			}
		}()
	}
	wg.Wait()

	if winCount != 1 {
		t.Fatalf("rds 键并发 %d 次认领应只有 1 个成功,实际 %d", n, winCount)
	}
	if d, _ := store.GetLastDate(ctx, GateResourceRDS); d != "2026-09-21" {
		t.Fatalf("rds 键应记录认领日期 2026-09-21,实际 %q", d)
	}
}

// AC1:rds 键首次认领过渡(日闸层):无记录 → 首次认领成功;同日不重复写;
// nas/oss/disk 键互不影响(Hard Rule:分键独立)
func TestPersistentDailyGate_RDSKeyFirstClaimTransition(t *testing.T) {
	store := newFakeGateStore()
	// 预置 nas/oss/disk 已认领:rds 首次认领不得受其影响
	store.lastDates[GateResourceNAS] = "2026-09-21"
	store.lastDates[GateResourceOSS] = "2026-09-21"
	store.lastDates[GateResourceDisk] = "2026-09-21"
	g := newTestGate(store, &recordingAlerter{})
	ctx := context.Background()

	claimed, err := g.TryClaim(ctx, GateResourceRDS, "2026-09-21")
	if err != nil || !claimed {
		t.Fatalf("rds 键首次认领应成功(不受 nas/oss/disk 键影响): claimed=%v err=%v", claimed, err)
	}

	// 同日第二次:已认领,不再触发写
	claimed, err = g.TryClaim(ctx, GateResourceRDS, "2026-09-21")
	if err != nil || claimed {
		t.Fatalf("rds 键同日二次认领应返回 false: claimed=%v err=%v", claimed, err)
	}
	if store.claimCalls != 1 {
		t.Fatalf("rds 键同日不应重复写认领: claimCalls=%d", store.claimCalls)
	}

	// nas/oss/disk 键不受 rds 认领影响(Hard Rule:互不覆盖)
	for k, want := range map[string]string{
		GateResourceNAS:  "2026-09-21",
		GateResourceOSS:  "2026-09-21",
		GateResourceDisk: "2026-09-21",
	} {
		if d, _ := store.GetLastDate(ctx, k); d != want {
			t.Fatalf("%s 键应保持 %s 不变,实际 %q", k, want, d)
		}
	}
}

// AC3:rds 键写失败注入——指数退避重试耗尽升级告警,不提交任务;
// 退避窗口内下轮循环仍不提交
func TestCheckRDSMetricsCollection_WriteFailureRetriesAlertNoSubmit(t *testing.T) {
	store := newFakeGateStore()
	store.claimErr[GateResourceRDS] = errors.New("mongo write timeout")
	alerter := &recordingAlerter{}
	g := newTestGate(store, alerter)
	g.maxWriteRetries = 2

	s, repo := newRDSTestScheduler(t, g, true)

	s.checkRDSMetricsCollection()
	if n := countTasks(t, repo); n != 0 {
		t.Fatalf("日闸写失败不应提交任务,实际 %d 条", n)
	}
	if alerter.count() != 1 {
		t.Fatalf("写失败必须升级告警(非仅记日志),告警次数=%d", alerter.count())
	}
	if c := alerter.calls[0]; c.resourceType != GateResourceRDS || c.operation != "write" {
		t.Fatalf("告警参数不符: %+v", c)
	}

	// 退避窗口内下轮循环:仍不提交
	s.checkRDSMetricsCollection()
	if n := countTasks(t, repo); n != 0 {
		t.Fatalf("写退避窗口内不应提交任务,实际 %d 条", n)
	}
}

// AC3:rds 键读失败注入——≥5 分钟退避窗口内多轮循环不重复提交、不重复读
func TestPersistentDailyGate_RDSKeyReadFailureBackoff(t *testing.T) {
	store := newFakeGateStore()
	store.getErr[GateResourceRDS] = errors.New("mongo down")
	g := newTestGate(store, &recordingAlerter{})

	now := time.Now()
	clock := now
	g.now = func() time.Time { return clock }

	if g.readBackoffWindow < 5*time.Minute {
		t.Fatalf("读失败退避窗口不得小于 5 分钟: %v", g.readBackoffWindow)
	}

	ctx := context.Background()
	if _, err := g.TryClaim(ctx, GateResourceRDS, "2026-09-21"); err == nil {
		t.Fatal("rds 键读失败应返回错误")
	}

	getCallsAfterFirst := store.getCalls
	claimed, err := g.TryClaim(ctx, GateResourceRDS, "2026-09-21")
	if err != nil || claimed {
		t.Fatalf("退避窗口内应静默跳过: claimed=%v err=%v", claimed, err)
	}
	if store.getCalls != getCallsAfterFirst {
		t.Fatalf("退避窗口内不应重读: getCalls %d -> %d", getCallsAfterFirst, store.getCalls)
	}

	// 推进时钟越过退避窗口 → 恢复读
	clock = now.Add(6 * time.Minute)
	if _, err := g.TryClaim(ctx, GateResourceRDS, "2026-09-21"); err == nil {
		t.Fatal("越过退避窗口后应重试读(仍注入读失败,应再报错)")
	}
	if store.getCalls != getCallsAfterFirst+1 {
		t.Fatalf("越过退避窗口后应恰好重读一次: %d -> %d", getCallsAfterFirst, store.getCalls)
	}
}

// AC4:特性开关回滚(开关显式关闭)→ RDS 走内存闸(同日幂等、跨日触发),
// NAS/CDN/OSS/Disk 调度仍可提交、采集不中断;回滚后 RDS 不经日闸 rds 键
func TestRollbackSwitchRDSMemoryGate(t *testing.T) {
	// 日闸装配着也照样走内存闸(回滚不依赖日闸可用性)
	store := newFakeGateStore()
	s, repo := newRDSTestScheduler(t, newTestGate(store, &recordingAlerter{}), false)
	s.taskQueue.RegisterExecutor(&dummyNASExecutor{})
	s.taskQueue.RegisterExecutor(&dummyCDNExecutor{})
	s.taskQueue.RegisterExecutor(&dummyOSSExecutor{})
	s.taskQueue.RegisterExecutor(&dummyDiskExecutor{})

	cst := time.FixedZone("CST", 8*3600)
	clock := time.Date(2026, 9, 21, 10, 0, 0, 0, cst)
	s.nowFn = func() time.Time { return clock }

	// RDS 回滚内存闸:提交 1 条,同日不重复
	s.checkRDSMetricsCollection()
	s.checkRDSMetricsCollection()
	if n := countTasks(t, repo); n != 1 {
		t.Fatalf("回滚后 RDS 内存闸应提交 1 条且同日幂等,实际 %d", n)
	}

	// NAS/CDN/OSS/Disk 回滚内存闸:各提交 1 条(采集不中断)
	s.checkNASMetricsCollection()
	s.checkMetricsCollection()
	s.checkOSSMetricsCollection()
	s.checkDiskMetricsCollection()
	if n := countTasks(t, repo); n != 5 {
		t.Fatalf("回滚后 NAS/CDN/OSS/Disk 应各再提交 1 条(累计 5),实际 %d", n)
	}

	// 跨日:RDS/NAS/CDN/OSS/Disk 各再提交 1 条(采集不中断)
	clock = time.Date(2026, 9, 22, 10, 0, 0, 0, cst)
	s.checkRDSMetricsCollection()
	s.checkNASMetricsCollection()
	s.checkMetricsCollection()
	s.checkOSSMetricsCollection()
	s.checkDiskMetricsCollection()
	if n := countTasks(t, repo); n != 10 {
		t.Fatalf("回滚后跨日 RDS/NAS/CDN/OSS/Disk 应各再提交 1 条(累计 10),实际 %d", n)
	}

	// 回滚期间日闸 rds 键保持未认领(内存闸是唯一触发路径)
	if d := store.lastDates[GateResourceRDS]; d != "" {
		t.Fatalf("回滚模式不应写日闸 rds 键,实际 %q", d)
	}
}

// 开关开启但未装配日闸(nil)→ 安全跳过不 panic,不降级回内存闸
// (内存闸的重启重复提交缺陷正是本闸要修的问题,不做半吊子回退)
func TestCheckRDSMetricsCollection_NilGateSafe(t *testing.T) {
	s, repo := newRDSTestScheduler(t, nil, true)
	s.checkRDSMetricsCollection()
	if n := countTasks(t, repo); n != 0 {
		t.Fatalf("未装配日闸不应提交任务,实际 %d 条", n)
	}
}
