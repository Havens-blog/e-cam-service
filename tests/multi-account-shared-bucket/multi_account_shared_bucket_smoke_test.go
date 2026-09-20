// @feature oss-ops-insight @api-functional
//
// Journey smoke: full shared-bucket golden path in sequence — three accounts
// collect the same bucket name → per-account rows land → same-day rerun is
// absorbed → Top dedups to one representative row → per-account trend reads
// stay isolated. Only happy-path outcomes.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multi_account_shared_bucket

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

func TestMultiAccountSharedBucket_FullJourneySmoke(t *testing.T) {
	h := osstest.NewHarness()
	p := h.NewPerAccountProvider()
	for _, id := range []int64{1, 2, 3} {
		h.SeedAccountNamed(id, p.Name)
		h.SeedBucket(id, p.Name, sharedBucketName)
	}

	// Step 1: 三账号采集同名 bucket → 各留一行
	for _, id := range []int64{1, 2, 3} {
		p.QuerierFor(id).SetMetrics(osstest.Metric(sharedBucketName, osstest.Today(), float64(100*id), int64(10*id)))
	}
	_, err := h.RunCollect(t, nil)
	require.NoError(t, err)
	requireSharedRows(t, h, osstest.Today(), map[int64]float64{1: 100, 2: 200, 3: 300})

	// Step 2: 同日重跑幂等吸收,首写值不变
	for _, id := range []int64{1, 2, 3} {
		p.QuerierFor(id).SetMetrics(osstest.Metric(sharedBucketName, osstest.Today(), float64(111*id), int64(11*id)))
	}
	_, err = h.RunCollect(t, nil)
	require.NoError(t, err)
	requireSharedRows(t, h, osstest.Today(), map[int64]float64{1: 100, 2: 200, 3: 300})

	// Step 4: Top 去重单代表行,不跨账号求和(代表行取同日容量最大 300)
	top, err := h.NewQueryService().GetTop(context.Background(), 1, 0, 7, service.OSSSortStorageSize, 10, 1, 10)
	require.NoError(t, err)
	require.Len(t, top.Items, 1)
	require.InDelta(t, 300, *top.Items[0].Latest.StorageSize, 1e-9)
	require.Equal(t, []int64{1, 2, 3}, top.Items[0].AccountIDs)

	// Step 5: 趋势按账号精确隔离
	respA, err := h.NewQueryService().GetBucketMetrics(context.Background(), 1, 1, sharedBucketName, 7)
	require.NoError(t, err)
	respB, err := h.NewQueryService().GetBucketMetrics(context.Background(), 1, 2, sharedBucketName, 7)
	require.NoError(t, err)
	require.Equal(t, float64(100), *respA.Days[len(respA.Days)-1].StorageSize)
	require.Equal(t, float64(200), *respB.Days[len(respB.Days)-1].StorageSize)
}
