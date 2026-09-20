// @feature oss-ops-insight @api-functional
//
// Contract oss step-1-vendor-failure-isolated: 必达厂商适配器调用失败不阻塞
// 全流程 — a failing mandatory vendor only returns its own empty result with
// the failure recorded in Result.failures, while other vendors/accounts keep
// collecting; a single failing account never contaminates a healthy sibling
// under the same vendor.
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

// Outcome vendor-failure-isolated: 故障必达厂商只返回自身失败;ERROR+失败明细
// 记录;其他厂商与其他账号的采集正常继续,全流程不中断。
func TestOSSStep1_VendorFailureIsolated(t *testing.T) {
	h := osstest.NewHarness()
	bad := h.NewNamedProvider("aliyun", true)  // 故障必达厂商
	good := h.NewNamedProvider("huawei", true) // 正常必达厂商
	badAcc := h.SeedAccount(1, bad)
	goodAcc := h.SeedAccount(2, good)
	h.SeedBucket(1, bad.Name, "broken-bucket")
	h.SeedBucket(2, good.Name, "ok-bucket")

	bad.Querier.Fail(errors.New("injected: vendor monitoring API down"))
	good.Querier.SetMetrics(osstest.Metric("ok-bucket", osstest.Today(), 200, 20))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err, "单厂商失败不得反噬整轮采集任务")

	// 失败可观测:failures 明细定位到厂商+账号
	failures := nastest.ResultFailures(t, task)
	require.Len(t, failures, 1)
	require.Equal(t, "aliyun", failures[0].Provider)
	require.Equal(t, badAcc.ID, failures[0].AccountID)
	require.Equal(t, 1, failures[0].ErrorCount)
	require.Contains(t, failures[0].LastError, "vendor monitoring API down")

	// 正常厂商照常落库;故障厂商指标不落库
	ossRequireRowsInWindow(t, h, osstest.Today(), map[int64]float64{goodAcc.ID: 200})
	_, ok := h.MetricDAO.Row(badAcc.ID, "broken-bucket", osstest.Today())
	require.False(t, ok)
}

// Outcome single-account-failure-isolated: 同一必达厂商下 account A 凭证失效、
// account B 正常 — 仅 A 计入失败明细,B 照常落库,账号级失败互不传染。
func TestOSSStep1_SingleAccountFailureIsolated(t *testing.T) {
	h := osstest.NewHarness()
	p := h.NewPerAccountProvider()
	h.SeedAccountNamed(1, p.Name) // account A:凭证失效
	h.SeedAccountNamed(2, p.Name) // account B:正常
	h.SeedBucket(1, p.Name, "expired-bucket")
	h.SeedBucket(2, p.Name, "ok-bucket")

	p.QuerierFor(1).Fail(errors.New("injected: account credentials expired"))
	p.QuerierFor(2).SetMetrics(osstest.Metric("ok-bucket", osstest.Today(), 300, 30))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	failures := nastest.ResultFailures(t, task)
	require.Len(t, failures, 1, "仅失效账号计入失败明细")
	require.Equal(t, int64(1), failures[0].AccountID)
	require.Contains(t, failures[0].LastError, "credentials expired")

	// B 的指标行正常落库;A 无指标行
	_, ok := h.MetricDAO.Row(2, "ok-bucket", osstest.Today())
	require.True(t, ok)
	_, ok = h.MetricDAO.Row(1, "expired-bucket", osstest.Today())
	require.False(t, ok)
}
