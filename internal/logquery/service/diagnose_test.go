package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/logquery/diagnose"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// ---------------------------------------------------------------------
// 测试桩:按 (窗口, 维度) 编程的聚合 provider
// ---------------------------------------------------------------------

// diagCall 记录一次 provider 聚合调用(帧数/窗口/维度断言用)。
type diagCall struct {
	Start, End int64
	Dimension  string
}

func diagKey(start, end int64, dim string) string {
	return fmt.Sprintf("%d|%d|%s", start, end, dim)
}

// scriptedAggregator 可编程聚合桩:结果/错误按 "start|end|dim" 键注入;
// 缺键返回空结果(窗口无数据)。并发安全(诊断多维并发调用)。
var dbg = false

// scriptedAggregator 可编程聚合桩:结果/错误按 "start|end|dim" 键注入;
// 缺键返回空结果(窗口无数据)。并发安全(诊断多维并发调用)。
type scriptedAggregator struct {
	fakeProvider
	mu      sync.Mutex
	calls   []diagCall
	results map[string]logquery.AggregateResult
	errs    map[string]error
}

func (p *scriptedAggregator) Aggregate(_ context.Context, _ *domain.CloudAccount, params logquery.AggregateParams) (*logquery.AggregateResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, diagCall{Start: params.StartTime, End: params.EndTime, Dimension: params.Dimension})
	key := diagKey(params.StartTime, params.EndTime, params.Dimension)
	if err, ok := p.errs[key]; ok {
		return nil, err
	}
	if r, ok := p.results[key]; ok {
		cp := r
		return &cp, nil
	}
	return &logquery.AggregateResult{}, nil
}

func (p *scriptedAggregator) snapshot() []diagCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]diagCall(nil), p.calls...)
}

// errAggregator 恒失败聚合桩(源失败隔离用)。
type errAggregator struct{ err error }

func (p *errAggregator) Cloud() domain.CloudProvider { return testCloud }
func (p *errAggregator) LogType() logquery.LogType   { return logquery.LogTypeWAF }
func (p *errAggregator) ListLogSources(context.Context, *domain.CloudAccount) ([]logquery.LogSource, error) {
	return nil, nil
}
func (p *errAggregator) Search(context.Context, *domain.CloudAccount, logquery.SearchParams) ([]logquery.LogEntry, error) {
	return nil, nil
}
func (p *errAggregator) Aggregate(context.Context, *domain.CloudAccount, logquery.AggregateParams) (*logquery.AggregateResult, error) {
	return nil, p.err
}

// topItem TopN 条目(count 指标 Value=Count,对齐真实 provider 语义)。
func topItem(name string, count int64) logquery.TopNItem {
	return logquery.TopNItem{Name: name, Count: count, Value: float64(count)}
}

// diagResult 编程一份聚合结果(Total 由分桶求和,与联邦语义一致)。
func diagResult(topn []logquery.TopNItem, bucketCounts ...int64) logquery.AggregateResult {
	r := logquery.AggregateResult{TopN: topn}
	for i, c := range bucketCounts {
		r.Buckets = append(r.Buckets, logquery.AggregateBucket{Timestamp: int64(i+1) * 60, Count: c})
	}
	return r
}

// registerDiag 注册 WAF 诊断测试 provider;failAccountID>0 时该账号恒失败。
func registerDiag(t *testing.T, agg *scriptedAggregator, failAccountID int64) {
	t.Helper()
	logquery.RegisterProvider(testCloud, logquery.LogTypeWAF, func(acc *domain.CloudAccount) (logquery.LogProvider, error) {
		if failAccountID > 0 && acc.ID == failAccountID {
			return &errAggregator{err: errors.New("boom")}, nil
		}
		return agg, nil
	})
}

// diagWindow 标准 10 分钟诊断窗。
func diagWindow() (start, end int64) {
	const span = int64(600_000)
	end = time.Now().UnixMilli()
	return end - span, end
}

// ---------------------------------------------------------------------
// 用例
// ---------------------------------------------------------------------

