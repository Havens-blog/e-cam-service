// @feature oss-ops-insight @api-functional
//
// Contract step-2-top-sort-object-count: 切换排序维度为对象数 — sort=object_count
// sorts by the near-N-day average object count with the same dedup semantics;
// out-of-domain sort values answer 400 naming the legal enum, and an
// over-limit top is normalized server-side to the 50 cap.
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

// seedObjectCountWorld 三 bucket 对象数互有差异,且对象数排序与容量排序相反
// (守护排序维度真实切换而非沿用容量)。
func seedObjectCountWorld(t *testing.T) (*osstest.Harness, *osstest.Provider) {
	t.Helper()
	h, provider := newJourneyHarness()
	h.SeedAccount(1, provider)
	world := []struct {
		name    string
		storage float64
		objects int64
	}{
		{name: "tiny-but-many", storage: 100, objects: 9000},
		{name: "huge-but-few", storage: 900, objects: 100},
		{name: "middle", storage: 500, objects: 1000},
	}
	for _, b := range world {
		h.SeedBucket(1, provider.Name, b.name)
		for d := 1; d <= 3; d++ {
			h.SeedMetric(t, 1, provider.Name,
				osstest.Metric(b.name, osstest.DayOffset(-d+1), b.storage+float64(d), b.objects))
		}
	}
	return h, provider
}

// Outcome top-sorted-by-avg-object-count: sort=object_count 返回按对象数
// 近 N 天均值口径降序的 Top;bucket_name 去重语义与容量排序一致。
func TestStep2_TopSortedByAvgObjectCount(t *testing.T) {
	h, _ := seedObjectCountWorld(t)
	router := h.NewOSSRouter(1)

	status, env := osstest.GetJSON(t, router, "/assets/oss/top?days=7&sort=object_count")
	require.Equal(t, 200, status)

	var resp service.OSSTopResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.Len(t, resp.Items, 3)
	// 对象数均值降序:tiny-but-many 9000 > middle 1000 > huge-but-few 100
	require.Equal(t, "tiny-but-many", resp.Items[0].BucketName)
	require.Equal(t, "middle", resp.Items[1].BucketName)
	require.Equal(t, "huge-but-few", resp.Items[2].BucketName)
	require.NotNil(t, resp.Items[0].Average.ObjectCount)
	require.InDelta(t, 9000, *resp.Items[0].Average.ObjectCount, 1e-9, "排序口径为对象数均值")
}

// Outcome invalid-query-parameter: sort 携带合法域之外值 → 400 并明确提示
// 合法枚举;top 超上限 → 服务端按上限 50 收敛,不返回未受限结果集。
func TestStep2_InvalidQueryParameter(t *testing.T) {
	h, _ := seedObjectCountWorld(t)
	router := h.NewOSSRouter(1)

	// 非法 sort → 400,提示合法域 storage_size|object_count
	status, env := osstest.GetJSON(t, router, "/assets/oss/top?sort=cost")
	require.Equal(t, 400, status)
	require.Contains(t, env.Msg, "storage_size")
	require.Contains(t, env.Msg, "object_count")

	// 超上限参数 → 服务端按上限 50 收敛,不返回未受限结果集
	// (top=100/page_size=100 均被 normalizeBound 收敛到 50,55 个 bucket 只回 50 条)
	h2, provider := newJourneyHarness()
	h2.SeedAccount(1, provider)
	for i := 0; i < 55; i++ {
		name := fmt.Sprintf("cap-bucket-%02d", i)
		h2.SeedBucket(1, provider.Name, name)
		h2.SeedMetric(t, 1, provider.Name, osstest.Metric(name, osstest.Today(), float64(100+i), int64(i)))
	}
	top, err := h2.NewQueryService().GetTop(context.Background(), 1, 0, 7, service.OSSSortStorageSize, 100, 1, 100)
	require.NoError(t, err)
	require.LessOrEqual(t, len(top.Items), 50, "top/page_size 参数必须在服务端收敛到上限 50")
	require.Equal(t, 50, len(top.Items), "超上限参数收敛后应恰好返回 50 条")
}

// top cap 语义锚:service 层与 handler 层共用同一 normalize 收敛(1~50)。
func TestStep2_TopNormalization_ServiceConsistent(t *testing.T) {
	h, _ := seedObjectCountWorld(t)
	// top=0 回默认 10;此处仅 3 个 bucket,断言不报错且全量返回
	top, err := h.NewQueryService().GetTop(context.Background(), 1, 0, 7, service.OSSSortObjectCount, 0, 1, 10)
	require.NoError(t, err)
	require.Len(t, top.Items, 3)
}
