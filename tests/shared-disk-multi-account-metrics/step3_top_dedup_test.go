// @feature disk-ops-insight @api-functional
//
// Contract shared-disk-multi-account-metrics step-3-top-dedup: Top dedups the
// shared disk into one entry whose representative row follows "date desc,
// then usage desc" ( same-day tie keeps the result deterministic ), values
// are never summed across accounts, dedup happens before paging, and an empty
// account scope answers a non-error empty list.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package shared_disk_multi_account_metrics

import (
	"encoding/json"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/stretchr/testify/require"
)

// seedCrossDateWorld 三账号共享盘跨日期/跨值行(代表行选择世界)。
func seedCrossDateWorld(t *testing.T) (*disktest.Harness, string) {
	t.Helper()
	h, provider := newJourneyHarness()
	for _, id := range []int64{1, 2, 3} {
		acc := h.SeedAccountNamed(id, provider.Name)
		h.SeedDisk(acc.ID, provider.Name, sharedDiskID, "cn-test-1")
	}
	seedSharedMetricT(t, h, 1, provider.Name, disktest.DayOffset(-2), 50.0, 50, 5.0) // 最旧
	seedSharedMetricT(t, h, 2, provider.Name, disktest.DayOffset(-1), 40.0, 40, 4.0)
	seedSharedMetricT(t, h, 3, provider.Name, disktest.DayOffset(-1), 45.0, 45, 4.5) // 昨日使用率最大
	seedSharedMetricT(t, h, 2, provider.Name, disktest.Today(), 20.0, 20, 2.0)       // 最新但小
	return h, provider.Name
}

// Outcome success: 同一 disk_id 在 Top 结果中至多出现一次;代表行取
// 「日期 desc,再使用率 desc」规则选出的行;item 携带去重后账号列表、latest
// (代表行最新一天)、average(按每日代表行计算的均值);不跨账号求和。
func TestStep3_TopDedup_SingleRepresentativeRow(t *testing.T) {
	h, _ := seedCrossDateWorld(t)
	router := h.NewDiskRouter(1)

	status, env := disktest.GetJSON(t, router, "/assets/disk/top?days=7")
	require.Equal(t, 200, status)

	var resp service.DiskTopResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.Equal(t, 1, resp.Total, "共享盘去重后 total=1")
	require.Len(t, resp.Items, 1)

	item := resp.Items[0]
	require.Equal(t, sharedDiskID, item.DiskID)
	require.NotNil(t, item.Latest.UsagePercent)
	// 最新日期(今日)只有账号 2 一行 → 代表行 20;不是跨账号最大 50,更不是求和
	require.InDelta(t, 20.0, *item.Latest.UsagePercent, 1e-9, "代表行取最新日期行(日期 desc 优先)")
	require.Equal(t, disktest.Today(), item.Latest.Date)
	require.Equal(t, []int64{1, 2, 3}, item.AccountIDs, "AccountIDs 为该盘全部有行账号去重升序列表")
	// average 按每日代表行计算:每日代表 = (50), (45), (20) → 均值 ≈38.33
	require.NotNil(t, item.Average.UsagePercent)
	require.InDelta(t, (50.0+45.0+20.0)/3.0, *item.Average.UsagePercent, 1e-9,
		"均值按每日代表行计算,不跨账号双计")
}

// Outcome rep-row-tie-break: 两账号行日期相同且使用率相同(排序键并列)—
// 同日内确定唯一代表行,同一 disk_id 仍只出现一次,结果确定且稳定可复现。
func TestStep3_TopDedup_RepRowTieBreak(t *testing.T) {
	h, provider := newJourneyHarness()
	accA := h.SeedAccountNamed(1, provider.Name)
	accB := h.SeedAccountNamed(2, provider.Name)
	h.SeedDisk(accA.ID, provider.Name, sharedDiskID, "cn-test-1")
	h.SeedDisk(accB.ID, provider.Name, sharedDiskID, "cn-test-1")
	// 两账号行同日期同使用率(并列形态)
	seedSharedMetricT(t, h, accA.ID, provider.Name, disktest.Today(), 55.5, 100, 8.0)
	seedSharedMetricT(t, h, accB.ID, provider.Name, disktest.Today(), 55.5, 100, 8.0)

	router := h.NewDiskRouter(1)
	var first string
	for i := 0; i < 5; i++ {
		status, env := disktest.GetJSON(t, router, "/assets/disk/top?days=7")
		require.Equal(t, 200, status)
		var resp service.DiskTopResp
		require.NoError(t, json.Unmarshal(env.Data, &resp))
		require.Len(t, resp.Items, 1, "并列形态下共享盘仍只出现一次")
		raw, err := json.Marshal(resp.Items[0])
		require.NoError(t, err)
		if i == 0 {
			first = string(raw)
			continue
		}
		require.Equal(t, first, string(raw), "第 %d 次请求代表行应与前次完全一致(确定性)", i+1)
	}
}

