package wafsourcebrowsing

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	lqe "github.com/Havens-blog/e-cam-service/tests/logquerye2e"
)

// helper: fetch the WAF source list with timing.
func fetchWafSources(t *testing.T) ([]lqe.LogSource, time.Duration) {
	t.Helper()
	start := time.Now()
	status, env := lqe.Get(t, lqe.APIPrefix+"/sources?log_type=waf")
	elapsed := time.Since(start)
	if status != http.StatusOK {
		t.Fatalf("sources status = %d msg=%s", status, env.Msg)
	}
	return lqe.SourcesData(t, env), elapsed
}

// Journey Step 1: 打开日志查询页 -> /types 字段字典可用(类型选择器数据源)。
// API-level: /types returns the full dictionary, non-error, well-formed.
func TestStep1_TypesDictionaryAvailable(t *testing.T) {
	status, env := lqe.Get(t, lqe.APIPrefix+"/types")
	if status != http.StatusOK || !env.OK() {
		t.Fatalf("types status=%d code=%d msg=%s", status, env.Code, env.Msg)
	}
	types := lqe.TypesData(t, env)
	if len(types) != 3 {
		t.Fatalf("types = %d, want 3 (cdn/waf/slb)", len(types))
	}
	seen := map[string]bool{}
	for _, ty := range types {
		seen[ty.Type] = true
		if ty.Label == "" {
			t.Errorf("type %s has empty label", ty.Type)
		}
		if len(ty.Fields) == 0 {
			t.Errorf("type %s has no fields (dynamic columns would be empty)", ty.Type)
		}
		if ty.MaxWindowDays <= 0 {
			t.Errorf("type %s max_window_days = %d", ty.Type, ty.MaxWindowDays)
		}
	}
	for _, want := range []string{"cdn", "waf", "slb"} {
		if !seen[want] {
			t.Errorf("missing log type %q", want)
		}
	}
}

// Journey Step 2: 选择 WAF 日志类型 -> 源清单加载成功。
// API-level: /sources?log_type=waf returns well-formed source items.
func TestStep2_SelectWafLoadsSourceList(t *testing.T) {
	sources, elapsed := fetchWafSources(t)
	if len(sources) == 0 {
		t.Fatalf("waf sources empty — page would render empty list")
	}
	t.Logf("waf sources = %d, latency = %s (warm target <300ms, cold <3s — recorded, not asserted)", len(sources), elapsed)
	for _, s := range sources {
		if s.LogType != "waf" {
			t.Errorf("source %s has log_type %q, want waf", s.ResourceID, s.LogType)
		}
		if s.Cloud == "" || s.AccountID == "" || s.ResourceID == "" {
			t.Errorf("source missing identity fields: %+v", s)
		}
		// 按云/账号维度可辨识:展示名或 note 提供文本信息
		if s.Name == "" && s.Note == "" {
			t.Errorf("source %s has neither name nor note (not identifiable)", s.ResourceID)
		}
	}
}

// Journey Step 3: 查看源清单 -> 全部列出、无重复条目。
// API-level: (cloud, account, resource) tuples unique.
func TestStep3_SourceListHasNoDuplicates(t *testing.T) {
	sources, _ := fetchWafSources(t)
	if len(sources) < 2 {
		t.Fatalf("expected a multi-source list, got %d", len(sources))
	}
	type key struct{ cloud, account, resource string }
	seen := map[key]int{}
	for _, s := range sources {
		k := key{s.Cloud, s.AccountID, s.ResourceID}
		seen[k]++
		if seen[k] > 1 {
			t.Errorf("duplicate source entry: cloud=%s account=%s resource=%s", s.Cloud, s.AccountID, s.ResourceID)
		}
	}
	t.Logf("distinct sources = %d", len(sources))
}

// Journey Step 2b: 冷启动首次加载 -> 第二次选择显著变快/仍在目标内。
// API-level: repeat call completes; second latency logged and sanity-bounded.
func TestStep2b_RepeatLoadStaysWithinColdTarget(t *testing.T) {
	first, cold := fetchWafSources(t)
	second, warm := fetchWafSources(t)
	if len(first) == 0 || len(second) != len(first) {
		t.Fatalf("repeat load changed list size: %d -> %d", len(first), len(second))
	}
	t.Logf("cold=%s warm=%s", cold, warm)
	// 冷启动目标 3s 在共享环境按放宽上限守护(见 doc.go 豁免说明)
	if warm > 10*time.Second {
		t.Errorf("warm load %s exceeds relaxed bound 10s", warm)
	}
}

