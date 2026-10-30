package paging

import "testing"

// 上限归一：`limit=999999` 这类请求必须被钳到 MaxLimit（实测会返回数千条 / 数 MB）。
func TestClamp(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{0, 0},        // <=0 保持原值：由调用方既有"默认 20"语义处理
		{-5, -5},      // 同上
		{1, 1},        // 正常值不动
		{20, 20},      // 前端默认页长
		{200, 200},    // 前端**实际使用的最大值**必须原样放行
		{MaxLimit, MaxLimit},          // 边界：等于上限
		{MaxLimit + 1, MaxLimit},      // 刚越界 → 钳到上限
		{999999, MaxLimit},            // 实测的超大值 → 钳到上限
	}
	for _, c := range cases {
		if got := Clamp(c.in); got != c.want {
			t.Errorf("Clamp(%d) = %d, want %d", c.in, got, c.want)
		}
		if got := ClampInt64(int64(c.in)); int(got) != c.want {
			t.Errorf("ClampInt64(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// Normalize：非法/<=0 → 默认值；超上限 → 上限；合法值不动。
func TestNormalize(t *testing.T) {
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
		{"1001", 20, MaxLimit},
		{"999999", 20, MaxLimit},
	}
	for _, c := range cases {
		if got := Normalize(c.raw, c.def); got != c.want {
			t.Errorf("Normalize(%q, %d) = %d, want %d", c.raw, c.def, got, c.want)
		}
		if got := NormalizeInt64(c.raw, int64(c.def)); int(got) != c.want {
			t.Errorf("NormalizeInt64(%q, %d) = %d, want %d", c.raw, c.def, got, c.want)
		}
	}
}

// 上限必须覆盖前端当前最大请求（200），否则会截断正常分页 —— 这条断言防止有人把上限调得过小。
func TestMaxLimitCoversFrontendUsage(t *testing.T) {
	const frontendMax = 200
	if MaxLimit < frontendMax {
		t.Fatalf("MaxLimit=%d 小于前端最大请求 %d，会截断正常分页", MaxLimit, frontendMax)
	}
}
