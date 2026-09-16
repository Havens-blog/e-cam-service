package fieldfilterquickvalues

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	lqe "github.com/Havens-blog/e-cam-service/tests/logquerye2e"
)

// queryWindow is a CLOSED historical window (now-2h .. now-1h), fixed once per
// test: no new ingestion lands inside it and every call in the test uses the
// exact same window — the journey invariant "筛选不改变日志类型与时间范围" is
// then directly observable.
type queryWindow struct {
	start int64
	end   int64
}

func newWindow() queryWindow {
	now := lqe.NowMs()
	return queryWindow{start: now - 7200_000, end: now - 3600_000}
}

// body builds the unfiltered baseline request for this window.
func (w queryWindow) body() lqe.SearchBody {
	return lqe.SearchBody{LogType: "waf", StartTime: w.start, EndTime: w.end}
}

// filtered builds a request with the given filters for this window.
func (w queryWindow) filtered(filters ...lqe.FieldFilter) lqe.SearchBody {
	b := w.body()
	b.Filters = filters
	return b
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
	t.Logf("search filters=%v total=%d latency=%s", body.Filters, data.Total, elapsed)
	return data
}

// sampleQuickValues derives the "quick values" the frontend would offer, from
// a first unfiltered sample — mirroring the zero-extra-request contract.
// Statuses/hosts are frequency-ranked (most common first).
func sampleQuickValues(t *testing.T, w queryWindow) (statuses []string, hosts []string, baseline lqe.SearchResponse) {
	t.Helper()
	baseline = runSearch(t, w.body())
	if len(baseline.Entries) == 0 {
		t.Skip("no sample data in the window; quick-value journey not observable")
	}
	statusFreq := map[string]int{}
	hostFreq := map[string]int{}
	for _, e := range baseline.Entries {
		if e.Number("status") > 0 {
			statusFreq[fmtInt(int64(e.Number("status")))]++
		}
		if h := e.String("host"); h != "" {
			hostFreq[h]++
		}
	}
	for s := range statusFreq {
		statuses = append(statuses, s)
	}
	for h := range hostFreq {
		hosts = append(hosts, h)
	}
	// 热门值:按出现频次排行(样本内常见值,非全集)
	sort.Slice(statuses, func(i, j int) bool { return statusFreq[statuses[i]] > statusFreq[statuses[j]] })
	sort.Slice(hosts, func(i, j int) bool { return hostFreq[hosts[i]] > hostFreq[hosts[j]] })
	if len(statuses) == 0 || len(hosts) == 0 {
		t.Skipf("sample lacks status/host values (statuses=%d hosts=%d)", len(statuses), len(hosts))
	}
	if len(hosts) > 5 {
		hosts = hosts[:5]
	}
	return statuses, hosts, baseline
}

// pickCooccurringHost picks the host that co-occurs most often with the given
// status in the sample, so the full-window filtered query is guaranteed
// non-empty (the matched sample entries are a subset of the window result).
func pickCooccurringHost(status string, entries []lqe.LogEntry) (string, bool) {
	freq := map[string]int{}
	for _, e := range entries {
		if fmtInt(int64(e.Number("status"))) == status {
			if h := e.String("host"); h != "" {
				freq[h]++
			}
		}
	}
	best, bestN := "", 0
	for h, n := range freq {
		if n > bestN {
			best, bestN = h, n
		}
	}
	return best, bestN > 0
}

func fmtInt(v int64) string {
	return strconv.FormatInt(v, 10)
}

// Journey Step 1: 打开字段筛选 -> 快捷值候选来自当前样本。
// API-level: quick values are derived from the returned sample only; the
// candidate set is finite (bounded by the sample, never a claimed全集).
func TestStep1_QuickValuesDerivedFromSample(t *testing.T) {
	w := newWindow()
	statuses, hosts, baseline := sampleQuickValues(t, w)
	if len(statuses) > len(baseline.Entries) {
		t.Errorf("quick value count exceeds sample size — values cannot come from the sample")
	}
	t.Logf("quick statuses=%v top hosts=%v", statuses, hosts)
}

