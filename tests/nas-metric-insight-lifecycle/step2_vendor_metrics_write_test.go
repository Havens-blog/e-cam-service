// @feature nas-ops-insight @api-functional
//
// Contract step-2-vendor-metrics-write: 厂商指标按 GB 口径落库 — vendor byte
// values converted at the collect boundary land as capacity/used_capacity GB
// rows ( utilization never persisted ), vendor API failures stay isolated in
// Result.failures, capacity=0 rows land as zero_exception, and out-of-range
// capacity batches are rejected whole.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package nas_metric_insight_lifecycle

import (
	"errors"
	"testing"

	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// newJourneyHarness 构建带一个可指标厂商的 journey 世界。
func newJourneyHarness() (*nastest.Harness, *nastest.Provider) {
	h := nastest.NewHarness()
	return h, h.NewProvider(true)
}

// Outcome success: 每个活跃账号 × NAS 实例 × 当日各落一行 capacity/used
// (GB);utilization 不落库(模型无该字段,行结构即证);Result 的
// metrics_total 反映落库行数;数值落在 [1MB, 1PB] 数量级区间。
func TestStep2_VendorMetricsWrite_GBLandedRows(t *testing.T) {
	h, provider := newJourneyHarness()
	account := h.SeedAccount(1, provider)
	h.SeedInstance(1, provider, "fs-a", "共享盘A", "cn-hangzhou")
	// 厂商原始字节在采集边界换算 GB:16GiB=17179869184B → 16GB
	provider.Querier.SetMetrics(
		nastest.Metric("fs-a", "共享盘A", nastest.Today(), 16, 4),
	)

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Equal(t, 1, nastest.ResultField(t, task, "metrics_total").(int))

	row, ok := h.MetricDAO.Row(account.ID, "fs-a", nastest.Today())
	require.True(t, ok)
	require.Equal(t, float64(16), row.Capacity, "落库 capacity 为 GB 口径")
	require.Equal(t, float64(4), row.UsedCapacity)
	require.Equal(t, account.ID, row.AccountID, "行携带账号维度唯一键")
	require.Equal(t, provider.Name, row.Provider)
	require.GreaterOrEqual(t, row.Capacity, 1.0/1024, "数量级下界 1MB(GB 计)")
	require.LessOrEqual(t, row.Capacity, float64(1024*1024), "数量级上界 1PB(GB 计)")
}

// Outcome vendor-api-failure-isolated: 一家厂商宕机仅返回自身空结果,其余厂商
// 照常产出指标行,任务 Result 的 failures 含失败计数与末次错误,整体不失败。
func TestStep2_VendorMetricsWrite_VendorFailureIsolated(t *testing.T) {
	h, okProvider := newJourneyHarness()
	badProvider := h.NewProvider(true)

	h.SeedAccount(1, okProvider)
	h.SeedAccount(2, badProvider)
	h.SeedInstance(1, okProvider, "fs-ok", "健康盘", "cn-hangzhou")
	h.SeedInstance(2, badProvider, "fs-bad", "故障盘", "cn-shanghai")
	okProvider.Querier.SetMetrics(nastest.Metric("fs-ok", "健康盘", nastest.Today(), 16, 4))
	badProvider.Querier.Fail(errors.New("injected: monitor API down"))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	entry, found := nastest.FailureFor(nastest.ResultFailures(t, task), badProvider.Name)
	require.True(t, found)
	require.GreaterOrEqual(t, entry.ErrorCount, 1)
	require.Contains(t, entry.LastError, "monitor API down")

	require.Equal(t, 1, h.MetricDAO.Count(), "失败厂商实例不落库,其余厂商正常落库")
	_, ok := h.MetricDAO.Row(1, "fs-ok", nastest.Today())
	require.True(t, ok)
	_, ok = h.MetricDAO.Row(2, "fs-bad", nastest.Today())
	require.False(t, ok)
}

// Outcome zero-capacity-zero-exception: 厂商返回 capacity=0 不拦截不跳过,
// 行标记 qc_status=zero_exception 后正常落库,读取侧该日映射
// data_status=zero_exception、utilization 记 null。
func TestStep2_VendorMetricsWrite_ZeroCapacityZeroException(t *testing.T) {
	h, provider := newJourneyHarness()
	h.SeedAccount(1, provider)
	h.SeedInstance(1, provider, "fs-zero", "零容量盘", "cn-beijing")
	provider.Querier.SetMetrics(nastest.Metric("fs-zero", "零容量盘", nastest.Today(), 0, 0))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Equal(t, 1, nastest.ResultField(t, task, "metrics_total").(int))
	require.Empty(t, nastest.ResultFailures(t, task), "零容量是异常标注而非失败")

	row, ok := h.MetricDAO.Row(1, "fs-zero", nastest.Today())
	require.True(t, ok)
	require.Equal(t, types.NASMetricQcZeroException, row.QcStatus)
}

// Outcome capacity-out-of-range-rejected: 非零 capacity 越出 [1MB, 1PB]
// (字节直写 GB 字段的单位 bug 形态)整批拒绝,错误携带 fs_id 与 date,
// 不良行全部不得落库,执行器失败计数累加进 Result。
func TestStep2_VendorMetricsWrite_OutOfRangeRejected(t *testing.T) {
	h, provider := newJourneyHarness()
	h.SeedAccount(1, provider)
	h.SeedInstance(1, provider, "fs-bad", "越界盘", "cn-hangzhou")
	// 原始字节 1e12 直写 GB 字段的缺陷形态(远超 1PiB=1048576 GB)
	provider.Querier.SetMetrics(nastest.Metric("fs-bad", "越界盘", nastest.Today(), 1e12, 1e11))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err, "写入门禁拒绝不反噬任务整体(尽力而为语义)")

	require.Equal(t, 0, h.MetricDAO.Count(), "越界行不得落库")
	entry, found := nastest.FailureFor(nastest.ResultFailures(t, task), provider.Name)
	require.True(t, found, "执行器失败计数累加,末次错误进入 Result")
	require.Contains(t, entry.LastError, "fs-bad")
	require.Contains(t, entry.LastError, nastest.Today())
}
