// Package web 日志查询 HTTP 面(Phase 1.4,plan.md §4.5)。
//
// 三接口:types(字段字典)/ sources(日志源清单)/ search(联邦查询)。
// 鉴权由全局中间件链承接;组级 RequireTenant(日志按云账号隔离,
// 云账号属租户,租户边界必须在此拒绝)。
package web

import (
	"net/http"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/logquery/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/logquery"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
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
}

// Types GET /types 字段字典。
func (h *LogQueryHandler) Types(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": logTypes})
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
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": sources})
}

// searchRequest POST /search 请求体。
type searchRequest struct {
	LogType    string   `json:"log_type" binding:"required"`
	StartTime  int64    `json:"start_time" binding:"required"`
	EndTime    int64    `json:"end_time" binding:"required"`
	Query      string   `json:"query"`
	Clouds     []string `json:"clouds"`
	AccountIDs []int64  `json:"account_ids"`
	Resources  []string `json:"resources"`
	Limit      int      `json:"limit"`
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
		Limit:      req.Limit,
	})
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	if resp.Entries == nil {
		resp.Entries = []logquery.LogEntry{}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": resp})
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
