// @feature nas-ops-insight @api-functional
//
// Contract step-3-per-account-trend: 趋势按账号隔离读取 — the same shared fs
// read through three account ids returns three independent day-value series,
// cross-tenant account ids answer 404 without leaking, and unauthorized
// requests are exempt ( global auth middleware, see doc.go ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multi_account_shared_fs

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// seedThreeAccountTrend 三账号同 fs 各留当日一行(容量口径互不相同)。
func seedThreeAccountTrend(t *testing.T) (*nastest.Harness, *nastest.PerAccountProvider, []int64) {
	t.Helper()
	h, provider := newJourneyHarness()
	ids := seedSharedFSCollect(t, h, provider, map[int64]float64{1: 100, 2: 200, 3: 300})
	_, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	return h, provider, ids
}

// Outcome success: 分别以三个 account_id 请求同一 fs 的趋势,三次均 200 且
// 各返回各自视角的日值序列,数值互不混合。
func TestStep3_PerAccountTrend_IsolatedSeries(t *testing.T) {
	h, _, ids := seedThreeAccountTrend(t)
	query := h.NewQueryService()
	ctx := context.Background()

	wantCapacity := map[int64]float64{1: 100, 2: 200, 3: 300}
	for _, accountID := range ids {
		trend, err := query.GetFsMetrics(ctx, h.TenantID, accountID, SharedFS, 30)
		require.NoError(t, err, "账号 %d 鉴权校验应通过", accountID)
		today := trend.Days[len(trend.Days)-1]
		require.NotNil(t, today.Capacity)
		require.Equal(t, wantCapacity[accountID], *today.Capacity,
			"趋势按账号保留各自行,数值不得与其他账号混合")
		require.Equal(t, service.NASDataStatusOK, today.DataStatus)
	}
}

// Outcome cross-tenant-account-404: 以租户外账号读同 fs 趋势返回 404,
// 不泄露该账号存在性与指标数据。
func TestStep3_PerAccountTrend_CrossTenant404(t *testing.T) {
	h, provider, _ := seedThreeAccountTrend(t)
	// 租户 2 账号(会话租户为 1 时不可见)
	h.SeedAccountTenant(99, provider.Name, 2)
	router := h.NewNASRouter(1)

	status, env := nastest.GetJSON(t, router, "/assets/nas/metrics?fs_id="+SharedFS+"&account_id=99&days=30")
	require.Equal(t, 404, status)
	require.Equal(t, errs.AccountNotFound.Code, env.Code)
}

// Outcome unauthorized-401: 豁免(见 doc.go)。
func TestStep3_PerAccountTrend_UnauthorizedExempt(t *testing.T) {
	t.Log("exempt: 401 enforced by global auth middleware outside the NAS surface")
}