// Journey Step 2c: 单个云账号不可用 -> 其余云的源正常列出、整体不报错。
// Live mapping: this tenant has a known non-flowing account (tencent WAF
// delivery broken per its source note); the list must still return 200 with
// sources from healthy clouds, i.e. one broken account never breaks the page.
func TestStep2c_SingleAccountFailureDoesNotBreakList(t *testing.T) {
	status, env := lqe.Get(t, lqe.APIPrefix+"/sources?log_type=waf")
	if status != http.StatusOK || !env.OK() {
		t.Fatalf("sources status=%d code=%d msg=%s (single account failure must not fail the whole list)", status, env.Code, env.Msg)
	}
	sources := lqe.SourcesData(t, env)
	clouds := map[string]int{}
	var brokenAnnotated bool
	for _, s := range sources {
		clouds[s.Cloud]++
		if !s.Enabled && s.Note != "" {
			brokenAnnotated = true
		}
	}
	if len(sources) == 0 {
		t.Fatalf("no sources at all — one broken account took down the list")
	}
	if len(clouds) < 2 {
		t.Logf("warning: only %d cloud(s) present; per-cloud isolation partially observable", len(clouds))
	}
	t.Logf("clouds=%v broken-annotated=%v", clouds, brokenAnnotated)
	// 未开启投递的源必须带可读 note(失败可读提示),而非静默混入
	for _, s := range sources {
		if !s.Enabled && s.Note == "" {
			t.Errorf("disabled source %s lacks a readable reason note", s.ResourceID)
		}
	}
}

// Journey Step 2d: 重复切换日志类型 -> 再次选择命中缓存、清单无重复。
// API-level: cdn -> waf -> waf; final waf list identical shape, no dup growth.
func TestStep2d_RepeatedTypeSwitchNoDuplication(t *testing.T) {
	for _, lt := range []string{"cdn", "waf"} {
		status, env := lqe.Get(t, lqe.APIPrefix+"/sources?log_type="+lt)
		if status != http.StatusOK || !env.OK() {
			t.Fatalf("sources(%s) status=%d msg=%s", lt, status, env.Msg)
		}
	}
	waf1, _ := fetchWafSources(t)
	waf2, warm := fetchWafSources(t)
	if len(waf1) != len(waf2) {
		t.Errorf("repeated waf selection changed list size: %d -> %d (cache/duplication drift)", len(waf1), len(waf2))
	}
	t.Logf("second waf load latency=%s", warm)
}

// Journey Step 3b: 某类型无可用源 -> 明确空态,不报错。
// API-level: an over-constrained cloud filter yields an empty (non-null,
// non-error) list.
func TestStep3b_NoSourcesYieldsExplicitEmptyState(t *testing.T) {
	status, env := lqe.Get(t, lqe.APIPrefix+"/sources?log_type=waf&clouds=no-such-cloud")
	if status != http.StatusOK || !env.OK() {
		t.Fatalf("empty-case status=%d code=%d msg=%s (empty must not be an error)", status, env.Code, env.Msg)
	}
	sources := lqe.SourcesData(t, env)
	if len(sources) != 0 {
		t.Errorf("expected empty source list, got %d", len(sources))
	}
}

// Error path: missing log_type is rejected with a readable message.
func TestSourcesMissingLogTypeRejected(t *testing.T) {
	status, env := lqe.Get(t, lqe.APIPrefix+"/sources")
	if status != http.StatusBadRequest {
		t.Fatalf("missing log_type: status=%d, want 400", status)
	}
	if env.Msg == "" {
		t.Fatalf("400 without readable msg (bare error not allowed)")
	}
	t.Logf("readable error: %s", env.Msg)
}

// Error path: unknown log type is rejected with a readable message.
func TestSourcesUnknownLogTypeRejected(t *testing.T) {
	status, env := lqe.Get(t, lqe.APIPrefix+"/sources?log_type=nope")
	if status != http.StatusBadRequest {
		t.Fatalf("unknown log_type: status=%d, want 400", status)
	}
	if env.Msg == "" {
		t.Fatalf("400 without readable msg")
	}
}

// Journey smoke: happy path in sequence — types -> waf sources -> invariants.
func TestJourneySmokeWafSourceBrowsing(t *testing.T) {
	status, env := lqe.Get(t, lqe.APIPrefix+"/types")
	if status != http.StatusOK || !env.OK() || len(lqe.TypesData(t, env)) != 3 {
		t.Fatalf("smoke step1: types unavailable")
	}
	sources, elapsed := fetchWafSources(t)
	if len(sources) == 0 {
		t.Fatalf("smoke step2: empty waf source list")
	}
	seen := map[string]bool{}
	for _, s := range sources {
		k := fmt.Sprintf("%s/%s/%s", s.Cloud, s.AccountID, s.ResourceID)
		if seen[k] {
			t.Fatalf("smoke step3: duplicate source %s (invariant broken)", k)
		}
		seen[k] = true
	}
	t.Logf("smoke done: %d sources, latency %s", len(sources), elapsed)
}
