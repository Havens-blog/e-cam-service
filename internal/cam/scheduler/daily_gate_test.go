package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gotomicro/ego/core/elog"
)

func testLogger() *elog.Component {
	return elog.DefaultLogger
}

// 持久化日闸单测(spec「持久化日闸 + 原子认领」):
//   - 原子认领:并发同触发只一个成功;
//   - 首次认领过渡:无记录视为首次认领,认领后当日不重复;
//   - 写失败:指数退避重试 + 升级告警(非仅记日志),退避窗口内不再打存储;
//   - 读失败:≥5 分钟退避窗口内不重读,防分钟级循环洪泛。

// fakeGateStore 内存原子日闸存储(模拟 findOneAndUpdate 原子性)
type fakeGateStore struct {
	mu         sync.Mutex
	lastDates  map[string]string
	getErr     map[string]error // resource_type -> 注入读失败
	claimErr   map[string]error // resource_type -> 注入写失败
	getCalls   int
	claimCalls int
}

func newFakeGateStore() *fakeGateStore {
	return &fakeGateStore{
		lastDates: make(map[string]string),
		getErr:    make(map[string]error),
		claimErr:  make(map[string]error),
	}
}

func (f *fakeGateStore) GetLastDate(_ context.Context, resourceType string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	if err := f.getErr[resourceType]; err != nil {
		return "", err
	}
	return f.lastDates[resourceType], nil
}

// TryClaimDaily 原子模拟:同格式 YYYY-MM-DD 字符串比较与 mongo $lt 语义一致
func (f *fakeGateStore) TryClaimDaily(_ context.Context, resourceType, date string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claimCalls++
	if err := f.claimErr[resourceType]; err != nil {
		return false, err
	}
	if f.lastDates[resourceType] >= date {
		return false, nil
	}
	f.lastDates[resourceType] = date
	return true, nil
}

// recordingAlerter 记录升级告警调用
type recordingAlerter struct {
	mu    sync.Mutex
	calls []gateAlertCall
}

type gateAlertCall struct {
	resourceType string
	operation    string
	failures     int
	err          error
}

func (a *recordingAlerter) AlertDailyGateFailure(_ context.Context, resourceType, operation string, consecutiveFailures int, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, gateAlertCall{resourceType, operation, consecutiveFailures, err})
}

func (a *recordingAlerter) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.calls)
}

func newTestGate(store DailyGateStore, alerter DailyGateAlerter) *PersistentDailyGate {
	g := NewPersistentDailyGate(store, alerter, testLogger())
	// 单测收窄窗口与重试间隔,避免真实睡眠
	g.writeRetryDelay = time.Millisecond
	return g
}

// AC1:首次无记录视为首次认领;认领后当日不重复
func TestPersistentDailyGate_FirstClaimTransition(t *testing.T) {
	store := newFakeGateStore()
	g := newTestGate(store, &recordingAlerter{})
	ctx := context.Background()

	claimed, err := g.TryClaim(ctx, GateResourceNAS, "2026-09-19")
	if err != nil || !claimed {
		t.Fatalf("首次认领应成功: claimed=%v err=%v", claimed, err)
	}

	// 同日第二次:已认领,不再触发写
	claimed, err = g.TryClaim(ctx, GateResourceNAS, "2026-09-19")
	if err != nil || claimed {
		t.Fatalf("同日二次认领应返回 false: claimed=%v err=%v", claimed, err)
	}
	if store.claimCalls != 1 {
		t.Fatalf("同日不应重复写认领: claimCalls=%d", store.claimCalls)
	}

	// 跨日重新认领
	claimed, err = g.TryClaim(ctx, GateResourceNAS, "2026-09-20")
	if err != nil || !claimed {
		t.Fatalf("跨日认领应成功: claimed=%v err=%v", claimed, err)
	}

	// 资源类型分键:nas/cdn 独立,互不覆盖(Hard Rule)
	if _, err := store.GetLastDate(ctx, GateResourceCDN); err != nil {
		t.Fatalf("查询 cdn 键失败: %v", err)
	}
	claimedCDN, err := g.TryClaim(ctx, GateResourceCDN, "2026-09-19")
	if err != nil || !claimedCDN {
		t.Fatalf("cdn 键首次认领应成功(不受 nas 影响): claimed=%v err=%v", claimedCDN, err)
	}
}

// AC2:多 goroutine 同触发只一个成功
func TestPersistentDailyGate_ConcurrentClaimOnlyOneWins(t *testing.T) {
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
			claimed, err := g.TryClaim(ctx, GateResourceNAS, "2026-09-19")
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
		t.Fatalf("并发 %d 次认领应只有 1 个成功,实际 %d", n, winCount)
	}
	if store.claimCalls < 1 {
		t.Fatal("至少应有一次原子写尝试")
	}
}

