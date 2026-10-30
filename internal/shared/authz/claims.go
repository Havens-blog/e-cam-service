// Package authz 授权相关的共享解析逻辑。
//
// 背景：能力码（capability codes）解析此前在 cert/web 与 logquery/web 各有一份实现，
// 且已经分叉过一次 —— logquery/web 那份漏了 TrimSpace，导致 claims 里带首尾空格的
// "cam:logs:export" 无法与精确匹配逻辑对上，有权限的用户被 403 拒绝。
// 因此收敛到此处单一实现，新增使用方不要再复制一份。
package authz

import (
	"encoding/json"
	"strings"
)

// ParseClaimCodes 解析能力码声明。
//
// 支持两种携带形态（eiam claims 是 map[string]string，数组会被序列化成字符串）：
//   - JSON 数组字符串：`["a","b"]`
//   - 逗号分隔：`a,b,c`
//
// 多个来源按顺序拼接；整体与每个分段都会 TrimSpace 并丢弃空项。
// 之所以逐段 trim：授权判定用精确匹配（如 CanExport 的 c == "cam:logs:export"），
// 带空格的能力码会静默失配，表现为「有权限却被拒」。
func ParseClaimCodes(raws ...string) []string {
	var codes []string
	for _, raw := range raws {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if raw[0] == '[' {
			var arr []string
			if err := json.Unmarshal([]byte(raw), &arr); err == nil {
				codes = append(codes, arr...)
				continue
			}
		}
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				codes = append(codes, part)
			}
		}
	}
	return codes
}
