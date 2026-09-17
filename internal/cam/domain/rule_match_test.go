package domain

import "testing"

func TestMatchValue(t *testing.T) {
	tests := []struct {
		name     string
		operator string
		actual   string
		expected string
		want     bool
	}{
		// equals 精确、大小写敏感
		{name: "equals exact match", operator: "equals", actual: "prod-01", expected: "prod-01", want: true},
		{name: "equals case sensitive mismatch", operator: "equals", actual: "Prod-01", expected: "prod-01", want: false},
		{name: "equals mismatch", operator: "equals", actual: "prod-01", expected: "prod-02", want: false},
		{name: "equals empty both", operator: "equals", actual: "", expected: "", want: true},

		// contains 大小写不敏感（双侧 ToLower）
		{name: "contains case insensitive", operator: "contains", actual: "my-Prod-01", expected: "PROD", want: true},
		{name: "contains pattern case mixed", operator: "contains", actual: "prod-01", expected: "Prod", want: true},
		{name: "contains not contained", operator: "contains", actual: "prod-01", expected: "stag", want: false},

		// prefix 大小写不敏感
		{name: "prefix case insensitive", operator: "prefix", actual: "Prod-01", expected: "prod", want: true},
		{name: "prefix pattern uppercase", operator: "prefix", actual: "prod-01", expected: "PROD", want: true},
		{name: "prefix not matched", operator: "prefix", actual: "prod-01", expected: "stag", want: false},
		{name: "prefix full string", operator: "prefix", actual: "prod-01", expected: "prod-01", want: true},

		// suffix 大小写不敏感
		{name: "suffix case insensitive", operator: "suffix", actual: "prod-PROD", expected: "prod", want: true},
		{name: "suffix pattern uppercase", operator: "suffix", actual: "prod-01", expected: "-01", want: true},
		{name: "suffix not matched", operator: "suffix", actual: "prod-01", expected: "02", want: false},
		{name: "suffix full string", operator: "suffix", actual: "prod-01", expected: "prod-01", want: true},

		// regex：合法匹配、转义、非法模式
		{name: "regex simple match", operator: "regex", actual: "prod-01", expected: `^prod-\d+$`, want: true},
		{name: "regex case sensitive", operator: "regex", actual: "Prod-01", expected: `^prod-\d+$`, want: false},
		{name: "regex escaped dot", operator: "regex", actual: "prod.01", expected: `^prod\.01$`, want: true},
		{name: "regex escaped dot no false positive", operator: "regex", actual: "prodX01", expected: `^prod\.01$`, want: false},
		{name: "regex invalid pattern", operator: "regex", actual: "prod-01", expected: `^prod-[`, want: false},
		{name: "regex no match", operator: "regex", actual: "prod-01", expected: `^stag`, want: false},

		// 未知操作符
		{name: "unknown operator", operator: "startswith", actual: "prod-01", expected: "prod", want: false},
		{name: "empty operator", operator: "", actual: "prod-01", expected: "prod-01", want: false},
		{name: "ne operator not supported here", operator: "ne", actual: "prod-01", expected: "prod-02", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchValue(tt.operator, tt.actual, tt.expected); got != tt.want {
				t.Errorf("MatchValue(%q, %q, %q) = %v, want %v", tt.operator, tt.actual, tt.expected, got, tt.want)
			}
		})
	}
}
