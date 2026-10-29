package web

import "testing"

// 单页上限归一：防止 limit 过大一次性拉全量（实测 limit=999999 返回数千条 / 数 MB）。
func TestClampLimit(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{0, 0},                 // <=0 保持原值：由调用方既有"默认 20"语义处理，避免悄悄改语义
		{-5, -5},               // 同上（负数交回原逻辑）
		{1, 1},                 // 正常值不动
		{20, 20},               // 前端默认页长
		{200, 200},             // 前端**实际使用的最大值**必须原样放行
		{1000, 1000},           // 边界：等于上限
		{1001, MaxPageLimit},   // 刚越界 → 钳到上限
		{999999, MaxPageLimit}, // 实测的超大值 → 钳到上限
	}
	for _, c := range cases {
		if got := clampLimit(c.in); got != c.want {
			t.Errorf("clampLimit(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// normalizeLimit：非法/<=0 → 默认值；超上限 → 上限；合法值不动。
func TestNormalizeLimit(t *testing.T) {
	cases := []struct {
		raw  string
		def  int
		want int
	}{
		{"", 20, 20},
		{"abc", 20, 20},
		{"0", 20, 20},
		{"-3", 20, 20},
		{"50", 20, 50},
		{"200", 20, 200},
		{"1000", 20, 1000},
		{"1001", 20, MaxPageLimit},
		{"999999", 20, MaxPageLimit},
	}
	for _, c := range cases {
		if got := normalizeLimit(c.raw, c.def); got != c.want {
			t.Errorf("normalizeLimit(%q, %d) = %d, want %d", c.raw, c.def, got, c.want)
		}
	}
}

// 上限必须足够大以覆盖前端当前最大请求(200)，否则会截断正常分页。
func TestMaxPageLimitCoversFrontendUsage(t *testing.T) {
	const frontendMax = 200
	if MaxPageLimit < frontendMax {
		t.Fatalf("MaxPageLimit=%d 小于前端最大请求 %d，会截断正常分页", MaxPageLimit, frontendMax)
	}
}
