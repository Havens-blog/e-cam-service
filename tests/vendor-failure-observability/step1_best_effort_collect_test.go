// @feature nas-ops-insight @api-functional
//
// Contract step-1-best-effort-collect: 弱厂商失败返回空不阻塞 — best-effort
// vendors produce their own results alongside mandatory vendors, "probe
// unsupported" is a nil-error empty result ( INFO semantics, no failure
// counting ), and real API failures stay isolated.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package vendor_failure_observability

import (
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// newJourneyHarness 构建带一个可指标厂商(必达侧)的世界。
func newJourneyHarness() (*nastest.Harness, *nastest.Provider) {
	h := nastest.NewHarness()
	return h, h.NewProvider(true)
}

// Outcome success: 必达厂商与尽力而为厂商并存,后者正常返回时两厂商各自
// 产出指标结果,任务整体成功,单实例失败跳过继续。
func TestStep1_BestEffortCollect_BothVendorsProduce(t *testing.T) {
	h, mandatory := newJourneyHarness()
	bestEffort := h.NewProvider(true)
	h.SeedAccount(1, mandatory)
	h.SeedAccount(2, bestEffort)
	h.SeedInstance(1, mandatory, "fs-ali", "必达盘", "cn-hangzhou")
	h.SeedInstance(2, bestEffort, "fs-tenc", "尽力盘", "ap-shanghai")
	mandatory.Querier.SetMetrics(nastest.Metric("fs-ali", "必达盘", nastest.Today(), 100, 10))
	bestEffort.Querier.SetMetrics(nastest.Metric("fs-tenc", "尽力盘", nastest.Today(), 50, 5))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Equal(t, 2, nastest.ResultField(t, task, "metrics_total").(int))
	require.Empty(t, nastest.ResultFailures(t, task))
}

// Outcome probe-unsupported-info: 厂商对全部候选指标返回不支持形态时,适配器
// 返回空结果加 nil 错误(INFO 语义),不进失败计数,Result 的
// no_metric_support 可见、failures 为空。
func TestStep1_BestEffortCollect_ProbeUnsupportedInfo(t *testing.T) {
	h, mandatory := newJourneyHarness()
	plainVendor := h.NewProvider(false) // 未实现 NASMetricQuerier:探测不支持形态
	h.SeedAccount(1, mandatory)
	h.SeedAccount(2, plainVendor)
	h.SeedInstance(1, mandatory, "fs-ali", "必达盘", "cn-hangzhou")
	h.SeedInstance(2, plainVendor, "fs-plain", "不支持盘", "ap-shanghai")
	mandatory.Querier.SetMetrics(nastest.Metric("fs-ali", "必达盘", nastest.Today(), 100, 10))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err, "探测不支持不是错误,任务整体成功")

	noSupport := nastest.ResultStrings(t, task, "no_metric_support")
	require.Contains(t, noSupport, plainVendor.Name, "探测不支持在 Result 上可见")
	require.Empty(t, nastest.ResultFailures(t, task), "探测不支持的厂商不进 failures")
	require.Equal(t, 1, h.MetricDAO.Count(), "该厂商实例不产生指标行,必达厂商正常落库")
}

// Outcome api-call-failure-error: 真实调用失败(API 错误/超时/鉴权失败)形态
// 与探测不支持可分辨 — 返回错误、失败计数累加、末次错误进入 Result.failures。
func TestStep1_BestEffortCollect_APICallFailureError(t *testing.T) {
	h, mandatory := newJourneyHarness()
	bestEffort := h.NewProvider(true)
	h.SeedAccount(1, mandatory)
	h.SeedAccount(2, bestEffort)
	h.SeedInstance(1, mandatory, "fs-ali", "必达盘", "cn-hangzhou")
	h.SeedInstance(2, bestEffort, "fs-tenc", "尽力盘", "ap-shanghai")
	mandatory.Querier.SetMetrics(nastest.Metric("fs-ali", "必达盘", nastest.Today(), 100, 10))
	bestEffort.Querier.Fail(errors.New("injected: timeout after 30s"))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err, "尽力而为厂商失败不阻塞全流程")

	noSupport := nastest.ResultStrings(t, task, "no_metric_support")
	require.NotContains(t, noSupport, bestEffort.Name, "调用失败不属于探测不支持")

	entry, found := nastest.FailureFor(nastest.ResultFailures(t, task), bestEffort.Name)
	require.True(t, found, "调用失败进 failures,绝不静默吞掉")
	require.GreaterOrEqual(t, entry.ErrorCount, 1)
	require.Contains(t, entry.LastError, "timeout")
}
