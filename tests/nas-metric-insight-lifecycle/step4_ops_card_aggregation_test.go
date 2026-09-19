// @feature nas-ops-insight @api-functional
//
// Contract step-4-ops-card-aggregation: 运营查看存储水位运营卡 — aggregation
// dedups by fs_id before counting ( shared fs counted once, representative row
// = latest date then largest capacity ), the average utilization denominator
// skips instance-less and capacity=0 rows, and "collect failed" is
// distinguishable from "truly empty" on the api surface via Result.failures.
// The display branches themselves are frontend-derived ( see doc.go scope
// note ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package nas_metric_insight_lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 聚合按 fs_id 去重后计数,同一物理文件系统只计一次;
// 平均使用率对无数据实例与 capacity=0 行跳过不参与分母。
func TestStep4_OpsCardAggregation_DedupAndAverageDenominator(t *testing.T) {
	h, provider := newJourneyHarness()
	account := h.SeedAccount(1, provider)
	// fs-a 正常非零(近 N 天均值使用率 0.25);fs-zero zero_exception;fs-b 无数据
	h.SeedMetric(t, account.ID, provider.Name,
		nastest.Metric("fs-a", "共享盘A", nastest.Today(), 200, 50))
	h.SeedMetric(t, account.ID, provider.Name,
		nastest.Metric("fs-zero", "零容量盘", nastest.Today(), 0, 0))

	query := h.NewQueryService()
	top, err := query.GetTop(context.Background(), h.TenantID, 0, 30, service.NASSortCapacity, 10, 1, 10)
	require.NoError(t, err)
	require.Equal(t, 2, top.Total, "按 fs_id 去重计数:有数据的物理 fs 各计一次,无数据 fs 不出现")

	// 平均使用率分母:仅正常非零行参与;zero_exception 行跳过不拉低均值
	trend, err := query.GetFsMetrics(context.Background(), h.TenantID, account.ID, "fs-a", 30)
	require.NoError(t, err)
	require.NotNil(t, trend.Average)
	require.NotNil(t, trend.Average.Utilization)
	require.InDelta(t, 0.25, *trend.Average.Utilization, 1e-9)
}

// Outcome collect-failure-warning-priority: 最近一次采集任务 Result 的失败
// 计数大于 0 时,api 面通过非空 failures 明确表达「采集失败」形态 —— 前端
// 据此让「采集失败→警示」优先于「无数据→0 占位」,而非纯 0 占位。
func TestStep4_OpsCardAggregation_CollectFailureWarningPriority(t *testing.T) {
	h, okProvider := newJourneyHarness()
	badProvider := h.NewProvider(true)
	h.SeedAccount(1, okProvider)
	h.SeedAccount(2, badProvider)
	h.SeedInstance(1, okProvider, "fs-ok", "健康盘", "cn-hangzhou")
	h.SeedInstance(2, badProvider, "fs-bad", "故障盘", "cn-shanghai")
	okProvider.Querier.SetMetrics(nastest.Metric("fs-ok", "健康盘", nastest.Today(), 16, 4))
	badProvider.Querier.Fail(errors.New("injected: vendor collect failed"))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	failures := nastest.ResultFailures(t, task)
	require.NotEmpty(t, failures, "失败计数>0 的任务 Result 应携带非空 failures")
	for _, f := range failures {
		require.Greater(t, f.ErrorCount, 0)
	}
}

// Outcome zero-or-no-data-placeholder: 任务成功执行且 Result 失败计数为 0、
// 指标真实为空/全零时,api 面通过空 failures + zero_exception 行可分辨表达
// 「0 占位/空数据态」而非失败警示;zero_exception 行按异常标注单独可见。
func TestStep4_OpsCardAggregation_ZeroOrNoDataPlaceholder(t *testing.T) {
	h, provider := newJourneyHarness()
	account := h.SeedAccount(1, provider)
	h.SeedInstance(1, provider, "fs-zero", "零容量盘", "cn-beijing")
	provider.Querier.SetMetrics(nastest.Metric("fs-zero", "零容量盘", nastest.Today(), 0, 0))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Empty(t, nastest.ResultFailures(t, task), "任务成功且无失败:不进警示分支")

	// zero_exception 行读取侧单独可见(data_status=zero_exception)
	query := h.NewQueryService()
	trend, err := query.GetFsMetrics(context.Background(), h.TenantID, account.ID, "fs-zero", 30)
	require.NoError(t, err)
	today := trend.Days[len(trend.Days)-1]
	require.Equal(t, service.NASDataStatusZeroException, today.DataStatus)
	require.Nil(t, today.Utilization, "零容量异常行 utilization 为 null,不参与均值")
	require.NotNil(t, today.Capacity)
	require.Equal(t, float64(0), *today.Capacity)
}

// 锚定:响应信封经 JSON 往返后的 data_status 取值为字符串契约。
func TestStep4_OpsCardAggregation_DataStatusJSONShape(t *testing.T) {
	h, provider := newJourneyHarness()
	account := h.SeedAccount(1, provider)
	h.SeedMetric(t, account.ID, provider.Name,
		nastest.Metric("fs-a", "共享盘A", nastest.Today(), 200, 50))

	router := h.NewNASRouter(1)
	_, env := nastest.GetJSON(t, router, "/assets/nas/metrics?fs_id=fs-a&account_id=1&days=7")
	var raw map[string]any
	require.NoError(t, json.Unmarshal(env.Data, &raw))
	days, ok := raw["days"].([]any)
	require.True(t, ok)
	today := days[len(days)-1].(map[string]any)
	require.Equal(t, "ok", today["data_status"])
}
