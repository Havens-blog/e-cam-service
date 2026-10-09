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

// NAS 每日采集接入单测(spec AC「NAS 每日采集接入」):
// 调度器分钟级循环经持久化日闸原子认领,认领成功才提交 nas:collect_metrics
// (days=2);认领失败/日闸故障不提交;未装配日闸时安全跳过。

// mockTaskRepo 仅实现 Create 的任务仓储桩
type mockTaskRepo struct {
	mu    sync.Mutex
	tasks []taskx.Task
}

func (m *mockTaskRepo) Create(_ context.Context, t taskx.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks = append(m.tasks, t)
	return nil
}

func (m *mockTaskRepo) GetByID(_ context.Context, _ string) (taskx.Task, error) {
	return taskx.Task{}, errors.New("not implemented")
}
func (m *mockTaskRepo) Update(_ context.Context, _ taskx.Task) error { return nil }
func (m *mockTaskRepo) UpdateStatus(_ context.Context, _ string, _ taskx.TaskStatus, _ string) error {
	return nil
}
func (m *mockTaskRepo) UpdateProgress(_ context.Context, _ string, _ int, _ string) error {
	return nil
}
func (m *mockTaskRepo) List(_ context.Context, _ taskx.TaskFilter) ([]taskx.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]taskx.Task(nil), m.tasks...), nil
}
func (m *mockTaskRepo) Count(_ context.Context, _ taskx.TaskFilter) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.tasks)), nil
}
func (m *mockTaskRepo) Delete(_ context.Context, _ string) error { return nil }

// dummyNASExecutor 让队列允许提交 nas:collect_metrics(测试不启动 worker)
type dummyNASExecutor struct{}

func (d *dummyNASExecutor) Execute(_ context.Context, _ *taskx.Task) error { return nil }
func (d *dummyNASExecutor) GetType() taskx.TaskType                        { return executor.TaskTypeNASCollectMetrics }

func newTestScheduler(t *testing.T, gate *PersistentDailyGate) (*AutoSyncScheduler, *mockTaskRepo) {
	t.Helper()
	repo := &mockTaskRepo{}
	queue := taskx.NewQueue(repo, testLogger(), taskx.Config{WorkerNum: 1, BufferSize: 10})
	queue.RegisterExecutor(&dummyNASExecutor{})
	return NewAutoSyncScheduler(nil, queue, testLogger(), gate, true), repo
}

// 认领成功 → 恰好提交 1 条 nas:collect_metrics(days=2);同日第二轮不重复提交
func TestCheckNASMetricsCollection_SubmitsOnClaim(t *testing.T) {
	g := newTestGate(newFakeGateStore(), &recordingAlerter{})
	s, repo := newTestScheduler(t, g)

	s.checkNASMetricsCollection()

	tasks, err := repo.List(context.Background(), taskx.TaskFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("认领成功应恰好提交 1 条任务,实际 %d", len(tasks))
	}
	task := tasks[0]
	if task.Type != executor.TaskTypeNASCollectMetrics {
		t.Fatalf("任务类型应为 nas:collect_metrics,实际 %s", task.Type)
	}
	if days, _ := task.Params["days"].(int); days != 2 {
		t.Fatalf("days 语义应为 2(补昨日完整行+今日初态),实际 %v", task.Params["days"])
	}

	// 同日第二轮:日闸已认领,不重复提交(服务重启亦由持久化闸保证)
	s.checkNASMetricsCollection()
	tasks, _ = repo.List(context.Background(), taskx.TaskFilter{})
	if len(tasks) != 1 {
		t.Fatalf("同日不应重复提交,实际 %d 条", len(tasks))
	}
}

// 日闸读失败/写失败:不提交任务
func TestCheckNASMetricsCollection_GateFailureNoSubmit(t *testing.T) {
	store := newFakeGateStore()
	store.getErr[GateResourceNAS] = errors.New("mongo down")
	g := newTestGate(store, &recordingAlerter{})
	now := time.Now()
	clock := now
	g.now = func() time.Time { return clock }
	s, repo := newTestScheduler(t, g)

	// 读失败
	s.checkNASMetricsCollection()
	if n, _ := repo.Count(context.Background(), taskx.TaskFilter{}); n != 0 {
		t.Fatalf("日闸读失败不应提交任务,实际 %d 条", n)
	}

	// 写失败(推进时钟越过读退避窗口)
	clock = now.Add(6 * time.Minute)
	store.getErr[GateResourceNAS] = nil
	store.claimErr[GateResourceNAS] = errors.New("mongo write timeout")
	s.checkNASMetricsCollection()
	if n, _ := repo.Count(context.Background(), taskx.TaskFilter{}); n != 0 {
		t.Fatalf("日闸写失败不应提交任务,实际 %d 条", n)
	}
}

// 并发触发:同一资源只被一个实例认领 → 任务队列只多一条
func TestCheckNASMetricsCollection_ConcurrentTriggerSingleSubmit(t *testing.T) {
	g := newTestGate(newFakeGateStore(), &recordingAlerter{})
	s, repo := newTestScheduler(t, g)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.checkNASMetricsCollection()
		}()
	}
	wg.Wait()

	if n, _ := repo.Count(context.Background(), taskx.TaskFilter{}); n != 1 {
		t.Fatalf("并发触发应只提交 1 条任务,实际 %d 条", n)
	}
}

// 未装配日闸(nil):安全跳过不 panic
func TestCheckNASMetricsCollection_NilGateSafe(t *testing.T) {
	s, repo := newTestScheduler(t, nil)
	s.checkNASMetricsCollection()
	if n, _ := repo.Count(context.Background(), taskx.TaskFilter{}); n != 0 {
		t.Fatalf("未装配日闸不应提交任务,实际 %d 条", n)
	}
}
