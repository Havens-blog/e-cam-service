package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/logquery/cdncache"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// ---------------------------------------------------------------------
// 测试桩:按 (窗口, 维度, 指标) 编程的聚合 provider(缓存分析并发调用以
// metric 区分 cache_hit 的 count/sum_bytes 两次调用,键比诊断桩多一维)。
// ---------------------------------------------------------------------

// cacheCall 记录一次 provider 聚合调用(帧数/窗口/维度/指标断言用)。
type cacheCall struct {
	Start, End int64
	Dimension  string
	Metric     string
}

// cacheAggKey 编程键(空 metric 视为 count,与 provider 语义一致)。
func cacheAggKey(start, end int64, dim, metric string) string {
	if metric == "" {
		metric = "count"
	}
	return fmt.Sprintf("%d|%d|%s|%s", start, end, dim, metric)
}

// cacheScriptedAggregator 可编程聚合桩:结果/错误按 "start|end|dim|metric" 键注入;
// 缺键返回空结果(窗口无数据)。并发安全(缓存分析多维并发调用)。
type cacheScriptedAggregator struct {
	fakeProvider
	mu      sync.Mutex
	calls   []cacheCall
	results map[string]logquery.AggregateResult
	errs    map[string]error
}

func (p *cacheScriptedAggregator) Aggregate(_ context.Context, _ *domain.CloudAccount, params logquery.AggregateParams) (*logquery.AggregateResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, cacheCall{Start: params.StartTime, End: params.EndTime,
		Dimension: params.Dimension, Metric: params.Metric})
	key := cacheAggKey(params.StartTime, params.EndTime, params.Dimension, params.Metric)
	if err, ok := p.errs[key]; ok {
		return nil, err
	}
	if r, ok := p.results[key]; ok {
		cp := r
		return &cp, nil
	}
	return &logquery.AggregateResult{}, nil
}

// snapshot 当前调用清单(并发读安全)。
func (p *cacheScriptedAggregator) snapshot() []cacheCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]cacheCall(nil), p.calls...)
}

// registerCacheAgg 注册 CDN 缓存分析测试 provider;failAccountID>0 时该账号恒失败。
func registerCacheAgg(t *testing.T, agg *cacheScriptedAggregator, failAccountID int64) {
	t.Helper()
	logquery.RegisterProvider(testCloud, logquery.LogTypeCDN, func(acc *domain.CloudAccount) (logquery.LogProvider, error) {
		if failAccountID > 0 && acc.ID == failAccountID {
			return &errAggregator{err: errors.New("boom")}, nil
		}
		return agg, nil
	})
}

// newCacheAgg 建桩 + 注册(默认无失败账号)。
func newCacheAgg(t *testing.T, failAccountID int64) *cacheScriptedAggregator {
	t.Helper()
	agg := &cacheScriptedAggregator{results: map[string]logquery.AggregateResult{}, errs: map[string]error{}}
	registerCacheAgg(t, agg, failAccountID)
	return agg
}

// cacheItem TopN 条目(Value 语义随指标:count=计数,sum_bytes=字节)。
func cacheItem(name string, count int64, value float64) logquery.TopNItem {
	return logquery.TopNItem{Name: name, Count: count, Value: value}
}

// cacheWindow 标准 10 分钟分析窗。
func cacheWindow() (start, end int64) {
	const span = int64(600_000)
	end = time.Now().UnixMilli()
	return end - span, end
}

