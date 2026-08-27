package service

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/dns"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
)

func TestBuildProbeHostname(t *testing.T) {
	cases := map[struct{ rr, domain string }]string{
		{"www", "example.com"}:    "www.example.com",
		{"@", "example.com"}:      "example.com",
		{"", "example.com"}:        "example.com",
		{"api", "example.com"}:     "api.example.com",
		{"*.example", "example.com"}: "", // 通配符 DNS 记录跳过
		{"www", ""}:               "",
	}
	for in, want := range cases {
		got := buildProbeHostname(in.rr, in.domain)
		if got != want {
			t.Errorf("buildProbeHostname(%q,%q)=%q, want %q", in.rr, in.domain, got, want)
		}
	}
}

func TestCoversSingleLabel(t *testing.T) {
	if !coversSingleLabel("www.example.com", "example.com") {
		t.Error("www.example.com 应被 *.example.com 单标签覆盖")
	}
	if coversSingleLabel("a.b.example.com", "example.com") {
		t.Error("多标签子域名 a.b.example.com 不应被 *.example.com 覆盖")
	}
	if coversSingleLabel("example.com", "example.com") {
		t.Error("根域 example.com 不应被 *.example.com 覆盖（无标签）")
	}
	if coversSingleLabel("www.other.com", "example.com") {
		t.Error("不匹配 base 的域名不应覆盖")
	}
}

func TestCoverageIndex_Covers(t *testing.T) {
	certs := []domain.Certificate{
		{Fingerprint: "fp-wild", Sans: []string{"*.example.com"}},
		{Fingerprint: "fp-exact", Sans: []string{"api.example.com"}},
	}
	ci := buildCoverageIndex(certs)

	// 通配符子域名命中通配符证书
	if !ci.covers("www.example.com", "fp-wild") {
		t.Error("www.example.com 应覆盖 fp-wild（*.example.com）")
	}
	// 精确 SAN 命中精确证书
	if !ci.covers("api.example.com", "fp-exact") {
		t.Error("api.example.com 应覆盖 fp-exact")
	}
	// 子域名不命中另一张证书的指纹 → diff
	if ci.covers("www.example.com", "fp-exact") {
		t.Error("www.example.com 不应覆盖 fp-exact")
	}
	// 多标签子域名不被通配符覆盖
	if ci.covers("a.b.example.com", "fp-wild") {
		t.Error("多标签子域名不应被 *.example.com 覆盖")
	}
}

func TestLinkedResourceType(t *testing.T) {
	if got := linkedResourceType(&dns.LinkedResource{Type: "cdn"}); got != "cdn" {
		t.Errorf("got %q", got)
	}
	if got := linkedResourceType(nil); got != "" {
		t.Errorf("nil 应返回空串，got %q", got)
	}
}

func TestIsPlaceholderFingerprint(t *testing.T) {
	if !isPlaceholderFingerprint("certscan-unresolved:aliyun|acc|1997") {
		t.Error("占位指纹应识别为 true")
	}
	if isPlaceholderFingerprint("abcdef0123456789") {
		t.Error("真实指纹应识别为 false")
	}
}

func TestRefIndexMatches(t *testing.T) {
	idx := map[string]map[string]bool{
		"cdn|www.example.com": {"fp-real": true},
		"cdn|api.example.com": {"fp-real": true},
	}
	cdn := &dns.LinkedResource{Type: "cdn"}
	waf := &dns.LinkedResource{Type: "waf"}
	ext := &dns.LinkedResource{Type: "external"}

	// CDN hostname 命中引用且指纹一致 → true
	if !refIndexMatches(idx, cdn, "www.example.com", "fp-real") {
		t.Error("www.example.com 引用命中 fp-real 应 consistent")
	}
	// 命中引用但指纹不一致 → false（资源级漂移，落 diff）
	if refIndexMatches(idx, cdn, "www.example.com", "fp-other") {
		t.Error("引用命中但指纹不一致应返回 false")
	}
	// 无引用记录 → false（回退 coverage）
	if refIndexMatches(idx, cdn, "no.example.com", "fp-real") {
		t.Error("无引用记录应返回 false")
	}
	// WAF 未在索引中 → false
	if refIndexMatches(idx, waf, "www.example.com", "fp-real") {
		t.Error("waf product 无索引应返回 false")
	}
	// external（ALB 源站 A 记录）→ false，回退 coverage
	if refIndexMatches(idx, ext, "www.example.com", "fp-real") {
		t.Error("external linked_resource 应回退 coverage")
	}
	// nil linked_resource → false
	if refIndexMatches(idx, nil, "www.example.com", "fp-real") {
		t.Error("nil linked_resource 应返回 false")
	}
	// nil 索引 → false
	if refIndexMatches(nil, cdn, "www.example.com", "fp-real") {
		t.Error("nil 索引应返回 false")
	}
}
