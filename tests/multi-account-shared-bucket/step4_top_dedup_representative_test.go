// @feature oss-ops-insight @api-functional
//
// Contract step-4-top-dedup-representative: Top 按 bucket_name 去重取代表行 —
// one entry per bucket name chosen by "date desc, then capacity desc",
// deterministic across repeated requests, kept alive when only one of several
// accounts has data, and never summed across accounts.
// unauthorized-401 is exempt ( see doc.go ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multi_account_shared_bucket

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// seedCrossDateWorld 构建跨日期代表行世界:三账号各有不同日期/容量的行。
func seedCrossDateWorld(t *testing.T) (*osstest.Harness, *osstest.Provider) {
	t.Helper()
	h, provider := newJourneyHarness()
	for _, id := range []int64{1, 2, 3} {
		seedSharedBucketAccount(t, h, provider, id)
	}
	seedSharedMetricT(t, h, 1, provider.Name, osstest.DayOffset(-2), 500, 50) // 最旧
	seedSharedMetricT(t, h, 2, provider.Name, osstest.DayOffset(-1), 400, 40)
	seedSharedMetricT(t, h, 3, provider.Name, osstest.DayOffset(-1), 450, 45) // 昨日容量最大
	seedSharedMetricT(t, h, 2, provider.Name, osstest.Today(), 200, 20)       // 最新但容量小
	return h, provider
}

// Outcome dedup-single-representative-row: shared-assets 在 Top 中只出现一次;
// 代表行按「日期 desc 再容量 desc」选取(今日行存在取今日,容量不跨账号求和)。
func TestStep4_DedupSingleRepresentativeRow(t *testing.T) {
	h, _ := seedCrossDateWorld(t)
	router := h.NewOSSRouter(1)

	status, env := osstest.GetJSON(t, router, "/assets/oss/top?days=7")
	require.Equal(t, 200, status)

	var resp service.OSSTopResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.Equal(t, 1, resp.Total, "shared-assets 去重后 total=1")
	require.Len(t, resp.Items, 1)

	item := resp.Items[0]
	require.Equal(t, sharedBucketName, item.BucketName)
	require.NotNil(t, item.Latest.StorageSize)
	// 最新日期(今日)只有账号 2 一行 → 代表行 200;不是跨账号最大 500,更不是求和
	require.Equal(t, float64(200), *item.Latest.StorageSize, "代表行取最新日期行(日期 desc 优先)")
	require.Equal(t, osstest.Today(), item.Latest.Date)
	require.Equal(t, []int64{1, 2, 3}, item.AccountIDs,
		"AccountIDs 为该 bucket 全部有行账号的去重升序列表(跨账号并存)")
}

// Outcome rep-row-order-stable: 同一 bucket_name 跨账号多行时,代表行选取
// 「日期 desc 再容量 desc」确定性排序,多次请求结果稳定一致。
func TestStep4_RepRowOrderStable(t *testing.T) {
	h, _ := seedCrossDateWorld(t)
	router := h.NewOSSRouter(1)

	var first string
	for i := 0; i < 5; i++ {
		status, env := osstest.GetJSON(t, router, "/assets/oss/top?days=7")
		require.Equal(t, 200, status)
		var resp service.OSSTopResp
		require.NoError(t, json.Unmarshal(env.Data, &resp))
		require.Len(t, resp.Items, 1)
		raw, err := json.Marshal(resp.Items[0])
		require.NoError(t, err)
		if i == 0 {
			first = string(raw)
			continue
		}
		require.Equal(t, first, string(raw), "第 %d 次请求代表行应与前次完全一致", i+1)
	}

	// 语义锚:无今日行时代表行取昨日同日容量最大行(450 > 400)
	h2, provider := newJourneyHarness()
	for _, id := range []int64{1, 2} {
		seedSharedBucketAccount(t, h2, provider, id)
	}
	seedSharedMetricT(t, h2, 1, provider.Name, osstest.DayOffset(-1), 400, 40)
	seedSharedMetricT(t, h2, 2, provider.Name, osstest.DayOffset(-1), 450, 45)
	top, err := h2.NewQueryService().GetTop(context.Background(), 1, 0, 7, service.OSSSortStorageSize, 10, 1, 10)
	require.NoError(t, err)
	require.Len(t, top.Items, 1)
	require.InDelta(t, 450, *top.Items[0].Latest.StorageSize, 1e-9, "同日内取容量最大行为代表行")
}

// Outcome one-account-empty-kept-in-top: 仅账号 A 有数据(account B 采集为零)
// — 代表行取自有数据的账号行,B 的缺失不把 bucket 从 Top 中抹除,也不把 B 的
// 空值计入。
func TestStep4_OneAccountEmptyKeptInTop(t *testing.T) {
	h, provider := newJourneyHarness()
	accA := h.SeedAccount(1, provider)
	accB := h.SeedAccount(2, provider)
	h.SeedBucket(1, provider.Name, sharedBucketName)
	h.SeedBucket(2, provider.Name, sharedBucketName) // B 有资产但无任何指标行
	seedSharedMetricT(t, h, accA.ID, provider.Name, osstest.Today(), 150, 15)

	top, err := h.NewQueryService().GetTop(context.Background(), 1, 0, 7, service.OSSSortStorageSize, 10, 1, 10)
	require.NoError(t, err)

	require.Len(t, top.Items, 1, "B 无数据不把 bucket 从 Top 抹除")
	item := top.Items[0]
	require.InDelta(t, 150, *item.Latest.StorageSize, 1e-9, "代表行取自有数据的账号 A 行")
	require.Equal(t, []int64{accA.ID}, item.AccountIDs, "空值账号不计入 account_id 列表")
	require.NotContains(t, item.AccountIDs, accB.ID)
}

// Outcome unauthorized: 豁免(见 doc.go)。
func TestStep4_TopDedup_UnauthorizedExempt(t *testing.T) {
	t.Log("exempt: 401 enforced by global auth middleware outside the OSS surface")
}
