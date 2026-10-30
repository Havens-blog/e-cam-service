package web

import (
	"testing"
)

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
