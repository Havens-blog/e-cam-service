package web

import (
	"context"
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cloudx-sdk"
	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

// CDNCacheConfigService CDN 查询类接口(web 层只依赖此接口):
// 缓存配置/功能配置为按需实时查询(厂商 API),指标类为纯读本地指标表
type CDNCacheConfigService interface {
	GetCacheConfig(ctx context.Context, tenantID, accountID int64, domainName, domainID string) ([]types.CDNCacheRule, error)
	// GetDomainSettings CDN 域名功能配置全景按需查询（性能优化/访问控制/
	// 流量限制/HTTPS/重定向/回源；仅 account_id+域名标识命中时调用）。
	GetDomainSettings(ctx context.Context, tenantID, accountID int64, domainName, domainID string) (*types.CDNDomainSettings, error)
	// GetDomainMetrics CDN 域名近 N 天单日指标(只读指标表,租户隔离)
	GetDomainMetrics(ctx context.Context, tenantID int64, domainName string, days int) ([]types.CDNMetric, error)
	// GetTopDomains CDN 近 N 天流量 Top 域名(只读指标表,租户隔离)
	GetTopDomains(ctx context.Context, tenantID int64, days, limit int) ([]types.CDNMetricTopRow, error)
}

// ListCDN 获取CDN加速域名列表
func (h *AssetHandler) ListCDN(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	status := ctx.Query("status")
	name := ctx.Query("name")
	accountIDStr := ctx.Query("account_id")

	// CDN 特有过滤参数
	businessType := ctx.Query("business_type")
	serviceArea := ctx.Query("service_area")
	httpsEnabled := ctx.Query("https_enabled")

	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	var accountID int64
	if accountIDStr != "" {
		accountID, _ = strconv.ParseInt(accountIDStr, 10, 64)
	}

	attributes := make(map[string]interface{})
	if region != "" {
		attributes["region"] = region
	}
	// 状态/业务类型/服务区域统一枚举 → $in 历史原始值,
	// 归一化重同步前后两代数据都能命中
	if status != "" {
		attributes["status"] = bson.M{"$in": cloudx.StatusVariants(status)}
	}
	if businessType != "" {
		attributes["business_type"] = bson.M{"$in": cloudx.BusinessTypeVariants(businessType)}
	}
	if serviceArea != "" {
		attributes["service_area"] = bson.M{"$in": cloudx.ServiceAreaVariants(serviceArea)}
	}
	if httpsEnabled != "" {
		attributes["https_enabled"] = httpsEnabled == "true"
	}

	filter := domain.InstanceFilter{
		ModelUID:   "cdn",
		TenantID:   tenantID,
		AccountID:  accountID,
		AssetName:  name,
		Provider:   provider,
		Attributes: attributes,
		Offset:     int64(offset),
		Limit:      int64(limit),
	}

	instances, total, err := h.instanceSvc.List(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(UnifiedAssetListResp{
		Items: h.toUnifiedAssetVOs(instances),
		Total: total,
	}))
}

// GetCDN 获取CDN加速域名详情
func (h *AssetHandler) GetCDN(ctx *gin.Context) {
	h.getAsset(ctx, "cdn")
}

// CDNCacheConfigResp CDN缓存配置响应
type CDNCacheConfigResp struct {
	Domain string               `json:"domain"`
	Rules  []types.CDNCacheRule `json:"rules"`
}

// GetCDNCacheConfig 按需查询 CDN 域名缓存配置(实时经厂商 API)
func (h *AssetHandler) GetCDNCacheConfig(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	accountID, _ := strconv.ParseInt(ctx.Query("account_id"), 10, 64)
	domainName := ctx.Query("domain_name")
	domainID := ctx.Query("domain_id")

	if accountID <= 0 || (domainName == "" && domainID == "") {
		ctx.JSON(400, ErrorResultWithMsg(errs.FieldInvalid, "缺少 account_id 或域名标识"))
		return
	}

	rules, err := h.cdnQuery.GetCacheConfig(ctx.Request.Context(), tenantID, accountID, domainName, domainID)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(CDNCacheConfigResp{
		Domain: domainName,
		Rules:  rules,
	}))
}

// GetCDNDomainSettings 按需查询 CDN 域名功能配置全景(性能优化/访问控制/
// 流量限制/HTTPS/重定向/回源,实时经厂商 API)
func (h *AssetHandler) GetCDNDomainSettings(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	accountID, _ := strconv.ParseInt(ctx.Query("account_id"), 10, 64)
	domainName := ctx.Query("domain_name")
	domainID := ctx.Query("domain_id")

	if accountID <= 0 || (domainName == "" && domainID == "") {
		ctx.JSON(400, ErrorResultWithMsg(errs.FieldInvalid, "缺少 account_id 或域名标识"))
		return
	}

	settings, err := h.cdnQuery.GetDomainSettings(ctx.Request.Context(), tenantID, accountID, domainName, domainID)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(settings))
}

// CDNMetricItem CDN 单日指标点
type CDNMetricItem struct {
	Date      string  `json:"date"`      // YYYY-MM-DD
	Bytes     int64   `json:"bytes"`     // 当日流量(字节)
	Bandwidth int64   `json:"bandwidth"` // 当日带宽峰值(bps)
	HitRate   float64 `json:"hit_rate"`  // 命中率 0-1;-1=厂商未提供
}

// CDNDomainMetricsResp GET /assets/cdn/metrics 响应
type CDNDomainMetricsResp struct {
	Domain string          `json:"domain"`
	Items  []CDNMetricItem `json:"items"`
}