// scriptHappyCurrent 编程当前窗 5 个维度组聚合样本(不含前窗):
// 全请求 1000;命中率 750/1000(全请求)、750/850(可缓存,max(error,4xx+5xx)=150);
// 字节总量 16_250_000,命中字节 7_200_000(hit 7M + partial 0.2M)。
func scriptHappyCurrent(agg *cacheScriptedAggregator, start, end int64) {
	// 当前窗:cache_hit 分布(count;组键为源原始值,归一由编排层完成)
	agg.results[cacheAggKey(start, end, "cache_hit", "count")] = diagResult(
		[]logquery.TopNItem{
			cacheItem("TCP_HIT", 700, 700),
			cacheItem("TCP_MISS", 200, 200),
			cacheItem("TCP_REFRESH", 50, 50),
			cacheItem("ERROR_TIMEOUT", 30, 30),
			cacheItem("-", 20, 20),
		}, 400, 600)
	// 当前窗:cache_hit × sum_bytes(双字节口径输入)
	agg.results[cacheAggKey(start, end, "cache_hit", "sum_bytes")] = diagResult(
		[]logquery.TopNItem{
			cacheItem("TCP_HIT", 700, 7_000_000),
			cacheItem("TCP_MISS", 200, 9_000_000),
			cacheItem("TCP_REFRESH", 50, 200_000),
			cacheItem("ERROR_TIMEOUT", 30, 30_000),
			cacheItem("-", 20, 20_000),
		}, 400, 600)
	// 当前窗:状态码分布(4xx=100、5xx=50)
	agg.results[cacheAggKey(start, end, "status", "count")] = diagResult(
		[]logquery.TopNItem{
			cacheItem("200", 850, 850),
			cacheItem("404", 100, 100),
			cacheItem("500", 50, 50),
		}, 400, 600)
	// 当前窗:host × nonhit_count(未命中 Top 域名:Count=该域名全请求,Value=未命中数)
	agg.results[cacheAggKey(start, end, "host", "nonhit_count")] = diagResult(
		[]logquery.TopNItem{
			cacheItem("a.example.com", 600, 180),
			cacheItem("b.example.com", 400, 50),
		}, 400, 600)
	// 当前窗:url × nonhit_count(未命中 Top URI:含查询串与完整 URL 两种形态)
	agg.results[cacheAggKey(start, end, "url", "nonhit_count")] = diagResult(
		[]logquery.TopNItem{
			cacheItem("/api/list?id=2", 300, 180),
			cacheItem("http://a.example.com/download/pkg.tar.gz?v=2", 200, 10),
			cacheItem("/api/list?id=3", 50, 40),
		}, 400, 600)
}

// scriptHappyWindow 当前窗 5 维度组 + 前一等长窗口 cache_hit 分布 1 帧
// (前窗命中率 0.9,当前窗 0.75 → 下降 0.15)。
func scriptHappyWindow(agg *cacheScriptedAggregator, start, end int64) {
	scriptHappyCurrent(agg, start, end)
	span := end - start
	agg.results[cacheAggKey(start-span, start, "cache_hit", "count")] = diagResult(
		[]logquery.TopNItem{
			cacheItem("TCP_HIT", 900, 900),
			cacheItem("TCP_MISS", 100, 100),
		}, 900, 100)
}

// newCacheService 建 FederationService(单账号 testCloud,聚合调用单次记录)。
func newCacheService(t *testing.T, failAccountID int64) (*FederationService, *cacheScriptedAggregator) {
	t.Helper()
	agg := newCacheAgg(t, failAccountID)
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, testCloud),
	}}, nil)
	return svc, agg
}

// newCacheServiceMulti 建 FederationService(两账号,per-source 隔离用例;
// 账号 2 供 failAccountID=2 恒失败)。
func newCacheServiceMulti(t *testing.T, failAccountID int64) (*FederationService, *cacheScriptedAggregator) {
	t.Helper()
	agg := newCacheAgg(t, failAccountID)
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, testCloud), testAccount(2, testCloud),
	}}, nil)
	return svc, agg
}

// ---------------------------------------------------------------------
// 用例
// ---------------------------------------------------------------------

// TestCacheAnalyzeDisabledByDefault AC5:feature flag 默认关,关闭时明确报错。
func TestCacheAnalyzeDisabledByDefault(t *testing.T) {
	svc, _ := newCacheService(t, 0)
	start, end := cacheWindow()
	_, err := svc.CacheAnalyze(context.Background(), 3, CacheAnalyzeRequest{
		LogType: logquery.LogTypeCDN, StartTime: start, EndTime: end,
	})
	if err == nil {
		t.Fatal("flag 默认关时应明确报错")
	}
	if !strings.Contains(err.Error(), "LOGQUERY_CACHE_ANALYZE_ENABLED") {
		t.Errorf("错误应提示 flag 名,got: %v", err)
	}
}

// TestCacheAnalyzeOnlyCDN 硬规则:仅 CDN 开放,SLB/WAF 明确报错。
func TestCacheAnalyzeOnlyCDN(t *testing.T) {
	t.Setenv(cacheAnalyzeFlagEnv, "1")
	svc, _ := newCacheService(t, 0)
	start, end := cacheWindow()
	for _, lt := range []logquery.LogType{logquery.LogTypeWAF, logquery.LogTypeSLB} {
		_, err := svc.CacheAnalyze(context.Background(), 3, CacheAnalyzeRequest{
			LogType: lt, StartTime: start, EndTime: end,
		})
		if err == nil || !strings.Contains(err.Error(), "cdn") {
			t.Errorf("log_type %s 应明确报错仅支持 cdn,got: %v", lt, err)
		}
	}
}

