// @feature disk-ops-insight @api-functional
//
// Contract disk-metrics-daily-collection step-3-vendor-metric-query: per-vendor
// region-scoped metric queries with executor-side AccountID/Provider backfill
// and passthrough of adapter qc_status/usage_scope annotations; probe-
// unsupported vendors degrade to INFO + no_metric_support ( not a failure ),
// and adapter call failures count into failed_disks + failures without
// blocking other vendors/accounts.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package disk_metrics_daily_collection

import (
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 按实例真实 region 调用 GetDiskMetrics,区间默认[昨日,今日];
// 返回归一化指标(usage_percent 0~100 + usage_scope 口径标注,iops 次/秒,
// throughput MB/s);执行器回填 AccountID/Provider,透传 qc_status/usage_scope
// 标注不篡改。
func TestStep3_VendorQuery_Success(t *testing.T) {
	h, provider := newJourneyHarness()
	acc := h.SeedAccount(1, provider)
	h.SeedDisk(acc.ID, provider.Name, "dsk-q1", "cn-test-9")

	// 归一化指标(usage_scope=instance_level 口径标注,无 qc 标注=正常)
	provider.Querier.SetMetrics(disktest.Metric("dsk-q1", disktest.Today(), 48.5, 130, 9.25))

	_, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	// 查询区间 [昨日,今日](days=2),region 透传实例 attributes
	calls := provider.Querier.Calls()
	require.Len(t, calls, 1)
	require.Equal(t, "dsk-q1", calls[0].DiskID)
	require.Equal(t, "cn-test-9", calls[0].Region)
	require.Equal(t, disktest.DayOffset(-1), calls[0].Start, "采集区间起点应为昨日(days=2)")
	require.Equal(t, disktest.Today(), calls[0].End)

	// 执行器回填 AccountID/Provider,usage_scope 透传
	row, ok := h.MetricDAO.Row(acc.ID, "dsk-q1", disktest.Today())
	require.True(t, ok)
	require.Equal(t, acc.ID, row.AccountID, "执行器应回填 account_id")
	require.Equal(t, provider.Name, row.Provider, "执行器应回填 provider")
	require.Equal(t, types.DiskUsageScopeInstanceLevel, row.UsageScope, "usage_scope 标注透传不篡改")
	require.Equal(t, "", row.QcStatus, "非零使用率行 qc 保持正常空值")
	require.InDelta(t, 48.5, row.UsagePercent, 1e-9)
	require.InDelta(t, 130, row.IOPS, 1e-9)
	require.InDelta(t, 9.25, row.Throughput, 1e-9)
}

// Outcome metric-unsupported-info: 尽力而为厂商(tencent)适配器未实现
// DiskMetricQuerier — INFO 语义整体返回空,不计失败、不触发告警;结果汇总
// no_metric_support 列出该厂商;不写入任何指标行。
func TestStep3_VendorQuery_MetricUnsupportedInfo(t *testing.T) {
	h := disktest.NewHarness()
	// 命中生产「尽力而为厂商」字面量:探测不支持形态(未实现 DiskMetricQuerier)
	plain := h.NewNamedProvider("tencent", false)
	acc := h.SeedAccount(1, plain)
	h.SeedDisk(acc.ID, plain.Name, "dsk-t1", "cn-test-1")

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	require.Equal(t, []string{"tencent"}, nastest.ResultStrings(t, task, "no_metric_support"),
		"探测不支持厂商应入 no_metric_support 清单")
	require.Len(t, nastest.ResultFailures(t, task), 0, "探测不支持不计失败")
	require.Empty(t, h.HealthAlerter.Calls(), "探测不支持不触发告警")
	require.Equal(t, 0, task.Result["failed_disks"])
	require.Equal(t, 0, h.MetricDAO.Count(), "不写入该厂商任何指标行")
}

// Outcome adapter-call-failure: 厂商监控 API 调用返回错误 — ERROR 留痕,该盘
// 计入 failed_disks,失败明细(provider/account_id/error_count/last_error)汇入
// Result.failures;该盘无指标行;其余厂商/账号采集不受影响。
func TestStep3_VendorQuery_AdapterCallFailure(t *testing.T) {
	h, provider := newJourneyHarness()
	// 故障账号(查询失败)
	accBad := h.SeedAccount(1, provider)
	h.SeedDisk(accBad.ID, provider.Name, "dsk-bad", "cn-test-1")
	provider.Querier.FailDisk("dsk-bad", errors.New("injected: vendor monitor API error"))
	// 健康账号:失败隔离语义(不同厂商)
	okProvider := h.NewProvider(true)
	accOk := h.SeedAccount(2, okProvider)
	h.SeedDisk(accOk.ID, okProvider.Name, "dsk-good", "cn-test-1")
	okProvider.Querier.SetMetrics(disktest.Metric("dsk-good", disktest.Today(), 33.0, 70, 4.0))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err, "单盘失败不得让任务整体失败")

	require.Equal(t, 1, task.Result["failed_disks"], "失败盘应计入 failed_disks")
	failures := nastest.ResultFailures(t, task)
	require.NotEmpty(t, failures, "失败明细必须可观测")
	var badFailure *nastest.ProviderFailure
	for i := range failures {
		if failures[i].Provider == provider.Name {
			badFailure = &failures[i]
		}
	}
	require.NotNil(t, badFailure, "失败明细应含故障厂商")
	require.Equal(t, accBad.ID, badFailure.AccountID)
	require.GreaterOrEqual(t, badFailure.ErrorCount, 1)
	require.NotEmpty(t, badFailure.LastError)

	// 失败盘无指标行;健康盘照常落库(失败隔离)
	_, ok := h.MetricDAO.Row(accBad.ID, "dsk-bad", disktest.Today())
	require.False(t, ok, "失败盘不得落库")
	_, ok = h.MetricDAO.Row(accOk.ID, "dsk-good", disktest.Today())
	require.True(t, ok, "其余厂商/账号的采集不受影响")
	require.Equal(t, 1, task.Result["metrics_total"])
}
