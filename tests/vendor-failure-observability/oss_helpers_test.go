// @feature oss-ops-insight @api-functional
//
// Journey fixtures for the oss-ops-insight vendor-failure-observability
// contract tests ( co-located with the nas-ops-insight suite of the same
// journey — OSS symbols carry the oss prefix to coexist in this package ):
// mandatory/best-effort OSS vendors wired to the production collect executor.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package vendor_failure_observability

import (
	"testing"

	"github.com/Havens-blog/e-common-go/taskx"
	"github.com/Havens-blog/e-cam-service/tests/nastest"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// nastestResultAlerts 读取采集任务 Result 的 health_alerts 厂商清单。
func nastestResultAlerts(t *testing.T, task *taskx.Task) []string {
	t.Helper()
	return nastest.ResultAlerts(t, task)
}

// ossNewHarness builds the OSS journey world with one metric-capable vendor.
func ossNewHarness() (*osstest.Harness, *osstest.Provider) {
	h := osstest.NewHarness()
	provider := h.NewProvider(true)
	return h, provider
}

// ossNewMandatoryHarness 以必达厂商字面量名(aliyun/huawei/aws)注册厂商,
// 命中自我健康监控的必达清单;尽力而为厂商传 tencent/volcengine。
func ossNewMandatoryHarness(t *testing.T, mandatoryName string) (*osstest.Harness, *osstest.Provider) {
	t.Helper()
	h := osstest.NewHarness()
	provider := h.NewNamedProvider(mandatoryName, true)
	return h, provider
}

// ossSeedBucketAccount 注册活跃账号并挂 n 个 OSS bucket 资产。
func ossSeedBucketAccount(t *testing.T, h *osstest.Harness, provider *osstest.Provider, accountID int64, bucketNames ...string) {
	t.Helper()
	h.SeedAccount(accountID, provider)
	for _, name := range bucketNames {
		h.SeedBucket(accountID, provider.Name, name)
	}
}

// ossRequireRowsInWindow 断言各账号今日行存在且容量各归其主。
func ossRequireRowsInWindow(t *testing.T, h *osstest.Harness, date string, expect map[int64]float64) {
	t.Helper()
	for accountID, storage := range expect {
		row, ok := h.MetricDAO.Row(accountID, "ok-bucket", date)
		require.True(t, ok, "账号 %d 在 %s 应保留自己的指标行", accountID, date)
		require.Equal(t, storage, row.StorageSize, "账号 %d 的容量口径不得被其他账号写入覆盖", accountID)
	}
}
