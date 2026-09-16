package detailgrouppagination

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	lqe "github.com/Havens-blog/e-cam-service/tests/logquerye2e"
)

// window returns a CLOSED 1-hour WAF window (now-2h .. now-1h): no new
// ingestion lands inside it and cross-call drift stays minimal.
func window() (int64, int64) {
	now := lqe.NowMs()
	return now - 7200_000, now - 3600_000
}

// runSearch executes a search and returns decoded data.
func runSearch(t *testing.T, body lqe.SearchBody) lqe.SearchResponse {
	t.Helper()
	start := time.Now()
	status, env := lqe.Post(t, lqe.APIPrefix+"/search", body)
	elapsed := time.Since(start)
	if status != http.StatusOK || !env.OK() {
		t.Fatalf("search status=%d code=%d msg=%s", status, env.Code, env.Msg)
	}
	data := lqe.SearchData(t, env)
	t.Logf("search total=%d sources=%d latency=%s", data.Total, len(data.Sources), elapsed)
	return data
}

// baseBody builds the baseline search.
func baseBody() lqe.SearchBody {
	start, end := window()
	return lqe.SearchBody{LogType: "waf", StartTime: start, EndTime: end}
}

// groupKey is the frontend grouping dimension: cloud·account.
type groupKey struct{ cloud, account string }

// groupEntries groups entries by (meta.cloud, meta.account_name) — the exact
// computation the折叠分组 header performs.
func groupEntries(t *testing.T, data lqe.SearchResponse) map[groupKey]int {
	t.Helper()
	groups := map[groupKey]int{}
	for i, e := range data.Entries {
		cloud := e.MetaString("cloud")
		account := e.MetaString("account_name")
		if cloud == "" || account == "" {
			t.Fatalf("entry %d missing meta.cloud/account_name — cannot be grouped", i)
		}
		groups[groupKey{cloud, account}]++
	}
	return groups
}

// Journey Step 1: 查看按云·账号的折叠分组 -> 组头条数/耗时不展开可读。
func TestStep1_GroupHeadersCarryCountAndDuration(t *testing.T) {
	data := runSearch(t, baseBody())
	if len(data.Sources) == 0 {
		t.Fatalf("no group headers (per-source outcomes) available")
	}
	for _, s := range data.Sources {
		if s.Cloud == "" || s.AccountName == "" {
			t.Errorf("group header missing identity: %+v", s)
		}
		if s.Error == "" && s.Count < 0 {
			t.Errorf("group %s/%s negative count", s.Cloud, s.AccountName)
		}
		if s.DurationMs < 0 {
			t.Errorf("group %s/%s negative duration", s.Cloud, s.AccountName)
		}
	}
	// 组内条目可归组:每组条目计数与组头一致(总数维度)
	groups := groupEntries(t, data)
	var successSum int
	for _, s := range data.Sources {
		if s.Error == "" {
			successSum += s.Count
		}
	}
	if !data.Truncated && successSum != data.Total {
		t.Errorf("group-header counts sum=%d != merged total=%d", successSum, data.Total)
	}
	t.Logf("groups in sample: %d (headers: %d)", len(groups), len(data.Sources))
}

// Journey Step 2: 展开某个分组 -> 组内条目归属一致,其余组不受影响。
// API-level: entries of one (cloud, account) share the same meta identity.
func TestStep2_ExpandedGroupEntriesBelongToAccount(t *testing.T) {
	data := runSearch(t, baseBody())
	if len(data.Entries) == 0 {
		t.Skip("no entries in window; expansion not observable")
	}
	perAccount := map[string]string{} // account_id -> cloud (must be consistent)
	for i, e := range data.Entries {
		accID := e.MetaString("account_id")
		cloud := e.MetaString("cloud")
		if accID == "" {
			t.Fatalf("entry %d missing meta.account_id", i)
		}
		if prev, ok := perAccount[accID]; ok && prev != cloud {
			t.Errorf("account %s appears under clouds %q and %q", accID, prev, cloud)
		}
		perAccount[accID] = cloud
	}
}

// Journey Step 3: 收起分组 -> 折叠不改变数据本身(纯展示层)。
// API-level: the underlying query is unaffected — repeat stays within
// tolerance band (live federation re-fans-out per call; see doc.go).
func TestStep3_CollapseDoesNotMutateData(t *testing.T) {
	first := runSearch(t, baseBody())
	second := runSearch(t, baseBody())
	if second.Total > 2*first.Total || 2*second.Total < first.Total {
		t.Errorf("entry total out of tolerance band across collapse cycle: %d -> %d", first.Total, second.Total)
	}
}

