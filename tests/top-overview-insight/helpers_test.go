// @feature oss-ops-insight @api-functional
//
// Journey fixtures for top-overview-insight contract tests: multi-bucket /
// multi-day metric worlds seeded directly into the in-memory metric table
// ( read-side journeys ), plus the shared read router.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package top_overview_insight

import (
	"fmt"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/osstest"
)

// newJourneyHarness builds the journey world with one metric-capable vendor.
func newJourneyHarness() (*osstest.Harness, *osstest.Provider) {
	h := osstest.NewHarness()
	provider := h.NewProvider(true)
	return h, provider
}

// seedTopWorld 注册一个活跃账号并直接落库 n 个 bucket 的指标行
// (读取侧 journey 以指标表为输入,资产表仅用于账号归属)。
func seedTopWorld(t *testing.T, h *osstest.Harness, provider *osstest.Provider, accountID int64, bucketCount int, prefix string) {
	t.Helper()
	h.SeedAccount(accountID, provider)
	for i := 0; i < bucketCount; i++ {
		name := fmt.Sprintf("%s-%02d", prefix, i)
		h.SeedBucket(accountID, provider.Name, name)
		// 近 7 天逐日行:容量随 bucket 序号递减形成稳定排序
		for d := 1; d <= 7; d++ {
			storage := float64(1000-10*i) + float64(d)
			h.SeedMetric(t, accountID, provider.Name, osstest.Metric(name, osstest.DayOffset(-d+1), storage, int64(100-i)))
		}
	}
}
