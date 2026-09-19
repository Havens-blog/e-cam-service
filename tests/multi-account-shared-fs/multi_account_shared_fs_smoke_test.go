// @feature nas-ops-insight @api-functional
//
// Journey smoke: multi-account-shared-fs happy path — three accounts collect
// the same physical fs, three rows coexist under the unique key, trend reads
// stay account-isolated, and the aggregation dedups the shared fs into one
// item carrying the deduped ascending account list.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multi_account_shared_fs

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/stretchr/testify/require"
)

func TestMultiAccountSharedFs_FullJourneySmoke(t *testing.T) {
	h, provider := newJourneyHarness()

	// Step 1: 三账号同 fs 并发采集,各产出各自口径
	seedSharedFSCollect(t, h, provider, map[int64]float64{1: 100, 2: 200, 3: 300})
	task, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	require.Equal(t, 3, nastest.ResultField(t, task, "metrics_total").(int))

	// Step 2: 唯一键各行独立落库(三行并存,无覆盖)
	require.Equal(t, 3, h.MetricDAO.Count())
	nastest.RequireNoCrossAccountLoss(t, h.MetricDAO, SharedFS, nastest.Today(),
		map[int64]float64{1: 100, 2: 200, 3: 300})

	// Step 3: 趋势按账号隔离读取
	query := h.NewQueryService()
	ctx := context.Background()
	trend3, err := query.GetFsMetrics(ctx, h.TenantID, 3, SharedFS, 30)
	require.NoError(t, err)
	today3 := trend3.Days[len(trend3.Days)-1]
	require.NotNil(t, today3.Capacity)
	require.Equal(t, float64(300), *today3.Capacity)

	// Step 4: 聚合层去重消费(代表行 + 去重账号列表)
	top, err := query.GetTop(ctx, h.TenantID, 0, 30, service.NASSortCapacity, 10, 1, 10)
	require.NoError(t, err)
	require.Equal(t, 1, top.Total)
	item := top.Items[0]
	require.NotNil(t, item.Latest.Capacity)
	require.Equal(t, float64(300), *item.Latest.Capacity)
	require.Equal(t, []int64{1, 2, 3}, item.AccountIDs)
}
