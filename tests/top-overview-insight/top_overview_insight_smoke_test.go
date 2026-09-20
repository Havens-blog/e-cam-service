// @feature oss-ops-insight @api-functional
//
// Journey smoke: the read-side golden path in sequence — metric rows seeded,
// default Top sorted by average storage, sort switch to object_count honored,
// pagination seamless, ops-card values all metric-table-sourced. Only
// happy-path outcomes.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package top_overview_insight

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

func TestTopOverviewInsight_FullJourneySmoke(t *testing.T) {
	h, provider := newJourneyHarness()
	h.SeedAccount(1, provider)
	// 三个 bucket:容量排序 big>middle>small,对象数排序反转
	world := []struct {
		name    string
		storage float64
		objects int64
	}{
		{name: "big", storage: 900, objects: 100},
		{name: "middle", storage: 500, objects: 1000},
		{name: "small", storage: 100, objects: 9000},
	}
	for _, b := range world {
		h.SeedBucket(1, provider.Name, b.name)
		for d := 1; d <= 7; d++ {
			h.SeedMetric(t, 1, provider.Name,
				osstest.Metric(b.name, osstest.DayOffset(-d+1), b.storage+float64(d), b.objects))
		}
	}

	// Step 1: 默认按近 7 天均值容量降序
	top, err := h.NewQueryService().GetTop(context.Background(), 1, 0, 7, service.OSSSortStorageSize, 10, 1, 10)
	require.NoError(t, err)
	require.Len(t, top.Items, 3)
	require.Equal(t, "big", top.Items[0].BucketName)
	require.Equal(t, "small", top.Items[2].BucketName)

	// Step 2: 切换对象数维度,排序真实反转
	byObjects, err := h.NewQueryService().GetTop(context.Background(), 1, 0, 7, service.OSSSortObjectCount, 10, 1, 10)
	require.NoError(t, err)
	require.Equal(t, "small", byObjects.Items[0].BucketName)

	// Step 3: 分页无缝(page=2/page_size=2 → 第 3 条)
	page2, err := h.NewQueryService().GetTop(context.Background(), 1, 0, 7, service.OSSSortStorageSize, 10, 2, 2)
	require.NoError(t, err)
	require.Len(t, page2.Items, 1)
	require.Equal(t, "small", page2.Items[0].BucketName)
	require.Equal(t, 3, page2.Total)

	// Step 4: 运营卡数值全部来自指标表(均值/最新值齐备)
	require.InDelta(t, 904, *top.Items[0].Average.StorageSize, 1e-9)
	require.InDelta(t, 901, *top.Items[0].Latest.StorageSize, 1e-9)
}
