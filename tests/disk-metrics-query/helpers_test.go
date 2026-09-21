// @feature disk-ops-insight @api-functional
//
// Journey fixtures for disk-metrics-query contract tests: seeded metric rows
// read straight through the production DiskQueryService + gin disk routes.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package disk_metrics_query

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/disktest"
)

// newJourneyHarness builds the read-world with one metric-capable vendor.
func newJourneyHarness() (*disktest.Harness, *disktest.Provider) {
	h := disktest.NewHarness()
	provider := h.NewProvider(true)
	return h, provider
}

// seedT 直写一行指标(经写入门禁,0 值行自动打 zero_exception)。
func seedT(t *testing.T, h *disktest.Harness, accountID int64, providerName, diskID, date string, usage, iops, throughput float64) {
	t.Helper()
	h.SeedMetric(t, accountID, providerName, disktest.Metric(diskID, date, usage, iops, throughput))
}
