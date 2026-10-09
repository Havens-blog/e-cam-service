// Package web 日志查询 HTTP 面(Phase 1.4,plan.md §4.5)。
//
// 接口:types(字段字典)/ sources(日志源清单)/ search(联邦查询)/
// aggregate(窗口聚合)/ diagnose(WAF 流量诊断,手动触发)。
// 鉴权由全局中间件链承接;组级 RequireTenant(日志按云账号隔离,
// 云账号属租户,租户边界必须在此拒绝)。
package web

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/logquery/service"
	"github.com/Havens-blog/e-cloudx-sdk/logquery"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------
// 字段字典(动态列驱动;后端加字段,前端自动多列)
// ---------------------------------------------------------------------

// FieldDef 统一字段定义(展示名 + 键)。
type FieldDef struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Fixed 是否固定列(时间/云/账号/资源);其余为类型专属列。
	Fixed bool `json:"fixed"`
}

type typeMeta struct {
	Type   logquery.LogType `json:"type"`
	Label  string           `json:"label"`
	Fields []FieldDef       `json:"fields"`
	// WindowHints 前端时间范围约束(与后端一致:CDN 类 7 天,实时类 24h 起步)。
	MaxWindowDays int `json:"max_window_days"`
	// Aggregatable 该类型可聚合字段清单(分组聚合维度白名单;空=探测失败/
	// 无账号,前端回退全量字段字典)。由运行时索引探测填充,非静态字典。
	Aggregatable []string `json:"aggregatable,omitempty"`
}

// fixedFields 所有类型共有固定列。
func fixedFields() []FieldDef {
	return []FieldDef{
		{Key: "meta.cloud", Label: "云", Fixed: true},
		{Key: "meta.account_name", Label: "云账号", Fixed: true},
		{Key: "meta.region", Label: "区域", Fixed: true},
		{Key: "meta.resource_id", Label: "资源", Fixed: true},
		{Key: "timestamp", Label: "时间", Fixed: true},
	}
}

// logTypes 字段字典(与 types.go 三 schema 一一对应;变更需同步)。
var logTypes = []typeMeta{
	{
		Type: logquery.LogTypeCDN, Label: "CDN 访问日志", MaxWindowDays: 7,
		Fields: append(fixedFields(),
			FieldDef{Key: "client_ip", Label: "客户端 IP"},
			FieldDef{Key: "method", Label: "方法"},
			FieldDef{Key: "url", Label: "URL"},
			FieldDef{Key: "host", Label: "域名"},
			FieldDef{Key: "status", Label: "状态码"},
			FieldDef{Key: "bytes_sent", Label: "下行字节"},
			FieldDef{Key: "cache_hit", Label: "缓存命中"},
			FieldDef{Key: "latency_ms", Label: "耗时(ms)"},
			FieldDef{Key: "referer", Label: "Referer"},
			FieldDef{Key: "user_agent", Label: "UA"},
			FieldDef{Key: "edge_node", Label: "边缘节点"},
			FieldDef{Key: "request_id", Label: "请求 ID"},
		),
	},
	{
		Type: logquery.LogTypeWAF, Label: "WAF 日志", MaxWindowDays: 7,
		Fields: append(fixedFields(),
			FieldDef{Key: "client_ip", Label: "客户端 IP"},
			FieldDef{Key: "host", Label: "域名"},
			FieldDef{Key: "uri", Label: "URI"},
			FieldDef{Key: "method", Label: "方法"},
			FieldDef{Key: "rule_name", Label: "规则"},
			FieldDef{Key: "rule_id", Label: "规则 ID"},
			FieldDef{Key: "action", Label: "动作"},
			FieldDef{Key: "severity", Label: "严重度"},
			FieldDef{Key: "status", Label: "状态码"},
			FieldDef{Key: "user_agent", Label: "UA"},
			FieldDef{Key: "geo", Label: "地理"},
		),
	},
	{
		Type: logquery.LogTypeSLB, Label: "负载均衡访问日志", MaxWindowDays: 3,
		Fields: append(fixedFields(),
			FieldDef{Key: "client_ip", Label: "客户端 IP"},
			FieldDef{Key: "method", Label: "方法"},
			FieldDef{Key: "url", Label: "URL"},
			FieldDef{Key: "host", Label: "域名"},
			FieldDef{Key: "status", Label: "状态码"},
			FieldDef{Key: "target_ip", Label: "后端 IP"},
			FieldDef{Key: "latency_ms", Label: "总耗时(ms)"},
			FieldDef{Key: "upstream_latency_ms", Label: "后端耗时(ms)"},
			FieldDef{Key: "upstream_status", Label: "后端状态"},
			FieldDef{Key: "bytes_sent", Label: "下行字节"},
			FieldDef{Key: "tls_protocol", Label: "TLS"},
		),
	},
	{
		Type: logquery.LogTypeAccess, Label: "源站访问日志", MaxWindowDays: 3,
		Fields: append(fixedFields(),
			FieldDef{Key: "client_ip", Label: "客户端 IP"},
			FieldDef{Key: "method", Label: "方法"},
			FieldDef{Key: "url", Label: "URL"},
			FieldDef{Key: "host", Label: "域名"},
			FieldDef{Key: "status", Label: "状态码"},
			FieldDef{Key: "target_ip", Label: "后端 IP"},
			FieldDef{Key: "latency_ms", Label: "总耗时(ms)"},
			FieldDef{Key: "upstream_latency_ms", Label: "后端耗时(ms)"},
			FieldDef{Key: "upstream_status", Label: "后端状态"},
			FieldDef{Key: "bytes_sent", Label: "下行字节"},
			FieldDef{Key: "tls_protocol", Label: "TLS"},
		),
	},
}