// CDNTopDomainItem Top 域名流量项
type CDNTopDomainItem struct {
	Domain string `json:"domain"`
	Bytes  int64  `json:"bytes"` // 近 N 天流量合计(字节)
}

// CDNTopDomainsResp GET /assets/cdn/top 响应
type CDNTopDomainsResp struct {
	Items []CDNTopDomainItem `json:"items"`
}

// GetCDNDomainMetrics GET /assets/cdn/metrics?domain_name=&days=30
// 查询 CDN 域名近 N 天单日指标。纯读指标表(采集任务落库),不走厂商 API。
func (h *AssetHandler) GetCDNDomainMetrics(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	domainName := ctx.Query("domain_name")
	if domainName == "" {
		ctx.JSON(400, ErrorResultWithMsg(errs.FieldInvalid, "缺少 domain_name"))
		return
	}
	days, err := strconv.Atoi(ctx.DefaultQuery("days", "30"))
	if err != nil || days <= 0 {
		ctx.JSON(400, ErrorResultWithMsg(errs.FieldInvalid, "days 必须为正整数"))
		return
	}

	metrics, err := h.cdnQuery.GetDomainMetrics(ctx.Request.Context(), tenantID, domainName, days)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	items := make([]CDNMetricItem, 0, len(metrics))
	for _, m := range metrics {
		items = append(items, CDNMetricItem{
			Date:      m.Date,
			Bytes:     m.Bytes,
			Bandwidth: m.Bandwidth,
			HitRate:   m.HitRate,
		})
	}
	ctx.JSON(200, Result(CDNDomainMetricsResp{Domain: domainName, Items: items}))
}

// GetCDNTopDomains GET /assets/cdn/top?metric=bytes&days=7&limit=10
// 查询近 N 天流量 Top 域名。metric 一期仅支持 bytes,其他取值返回 400。
func (h *AssetHandler) GetCDNTopDomains(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	metric := ctx.DefaultQuery("metric", "bytes")
	if metric != "bytes" {
		ctx.JSON(400, ErrorResultWithMsg(errs.FieldInvalid, "metric 仅支持 bytes"))
		return
	}
	days, err := strconv.Atoi(ctx.DefaultQuery("days", "7"))
	if err != nil || days <= 0 {
		ctx.JSON(400, ErrorResultWithMsg(errs.FieldInvalid, "days 必须为正整数"))
		return
	}
	limit, err := strconv.Atoi(ctx.DefaultQuery("limit", "10"))
	if err != nil || limit <= 0 {
		ctx.JSON(400, ErrorResultWithMsg(errs.FieldInvalid, "limit 必须为正整数"))
		return
	}

	rows, err := h.cdnQuery.GetTopDomains(ctx.Request.Context(), tenantID, days, limit)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	items := make([]CDNTopDomainItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, CDNTopDomainItem{Domain: r.Domain, Bytes: r.Bytes})
	}
	ctx.JSON(200, Result(CDNTopDomainsResp{Items: items}))
}

// ListWAF 获取WAF实例列表
func (h *AssetHandler) ListWAF(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	status := ctx.Query("status")
	name := ctx.Query("name")
	accountIDStr := ctx.Query("account_id")

	// WAF 特有过滤参数
	edition := ctx.Query("edition")

	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	var accountID int64
	if accountIDStr != "" {
		accountID, _ = strconv.ParseInt(accountIDStr, 10, 64)
	}

	attributes := make(map[string]interface{})
	if region != "" {
		attributes["region"] = region
	}
	if status != "" {
		attributes["status"] = status
	}
	if edition != "" {
		attributes["edition"] = edition
	}

	filter := domain.InstanceFilter{
		ModelUID:   "waf",
		TenantID:   tenantID,
		AccountID:  accountID,
		AssetName:  name,
		Provider:   provider,
		Attributes: attributes,
		Offset:     int64(offset),
		Limit:      int64(limit),
	}

	instances, total, err := h.instanceSvc.List(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(UnifiedAssetListResp{
		Items: h.toUnifiedAssetVOs(instances),
		Total: total,
	}))
}

// GetWAF 获取WAF实例详情
func (h *AssetHandler) GetWAF(ctx *gin.Context) {
	h.getAsset(ctx, "waf")
}

// ListDDOS 获取DDoS防护实例列表
func (h *AssetHandler) ListDDOS(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	status := ctx.Query("status")
	name := ctx.Query("name")
	accountIDStr := ctx.Query("account_id")

	// DDoS 特有过滤参数
	edition := ctx.Query("edition")

	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	var accountID int64
	if accountIDStr != "" {
		accountID, _ = strconv.ParseInt(accountIDStr, 10, 64)
	}

	attributes := make(map[string]interface{})
	if region != "" {
		attributes["region"] = region
	}
	if status != "" {
		attributes["status"] = status
	}
	if edition != "" {
		attributes["edition"] = edition
	}

	filter := domain.InstanceFilter{
		ModelUID:   "ddos",
		TenantID:   tenantID,
		AccountID:  accountID,
		AssetName:  name,
		Provider:   provider,
		Attributes: attributes,
		Offset:     int64(offset),
		Limit:      int64(limit),
	}

	instances, total, err := h.instanceSvc.List(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(UnifiedAssetListResp{
		Items: h.toUnifiedAssetVOs(instances),
		Total: total,
	}))
}

// GetDDOS 获取DDoS防护实例详情
func (h *AssetHandler) GetDDOS(ctx *gin.Context) {
	h.getAsset(ctx, "ddos")
}
