// @feature oss-ops-insight @api-functional
//
// Contract step-5-trend-per-account-isolation: 趋势读取按账号精确隔离 —
// accounts A and B each see their own shared-bucket series ( never each
// other's ), and a cross-tenant account_id is answered 404 without leaking
// the account's existence. unauthorized-401 is exempt ( see doc.go ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multi_account_shared_bucket

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// seedTwoAccountSeries 账号 A/B 各持 shared-assets 的不同数值序列。
func seedTwoAccountSeries(t *testing.T) (*osstest.Harness, *osstest.Provider) {
	t.Helper()
	h, provider := newJourneyHarness()
	seedSharedBucketAccount(t, h, provider, 1) // account A
	seedSharedBucketAccount(t, h, provider, 2) // account B
	seedSharedMetricT(t, h, 1, provider.Name, osstest.Today(), 111, 11)
	seedSharedMetricT(t, h, 2, provider.Name, osstest.Today(), 999, 99)
	return h, provider
}

// Outcome per-account-trend-isolated: 分别以 account A 与 account B 查询
// shared-assets 趋势 — 各自返回本账号序列,不出现跨账号串数据。
func TestStep5_PerAccountTrendIsolated(t *testing.T) {
	h, _ := seedTwoAccountSeries(t)
	query := h.NewQueryService()
	ctx := context.Background()

	respA, err := query.GetBucketMetrics(ctx, 1, 1, sharedBucketName, 7)
	require.NoError(t, err)
	respB, err := query.GetBucketMetrics(ctx, 1, 2, sharedBucketName, 7)
	require.NoError(t, err)

	todayA := respA.Days[len(respA.Days)-1]
	todayB := respB.Days[len(respB.Days)-1]
	require.NotNil(t, todayA.StorageSize)
	require.NotNil(t, todayB.StorageSize)
	require.Equal(t, float64(111), *todayA.StorageSize, "账号 A 序列为其自有口径")
	require.Equal(t, float64(999), *todayB.StorageSize, "账号 B 序列为其自有口径,不与 A 串数据")
	require.Equal(t, int64(11), *todayA.ObjectCount)
	require.Equal(t, int64(99), *todayB.ObjectCount)
}

// Outcome cross-tenant-account-404: 租户 1 用户以租户 2 的账号查询 — 返回 404
// (不泄露账号存在性),不返回该账号任何指标数据。
func TestStep5_CrossTenantAccount404(t *testing.T) {
	h, provider := seedTwoAccountSeries(t)
	// 账号 9 属租户 2(不属于请求用户所在租户 1),且持有同名 bucket 指标
	h.SeedAccountTenant(9, provider.Name, 2)
	h.SeedMetric(t, 9, provider.Name, osstest.Metric(sharedBucketName, osstest.Today(), 777, 77))

	router := h.NewOSSRouter(1)
	status, env := osstest.GetJSON(t, router, "/assets/oss/metrics?bucket_name=shared-assets&account_id=9&days=7")
	require.Equal(t, 404, status, "越权 account_id 映射 404 而非 403(不泄露账号存在性)")
	require.Equal(t, errs.AccountNotFound.Msg, env.Msg, "错误信息与账号不存在同文案,不泄露存在性")
	require.True(t, len(env.Data) == 0 || string(env.Data) == "null",
		"越权响应不得携带任何指标数据: %s", string(env.Data))
}

// Outcome unauthorized: 豁免(见 doc.go)。
func TestStep5_TrendIsolation_UnauthorizedExempt(t *testing.T) {
	t.Log("exempt: 401 enforced by global auth middleware outside the OSS surface")
}