// ---------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------

// LogQueryHandler 日志查询 HTTP handler。
type LogQueryHandler struct {
	svc *service.FederationService
}

// NewLogQueryHandler 创建 handler。
func NewLogQueryHandler(svc *service.FederationService) *LogQueryHandler {
	return &LogQueryHandler{svc: svc}
}

// RegisterRoutes 组内注册(挂 /api/v1/cam/logs 前缀)。
func (h *LogQueryHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/types", h.Types)
	g.GET("/sources", h.Sources)
	g.POST("/search", h.Search)
	g.POST("/aggregate", h.Aggregate)
	g.POST("/diagnose", h.Diagnose)
	g.POST("/cache-analyze", h.CacheAnalyze)
}

// Types GET /types 字段字典(逐类型并发探测可聚合字段填充白名单)。
// 探测带进程级索引缓存(30min)+ 服务层 SWR(60s 新鲜/10min 宽限),冷调用后
// 零 API 开销;探测带 6s 硬时限,超时留空 —— 前端回退全量字典,不阻塞类型
// 页加载(白名单是增强,不是可用性前提)。
func (h *LogQueryHandler) Types(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	out := make([]typeMeta, len(logTypes))
	copy(out, logTypes)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 6*time.Second)
	defer cancel()
	type result struct{ fields []string }
	var wg sync.WaitGroup
	for i := range out {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := make(chan result, 1)
			go func() {
				fields, err := h.svc.AggregatableFields(ctx, tenantID, out[i].Type, nil, nil)
				if err != nil || len(fields) == 0 {
					ch <- result{}
					return
				}
				ch <- result{fields: fields}
			}()
			select {
			case r := <-ch:
				out[i].Aggregatable = r.fields
			case <-ctx.Done():
				// 超时:留空,前端回退全量字典;不阻塞整页类型返回
			}
		}()
	}
	wg.Wait()
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": out})
}

