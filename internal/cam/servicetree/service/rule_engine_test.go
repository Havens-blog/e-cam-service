package service

import (
	"testing"

	stdomain "github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
)

// TestRuleEngineCompareValue 锁定 compareValue 各操作符语义。
// Contains 语义已由大小写敏感放宽为不敏感（唯一行为变化，委托 cam/domain.MatchValue）。
func TestRuleEngineCompareValue(t *testing.T) {
	s := &ruleEngineService{}
	tests := []struct {
		name     string
		actual   string
		operator string
		expected string
		want     bool
	}{
		// eq：精确、大小写敏感（委托 MatchValue "equals"，语义不变）
		{name: "eq 相等匹配", actual: "prod-01", operator: stdomain.OperatorEq, expected: "prod-01", want: true},
		{name: "eq 大小写敏感不匹配", actual: "prod-01", operator: stdomain.OperatorEq, expected: "Prod-01", want: false},
		// contains：大小写不敏感（行为变化点，显式断言防回归）
		{name: "contains 大小写不敏感 Prod 匹配 prod-01", actual: "prod-01", operator: stdomain.OperatorContains, expected: "Prod", want: true},
		{name: "contains 大小写不敏感反向", actual: "Prod-01", operator: stdomain.OperatorContains, expected: "prod", want: true},
		{name: "contains 不匹配", actual: "prod-01", operator: stdomain.OperatorContains, expected: "stage", want: false},
		// regex：大小写敏感、非法表达式返回 false（委托 MatchValue "regex"）
		{name: "regex 匹配", actual: "prod-01", operator: stdomain.OperatorRegex, expected: `^prod-[0-9]+$`, want: true},
		{name: "regex 大小写敏感", actual: "prod-01", operator: stdomain.OperatorRegex, expected: `^Prod`, want: false},
		{name: "regex 非法表达式返回 false", actual: "prod-01", operator: stdomain.OperatorRegex, expected: "[invalid", want: false},
		// ne/in/not_in/exists：原样保留，不经 MatchValue
		{name: "ne 不同为真", actual: "prod-01", operator: stdomain.OperatorNe, expected: "stage-01", want: true},
		{name: "ne 相同为假", actual: "prod-01", operator: stdomain.OperatorNe, expected: "prod-01", want: false},
		{name: "in 含空格容忍", actual: "prod", operator: stdomain.OperatorIn, expected: " stage , prod ", want: true},
		{name: "in 不匹配", actual: "prod", operator: stdomain.OperatorIn, expected: "stage,dev", want: false},
		{name: "not_in 不在列表为真", actual: "prod", operator: stdomain.OperatorNotIn, expected: "stage,dev", want: true},
		{name: "not_in 在列表为假", actual: "prod", operator: stdomain.OperatorNotIn, expected: "stage,prod", want: false},
		{name: "exists 非空为真", actual: "x", operator: stdomain.OperatorExists, expected: "", want: true},
		{name: "exists 空为假", actual: "", operator: stdomain.OperatorExists, expected: "", want: false},
		{name: "未知操作符返回 false", actual: "prod-01", operator: "bogus", expected: "prod", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.compareValue(tt.actual, tt.operator, tt.expected); got != tt.want {
				t.Errorf("compareValue(%q, %q, %q) = %v, want %v", tt.actual, tt.operator, tt.expected, got, tt.want)
			}
		})
	}
}