// TestDiagnoseHappyPath 当前窗 4 维 + 前窗 1 帧:判定/对比/标注齐全。
// AC1(响应结构)/AC2(前窗对比与突增倍数)/AC4(≤2 窗口帧)。
func TestDiagnoseHappyPath(t *testing.T) {
	agg := &scriptedAggregator{results: map[string]logquery.AggregateResult{}, errs: map[string]error{}}
	registerDiag(t, agg, 0)
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, testCloud),
	}}, nil)
	start, end := diagWindow()
	span := end - start
	agg.results[diagKey(start, end, "client_ip")] = diagResult(
		[]logquery.TopNItem{topItem("1.2.3.4", 800), topItem("5.6.7.8", 200)}, 400, 600)
	agg.results[diagKey(start, end, "user_agent")] = diagResult(
		[]logquery.TopNItem{topItem("curl/8.0", 850), topItem("Mozilla/5.0 Chrome", 150)}, 400, 600)
	agg.results[diagKey(start, end, "uri")] = diagResult(
		[]logquery.TopNItem{topItem("/api/login", 600), topItem("/admin", 400)}, 400, 600)
	agg.results[diagKey(start, end, "status")] = diagResult(
		[]logquery.TopNItem{topItem("404", 700), topItem("200", 300)}, 400, 600)
	agg.results[diagKey(start, end, "action")] = diagResult(
		[]logquery.TopNItem{topItem("block", 300), topItem("none", 700)}, 400, 600)
	agg.results[diagKey(start-span, start, "client_ip")] = diagResult(
		[]logquery.TopNItem{topItem("1.2.3.4", 80)}, 40, 60)

	resp, err := svc.Diagnose(context.Background(), 3, DiagnoseRequest{
		LogType: logquery.LogTypeWAF, StartTime: start, EndTime: end,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1000 || resp.WindowSec != span/1000 {
		t.Errorf("total/window = %d/%d, want 1000/%d", resp.Total, resp.WindowSec, span/1000)
	}
	if len(resp.TopIPs) != 2 || len(resp.TopUAs) != 2 || len(resp.TopURIs) != 2 ||
		len(resp.StatusCodes) != 2 || len(resp.Actions) != 2 || len(resp.Buckets) != 2 {
		t.Errorf("dimension details incomplete: %+v", resp)
	}
	if resp.TopURIs[0].Name == "" || resp.TopURIs[0].Count <= 0 {
		t.Errorf("top_uris first entry invalid: %+v", resp.TopURIs)
	}
	res := resp.Result
	if res == nil {
		t.Fatal("result = nil")
	}
	if len(res.TopSources) != 2 || res.TopSources[0].IP != "1.2.3.4" ||
		res.TopSources[0].Count != 800 || res.TopSources[0].Share != 0.8 {
		t.Errorf("top_sources = %+v, want 1.2.3.4/800/0.8", res.TopSources)
	}
	if len(res.Measures) < 3 {
		t.Errorf("measures = %d, want >=3", len(res.Measures))
	}
	if res.AttackType != diagnose.AttackTypeCrawler {
		t.Errorf("attack_type = %s, want %s(curl UA 高占比 + 4xx 高)", res.AttackType, diagnose.AttackTypeCrawler)
	}
	// 前窗对比:Total 倍数 1000/100=10,Top IP 倍数 800/80=10。
	if resp.Prev == nil || resp.Prev.Total != 100 || resp.Prev.TopIPCount != 80 {
		t.Errorf("prev = %+v, want total=100 topIP=80", resp.Prev)
	}
	if res.SurgeMultiplier != 10 {
		t.Errorf("surge = %v, want 10", res.SurgeMultiplier)
	}
	if res.Degraded {
		t.Errorf("degraded = true, want false(前窗/窗口均可用): %s", res.DegradedReason)
	}
	if resp.Summary != "" {
		t.Errorf("summary = %q, want 空串(AI 解读后置)", resp.Summary)
	}
	if resp.AggregateFrames != 2 {
		t.Errorf("frames = %d, want 2", resp.AggregateFrames)
	}
	if len(resp.Sources) != 1 || resp.Sources[0].Error != "" {
		t.Errorf("sources = %+v, want 1 个正常源", resp.Sources)
	}
	if len(resp.PrevSources) != 1 {
		t.Errorf("prev_sources = %+v, want 1 个", resp.PrevSources)
	}
	// 帧数:当前窗 5 维 + 前窗 1 帧(client_ip);前窗恰一次且窗口等长前移。
	calls := agg.snapshot()
	if len(calls) != 6 {
		t.Fatalf("aggregate frames = %d, want 6(当前窗 5 维 + 前窗 1 帧)", len(calls))
	}
	prevFrames := 0
	dims := map[string]bool{}
	for _, c := range calls {
		if c.Start == start-span && c.End == start {
			if c.Dimension != "client_ip" {
				t.Errorf("prev frame dimension = %s, want client_ip", c.Dimension)
			}
			prevFrames++
			continue
		}
		if c.Start != start || c.End != end {
			t.Errorf("unexpected window frame [%d,%d)", c.Start, c.End)
		}
		dims[c.Dimension] = true
	}
	if prevFrames != 1 {
		t.Errorf("prev frames = %d, want 1(前窗仅 1 帧)", prevFrames)
	}
	for _, want := range []string{"client_ip", "user_agent", "uri", "status", "action"} {
		if !dims[want] {
			t.Errorf("current window missing dimension %s", want)
		}
	}
}

// TestDiagnoseRejectsNonWAF 诊断仅 WAF 开放(SLB/CDN 明确报错)。Hard Rules
func TestDiagnoseRejectsNonWAF(t *testing.T) {
	svc := NewFederationService(&fakeAccountSource{}, nil)
	now := time.Now().UnixMilli()
	for _, lt := range []logquery.LogType{logquery.LogTypeCDN, logquery.LogTypeSLB} {
		_, err := svc.Diagnose(context.Background(), 3, DiagnoseRequest{LogType: lt, StartTime: now - 1, EndTime: now})
		if err == nil || !strings.Contains(err.Error(), "only supports waf") {
			t.Errorf("%s: err = %v, want waf-only error", lt, err)
		}
	}
	if _, err := svc.Diagnose(context.Background(), 3, DiagnoseRequest{
		LogType: logquery.LogTypeWAF, StartTime: now, EndTime: now,
	}); err == nil {
		t.Error("empty window should fail")
	}
}

// TestDiagnoseInvalidFilters 结构化筛选校验前置(与 Aggregate 同规则,失败快返回)。
func TestDiagnoseInvalidFilters(t *testing.T) {
	svc := NewFederationService(&fakeAccountSource{}, nil)
	now := time.Now().UnixMilli()
	req := func(f logquery.FieldFilter) DiagnoseRequest {
		return DiagnoseRequest{LogType: logquery.LogTypeWAF, StartTime: now - 1, EndTime: now,
			Filters: []logquery.FieldFilter{f}}
	}
	if _, err := svc.Diagnose(context.Background(), 3, req(logquery.FieldFilter{Op: "bogus", Field: "status", Value: "404"})); err == nil {
		t.Error("invalid filter op should fail")
	}
	if _, err := svc.Diagnose(context.Background(), 3, req(logquery.FieldFilter{Op: "eq", Field: "status", Value: ""})); err == nil {
		t.Error("incomplete filter should fail")
	}
}

// TestDiagnosePrevEmptyDegrades 前窗无数据(扫描成功但 Total=0):降级为
// 当前窗绝对量判定,不报错。AC2
func TestDiagnosePrevEmptyDegrades(t *testing.T) {
	agg := &scriptedAggregator{results: map[string]logquery.AggregateResult{}, errs: map[string]error{}}
	registerDiag(t, agg, 0)
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, testCloud),
	}}, nil)
	start, end := diagWindow()
	agg.results[diagKey(start, end, "client_ip")] = diagResult(
		[]logquery.TopNItem{topItem("1.2.3.4", 300)}, 200, 300)

	resp, err := svc.Diagnose(context.Background(), 3, DiagnoseRequest{
		LogType: logquery.LogTypeWAF, StartTime: start, EndTime: end,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Result.Degraded || !strings.Contains(resp.Result.DegradedReason, "前窗") {
		t.Errorf("degraded = %v/%q, want 前窗降级标注", resp.Result.Degraded, resp.Result.DegradedReason)
	}
	if resp.Prev == nil || resp.Prev.Total != 0 || resp.PrevError != "" {
		t.Errorf("prev = %+v error = %q, want 空前窗且无错误", resp.Prev, resp.PrevError)
	}
	if resp.Result.SurgeMultiplier != 0 {
		t.Errorf("surge = %v, want 0", resp.Result.SurgeMultiplier)
	}
	if resp.Total != 500 {
		t.Errorf("total = %d, want 500(当前窗判定不受影响)", resp.Total)
	}
}

// TestDiagnosePrevFailureDegrades 前窗聚合整体失败:降级不报错,原因透出。AC2/AC3
func TestDiagnosePrevFailureDegrades(t *testing.T) {
	agg := &scriptedAggregator{results: map[string]logquery.AggregateResult{}, errs: map[string]error{}}
	registerDiag(t, agg, 0)
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, testCloud),
	}}, nil)
	start, end := diagWindow()
	agg.results[diagKey(start, end, "client_ip")] = diagResult(
		[]logquery.TopNItem{topItem("1.2.3.4", 300)}, 500)
	agg.errs[diagKey(start-(end-start), start, "client_ip")] = errors.New("sls quota exceeded")

	resp, err := svc.Diagnose(context.Background(), 3, DiagnoseRequest{
		LogType: logquery.LogTypeWAF, StartTime: start, EndTime: end,
	})
	if err != nil {
		t.Fatalf("prev failure must not error the diagnose: %v", err)
	}
	if resp.Prev != nil {
		t.Errorf("prev = %+v, want nil(前窗失败)", resp.Prev)
	}
	if !strings.Contains(resp.PrevError, "quota") {
		t.Errorf("prev_error = %q, want 失败原因", resp.PrevError)
	}
	if !resp.Result.Degraded {
		t.Error("degraded = false, want true(前窗失败降级)")
	}
	if resp.Total != 500 {
		t.Errorf("total = %d, want 500(当前窗照常判定)", resp.Total)
	}
}

