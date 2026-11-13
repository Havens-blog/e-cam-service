package dns

import "testing"

func TestBuildProbeHostname(t *testing.T) {
	cases := map[struct{ rr, domain string }]string{
		// 既有口径回归（相对标签拼接）
		{"www", "example.com"}:       "www.example.com",
		{"@", "example.com"}:         "example.com",
		{"", "example.com"}:          "example.com",
		{"api", "example.com"}:       "api.example.com",
		{"*.example", "example.com"}: "", // 通配符 DNS 记录跳过
		{"_bbbba123", "example.com"}: "", // 证书校验等下划线协议记录非 TLS 端点
		{"www", ""}:                  "",
		// 畸形域名治理：rr 已是完整域名时不得重复后缀
		{"api.jlcpcb.com", "jlcpcb.com"}:           "api.jlcpcb.com",
		{"3d.jlcpcb.com", "jlcpcb.com"}:            "3d.jlcpcb.com",
		{"account.forface3d.net", "forface3d.net"}: "account.forface3d.net",
		{"API.JLCpcb.CN", "jlcpcb.cn"}:             "API.JLCpcb.CN", // 大小写不敏感判定，保留原写法
		{"jlcpcb.com.", "jlcpcb.com"}:              "jlcpcb.com",    // FQDN 尾点归一
		{"www.jlcpcb.com.", "jlcpcb.com"}:          "www.jlcpcb.com",
		// rr 恰等于根域（部分源把 apex 记录 rr 填成全域名）
		{"example.com", "example.com"}: "example.com",
		// 易误伤的边界：rr 以根域结尾但并非其子域（xxxexample.com 不是 example.com 的子域）
		{"xxxexample.com", "example.com"}: "xxxexample.com.example.com",
		// 相对标签 + 多级根域（zone 本身含多级）
		{"www", "sub.example.com"}:           "www.sub.example.com",
		{"a.b.example.com", "b.example.com"}: "a.b.example.com",
	}
	for in, want := range cases {
		got := buildProbeHostname(in.rr, in.domain)
		if got != want {
			t.Errorf("buildProbeHostname(%q,%q)=%q, want %q", in.rr, in.domain, got, want)
		}
	}
}
