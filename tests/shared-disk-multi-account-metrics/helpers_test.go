// @feature disk-ops-insight @api-functional
//
// Journey fixtures for shared-disk-multi-account-metrics contract tests: two
// accounts sharing one disk_id, per-account queriers with independent fault
// injection, and the production DiskQueryService + gin disk routes.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package shared_disk_multi_account_metrics

import (
	"testing"

	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-cam-service/tests/disktest"
	"github.com/stretchr/testify/require"
)

// sharedDiskID 共享盘 ID:两账号同时纳管同一物理盘。
const sharedDiskID = "dsk-shared-1"

// newJourneyHarness builds the shared-disk world: one per-account provider so
// each account's querier ( and fault injection ) stays independent.
func newJourneyHarness() (*disktest.Harness, *disktest.PerAccountProvider) {
	h := disktest.NewHarness()
	provider := h.NewPerAccountProvider()
	return h, provider
}

// seedSharedDiskAccount 注册一个活跃账号并挂共享盘资产。
func seedSharedDiskAccount(t *testing.T, h *disktest.Harness, provider *disktest.PerAccountProvider, accountID int64) {
	t.Helper()
	h.SeedAccountNamed(accountID, provider.Name)
	h.SeedDisk(accountID, provider.Name, sharedDiskID, "cn-test-1")
}

// seedSharedMetricT 直写一行共享盘指标(经写入门禁,0 值行自动打标注)。
func seedSharedMetricT(t *testing.T, h *disktest.Harness, accountID int64, providerName, date string, usage, iops, throughput float64) {
	t.Helper()
	m := disktest.Metric(sharedDiskID, date, usage, iops, throughput)
	m.UsageScope = types.DiskUsageScopeInstanceLevel
	h.SeedMetric(t, accountID, providerName, m)
}

// seedRowWithScope 直写一行任意盘指标并显式指定 usage_scope(0 值甄别用)。
func seedRowWithScope(t *testing.T, h *disktest.Harness, accountID int64, providerName, diskID, date string, usage, iops, throughput float64, scope string) {
	t.Helper()
	m := disktest.Metric(diskID, date, usage, iops, throughput)
	m.UsageScope = scope
	h.SeedMetric(t, accountID, providerName, m)
	require.NotEmpty(t, diskID)
}

// seedT 直写一行任意盘指标(instance_level 口径)。
func seedT(t *testing.T, h *disktest.Harness, accountID int64, providerName, diskID, date string, usage, iops, throughput float64) {
	t.Helper()
	seedRowWithScope(t, h, accountID, providerName, diskID, date, usage, iops, throughput, types.DiskUsageScopeInstanceLevel)
}
