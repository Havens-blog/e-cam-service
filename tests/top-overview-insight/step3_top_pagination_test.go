// @feature oss-ops-insight @api-functional
//
// Contract step-3-top-pagination: 分页浏览大结果集 — page 2 seamlessly
// continues page 1 ( no overlap, no gap ), and an out-of-range page returns
// an empty items page with the total unchanged ( never a 500 ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package top_overview_insight

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// context0 直读 DAO/服务用 context。
func context0() context.Context { return context.Background() }

// seedPageWorld 15 个含数据 bucket(fixture_spec: OSSMetricRow>=15)。
func seedPageWorld(t *testing.T) (*osstest.Harness, *osstest.Provider) {
	t.Helper()
	h, provider := newJourneyHarness()
	h.SeedAccount(1, provider)
	for i := 0; i < 15; i++ {
		name := fmt.Sprintf("page-bucket-%02d", i)
		h.SeedBucket(1, provider.Name, name)
		h.SeedMetric(t, 1, provider.Name, osstest.Metric(name, osstest.Today(), float64(100+i), int64(i)))
	}
	return h, provider
}

// fetchTopPage 请求 Top 并解析分页响应,返回去重后的 bucket 名清单。
func fetchTopPage(t *testing.T, h *osstest.Harness, query string) (total int, names []string) {
	t.Helper()
	status, env := osstest.GetJSON(t, h.NewOSSRouter(1), "/assets/oss/top?days=7&"+query)
	require.Equal(t, 200, status)
	var resp service.OSSTopResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	names = make([]string, 0, len(resp.Items))
	for _, item := range resp.Items {
		names = append(names, item.BucketName)
	}
	return resp.Total, names
}

// Outcome pagination-seamless: page=2&page_size=10 返回第二页与分页元信息,
// 翻页结果与第一页无缝衔接、不重不漏。
func TestStep3_PaginationSeamless(t *testing.T) {
	h, _ := seedPageWorld(t)

	total1, page1 := fetchTopPage(t, h, "page=1&page_size=10")
	total2, page2 := fetchTopPage(t, h, "page=2&page_size=10")
	require.Equal(t, 15, total1)
	require.Equal(t, total1, total2, "分页元信息 total 各页一致")
	require.Len(t, page1, 10)
	require.Len(t, page2, 5, "第二页收尾 5 条")

	// 不重不漏:两页并集 = 全集,交集为空
	seen := make(map[string]bool)
	for _, name := range append(append([]string{}, page1...), page2...) {
		require.False(t, seen[name], "bucket %s 跨页重复", name)
		seen[name] = true
	}
	require.Len(t, seen, 15)
}

// Outcome page-out-of-range-empty: page=99 返回空数据页与正确分页元信息
// (total 不变);不报 500、不重复返回已有页数据。
func TestStep3_PageOutOfRangeEmpty(t *testing.T) {
	h, _ := seedPageWorld(t)

	total, page := fetchTopPage(t, h, "page=99&page_size=10")
	require.Equal(t, 15, total, "页码超界 total 不变")
	require.Empty(t, page, "超界页返回空数据页")
}

// 边界锚:page_size 上限 50 收敛(page_size=100 → 50),页码缺省回 1。
func TestStep3_PageSizeBoundNormalized(t *testing.T) {
	h, _ := seedPageWorld(t)
	top, err := h.NewQueryService().GetTop(context0(), 1, 0, 7, service.OSSSortStorageSize, 10, 1, 100)
	require.NoError(t, err)
	require.Equal(t, 50, top.PageSize, "page_size 超上限收敛到 50")
	require.Equal(t, 15, top.Total)
	require.Len(t, top.Items, 15)
}
