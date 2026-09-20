// @feature oss-ops-insight @api-functional
//
// Journey fixtures for multi-account-shared-bucket contract tests: three
// accounts each managing the same bucket name, a per-account vendor querier
// for independent failure/capability injection, and the shared read router.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multi_account_shared_bucket

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// testCtx 直写 DAO 用 context。
func testCtx() context.Context { return context.Background() }

// sharedBucketName 三账号共管的同名 bucket(跨账号共享存储语义锚)。
const sharedBucketName = "shared-assets"

// newJourneyHarness builds the journey world with one metric-capable vendor.
func newJourneyHarness() (*osstest.Harness, *osstest.Provider) {
	h := osstest.NewHarness()
	provider := h.NewProvider(true)
	return h, provider
}

// seedSharedBucketAccount 注册一个活跃账号并纳管同名 shared bucket。
func seedSharedBucketAccount(t *testing.T, h *osstest.Harness, provider *osstest.Provider, accountID int64) {
	t.Helper()
	h.SeedAccount(accountID, provider)
	h.SeedBucket(accountID, provider.Name, sharedBucketName)
}

// seedSharedMetricT 直写一行 shared bucket 指标(经写入门禁)。
func seedSharedMetricT(t *testing.T, h *osstest.Harness, accountID int64, providerName, date string, storageGB float64, objects int64) {
	t.Helper()
	h.SeedMetric(t, accountID, providerName, osstest.Metric(sharedBucketName, date, storageGB, objects))
}

// requireSharedRows 断言各账号在同日 shared bucket 下各有一行且值各归其主
// (唯一键含 account_id 的 Hard Rule 实证)。
func requireSharedRows(t *testing.T, h *osstest.Harness, date string, expect map[int64]float64) {
	t.Helper()
	for accountID, storage := range expect {
		row, ok := h.MetricDAO.Row(accountID, sharedBucketName, date)
		require.True(t, ok, "账号 %d 在 %s 应保留自己的指标行", accountID, date)
		require.Equal(t, storage, row.StorageSize, "账号 %d 的容量口径不得被其他账号写入覆盖", accountID)
	}
	require.Equal(t, len(expect), h.MetricDAO.Count(), "唯一键之下不应产生合并/重复行")
}
