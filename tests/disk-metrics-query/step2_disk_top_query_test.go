// @feature disk-ops-insight @api-functional
//
// Contract disk-metrics-query step-2-disk-top-query: the account-scoped Top —
// average-window sorted items with paging metadata, the legal sort enum, the
// 400 parameter validation surface ( days / sort / top / missing keys ), and
// the cross-tenant 404 that never leaks account existence.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package disk_metrics_query

import (
	"encoding/json"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/stretchr/testify/require"
)

// seedTopWorld 两块盘不同均值,验证排序与分页元数据。
func seedTopWorld(t *testing.T) (*disktest.Harness, *disktest.Provider) {
	t.Helper()
	h, provider := newJourneyHarness()
	h.SeedAccount(1, provider)
	seedT(t, h, 1, provider.Name, "dsk-high", disktest.Today(), 90.0, 500, 40.0)
	seedT(t, h, 1, provider.Name, "dsk-high", disktest.DayOffset(-1), 80.0, 400, 30.0)
	seedT(t, h, 1, provider.Name, "dsk-low", disktest.Today(), 20.0, 50, 2.0)
	return h, provider
}

// Outcome success: 200 返回按近 N 天均值口径排序的磁盘 Top 列表,含分页元
// 数据;sort 缺省 usage_percent;top 缺省 10 最大 50;重复调用幂等。
func TestStep2_TopQuery_Success(t *testing.T) {
	h, _ := seedTopWorld(t)
	router := h.NewDiskRouter(1)

	status, env := disktest.GetJSON(t, router,
		"/assets/disk/top?account_id=1&days=7&sort=usage_percent&top=10&page=1&page_size=20")
	require.Equal(t, 200, status)

	var resp service.DiskTopResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.Equal(t, 2, resp.Total, "去重后的磁盘总数(分页分母)")
	require.Equal(t, 1, resp.Page)
	require.Equal(t, 20, resp.PageSize)
	require.Len(t, resp.Items, 2)
	// 排序键统一近 N 天均值口径:dsk-high 均值 85 > dsk-low 20
	require.Equal(t, "dsk-high", resp.Items[0].DiskID)
	require.Equal(t, "dsk-low", resp.Items[1].DiskID)
	require.InDelta(t, 85.0, *resp.Items[0].Average.UsagePercent, 1e-9)
	require.InDelta(t, 20.0, *resp.Items[1].Average.UsagePercent, 1e-9)

	// 其余合法 sort 枚举值不 400,幂等
	for _, sort := range []string{service.DiskSortIOPS, service.DiskSortThroughput} {
		status, env := disktest.GetJSON(t, router, "/assets/disk/top?account_id=1&days=7&sort="+sort)
		require.Equal(t, 200, status, "sort=%s 应合法", sort)
		var resp2 service.DiskTopResp
		require.NoError(t, json.Unmarshal(env.Data, &resp2))
		require.Len(t, resp2.Items, 2)
	}
}

// Outcome validation-error: 参数越界 — 400 Bad Request,响应体明确列出校验
// 失败项(days 限 1~90,sort 仅支持合法枚举,top 上限 50;disk_id/account_id
// 缺失同样 400);不进入业务查询。
func TestStep2_TopQuery_ValidationError400(t *testing.T) {
	h, _ := seedTopWorld(t)
	router := h.NewDiskRouter(1)

	for _, tc := range []struct {
		path    string
		wantMsg string
	}{
		{"/assets/disk/top?account_id=1&days=0", "1~90"},
		{"/assets/disk/top?account_id=1&days=91", "1~90"},
		{"/assets/disk/top?account_id=1&sort=latency", "usage_percent|iops|throughput"},
		{"/assets/disk/top?account_id=1&top=100", "1~50"},
		{"/assets/disk/metrics?account_id=1&days=7", "disk_id"},     // 趋势缺 disk_id
		{"/assets/disk/metrics?disk_id=dsk-a&days=7", "account_id"}, // 趋势缺 account_id
	} {
		status, env := disktest.GetJSON(t, router, tc.path)
		require.Equal(t, 400, status, "%s 应 400", tc.path)
		require.Contains(t, env.Msg, tc.wantMsg, "%s 错误信息应指出校验失败项", tc.path)
	}
}

// Outcome cross-tenant-404: account_id 不属于当前租户账号集合 — 404(错误码
// 账号不存在语义),不泄露账号存在性;响应中无该账号任何数据。
func TestStep2_TopQuery_CrossTenant404(t *testing.T) {
	h, provider := seedTopWorld(t)
	// 租户 2 的账号(对租户 1 会话不可见)
	h.SeedAccountTenant(99, provider.Name, 2)
	router := h.NewDiskRouter(1)

	for _, path := range []string{
		"/assets/disk/top?account_id=99&days=7",
		"/assets/disk/metrics?disk_id=dsk-a&account_id=99&days=7",
	} {
		status, env := disktest.GetJSON(t, router, path)
		require.Equal(t, 404, status, "%s 越权应 404", path)
		require.Equal(t, errs.AccountNotFound.Code, env.Code, "%s 错误码不泄露账号存在性", path)
	}
}