// TestCacheAnalyzeWindowLimits AC2:窗口上限 24h;>6h 预估超限默认拦截,
// confirm 后放行。
func TestCacheAnalyzeWindowLimits(t *testing.T) {
	t.Setenv(cacheAnalyzeFlagEnv, "1")
	svc, agg := newCacheService(t, 0)
	end := time.Now().UnixMilli()

	// 超 24h:硬性报错
	_, err := svc.CacheAnalyze(context.Background(), 3, CacheAnalyzeRequest{
		LogType: logquery.LogTypeCDN, StartTime: end - 25*3600_000, EndTime: end,
	})
	if err == nil || !strings.Contains(err.Error(), "24h") {
		t.Errorf("超 24h 窗口应报错, got: %v", err)
	}

	// 7h 未确认:预估拦截(报错含 confirm 提示)
	start7h := end - 7*3600_000
	_, err = svc.CacheAnalyze(context.Background(), 3, CacheAnalyzeRequest{
		LogType: logquery.LogTypeCDN, StartTime: start7h, EndTime: end,
	})
	if err == nil || !strings.Contains(err.Error(), "confirm") {
		t.Errorf("超限未确认应拦截并提示确认, got: %v", err)
	}
	if calls := len(agg.snapshot()); calls != 0 {
		t.Errorf("拦截发生在执行前,不应有聚合调用, got %d", calls)
	}

	// 7h + confirm:放行
	scriptHappyWindow(agg, start7h, end)
	resp, err := svc.CacheAnalyze(context.Background(), 3, CacheAnalyzeRequest{
		LogType: logquery.LogTypeCDN, StartTime: start7h, EndTime: end, Confirm: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Result == nil {
		t.Fatal("confirm 后应返回结果")
	}
}

// TestCacheAnalyzeHappyPath AC1/AC2/AC5:响应结构完整、≤2 帧窗口聚合
// (当前窗 5 维度组 + 前窗 1 帧)、口径数值精确可复现(golden 回放:全部
// 当前窗调用同一时间边界,前窗为 [start-span, start))。
func TestCacheAnalyzeHappyPath(t *testing.T) {
	t.Setenv(cacheAnalyzeFlagEnv, "1")
	svc, agg := newCacheService(t, 0)
	start, end := cacheWindow()
	span := end - start
	scriptHappyWindow(agg, start, end)

	resp, err := svc.CacheAnalyze(context.Background(), 3, CacheAnalyzeRequest{
		LogType: logquery.LogTypeCDN, StartTime: start, EndTime: end,
	})
	if err != nil {
		t.Fatal(err)
	}

	// ---- 窗口帧 golden 断言:当前窗 5 次维度聚合同窗 + 前窗 1 帧 ----
	calls := agg.snapshot()
	if len(calls) != 6 {
		t.Fatalf("聚合调用应 6 次(当前窗 5 + 前窗 1), got %d: %+v", len(calls), calls)
	}
	curDims := map[string]bool{}
	for _, c := range calls {
		switch {
		case c.Start == start && c.End == end:
			curDims[cacheAggKey(c.Start, c.End, c.Dimension, c.Metric)] = true
		case c.Start == start-span && c.End == start:
			if c.Dimension != "cache_hit" || c.Metric != "count" {
				t.Errorf("前窗应仅 1 帧 cache_hit 分布, got %s/%s", c.Dimension, c.Metric)
			}
		default:
			t.Errorf("越界窗口调用: %+v", c)
		}
	}
	for _, want := range []string{"cache_hit|count", "cache_hit|sum_bytes", "status|count", "host|nonhit_count", "url|nonhit_count"} {
		if !curDims[""+fmt.Sprintf("%d|%d|", start, end)+want] {
			t.Errorf("当前窗缺少维度组 %s, calls: %+v", want, calls)
		}
	}
	if resp.AggregateFrames != 2 {
		t.Errorf("aggregate_frames = %d, want 2(当前窗+前窗)", resp.AggregateFrames)
	}

	// ---- 双命中率口径(AC5 可复现:同一输入精确数值)----
	res := resp.Result
	if res == nil {
		t.Fatal("result = nil")
	}
	if resp.Total != 1000 {
		t.Errorf("total = %d, want 1000(聚合分桶精确总数)", resp.Total)
	}
	rr := res.RequestHitRate
	if !rr.All.Available || rr.All.Numerator != 750 || rr.All.Denominator != 1000 {
		t.Errorf("请求命中率全请求档 = %+v, want 750/1000", rr.All)
	}
	if !rr.Cacheable.Available || rr.Cacheable.Numerator != 750 || rr.Cacheable.Denominator != 850 {
		t.Errorf("请求命中率可缓存档 = %+v, want 750/850(剔除 max(error 30, 4xx+5xx 150))", rr.Cacheable)
	}
	br := res.ByteHitRate
	if !br.All.Available || br.All.Numerator != 7_200_000 || br.All.Denominator != 16_250_000 {
		t.Errorf("字节命中率全请求档 = %+v, want 7.2M/16.25M", br.All)
	}
	if !br.Cacheable.Available || br.Cacheable.Denominator != 16_220_000 {
		t.Errorf("字节命中率可缓存档 = %+v, want 分母 16.22M(剔除 error 字节)", br.Cacheable)
	}
	if resp.TotalBytes != 16_250_000 {
		t.Errorf("total_bytes = %d, want 16250000", resp.TotalBytes)
	}
	if res.Grade != cdncache.GradeFair {
		t.Errorf("grade = %s, want fair(0.882 ∈ [0.80,0.90))", res.Grade)
	}

	// ---- 域名命中率 Top 8(按未命中 Top 域名,双口径分列)----
	if len(res.DomainRanking) != 2 {
		t.Fatalf("domain_ranking = %d 条, want 2", len(res.DomainRanking))
	}
	a := res.DomainRanking[0]
	if a.Host != "a.example.com" || a.Requests != 600 || a.HitRate != 0.7 || a.MissTrafficRatio != 180.0/230.0 {
		t.Errorf("域名榜首 = %+v, want a.example.com 600 req rate 0.7 miss 180/230", a)
	}

	// ---- 未命中 URI TOP(查询串归一 + 变体归并 + 归属域名)----
	if len(res.MissURITop) != 2 {
		t.Fatalf("miss_uri_top = %d 条, want 2: %+v", len(res.MissURITop), res.MissURITop)
	}
	uri1 := res.MissURITop[0]
	if uri1.URI != "/api/list" || uri1.MissCount != 220 || uri1.Variants != 2 || uri1.SampleQuery != "id=2" {
		t.Errorf("URI 榜首 = %+v, want /api/list miss 220 variants 2 sample id=2", uri1)
	}
	uri2 := res.MissURITop[1]
	if uri2.URI != "/download/pkg.tar.gz" || uri2.Host != "a.example.com" || uri2.MissCount != 10 {
		t.Errorf("URI 第二 = %+v, want /download/pkg.tar.gz 归属 a.example.com miss 10", uri2)
	}

	// ---- 状态码分布(4xx/5xx 占比)----
	if res.Status.ClientErrorCount != 100 || res.Status.ServerErrorCount != 50 ||
		res.Status.ClientErrorRatio != 0.1 || res.Status.ServerErrorRatio != 0.05 {
		t.Errorf("状态码分布 = %+v, want 4xx 100/10%%、5xx 50/5%%", res.Status)
	}

	// ---- 优化项(非优档生成,Top 5 上限,可信度分级)----
	if len(res.Recommendations) == 0 || len(res.Recommendations) > 5 {
		t.Errorf("优化项 = %d 条, want (0,5]", len(res.Recommendations))
	}

	// ---- 前窗趋势(下降 0.15 → down + 关注)----
	if !res.Prev.Available || res.Prev.Direction != cdncache.TrendDown || !res.Prev.Alert {
		t.Errorf("前窗趋势 = %+v, want available down alert(0.75-0.9)", res.Prev)
	}
	if resp.Prev == nil || resp.Prev.Total != 1000 {
		t.Errorf("前窗原始分布 = %+v, want total 1000", resp.Prev)
	}

	// ---- per-source 状态 / summary 占位 / 缓存标注 ----
	if len(resp.Sources) != 1 {
		t.Fatalf("sources = %d, want 1(单账号)", len(resp.Sources))
	}
	for _, oc := range resp.Sources {
		if oc.Error != "" {
			t.Errorf("源 %s 不应失败: %s", oc.AccountID, oc.Error)
		}
	}
	if !strings.Contains(resp.DimensionNotes, "已覆盖源") {
		t.Errorf("dimension_notes 应含覆盖源口径说明: %q", resp.DimensionNotes)
	}
	if resp.Summary != "" {
		t.Errorf("summary 应为占位空串(不引入 LLM), got %q", resp.Summary)
	}
	if resp.Cached {
		t.Error("首次调用不应命中缓存")
	}
}

// TestCacheAnalyzeURINormalization AC3:查询串归一(剥离/排序)后按路径聚合,
// 归属域名从完整 URL 解析,无主机形态置空。
func TestCacheAnalyzeURINormalization(t *testing.T) {
	for _, c := range []struct {
		raw      string
		okHost   string
		okSuffix string
	}{
		{"http://a.example.com/download/pkg.tar.gz?v=2", "a.example.com", "/download/pkg.tar.gz?v=2"},
		{"https://b.example.com/api/x?a=1&b=2", "b.example.com", "/api/x?a=1&b=2"},
		{"/api/list?id=2", "", "/api/list?id=2"},
	} {
		host, path := splitURLHost(c.raw)
		if host != c.okHost || !strings.HasSuffix(path, c.okSuffix) {
			t.Errorf("splitURLHost(%q) = (%q,%q), want (%q, *%q)", c.raw, host, path, c.okHost, c.okSuffix)
		}
	}
	// 无查询串完整 URL 不追加 "?"
	host, path := splitURLHost("http://a.example.com/p")
	if host != "a.example.com" || path != "/p" || strings.Contains(path, "?") {
		t.Errorf("splitURLHost 无查询串 = (%q,%q)", host, path)
	}
}

// TestCacheAnalyzeDegradedDimensions AC4:非主维度失败仅标注不整卡失败,
// 总览命中率仍由 cache_hit 分布计算。
func TestCacheAnalyzeDegradedDimensions(t *testing.T) {
	t.Setenv(cacheAnalyzeFlagEnv, "1")
	svc, agg := newCacheService(t, 0)
	start, end := cacheWindow()
	agg.results[cacheAggKey(start, end, "cache_hit", "count")] = diagResult(
		[]logquery.TopNItem{cacheItem("TCP_HIT", 700, 700), cacheItem("TCP_MISS", 300, 300)},
		700, 300)
	agg.errs[cacheAggKey(start, end, "cache_hit", "sum_bytes")] = errors.New("bytes boom")
	agg.errs[cacheAggKey(start, end, "status", "count")] = errors.New("status boom")
	agg.errs[cacheAggKey(start, end, "host", "nonhit_count")] = errors.New("host boom")
	agg.errs[cacheAggKey(start, end, "url", "nonhit_count")] = errors.New("url boom")

	resp, err := svc.CacheAnalyze(context.Background(), 3, CacheAnalyzeRequest{
		LogType: logquery.LogTypeCDN, StartTime: start, EndTime: end,
	})
	if err != nil {
		t.Fatalf("维度失败不应整卡失败: %v", err)
	}
	res := resp.Result
	if !res.RequestHitRate.All.Available || res.RequestHitRate.All.Numerator != 700 {
		t.Errorf("总览命中率仍应可算 = %+v", res.RequestHitRate.All)
	}
	if len(res.DomainRanking) != 0 {
		t.Errorf("host 维度失败域名排行应降级为空, got %+v", res.DomainRanking)
	}
	if len(res.MissURITop) != 0 {
		t.Errorf("url 维度失败 URI TOP 应降级为空, got %+v", res.MissURITop)
	}
	if res.ByteHitRate.All.Available {
		t.Errorf("字节口径缺失应标记不可用 = %+v", res.ByteHitRate.All)
	}
	for _, want := range []string{"sum_bytes", "status", "host", "url"} {
		if !strings.Contains(resp.DimensionNotes, want) {
			t.Errorf("dimension_notes 应含 %s 失败说明: %q", want, resp.DimensionNotes)
		}
	}
}

// TestCacheAnalyzePrevWindowDegrade AC4:前窗失败/无数据均降级为当前窗
// 绝对量判定,不报错。
func TestCacheAnalyzePrevWindowDegrade(t *testing.T) {
	t.Setenv(cacheAnalyzeFlagEnv, "1")
	start, end := cacheWindow()

	// 前窗聚合失败
	svc, agg := newCacheService(t, 0)
	scriptHappyWindow(agg, start, end)
	agg.errs[cacheAggKey(start-(end-start), start, "cache_hit", "count")] = errors.New("prev boom")
	resp, err := svc.CacheAnalyze(context.Background(), 3, CacheAnalyzeRequest{
		LogType: logquery.LogTypeCDN, StartTime: start, EndTime: end,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.PrevError == "" {
		t.Error("前窗失败应透出 prev_error")
	}
	if resp.Result.Prev.Available {
		t.Error("前窗失败趋势应不可用")
	}

	// 前窗确实无数据(源正常,窗口为空)
	svc2, agg2 := newCacheService(t, 0)
	scriptHappyCurrent(agg2, start, end)
	resp2, err := svc2.CacheAnalyze(context.Background(), 3, CacheAnalyzeRequest{
		LogType: logquery.LogTypeCDN, StartTime: start, EndTime: end,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp2.PrevError != "" {
		t.Errorf("前窗无数据不应报错, got %q", resp2.PrevError)
	}
	if resp2.Prev == nil || resp2.Prev.Total != 0 {
		t.Errorf("前窗无数据应保留空前窗, got %+v", resp2.Prev)
	}
	if resp2.Result.Prev.Available {
		t.Error("前窗无数据趋势应不可用")
	}
}

// TestCacheAnalyzeSourceFailure AC4/AC1:单源聚合失败 → per-source 状态标注,
// 命中率按已覆盖源计算并标注覆盖范围,不白屏。
func TestCacheAnalyzeSourceFailure(t *testing.T) {
	t.Setenv(cacheAnalyzeFlagEnv, "1")
	svc, agg := newCacheServiceMulti(t, 2) // 账号 2 恒失败
	start, end := cacheWindow()
	scriptHappyWindow(agg, start, end)

	resp, err := svc.CacheAnalyze(context.Background(), 3, CacheAnalyzeRequest{
		LogType: logquery.LogTypeCDN, StartTime: start, EndTime: end,
	})
	if err != nil {
		t.Fatal(err)
	}
	var failed bool
	for _, oc := range resp.Sources {
		if oc.Error != "" {
			failed = true
		}
	}
	if !failed {
		t.Error("失败账号应在 per-source 状态标注")
	}
	if !strings.Contains(resp.DimensionNotes, "1/2") {
		t.Errorf("dimension_notes 应标注覆盖 1/2 源: %q", resp.DimensionNotes)
	}
	if resp.Result.RequestHitRate.All.Denominator != 1000 {
		t.Errorf("命中率应按已覆盖源计算 = %+v", resp.Result.RequestHitRate.All)
	}
}

// TestCacheAnalyzeCachedReproducible AC5:同参复用诊断级 SWR 缓存,
// 同一时间边界 + 同一执行批次结果可复现。
func TestCacheAnalyzeCachedReproducible(t *testing.T) {
	t.Setenv(cacheAnalyzeFlagEnv, "1")
	svc, agg := newCacheService(t, 0)
	start, end := cacheWindow()
	scriptHappyWindow(agg, start, end)
	req := CacheAnalyzeRequest{LogType: logquery.LogTypeCDN, StartTime: start, EndTime: end}

	first, err := svc.CacheAnalyze(context.Background(), 3, req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Cached {
		t.Error("首次调用不应命中缓存")
	}
	second, err := svc.CacheAnalyze(context.Background(), 3, req)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Cached {
		t.Error("同参第二次调用应命中缓存")
	}
	if second.Result.RequestHitRate.All.Rate != first.Result.RequestHitRate.All.Rate {
		t.Error("同参结果应可复现(命中率一致)")
	}
}

// TestNormalizeCacheHitDist AC5/硬规则:聚合组键(源原始值)经单一映射源
// logquery.NormalizeCacheHit 归一并同态合并,不另起归一路径。
func TestNormalizeCacheHitDist(t *testing.T) {
	in := []logquery.TopNItem{
		cacheItem("TCP_HIT", 100, 100),
		cacheItem("HIT", 50, 50),
		cacheItem("TCP_MISS", 30, 30),
		cacheItem("ERROR_X", 5, 5),
		cacheItem("-", 15, 15),
	}
	out := normalizeCacheHitDist(in)
	got := map[string]logquery.TopNItem{}
	for _, it := range out {
		got[it.Name] = it
	}
	if got["hit"].Count != 150 || got["miss"].Count != 30 || got["error"].Count != 5 || got["-"].Count != 15 {
		t.Errorf("归一合并结果错误: %+v", out)
	}
	if len(out) != 4 {
		t.Errorf("归一后应 4 组, got %d: %+v", len(out), out)
	}
}

// TestCacheAnalyzeInputValidation 入参校验分支(时间窗/筛选器合法性)。
func TestCacheAnalyzeInputValidation(t *testing.T) {
	t.Setenv(cacheAnalyzeFlagEnv, "1")
	svc, _ := newCacheService(t, 0)
	end := time.Now().UnixMilli()

	// 逆序窗口
	if _, err := svc.CacheAnalyze(context.Background(), 3, CacheAnalyzeRequest{
		LogType: logquery.LogTypeCDN, StartTime: end, EndTime: end,
	}); err == nil || !strings.Contains(err.Error(), "invalid time window") {
		t.Errorf("逆序窗口应报错, got: %v", err)
	}
	// 非法筛选 op
	if _, err := svc.CacheAnalyze(context.Background(), 3, CacheAnalyzeRequest{
		LogType: logquery.LogTypeCDN, StartTime: end - 600_000, EndTime: end,
		Filters: []logquery.FieldFilter{{Field: "host", Op: "hack", Value: "x"}},
	}); err == nil || !strings.Contains(err.Error(), "invalid filter op") {
		t.Errorf("非法筛选 op 应报错, got: %v", err)
	}
	// 缺失筛选值
	if _, err := svc.CacheAnalyze(context.Background(), 3, CacheAnalyzeRequest{
		LogType: logquery.LogTypeCDN, StartTime: end - 600_000, EndTime: end,
		Filters: []logquery.FieldFilter{{Field: "host", Op: "eq"}},
	}); err == nil || !strings.Contains(err.Error(), "incomplete field filter") {
		t.Errorf("缺失筛选值应报错, got: %v", err)
	}
}

// TestCacheAnalyzeParseHelpers 聚合结果 → 引擎输入的解析辅助(截断/越界/
// 空值防御,保证脏数据不破坏口径)。
func TestCacheAnalyzeParseHelpers(t *testing.T) {
	// host:Value 越界钳制与空名跳过
	stats := hostStatsFromMissTop([]logquery.TopNItem{
		cacheItem("", 100, 50),    // 空域名跳过
		cacheItem("h1", 0, 0),     // 零请求跳过
		cacheItem("h2", 100, -5),  // 负未命中钳 0
		cacheItem("h3", 100, 999), // 未命中超请求钳至请求数
		cacheItem("h4", 100, 30),  // 正常
	})
	if len(stats) != 3 {
		t.Fatalf("hostStats = %d 条, want 3(空名/零请求跳过)", len(stats))
	}
	if stats[0].Host != "h2" || stats[0].Hit != 100 || stats[0].Miss != 0 {
		t.Errorf("负值钳制错误: %+v", stats[0])
	}
	if stats[1].Host != "h3" || stats[1].Miss != 100 || stats[1].Hit != 0 {
		t.Errorf("越界钳制错误: %+v", stats[1])
	}
	if stats[2].Host != "h4" || stats[2].Hit != 70 || stats[2].Miss != 30 {
		t.Errorf("正常解析错误: %+v", stats[2])
	}

	// uri:零/负未命中跳过
	items := uriMissesFromMissTop([]logquery.TopNItem{
		cacheItem("/a", 100, 0),
		cacheItem("/b", 100, -1),
		cacheItem("", 100, 50),
		cacheItem("/c", 100, 50),
	})
	if len(items) != 1 || items[0].URI != "/c" || items[0].Miss != 50 {
		t.Errorf("uriMisses 解析错误: %+v", items)
	}

	// splitURLHost:空串与解析失败形态
	if host, path := splitURLHost(""); host != "" || path != "/" {
		t.Errorf("splitURLHost(\"\") = (%q,%q), want (\"\",/)", host, path)
	}
	if host, path := splitURLHost("   "); host != "" || path != "/" {
		t.Errorf("splitURLHost(空白) = (%q,%q), want (\"\",/)", host, path)
	}

	// 维度说明:nil 响应分支(聚合无结果)。
	notes := cacheDimensionNotes(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if len(notes) == 0 {
		t.Fatal("nil 响应应产出缺失说明")
	}
}
