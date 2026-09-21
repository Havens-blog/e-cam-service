// @feature disk-ops-insight @api-functional
//
// Contract disk-metrics-daily-collection step-4-metric-persistence: today rows
// go through BulkInsertIfAbsent ( first write wins ), past rows through
// BulkUpsertMetrics ( overwrite for next-day backfill ), usage_percent=0 rows
// are forced to qc_status=zero_exception and stored visibly ( no CDN-style
// all-zero filtering ), and any out-of-range row rejects the whole batch with
// a disk_id/date-carrying error.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package disk_metrics_daily_collection

import (
	"strings"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 当日唯一键无已存在行 — 今日行经 BulkInsertIfAbsent、昨日行
// 经 BulkUpsertMetrics 写入;每盘每日 ≥1 行落库,含全字段;usage_percent=0 行
// 由写路径门禁强制打 qc_status=zero_exception。
func TestStep4_MetricPersistence_Success(t *testing.T) {
	h, provider := newJourneyHarness()
	acc := h.SeedAccount(1, provider)
	h.SeedDisk(acc.ID, provider.Name, "dsk-p1", "cn-test-1")

	// 今日初态行 + 昨日完整行(days=2 补采语义)
	provider.Querier.SetMetrics(
		disktest.Metric("dsk-p1", disktest.Today(), 55.0, 100, 8.0),
		disktest.Metric("dsk-p1", disktest.DayOffset(-1), 70.0, 150, 10.5),
	)

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Equal(t, 2, task.Result["metrics_total"], "今日行+昨日行各 1 条")

	// 唯一键 (account_id, disk_id, date) 行建立,全字段完整
	today, ok := h.MetricDAO.Row(acc.ID, "dsk-p1", disktest.Today())
	require.True(t, ok)
	require.Equal(t, "dsk-p1", today.DiskID)
	require.Equal(t, acc.ID, today.AccountID)
	require.Equal(t, provider.Name, today.Provider)
	require.InDelta(t, 55.0, today.UsagePercent, 1e-9)
	require.Equal(t, types.DiskUsageScopeInstanceLevel, today.UsageScope)
	require.InDelta(t, 100, today.IOPS, 1e-9)
	require.InDelta(t, 8.0, today.Throughput, 1e-9)

	yesterday, ok := h.MetricDAO.Row(acc.ID, "dsk-p1", disktest.DayOffset(-1))
	require.True(t, ok, "昨日完整行应落库")
	require.InDelta(t, 70.0, yesterday.UsagePercent, 1e-9)
	require.Equal(t, 2, h.MetricDAO.Count())
}

// Outcome same-day-idempotent: 当日该键行已存在 — 今日行保持首写结果不被
// 覆盖($setOnInsert 仅缺失时插入);昨日及更早行按覆盖更新语义刷新。
func TestStep4_MetricPersistence_SameDayIdempotent(t *testing.T) {
	h, provider := newJourneyHarness()
	acc := h.SeedAccount(1, provider)
	h.SeedDisk(acc.ID, provider.Name, "dsk-p2", "cn-test-1")

	// 当日首写结果(此前调度重跑已写入)
	h.SeedMetric(t, acc.ID, provider.Name, disktest.Metric("dsk-p2", disktest.Today(), 77.0, 300, 20.0))

	// 再次采集:今日值 55(不得覆盖 77),昨日值 33(覆盖既有 40)
	h.SeedMetric(t, acc.ID, provider.Name, disktest.Metric("dsk-p2", disktest.DayOffset(-1), 40.0, 10, 1.0))
	provider.Querier.SetMetrics(
		disktest.Metric("dsk-p2", disktest.Today(), 55.0, 100, 8.0),
		disktest.Metric("dsk-p2", disktest.DayOffset(-1), 33.0, 60, 5.0),
	)

	_, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	today, ok := h.MetricDAO.Row(acc.ID, "dsk-p2", disktest.Today())
	require.True(t, ok)
	require.InDelta(t, 77.0, today.UsagePercent, 1e-9, "今日行必须保持首写结果")
	require.InDelta(t, 300, today.IOPS, 1e-9)
	require.InDelta(t, 20.0, today.Throughput, 1e-9)

	yesterday, ok := h.MetricDAO.Row(acc.ID, "dsk-p2", disktest.DayOffset(-1))
	require.True(t, ok)
	require.InDelta(t, 33.0, yesterday.UsagePercent, 1e-9, "昨日行按覆盖更新语义刷新")
	require.InDelta(t, 60, yesterday.IOPS, 1e-9)
	require.Equal(t, 2, h.MetricDAO.Count(), "同键至多一行")
}

// Outcome zero-exception-tagged: usage_percent=0 行(未挂载/口径缺失)不做
// CDN 式全零过滤 — 原样落库可见,写路径门禁强制打 qc_status=zero_exception,
// usage_scope 保留口径标注;读取侧可分辨。
func TestStep4_MetricPersistence_ZeroExceptionTagged(t *testing.T) {
	h, provider := newJourneyHarness()
	acc := h.SeedAccount(1, provider)
	h.SeedDisk(acc.ID, provider.Name, "dsk-unmounted", "cn-test-1")

	// usage_percent=0(available 未挂载形态),保留 usage_scope 标注
	m := disktest.Metric("dsk-unmounted", disktest.Today(), 0, 0, 0)
	m.UsageScope = types.DiskUsageScopeInstanceLevel
	provider.Querier.SetMetrics(m)

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Equal(t, 1, task.Result["metrics_total"], "0 值行必须落库可见,不做全零过滤")

	row, ok := h.MetricDAO.Row(acc.ID, "dsk-unmounted", disktest.Today())
	require.True(t, ok, "zero 行应原样落库")
	require.InDelta(t, 0, row.UsagePercent, 1e-9)
	require.Equal(t, types.DiskMetricQcZeroException, row.QcStatus, "写路径门禁应强制打 zero_exception")
	require.Equal(t, types.DiskUsageScopeInstanceLevel, row.UsageScope, "usage_scope 保留口径标注")
}

// Outcome qc-gate-reject: 任一行 usage_percent 越出 [0,100] 或 iops/throughput
// 为负 — 写入报错(携带 disk_id 与 date),整批拒绝;不良行不得落库;该盘计入
// 失败明细。
func TestStep4_MetricPersistence_QCGateReject(t *testing.T) {
	h, provider := newJourneyHarness()
	acc := h.SeedAccount(1, provider)
	h.SeedDisk(acc.ID, provider.Name, "dsk-badrange", "cn-test-1")

	// 越界形态:usage_percent=150(阿里 Burst -1 哨兵同类的未归一形态)
	provider.Querier.SetMetrics(
		disktest.Metric("dsk-badrange", disktest.Today(), 150.0, 100, 8.0),
	)

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err, "门禁拒绝不得让任务整体失败")

	require.Equal(t, 1, task.Result["failed_disks"], "整批拒绝的盘应计入失败")
	require.Equal(t, 0, task.Result["metrics_total"], "不良行不得落库")
	require.Equal(t, 0, h.MetricDAO.Count(), "ecam_disk_metric 无任何不良行")

	failures := nastest.ResultFailures(t, task)
	require.NotEmpty(t, failures)
	require.Contains(t, failures[0].LastError, "dsk-badrange", "错误应携带 disk_id 供归因")
	require.Contains(t, failures[0].LastError, "date=", "错误应携带 date")
	require.True(t, strings.Contains(failures[0].LastError, "0,100"), "错误应指出合法域 [0,100]")
}
