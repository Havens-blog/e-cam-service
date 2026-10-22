// Package metrics 集中定义并注册 e-cam-service 的 Prometheus 指标。
//
// 指标统一挂 ecam 命名空间前缀，避免多服务混抓时指标名冲突；
// 全部注册进 prometheus 默认 registry，经 ioc/gin.go 的 /metrics 端点
// （promhttp.Handler()）暴露给抓取侧。
//
// 当前覆盖两类观测面：
//   - HTTP 流量：QPS/状态码/耗时（经 PrometheusMiddleware 采集）
//   - 同步任务健康度：每类任务的成败次数与耗时（经 InstrumentExecutor 装饰）
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// HTTP 指标：route 标签取 gin 路由模板（c.FullPath()），不用原始路径，
// 避免租户 ID/资源 ID 灌入标签导致基数爆炸（见 middleware.go routeLabel）。
var (
	// HTTPRequestsTotal HTTP 请求计数，按 method/route/status 维度统计。
	HTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ecam",
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests processed, partitioned by method, route template and status code.",
		},
		[]string{"method", "route", "status"},
	)

	// HTTPRequestDuration HTTP 请求耗时直方图，按 method/route 维度统计。
	HTTPRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "ecam",
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request latency in seconds, partitioned by method and route template.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "route"},
	)
)

// 同步任务指标：task_type 取 taskx 执行器类型字符串（如 cam:sync_assets），
// 不动态拼接租户/账号，保持低基数。
var (
	// SyncTaskTotal 同步任务执行次数，按 task_type/result(success|failure) 维度统计。
	SyncTaskTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ecam",
			Name:      "sync_task_total",
			Help:      "Total number of sync task executions, partitioned by task type and result (success/failure).",
		},
		[]string{"task_type", "result"},
	)

	// SyncTaskDuration 同步任务执行耗时直方图（秒）。同步任务普遍分钟级起步，
	// 默认 DefBuckets 上限 10s 会让样本全堆进 +Inf，故用任务级分桶。
	SyncTaskDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "ecam",
			Name:      "sync_task_duration_seconds",
			Help:      "Sync task execution duration in seconds, partitioned by task type.",
			Buckets:   []float64{1, 10, 30, 60, 300, 600, 1800, 3600},
		},
		[]string{"task_type"},
	)
)

func init() {
	prometheus.MustRegister(
		HTTPRequestsTotal,
		HTTPRequestDuration,
		SyncTaskTotal,
		SyncTaskDuration,
	)
}
