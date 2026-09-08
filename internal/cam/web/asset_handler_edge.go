package web

import (
	"context"
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

// CDNCacheConfigService 详情页缓存配置按需查询(web 层只依赖此接口)
type CDNCacheConfigService interface {
	GetCacheConfig(ctx context.Context, tenantID, accountID int64, domainName, domainID string) ([]types.CDNCacheRule, error)
	// GetDomainSettings CDN 域名功能配置全景按需查询（性能优化/访问控制/
	// 流量限制/HTTPS/重定向/回源；仅 account_id+域名标识命中时调用）。
	GetDomainSettings(ctx context.Context, tenantID, accountID int64, domainName, domainID string) (*types.CDNDomainSettings, error)
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
	Domain string              `json:"domain"`
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
