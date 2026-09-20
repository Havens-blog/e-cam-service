// @feature oss-ops-insight @api-functional
//
// Journey smoke: OSS vendor failure observability golden path — a failing
// mandatory vendor stays isolated while healthy vendors land rows, probe-
// unsupported vendors are INFO-classified, the failure summary lands in
// Result["failures"], and a persistent zero-success mandatory vendor fires a
// single throttled health alert. Only happy-path outcomes of the observability
// contract ( failure-path edge cases live in the step files ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package vendor_failure_observability

import (
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

func TestOSSVendorFailureObservability_FullJourneySmoke(t *testing.T) {
	h := osstest.NewHarness()
	failing := h.NewNamedProvider("aws", true)    // 故障必达厂商
	healthy := h.NewNamedProvider("aliyun", true) // 健康必达厂商
	plain := h.NewNamedProvider("tencent", false) // 尽力而为:探测不支持
	failingAcc := h.SeedAccount(1, failing)
	healthyAcc := h.SeedAccount(2, healthy)
	ossSeedBucketAccount(t, h, plain, 3, "plain-bucket")
	h.SeedBucket(failingAcc.ID, failing.Name, "broken-bucket")
	h.SeedBucket(healthyAcc.ID, healthy.Name, "ok-bucket")

	failing.Querier.Fail(errors.New("injected: vendor API down"))
	healthy.Querier.SetMetrics(osstest.Metric("ok-bucket", osstest.Today(), 180, 18))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err, "失败/不支持厂商均不得中断整轮采集")

	// Step 1+3: 失败隔离且汇总可定位
	failures := nastest.ResultFailures(t, task)
	require.Len(t, failures, 1)
	require.Equal(t, "aws", failures[0].Provider)
	require.Equal(t, failingAcc.ID, failures[0].AccountID)
	require.Equal(t, 1, nastest.ResultField(t, task, "failed_buckets").(int))

	// Step 2: 探测不支持 INFO 归类,不混淆失败口径
	require.Equal(t, []string{"tencent"}, nastest.ResultStrings(t, task, "no_metric_support"))

	// 健康厂商照常落库
	_, ok := h.MetricDAO.Row(healthyAcc.ID, "ok-bucket", osstest.Today())
	require.True(t, ok)

	// Step 4: 健康告警按口径触发 — 持续零成功的必达 aws 告警,窗口内有成功行
	// 的 aliyun 不告警(无误报)
	require.Equal(t, []string{"aws"}, nastestResultAlerts(t, task))
	require.Len(t, h.HealthAlerter.Calls(), 1, "每轮每厂商至多一次告警")
}