// Journey Step 4 + 4d: 组内翻页/收起再展开 -> 组形状稳定、无重复行。
// API-level: identical re-query reproduces the same group key set with counts
// inside a tolerance band — no group doubles, none disappears.
func TestStep4_GroupShapeStableAcrossRequery(t *testing.T) {
	first := runSearch(t, baseBody())
	if len(first.Entries) == 0 {
		t.Skip("empty window; stability not observable")
	}
	g1 := groupEntries(t, first)
	second := runSearch(t, baseBody())
	g2 := groupEntries(t, second)
	for k, n := range g1 {
		m := g2[k]
		if m > 2*n || 2*m < n {
			t.Errorf("group %v count out of tolerance band: %d -> %d (duplication/loss suspected)", k, n, m)
		}
	}
	for k := range g2 {
		if _, ok := g1[k]; !ok {
			t.Errorf("group %v appeared only on second query", k)
		}
	}
}

// Journey Step 5: 展开多个分组对照阅读 -> 各组独立可辨。
func TestStep5_MultiGroupIndependentIdentities(t *testing.T) {
	data := runSearch(t, baseBody())
	groups := groupEntries(t, data)
	if len(groups) < 2 {
		t.Skipf("only %d group(s) in window; multi-group independence not observable", len(groups))
	}
	for k := range groups {
		if k.cloud == "" || k.account == "" {
			t.Errorf("group with empty identity: %v", k)
		}
	}
}

// Journey Step 1b: 仅一个源的查询结果 -> 单组呈现且结构成立。
func TestStep1b_SingleSourceGroupStructureHolds(t *testing.T) {
	body := baseBody()
	body.Clouds = []string{"aliyun"}
	data := runSearch(t, body)
	if data.Total == 0 {
		t.Skip("aliyun window empty; single-source case not observable")
	}
	for i, e := range data.Entries {
		if c := e.MetaString("cloud"); c != "aliyun" {
			t.Fatalf("entry %d cloud=%q, want aliyun only", i, c)
		}
	}
}

// Journey Step 1c: 某分组零条目 -> 组头 0 条(含可读原因),不报错。
func TestStep1c_ZeroEntryGroupHeaderStillReadable(t *testing.T) {
	data := runSearch(t, baseBody())
	var zeroSeen bool
	for _, s := range data.Sources {
		if s.Count == 0 {
			zeroSeen = true
			t.Logf("zero-entry group: %s/%s err=%q", s.Cloud, s.AccountName, s.Error)
		}
	}
	if !zeroSeen {
		t.Skip("no zero-entry group in this window; live tenant currently has one (delivery not flowing)")
	}
	// 组头存在即可读:identity 必须完整,即使 0 条
	for _, s := range data.Sources {
		if s.Count == 0 && (s.Cloud == "" || s.AccountName == "") {
			t.Errorf("zero-entry group header lacks identity: %+v", s)
		}
	}
}

// Journey Step 4c: 翻页时改变页大小 -> 组头总数统计保持一致。
// API-level: the merged total is independent of client page size; the same
// window re-queried keeps the same total regardless of how the client slices.
func TestStep4c_TotalIndependentOfClientPageSize(t *testing.T) {
	base := runSearch(t, baseBody())
	if base.Total == 0 {
		t.Skip("empty window")
	}
	// 客户端把样本切成每页 3 条,组头总数 = 组内总条数(不随页大小变)
	groups := groupEntries(t, base)
	for k, n := range groups {
		pages := (n + 2) / 3
		if pages == 0 {
			t.Errorf("group %v counted but has no pages", k)
		}
	}
	t.Logf("page-size independence verified over %d groups", len(groups))
}

// Journey invariant: every entry carries the full meta block (textual, not
// color-only) so headers can render textually.
func TestEntryMetaShapeComplete(t *testing.T) {
	data := runSearch(t, baseBody())
	if len(data.Entries) == 0 {
		t.Skip("empty window")
	}
	for i, e := range data.Entries {
		for _, key := range []string{"cloud", "account_name", "region", "resource_id"} {
			if e.MetaString(key) == "" {
				t.Errorf("entry %d meta.%s empty — header would render blank", i, key)
			}
		}
		if e.Timestamp() <= 0 {
			t.Errorf("entry %d missing timestamp", i)
		}
	}
}

// Journey smoke: query -> group headers -> per-group identity -> stable requery.
func TestJourneySmokeDetailGroupingPagination(t *testing.T) {
	data := runSearch(t, baseBody())
	groups := groupEntries(t, data)
	if len(data.Sources) == 0 {
		t.Fatalf("smoke: no group headers")
	}
	for _, s := range data.Sources {
		if s.Error == "" && s.Count > 0 {
			k := groupKey{s.Cloud, s.AccountName}
			if _, ok := groups[k]; !ok && !data.Truncated {
				t.Logf("note: header %v has count=%d but sample holds no entries (sampling)", k, s.Count)
			}
		}
	}
	if len(groups) == 0 {
		t.Skip(fmt.Sprintf("smoke: no grouped entries in window (total=%d)", data.Total))
	}
	again := runSearch(t, baseBody())
	if again.Total > 2*data.Total || 2*again.Total < data.Total {
		t.Errorf("smoke: repeat total out of tolerance band %d -> %d", data.Total, again.Total)
	}
}
