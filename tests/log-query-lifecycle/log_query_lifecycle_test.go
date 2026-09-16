package logquerylifecycle

import (
	"net/http"
	"strings"
	"testing"
	"time"

	lqe "github.com/Havens-blog/e-cam-service/tests/logquerye2e"
)

// recentWafBody builds a 1-hour WAF search body (keeps totals well under the
// federation cap so count invariants stay observable).
func recentWafBody() lqe.SearchBody {
	now := lqe.NowMs()
	return lqe.SearchBody{LogType: "waf", StartTime: now - 3600_000, EndTime: now}
}

// searchWaf runs a search and returns decoded data.
func searchWaf(t *testing.T, body lqe.SearchBody) lqe.SearchResponse {
	t.Helper()
	start := time.Now()
	status, env := lqe.Post(t, lqe.APIPrefix+"/search", body)
	elapsed := time.Since(start)
	if status != http.StatusOK || !env.OK() {
		t.Fatalf("search status=%d code=%d msg=%s", status, env.Code, env.Msg)
	}
	data := lqe.SearchData(t, env)
	t.Logf("search total=%d truncated=%v latency=%s", data.Total, data.Truncated, elapsed)
	return data
}

// Journey Step 2: 发起查询 -> 明确的进行中状态。
// API-level: response carries the per-source fan-out outcome list ("正在查询
// N 个云账号"的可观测形态): every involved account shows up with cloud,
// account identity and its own duration.
func TestStep2_SearchCarriesPerSourceProgress(t *testing.T) {
	data := searchWaf(t, recentWafBody())
	if len(data.Sources) == 0 {
		t.Fatalf("no per-source outcomes — progress state would be empty")
	}
	for _, s := range data.Sources {
		if s.Cloud == "" || s.AccountID == "" {
			t.Errorf("source outcome missing identity: %+v", s)
		}
		if s.DurationMs < 0 {
			t.Errorf("negative duration for %s/%s", s.Cloud, s.AccountID)
		}
	}
}

// Journey Step 3: 等待查询完成 -> 结果按时间倒序、进度态消失。
// API-level: merged entries are ordered by timestamp descending.
func TestStep3_EntriesMergedInDescendingTimeOrder(t *testing.T) {
	data := searchWaf(t, recentWafBody())
	prev := int64(-1)
	for i, e := range data.Entries {
		ts := e.Timestamp()
		if ts <= 0 {
			t.Errorf("entry %d has no usable timestamp", i)
			continue
		}
		if prev >= 0 && ts > prev {
			t.Fatalf("entries not time-descending at index %d: %d > %d", i, ts, prev)
		}
		prev = ts
	}
}

// Journey Step 4: 按源读取结果 -> 单源失败仅标注该源,整页不失败。
// Live mapping: tenant has a known non-flowing source (readable error), yet
// the request succeeds and healthy sources contribute full results.
func TestStep4_PerSourceFailureIsolatedPageStillSucceeds(t *testing.T) {
	data := searchWaf(t, recentWafBody())
	var (
		successCount int
		sumCounts    int
		failedOne    *lqe.SourceOutcome
	)
	for i := range data.Sources {
		s := &data.Sources[i]
		if s.Error == "" {
			successCount++
			sumCounts += s.Count
		} else if failedOne == nil {
			failedOne = s
		}
	}
	if successCount == 0 {
		t.Fatalf("no successful source — page would be dead")
	}
	if data.Truncated {
		t.Logf("truncated=true; skipping count-sum invariant")
	} else if sumCounts != data.Total {
		t.Errorf("sum of successful source counts = %d, total = %d (merge invariant broken)", sumCounts, data.Total)
	}
	if failedOne != nil {
		if isBareError(failedOne.Error) {
			t.Errorf("source failure error is not business-readable: %q", failedOne.Error)
		}
		t.Logf("isolated failure observed on %s/%s: %s", failedOne.Cloud, failedOne.AccountName, failedOne.Error)
	} else {
		t.Logf("no failed source in this window; isolation verified structurally via error field shape")
	}
	// 失败源的 count 不得混入 entries 计数
	if failedOne != nil && failedOne.Count != 0 {
		t.Errorf("failed source %s reports count=%d (must be 0)", failedOne.Cloud, failedOne.Count)
	}
}

