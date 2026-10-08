package executor

import "testing"

// 回归:同一账号同一资产类型的并发多个同步任务(手动连点+调度器+重试叠加)
// 会浪费厂商 API 配额并互相拖慢,故按「账号×资产类型」互斥:
//   - 同账号同类型同时只放行一个任务;
//   - 同账号不同类型互不阻塞(全量同步跑 OSS/RDS 时,WAF 类型锁空闲);
//   - 不同账号互不影响;
//   - 释放后可再次获取,非持有者释放不误删。
func TestAccountSyncLock(t *testing.T) {
	e := &SyncAssetsExecutor{syncingNow: make(map[string]string)}

	// 不同账号可并行
	if !e.tryAcquireAccount(1, "ecs", "taskA") {
		t.Fatal("账号1首次获取应成功")
	}
	if !e.tryAcquireAccount(2, "ecs", "taskA") {
		t.Fatal("账号2获取应成功(与账号1互不影响)")
	}

	// 同一账号同一类型第二个任务必须被拒
	if e.tryAcquireAccount(1, "ecs", "taskB") {
		t.Fatal("账号1的 ecs 被 taskA 持有,taskB 获取应失败")
	}

	// 同一账号不同类型可并行(核心:类型级隔离)
	if !e.tryAcquireAccount(1, "waf", "taskB") {
		t.Fatal("账号1的 waf 类型应与 ecs 类型互不阻塞")
	}

	// 持有者重复获取自己应幂等成功(防御性)
	if !e.tryAcquireAccount(1, "ecs", "taskA") {
		t.Fatal("持有者 taskA 重复获取应成功")
	}

	// 释放非持有者不应误删
	e.releaseAccount(1, "ecs", "taskB")
	if e.tryAcquireAccount(1, "ecs", "taskC") {
		t.Fatal("taskB 释放不属于它的锁后,taskC 仍不应获取成功")
	}

	// 持有者释放后可被其他任务获取
	e.releaseAccount(1, "ecs", "taskA")
	if !e.tryAcquireAccount(1, "ecs", "taskC") {
		t.Fatal("taskA 释放后,taskC 应获取成功")
	}
}
