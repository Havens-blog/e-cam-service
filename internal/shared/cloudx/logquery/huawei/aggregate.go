// 聚合 SQL 构造(华为 LTS,plan.md §8):is_analysis_query 管道 SQL
// (2026-09-07 活体验证可用,24h/98.7 万条 1.2s)。
// 方言差异:__time 为毫秒(分桶取模用 bucketSec*1000,t 已是 ms);
// 非结构化流仅内置/结构化列可解析(content 列不可解析——WAF 规则在
// content JSON 内,SQL 取不到,TopN 只对 ELB host 开放)。
package huawei

import "fmt"

// aggregateTopNExpr 按 kind 返回 TopN 分组表达式(空=跳过)。
// WAF 攻击/访问流规则在 content JSON 里,列不可解析,跳过(采样兜底);
// ELB host 与阿里 ALB http_host 同维度,参与归并。
func aggregateTopNExpr(kind mapperKind) string {
	switch kind {
	case kindELB:
		return "host"
	default:
		return "" // kindWAFAttack/kindWAFAccess:规则维度列不可解析
	}
}

// buildAggregateBucketSQL 分桶 SQL:__time 为毫秒,t = 桶起点毫秒。
func buildAggregateBucketSQL(bucketSec int64) string {
	bucketMs := bucketSec * 1000
	return fmt.Sprintf("* | select __time - __time %% %d as t, count(1) as c group by t order by t limit 200", bucketMs)
}

// buildAggregateTopNSQL TopN SQL(ELB host)。
func buildAggregateTopNSQL(expr string, limit int) string {
	return fmt.Sprintf("* | select %s as k, count(1) as c group by k order by c desc limit %d", expr, limit)
}
