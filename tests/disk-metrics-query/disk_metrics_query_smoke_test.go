// @feature disk-ops-insight @api-functional
//
// Journey smoke for disk-metrics-query: the read-only happy path end-to-end —
// trend query with latest/average, the account-scoped Top with paging
// metadata, and the zero-usage anomaly closure — plus the journey invariants
// ( parameter bounds, honest data, read-only idempotency ).
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

func TestDiskMetricsQuery_FullJourneySmoke(t *testing.T) {
	h, provider := newJourneyHarness()
	h.SeedAccount(1, provider)
	seedT(t, h, 1, provider.Name, "dsk-a", disktest.DayOffset(-1), 60.0, 120, 9.0)
	seedT(t, h, 1, provider.Name, "dsk-a", disktest.Today(), 50.0, 100, 8.0)
	seedT(t, h, 1, provider.Name, "dsk-b", disktest.Today(), 20.0, 40, 2.0)
	router := h.NewDiskRouter(1)

	// Step 1: 查询单盘趋势(200,升序,latest + average)
	status, env := disktest.GetJSON(t, router, "/assets/disk/metrics?disk_id=dsk-a&account_id=1&days=30")
	require.Equal(t, 200, status, "step1: 趋势查询应 200")
	trendData := env.Data
	var trend service.DiskMetricsResp
	require.NoError(t, json.Unmarshal(env.Data, &trend))
	require.NotNil(t, trend.Latest)
	require.Equal(t, disktest.Today(), trend.Latest.Date)
	require.InDelta(t, 50.0, *trend.Latest.UsagePercent, 1e-9)
	require.NotNil(t, trend.Average)
	require.InDelta(t, 55.0, *trend.Average.UsagePercent, 1e-9, "step1: 均值基于实际存在日 (60+50)/2")

	// Step 2: 查询账号视角 Top 榜(200,均值口径排序,分页元数据)
	status, env = disktest.GetJSON(t, router,
		"/assets/disk/top?account_id=1&days=7&sort=usage_percent&top=10&page=1&page_size=20")
	require.Equal(t, 200, status, "step2: Top 查询应 200")
	var top service.DiskTopResp
	require.NoError(t, json.Unmarshal(env.Data, &top))
	require.Equal(t, 2, top.Total)
	require.Equal(t, "dsk-a", top.Items[0].DiskID, "step2: Top 按均值排序 dsk-a 在前")

	// Step 3: 分辨 qc_status 异常行(口径缺失 0 行 → data_status=zero_exception)
	zeroRow := disktest.Metric("dsk-zero", disktest.Today(), 0, 0, 0)
	zeroRow.UsageScope = ""
	h.SeedMetric(t, 1, provider.Name, zeroRow)
	status, env = disktest.GetJSON(t, router, "/assets/disk/metrics?disk_id=dsk-zero&account_id=1&days=7")
	require.Equal(t, 200, status, "step3: 异常行盘趋势应 200")
	var zeroTrend service.DiskMetricsResp
	require.NoError(t, json.Unmarshal(env.Data, &zeroTrend))
	found := false
	for _, day := range zeroTrend.Days {
		if day.Date == disktest.Today() {
			found = true
			require.Equal(t, service.DiskDataStatusZeroException, day.DataStatus, "step3: 0 值异常应可分辨")
		}
	}
	require.True(t, found, "step3: 今日行应存在")

	// Journey Invariants: 参数边界恒成立 + 只读幂等
	status, _ = disktest.GetJSON(t, router, "/assets/disk/metrics?disk_id=dsk-a&account_id=1&days=91")
	require.Equal(t, 400, status, "invariant: days 越出 1~90 应 400")
	status2, env2 := disktest.GetJSON(t, router, "/assets/disk/metrics?disk_id=dsk-a&account_id=1&days=30")
	require.Equal(t, 200, status2)
	require.JSONEq(t, string(trendData), string(env2.Data), "invariant: 重复调用幂等(只读无副作用)")
}
