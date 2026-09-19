// @feature nas-ops-insight @api-functional
//
// Contract step-3-fs-trend-query: 运营查看单实例近 30 天趋势 — GET
// /assets/nas/metrics returns the ascending days[] series with derived
// utilization and missing-day annotation, cross-tenant account ids answer 404
// without leaking existence, and out-of-range days answer 400. The
// unauthorized-401 outcome is exempt ( global auth middleware, see doc.go ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package nas_metric_insight_lifecycle

import (
	"encoding/json"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// seedTrendWorld 构建一个有趋势数据的世界:账号 1(租户 1)有 fs-a 的
// 昨日/今日两行,今日行缺失用于 missing 标注由请求窗口决定。
func seedTrendWorld(t *testing.T) (*nastest.Harness, *nastest.Provider) {
	t.Helper()
	h, provider := newJourneyHarness()
	account := h.SeedAccount(1, provider)
	h.SeedMetric(t, account.ID, provider.Name,
		nastest.Metric("fs-a", "共享盘A", nastest.DayOffset(-1), 100, 25))
	h.SeedMetric(t, account.ID, provider.Name,
		nastest.Metric("fs-a", "共享盘A", nastest.Today(), 200, 50))
	return h, provider
}

// Outcome success: 200 响应含 fs_id 与 days 数组;days 按日期升序,每项含
// date/capacity/used/utilization/data_status/qc_status;另含 latest 与 average
// 汇总;utilization 由 capacity/used 读取时派生(0.25 = 50/200)。
func TestStep3_FsTrendQuery_Success(t *testing.T) {
	h, _ := seedTrendWorld(t)
	router := h.NewNASRouter(1)

	status, env := nastest.GetJSON(t, router, "/assets/nas/metrics?fs_id=fs-a&account_id=1&days=30")
	require.Equal(t, 200, status)
	require.Equal(t, errs.Success.Code, env.Code)

	var resp service.NASFsMetricsResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.Equal(t, "fs-a", resp.FsID)
	require.Len(t, resp.Days, 30, "窗口逐日展开")

	// 升序 + 末日为今日
	require.True(t, resp.Days[0].Date < resp.Days[1].Date)
	require.Equal(t, nastest.Today(), resp.Days[len(resp.Days)-1].Date)
	today := resp.Days[len(resp.Days)-1]
	require.Equal(t, service.NASDataStatusOK, today.DataStatus)
	require.Equal(t, "", today.QcStatus)
	require.NotNil(t, today.Capacity)
	require.NotNil(t, today.Utilization)
	require.InDelta(t, 0.25, *today.Utilization, 1e-9, "utilization 读取时派生 50/200")

	// 汇总:最新一天 + 近 N 天均值
	require.NotNil(t, resp.Latest)
	require.Equal(t, nastest.Today(), resp.Latest.Date)
	require.NotNil(t, resp.Average)
	require.NotNil(t, resp.Average.Utilization)
	require.InDelta(t, 0.25, *resp.Average.Utilization, 1e-9)
}

// Outcome success(缺失日语义): 窗口内无行日期以 data_status=missing 标注,
// capacity/used/utilization 均为 null,不填充假值。
func TestStep3_FsTrendQuery_MissingDaysNotFilled(t *testing.T) {
	h, _ := seedTrendWorld(t)
	router := h.NewNASRouter(1)

	_, env := nastest.GetJSON(t, router, "/assets/nas/metrics?fs_id=fs-a&account_id=1&days=30")
	var resp service.NASFsMetricsResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))

	missing := 0
	for _, day := range resp.Days {
		if day.DataStatus == service.NASDataStatusMissing {
			missing++
			require.Nil(t, day.Capacity)
			require.Nil(t, day.Used)
			require.Nil(t, day.Utilization, "缺失日绝不以 0/假值填充")
		}
	}
	require.Equal(t, 28, missing, "30 天窗口内仅两日有行")
}

// Outcome cross-tenant-account-404: 请求租户外 account_id 返回 404(错误码
// 404002 账号不存在语义),不泄露该账号存在性及其指标数据。
func TestStep3_FsTrendQuery_CrossTenantAccount404(t *testing.T) {
	h, provider := seedTrendWorld(t)
	// 租户 2 的账号(对租户 1 会话不可见)
	h.SeedAccountTenant(99, provider.Name, 2)
	router := h.NewNASRouter(1)

	status, env := nastest.GetJSON(t, router, "/assets/nas/metrics?fs_id=fs-a&account_id=99&days=30")
	require.Equal(t, 404, status)
	require.Equal(t, errs.AccountNotFound.Code, env.Code, "错误码 404002,不泄露账号存在性")
}

// Outcome days-out-of-range-400: days 越出 1~90 合法域(days=0/91)或非法
// 整数返回 400,错误信息指出合法域,不触发任何指标查询。
func TestStep3_FsTrendQuery_DaysOutOfRange400(t *testing.T) {
	h, _ := seedTrendWorld(t)
	router := h.NewNASRouter(1)

	for _, query := range []string{
		"/assets/nas/metrics?fs_id=fs-a&account_id=1&days=0",
		"/assets/nas/metrics?fs_id=fs-a&account_id=1&days=91",
		"/assets/nas/metrics?fs_id=fs-a&account_id=1&days=abc",
	} {
		status, env := nastest.GetJSON(t, router, query)
		require.Equal(t, 400, status, "%s 应 400", query)
		require.Contains(t, env.Msg, "1~90", "%s 错误信息应指出 days 合法域", query)
	}
}

// Outcome unauthorized-401: 豁免(见 doc.go)——认证由全局鉴权中间件承载,
// NAS 读取面自身无认证逻辑。
func TestStep3_FsTrendQuery_UnauthorizedExempt(t *testing.T) {
	t.Log("exempt: 401 enforced by global auth middleware outside the NAS surface")
}