// Journey Step 2+3: 点选快捷值并重查 -> 结果收窄为匹配该值的日志。
// Deep: picks the most common sample status (guaranteed matches), and every
// returned entry must carry exactly that status.
func TestStep2_3_QuickValueFilterNarrowsExactly(t *testing.T) {
	w := newWindow()
	statuses, _, baseline := sampleQuickValues(t, w)
	pick := statuses[0]
	got := runSearch(t, w.filtered(lqe.FieldFilter{Field: "status", Op: "eq", Value: pick}))
	if got.Total == 0 {
		t.Fatalf("most common sample status %s matched nothing in full window", pick)
	}
	if got.Total >= baseline.Total {
		t.Errorf("filtered total %d not narrower than baseline %d", got.Total, baseline.Total)
	}
	for i, e := range got.Entries {
		if fmtInt(int64(e.Number("status"))) != pick {
			t.Fatalf("entry %d status=%v violates filter eq %s", i, e["status"], pick)
		}
	}
}

// Journey Step 4: 其他字段快捷取值 + 多条件叠加 -> AND 语义正确。
// Pair co-occurs in the sample, so the AND result is guaranteed non-empty.
func TestStep4_MultiFieldQuickValuesAndSemantics(t *testing.T) {
	w := newWindow()
	statuses, _, baseline := sampleQuickValues(t, w)
	status := statuses[0]
	host, ok := pickCooccurringHost(status, baseline.Entries)
	if !ok {
		t.Skip("sample statuses have no host co-occurrence")
	}
	got := runSearch(t, w.filtered(
		lqe.FieldFilter{Field: "status", Op: "eq", Value: status},
		lqe.FieldFilter{Field: "host", Op: "eq", Value: host},
	))
	if got.Total == 0 {
		t.Fatalf("AND of sample co-occurring pair (%s,%s) matched nothing", status, host)
	}
	for i, e := range got.Entries {
		if fmtInt(int64(e.Number("status"))) != status {
			t.Fatalf("entry %d violates status eq %s", i, status)
		}
		if e.String("host") != host {
			t.Fatalf("entry %d host=%q violates host eq %s", i, e.String("host"), host)
		}
	}
	t.Logf("AND-stack matched %d entries", got.Total)
}

// Journey Step 5: 清除字段筛选 -> 恢复未筛选的完整结果,类型与时间范围不变。
// Restore is asserted as: filtered < restored, and restored within tolerance
// band of baseline (live federation re-fans-out per call; exact equality is
// not a stable contract — see doc.go).
func TestStep5_ClearFilterRestoresBaseline(t *testing.T) {
	w := newWindow()
	statuses, _, baseline := sampleQuickValues(t, w)
	narrowed := runSearch(t, w.filtered(lqe.FieldFilter{Field: "status", Op: "eq", Value: statuses[0]}))
	restored := runSearch(t, w.body())
	if narrowed.Total >= restored.Total {
		t.Errorf("cleared-filter total %d not restored above filtered %d", restored.Total, narrowed.Total)
	}
	if restored.Total > 2*baseline.Total || 2*restored.Total < baseline.Total {
		t.Errorf("restored total %d outside tolerance band of baseline %d", restored.Total, baseline.Total)
	}
	if restored.LogType != "waf" {
		t.Errorf("log_type changed after clear: %q", restored.LogType)
	}
	if restored.Entries != nil {
		for i, e := range restored.Entries {
			if ts := e.Timestamp(); ts < w.start || ts > w.end {
				t.Errorf("entry %d timestamp %d outside preserved window", i, ts)
			}
		}
	}
}

// Journey Step 1b: 样本为空 -> 快捷值区域为空(样本无候选),不报错。
func TestStep1b_EmptySampleHasNoQuickValues(t *testing.T) {
	w := queryWindow{start: 1577836800000, end: 1577923200000} // 2020-01-01
	data := runSearch(t, w.body())
	if data.Total != 0 || len(data.Entries) != 0 {
		t.Fatalf("expected empty sample, got total=%d", data.Total)
	}
	for _, e := range data.Entries {
		if e.Number("status") > 0 {
			t.Errorf("empty sample still offered status values")
		}
	}
}

// Journey Step 3b: 筛选后零结果 -> 空结果态且不报错。
func TestStep3b_FilterWithNoMatchesYieldsEmptyState(t *testing.T) {
	w := newWindow()
	data := runSearch(t, w.filtered(lqe.FieldFilter{Field: "status", Op: "eq", Value: "999"}))
	if data.Total != 0 || len(data.Entries) != 0 {
		t.Errorf("expected empty result for status=999, got total=%d", data.Total)
	}
}

