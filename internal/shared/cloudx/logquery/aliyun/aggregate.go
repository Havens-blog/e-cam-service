// 聚合 SQL 构造(plan.md §8):SLS 检索|SQL 管道下推,窗口内真实统计。
// 纯函数,单测覆盖检索段拼接 / 分桶 / TopN / kind 字段映射。
package aliyun

import (
	"fmt"
	"strings"
)

// aggregateTopNExpr 按 kind 返回 TopN 分组表达式(空=该源无此维度,跳过 TopN)。
// 与前端统计图对齐:CDN=域名,WAF=规则名(waf3 访问流无规则字段,跳过),SLB=host。
func aggregateTopNExpr(kind mapperKind) string {
	switch kind {
	case kindDCDN:
		return "domain"
	case kindAkamaiCDN:
		return "reqHost"
	case kindCDNOffline:
		// PascalCase 转存无域名独立字段,从完整 URL 提取 host
		return "regexp_extract(RequestURL, '^(?:https?://)?([^/?]+)', 1)"
	case kindAkamaiWAF:
		return "name" // CEF 规则名字段(映射层的 rule_name)
	case kindALB:
		return "http_host"
	default:
		return "" // kindWAF3 访问流无规则维度
	}
}

// buildAggregateSearchPart 管道前的检索段:kind 专属 __topic__ 过滤(与
// Search 的 buildQuery 同源,防聚合 project 噪声流)+ 用户检索式 + 混装源
// 域名过滤(Resources 与 Search 扇出同语义 = 域名清单)。
func buildAggregateSearchPart(kind mapperKind, userQuery string, resources []string) string {
	var parts []string
	switch kind {
	case kindALB:
		parts = append(parts, "__topic__:alb_layer7_access_log")
	case kindWAF3:
		parts = append(parts, "__topic__:waf_access_log")
	}
	if q := strings.TrimSpace(userQuery); q != "" && q != "*" {
		parts = append(parts, "("+q+")")
	}
	if field := domainField(kind); field != "" && len(resources) > 0 {
		var terms []string
		for _, r := range resources {
			if r != "" {
				terms = append(terms, field+": "+r)
			}
		}
		if len(terms) > 0 {
			parts = append(parts, "("+strings.Join(terms, " or ")+")")
		}
	}
	return strings.Join(parts, " and ")
}

// buildAggregateBucketSQL 分桶 SQL:t = 桶起点(Unix 秒),c = 桶内精确条数。
// Total 由分桶求和得出,不再单独跑 count(省一次全窗扫描)。
func buildAggregateBucketSQL(searchPart string, bucketSec int64) string {
	if searchPart == "" {
		searchPart = "*"
	}
	return fmt.Sprintf("%s | select __time__ - __time__ %% %d as t, count(1) as c group by t order by t limit 200",
		searchPart, bucketSec)
}

// buildAggregateTopNSQL TopN SQL:k = 分组维度,c = 条数(降序取前 limit)。
func buildAggregateTopNSQL(searchPart, expr string, limit int) string {
	if searchPart == "" {
		searchPart = "*"
	}
	return fmt.Sprintf("%s | select %s as k, count(1) as c group by k order by c desc limit %d",
		searchPart, expr, limit)
}
