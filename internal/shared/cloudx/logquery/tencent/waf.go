// 腾讯云 WAF 访问日志映射(CLS 投递,标准字段结构)。
//
// ⚠️ unverified:topic 近 7/30 天均 0 条(2026-09-17),字段名按腾讯云 WAF
// 日志服务公开结构编写并做多候选容错(req_method/method 等);投递开启后
// 用真实日志校准(field-mapping.md §四惯例)。
package tencent

import (
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
)

// wafFieldMap 统一字段 → WAF 原始列(检索/聚合可下推;多候选容错)。
var wafFieldMap = map[string]string{
	"host":       "domain",
	"client_ip":  "client_ip",
	"method":     "req_method",
	"uri":        "req_uri",
	"status":     "resp_status",
	"rule_id":    "rule_id",
	"rule_name":  "rule_name",
	"action":     "action",
	"attack_type": "attack_type",
}

// wafMetricExpr WAF 聚合指标(访问日志无字节/耗时列,仅计数)。
var wafMetricExpr = map[string]string{"count": "count(*)"}

// wafDimensionExpr 分组维度编译(同 EO:统一字段映射优先,否则透传,非法拒绝)。
func wafDimensionExpr(dim string) (string, bool) {
	if dim == "" {
		return "", true
	}
	if col, ok := wafFieldMap[dim]; ok {
		return col, true
	}
	if identifierLike(dim) {
		return dim, true
	}
	return "", false
}

// wafLog 一条 WAF 访问日志 → 统一 WAF 模型(字段缺失/占位容忍;Raw 全量保留)。
func wafLog(m logquery.LogMeta, raw map[string]string) *logquery.WAFLogEntry {
	ts := logquery.Int(raw["__TIMESTAMP__"])
	if ts <= 0 {
		ts = logquery.Int(raw["time"])
		if ts > 0 && ts < 1e12 {
			ts *= 1000 // 腾讯 WAF time 为 Unix 秒(兜底换算)
		}
	}
	if ts <= 0 {
		return nil
	}
	host := firstNonEmpty(raw, "domain", "host")
	if host != "" {
		m.ResourceID = host // 混装 topic 的选择粒度收敛到域名
	}
	uri := firstNonEmpty(raw, "req_uri", "request_uri")
	if qs := firstNonEmpty(raw, "req_query_string", "query_string"); qs != "" && !strings.Contains(uri, "?") {
		uri += "?" + qs
	}
	status := int(logquery.Int(firstNonEmpty(raw, "resp_status", "status")))
	action := normalizeWAFAction(firstNonEmpty(raw, "action", "waf_action"))
	severity := logquery.NormalizeSeverity(firstNonEmpty(raw, "rule_level", "level"))
	geo := joinNonEmpty(firstNonEmpty(raw, "client_country", "country"), firstNonEmpty(raw, "client_province", "province"))
	return &logquery.WAFLogEntry{
		Meta:      m,
		Timestamp: ts,
		ClientIP:  firstNonEmpty(raw, "client_ip", "attack_ip"),
		Host:      host,
		URI:       uri,
		Method:    firstNonEmpty(raw, "req_method", "method"),
		RuleID:    firstNonEmpty(raw, "rule_id", "ruleid"),
		RuleName:  firstNonEmpty(raw, "rule_name", "rulename"),
		Action:    action,
		Severity:  severity,
		Status:    status,
		Geo:       geo,
		Raw:       toRawMap(raw),
	}
}

// normalizeWAFAction 腾讯 WAF action → 统一枚举:
// attack(拦截)→ block,observe(观察)→ alert,pass(放行)→ pass;
// 其余交给通用归一(小写直返,兜底不丢语义)。
func normalizeWAFAction(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "attack", "block", "intercept", "deny", "reject":
		return "block"
	case "observe", "alert", "log":
		return "alert"
	case "pass", "allow", "default":
		return "pass"
	default:
		return logquery.NormalizeAction(s)
	}
}

// hasWAFFields topic 采样判定为 WAF 访问日志(特征字段;与 EO/Kong 区分)。
func hasWAFFields(raw map[string]string) bool {
	if firstNonEmpty(raw, "req_uri", "request_uri") == "" {
		return false
	}
	return raw["rule_id"] != "" || raw["rule_name"] != "" || raw["attack_type"] != "" || raw["waf_action"] != ""
}

// firstNonEmpty 依次取首个非空字段(多候选容错)。
func firstNonEmpty(raw map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := raw[k]; v != "" && v != "-" {
			return v
		}
	}
	return ""
}

// joinNonEmpty 非空片段拼接(地理等)。
func joinNonEmpty(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "/")
}