// Outcome pagination-after-dedup: 共享盘较多,top=50(上限)、分页参数合法 —
// 去重发生在分页之前;每页内 disk_id 不重复;total 为去重后的磁盘总数;
// top 默认 10、最大 50。
func TestStep3_TopDedup_PaginationAfterDedup(t *testing.T) {
	h, provider := newJourneyHarness()
	// 3 块独立盘,其中 sharedDiskID 为两账号共享盘(去重后仍是 1 块)
	accA := h.SeedAccountNamed(1, provider.Name)
	accB := h.SeedAccountNamed(2, provider.Name)
	h.SeedDisk(accA.ID, provider.Name, sharedDiskID, "cn-test-1")
	h.SeedDisk(accB.ID, provider.Name, sharedDiskID, "cn-test-1")
	h.SeedDisk(accA.ID, provider.Name, "dsk-solo-1", "cn-test-1")
	h.SeedDisk(accA.ID, provider.Name, "dsk-solo-2", "cn-test-1")
	seedSharedMetricT(t, h, accA.ID, provider.Name, disktest.Today(), 66.0, 150, 11.0)
	seedSharedMetricT(t, h, accB.ID, provider.Name, disktest.Today(), 33.0, 70, 5.0)
	seedT(t, h, accA.ID, provider.Name, "dsk-solo-1", disktest.Today(), 10.0, 10, 1.0)
	seedT(t, h, accA.ID, provider.Name, "dsk-solo-2", disktest.Today(), 90.0, 900, 90.0)

	router := h.NewDiskRouter(1)
	// top=50 为合法上限
	status, env := disktest.GetJSON(t, router, "/assets/disk/top?days=7&top=50&page=1&page_size=2")
	require.Equal(t, 200, status)
	var resp service.DiskTopResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.Equal(t, 3, resp.Total, "total 为去重后的磁盘总数(共享盘计 1)")
	require.Equal(t, 1, resp.Page)
	require.Equal(t, 2, resp.PageSize)
	require.Len(t, resp.Items, 2)
	// 每页内 disk_id 不重复(去重先于分页)
	seen := map[string]struct{}{}
	for _, item := range resp.Items {
		require.NotContains(t, seen, item.DiskID, "每页内 disk_id 不得重复")
		seen[item.DiskID] = struct{}{}
	}
	// 第二页不重复且并集完整
	status, env = disktest.GetJSON(t, router, "/assets/disk/top?days=7&top=50&page=2&page_size=2")
	require.Equal(t, 200, status)
	var resp2 service.DiskTopResp
	require.NoError(t, json.Unmarshal(env.Data, &resp2))
	require.Len(t, resp2.Items, 1)
	for _, item := range resp2.Items {
		require.NotContains(t, seen, item.DiskID)
	}
	require.Equal(t, 3, resp2.Total, "total 稳定")
}

// Outcome empty-account-scope-empty-items: 查询范围解析后的账号集合为空(租户
// 无可纳管账号)— 返回空 items 列表与分页元数据,非错误响应。
func TestStep3_TopDedup_EmptyAccountScopeEmptyItems(t *testing.T) {
	h, _ := newJourneyHarness() // 租户 1 无任何账号
	router := h.NewDiskRouter(1)

	status, env := disktest.GetJSON(t, router, "/assets/disk/top?days=7")
	require.Equal(t, 200, status, "空账号范围是非错误空态")

	var resp service.DiskTopResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.Empty(t, resp.Items, "应返回空 items 列表")
	require.Equal(t, 0, resp.Total)
	require.Equal(t, 1, resp.Page, "分页元数据照常返回")
	require.Equal(t, 10, resp.PageSize, "page_size 缺省 10")
}
