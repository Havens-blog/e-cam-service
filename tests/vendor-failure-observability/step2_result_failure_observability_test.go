// @feature nas-ops-insight @api-functional
//
// Contract step-2-result-failure-observability: 失败计数与末次错误进入任务
// Result — failures carries provider/account_id/error_count/last_error,
// capacity=0 rows stay counted and visible, mandatory providers with zero
// success rows plus >=1 live NAS instance escalate a critical health alert
// ( Result.health_alerts ), and instance-less providers never alert.
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

// Outcome success: 一次采集存在失败厂商时,Result 按厂商/账号维护失败计数与
// 末次错误,失败厂商与成功厂商区分可查。
func TestStep2_ResultFailureObservability_FailuresInResult(t *testing.T) {
	h, okProvider := newJourneyHarness()
	badProvider := h.NewProvider(true)
	h.SeedAccount(1, okProvider)
	h.SeedAccount(2, badProvider)
	h.SeedInstance(1, okProvider, "fs-ok", "健康盘", "cn-hangzhou")
	h.SeedInstance(2, badProvider, "fs-bad", "故障盘", "cn-shanghai")
	okProvider.Querier.SetMetrics(nastest.Metric("fs-ok", "健康盘", nastest.Today(), 100, 10))
	badProvider.Querier.Fail(errors.New("injected: auth failure"))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	failures := nastest.ResultFailures(t, task)
	entry, found := nastest.FailureFor(failures, badProvider.Name)
	require.True(t, found, "失败厂商与成功厂商区分可查")
	require.Equal(t, int64(2), entry.AccountID)
	require.GreaterOrEqual(t, entry.ErrorCount, 1)
	require.Contains(t, entry.LastError, "auth failure")

	_, foundOK := nastest.FailureFor(failures, okProvider.Name)
	require.False(t, foundOK, "成功厂商不出现于 failures")
}

// Outcome zero-capacity-row-visible: capacity=0 异常行标记 zero_exception 后
// 落库可见,metrics_total 计入该行,不因全零被过滤。
func TestStep2_ResultFailureObservability_ZeroCapacityRowVisible(t *testing.T) {
	h, provider := newJourneyHarness()
	h.SeedAccount(1, provider)
	h.SeedInstance(1, provider, "fs-zero", "零容量盘", "cn-beijing")
	provider.Querier.SetMetrics(nastest.Metric("fs-zero", "零容量盘", nastest.Today(), 0, 0))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Equal(t, 1, nastest.ResultField(t, task, "metrics_total").(int), "metrics_total 计入零容量行")

	row, ok := h.MetricDAO.Row(1, "fs-zero", nastest.Today())
	require.True(t, ok, "ecam_nas_metric 存在该 zero_exception 行")
	require.Equal(t, "zero_exception", row.QcStatus)
}

// Outcome mandatory-zero-success-alert: 必达厂商(实盘存在 ≥1 NAS 实例)在
// 健康窗口内零成功写库行且本次为全量运行时,触发升级告警并出现在
// Result.health_alerts(经共用告警通道)。
func TestStep2_ResultFailureObservability_MandatoryZeroSuccessAlert(t *testing.T) {
	h, provider := newMandatoryHarness("aliyun")
	// 必达厂商 aliyun:实例存在,但厂商持续返回空结果(零成功形态)
	h.SeedAccount(1, provider)
	h.SeedInstance(1, provider, "fs-ali", "必达盘", "cn-hangzhou")

	task, err := h.RunCollect(t, nil) // 全量运行(无 account_id/provider 限定)
	require.NoError(t, err)

	alerts := h.HealthAlerter.Calls()
	require.NotEmpty(t, alerts, "连续零成功且实盘有实例的必达厂商应升级告警")
	last := alerts[len(alerts)-1]
	require.Equal(t, "aliyun", last.Provider)
	require.Equal(t, 3, last.WindowDays, "健康监控窗口默认 3 天")
	require.GreaterOrEqual(t, last.InstanceCount, int64(1))

	require.Contains(t, nastest.ResultAlerts(t, task), "aliyun", "health_alerts 含该厂商条目")
}

// Outcome mandatory-zero-success-alert(手动运行豁免): 手动单账号/单厂商
// 运行不做零成功判定(避免以偏概全误报)。
func TestStep2_ResultFailureObservability_ManualRunNoAlert(t *testing.T) {
	h, provider := newMandatoryHarness("aliyun")
	h.SeedAccount(1, provider)
	h.SeedInstance(1, provider, "fs-ali", "必达盘", "cn-hangzhou")

	_, err := h.RunCollect(t, map[string]any{"account_id": 1})
	require.NoError(t, err)
	require.Empty(t, h.HealthAlerter.Calls(), "手动局部运行不判定零成功告警")
}

// Outcome no-instance-no-alert: 必达厂商 NAS 实例枚举数为 0 时不触发零成功
// 告警,避免稳定误报造成告警疲劳。
func TestStep2_ResultFailureObservability_NoInstanceNoAlert(t *testing.T) {
	h, provider := newMandatoryHarness("aws")
	h.SeedAccount(1, provider) // 账号存在但无任何 NAS 实例

	_, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Empty(t, h.HealthAlerter.Calls(), "无实例厂商不触发零成功告警")
}

// newMandatoryHarness 构建命中必达厂商清单字面量(aliyun/huawei/aws)的世界。
func newMandatoryHarness(mandatoryName string) (*nastest.Harness, *nastest.Provider) {
	h := nastest.NewHarness()
	provider := h.NewNamedProvider(mandatoryName, true)
	// 健康监控仅统计必达厂商;内存 DAO 为空即窗口内零成功行
	return h, provider
}
