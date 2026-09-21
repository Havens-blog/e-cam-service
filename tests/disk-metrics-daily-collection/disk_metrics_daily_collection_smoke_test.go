// @feature disk-ops-insight @api-functional
//
// Journey smoke for disk-metrics-daily-collection: the golden path end-to-end —
// gate claim → account enumeration → vendor query → metric persistence →
// result summary → the user reads the disk trend through the API.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package disk_metrics_daily_collection

import (
	"encoding/json"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

func TestDiskMetricsDailyCollection_FullJourneySmoke(t *testing.T) {
	h, provider := newJourneyHarness()
	acc := h.SeedAccount(1, provider)
	h.SeedDisk(acc.ID, provider.Name, "dsk-smoke", "cn-test-1")
	provider.Querier.SetMetrics(
		disktest.Metric("dsk-smoke", disktest.Today(), 66.0, 150, 11.0),
		disktest.Metric("dsk-smoke", disktest.DayOffset(-1), 58.0, 140, 10.0),
	)

	// Step 1: 调度器认领当日 disk 日闸 → 提交 days=2 采集任务
	gate := newGate(h)
	queue := newTaskQueue(h)
	require.True(t, submitDiskDailyCollect(t, h, gate, queue), "step1: 日闸认领应成功")
	requireSubmittedDiskDailyTasks(t, h, 1, "step1: 应恰好一条采集任务")

	// Step 2+3: 账号遍历 + 逐厂商查询(执行器直接执行任务)
	task, err := h.RunCollect(t, nil)
	require.NoError(t, err, "step2/3: 采集应成功")
	require.Len(t, provider.Querier.Calls(), 1, "step3: 应按实例 region 逐盘查询")
	require.Equal(t, "cn-test-1", provider.Querier.Calls()[0].Region)

	// Step 4: 指标落库(唯一键行,全字段)
	require.Equal(t, 2, task.Result["metrics_total"], "step4: 今日行+昨日行应落库")
	row, ok := h.MetricDAO.Row(acc.ID, "dsk-smoke", disktest.Today())
	require.True(t, ok, "step4: 今日行应存在")
	require.Equal(t, acc.ID, row.AccountID)

	// Step 5: 结果汇总可观测
	require.Equal(t, 100, task.Progress, "step5: progress 应达 100")
	require.Contains(t, task.Result, "failures", "step5: Result 应含失败明细键")

	// Step 6: 用户查看单盘趋势(API 面读取自己的采集数据)
	router := h.NewDiskRouter(1)
	status, env := disktest.GetJSON(t, router,
		"/assets/disk/metrics?disk_id=dsk-smoke&account_id=1&days=7")
	require.Equal(t, 200, status, "step6: 趋势查询应 200")
	var resp service.DiskMetricsResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.NotNil(t, resp.Latest)
	require.InDelta(t, 66.0, *resp.Latest.UsagePercent, 1e-9, "step6: 今日采集行应可见")

	// Journey Invariants: 唯一键恒成立 + 数据来源唯一 + 失败隔离(零失败)
	require.Equal(t, 2, h.MetricDAO.Count(), "invariant: (account_id, disk_id, date) 至多两行(两日)")
	require.Empty(t, nastest.ResultFailures(t, task), "invariant: 正常旅程零失败")
}
