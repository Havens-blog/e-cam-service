// @feature oss-ops-insight @api-functional
//
// Contract oss step-3-failures-summary: 失败汇总入 Result["failures"] — the
// failure summary locates vendor+account granularity with
// provider/account_id/error_count/last_error plus failed_buckets, and an
// all-success round reports an empty summary without inflating failures.
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

// Outcome failures-summary-locatable: 一轮采集中存在厂商/账号维度失败 —
// Result["failures"] 含 provider/account_id/error_count/last_error 维度,
// 可定位到具体厂商与账号;failed_buckets 计数同步可见。
func TestOSSStep3_FailuresSummaryLocatable(t *testing.T) {
	h := osstest.NewHarness()
	bad := h.NewNamedProvider("aws", true) // 故障必达厂商
	good := h.NewNamedProvider("huawei", true)
	badAcc := h.SeedAccount(1, bad)
	h.SeedAccount(2, good)
	// 故障账号两个 bucket(账号维度失败累计 2 次);正常账号一个 bucket
	h.SeedBucket(badAcc.ID, bad.Name, "bad-bucket-1")
	h.SeedBucket(badAcc.ID, bad.Name, "bad-bucket-2")
	h.SeedBucket(2, good.Name, "ok-bucket")

	bad.Querier.Fail(errors.New("injected: quota exceeded"))
	good.Querier.SetMetrics(osstest.Metric("ok-bucket", osstest.Today(), 100, 10))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	failures := nastest.ResultFailures(t, task)
	require.Len(t, failures, 1, "失败明细按厂商+账号粒度归并")
	f := failures[0]
	require.Equal(t, "aws", f.Provider)
	require.Equal(t, badAcc.ID, f.AccountID)
	require.Equal(t, 2, f.ErrorCount, "error_count 累计该账号全部 bucket 失败次数")
	require.NotEmpty(t, f.LastError, "last_error 必须携带末次错误供归因")
	require.Contains(t, f.LastError, "quota exceeded")

	// failed_buckets 计数同步可见
	require.Equal(t, 2, nastest.ResultField(t, task, "failed_buckets").(int))
}

// Outcome no-failure-empty-summary: 一轮全量采集全部成功 — failures 为空列表;
// no_metric_support 与 accounts_without_oss 如实反映,不虚报失败。
func TestOSSStep3_NoFailureEmptySummary(t *testing.T) {
	h := osstest.NewHarness()
	mandatory := h.NewNamedProvider("aliyun", true)
	plain := h.NewNamedProvider("tencent", false)
	acc := h.SeedAccount(1, mandatory)
	ossSeedBucketAccount(t, h, plain, 2, "plain-bucket") // 探测不支持厂商
	h.SeedAccount(3, mandatory)                          // 无 bucket 账号
	h.SeedBucket(acc.ID, mandatory.Name, "ok-bucket")
	mandatory.Querier.SetMetrics(osstest.Metric("ok-bucket", osstest.Today(), 120, 12))

	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	require.Empty(t, nastest.ResultFailures(t, task), "全部成功时 failures 为空列表")
	require.Equal(t, 0, nastest.ResultField(t, task, "failed_buckets").(int))
	// 如实反映的口径清单
	require.Equal(t, []string{"tencent"}, nastest.ResultStrings(t, task, "no_metric_support"))
	require.Equal(t, 2, nastest.ResultField(t, task, "accounts").(int),
		"有 bucket 的账号计入采集账号(探测不支持账号亦然);无 bucket 账号不计入")
	withoutOSS := nastest.ResultStrings(t, task, "accounts_without_oss")
	require.Len(t, withoutOSS, 1, "无 bucket 账号如实入 accounts_without_oss")
}
