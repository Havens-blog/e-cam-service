// @feature disk-ops-insight @api-functional
//
// Contract shared-disk-multi-account-metrics step-2-account-scoped-query: the
// account-scoped trend read returns only the requested account's rows ( the
// other account's same-disk rows never appear ), cross-tenant account ids
// answer 404 without leaking existence, and unauthorized is exempt.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package shared_disk_multi_account_metrics

import (
	"encoding/json"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/stretchr/testify/require"
)

// Outcome success: 以账号 A 视角查询单盘趋势 — 200 仅返回账号 A 的行;账号 B
// 的同行数据不出现;响应含最新一天值与近 N 天均值两类值。
func TestStep2_AccountScopedQuery_Success(t *testing.T) {
	h, provider := newJourneyHarness()
	accA := h.SeedAccountNamed(1, provider.Name)
	accB := h.SeedAccountNamed(2, provider.Name)
	h.SeedDisk(accA.ID, provider.Name, sharedDiskID, "cn-test-1")
	h.SeedDisk(accB.ID, provider.Name, sharedDiskID, "cn-test-1")
	// 两账号均有该共享盘的当日行(值不同,便于甄别)
	seedSharedMetricT(t, h, accA.ID, provider.Name, disktest.Today(), 66.0, 150, 11.0)
	seedSharedMetricT(t, h, accB.ID, provider.Name, disktest.Today(), 33.0, 70, 5.0)

	router := h.NewDiskRouter(1)
	status, env := disktest.GetJSON(t, router,
		"/assets/disk/metrics?disk_id="+sharedDiskID+"&account_id=1&days=7")
	require.Equal(t, 200, status)

	var resp service.DiskMetricsResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	todayPoint := resp.Days[len(resp.Days)-1]
	require.Equal(t, disktest.Today(), todayPoint.Date)
	require.NotNil(t, todayPoint.UsagePercent)
	require.InDelta(t, 66.0, *todayPoint.UsagePercent, 1e-9, "仅返回账号 A 的行")
	// latest/average 均为账号 A 口径
	require.NotNil(t, resp.Latest)
	require.InDelta(t, 66.0, *resp.Latest.UsagePercent, 1e-9)
	require.NotNil(t, resp.Average)
	require.InDelta(t, 66.0, *resp.Average.UsagePercent, 1e-9, "均值仅为账号 A 行口径")
}

// Outcome cross-account-404: account_id=B 不属于当前租户账号集合(租户外
// 身份查询)— 404 响应,不泄露账号存在性;响应中无该账号任何数据。
func TestStep2_AccountScopedQuery_CrossAccount404(t *testing.T) {
	h, provider := newJourneyHarness()
	accA := h.SeedAccountNamed(1, provider.Name)
	h.SeedDisk(accA.ID, provider.Name, sharedDiskID, "cn-test-1")
	seedSharedMetricT(t, h, accA.ID, provider.Name, disktest.Today(), 66.0, 150, 11.0)
	// 账号 B 属租户 2(对租户 1 不可见),但库里有其历史行
	h.SeedAccountTenant(2, provider.Name, 2)
	seedSharedMetricT(t, h, 2, provider.Name, disktest.Today(), 33.0, 70, 5.0)

	router := h.NewDiskRouter(1)
	status, env := disktest.GetJSON(t, router,
		"/assets/disk/metrics?disk_id="+sharedDiskID+"&account_id=2&days=7")
	require.Equal(t, 404, status, "越权 account_id 应 404")
	require.Equal(t, errs.AccountNotFound.Code, env.Code, "错误码不泄露账号存在性")
	// 响应 Data 为 JSON null:无该账号任何数据
	require.JSONEq(t, "null", string(env.Data), "越权响应不得携带该账号任何数据")
}

// Outcome unauthorized-401: 豁免(见 doc.go)——认证由全局鉴权中间件承载,
// disk 读取面自身无认证逻辑。
func TestStep2_AccountScopedQuery_UnauthorizedExempt(t *testing.T) {
	t.Log("exempt: 401 enforced by global auth middleware outside the disk surface")
}