// TestDiagnoseSourceFailureIsolated 单源失败:per-source 标注,正常源照常判定。AC3
func TestDiagnoseSourceFailureIsolated(t *testing.T) {
	agg := &scriptedAggregator{results: map[string]logquery.AggregateResult{}, errs: map[string]error{}}
	registerDiag(t, agg, 2) // 账号 2 恒失败
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, testCloud), testAccount(2, testCloud),
	}}, nil)
	start, end := diagWindow()
	agg.results[diagKey(start, end, "client_ip")] = diagResult(
		[]logquery.TopNItem{topItem("1.2.3.4", 300)}, 500)

	resp, err := svc.Diagnose(context.Background(), 3, DiagnoseRequest{
		LogType: logquery.LogTypeWAF, StartTime: start, EndTime: end,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(resp.Sources))
	}
	var okSource, failed bool
	for _, s := range resp.Sources {
		if s.Error == "" {
			okSource = s.AccountID == "1"
		} else if strings.Contains(s.Error, "boom") {
			failed = s.AccountID == "2"
		}
	}
	if !okSource || !failed {
		t.Errorf("per-source status wrong: %+v", resp.Sources)
	}
	if resp.Total != 500 {
		t.Errorf("total = %d, want 500(仅正常源计入)", resp.Total)
	}
	if resp.Result == nil || len(resp.Result.TopSources) == 0 {
		t.Error("正常源应照常判定")
	}
}

