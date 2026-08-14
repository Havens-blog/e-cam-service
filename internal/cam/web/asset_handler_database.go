package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

// ==================== RDS 关系型数据库 ====================

// ListRDS 获取RDS实例列表
// @Summary 获取RDS实例列表
// @Description 从数据库获取已同步的RDS实例列表
// @Tags 资产管理-RDS
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "实例状态"
// @Param name query string false "实例名称(模糊搜索)"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} AssetListResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/assets/rds [get]
func (h *AssetHandler) ListRDS(ctx *gin.Context) {
	h.listAssets(ctx, "rds")
}

// GetRDS 获取RDS实例详情
// @Summary 获取RDS实例详情
// @Description 从数据库获取指定RDS实例的详细信息
// @Tags 资产管理-RDS
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param asset_id path string true "资产ID(云厂商实例ID)"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} AssetDetailResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Failure 404 {object} ErrorResponse "实例不存在"
// @Router /cam/assets/rds/{asset_id} [get]
func (h *AssetHandler) GetRDS(ctx *gin.Context) {
	h.getAsset(ctx, "rds")
}

// ==================== Redis 缓存 ====================

// ListRedis 获取Redis实例列表
// @Summary 获取Redis实例列表
// @Description 从数据库获取已同步的Redis实例列表
// @Tags 资产管理-Redis
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "实例状态"
// @Param name query string false "实例名称(模糊搜索)"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} AssetListResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/assets/redis [get]
func (h *AssetHandler) ListRedis(ctx *gin.Context) {
	h.listAssets(ctx, "redis")
}

// GetRedis 获取Redis实例详情
// @Summary 获取Redis实例详情
// @Description 从数据库获取指定Redis实例的详细信息
// @Tags 资产管理-Redis
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param asset_id path string true "资产ID(云厂商实例ID)"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} AssetDetailResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Failure 404 {object} ErrorResponse "实例不存在"
// @Router /cam/assets/redis/{asset_id} [get]
func (h *AssetHandler) GetRedis(ctx *gin.Context) {
	h.getAsset(ctx, "redis")
}

// ==================== MongoDB 文档数据库 ====================

// ListMongoDB 获取MongoDB实例列表
// @Summary 获取MongoDB实例列表
// @Description 从数据库获取已同步的MongoDB实例列表
// @Tags 资产管理-MongoDB
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "实例状态"
// @Param name query string false "实例名称(模糊搜索)"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} AssetListResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/assets/mongodb [get]
func (h *AssetHandler) ListMongoDB(ctx *gin.Context) {
	h.listAssets(ctx, "mongodb")
}

// GetMongoDB 获取MongoDB实例详情
// @Summary 获取MongoDB实例详情
// @Description 从数据库获取指定MongoDB实例的详细信息
// @Tags 资产管理-MongoDB
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param asset_id path string true "资产ID(云厂商实例ID)"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} AssetDetailResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Failure 404 {object} ErrorResponse "实例不存在"
// @Router /cam/assets/mongodb/{asset_id} [get]
func (h *AssetHandler) GetMongoDB(ctx *gin.Context) {
	h.getAsset(ctx, "mongodb")
}

// ==================== Kafka 消息队列 ====================

// ListKafka 获取Kafka实例列表
// @Summary 获取Kafka实例列表
// @Description 从数据库获取已同步的Kafka实例列表
// @Tags 资产管理-Kafka
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "实例状态"
// @Param name query string false "实例名称(模糊搜索)"
// @Param vpc_id query string false "VPC ID"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} AssetListResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/assets/kafka [get]
func (h *AssetHandler) ListKafka(ctx *gin.Context) {
	// 从中间件注入的上下文获取租户ID
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	status := ctx.Query("status")
	name := ctx.Query("name")
	accountIDStr := ctx.Query("account_id")

	// Kafka 特有过滤参数
	vpcID := ctx.Query("vpc_id")

	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	var accountID int64
	if accountIDStr != "" {
		accountID, _ = strconv.ParseInt(accountIDStr, 10, 64)
	}

	// 构建属性过滤条件
	attributes := make(map[string]interface{})
	if region != "" {
		attributes["region"] = region
	}
	if status != "" {
		attributes["status"] = status
	}
	if vpcID != "" {
		attributes["vpc_id"] = vpcID
	}

	filter := domain.InstanceFilter{
		ModelUID:   "kafka",
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

// GetKafka 获取Kafka实例详情
// @Summary 获取Kafka实例详情
// @Description 从数据库获取指定Kafka实例的详细信息
// @Tags 资产管理-Kafka
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param asset_id path string true "资产ID(实例ID)"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} AssetDetailResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Failure 404 {object} ErrorResponse "实例不存在"
// @Router /cam/assets/kafka/{asset_id} [get]
func (h *AssetHandler) GetKafka(ctx *gin.Context) {
	h.getAsset(ctx, "kafka")
}

// ==================== Elasticsearch 搜索服务 ====================

// ListElasticsearch 获取Elasticsearch实例列表
// @Summary 获取Elasticsearch实例列表
// @Description 从数据库获取已同步的Elasticsearch实例列表
// @Tags 资产管理-Elasticsearch
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "实例状态"
// @Param name query string false "实例名称(模糊搜索)"
// @Param version query string false "ES版本"
// @Param vpc_id query string false "VPC ID"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} AssetListResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/assets/elasticsearch [get]
func (h *AssetHandler) ListElasticsearch(ctx *gin.Context) {
	// 从中间件注入的上下文获取租户ID
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	status := ctx.Query("status")
	name := ctx.Query("name")
	accountIDStr := ctx.Query("account_id")

	// Elasticsearch 特有过滤参数
	version := ctx.Query("version")
	vpcID := ctx.Query("vpc_id")

	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	var accountID int64
	if accountIDStr != "" {
		accountID, _ = strconv.ParseInt(accountIDStr, 10, 64)
	}

	// 构建属性过滤条件
	attributes := make(map[string]interface{})
	if region != "" {
		attributes["region"] = region
	}
	if status != "" {
		attributes["status"] = status
	}
	if version != "" {
		attributes["version"] = version
	}
	if vpcID != "" {
		attributes["vpc_id"] = vpcID
	}

	filter := domain.InstanceFilter{
		ModelUID:   "elasticsearch",
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

// GetElasticsearch 获取Elasticsearch实例详情
// @Summary 获取Elasticsearch实例详情
// @Description 从数据库获取指定Elasticsearch实例的详细信息
// @Tags 资产管理-Elasticsearch
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param asset_id path string true "资产ID(实例ID)"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} AssetDetailResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Failure 404 {object} ErrorResponse "实例不存在"
// @Router /cam/assets/elasticsearch/{asset_id} [get]
func (h *AssetHandler) GetElasticsearch(ctx *gin.Context) {
	h.getAsset(ctx, "elasticsearch")
}
