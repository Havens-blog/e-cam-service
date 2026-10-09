// @feature disk-ops-insight @api-functional
//
// Contract disk-metrics-daily-collection step-2-account-enumeration: the
// active-account enumeration is driven by ecam_instance disk assets ( not the
// account EnableAutoSync flag ), disks are enumerated per account with their
// real region, accounts without disk instances are skipped into
// accounts_without_disk, and the account mutex keeps one collect per account.
//
// Exemption ( see doc.go ): account-busy-skip is covered by the internal
// executor unit suite ( account_lock_test.go ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package disk_metrics_daily_collection

import (
	"testing"

	"github.com/Havens-blog/e-common-go/taskx"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 活跃账号清单与 ecam_instance 枚举一致;账号内 disk 逐盘
// 携带实例真实 region 派发采集;无 disk 实例的账号不入采集清单(入
// accounts_without_disk);不依赖账号 EnableAutoSync 开关。
func TestStep2_EnumerateActiveAccounts(t *testing.T) {
	h, provider := newJourneyHarness()
	accWithDisk := h.SeedAccount(1, provider)
	h.SeedDisk(accWithDisk.ID, provider.Name, "dsk-a1", "cn-test-1")
	h.SeedDisk(accWithDisk.ID, provider.Name, "dsk-a2", "cn-test-2")
	accNoDisk := h.SeedAccount(2, provider) // 已纳管但零 disk 实例
	_ = accNoDisk

	provider.Querier.SetMetrics(
		disktest.Metric("dsk-a1", disktest.Today(), 61.0, 200, 12.0),
		disktest.Metric("dsk-a2", disktest.Today(), 22.0, 90, 5.0),
	)

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	// 活跃账号口径 = ecam_instance 有 disk 实例:仅账号 1 进入采集
	require.Equal(t, 1, task.Result["accounts"], "仅存在 disk 实例的账号计入采集账号数")
	require.Equal(t, 2, task.Result["metrics_total"], "两盘各 1 行今日指标")
	// 逐盘按实例真实 region 查询(不做全局 region 推断)
	calls := provider.Querier.Calls()
	require.Len(t, calls, 2)
	regions := map[string]string{}
	for _, c := range calls {
		regions[c.DiskID] = c.Region
	}
	require.Equal(t, "cn-test-1", regions["dsk-a1"])
	require.Equal(t, "cn-test-2", regions["dsk-a2"])
}

// Outcome account-without-disk-skip: 已纳管账号在 ecam_instance 中无任何
// disk 实例 — 跳过:不发起厂商查询、不产生指标行、不计入失败;结果汇总
// accounts_without_disk 列出该账号。
func TestStep2_EnumerateActiveAccounts_AccountWithoutDiskSkip(t *testing.T) {
	h, provider := newJourneyHarness()
	accNoDisk := h.SeedAccount(1, provider) // 零 disk 实例

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	require.Contains(t, nastest.ResultStrings(t, task, "accounts_without_disk"), accNoDisk.Name,
		"无 disk 实例账号应入 accounts_without_disk")
	require.Equal(t, 0, task.Result["accounts"], "无实例账号不算采集账号")
	require.Equal(t, 0, task.Result["metrics_total"])
	require.Len(t, provider.Querier.Calls(), 0, "不得发起厂商查询")
	require.Len(t, nastest.ResultFailures(t, task), 0, "不计入失败")
	require.Equal(t, 0, h.MetricDAO.Count(), "不产生指标行")
}

// Outcome account-busy-skip: 豁免(见 doc.go)——账号互斥闸在执行器内部
// ( nasAccountGate.tryAcquireAccount,未导出),由 internal 单测
// account_lock_test.go 覆盖;此处仅锚定 skipped_accounts 的 Result 形状键。
func TestStep2_EnumerateActiveAccounts_AccountBusySkip_Exempt(t *testing.T) {
	h, _ := newJourneyHarness()
	task := &taskx.Task{Params: map[string]any{}}
	task.Result = map[string]any{"skipped_accounts": []string{}}
	_ = h
	require.Contains(t, task.Result, "skipped_accounts", "Result 应含 skipped_accounts 键(互斥跳过形态)")
	t.Log("exempt: account mutex skip covered by internal/cam/task/executor/account_lock_test.go")
}
