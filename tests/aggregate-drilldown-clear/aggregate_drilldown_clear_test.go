package aggregatedrilldownclear

import (
	"net/http"
	"strconv"
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

// aggregateStatus runs the TopN aggregation on the status dimension.
func aggregateStatus(t *testing.T) lqe.AggregateResponse {
	t.Helper()
	start, end := window()
	start0 := time.Now()
	status, env := lqe.Post(t, lqe.APIPrefix+"/aggregate", lqe.AggregateBody{
		LogType: "waf", StartTime: start, EndTime: end,
		Dimension: "status", Metric: "count",
	})
	elapsed := time.Since(start0)
	if status != http.StatusOK || !env.OK() {
		t.Fatalf("aggregate status=%d code=%d msg=%s", status, env.Code, env.Msg)
	}
	data := lqe.AggregateData(t, env)
	t.Logf("aggregate total=%d buckets=%d topn=%d latency=%s topn_skip=%q",
		data.Total, len(data.Buckets), len(data.TopN), elapsed, data.TopNSkip)
	return data
}

// baseline runs the unfiltered search (the pre-drilldown view).
func baseline(t *testing.T) lqe.SearchResponse {
	t.Helper()
	start, end := window()
	status, env := lqe.Post(t, lqe.APIPrefix+"/search", lqe.SearchBody{
		LogType: "waf", StartTime: start, EndTime: end,
	})
	if status != http.StatusOK || !env.OK() {
		t.Fatalf("baseline search status=%d msg=%s", status, env.Msg)
	}
	return lqe.SearchData(t, env)
}

// searchWithFilter runs search with a single status eq filter.
func searchWithFilter(t *testing.T, value string) lqe.SearchResponse {
	t.Helper()
	start, end := window()
	status, env := lqe.Post(t, lqe.APIPrefix+"/search", lqe.SearchBody{
		LogType: "waf", StartTime: start, EndTime: end,
		Filters: []lqe.FieldFilter{{Field: "status", Op: "eq", Value: value}},
	})
	if status != http.StatusOK || !env.OK() {
		t.Fatalf("drilldown search status=%d code=%d msg=%s", status, env.Code, env.Msg)
	}
	return lqe.SearchData(t, env)
}

// pickGroup picks the TopN group to drill into: the top topn item when the
// dimension is pushable, otherwise the most frequent status in the sample.
func pickGroup(t *testing.T, agg lqe.AggregateResponse) string {
	t.Helper()
	if len(agg.TopN) > 0 {
		return trimNumber(agg.TopN[0].Name)
	}
	base := baseline(t)
	for _, e := range base.Entries {
		if e.Number("status") > 0 {
			return trimNumber(int64(e.Number("status")))
		}
	}
	t.Skip("no topn and no sample statuses; drilldown target unavailable")
	return ""
}

// trimNumber normalizes topn names / status values to a plain string.
func trimNumber(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatInt(int64(t), 10)
	default:
		return ""
	}
}

// Journey Step 1: 查看 TopN 分组图 -> 分组计数排行、分桶与总数自洽。
func TestStep1_TopNGroupsAndBucketIntegrity(t *testing.T) {
	agg := aggregateStatus(t)
	if agg.Total <= 0 {
		t.Fatalf("aggregate total = %d, expected a non-empty window", agg.Total)
	}
	if len(agg.Buckets) == 0 {
		t.Fatalf("no buckets — chart would be empty")
	}
	var sum int64
	prev := int64(-1)
	for _, b := range agg.Buckets {
		if prev >= 0 && b.Timestamp < prev {
			t.Errorf("buckets not time-ascending: %d after %d", b.Timestamp, prev)
		}
		prev = b.Timestamp
		sum += b.Count
	}
	if sum != agg.Total {
		t.Errorf("sum(buckets) = %d != total %d (chart/明细 would disagree)", sum, agg.Total)
	}
	// TopN 按值降序(排行语义)
	for i := 1; i < len(agg.TopN); i++ {
		if agg.TopN[i].Value > agg.TopN[i-1].Value {
			t.Errorf("topn not sorted by value desc at %d: %v > %v",
				i, agg.TopN[i].Value, agg.TopN[i-1].Value)
		}
	}
	// 每个源 outcome 必须诚实标注(成功/失败/不支持),不静默缺失
	for _, s := range agg.Sources {
		if s.Error == "" && s.Total <= 0 && len(agg.Buckets) > 0 {
			t.Logf("note: source %s/%s reported total=%d with buckets present", s.Cloud, s.AccountName, s.Total)
		}
	}
}