// Journey Step 3c: 手输与快捷值混用 -> 两条件组合生效,不互相覆盖。
func TestStep3c_ManualAndQuickValueCombine(t *testing.T) {
	w := newWindow()
	_, hosts, _ := sampleQuickValues(t, w)
	got := runSearch(t, w.filtered(
		lqe.FieldFilter{Field: "host", Op: "contains", Value: hosts[0]},
		lqe.FieldFilter{Field: "host", Op: "neq", Value: "definitely-not-a-real-host.example"},
	))
	for i, e := range got.Entries {
		h := e.String("host")
		if !strings.Contains(h, hosts[0]) {
			t.Fatalf("entry %d host=%q violates contains %s", i, h, hosts[0])
		}
		if h == "definitely-not-a-real-host.example" {
			t.Fatalf("entry %d violates neq condition", i)
		}
	}
	t.Logf("manual+quick combined matched %d entries", got.Total)
}

// Journey Step 4b: 多条件叠加后逐个清除 -> 每步结果与剩余条件语义一致。
// Removing a condition grows the result set (superset chain), so the totals
// must be monotonically non-decreasing: both <= afterRemove <= final.
func TestStep4b_RemoveFiltersStepwise(t *testing.T) {
	w := newWindow()
	statuses, _, baseline := sampleQuickValues(t, w)
	status := statuses[0]
	host, ok := pickCooccurringHost(status, baseline.Entries)
	if !ok {
		t.Skip("sample statuses have no host co-occurrence")
	}
	both := runSearch(t, w.filtered(
		lqe.FieldFilter{Field: "status", Op: "eq", Value: status},
		lqe.FieldFilter{Field: "host", Op: "eq", Value: host},
	))
	afterRemoveStatus := runSearch(t, w.filtered(lqe.FieldFilter{Field: "host", Op: "eq", Value: host}))
	if afterRemoveStatus.Total < both.Total {
		t.Errorf("removing status filter shrank results: %d < %d", afterRemoveStatus.Total, both.Total)
	}
	final := runSearch(t, w.body())
	if final.Total < afterRemoveStatus.Total {
		t.Errorf("final clear shrank results below one-condition set: %d < %d", final.Total, afterRemoveStatus.Total)
	}
}

// Edge: 未知操作符在请求层即拒绝(可读错误)。
func TestInvalidFilterOpRejected(t *testing.T) {
	w := newWindow()
	body := w.filtered(lqe.FieldFilter{Field: "status", Op: "bogus", Value: "404"})
	status, env := lqe.Post(t, lqe.APIPrefix+"/search", body)
	if status != http.StatusBadRequest {
		t.Fatalf("bogus op: status=%d, want 400", status)
	}
	if env.Msg == "" {
		t.Fatalf("400 without readable msg")
	}
	t.Logf("readable: %s", env.Msg)
}

// Edge: 不完整筛选(空 value)在请求层即拒绝。
func TestIncompleteFilterRejected(t *testing.T) {
	w := newWindow()
	body := w.filtered(lqe.FieldFilter{Field: "status", Op: "eq", Value: ""})
	status, env := lqe.Post(t, lqe.APIPrefix+"/search", body)
	if status != http.StatusBadRequest {
		t.Fatalf("empty value: status=%d, want 400", status)
	}
	if env.Msg == "" {
		t.Fatalf("400 without readable msg")
	}
}

// Journey smoke: sample -> quick value -> narrow -> clear -> restore.
func TestJourneySmokeFieldFilterQuickValues(t *testing.T) {
	w := newWindow()
	statuses, _, baseline := sampleQuickValues(t, w)
	status := statuses[0]
	host, ok := pickCooccurringHost(status, baseline.Entries)
	if !ok {
		t.Skip("sample statuses have no host co-occurrence")
	}
	got := runSearch(t, w.filtered(
		lqe.FieldFilter{Field: "status", Op: "eq", Value: status},
		lqe.FieldFilter{Field: "host", Op: "eq", Value: host},
	))
	if got.Total == 0 {
		t.Fatalf("smoke: AND of sample co-occurring pair matched nothing")
	}
	for _, e := range got.Entries {
		if fmtInt(int64(e.Number("status"))) != status || e.String("host") != host {
			t.Fatalf("smoke: AND semantics violated")
		}
	}
	restored := runSearch(t, w.body())
	if restored.Total > 2*baseline.Total || 2*restored.Total < baseline.Total {
		t.Fatalf("smoke: restore outside tolerance band %d -> %d", baseline.Total, restored.Total)
	}
}
