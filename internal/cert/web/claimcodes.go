package web

import (
	"encoding/json"
	"strings"
)

// ParseClaimCodes 解析能力码声明。
//
// 为什么在这里而不是 internal/shared/authz：
//
//	internal/cert 是 depcheck 锁定的 extraction-ready 域（R4）——它不得 import internal/shared，
//	否则该域就无法被整体剥离为独立服务。历史上这里曾直接复用 shared/authz.ParseClaimCodes，
//	被 depcheck 判为跨域违规（main 上 CI 一直因此为红）。
//	纯解析函数（无状态、无外部依赖）在域内自持一份，是保住提取边界的最小代价；
//	internal/shared/authz 仍服务其它域（logquery 等），两边共用同一段逻辑约定与注释。
//
// 支持两种携带形态（eiam claims 是 map[string]string，数组会被序列化成字符串）：
//   - JSON 数组字符串：`["a","b"]`
//   - 逗号分隔：`a,b,c`
//
// 多个来源按顺序拼接；整体与每个分段都做 TrimSpace 并丢弃空项。
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
