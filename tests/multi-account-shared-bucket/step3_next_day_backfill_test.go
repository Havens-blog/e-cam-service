// @feature oss-ops-insight @api-functional
//
// Contract step-3-next-day-backfill: 次日补采覆盖昨日行 — each account's
// yesterday row is overwritten to the vendor day-end aggregate while today
// rows stay independent, and the overwrite never crosses accounts.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multi_account_shared_bucket

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// Outcome per-account-yesterday-overwritten: 次日采集包含昨日区间,三账号的
// 昨日行各自被覆盖更新为厂商日末聚合值;今日行独立不受影响。
func TestStep3_PerAccountYesterdayOverwritten(t *testing.T) {
	h := osstest.NewHarness()
	p := h.NewPerAccountProvider()
	for _, id := range []int64{1, 2, 3} {
		h.SeedAccountNamed(id, p.Name)
		h.SeedBucket(id, p.Name, sharedBucketName)
		// 三账号昨日凌晨初态行(容量均为 90)
		h.SeedMetric(t, id, p.Name, osstest.Metric(sharedBucketName, osstest.DayOffset(-1), 90, 9))
	}
	// 次日补采:各账号厂商返回昨日日末聚合值 + 今日初态
	dayEnd := map[int64]float64{1: 110, 2: 330, 3: 220}
	for _, id := range []int64{1, 2, 3} {
		p.QuerierFor(id).SetMetrics(
			osstest.Metric(sharedBucketName, osstest.DayOffset(-1), dayEnd[id], int64(dayEnd[id]/10)),
			osstest.Metric(sharedBucketName, osstest.Today(), 120, 12),
		)
	}

	_, err := h.RunCollect(t, nil)
	require.NoError(t, err)

	// 昨日行按账号各归其主覆盖为日末态(账号 2 容量最大 330)
	for id, want := range map[int64]float64{1: 110, 2: 330, 3: 220} {
		row, ok := h.MetricDAO.Row(id, sharedBucketName, osstest.DayOffset(-1))
		require.True(t, ok, "账号 %d 在昨日应保留自己的指标行", id)
		require.Equal(t, want, row.StorageSize, "账号 %d 的昨日行应覆盖为各自日末值", id)
	}

	// 今日行独立首写,不受补采影响:三账号昨日行 + 三账号今日行 = 6
	require.Equal(t, 6, h.MetricDAO.Count(), "三账号 × (昨日补采 + 今日首写) = 6")
	for _, id := range []int64{1, 2, 3} {
		row, ok := h.MetricDAO.Row(id, sharedBucketName, osstest.Today())
		require.True(t, ok, "账号 %d 今日行应独立首写", id)
		require.Equal(t, float64(120), row.StorageSize)
	}
}
