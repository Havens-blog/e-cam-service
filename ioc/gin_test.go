package ioc

import "testing"

func TestOriginAllowed(t *testing.T) {
	allowed := []string{"http://localhost:8888", "*.jlcops.com"}

	tests := []struct {
		name     string
		origin   string
		list     []string
		allowAll bool
		want     bool
	}{
		{name: "精确匹配", origin: "http://localhost:8888", list: allowed, want: true},
		{name: "大小写不敏感", origin: "HTTP://LOCALHOST:8888", list: allowed, want: true},
		{name: "不在白名单", origin: "http://evil.example.com", list: allowed, want: false},
		{name: "通配子域命中", origin: "https://cam.jlcops.com", list: allowed, want: true},
		{name: "通配主域本身不命中", origin: "https://jlcops.com", list: allowed, want: false},
		{name: "仿冒后缀不命中", origin: "https://eviljlcops.com", list: allowed, want: false},
		{name: "allow_all 放行任意来源", origin: "http://anything.example", list: allowed, allowAll: true, want: true},
		{name: "空白名单拒绝", origin: "http://localhost:5173", list: []string{}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := originAllowed(tt.origin, tt.list, tt.allowAll); got != tt.want {
				t.Errorf("originAllowed(%q) = %v, want %v", tt.origin, got, tt.want)
			}
		})
	}
}

func TestOriginAllowedDefaults(t *testing.T) {
	// 默认白名单必须覆盖本地联调入口，否则未配置 cors 的部署会静默拒绝跨域
	for _, origin := range []string{
		"http://localhost:8888", "http://127.0.0.1:5173", "http://localhost:3333",
	} {
		if !originAllowed(origin, corsDefaultOrigins, false) {
			t.Errorf("默认白名单应放行 %q", origin)
		}
	}
}
