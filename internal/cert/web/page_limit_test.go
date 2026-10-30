package web

import "testing"

// cert 模块的本地单页上限：数值必须与 internal/shared/paging.MaxLimit 一致
// （此处**不能** import shared —— depcheck R4 要求 cert 保持零跨域出向，故用常量断言把口径钉在本模块内）。
func TestClampPageSize(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{0, 0},                // <=0 保持原值，交回调用方既有默认语义
		{-1, -1},              // 同上
		{20, 20},              // 前端默认
		{200, 200},            // 前端实际最大值必须放行
		{1000, 1000},          // 边界
		{1001, maxPageSize},   // 越界钳制
		{999999, maxPageSize}, // 实测的超大值
	}
	for _, c := range cases {
		if got := clampPageSize(c.in); got != c.want {
			t.Errorf("clampPageSize(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// 上限必须覆盖前端当前最大请求（200），否则会截断正常分页。
func TestMaxPageSizeCoversFrontendUsage(t *testing.T) {
	const frontendMax = 200
	if maxPageSize < frontendMax {
		t.Fatalf("maxPageSize=%d 小于前端最大请求 %d，会截断正常分页", maxPageSize, frontendMax)
	}
}