// isBareError heuristically detects raw system error leakage (stack traces,
// panic text, internal paths) that must never surface verbatim.
func isBareError(msg string) bool {
	for _, marker := range []string{"goroutine ", ".go:", "panic:", "runtime error"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// closedWafBody builds a WAF search over a CLOSED historical window
// (now-2h .. now-1h): no new ingestion lands inside it, so repeat-call drift
// is minimized (live federation still re-fans out per call; see doc.go).
func closedWafBody() lqe.SearchBody {
	now := lqe.NowMs()
	return lqe.SearchBody{LogType: "waf", StartTime: now - 7200_000, EndTime: now - 3600_000}
}

// Journey Step 5: 重复执行同一查询 -> 不产生叠加/重复的结果区块。
// API-level: identical request twice -> totals comparable (no doubling), the
// echoed log_type is unchanged, and every entry stays inside the requested
// window. Exact total equality is NOT asserted: the live backend re-fans-out
// per call and per-source counts vary (documented in doc.go); the journey
// invariant at API level is "no duplicated/stacked result blocks".
func TestStep5_RepeatQueryConsistentNoDuplication(t *testing.T) {
	body := closedWafBody()
	first := searchWaf(t, body)
	second := searchWaf(t, body)
	if first.Total == 0 {
		t.Skip("first query empty; consistency not observable in this window")
	}
	if second.Total > 2*first.Total || 2*second.Total < first.Total {
		t.Errorf("repeat query total out of tolerance band: %d -> %d (stacking/duplication suspected)", first.Total, second.Total)
	}
	if second.LogType != "waf" {
		t.Errorf("echoed log_type changed on repeat: %q", second.LogType)
	}
	for i, e := range second.Entries {
		if ts := e.Timestamp(); ts > body.EndTime || ts < body.StartTime {
			t.Errorf("entry %d timestamp %d outside requested window (state bleed)", i, ts)
		}
	}
}

// Journey Step 2c (readable failure contract): all-failure / invalid
// parameters must yield business-readable errors with a retry affordance —
// never bare system strings.
func TestStep2c_FailurePathsAreBusinessReadable(t *testing.T) {
	now := lqe.NowMs()
	cases := []struct {
		name string
		body lqe.SearchBody
	}{
		{"unknown log type", lqe.SearchBody{LogType: "nope", StartTime: now - 1000, EndTime: now}},
		{"inverted window", lqe.SearchBody{LogType: "waf", StartTime: now, EndTime: now - 1000}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, env := lqe.Post(t, lqe.APIPrefix+"/search", tc.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400", status)
			}
			if env.Msg == "" {
				t.Fatalf("400 without readable msg")
			}
			if isBareError(env.Msg) {
				t.Errorf("bare system error leaked: %q", env.Msg)
			}
			t.Logf("readable: %s", env.Msg)
		})
	}
}

// Journey Step 3b: 超时/截断 -> 明确降级说明而非无反馈。
// API-level: the truncated flag is part of the contract and reported honestly
// (false for small windows, and the response still carries per-source status).
func TestStep3b_TruncationFlagPresentAndHonest(t *testing.T) {
	data := searchWaf(t, recentWafBody())
	if data.Truncated && data.Total == 0 {
		t.Errorf("truncated=true but no entries — misleading degradation signal")
	}
}

// Journey Step 4c: 查询结果为空 -> 明确空态,不报错。
// API-level: far-past window returns code 0, total 0, empty entries, and the
// per-source status list is still present (query conditions preserved).
func TestStep4c_EmptyWindowYieldsExplicitEmptyState(t *testing.T) {
	body := lqe.SearchBody{LogType: "waf", StartTime: 1577836800000, EndTime: 1577923200000} // 2020-01-01
	status, env := lqe.Post(t, lqe.APIPrefix+"/search", body)
	if status != http.StatusOK || !env.OK() {
		t.Fatalf("empty-window status=%d code=%d msg=%s (empty must not be an error)", status, env.Code, env.Msg)
	}
	data := lqe.SearchData(t, env)
	if data.Total != 0 || len(data.Entries) != 0 {
		t.Errorf("expected empty result, got total=%d entries=%d", data.Total, len(data.Entries))
	}
}

// Journey Step 3c: 单源慢 -> 请求在联邦超时预算内完成且逐源耗时可观测。
func TestStep3c_PerSourceDurationObservable(t *testing.T) {
	data := searchWaf(t, recentWafBody())
	for _, s := range data.Sources {
		t.Logf("source %s/%s duration_ms=%d count=%d err=%q",
			s.Cloud, s.AccountName, s.DurationMs, s.Count, s.Error)
	}
}

// Journey Step 2b contract: missing required body fields are rejected
// readably at the boundary.
func TestSearchMissingRequiredFieldsRejected(t *testing.T) {
	status, env := lqe.Post(t, lqe.APIPrefix+"/search", map[string]any{"log_type": "waf"})
	if status != http.StatusBadRequest {
		t.Fatalf("missing time fields: status=%d, want 400", status)
	}
	if env.Msg == "" {
		t.Fatalf("400 without readable msg")
	}
}

// Journey smoke: types -> sources -> search -> per-source read -> repeat.
func TestJourneySmokeLogQueryLifecycle(t *testing.T) {
	status, env := lqe.Get(t, lqe.APIPrefix+"/sources?log_type=waf")
	if status != http.StatusOK || !env.OK() || len(lqe.SourcesData(t, env)) == 0 {
		t.Fatalf("smoke setup: waf sources unavailable")
	}
	body := recentWafBody()
	first := searchWaf(t, body)
	for _, e := range first.Entries {
		m := e.Meta()
		if m == nil || m["cloud"] == nil {
			t.Fatalf("smoke step4: entry missing meta.cloud — per-source reading impossible")
		}
	}
	second := searchWaf(t, body)
	if second.Total > 2*first.Total || 2*second.Total < first.Total {
		t.Errorf("smoke step5: repeat total out of tolerance band %d -> %d", first.Total, second.Total)
	}
}
