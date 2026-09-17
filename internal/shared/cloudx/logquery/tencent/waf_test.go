package tencent

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
)

// 腾讯 WAF 访问日志标准字段 fixture(结构按公开文档;投递开后用真实日志校准)。
func TestWAFLog(t *testing.T) {
	raw := map[string]string{
		"__TIMESTAMP__": "1789490755000",
		"domain":        "www.example.com",
		"client_ip":     "203.0.113.7",
		"req_method":    "POST",
		"req_uri":       "/api/login",
		"req_query_string": "a=1",
		"resp_status":   "403",
		"rule_id":       "1001",
		"rule_name":     "SQL 注入规则",
		"rule_level":    "5",
		"action":        "attack",
		"attack_type":   "sql",
		"client_country": "中国",
		"client_province": "广东",
	}
	e := wafLog(logquery.LogMeta{}, raw)
	if e == nil {
		t.Fatal("wafLog returned nil")
	}
	if e.Timestamp != 1789490755000 {
		t.Errorf("Timestamp = %d", e.Timestamp)
	}
	if e.Host != "www.example.com" || e.Meta.ResourceID != "www.example.com" {
		t.Errorf("Host/ResourceID = %q/%q", e.Host, e.Meta.ResourceID)
	}
	if e.ClientIP != "203.0.113.7" || e.Method != "POST" {
		t.Errorf("ClientIP/Method mismatch")
	}
	if e.URI != "/api/login?a=1" {
		t.Errorf("URI = %q", e.URI)
	}
	if e.Status != 403 {
		t.Errorf("Status = %d, want 403", e.Status)
	}
	if e.RuleID != "1001" || e.RuleName != "SQL 注入规则" {
		t.Errorf("Rule mismatch")
	}
	if e.Action != "block" { // attack → block
		t.Errorf("Action = %q, want block", e.Action)
	}
	if e.Severity != "high" { // rule_level 5 → high
		t.Errorf("Severity = %q, want high", e.Severity)
	}
	if e.Geo != "中国/广东" {
		t.Errorf("Geo = %q, want 中国/广东", e.Geo)
	}
}

// time 字段秒级兜底换算(×1000 得 ms)。
func TestWAFLogSecondsFallback(t *testing.T) {
	e := wafLog(logquery.LogMeta{}, map[string]string{
		"time": "1789490755", "domain": "x.com",
	})
	if e == nil || e.Timestamp != 1789490755000 {
		t.Fatalf("seconds fallback failed: %v", e)
	}
}

// 多候选容错:字段别名(resp_status/status 等)取首个非空。
func TestWAFLogAliasFallback(t *testing.T) {
	e := wafLog(logquery.LogMeta{}, map[string]string{
		"__TIMESTAMP__": "1789490755000", "host": "x.com",
		"status": "502", "method": "GET", "level": "2",
	})
	if e == nil {
		t.Fatal("nil")
	}
	if e.Host != "x.com" || e.Status != 502 || e.Method != "GET" || e.Severity != "low" {
		t.Errorf("alias fallback mismatch: %+v", e)
	}
}

// 动作归一:attack→block, observe→alert, pass→pass, 未知直返。
func TestNormalizeWAFAction(t *testing.T) {
	cases := map[string]string{
		"attack": "block", "intercept": "block", "observe": "alert",
		"pass": "pass", "allow": "pass", "blocked": "block", "weird": "weird",
	}
	for in, want := range cases {
		if got := normalizeWAFAction(in); got != want {
			t.Errorf("normalizeWAFAction(%q) = %q, want %q", in, got, want)
		}
	}
}

// 分类特征:WAF 判定;EO/Kong 不误判。
func TestHasWAFFields(t *testing.T) {
	waf := map[string]string{"req_uri": "/x", "rule_id": "1"}
	eo := map[string]string{"RequestHost": "a.com", "EdgeResponseStatusCode": "200"}
	kong := map[string]string{"request": "GET / HTTP/1.1"}
	if !hasWAFFields(waf) {
		t.Error("waf fields not detected")
	}
	if hasWAFFields(eo) || hasWAFFields(kong) {
		t.Error("eo/kong misclassified as waf")
	}
}

// 维度编译:统一字段映射 + 透传 + 非法拒绝。
func TestWAFDimensionExpr(t *testing.T) {
	cases := []struct {
		dim string
		ok  bool
		out string
	}{
		{"", true, ""},
		{"host", true, "domain"},
		{"uri", true, "req_uri"},
		{"attack_type", true, "attack_type"},
		{"rule_level", true, "rule_level"}, // 透传原始字段
		{"a;drop", false, ""},
	}
	for _, c := range cases {
		got, ok := wafDimensionExpr(c.dim)
		if ok != c.ok || (ok && got != c.out) {
			t.Errorf("wafDimensionExpr(%q) = (%q,%v), want (%q,%v)", c.dim, got, ok, c.out, c.ok)
		}
	}
}
