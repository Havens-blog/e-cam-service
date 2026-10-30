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

func TestParseClaimCodes(t *testing.T) {
	cases := []struct {
		name string
		raw  []string
		want []string
	}{
		{"JSON 数组", []string{`["a","b"]`}, []string{"a", "b"}},
		{"逗号分隔", []string{"a,b,c"}, []string{"a", "b", "c"}},
		{"混合多源", []string{`["a"]`, "b,c"}, []string{"a", "b", "c"}},
		{"空忽略", []string{"", "  ", "x"}, []string{"x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseClaimCodes(tc.raw...)
			if len(got) != len(tc.want) {
				t.Fatalf("parseClaimCodes(%v) = %v, want %v", tc.raw, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("parseClaimCodes(%v) = %v, want %v", tc.raw, got, tc.want)
				}
			}
		})
	}
}