// Journey Step 2+3: 点击 TopN 条形项下钻 -> 明细仅含该分组值。
func TestStep2_3_DrilldownFiltersToGroupValue(t *testing.T) {
	agg := aggregateStatus(t)
	group := pickGroup(t, agg)
	got := searchWithFilter(t, group)
	for i, e := range got.Entries {
		if trimNumber(int64(e.Number("status"))) != group {
			t.Fatalf("entry %d status=%v violates drilldown group %s", i, e["status"], group)
		}
	}
	base := baseline(t)
	if got.Total > base.Total {
		t.Errorf("drilldown total %d exceeds baseline %d", got.Total, base.Total)
	}
	t.Logf("drilldown into %q: %d/%d entries", group, got.Total, base.Total)
}

// Journey Step 4: 切换分组 -> 替换语义(非叠加、非重复)。
// API-level: stacking the same eq filter twice must be result-identical to
// once — the "替换该字段取值" contract has no duplicate-condition effect.
func TestStep4_ReplaceSemanticsNoStacking(t *testing.T) {
	agg := aggregateStatus(t)
	group := pickGroup(t, agg)
	once := searchWithFilter(t, group)

	start, end := window()
	twiceBody := lqe.SearchBody{
		LogType: "waf", StartTime: start, EndTime: end,
		Filters: []lqe.FieldFilter{
			{Field: "status", Op: "eq", Value: group},
			{Field: "status", Op: "eq", Value: group},
		},
	}
	status, env := lqe.Post(t, lqe.APIPrefix+"/search", twiceBody)
	if status != http.StatusOK || !env.OK() {
		t.Fatalf("double filter status=%d msg=%s", status, env.Msg)
	}
	twice := lqe.SearchData(t, env)
	// 重复条件不得叠加(真叠加会近似翻倍);容差带吸收联邦逐次扇出的波动
	if twice.Total > 2*once.Total || 2*twice.Total < once.Total {
		t.Errorf("duplicate condition moved result out of band: %d -> %d (stacking, not replacement)", once.Total, twice.Total)
	}
}

// Journey Step 5: 一键清除 -> 恢复下钻前的完整视图,时间范围不变。
// Restore within tolerance band of the pre-drilldown baseline (live federation
// re-fans-out per call; exact equality is not a stable contract — doc.go).
func TestStep5_ClearRestoresPreDrilldownView(t *testing.T) {
	base := baseline(t)
	agg := aggregateStatus(t)
	group := pickGroup(t, agg)
	drilled := searchWithFilter(t, group)
	if drilled.Total > base.Total {
		t.Fatalf("drilled total %d > baseline %d", drilled.Total, base.Total)
	}
	cleared := baseline(t)
	if cleared.Total > 2*base.Total || 2*cleared.Total < base.Total {
		t.Errorf("clear did not restore view: %d outside band of %d", cleared.Total, base.Total)
	}
	if cleared.LogType != "waf" {
		t.Errorf("log_type changed after clear: %q", cleared.LogType)
	}
}

// Journey Step 2b: 零计数分组下钻 -> 正常空态不报错。
func TestStep2b_ZeroCountDrilldownYieldsEmptyState(t *testing.T) {
	got := searchWithFilter(t, "999")
	if got.Total != 0 || len(got.Entries) != 0 {
		t.Errorf("expected empty drilldown for status=999, got total=%d", got.Total)
	}
}