// Sources GET /sources?log_type=cdn&clouds=aliyun,aws 日志源清单。
func (h *LogQueryHandler) Sources(c *gin.Context) {
	logType := logquery.LogType(c.Query("log_type"))
	if logType == "" {
		writeError(c, http.StatusBadRequest, "log_type is required")
		return
	}
	tenantID, ok := tenantID(c)
	if !ok {
		return // middleware.RequireTenant 已拒绝,此处防御
	}
	sources, err := h.svc.ListSources(c.Request.Context(), tenantID, logType, parseClouds(c.Query("clouds")), nil)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	if sources == nil {
		sources = []logquery.LogSource{}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": sources})
}

// searchRequest POST /search 请求体。
type searchRequest struct {
	LogType    string                 `json:"log_type" binding:"required"`
	StartTime  int64                  `json:"start_time" binding:"required"`
	EndTime    int64                  `json:"end_time" binding:"required"`
	Query      string                 `json:"query"`
	Clouds     []string               `json:"clouds"`
	AccountIDs []int64                `json:"account_ids"`
	Resources  []string               `json:"resources"`
	Filters    []logquery.FieldFilter `json:"filters"` // 结构化字段筛选(AND 叠加)
	Limit      int                    `json:"limit"`
}

// Search POST /search 联邦查询。
func (h *LogQueryHandler) Search(c *gin.Context) {
	var req searchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	resp, err := h.svc.Search(c.Request.Context(), tenantID, service.SearchRequest{
		LogType:    logquery.LogType(req.LogType),
		StartTime:  req.StartTime,
		EndTime:    req.EndTime,
		Query:      req.Query,
		Clouds:     toProviders(req.Clouds),
		AccountIDs: req.AccountIDs,
		Resources:  req.Resources,
		Filters:    req.Filters,
		Limit:      req.Limit,
	})
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	if resp.Entries == nil {
		resp.Entries = []logquery.LogEntry{}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": resp})
}

// aggregateRequest POST /aggregate 请求体(与 searchRequest 对齐,无 limit)。
type aggregateRequest struct {
	LogType    string                 `json:"log_type" binding:"required"`
	StartTime  int64                  `json:"start_time" binding:"required"`
	EndTime    int64                  `json:"end_time" binding:"required"`
	Query      string                 `json:"query"`
	Clouds     []string               `json:"clouds"`
	AccountIDs []int64                `json:"account_ids"`
	Resources  []string               `json:"resources"`
	Filters    []logquery.FieldFilter `json:"filters"`   // 字段筛选(可下推源生效)
	Dimension  string                 `json:"dimension"` // 分组维度(/types 字段 key)
	Metric     string                 `json:"metric"`    // count/sum_bytes/avg_latency/p99_latency
}

// Aggregate POST /aggregate 窗口内真实聚合(趋势/总数/TopN 下推云引擎,
// 不受采样上限约束;不支持的源显式标注)。
func (h *LogQueryHandler) Aggregate(c *gin.Context) {
	var req aggregateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	resp, err := h.svc.Aggregate(c.Request.Context(), tenantID, service.AggregateRequest{
		LogType:    logquery.LogType(req.LogType),
		StartTime:  req.StartTime,
		EndTime:    req.EndTime,
		Query:      req.Query,
		Clouds:     toProviders(req.Clouds),
		AccountIDs: req.AccountIDs,
		Resources:  req.Resources,
		Filters:    req.Filters,
		Dimension:  req.Dimension,
		Metric:     req.Metric,
	})
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	if resp.Buckets == nil {
		resp.Buckets = []logquery.AggregateBucket{}
	}
	if resp.TopN == nil {
		resp.TopN = []logquery.TopNItem{}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": resp})
}

// diagnoseRequest POST /diagnose 请求体(与 aggregateRequest 对齐,无
// dimension/metric —— 维度集由诊断编排固定;仅 waf 类型开放)。
type diagnoseRequest struct {
	LogType    string                 `json:"log_type" binding:"required"`
	StartTime  int64                  `json:"start_time" binding:"required"`
	EndTime    int64                  `json:"end_time" binding:"required"`
	Query      string                 `json:"query"`
	Clouds     []string               `json:"clouds"`
	AccountIDs []int64                `json:"account_ids"`
	Resources  []string               `json:"resources"`
	Filters    []logquery.FieldFilter `json:"filters"` // 字段筛选(AND 叠加)
}

// Diagnose POST /diagnose WAF 流量诊断(手动触发:当前窗 + 前一等长窗口
// 各一帧聚合,规则引擎判定;SLB/CDN 不开放,返回明确错误)。
func (h *LogQueryHandler) Diagnose(c *gin.Context) {
	var req diagnoseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	resp, err := h.svc.Diagnose(c.Request.Context(), tenantID, service.DiagnoseRequest{
		LogType:    logquery.LogType(req.LogType),
		StartTime:  req.StartTime,
		EndTime:    req.EndTime,
		Query:      req.Query,
		Clouds:     toProviders(req.Clouds),
		AccountIDs: req.AccountIDs,
		Resources:  req.Resources,
		Filters:    req.Filters,
	})
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	if resp.Buckets == nil {
		resp.Buckets = []logquery.AggregateBucket{}
	}
	for _, f := range []*[]logquery.TopNItem{&resp.TopIPs, &resp.TopUAs, &resp.StatusCodes, &resp.Actions} {
		if *f == nil {
			*f = []logquery.TopNItem{}
		}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": resp})
}

// cacheAnalyzeRequest POST /cache-analyze 请求体(与 diagnoseRequest 对齐 +
// confirm;维度集由缓存分析编排固定 —— 当前窗 5 维度组 + 前窗 1 帧;仅 cdn
// 类型开放,独立 feature flag 默认关)。
type cacheAnalyzeRequest struct {
	LogType    string                 `json:"log_type" binding:"required"`
	StartTime  int64                  `json:"start_time" binding:"required"`
	EndTime    int64                  `json:"end_time" binding:"required"`
	Query      string                 `json:"query"`
	Clouds     []string               `json:"clouds"`
	AccountIDs []int64                `json:"account_ids"`
	Resources  []string               `json:"resources"`
	Filters    []logquery.FieldFilter `json:"filters"` // 字段筛选(AND 叠加)
	Confirm    bool                   `json:"confirm"` // 预估扫描量超限后的人工确认
}

// CacheAnalyze POST /cache-analyze CDN 缓存分析(手动触发:当前窗 5 维度组 +
// 前一等长窗口 1 帧聚合,规则引擎判定;仅 cdn 开放,SLB/WAF 返回明确错误;
// feature flag LOGQUERY_CACHE_ANALYZE_ENABLED 默认关,关闭时明确报错)。
func (h *LogQueryHandler) CacheAnalyze(c *gin.Context) {
	var req cacheAnalyzeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	resp, err := h.svc.CacheAnalyze(c.Request.Context(), tenantID, service.CacheAnalyzeRequest{
		LogType:    logquery.LogType(req.LogType),
		StartTime:  req.StartTime,
		EndTime:    req.EndTime,
		Query:      req.Query,
		Clouds:     toProviders(req.Clouds),
		AccountIDs: req.AccountIDs,
		Resources:  req.Resources,
		Filters:    req.Filters,
		Confirm:    req.Confirm,
	})
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	if resp.Sources == nil {
		resp.Sources = []service.AggregateSourceOutcome{}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": resp})
}

// ---------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------

// tenantID 从会话取租户(middleware.RequireTenant 已保证非 0)。
func tenantID(c *gin.Context) (int64, bool) {
	id := middleware.GetTenantID(c)
	if id == 0 {
		writeError(c, http.StatusForbidden, "tenant context required")
		return 0, false
	}
	return id, true
}

// parseClouds "aliyun,aws" -> []CloudProvider。
func parseClouds(raw string) []domain.CloudProvider {
	var out []domain.CloudProvider
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, domain.CloudProvider(p))
		}
	}
	return out
}

// toProviders []string -> []CloudProvider。
func toProviders(raw []string) []domain.CloudProvider {
	out := make([]domain.CloudProvider, 0, len(raw))
	for _, p := range raw {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, domain.CloudProvider(p))
		}
	}
	return out
}

// writeError 错误信封(与 cert/web.WriteAPIError 同构;独立实现避免反向依赖)。
func writeError(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"code": status, "msg": msg, "data": nil})
}
