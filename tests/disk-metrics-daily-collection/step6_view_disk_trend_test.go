// @feature disk-ops-insight @api-functional
//
// Contract disk-metrics-daily-collection step-6-view-disk-trend: the user sees
// today's collected row through GET /assets/disk/metrics — days[] ascending
// with per-day usage/iops/throughput/qc_status, latest + average two-value
// shapes, data sourced exclusively from ecam_disk_metric ( the asset-table
// snapshot sentinel never leaks ), and missing days annotated with
// data_status=missing instead of fake values.
//
// Exemption ( see doc.go ): unauthorized-401 is enforced by the global auth
// middleware outside the disk surface.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package disk_metrics_daily_collection

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/stretchr/testify/require"
)

// seedTrendWorld 采集落库 3 天指标行并装配读取路由(读自己的采集数据)。
func seedTrendWorld(t *testing.T) (*disktest.Harness, *disktest.Provider, int64) {
	t.Helper()
	h, provider := newJourneyHarness()
	acc := h.SeedAccount(1, provider)
	h.SeedDisk(acc.ID, provider.Name, "dsk-view", "cn-test-1")
	provider.Querier.SetMetrics(
		disktest.Metric("dsk-view", disktest.Today(), 50.0, 100, 8.0),
		disktest.Metric("dsk-view", disktest.DayOffset(-1), 60.0, 120, 9.0),
		disktest.Metric("dsk-view", disktest.DayOffset(-2), 40.0, 80, 7.0),
	)
	_, err := h.RunCollect(t, map[string]any{"days": 3}) // 覆盖 -2/-1/0 三天采集窗口
	require.NoError(t, err)
	return h, provider, acc.ID
}

// Outcome success: 200 响应 — days 数组按日期升序含逐日指标;latest(最新一天)
// 与 average(近 N 天均值)两类值;数据一律来自 ecam_disk_metric 指标表,
// 资产表快照数值不展示。
func TestStep6_ViewDiskTrend_Success(t *testing.T) {
	h, provider, _ := seedTrendWorld(t)
	router := h.NewDiskRouter(1)

	status, env := disktest.GetJSON(t, router,
		"/assets/disk/metrics?disk_id=dsk-view&account_id=1&days=30")
	require.Equal(t, 200, status)

	var resp service.DiskMetricsResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.Equal(t, "dsk-view", resp.DiskID)
	require.Len(t, resp.Days, 30, "days=30 窗口应逐日展开")

	// 升序 + 采集落库行可见
	for i := 1; i < len(resp.Days); i++ {
		require.Less(t, resp.Days[i-1].Date, resp.Days[i].Date, "days 应按日期升序")
	}
	todayPoint := resp.Days[len(resp.Days)-1]
	require.Equal(t, disktest.Today(), todayPoint.Date)
	require.NotNil(t, todayPoint.UsagePercent)
	require.InDelta(t, 50.0, *todayPoint.UsagePercent, 1e-9, "当日采集落库的行应可见")
	require.InDelta(t, 100, *todayPoint.IOPS, 1e-9)
	require.InDelta(t, 8.0, *todayPoint.Throughput, 1e-9)

	// latest + average 两类值(均值 = 三天实际存在日)
	require.NotNil(t, resp.Latest)
	require.Equal(t, disktest.Today(), resp.Latest.Date)
	require.InDelta(t, 50.0, *resp.Latest.UsagePercent, 1e-9)
	require.NotNil(t, resp.Average)
	require.InDelta(t, 50.0, *resp.Average.UsagePercent, 1e-9, "均值应基于实际存在日 (50+60+40)/3")
	require.InDelta(t, 100.0, *resp.Average.IOPS, 1e-9)

	// 数据来源唯一:资产表快照哨兵值 (size=-1) 不得在任何指标界面数值出现
	require.False(t, strings.Contains(string(env.Data), ":-1"),
		"指标响应不得泄漏资产表快照哨兵值(数据来源唯一:ecam_disk_metric)")
	_ = provider
}

// Outcome unauthorized: 豁免(见 doc.go)——认证由全局鉴权中间件承载,
// disk 读取面自身无认证逻辑。
func TestStep6_ViewDiskTrend_UnauthorizedExempt(t *testing.T) {
	t.Log("exempt: 401 enforced by global auth middleware outside the disk surface")
}

// Outcome missing-day-annotated: 请求窗口内某天无指标行 — 缺失日以
// data_status=missing 标注且各指标值为 null,不填假值;近 N 天均值仅基于
// 实际存在日计算。
func TestStep6_ViewDiskTrend_MissingDayAnnotated(t *testing.T) {
	h, _, _ := seedTrendWorld(t)
	router := h.NewDiskRouter(1)

	// days=5 窗口:仅 3 天有行(-2/-1/0),其余 2 天缺失
	status, env := disktest.GetJSON(t, router,
		"/assets/disk/metrics?disk_id=dsk-view&account_id=1&days=5")
	require.Equal(t, 200, status)

	var resp service.DiskMetricsResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.Len(t, resp.Days, 5)

	missing := 0
	for _, day := range resp.Days {
		if day.DataStatus == service.DiskDataStatusMissing {
			missing++
			require.Nil(t, day.UsagePercent, "缺失日 usage_percent 应为 null,不填假值")
			require.Nil(t, day.IOPS)
			require.Nil(t, day.Throughput)
		} else {
			require.Equal(t, service.DiskDataStatusOK, day.DataStatus)
			require.NotNil(t, day.UsagePercent)
		}
	}
	require.Equal(t, 2, missing, "5 天窗口内应恰有 2 个缺失日")

	// 均值仅基于实际存在日计算
	require.NotNil(t, resp.Average)
	require.InDelta(t, 50.0, *resp.Average.UsagePercent, 1e-9, "均值不因缺失日被拉低")
}
