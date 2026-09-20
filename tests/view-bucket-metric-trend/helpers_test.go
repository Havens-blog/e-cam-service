// @feature oss-ops-insight @api-functional
//
// Journey fixtures for view-bucket-metric-trend contract tests: metric rows
// seeded directly into the in-memory metric table ( read-side journey ) plus
// the shared trend router.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package view_bucket_metric_trend

import (
	"fmt"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/osstest"
)

// trendBucket 趋势 journey 的标准 bucket 名。
const trendBucket = "trend-bucket"

// newJourneyHarness builds the journey world with one metric-capable vendor.
func newJourneyHarness() (*osstest.Harness, *osstest.Provider) {
	h := osstest.NewHarness()
	provider := h.NewProvider(true)
	return h, provider
}

// seedTrendAccount 注册活跃账号并纳管趋势 bucket。
func seedTrendAccount(t *testing.T, h *osstest.Harness, provider *osstest.Provider, accountID int64) {
	t.Helper()
	h.SeedAccount(accountID, provider)
	h.SeedBucket(accountID, provider.Name, trendBucket)
}

// seedTrendWindow 落库近 windowDays 天的连续指标行(容量基数 base,逐日 +1)。
func seedTrendWindow(t *testing.T, h *osstest.Harness, provider *osstest.Provider, accountID int64, windowDays, base int) {
	t.Helper()
	for d := windowDays; d >= 1; d-- {
		storage := float64(base + (windowDays - d))
		h.SeedMetric(t, accountID, provider.Name,
			osstest.Metric(trendBucket, osstest.DayOffset(-d+1), storage, int64(10+(windowDays-d))))
	}
}

// seedSingleDay 落库指定偏移日的单行指标。
func seedSingleDay(t *testing.T, h *osstest.Harness, provider *osstest.Provider, accountID int64, dayOffset int, storage float64, objects int64) {
	t.Helper()
	h.SeedMetric(t, accountID, provider.Name,
		osstest.Metric(trendBucket, osstest.DayOffset(dayOffset), storage, objects))
}

// trendPath 趋势接口路径构造。
func trendPath(accountID int64, days int) string {
	return fmt.Sprintf("/assets/oss/metrics?bucket_name=%s&account_id=%d&days=%d", trendBucket, accountID, days)
}
