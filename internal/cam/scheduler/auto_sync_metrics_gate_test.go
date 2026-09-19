package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
)

// T8 CDN 迁移持久化日闸 + 值回归 + 特性开关回滚(spec/proposal「CDN 迁移回归
// 与首部署过渡」「特性开关与回滚」):
//   - CDN checkMetricsCollection 从内存 lastMetricsCollectDate 迁到持久化日闸
//     cdn 键(原子认领,重启不重复提交);
//   - 迁移值回归:内存闸基线 → 持久化闸无缺口、无历史重采、days=2 语义不变;
//   - 首部署过渡:scheduler_state 无 cdn 记录 → 首次认领触发一次当日提交;
//   - 特性开关 SCHEDULER_PERSISTENT_GATE_ENABLED 默认开启;切回内存闸后
//     NAS/CDN 调度仍正常提交(回滚验证,Hard Rule)。

// dummyCDNExecutor 让队列允许提交 cdn:collect_metrics(测试不启动 worker)
type dummyCDNExecutor struct{}

func (d *dummyCDNExecutor) Execute(_ context.Context, _ *taskx.Task) error { return nil }
func (d *dummyCDNExecutor) GetType() taskx.TaskType                        { return executor.TaskTypeCollectMetrics }

// newFlagTestScheduler 建调度器(持久化闸可注入开关状态),注册 NAS/CDN 执行器
func newFlagTestScheduler(t *testing.T, gate *PersistentDailyGate, persistentEnabled bool) (*AutoSyncScheduler, *mockTaskRepo) {
	t.Helper()
	repo := &mockTaskRepo{}
	queue := taskx.NewQueue(repo, testLogger(), taskx.Config{WorkerNum: 1, BufferSize: 10})
	queue.RegisterExecutor(&dummyNASExecutor{})
	queue.RegisterExecutor(&dummyCDNExecutor{})
	s := NewAutoSyncScheduler(nil, queue, testLogger(), gate, persistentEnabled)
	return s, repo
}

