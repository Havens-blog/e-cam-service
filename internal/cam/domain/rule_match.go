package domain

import (
	"regexp"
	"strings"
)

// MatchValue 按规则操作符求值 actual 是否匹配 expected。
// 操作符命名以 tag 侧为准：equals/contains/prefix/suffix/regex。
//   - equals：精确比较（大小写敏感）
//   - contains/prefix/suffix：双侧 strings.ToLower 后比较（大小写不敏感）
//   - regex：regexp.MatchString，仅 err == nil 且 matched 时为 true
//   - 未知操作符返回 false
func MatchValue(operator, actual, expected string) bool {
	switch operator {
	case "equals":
		return actual == expected
	case "contains":
		return strings.Contains(strings.ToLower(actual), strings.ToLower(expected))
	case "prefix":
		return strings.HasPrefix(strings.ToLower(actual), strings.ToLower(expected))
	case "suffix":
		return strings.HasSuffix(strings.ToLower(actual), strings.ToLower(expected))
	case "regex":
		matched, err := regexp.MatchString(expected, actual)
		return err == nil && matched
	default:
		return false
	}
}
