// @feature disk-ops-insight @api-functional
//
// Contract disk-metrics-query step-1-disk-trend-query: the single-disk trend
// read — 30-day ascending window with latest + average two-value shapes,
// missing days honestly annotated, idempotent reads, and the exempted
// unauthorized outcome.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package disk_metrics_query

import (
	"encoding/json"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/stretchr/testify/require"
)

// seedTrendWorld 三天指标行(同一盘),租户 1 账号 1。
func seedTrendWorld(t *testing.T) (*disktest.Harness, *disktest.Provider) {
	t.Helper()
	h, provider := newJourneyHarness()
	h.SeedAccount(1, provider)
	seedT(t, h, 1, provider.Name, "dsk-a", disktest.DayOffset(-2), 40.0, 80, 7.0)
	seedT(t, h, 1, provider.Name, "dsk-a", disktest.DayOffset(-1), 60.0, 120, 9.0)
	seedT(t, h, 1, provider.Name, "dsk-a", disktest.Today(), 50.0, 100, 8.0)
	return h, provider
}

// Outcome success: 200 返回该盘 30 天内逐日趋势(days 数组按日期升序),同时
// 包含「最新一天」值与「近 N 天均值」两类值;缺失日以 data_status=missing
// 标注,不填假值;只读无副作用,重复调用幂等。
func TestStep1_TrendQuery_Success(t *testing.T) {
	h, _ := seedTrendWorld(t)
	router := h.NewDiskRouter(1)

	status, env := disktest.GetJSON(t, router, "/assets/disk/metrics?disk_id=dsk-a&account_id=1&days=30")
	require.Equal(t, 200, status)

	var resp service.DiskMetricsResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.Equal(t, "dsk-a", resp.DiskID)
	require.Len(t, resp.Days, 30)
	for i := 1; i < len(resp.Days); i++ {
		require.Less(t, resp.Days[i-1].Date, resp.Days[i].Date, "days 应按日期升序")
	}
	// 最新一天
	require.NotNil(t, resp.Latest)
	require.Equal(t, disktest.Today(), resp.Latest.Date)
	require.InDelta(t, 50.0, *resp.Latest.UsagePercent, 1e-9)
	require.InDelta(t, 100, *resp.Latest.IOPS, 1e-9)
	require.InDelta(t, 8.0, *resp.Latest.Throughput, 1e-9)
	// 近 N 天均值(基于实际存在日 3 天)
	require.NotNil(t, resp.Average)
	require.InDelta(t, 50.0, *resp.Average.UsagePercent, 1e-9, "(40+60+50)/3")
	require.InDelta(t, 100.0, *resp.Average.IOPS, 1e-9)
	// 缺失日诚实标注(30 天窗口仅 3 天有行)
	missing := 0
	for _, day := range resp.Days {
		if day.DataStatus == service.DiskDataStatusMissing {
			missing++
			require.Nil(t, day.UsagePercent, "缺失日不得填假值")
		}
	}
	require.Equal(t, 27, missing)

	// 只读幂等:重复调用结果一致
	status2, env2 := disktest.GetJSON(t, router, "/assets/disk/metrics?disk_id=dsk-a&account_id=1&days=30")
	require.Equal(t, 200, status2)
	require.JSONEq(t, string(env.Data), string(env2.Data), "重复调用应幂等")
}

// Outcome unauthorized-401: 豁免(见 doc.go)——认证由全局鉴权中间件承载,
// disk 读取面自身无认证逻辑。
func TestStep1_TrendQuery_UnauthorizedExempt(t *testing.T) {
	t.Log("exempt: 401 enforced by global auth middleware outside the disk surface")
}