func countTasks(t *testing.T, repo *mockTaskRepo) int64 {
	t.Helper()
	n, err := repo.Count(context.Background(), taskx.TaskFilter{})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// AC1:CDN 经持久化日闸 cdn 键认领成功才提交,恰好 1 条 days=2;同日不重复
func TestCheckMetricsCollection_PersistentGateClaimSubmits(t *testing.T) {
	g := newTestGate(newFakeGateStore(), &recordingAlerter{})
	s, repo := newFlagTestScheduler(t, g, true)

	cst := time.FixedZone("CST", 8*3600)
	clock := time.Date(2026, 9, 19, 10, 0, 0, 0, cst)
	s.nowFn = func() time.Time { return clock }

	s.checkMetricsCollection()

	if n := countTasks(t, repo); n != 1 {
		t.Fatalf("认领成功应恰好提交 1 条任务,实际 %d", n)
	}
	tasks, _ := repo.List(context.Background(), taskx.TaskFilter{})
	task := tasks[0]
	if task.Type != executor.TaskTypeCollectMetrics {
		t.Fatalf("任务类型应为 cdn:collect_metrics,实际 %s", task.Type)
	}
	if days, _ := task.Params["days"].(int); days != 2 {
		t.Fatalf("days 语义应为 2(补昨日完整行+今日初态),实际 %v", task.Params["days"])
	}
	// cdn 键已持久化认领(资源分键,不与 nas 混淆)
	if d, _ := g.store.(*fakeGateStore); d.lastDates[GateResourceCDN] != "2026-09-19" {
		t.Fatalf("cdn 键应记录认领日期 2026-09-19,实际 %q", d.lastDates[GateResourceCDN])
	}

	// 同日第二轮(含重启语义:同一存储再起新闸):不重复提交
	s.checkMetricsCollection()
	if n := countTasks(t, repo); n != 1 {
		t.Fatalf("同日不应重复提交,实际 %d 条", n)
	}
}

// AC4:连续 3 次重启(同一 scheduler_state 再起新调度器)× 每日 CDN 任务仅 1 条
func TestCheckMetricsCollection_RestartThreeTimesSingleSubmit(t *testing.T) {
	store := newFakeGateStore()

	cst := time.FixedZone("CST", 8*3600)
	clock := time.Date(2026, 9, 19, 10, 0, 0, 0, cst)

	var total int64
	for i := 0; i < 3; i++ {
		g := newTestGate(store, &recordingAlerter{})
		s, repo := newFlagTestScheduler(t, g, true)
		s.nowFn = func() time.Time { return clock }
		s.checkMetricsCollection()
		n := countTasks(t, repo)
		if i > 0 && n != 0 {
			t.Fatalf("第 %d 次重启不应重复提交,实际 %d 条", i+1, n)
		}
		total += n
	}
	if total != 1 {
		t.Fatalf("3 次重启后当日 CDN 任务总计应仅 1 条,实际 %d", total)
	}
}

// AC4:写失败注入——指数退避重试耗尽升级告警(cdn 键),不提交任务
func TestCheckMetricsCollection_WriteFailureRetriesAlertNoSubmit(t *testing.T) {
	store := newFakeGateStore()
	store.claimErr[GateResourceCDN] = errors.New("mongo write timeout")
	alerter := &recordingAlerter{}
	g := newTestGate(store, alerter)
	g.maxWriteRetries = 2

	s, repo := newFlagTestScheduler(t, g, true)

	s.checkMetricsCollection()
	if n := countTasks(t, repo); n != 0 {
		t.Fatalf("日闸写失败不应提交任务,实际 %d 条", n)
	}
	if alerter.count() != 1 {
		t.Fatalf("写失败必须升级告警(非仅记日志),告警次数=%d", alerter.count())
	}
	if c := alerter.calls[0]; c.resourceType != GateResourceCDN || c.operation != "write" {
		t.Fatalf("告警参数不符: %+v", c)
	}

	// 退避窗口内下轮循环:仍不提交(不重复打存储)
	s.checkMetricsCollection()
	if n := countTasks(t, repo); n != 0 {
		t.Fatalf("写退避窗口内不应提交任务,实际 %d 条", n)
	}
}

// AC4:读失败注入——≥5 分钟退避窗口内多轮循环不重复提交、不重复读
func TestCheckMetricsCollection_ReadFailureBackoffNoRepeatSubmit(t *testing.T) {
	store := newFakeGateStore()
	store.getErr[GateResourceCDN] = errors.New("mongo down")
	g := newTestGate(store, &recordingAlerter{})

	now := time.Now()
	clock := now
	g.now = func() time.Time { return clock }

	s, repo := newFlagTestScheduler(t, g, true)

	if g.readBackoffWindow < 5*time.Minute {
		t.Fatalf("读失败退避窗口不得小于 5 分钟: %v", g.readBackoffWindow)
	}

	// 模拟分钟级循环连跑 5 轮(均在退避窗口内)
	s.checkMetricsCollection()
	clock = now.Add(1 * time.Minute)
	s.checkMetricsCollection()
	clock = now.Add(2 * time.Minute)
	s.checkMetricsCollection()
	clock = now.Add(3 * time.Minute)
	s.checkMetricsCollection()
	clock = now.Add(4 * time.Minute)
	s.checkMetricsCollection()

	if n := countTasks(t, repo); n != 0 {
		t.Fatalf("读失败退避窗口内不应提交任务,实际 %d 条", n)
	}
	if store.getCalls != 1 {
		t.Fatalf("退避窗口内应只读 1 次存储,实际 %d 次", store.getCalls)
	}
}

// AC2 迁移值回归:内存闸基线(日 15~17 各 1 条)→ 迁移到持久化闸后
// 无缺口(每日均有采集覆盖)、无历史重采(不回溯补采 days=2 以外的历史)、
// days=2 语义不变
func TestCDNMigrationValueRegression(t *testing.T) {
	store := newFakeGateStore()
	g := newTestGate(store, &recordingAlerter{})

	// 迁移前:内存闸(开关关闭)基线,日 15/16/17 各提交 1 条
	s, repo := newFlagTestScheduler(t, nil, false)
	cst := time.FixedZone("CST", 8*3600)
	clock := time.Date(2026, 9, 15, 10, 0, 0, 0, cst)
	s.nowFn = func() time.Time { return clock }

	for _, day := range []int{15, 16, 17} {
		clock = time.Date(2026, 9, day, 10, 0, 0, 0, cst)
		s.checkMetricsCollection()
	}
	if n := countTasks(t, repo); n != 3 {
		t.Fatalf("内存闸基线期应 3 天各 1 条共 3 条,实际 %d", n)
	}

	// 迁移:开关开启,挂上持久化闸(scheduler_state 尚无 cdn 记录 → 首部署过渡)
	s.dailyGate = g
	s.persistentGateEnabled = true

	// 迁移当日(17 日):首次认领 → 触发一次当日提交(days=2 只覆盖昨日+今日,
	// 不回溯补采更早历史);跨日 18/19 各 1 条
	for _, day := range []int{17, 18, 19} {
		clock = time.Date(2026, 9, day, 10, 0, 0, 0, cst)
		s.checkMetricsCollection()
	}

	// 无重复历史:17 日迁移过渡允许同日重采(upsert 幂等,值不重复),
	// 但不得出现对更早历史的额外回溯补采 → 总提交 = 3(基线) + 3(17/18/19) = 6
	if n := countTasks(t, repo); n != 6 {
		t.Fatalf("迁移后总提交应 6 条(3 基线 + 3 日闸),实际 %d", n)
	}

	// days=2 语义不变:迁移后所有任务仍 days=2
	tasks, _ := repo.List(context.Background(), taskx.TaskFilter{})
	for _, task := range tasks[3:] {
		if days, _ := task.Params["days"].(int); days != 2 {
			t.Fatalf("迁移后 days 语义应保持 2,实际 %v", task.Params["days"])
		}
	}

	// 无缺口:持久化闸 cdn 键按日连续推进,停在最后认领日
	// (基线 15/16/17 + 日闸 17/18/19,日期无间断)
	if store.lastDates[GateResourceCDN] != "2026-09-19" {
		t.Fatalf("cdn 键应连续推进至 2026-09-19(无缺口),实际 %q", store.lastDates[GateResourceCDN])
	}
}

// AC2 首部署过渡:scheduler_state 无 cdn 记录 → 首次认领触发一次当日提交,
// 不回溯补采历史;此后按「每日一次」语义运行
func TestCheckMetricsCollection_FirstDeployTransition(t *testing.T) {
	g := newTestGate(newFakeGateStore(), &recordingAlerter{})
	s, repo := newFlagTestScheduler(t, g, true)

	cst := time.FixedZone("CST", 8*3600)
	clock := time.Date(2026, 9, 19, 10, 0, 0, 0, cst)
	s.nowFn = func() time.Time { return clock }

	// 首部署:无记录 → 首次认领 → 恰好 1 次当日提交
	s.checkMetricsCollection()
	if n := countTasks(t, repo); n != 1 {
		t.Fatalf("首部署首次认领应触发一次当日提交,实际 %d", n)
	}

	// 同日多轮:不再提交(不重复)
	s.checkMetricsCollection()
	s.checkMetricsCollection()
	if n := countTasks(t, repo); n != 1 {
		t.Fatalf("首部署同日不应重复提交,实际 %d 条", n)
	}

	// 跨日:每日一次
	clock = time.Date(2026, 9, 20, 10, 0, 0, 0, cst)
	s.checkMetricsCollection()
	if n := countTasks(t, repo); n != 2 {
		t.Fatalf("跨日应提交 1 次(累计 2 条),实际 %d", n)
	}
}

// AC3 特性开关回滚:SCHEDULER_PERSISTENT_GATE_ENABLED 显式关闭 → CDN 走内存闸
// (同日幂等、跨日触发)、NAS 走内存闸回退,调度任务仍正常提交(采集不中断)
func TestRollbackSwitchNASAndCDNMemoryGate(t *testing.T) {
	// 日闸装配着也照样走内存闸(回滚不依赖日闸可用性)
	s, repo := newFlagTestScheduler(t, newTestGate(newFakeGateStore(), &recordingAlerter{}), false)

	cst := time.FixedZone("CST", 8*3600)
	clock := time.Date(2026, 9, 19, 10, 0, 0, 0, cst)
	s.nowFn = func() time.Time { return clock }

	// CDN 回滚内存闸:提交 1 条,同日不重复
	s.checkMetricsCollection()
	s.checkMetricsCollection()
	if n := countTasks(t, repo); n != 1 {
		t.Fatalf("回滚后 CDN 内存闸应提交 1 条且同日幂等,实际 %d", n)
	}

	// NAS 回滚内存闸:提交 1 条,同日不重复
	s.checkNASMetricsCollection()
	s.checkNASMetricsCollection()
	if n := countTasks(t, repo); n != 2 {
		t.Fatalf("回滚后 NAS 内存闸应提交 1 条且同日幂等(累计 2),实际 %d", n)
	}

	// 跨日:NAS/CDN 各再提交 1 条(指标采集不中断)
	clock = time.Date(2026, 9, 20, 10, 0, 0, 0, cst)
	s.checkMetricsCollection()
	s.checkNASMetricsCollection()
	if n := countTasks(t, repo); n != 4 {
		t.Fatalf("回滚后跨日 NAS/CDN 应各再提交 1 条(累计 4),实际 %d", n)
	}
}

// AC3 特性开关默认开启(未设置环境变量时);仅显式 false/0/off 关闭
func TestIsPersistentGateEnabled(t *testing.T) {
	cases := []struct {
		env  string
		want bool
	}{
		{"", true},           // 未设置:默认开启
		{"true", true},       // 显式开启
		{"1", true},          // 显式开启
		{"false", false},     // 显式关闭
		{"0", false},         // 显式关闭
		{"off", false},       // 显式关闭(大小写不敏感)
		{"FALSE", false},     // 大小写不敏感
		{"  false  ", false}, // 容忍空白
	}
	for _, c := range cases {
		t.Run("env="+c.env, func(t *testing.T) {
			if c.env == "" {
				t.Setenv(EnvPersistentGateEnabled, "")
			} else {
				t.Setenv(EnvPersistentGateEnabled, c.env)
			}
			if got := IsPersistentGateEnabled(); got != c.want {
				t.Fatalf("IsPersistentGateEnabled() with %q = %v, want %v", c.env, got, c.want)
			}
		})
	}
}

// AC1 补充:开关开启但未装配日闸(nil)→ 安全跳过不 panic,不降级回内存闸
func TestCheckMetricsCollection_NilGateSafe(t *testing.T) {
	s, repo := newFlagTestScheduler(t, nil, true)
	s.checkMetricsCollection()
	if n := countTasks(t, repo); n != 0 {
		t.Fatalf("未装配日闸不应提交任务,实际 %d 条", n)
	}
}
