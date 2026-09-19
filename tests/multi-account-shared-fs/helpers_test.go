// @feature nas-ops-insight @api-functional
//
// Journey fixtures for multi-account-shared-fs contract tests: three accounts
// managing the same physical filesystem through a per-account vendor adapter.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multi_account_shared_fs

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/nastest"
)

// SharedFS 三账号共同纳管的物理文件系统 ID。
const SharedFS = "fs-jq-shared"

// newJourneyHarness 构建带按账号独立适配器的 journey 世界。
func newJourneyHarness() (*nastest.Harness, *nastest.PerAccountProvider) {
	h := nastest.NewHarness()
	return h, h.NewPerAccountProvider()
}

// seedSharedFSCollect 三账号各注册一个指向同一物理 fs 的实例,并让各自
// querier 返回该账号口径的当日值(capacity map: accountID → 容量 GB)。
// 返回账号 ID 列表(升序)。
func seedSharedFSCollect(t *testing.T, h *nastest.Harness, provider *nastest.PerAccountProvider, capacities map[int64]float64) []int64 {
	t.Helper()
	ids := make([]int64, 0, len(capacities))
	for accountID, capacity := range capacities {
		h.SeedAccountNamed(accountID, provider.Name)
		h.SeedInstanceNamed(accountID, provider.Name, SharedFS, "共享盘", "cn-hangzhou")
		provider.QuerierFor(accountID).SetMetrics(
			nastest.Metric(SharedFS, "共享盘", nastest.Today(), capacity, capacity*0.3))
		ids = append(ids, accountID)
	}
	// 升序排列(插入序随 map 遍历不稳定)
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
	return ids
}
