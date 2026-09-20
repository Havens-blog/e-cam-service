// @feature oss-ops-insight @api-functional
//
// Contract step-2-same-day-idempotent: 同日幂等(首写生效) — same-day
// re-collection keeps each account's first-write value, and cross-account
// aggregation never double-counts a shared bucket.
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

// Outcome first-write-kept-per-account: 三账号的 shared-assets 今日行均已存在,
// 同日再次采集对同键重复写入 — 各账号行保持首写值不被覆盖,无错误,行数不增长。
func TestStep2_FirstWriteKeptPerAccount(t *testing.T) {
	h := osstest.NewHarness()
	p := h.NewPerAccountProvider()
	for _, id := range []int64{1, 2, 3} {
		h.SeedAccountNamed(id, p.Name)
		h.SeedBucket(id, p.Name, sharedBucketName)
	}
	// 首写:各账号今日行(100/300/200)
	for _, id := range []int64{1, 2, 3} {
		p.QuerierFor(id).SetMetrics(osstest.Metric(sharedBucketName, osstest.Today(), float64(100*id), int64(10*id)))
	}
	_, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	// 同日重采:厂商当日口径已变化
	for _, id := range []int64{1, 2, 3} {
		p.QuerierFor(id).SetMetrics(osstest.Metric(sharedBucketName, osstest.Today(), float64(111*id), int64(11*id)))
	}
	_, err = h.RunCollect(t, nil)
	require.NoError(t, err)

	// 首写生效仅保护今日行:三账号行内容不变,行数不增长
	requireSharedRows(t, h, osstest.Today(), map[int64]float64{1: 100, 2: 200, 3: 300})
}

// Outcome cross-account-aggregation-guard: 三账号同名 bucket 数值互不相同时,
// Top 聚合禁止跨账号求和/双计 — 代表行取同日容量最大行,总量按唯一键隔离行
// 各自统计。
func TestStep2_CrossAccountAggregationGuard(t *testing.T) {
	h, provider := newJourneyHarness()
	for _, id := range []int64{1, 2, 3} {
		seedSharedBucketAccount(t, h, provider, id)
	}
	seedSharedMetricT(t, h, 1, provider.Name, osstest.Today(), 100, 10)
	seedSharedMetricT(t, h, 2, provider.Name, osstest.Today(), 200, 20)
	seedSharedMetricT(t, h, 3, provider.Name, osstest.Today(), 300, 30)

	top, err := h.NewQueryService().GetTop(context.Background(), 1, 0, 7, service.OSSSortStorageSize, 10, 1, 10)
	require.NoError(t, err)

	require.Len(t, top.Items, 1, "同名 bucket 在 Top 中只出现一次")
	item := top.Items[0]
	require.Equal(t, sharedBucketName, item.BucketName)
	require.NotNil(t, item.Latest.StorageSize)
	require.Equal(t, float64(300), *item.Latest.StorageSize,
		"代表行取同日容量最大行,绝不跨账号求和(100+300+200=600 不得出现)")
	require.Equal(t, []int64{1, 2, 3}, item.AccountIDs, "account_id 为去重升序账号列表")
	require.InDelta(t, 300, *item.Average.StorageSize, 1e-9,
		"均值亦按每日代表行计算,不叠加多账号数值")
}
