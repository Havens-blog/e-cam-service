package cdncache

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
)

// state 构造 cache_hit 计数分布条目。
func state(name string, count int64) logquery.TopNItem {
	return logquery.TopNItem{Name: name, Count: count}
}

// bstate 构造 cache_hit×sum_bytes 条目(Value=字节)。
func bstate(name string, count, bytes int64) logquery.TopNItem {
	return logquery.TopNItem{Name: name, Count: count, Value: float64(bytes)}
}

// uriMiss 构造 URI 未命中条目。
func uriMiss(uri, host string, miss int64) URIMissItem {
	return URIMissItem{URI: uri, Host: host, Miss: miss}
}

// almostEqual 浮点相等(1e-9 容差)。
func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// findItem 按动作查找优化项(测试辅助)。
func findItem(items []OptimizationItem, action string) *OptimizationItem {
	for i := range items {
		if items[i].Action == action {
			return &items[i]
		}
	}
	return nil
}

// containsNote 判定 Notes 中存在包含 substr 的标注。
func containsNote(res *CacheAnalyzeResult, substr string) bool {
	for _, n := range res.Notes {
		if strings.Contains(n, substr) {
			return true
		}
	}
	return false
}

// TestNormalizeURIPath URI 查询串剥离归一(带/不带 query、fragment、空值)。AC4
func TestNormalizeURIPath(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"/a/b.js?x=1&y=2", "/a/b.js"},
		{"/a/b.js", "/a/b.js"},
		{"", "/"},
		{"?x=1", "/"},
		{"/a/b#frag", "/a/b"},
		{"/a/b?x=1#frag", "/a/b"},
		{"a/b?x=1", "/a/b"}, // 补前导 /
		{"  /a?x=1  ", "/a"},
	}
	for _, c := range cases {
		if got := NormalizeURIPath(c.raw); got != c.want {
			t.Errorf("NormalizeURIPath(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

// TestNormalizeURIQuery URI 查询串排序归一(参数按名称/值升序,同参异序归一)。AC4
func TestNormalizeURIQuery(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"/a?b=2&a=1", "/a?a=1&b=2"},
		{"/a?a=1&b=2", "/a?a=1&b=2"},
		{"/a", "/a"},
		{"/a?", "/a"},
		{"/a?a=1&a=0&b=2", "/a?a=0&a=1&b=2"}, // 同名参数按值排序
		{"", "/"},
		{"/a?x=%2F1&y=2", "/a?x=%2F1&y=2"},
		{"#frag", "/"},
		{"?b=2&a=1", "/?a=1&b=2"},
	}
	for _, c := range cases {
		if got := NormalizeURIQuery(c.raw); got != c.want {
			t.Errorf("NormalizeURIQuery(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
	// 幂等:归一结果再归一不变。
	once := NormalizeURIQuery("/p?b=2&a=1&b=1")
	if twice := NormalizeURIQuery(once); twice != once {
		t.Errorf("NormalizeURIQuery not idempotent: %q -> %q", once, twice)
	}
}

// TestRequestHitRates 双命中率之请求命中率:全请求/可缓存两档,分子分母与
// 命中归类(命中=hit+partial,未命中=miss+error,"-" 计入分母)。AC1
func TestRequestHitRates(t *testing.T) {
	input := &CacheAnalyzeInput{
		TotalRequests: 1000,
		CacheHitDist: []logquery.TopNItem{
			state("hit", 600), state("partial", 100), state("miss", 150),
			state("error", 90), state("-", 60),
		},
		StatusDist: []logquery.TopNItem{state("404", 100), state("503", 50), state("200", 850)},
	}
	res := Evaluate(input)

	// 全请求:分子 = hit+partial = 700,分母 = 1000(含 "-" 60)。
	if got := res.RequestHitRate.All; got.Numerator != 700 || got.Denominator != 1000 || !almostEqual(got.Rate, 0.7) || !got.Available {
		t.Errorf("RequestHitRate.All = %+v, want {700,1000,0.7,available}", got)
	}
	// 可缓存:剔除量取 max(cache_hit=error 90, 4xx+5xx 150) = 150 → 分母 850。
	if got := res.RequestHitRate.Cacheable; got.Numerator != 700 || got.Denominator != 850 || !almostEqual(got.Rate, 700.0/850.0) || !got.Available {
		t.Errorf("RequestHitRate.Cacheable = %+v, want {700,850,0.8235…,available}", got)
	}
	// 可缓存近似口径标注(partial/error 归类文档化)。
	if !containsNote(res, "聚合层近似") {
		t.Errorf("Notes 缺可缓存近似口径标注: %v", res.Notes)
	}
	if !containsNote(res, "hit+partial") || !containsNote(res, "miss+error") {
		t.Errorf("Notes 缺命中归类文档化标注: %v", res.Notes)
	}
}

// TestByteHitRates 双命中率之字节命中率:全请求主口径(partial 按全命中计入并
// 标注上偏)+ 可缓存字节口径(仅剔除 cache_hit=error 字节)。AC1
func TestByteHitRates(t *testing.T) {
	input := &CacheAnalyzeInput{
		TotalRequests: 1000,
		CacheHitDist:  []logquery.TopNItem{state("hit", 700), state("miss", 300)},
		CacheHitBytes: []logquery.TopNItem{
			bstate("hit", 700, 5000), bstate("partial", 0, 3000), bstate("miss", 300, 2000),
		},
		TotalBytes: 10000,
	}
	res := Evaluate(input)
	// 全请求:分子 = 5000+3000(partial 全命中计入) = 8000。
	if got := res.ByteHitRate.All; got.Numerator != 8000 || got.Denominator != 10000 || !almostEqual(got.Rate, 0.8) || !got.Available {
		t.Errorf("ByteHitRate.All = %+v, want {8000,10000,0.8,available}", got)
	}
	// 可缓存:剔除 error 字节 0 → 分母 10000。
	if got := res.ByteHitRate.Cacheable; got.Denominator != 10000 || !almostEqual(got.Rate, 0.8) {
		t.Errorf("ByteHitRate.Cacheable = %+v, want {8000,10000,0.8}", got)
	}
	if !containsNote(res, "上偏") {
		t.Errorf("Notes 缺 partial 字节上偏标注: %v", res.Notes)
	}

	// TotalBytes 缺失 → 回退 CacheHitBytes 求和;error 字节被可缓存口径剔除。
	input2 := &CacheAnalyzeInput{
		TotalRequests: 1000,
		CacheHitDist:  []logquery.TopNItem{state("hit", 700), state("miss", 200), state("error", 100)},
		CacheHitBytes: []logquery.TopNItem{
			bstate("hit", 700, 5000), bstate("partial", 0, 3000),
			bstate("miss", 200, 1500), bstate("error", 100, 500),
		},
	}
	res2 := Evaluate(input2)
	if got := res2.ByteHitRate.All; got.Denominator != 10000 || !almostEqual(got.Rate, 0.8) {
		t.Errorf("ByteHitRate.All(fallback total) = %+v, want denom 10000 rate 0.8", got)
	}
	if got := res2.ByteHitRate.Cacheable; got.Denominator != 9500 || !almostEqual(got.Rate, 8000.0/9500.0) {
		t.Errorf("ByteHitRate.Cacheable(fallback) = %+v, want denom 9500 rate 0.8421", got)
	}

	// 字节口径缺失 → 不可用 + 降级标注。
	res3 := Evaluate(&CacheAnalyzeInput{TotalRequests: 100, CacheHitDist: []logquery.TopNItem{state("hit", 100)}})
	if res3.ByteHitRate.All.Available || res3.ByteHitRate.Cacheable.Available {
		t.Errorf("ByteHitRate 应不可用: %+v", res3.ByteHitRate)
	}
	if !containsNote(res3, "字节") {
		t.Errorf("Notes 缺字节缺失标注: %v", res3.Notes)
	}
}

// TestGradeThresholds 健康档位临界分(默认 0.90 优 / 0.80 中,含下界)与参数化。AC2
func TestGradeThresholds(t *testing.T) {
	cases := []struct {
		hit  int64
		want string
	}{
		{900, GradeGood}, // 恰在优档下界
		{899, GradeFair}, // 临界之下
		{800, GradeFair}, // 恰在中档下界
		{799, GradePoor}, // 中档之下
	}
	for _, c := range cases {
		input := &CacheAnalyzeInput{
			TotalRequests: 1000,
			CacheHitDist:  []logquery.TopNItem{state("hit", c.hit), state("miss", 1000-c.hit)},
		}
		if got := Evaluate(input).Grade; got != c.want {
			t.Errorf("hit=%d: Grade = %s, want %s", c.hit, got, c.want)
		}
	}
	// 参数化:自定义档位阈值生效。
	cfg := DefaultConfig
	cfg.DefaultLevels = LevelThresholds{GoodMin: 0.5, MediumMin: 0.3}
	input := &CacheAnalyzeInput{
		TotalRequests: 1000,
		CacheHitDist:  []logquery.TopNItem{state("hit", 600), state("miss", 400)},
	}
	if got := EvaluateWithConfig(input, &cfg).Grade; got != GradeGood {
		t.Errorf("自定义阈值下 Grade = %s, want %s", got, GradeGood)
	}
}

// TestCalibrateThresholds 按历史命中率分位数初始化档位边界;取不到回退默认。AC2
func TestCalibrateThresholds(t *testing.T) {
	cases := []struct {
		name     string
		hist     []float64
		wantGood float64
		wantMed  float64
	}{
		{"empty_falls_back", nil, 0.90, 0.80},
		{"p25_p75", []float64{0.5, 0.6, 0.7, 0.8, 0.9}, 0.80, 0.60},
		{"above_default_capped", []float64{0.95, 0.97, 0.99}, 0.90, 0.80},
		{"low_history", []float64{0.30, 0.35, 0.40, 0.45}, 0.4125, 0.3375},
		{"equal_values_gap", []float64{0.5, 0.5}, 0.51, 0.50}, // 档距兜底
		{"invalid_filtered", []float64{2, -1}, 0.90, 0.80},
	}
	for _, c := range cases {
		got := CalibrateThresholds(c.hist)
		if !almostEqual(got.GoodMin, c.wantGood) || !almostEqual(got.MediumMin, c.wantMed) {
			t.Errorf("%s: CalibrateThresholds = %+v, want {good %.4f, medium %.4f}", c.name, got, c.wantGood, c.wantMed)
		}
		if got.GoodMin <= got.MediumMin {
			t.Errorf("%s: 档位边界无序 good %v ≤ medium %v", c.name, got.GoodMin, got.MediumMin)
		}
	}
}

// TestGapLargeFile 请求命中尚可而字节明显低 → 大文件未命中(回源带宽浪费)。AC3
func TestGapLargeFile(t *testing.T) {
	input := &CacheAnalyzeInput{
		TotalRequests: 1000,
		CacheHitDist:  []logquery.TopNItem{state("hit", 850), state("partial", 50), state("miss", 100)},
		CacheHitBytes: []logquery.TopNItem{
			bstate("hit", 850, 20000), bstate("partial", 50, 5000), bstate("miss", 100, 75000),
		},
		TotalBytes: 100000,
	}
	res := Evaluate(input)
	if !res.Gap.LargeFileGap {
		t.Errorf("LargeFileGap = false, want true(gap %.2f)", res.Gap.ByteGapRatio)
	}
	if !almostEqual(res.Gap.ByteGapRatio, 0.9-0.25) {
		t.Errorf("ByteGapRatio = %v, want 0.65", res.Gap.ByteGapRatio)
	}
	it := findItem(res.Recommendations, ActionTuneTTL)
	if it == nil {
		t.Fatalf("缺大文件未命中优化项: %+v", res.Recommendations)
	}
	if it.Confidence != ConfidenceHigh || it.LowConfidence {
		t.Errorf("大文件项可信度 = %s, want high", it.Confidence)
	}
	// 未命中流量占比 = 未命中字节占总字节(该项关联流量口径)。
	if !almostEqual(it.MissTrafficRatio, 0.75) {
		t.Errorf("MissTrafficRatio = %v, want 0.75", it.MissTrafficRatio)
	}
	if !strings.Contains(it.Evidence, "带宽") {
		t.Errorf("Evidence 缺带宽浪费证据: %s", it.Evidence)
	}

	// 反例:字节命中率接近请求命中率 → 不判大文件 gap。
	input.CacheHitBytes = []logquery.TopNItem{
		bstate("hit", 850, 85000), bstate("partial", 50, 5000), bstate("miss", 100, 10000),
	}
	if res := Evaluate(input); res.Gap.LargeFileGap {
		t.Errorf("LargeFileGap = true, want false(字节命中 0.9)")
	}
}

// TestGapSmallFile 请求命中率低而字节尚可 → 小文件大量 miss(回源请求数/QPS 浪费)。AC3
func TestGapSmallFile(t *testing.T) {
	input := &CacheAnalyzeInput{
		TotalRequests: 1000,
		CacheHitDist: []logquery.TopNItem{
			state("hit", 300), state("partial", 100), state("miss", 500), state("error", 100),
		},
		CacheHitBytes: []logquery.TopNItem{
			bstate("hit", 300, 90000), bstate("partial", 100, 5000), bstate("miss", 600, 5000),
		},
		TotalBytes: 100000,
	}
	res := Evaluate(input)
	if !res.Gap.SmallFileReverse {
		t.Errorf("SmallFileReverse = false, want true")
	}
	it := findItem(res.Recommendations, ActionAddCacheRule)
	if it == nil {
		t.Fatalf("缺小文件未命中优化项: %+v", res.Recommendations)
	}
	if it.Confidence != ConfidenceHigh {
		t.Errorf("小文件项可信度 = %s, want high", it.Confidence)
	}
	if !almostEqual(it.MissTrafficRatio, 0.6) { // 未命中请求 600/1000
		t.Errorf("MissTrafficRatio = %v, want 0.6", it.MissTrafficRatio)
	}
	if !strings.Contains(it.Evidence, "QPS") {
		t.Errorf("Evidence 缺回源请求浪费证据: %s", it.Evidence)
	}
	// 大文件方向不应同时触发(字节命中率更高)。
	if findItem(res.Recommendations, ActionTuneTTL) != nil {
		t.Errorf("不应出现大文件 tune_ttl 优化项")
	}
}

// TestURIMissConcentration 未命中 URI 集中度(查询串归一)与"忽略查询串"证据、
// 动态接口低可信度。AC4/AC5
func TestURIMissConcentration(t *testing.T) {
	input := &CacheAnalyzeInput{
		TotalRequests: 1000,
		CacheHitDist:  []logquery.TopNItem{state("hit", 500), state("miss", 500)},
		CacheHitBytes: []logquery.TopNItem{bstate("hit", 500, 5000), bstate("miss", 500, 5000)},
		URIMisses: []URIMissItem{
			uriMiss("/img/banner.png?ts=222", "a.jlc.com", 100),
			uriMiss("/img/banner.png?ts=111", "a.jlc.com", 150),
			uriMiss("/api/user/list", "b.jlc.com", 100),
			uriMiss("/static/app.js", "", 50),
		},
	}
	res := Evaluate(input)

	// 未命中 URI TOP:归一路径合并变体,按 miss 降序。
	if len(res.MissURITop) != 3 {
		t.Fatalf("MissURITop = %d 条, want 3", len(res.MissURITop))
	}
	top := res.MissURITop[0]
	if top.URI != "/img/banner.png" || top.MissCount != 250 || !almostEqual(top.MissShare, 0.5) ||
		top.Variants != 2 || top.Host != "a.jlc.com" || top.SampleQuery != "ts=111" {
		t.Errorf("Top URI = %+v, want banner 250/0.5/variants2/a.jlc.com/ts=111", top)
	}

	// 变体 ≥2 → 忽略查询串动作 + miss 集中证据。
	it := findItem(res.Recommendations, ActionIgnoreQueryString)
	if it == nil {
		t.Fatalf("缺忽略查询串优化项: %+v", res.Recommendations)
	}
	if it.URIPrefix != "/img/banner.png" || it.Domain != "a.jlc.com" || !almostEqual(it.MissTrafficRatio, 0.5) {
		t.Errorf("忽略查询串项 = %+v", it)
	}
	if !strings.Contains(it.Evidence, "查询串") || !strings.Contains(it.Evidence, "2") {
		t.Errorf("忽略查询串项缺 miss 集中证据: %s", it.Evidence)
	}
	if it.Confidence != ConfidenceHigh {
		t.Errorf("忽略查询串项可信度 = %s, want high", it.Confidence)
	}

	// 动态接口启发式 → 低可信度 + 折叠标记。
	var dyn *OptimizationItem
	for i := range res.Recommendations {
		if res.Recommendations[i].URIPrefix == "/api/user/list" {
			dyn = &res.Recommendations[i]
		}
	}
	if dyn == nil {
		t.Fatalf("缺动态接口优化项: %+v", res.Recommendations)
	}
	if dyn.Confidence != ConfidenceLow || !dyn.LowConfidence {
		t.Errorf("动态接口项可信度 = %s low=%v, want low/true", dyn.Confidence, dyn.LowConfidence)
	}
	if !strings.Contains(dyn.Evidence, "动态") {
		t.Errorf("动态接口项缺启发式证据: %s", dyn.Evidence)
	}
}

// TestErrorStatusRatio 4xx/5xx 占比 → 强制回源校验优化项(中可信度)。AC4/AC5
func TestErrorStatusRatio(t *testing.T) {
	input := &CacheAnalyzeInput{
		TotalRequests: 1000,
		CacheHitDist:  []logquery.TopNItem{state("hit", 700), state("miss", 300)},
		CacheHitBytes: []logquery.TopNItem{bstate("hit", 700, 7000), bstate("miss", 300, 3000)},
		StatusDist:    []logquery.TopNItem{state("404", 120), state("503", 60), state("200", 820)},
	}
	res := Evaluate(input)
	if !almostEqual(res.Status.ClientErrorRatio, 0.12) || !almostEqual(res.Status.ServerErrorRatio, 0.06) {
		t.Errorf("Status = %+v, want 4xx 0.12 / 5xx 0.06", res.Status)
	}
	it := findItem(res.Recommendations, ActionOriginVerify)
	if it == nil {
		t.Fatalf("缺回源校验优化项: %+v", res.Recommendations)
	}
	if it.Confidence != ConfidenceMedium {
		t.Errorf("回源校验项可信度 = %s, want medium(整体口径近似)", it.Confidence)
	}
	if !almostEqual(it.MissTrafficRatio, 0.18) {
		t.Errorf("MissTrafficRatio = %v, want 0.18", it.MissTrafficRatio)
	}
	// 低占比不产生该项。
	input.StatusDist = []logquery.TopNItem{state("404", 50), state("200", 950)}
	if res := Evaluate(input); findItem(res.Recommendations, ActionOriginVerify) != nil {
		t.Errorf("4xx/5xx 低于阈值不应产生回源校验项")
	}
}

// TestOptimizationTopN 非优档按未命中流量占比降序 Top N;优档 0 条(不硬凑)。AC4
func TestOptimizationTopN(t *testing.T) {
	input := &CacheAnalyzeInput{
		TotalRequests: 1000,
		CacheHitDist:  []logquery.TopNItem{state("hit", 400), state("miss", 600)},
		CacheHitBytes: []logquery.TopNItem{bstate("hit", 400, 4000), bstate("miss", 600, 6000)},
		HostStats: []HostCacheStat{
			{Host: "a.jlc.com", Hit: 200, Miss: 400},
			{Host: "b.jlc.com", Hit: 100, Miss: 150},
		},
		URIMisses: []URIMissItem{
			uriMiss("/x/a.png", "a.jlc.com", 300),
			uriMiss("/x/b.png", "b.jlc.com", 120),
			uriMiss("/x/c.png", "", 60),
		},
		StatusDist: []logquery.TopNItem{state("404", 150), state("200", 850)},
	}
	res := Evaluate(input)
	if len(res.Recommendations) != 5 {
		t.Fatalf("Recommendations = %d 条, want Top 5", len(res.Recommendations))
	}
	// 降序校验(占比相同时可信度/名称次序兜底)。
	for i := 1; i < len(res.Recommendations); i++ {
		if res.Recommendations[i-1].MissTrafficRatio < res.Recommendations[i].MissTrafficRatio {
			t.Errorf("优化项未按占比降序: %v", res.Recommendations)
		}
	}
	// Top1 = 域名 a.jlc.com(miss 占全局 400/600 ≈ 0.667,中可信度)。
	first := res.Recommendations[0]
	if first.Domain != "a.jlc.com" || first.Action != ActionAddCacheRule || first.Confidence != ConfidenceMedium {
		t.Errorf("Top1 优化项 = %+v, want a.jlc.com/add_cache_rule/medium", first)
	}
	// TopN 截断:占比最低的 /x/c.png(0.1)被截掉。
	for _, it := range res.Recommendations {
		if it.URIPrefix == "/x/c.png" {
			t.Errorf("/x/c.png(占比最低)不应进入 Top5: %+v", res.Recommendations)
		}
	}

	// 优档:0 条优化项(不硬凑建议)。
	good := &CacheAnalyzeInput{
		TotalRequests: 1000,
		CacheHitDist:  []logquery.TopNItem{state("hit", 950), state("miss", 50)},
		CacheHitBytes: []logquery.TopNItem{bstate("hit", 950, 9500), bstate("miss", 50, 500)},
		HostStats:     []HostCacheStat{{Host: "a.jlc.com", Hit: 950, Miss: 50}},
		URIMisses:     []URIMissItem{uriMiss("/x/a.png", "a.jlc.com", 50)},
		StatusDist:    []logquery.TopNItem{state("404", 80), state("200", 920)},
	}
	if res := Evaluate(good); res.Grade != GradeGood || len(res.Recommendations) != 0 {
		t.Errorf("优档 Grade=%s Recommendations=%d, want good/0", res.Grade, len(res.Recommendations))
	}
}

// TestDomainRanking 域名命中率排行(请求数降序、档位按域名阈值覆盖)。AC2
func TestDomainRanking(t *testing.T) {
	input := &CacheAnalyzeInput{
		TotalRequests: 2100,
		CacheHitDist: []logquery.TopNItem{
			state("hit", 690), state("partial", 110), state("miss", 1200), state("error", 100),
		},
		HostStats: []HostCacheStat{
			{Host: "b.jlc.com", Hit: 90, Partial: 10, Miss: 900},
			{Host: "a.jlc.com", Hit: 600, Partial: 100, Miss: 200, Error: 100},
		},
	}
	cfg := DefaultConfig
	cfg.DomainLevels = map[string]LevelThresholds{
		"b.jlc.com": {GoodMin: 0.05, MediumMin: 0.02}, // 历史分位数初始化的域名级覆盖
	}
	res := EvaluateWithConfig(input, &cfg)
	if len(res.DomainRanking) != 2 {
		t.Fatalf("DomainRanking = %d 条, want 2", len(res.DomainRanking))
	}
	a, b := res.DomainRanking[0], res.DomainRanking[1]
	if a.Host != "a.jlc.com" || b.Host != "b.jlc.com" {
		t.Errorf("排行顺序 = [%s, %s], want [a, b](请求数并列按域名升序)", a.Host, b.Host)
	}
	if !almostEqual(a.HitRate, 0.7) || !almostEqual(a.CacheableHitRate, 700.0/900.0) {
		t.Errorf("a.jlc.com 命中率 = %v/%v, want 0.7/0.7778", a.HitRate, a.CacheableHitRate)
	}
	if a.Grade != GradePoor {
		t.Errorf("a.jlc.com 档位 = %s, want poor(0.7778 < 0.8)", a.Grade)
	}
	if b.Grade != GradeGood {
		t.Errorf("b.jlc.com 档位 = %s, want good(域名阈值覆盖 0.05)", b.Grade)
	}
	if !almostEqual(b.MissTrafficRatio, 900.0/1300.0) {
		t.Errorf("b.jlc.com miss 占比 = %v, want 0.6923", b.MissTrafficRatio)
	}

	// TopN 截断。
	cfg.DomainTopLimit = 1
	if res := EvaluateWithConfig(input, &cfg); len(res.DomainRanking) != 1 {
		t.Errorf("DomainTopLimit=1 时排行 = %d 条, want 1", len(res.DomainRanking))
	}
}

// TestPrevTrend 前窗对比:趋势 up/down/flat 与下降关注阈值。AC2(前窗对比)
func TestPrevTrend(t *testing.T) {
	base := CacheAnalyzeInput{
		TotalRequests: 1000,
		CacheHitDist:  []logquery.TopNItem{state("hit", 700), state("miss", 300)},
	}
	prevOK := &PrevCacheWindow{Total: 1000, CacheHitDist: []logquery.TopNItem{state("hit", 500), state("miss", 500)}}

	// 上升:0.7 − 0.5 = +0.2。
	up := base
	up.Prev = prevOK
	if res := Evaluate(&up); !res.Prev.Available || res.Prev.Direction != "up" || res.Prev.Alert ||
		!almostEqual(res.Prev.PrevRequestHitRate, 0.5) || !almostEqual(res.Prev.Delta, 0.2) {
		t.Errorf("PrevTrend(up) = %+v", res.Prev)
	}
	// 下降超阈值:0.4 − 0.5 = −0.1 → down + 关注。
	down := base
	down.CacheHitDist = []logquery.TopNItem{state("hit", 400), state("miss", 600)}
	down.Prev = prevOK
	if res := Evaluate(&down); !res.Prev.Available || res.Prev.Direction != "down" || !res.Prev.Alert {
		t.Errorf("PrevTrend(down) = %+v", res.Prev)
	}
	// 平稳带:0.502 − 0.5 = +0.002 < 0.005 → flat。
	flat := base
	flat.CacheHitDist = []logquery.TopNItem{state("hit", 502), state("miss", 498)}
	flat.Prev = prevOK
	if res := Evaluate(&flat); res.Prev.Direction != "flat" {
		t.Errorf("PrevTrend(flat) = %+v, want flat", res.Prev)
	}
	// 前窗缺失:不可用 + 标注。
	noPrev := base
	if res := Evaluate(&noPrev); res.Prev.Available || !containsNote(res, "前窗") {
		t.Errorf("PrevTrend(none) = %+v, notes %v", res.Prev, res.Notes)
	}
	// 前窗 Total 有值但分布缺失 → 不可用。
	badPrev := base
	badPrev.Prev = &PrevCacheWindow{Total: 1000}
	if res := Evaluate(&badPrev); res.Prev.Available {
		t.Errorf("前窗分布缺失应不可用: %+v", res.Prev)
	}
}

// TestEvaluateNilAndEmpty nil/空输入安全(纯函数不 panic,输出安全空结论)。AC6
func TestEvaluateNilAndEmpty(t *testing.T) {
	res := Evaluate(nil)
	if res == nil || res.Grade != GradeUnknown {
		t.Errorf("nil 输入 Grade = %v, want unknown", res)
	}
	if !containsNote(res, "输入为空") {
		t.Errorf("nil 输入缺标注: %v", res.Notes)
	}
	empty := Evaluate(&CacheAnalyzeInput{})
	if empty.Grade != GradeUnknown {
		t.Errorf("空输入 Grade = %s, want unknown", empty.Grade)
	}
	if empty.RequestHitRate.All.Available || empty.ByteHitRate.All.Available {
		t.Errorf("空输入命中率应不可用")
	}
	if len(empty.Recommendations) != 0 {
		t.Errorf("空输入不应产生优化项: %v", empty.Recommendations)
	}
	if !containsNote(empty, "请求数据缺失") || !containsNote(empty, "域名") || !containsNote(empty, "URI") {
		t.Errorf("空输入缺降级标注: %v", empty.Notes)
	}
}

// TestDegradedNotes 数据面降级标注(域名交叉缺失、URI 缺失、TopN 下界)。AC6
func TestDegradedNotes(t *testing.T) {
	input := &CacheAnalyzeInput{
		TotalRequests: 100,
		CacheHitDist:  []logquery.TopNItem{state("hit", 50), state("miss", 50)},
	}
	res := Evaluate(input)
	if !containsNote(res, "域名") {
		t.Errorf("缺域名交叉缺失标注: %v", res.Notes)
	}
	if !containsNote(res, "URI") {
		t.Errorf("缺 URI 分布缺失标注: %v", res.Notes)
	}
	// 有 URI 数据时标注 TopN 占比为下界。
	input.URIMisses = []URIMissItem{uriMiss("/a", "h", 30)}
	res = Evaluate(input)
	if !containsNote(res, "下界") {
		t.Errorf("缺 TopN 占比下界标注: %v", res.Notes)
	}
}

// TestCacheableDenomGuard 可缓存分母保护:全 error 流量 → 可缓存档不可用,档位回退全请求口径。AC1
func TestCacheableDenomGuard(t *testing.T) {
	input := &CacheAnalyzeInput{
		TotalRequests: 1000,
		CacheHitDist:  []logquery.TopNItem{state("error", 1000)},
	}
	res := Evaluate(input)
	if res.RequestHitRate.Cacheable.Available {
		t.Errorf("可缓存档应不可用: %+v", res.RequestHitRate.Cacheable)
	}
	if res.Grade != GradePoor { // 回退全请求口径 0 → 差
		t.Errorf("Grade = %s, want poor(回退全请求口径)", res.Grade)
	}
}

// TestEvaluateDeterministic 同一输入 → 同一输出(口径可复现;含输出排序稳定)。AC6
func TestEvaluateDeterministic(t *testing.T) {
	input := &CacheAnalyzeInput{
		TotalRequests: 1000,
		CacheHitDist:  []logquery.TopNItem{state("hit", 400), state("miss", 600)},
		CacheHitBytes: []logquery.TopNItem{bstate("hit", 400, 4000), bstate("miss", 600, 6000)},
		HostStats: []HostCacheStat{
			{Host: "b.jlc.com", Hit: 100, Miss: 150},
			{Host: "a.jlc.com", Hit: 200, Miss: 400},
		},
		URIMisses: []URIMissItem{
			uriMiss("/x/a.png?b=2&a=1", "a.jlc.com", 300),
			uriMiss("/x/c.png", "", 60),
			uriMiss("/x/b.png", "b.jlc.com", 120),
		},
		StatusDist: []logquery.TopNItem{state("404", 150), state("200", 850)},
		Prev:       &PrevCacheWindow{Total: 900, CacheHitDist: []logquery.TopNItem{state("hit", 450), state("miss", 450)}},
	}
	a, err := json.Marshal(Evaluate(input))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	b, err := json.Marshal(Evaluate(input))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("同一输入两次输出不一致:\n%s\n%s", a, b)
	}
}

// TestLabels 展示标签(供 API 层/前端复用)。
func TestLabels(t *testing.T) {
	if GradeLabel(GradeGood) != "优" || GradeLabel(GradeFair) != "中" || GradeLabel(GradePoor) != "差" || GradeLabel(GradeUnknown) != "未知" {
		t.Errorf("GradeLabel 异常: %v %v %v %v", GradeLabel(GradeGood), GradeLabel(GradeFair), GradeLabel(GradePoor), GradeLabel(GradeUnknown))
	}
	if ActionLabel(ActionAddCacheRule) != "加/调缓存规则" ||
		ActionLabel(ActionTuneTTL) != "调整 TTL" ||
		ActionLabel(ActionIgnoreQueryString) != "忽略查询串" ||
		ActionLabel(ActionOriginVerify) != "强制回源校验" {
		t.Errorf("ActionLabel 异常")
	}
	if ConfidenceLabel(ConfidenceHigh) != "高" || ConfidenceLabel(ConfidenceMedium) != "中" || ConfidenceLabel(ConfidenceLow) != "低" {
		t.Errorf("ConfidenceLabel 异常")
	}
}