// AC3 读路径:GetLastDate 失败 → ≥5 分钟退避窗口,窗口内不重读不提交
func TestPersistentDailyGate_ReadFailureBackoff(t *testing.T) {
	store := newFakeGateStore()
	store.getErr[GateResourceNAS] = errors.New("mongo down")
	g := newTestGate(store, &recordingAlerter{})

	now := time.Now()
	clock := now
	g.now = func() time.Time { return clock }

	if g.readBackoffWindow < 5*time.Minute {
		t.Fatalf("读失败退避窗口不得小于 5 分钟: %v", g.readBackoffWindow)
	}

	ctx := context.Background()
	if _, err := g.TryClaim(ctx, GateResourceNAS, "2026-09-19"); err == nil {
		t.Fatal("读失败应返回错误")
	}
	if g.readBackoffWindow != 5*time.Minute {
		t.Fatalf("默认读退避窗口应为 5 分钟,实际 %v", g.readBackoffWindow)
	}

	getCallsAfterFirst := store.getCalls
	claimed, err := g.TryClaim(ctx, GateResourceNAS, "2026-09-19")
	if err != nil {
		t.Fatalf("退避窗口内应静默跳过,不应报错: %v", err)
	}
	if claimed {
		t.Fatal("退避窗口内不应认领")
	}
	if store.getCalls != getCallsAfterFirst {
		t.Fatalf("退避窗口内不应重读: getCalls %d -> %d", getCallsAfterFirst, store.getCalls)
	}

	// 推进时钟越过退避窗口 → 恢复读
	clock = now.Add(6 * time.Minute)
	if _, err := g.TryClaim(ctx, GateResourceNAS, "2026-09-19"); err == nil {
		t.Fatal("越过退避窗口后应重试读(仍注入读失败,应再报错)")
	}
	if store.getCalls != getCallsAfterFirst+1 {
		t.Fatalf("越过退避窗口后应恰好重读一次: %d -> %d", getCallsAfterFirst, store.getCalls)
	}
}

// AC3 写路径:写失败单轮指数退避重试,重试耗尽升级告警(非仅记日志),
// 并进入跨轮退避窗口;连续失败升级计数递增;恢复后计数清零
func TestPersistentDailyGate_WriteFailureRetryBackoffAlert(t *testing.T) {
	store := newFakeGateStore()
	store.claimErr[GateResourceNAS] = errors.New("mongo write timeout")
	alerter := &recordingAlerter{}
	g := newTestGate(store, alerter)
	g.maxWriteRetries = 2 // 首次 + 2 次重试 = 3 次尝试

	now := time.Now()
	clock := now
	g.now = func() time.Time { return clock }
	ctx := context.Background()

	// 第一轮:重试耗尽 → 告警 failures=1
	claimed, err := g.TryClaim(ctx, GateResourceNAS, "2026-09-19")
	if err == nil || claimed {
		t.Fatalf("写失败应报错且不认领: claimed=%v err=%v", claimed, err)
	}
	if store.claimCalls != 3 {
		t.Fatalf("首次 + 2 次重试应共 3 次写尝试,实际 %d", store.claimCalls)
	}
	if alerter.count() != 1 {
		t.Fatalf("写失败必须升级告警(非仅记日志),告警次数=%d", alerter.count())
	}
	if c := alerter.calls[0]; c.resourceType != GateResourceNAS || c.operation != "write" || c.failures != 1 {
		t.Fatalf("告警参数不符: %+v", c)
	}

	// 跨轮退避窗口内:不打存储、不重复告警
	claimCallsAfterRound1 := store.claimCalls
	alertsAfterRound1 := alerter.count()
	if claimed, err := g.TryClaim(ctx, GateResourceNAS, "2026-09-19"); err != nil || claimed {
		t.Fatalf("写退避窗口内应静默跳过: claimed=%v err=%v", claimed, err)
	}
	if store.claimCalls != claimCallsAfterRound1 || alerter.count() != alertsAfterRound1 {
		t.Fatal("写退避窗口内不应触碰存储或重复告警")
	}

	// 推进时钟:第二仍失败 → failures=2,告警升级计数递增
	clock = now.Add(2 * time.Minute)
	if _, err := g.TryClaim(ctx, GateResourceNAS, "2026-09-19"); err == nil {
		t.Fatal("第二轮写失败应报错")
	}
	if alerter.count() != 2 {
		t.Fatalf("第二轮失败应再次告警,告警次数=%d", alerter.count())
	}
	if c := alerter.calls[1]; c.failures != 2 {
		t.Fatalf("连续失败计数应升级为 2,实际 %d", c.failures)
	}

	// 恢复:写成功后计数清零,再失败时从 1 重新计数
	store.claimErr[GateResourceNAS] = nil
	clock = now.Add(10 * time.Minute)
	if claimed, err := g.TryClaim(ctx, GateResourceNAS, "2026-09-19"); err != nil || !claimed {
		t.Fatalf("恢复后认领应成功: claimed=%v err=%v", claimed, err)
	}
	if alerter.count() != 2 {
		t.Fatalf("恢复不应触发告警,告警次数=%d", alerter.count())
	}
	store.claimErr[GateResourceNAS] = errors.New("mongo write timeout again")
	store.lastDates[GateResourceNAS] = "" // 模拟跨日重新认领
	clock = now.Add(20 * time.Minute)
	if _, err := g.TryClaim(ctx, GateResourceNAS, "2026-09-20"); err == nil {
		t.Fatal("恢复后再失败应报错")
	}
	if c := alerter.calls[alerter.count()-1]; c.failures != 1 {
		t.Fatalf("恢复后失败计数应从 1 重新计,实际 %d", c.failures)
	}
}

// 读成功但已被他实例认领(false, nil)不触发告警,且写失败计数清零
func TestPersistentDailyGate_AlreadyClaimedNoAlert(t *testing.T) {
	store := newFakeGateStore()
	store.lastDates[GateResourceNAS] = "2026-09-19"
	alerter := &recordingAlerter{}
	g := newTestGate(store, alerter)
	ctx := context.Background()

	claimed, err := g.TryClaim(ctx, GateResourceNAS, "2026-09-19")
	if err != nil || claimed {
		t.Fatalf("已被认领应返回 false,nil: %v %v", claimed, err)
	}
	if alerter.count() != 0 {
		t.Fatal("已被认领不是故障,不应告警")
	}
}
