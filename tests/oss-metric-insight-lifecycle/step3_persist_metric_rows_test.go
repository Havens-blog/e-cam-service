// @feature oss-ops-insight @api-functional
//
// Contract step-3-persist-metric-rows: 指标行落库(唯一键 upsert 首写生效) —
// today rows insert-if-absent, yesterday rows are overwritten by the next-day
// backfill, storage_size=0 rows land visible with qc_status=zero_exception,
// and out-of-range non-zero rows reject the whole batch with the bucket name
// and date carried in the error.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package oss_metric_insight_lifecycle

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// Outcome first-write-and-backfill-upsert: 今日行首写生效新增,昨日行被补采
// 覆盖为日末态;metrics_total 相应增加;唯一键之下同键至多一行。
func TestStep3_FirstWriteAndBackfillUpsert(t *testing.T) {
	h, provider := newJourneyHarness()
	seedBucketAccount(t, h, provider, 1, "web-assets")
	// 昨日 00:10 初态行(容量 512),今日无行
	seedMetric(t, h, 1, provider.Name, osstest.Metric("web-assets", osstest.DayOffset(-1), 512, 4000))
	provider.Querier.SetMetrics(
		osstest.Metric("web-assets", osstest.DayOffset(-1), 530, 4100), // 日末聚合值
		osstest.Metric("web-assets", osstest.Today(), 600, 4200),
	)

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Equal(t, 2, nastest.ResultField(t, task, "metrics_total").(int))

	yesterday, ok := h.MetricDAO.Row(1, "web-assets", osstest.DayOffset(-1))
	require.True(t, ok)
	require.Equal(t, float64(530), yesterday.StorageSize, "昨日行应被补采覆盖为日末值")
	today, ok := h.MetricDAO.Row(1, "web-assets", osstest.Today())
	require.True(t, ok)
	require.Equal(t, float64(600), today.StorageSize, "今日行应为首写新插入")
	require.Equal(t, 2, h.MetricDAO.Count(), "唯一键之下不应产生重复行")
}

// Outcome zero-exception-row: storage_size=0 异常行不拦截不跳过,强制打
// qc_status=zero_exception 后照常落库;读取侧原样暴露并映射 data_status。
func TestStep3_ZeroExceptionRow(t *testing.T) {
	h, provider := newJourneyHarness()
	seedBucketAccount(t, h, provider, 1, "empty-bucket")
	provider.Querier.SetMetrics(osstest.Metric("empty-bucket", osstest.Today(), 0, 0))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Equal(t, 1, nastest.ResultField(t, task, "metrics_total").(int), "capacity=0 行必须计入写入行数")
	require.Empty(t, nastest.ResultFailures(t, task), "零容量是异常标注而非失败,不得进 failures")

	row, ok := h.MetricDAO.Row(1, "empty-bucket", osstest.Today())
	require.True(t, ok, "storage_size=0 异常行必须落库可见")
	require.Equal(t, types.OSSMetricQcZeroException, row.QcStatus)
	require.Equal(t, float64(0), row.StorageSize)

	// 读取侧闭环:qc_status 原样暴露并映射 data_status=zero_exception
	resp, err := h.NewQueryService().GetBucketMetrics(context.Background(), 1, 1, "empty-bucket", 7)
	require.NoError(t, err)
	today := resp.Days[len(resp.Days)-1]
	require.Equal(t, osstest.Today(), today.Date)
	require.Equal(t, "zero_exception", today.DataStatus)
	require.Equal(t, types.OSSMetricQcZeroException, today.QcStatus)
	require.NotNil(t, today.StorageSize)
	require.Equal(t, float64(0), *today.StorageSize)
}

// Outcome out-of-range-rejected: 非零越出 [1MB, 1PB] 的行(字节直写 GB 字段的
// 单位 bug 形态)整批拒绝,错误携带 bucket_name 与 date,不产生越界脏行。
func TestStep3_OutOfRangeRejected(t *testing.T) {
	h, _ := newJourneyHarness()

	validRow := osstest.Metric("healthy-bucket", osstest.Today(), 100, 10)
	// 1e9 GB ≈ 字节直写形态,远超 1PiB=1048576 GB 上界
	badRow := osstest.Metric("byte-bug-bucket", osstest.DayOffset(-1), 1e9, 1e8)

	err := h.MetricDAO.BulkUpsertMetrics(context.Background(), []types.OSSMetric{validRow, badRow})
	require.Error(t, err)
	require.Contains(t, err.Error(), "byte-bug-bucket", "错误应携带 bucket_name 供归因")
	require.Contains(t, err.Error(), osstest.DayOffset(-1), "错误应携带 date 供归因")
	require.Equal(t, 0, h.MetricDAO.Count(), "整批拒绝:同批健康行也不得落库")

	// 首写保护路径同门禁
	err = h.MetricDAO.BulkInsertIfAbsent(context.Background(), []types.OSSMetric{badRow})
	require.Error(t, err)
	require.Contains(t, err.Error(), "byte-bug-bucket")
}
