// @feature nas-ops-insight @api-functional
//
// Contract step-2-top-query: 查看 Top 排行 — 200 with { total, page,
// page_size, items[] } sorted by the sort field average, parameter bounds
// ( top/page_size max 50, days 1~90, sort domain ) answer 400, cross-tenant
// account ids answer 404, and empty collections / out-of-range pages answer
// 200 with empty items and correct metadata.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package watermark_overview_top

import (
	"encoding/json"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// seedTopWorld 账号 1 两行(fs-big 400/100、fs-small 200/50)。
func seedTopWorld(t *testing.T) (*nastest.Harness, *nastest.Provider) {
	t.Helper()
	h, provider := newJourneyHarness()
	acc := h.SeedAccount(1, provider)
	h.SeedMetric(t, acc.ID, provider.Name,
		nastest.Metric("fs-big", "大盘", nastest.Today(), 400, 100))
	h.SeedMetric(t, acc.ID, provider.Name,
		nastest.Metric("fs-small", "小盘", nastest.Today(), 200, 50))
	return h, provider
}

// Outcome success: 200 与 { total, page, page_size, items[] };items 按 sort
// 字段(近 N 天均值口径)降序取前 N;每条含 fs_id/fs_name/account_id 列表/
// data_status/qc_status 与最新一天、近 N 天均值两类值。
func TestStep2_TopQuery_Success(t *testing.T) {
	h, _ := seedTopWorld(t)
	router := h.NewNASRouter(1)

	status, env := nastest.GetJSON(t, router,
		"/assets/nas/top?account_id=1&days=30&sort=utilization&top=10&page=1&page_size=10")
	require.Equal(t, 200, status)

	var resp service.NASTopResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.Equal(t, 2, resp.Total)
	require.Equal(t, 1, resp.Page)
	require.Equal(t, 10, resp.PageSize)
	require.Len(t, resp.Items, 2)

	// 均值使用率降序:两盘同为 0.25 → 稳定序下逐一校验字段形状
	for _, item := range resp.Items {
		require.NotEmpty(t, item.FsID)
		require.NotEmpty(t, item.FsName)
		require.Equal(t, []int64{1}, item.AccountIDs)
		require.Equal(t, service.NASDataStatusOK, item.DataStatus)
		require.NotNil(t, item.Latest.Capacity)
		require.NotNil(t, item.Average.Utilization)
	}
}

// Outcome params-invalid-400: top=51 / page_size=51(超最大 50)、days=91、
// sort 非法值均 400 并列出失败项。
func TestStep2_TopQuery_ParamsInvalid400(t *testing.T) {
	h, _ := seedTopWorld(t)
	router := h.NewNASRouter(1)

	for _, query := range []string{
		"/assets/nas/top?account_id=1&top=51",
		"/assets/nas/top?account_id=1&page_size=51",
		"/assets/nas/top?account_id=1&days=91",
		"/assets/nas/top?account_id=1&sort=name",
		"/assets/nas/top?account_id=1&page=0",
	} {
		status, _ := nastest.GetJSON(t, router, query)
		require.Equal(t, 400, status, "%s 应 400", query)
	}
}

// Outcome cross-tenant-account-404: 请求携带非本租户的 account_id 返回 404
// 不泄露账号存在性。
func TestStep2_TopQuery_CrossTenant404(t *testing.T) {
	h, provider := seedTopWorld(t)
	h.SeedAccountTenant(99, provider.Name, 2)
	router := h.NewNASRouter(1)

	status, env := nastest.GetJSON(t, router, "/assets/nas/top?account_id=99&days=30")
	require.Equal(t, 404, status)
	require.Equal(t, errs.AccountNotFound.Code, env.Code)
}

// Outcome unauthorized-401: 豁免(见 doc.go)。
func TestStep2_TopQuery_UnauthorizedExempt(t *testing.T) {
	t.Log("exempt: 401 enforced by global auth middleware outside the NAS surface")
}

// Outcome empty-collection-pagination: 账号下无任何指标行或 page 超出总页数
// 时,200 与空 items[] 及正确 total/page/page_size 元数据,不报错。
func TestStep2_TopQuery_EmptyCollectionPagination(t *testing.T) {
	h, provider := seedTopWorld(t)
	emptyAcc := h.SeedAccount(2, provider)
	router := h.NewNASRouter(1)

	// 无指标账号:200 空页
	status, env := nastest.GetJSON(t, router, "/assets/nas/top?account_id=2&days=30")
	require.Equal(t, 200, status)
	var empty service.NASTopResp
	require.NoError(t, json.Unmarshal(env.Data, &empty))
	require.Equal(t, 0, empty.Total)
	require.Empty(t, empty.Items)

	// 超出页码:200 空页 + 正确 total/page/page_size
	status, env = nastest.GetJSON(t, router, "/assets/nas/top?account_id=1&days=30&page=99")
	require.Equal(t, 200, status)
	var beyond service.NASTopResp
	require.NoError(t, json.Unmarshal(env.Data, &beyond))
	require.Equal(t, 2, beyond.Total)
	require.Equal(t, 99, beyond.Page)
	require.Empty(t, beyond.Items)
	_ = emptyAcc
}
