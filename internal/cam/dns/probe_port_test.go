package dns

import "testing"

func TestBuildProbeHostname(t *testing.T) {
	cases := map[struct{ rr, domain string }]string{
		{"www", "example.com"}:      "www.example.com",
		{"@", "example.com"}:        "example.com",
		{"", "example.com"}:         "example.com",
		{"api", "example.com"}:      "api.example.com",
		{"*.example", "example.com"}: "", // 通配符 DNS 记录跳过
		{"www", ""}:                 "",
	}
	for in, want := range cases {
		got := buildProbeHostname(in.rr, in.domain)
		if got != want {
			t.Errorf("buildProbeHostname(%q,%q)=%q, want %q", in.rr, in.domain, got, want)
		}
	}
}
