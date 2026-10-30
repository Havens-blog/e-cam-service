package authz

import (
	"reflect"
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
		{"首尾空格", []string{"  cam:logs:export  "}, []string{"cam:logs:export"}},
		{"逗号两侧空格", []string{"a , b"}, []string{"a", "b"}},
		{"空分段丢弃", []string{"a,,b,"}, []string{"a", "b"}},
		{"无输入", nil, nil},
		{"非法 JSON 回退为逗号切分", []string{"[not-json"}, []string{"[not-json"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseClaimCodes(tc.raw...)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseClaimCodes(%v) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

// TestParseClaimCodes_FeedExactMatch 守住「trim 后能与精确匹配对上」这一关键契约：
// 这是此前 logquery 侧缺 trim 导致有权限用户被 403 的回归点。
func TestParseClaimCodes_FeedExactMatch(t *testing.T) {
	codes := ParseClaimCodes(" cam:logs:export ", "")
	found := false
	for _, c := range codes {
		if c == "cam:logs:export" {
			found = true
		}
	}
	if !found {
		t.Fatalf("trim 后应能精确匹配 cam:logs:export，实际 %v", codes)
	}
}
