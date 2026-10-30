package export

import (
	"testing"
)

func TestCanExport(t *testing.T) {
	cases := []struct {
		name    string
		isAdmin bool
		codes   []string
		want    bool
	}{
		{"admin 放行", true, nil, true},
		{"持 cam:logs:export 放行", false, []string{"cam:logs:export"}, true},
		{"空清单默认放行", false, nil, true},
		{"有权限但无导出码拒绝", false, []string{"cam:asset:view"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanExport(tc.isAdmin, tc.codes); got != tc.want {
				t.Fatalf("CanExport(%v, %v) = %v, want %v", tc.isAdmin, tc.codes, got, tc.want)
			}
		})
	}
}
