// 阿里云字段筛选/分组维度/聚合指标的列映射与检索段编译。
// 归一化字段 → SLS 原始列(mapper.go 同源;只收录单一列、无链式回退的字段,
// 保证 eq/前缀/包含语义可精确下推)。缓存命中/边缘节点等跨字段归一字段
// 不在列映射内 — 明细由 EntryMatches 兜底,聚合下推时该字段(源)显式跳过。
package aliyun

import (
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
)

// dimColumnExpr 归一化维度/筛选项 → SLS 列表达式(key 为 /types 字段字典键)。
// 表达式可为函数(如 CDN 离线转存的 host 从 RequestURL 提取)。
var dimColumnExpr = map[mapperKind]map[string]string{
	kindDCDN: {
		"host": "domain", "status": "return_code", "method": "method",
		"client_ip": "client_ip", "uri": "uri",
	},
	kindCDNOffline: {
		"host": "regexp_extract(RequestURL, '^(?:https?://)?([^/?]+)', 1)",
		"status": "HTTPStatus", "method": "HTTPMethod", "client_ip": "RemoteIP",
	},
	kindAkamaiCDN: {
		"host": "reqHost", "status": "statusCode", "method": "reqMethod",
		"client_ip": "cliIP",
	},
	kindWAF3: {
		"host": "host", "status": "status", "method": "request_method",
		"uri": "request_uri", "client_ip": "real_client_ip",
	},
	kindAkamaiWAF: {
		"host": "dhost", "rule_name": "name",
	},
	kindALB: {
		"host": "http_host", "status": "status", "method": "request_method",
		"uri": "request_uri", "client_ip": "client_ip", "upstream_status": "upstream_status",
	},
}

// metricExpr 聚合指标按 kind 编译(空=该 kind 不支持该指标)。
// 单位对齐 mapper:ALB request_time 为秒(secondsToMs 换算),×1000 补回 ms。
var metricExpr = map[mapperKind]map[string]string{
	kindDCDN: {
		"count":       "count(1)",
		"sum_bytes":   "sum(response_size)",
		"avg_latency": "avg(request_time)",
		"p99_latency": "approx_percentile(request_time, 0.99)",
	},
	kindCDNOffline: {
		"count":       "count(1)",
		"sum_bytes":   "sum(ResponseSize)",
		"avg_latency": "avg(RequestTime)",
		"p99_latency": "approx_percentile(RequestTime, 0.99)",
	},
	kindAkamaiCDN: {
		"count":       "count(1)",
		"sum_bytes":   "sum(bytes)",
		"avg_latency": "avg(turnAroundTimeMSec)",
		"p99_latency": "approx_percentile(turnAroundTimeMSec, 0.99)",
	},
	kindALB: {
		"count":       "count(1)",
		"sum_bytes":   "sum(body_bytes_sent)",
		"avg_latency": "avg(request_time) * 1000",
		"p99_latency": "approx_percentile(request_time, 0.99) * 1000",
	},
	// kindWAF3/kindAkamaiWAF:无 latency/bytes 指标列,仅 count
	kindWAF3:     {"count": "count(1)"},
	kindAkamaiWAF: {"count": "count(1)"},
}

// filterSearchPart 编译字段筛选为 SLS 检索段(多条件 AND;返回 false =
// 存在该 kind 无法下推的字段,调用方显式标注跳过该源)。
// 语法对齐 SLS 键检索:数字字面量不引号,其余短语引号;contain 用通配 *v*,
// prefix 用 v*,neq 用 not。
func filterSearchPart(kind mapperKind, filters []logquery.FieldFilter) (string, bool) {
	cols := dimColumnExpr[kind]
	var parts []string
	for _, f := range filters {
		col, ok := cols[f.Field]
		if !ok || f.Value == "" {
			return "", false
		}
		// eq 数字字面量不引号(数值检索更稳);其余统一短语引号(规避
		// SLS 词法空格/冒号等),末端追加 SLS 通配符(*val* / val*)。
		val := f.Value
		if f.Op == "contains" {
			val = "*" + val + "*"
		} else if f.Op == "prefix" {
			val = val + "*"
		}
		if !numericLiteral(val) || strings.ContainsAny(val, "*") {
			val = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(val) + `"`
		}
		switch f.Op {
		case "neq":
			parts = append(parts, "not "+col+": "+val)
		default: // eq / contains / prefix
			parts = append(parts, col+": "+val)
		}
	}
	if len(parts) == 0 {
		return "", true
	}
	return "(" + strings.Join(parts, " and ") + ")", true
}

// numericLiteral 数字字面量(含小数/负号)不加引号,SLS 数值检索更稳。
func numericLiteral(s string) bool {
	if s == "" {
		return false
	}
	dot := false
	for i, c := range s {
		switch {
		case c >= '0' && c <= '9':
		case c == '.' && i > 0 && !dot:
			dot = true
		case c == '-' && i == 0:
		default:
			return false
		}
	}
	return true
}

// dimensionExpr 分组维度编译(候选 = 列映射;kind 默认维度为空语义,
// 由上游在 Dimension 缺省时回退 aggregateTopNExpr)。
func dimensionExpr(kind mapperKind, dim string) (string, bool) {
	if dim == "" {
		return "", true
	}
	expr, ok := dimColumnExpr[kind][dim]
	return expr, ok
}

// metricSQLExpr 聚合指标编译;不支持的 (kind, metric) 返回 false。
func metricSQLExpr(kind mapperKind, metric string) (string, bool) {
	if metric == "" {
		metric = "count"
	}
	expr, ok := metricExpr[kind][metric]
	return expr, ok
}

// metricIsCount 指标是否为计数(count 语义)。
func metricIsCount(metric string) bool { return metric == "" || metric == "count" }

// metricIsWeighted avg/p99 等非可加指标:跨源归并需按 count 加权均值;
// count/sum_bytes 可加直接求和。
func metricIsWeighted(metric string) bool {
	switch metric {
	case "", "count", "sum_bytes":
		return false
	default:
		return true
	}
}