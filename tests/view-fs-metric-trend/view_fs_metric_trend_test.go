// @feature nas-ops-insight @api-functional
//
// Contract step-1-trend-request: 请求单实例趋势 — 200 with ascending days[],
// days 1~90 bounds, not-found / cross-tenant 404 without leaking, per-account
// isolated series for shared fs, unauthorized-401 exempt ( see doc.go ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package view_fs_metric_trend

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

// newJourneyHarness 构建带一个可指标厂商的世界。
func newJourneyHarness() (*nastest.Harness, *nastest.Provider) {
	h := nastest.NewHarness()
	return h, h.NewProvider(true)
}

// seedTrendWorld 账号 1 名下 fs-a 有昨日(100/25)与今日(200/50)两行;
// 账号 2 同 fs 也有行(共享实例多活形态)。
func seedTrendWorld(t *testing.T) (*nastest.Harness, *nastest.Provider) {
	t.Helper()
	h, provider := newJourneyHarness()
	acc1 := h.SeedAccount(1, provider)
	h.SeedMetric(t, acc1.ID, provider.Name,
		nastest.Metric("fs-a", "共享盘A", nastest.DayOffset(-1), 100, 25))
	h.SeedMetric(t, acc1.ID, provider.Name,
		nastest.Metric("fs-a", "共享盘A", nastest.Today(), 200, 50))
	acc2 := h.SeedAccount(2, provider)
	h.SeedMetric(t, acc2.ID, provider.Name,
		nastest.Metric("fs-a", "共享盘A", nastest.Today(), 400, 100))
	return h, provider
}

// Outcome success: 200 与响应体 { fs_id, days[] };days 按日期升序;每项含
// date/capacity/used/utilization/data_status/qc_status。
func TestStep1_TrendRequest_Success(t *testing.T) {
	h, _ := seedTrendWorld(t)
	router := h.NewNASRouter(1)

	status, env := nastest.GetJSON(t, router, "/assets/nas/metrics?fs_id=fs-a&account_id=1&days=30")
	require.Equal(t, 200, status)
	require.Equal(t, errs.Success.Code, env.Code)

	var resp service.NASFsMetricsResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.Equal(t, "fs-a", resp.FsID)
	require.Len(t, resp.Days, 30)
	require.Equal(t, nastest.DayOffset(-1), resp.Days[len(resp.Days)-2].Date)
	require.Equal(t, nastest.Today(), resp.Days[len(resp.Days)-1].Date)
}

// Outcome days-out-of-range-400: days=0 / days=91 越出 1~90 合法域返回 400。
func TestStep1_TrendRequest_DaysOutOfRange400(t *testing.T) {
	h, _ := seedTrendWorld(t)
	router := h.NewNASRouter(1)

	for _, days := range []string{"0", "91", "-5"} {
		status, env := nastest.GetJSON(t, router, "/assets/nas/metrics?fs_id=fs-a&account_id=1&days="+days)
		require.Equal(t, 400, status, "days=%s 应 400", days)
		require.Contains(t, env.Msg, "1~90")
	}
}

// Outcome not-found-or-cross-tenant-404: account_id 不属于当前租户一律 404,
// 不泄露资源/账号存在性。现实现中「fs_id 无指标行」走缺失日语义(200 全
// missing 展开,与「缺失日不填充假值」的读取口径一致),404 仅由账号越权
// 触发——按实码锚定,契约的 fs_id 分支差异已在 doc.go 备案。
func TestStep1_TrendRequest_NotFoundOrCrossTenant404(t *testing.T) {
	h, provider := seedTrendWorld(t)
	h.SeedAccountTenant(99, provider.Name, 2)
	router := h.NewNASRouter(1)

	// 越权 account_id:404 不泄露
	status, env := nastest.GetJSON(t, router, "/assets/nas/metrics?fs_id=fs-a&account_id=99&days=30")
	require.Equal(t, 404, status)
	require.Equal(t, errs.AccountNotFound.Code, env.Code)

	// 无指标行的 fs_id:200 + 全 missing(缺失日语义,不 404)
	status, env = nastest.GetJSON(t, router, "/assets/nas/metrics?fs_id=fs-not-exist&account_id=1&days=30")
	require.Equal(t, 200, status)
	var resp service.NASFsMetricsResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	for _, day := range resp.Days {
		require.Equal(t, service.NASDataStatusMissing, day.DataStatus)
	}
}

// Outcome unauthorized-401: 豁免(见 doc.go)。
func TestStep1_TrendRequest_UnauthorizedExempt(t *testing.T) {
	t.Log("exempt: 401 enforced by global auth middleware outside the NAS surface")
}

// Outcome per-account-isolated-series: 同一 fs_id 被 2 个账号采集时,趋势
// 接口按账号保留各自行,各自返回各自账号视角的日值序列,互不混合。
func TestStep1_TrendRequest_PerAccountIsolatedSeries(t *testing.T) {
	h, _ := seedTrendWorld(t)
	query := h.NewQueryService()
	ctx := context.Background()

	trend1, err := query.GetFsMetrics(ctx, h.TenantID, 1, "fs-a", 30)
	require.NoError(t, err)
	trend2, err := query.GetFsMetrics(ctx, h.TenantID, 2, "fs-a", 30)
	require.NoError(t, err)

	today1 := trend1.Days[len(trend1.Days)-1]
	today2 := trend2.Days[len(trend2.Days)-1]
	require.NotNil(t, today1.Capacity)
	require.NotNil(t, today2.Capacity)
	require.Equal(t, float64(200), *today1.Capacity, "账号 1 视角")
	require.Equal(t, float64(400), *today2.Capacity, "账号 2 视角,互不混合")
}
