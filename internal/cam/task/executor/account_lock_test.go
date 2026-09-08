package executor

import "testing"

// 回归:同一账号允许并发多个同步任务(手动连点+调度器+重试叠加),
// 多个全量同步同时对同一账号跑会浪费厂商 API 配额并互相拖慢。
// 账号级互斥:同一账号同时只允许一个任务持有;不同账号互不影响;释放后可再次获取。
func TestAccountSyncLock(t *testing.T) {
	e := &SyncAssetsExecutor{syncingNow: make(map[int64]string)}

	// 不同账号可并行
	if !e.tryAcquireAccount(1, "taskA") {
		t.Fatal("账号1首次获取应成功")
	}
	if !e.tryAcquireAccount(2, "taskA") {
		t.Fatal("账号2获取应成功(与账号1互不影响)")
	}

	// 同一账号第二个任务必须被拒
	if e.tryAcquireAccount(1, "taskB") {
		t.Fatal("账号1被 taskA 持有,taskB 获取应失败")
	}

	// 持有者重复获取自己应幂等成功(防御性)
	if !e.tryAcquireAccount(1, "taskA") {
		t.Fatal("持有者 taskA 重复获取应成功")
	}

	// 释放非持有者不应误删
	e.releaseAccount(1, "taskB")
	if e.tryAcquireAccount(1, "taskC") {
		t.Fatal("taskB 释放不属于它的锁后,taskC 仍不应获取成功")
	}

	// 持有者释放后可被其他任务获取
	e.releaseAccount(1, "taskA")
	if !e.tryAcquireAccount(1, "taskC") {
		t.Fatal("taskA 释放后,taskC 应获取成功")
	}
}
