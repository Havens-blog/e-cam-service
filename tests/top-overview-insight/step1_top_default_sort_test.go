// @feature oss-ops-insight @api-functional
//
// Contract step-1-top-default-sort: 查看账号视角 Top 排名 — the default Top
// list sorts by near-N-day average storage, dedups cross-account same-name
// buckets, carries total/page/page_size meta, answers 404 for cross-tenant
// account ids, and unauthorized-401 is exempt ( see doc.go ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package top_overview_insight

import (
	"encoding/json"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/tests/osstest"
	"github.com/stretchr/testify/require"
)

// seedDefaultSortWorld 两账号合计 8 个含数据 bucket(含跨账号同名 bucket),
// 多日连续落库(fixture_spec: CloudAccount>=2, OSSMetricRow>=15)。
func seedDefaultSortWorld(t *testing.T) (*osstest.Harness, *osstest.Provider) {
	t.Helper()
	h, provider := newJourneyHarness()
	h.SeedAccount(1, provider)
	h.SeedAccount(2, provider)
	// 账号 1:big-01(容量最大)、small-02;账号 2:mid-03、以及跨账号同名 big-01
	world := map[int64][]struct {
		name    string
		storage float64
		objects int64
	}{
		1: {{name: "big-01", storage: 900, objects: 90}, {name: "small-02", storage: 100, objects: 10}},
		2: {{name: "mid-03", storage: 500, objects: 50}, {name: "big-01", storage: 300, objects: 30}},
	}
	for accountID, buckets := range world {
		for _, b := range buckets {
			h.SeedBucket(accountID, provider.Name, b.name)
			// 7 天连续行:容量逐日 +1 形成稳定均值
			for d := 1; d <= 7; d++ {
				h.SeedMetric(t, accountID, provider.Name,
					osstest.Metric(b.name, osstest.DayOffset(-d+1), b.storage+float64(d), b.objects))
			}
		}
	}
	return h, provider
}

// Outcome top-sorted-by-avg-storage: 默认 sort=storage_size 返回近 N 天均值
// 口径降序 Top;跨账号同名 bucket 去重只出现一次(代表行不跨账号求和);
// 响应含 total/page/page_size 元信息。
func TestStep1_TopSortedByAvgStorage(t *testing.T) {
	h, _ := seedDefaultSortWorld(t)
	router := h.NewOSSRouter(1)

	status, env := osstest.GetJSON(t, router, "/assets/oss/top?days=7")
	require.Equal(t, 200, status)

	var resp topResp
	require.NoError(t, json.Unmarshal(env.Data, &resp))
	require.Equal(t, 3, resp.Total, "跨账号同名 big-01 去重后 total=3")
	require.Equal(t, 1, resp.Page)
	require.Equal(t, 10, resp.PageSize)
	require.Len(t, resp.Items, 3)

	// 均值口径降序:big-01(每日代表行=同日容量最大 900+d) > mid-03 > small-02
	require.Equal(t, "big-01", resp.Items[0].BucketName)
	require.Equal(t, "mid-03", resp.Items[1].BucketName)
	require.Equal(t, "small-02", resp.Items[2].BucketName)

	big := resp.Items[0]
	require.NotNil(t, big.Average.StorageSize)
	require.InDelta(t, 904, *big.Average.StorageSize, 1e-9,
		"排序值与展示值统一为近 N 天均值口径(每日代表行 901~907 的均值)")
	require.InDelta(t, 901, *big.Latest.StorageSize, 1e-9,
		"代表行取最新一天的同日容量最大行(901 > 301,不跨账号求和)")
	require.Equal(t, []int64{1, 2}, big.AccountIDs, "跨账号同名 bucket 的账号去重列表")
}

// Outcome cross-tenant-account-404: 请求携带不属于本租户的 account_id —
// 返回 404(不泄露账号存在性),不返回该账号任何指标数据。
func TestStep1_CrossTenantAccount404(t *testing.T) {
	h, provider := seedDefaultSortWorld(t)
	h.SeedAccountTenant(9, provider.Name, 2)
	h.SeedMetric(t, 9, provider.Name, osstest.Metric("tenant2-bucket", osstest.Today(), 5000, 500))

	router := h.NewOSSRouter(1)
	status, env := osstest.GetJSON(t, router, "/assets/oss/top?account_id=9&days=7")
	require.Equal(t, 404, status, "越权 account_id 映射 404 而非 403")
	require.Equal(t, errs.AccountNotFound.Msg, env.Msg, "与账号不存在同文案,不泄露存在性")
	require.True(t, len(env.Data) == 0 || string(env.Data) == "null", "越权响应不得携带指标数据")
}

// Outcome unauthorized: 豁免(见 doc.go)。
func TestStep1_TopDefaultSort_UnauthorizedExempt(t *testing.T) {
	t.Log("exempt: 401 enforced by global auth middleware outside the OSS surface")
}

// topResp Top 响应形状镜像(避免直接依赖 service 导出类型的字段名漂移)。
type topResp struct {
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	Items    []struct {
		BucketName string  `json:"bucket_name"`
		AccountIDs []int64 `json:"account_id"`
		Latest     struct {
			Date        string   `json:"date"`
			StorageSize *float64 `json:"storage_size"`
		} `json:"latest"`
		Average struct {
			StorageSize *float64 `json:"storage_size"`
		} `json:"average"`
	} `json:"items"`
}