// TestDiagnoseDimensionFailureNotes 非主维度失败:记入说明,判定照常(该维度
// 判据退化)。AC3
func TestDiagnoseDimensionFailureNotes(t *testing.T) {
	agg := &scriptedAggregator{results: map[string]logquery.AggregateResult{}, errs: map[string]error{}}
	registerDiag(t, agg, 0)
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, testCloud),
	}}, nil)
	start, end := diagWindow()
	agg.results[diagKey(start, end, "client_ip")] = diagResult(
		[]logquery.TopNItem{topItem("1.2.3.4", 300)}, 500)
	agg.errs[diagKey(start, end, "user_agent")] = errors.New("ua index missing")

	resp, err := svc.Diagnose(context.Background(), 3, DiagnoseRequest{
		LogType: logquery.LogTypeWAF, StartTime: start, EndTime: end,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.DimensionNotes, "user_agent") {
		t.Errorf("dimension_notes = %q, want user_agent 缺失说明", resp.DimensionNotes)
	}
	if resp.Total != 500 || resp.Result == nil {
		t.Error("主维度失败以外不应影响判定")
	}
}

// TestDiagnoseCached 同参重复诊断命中诊断级缓存,不重放聚合帧。AC4
func TestDiagnoseCached(t *testing.T) {
	agg := &scriptedAggregator{results: map[string]logquery.AggregateResult{}, errs: map[string]error{}}
	registerDiag(t, agg, 0)
	svc := NewFederationService(&fakeAccountSource{accounts: []domain.CloudAccount{
		testAccount(1, testCloud),
	}}, nil)
	start, end := diagWindow()
	req := DiagnoseRequest{LogType: logquery.LogTypeWAF, StartTime: start, EndTime: end}
	first, err := svc.Diagnose(context.Background(), 3, req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Cached {
		t.Error("first call should not be cached")
	}
	second, err := svc.Diagnose(context.Background(), 3, req)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Cached || second.CacheStale {
		t.Errorf("second call cached/stale = %v/%v, want true/false", second.Cached, second.CacheStale)
	}
	if len(agg.snapshot()) != 6 {
		t.Errorf("aggregate frames after 2 calls = %d, want 6(命中缓存不重放)", len(agg.snapshot()))
	}
}
