// 聚合能力(plan.md §8):窗口内真实统计下推到各云查询引擎
// (SLS 检索|SQL / LTS 分析查询),只回传分桶与分组结果——
// 百万/千万级原始行不过网关,趋势图与总数不再受采样限制。
package logquery

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// AggregateBucket 时间分桶(Timestamp=桶起点 Unix 毫秒 UTC;Count=窗口内该桶精确条数)。
type AggregateBucket struct {
	Timestamp int64 `json:"timestamp"`
	Count     int64 `json:"count"`
}

// TopNItem 聚合 TopN 条目(域名/规则等按类型维度;由 provider 归并后取前 10)。
type TopNItem struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// AggregateParams 聚合参数。BucketSec 由服务层按窗口统一计算并随请求下发,
// 保证联邦内所有源分桶对齐(合并 = 逐桶求和)。
type AggregateParams struct {
	StartTime int64    // Unix 毫秒 UTC(含)
	EndTime   int64    // Unix 毫秒 UTC(含)
	Query     string   // 原生检索式透传(与 Search 同语义)
	BucketSec int64    // 分桶秒数(PickBucketSec 预计算)
	Resources []string // 可选:限定资源(域名/LB ID)
}

// AggregateResult 单源聚合结果。Total = 分桶求和(省一次 count 扫描)。
type AggregateResult struct {
	Total   int64
	Buckets []AggregateBucket
	TopN    []TopNItem
}

// Aggregator 可选能力接口:窗口内真实聚合。未实现的 provider(S3 文件类、
// 占位 stub)由联邦层显式标注"不支持聚合",不静默缺失。
type Aggregator interface {
	Aggregate(ctx context.Context, account *domain.CloudAccount, params AggregateParams) (*AggregateResult, error)
}

// aggregateBucketSteps 分桶候选步长(窗口内 ≤100 桶取最小步长)。
var aggregateBucketSteps = []int64{60, 300, 900, 3600, 21600, 86400}

// PickBucketSec 按窗口自适应分桶秒数(服务层统一计算,随参数下发)。
func PickBucketSec(startMs, endMs int64) int64 {
	spanSec := (endMs - startMs) / 1000
	if spanSec <= 0 {
		return aggregateBucketSteps[0]
	}
	for _, s := range aggregateBucketSteps {
		if spanSec/s <= 100 {
			return s
		}
	}
	return aggregateBucketSteps[len(aggregateBucketSteps)-1]
}