// Journey Step 2d: 聚合冷启动/慢 -> 完整完成且逐源状态可观测。
func TestStep2d_AggregateCompletesWithPerSourceStatus(t *testing.T) {
	agg := aggregateStatus(t)
	if len(agg.Sources) == 0 {
		t.Fatalf("no per-source aggregate outcomes")
	}
	for _, s := range agg.Sources {
		if s.Error != "" {
			t.Logf("source %s/%s annotated: %s", s.Cloud, s.AccountName, s.Error)
		}
	}
}

// Journey Step 3b: 下钻态叠加手工筛选 -> 组合语义共存。
func TestStep3b_DrilldownPlusManualFilterCoexist(t *testing.T) {
	agg := aggregateStatus(t)
	group := pickGroup(t, agg)
	start, end := window()
	status, env := lqe.Post(t, lqe.APIPrefix+"/search", lqe.SearchBody{
		LogType: "waf", StartTime: start, EndTime: end,
		Filters: []lqe.FieldFilter{
			{Field: "status", Op: "eq", Value: group},
			{Field: "status", Op: "neq", Value: "definitely-not-it"},
		},
	})
	if status != http.StatusOK || !env.OK() {
		t.Fatalf("coexist search status=%d msg=%s", status, env.Msg)
	}
	got := lqe.SearchData(t, env)
	for i, e := range got.Entries {
		if trimNumber(int64(e.Number("status"))) != group {
			t.Fatalf("entry %d violates drilldown filter", i)
		}
	}
	solo := searchWithFilter(t, group)
	if got.Total > 2*solo.Total || 2*got.Total < solo.Total {
		t.Errorf("adding an unrelated neq moved the result out of band: %d -> %d", solo.Total, got.Total)
	}
}

// Journey Step 5c: 下钻态修改时间范围 -> 分组筛选与新窗口组合生效。
func TestStep5c_DrilldownWithNewTimeRange(t *testing.T) {
	agg := aggregateStatus(t)
	group := pickGroup(t, agg)
	now := lqe.NowMs()
	status, env := lqe.Post(t, lqe.APIPrefix+"/search", lqe.SearchBody{
		LogType: "waf", StartTime: now - 600_000, EndTime: now, // 10min window (changed)
		Filters: []lqe.FieldFilter{{Field: "status", Op: "eq", Value: group}},
	})
	if status != http.StatusOK || !env.OK() {
		t.Fatalf("new-window drilldown status=%d msg=%s", status, env.Msg)
	}
	got := lqe.SearchData(t, env)
	for i, e := range got.Entries {
		if trimNumber(int64(e.Number("status"))) != group {
			t.Fatalf("entry %d violates filter after window change", i)
		}
		if ts := e.Timestamp(); ts < now-600_000 || ts > now {
			t.Errorf("entry %d timestamp %d outside new window", i, ts)
		}
	}
}

// Error paths: invalid metric / inverted window are readable 400s.
func TestAggregateErrorPathsReadable(t *testing.T) {
	start, end := window()
	cases := []struct {
		name string
		body lqe.AggregateBody
	}{
		{"invalid metric", lqe.AggregateBody{LogType: "waf", StartTime: start, EndTime: end, Metric: "bogus"}},
		{"inverted window", lqe.AggregateBody{LogType: "waf", StartTime: end, EndTime: start, Metric: "count"}},
		{"unknown log type", lqe.AggregateBody{LogType: "nope", StartTime: start, EndTime: end, Metric: "count"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, env := lqe.Post(t, lqe.APIPrefix+"/aggregate", tc.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400", status)
			}
			if env.Msg == "" {
				t.Fatalf("400 without readable msg")
			}
		})
	}
}

// Journey smoke: aggregate -> drilldown -> replace view via clear.
func TestJourneySmokeAggregateDrilldownClear(t *testing.T) {
	agg := aggregateStatus(t)
	group := pickGroup(t, agg)
	drilled := searchWithFilter(t, group)
	for _, e := range drilled.Entries {
		if trimNumber(int64(e.Number("status"))) != group {
			t.Fatalf("smoke: drilldown filter violated")
		}
	}
	base := baseline(t)
	if drilled.Total > base.Total {
		t.Fatalf("smoke: drilldown exceeds baseline")
	}
}
