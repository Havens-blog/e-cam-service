package web

import (
	"strconv"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/middleware"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/gin-gonic/gin"
)

// AssetHandler 资产HTTP处理器
// 按资产类型提供RESTful风格的API
type AssetHandler struct {
	instanceSvc service.InstanceService
}

// NewAssetHandler 创建资产处理器
func NewAssetHandler(instanceSvc service.InstanceService) *AssetHandler {
	return &AssetHandler{
		instanceSvc: instanceSvc,
	}
}

// RegisterRoutes 注册资产路由 (旧方式，保留兼容)
// Deprecated: 请使用 RegisterRoutesWithGroup
func (h *AssetHandler) RegisterRoutes(rg *gin.RouterGroup) {
	assetsGroup := rg.Group("/assets")
	h.registerAssetRoutes(assetsGroup)
}

// RegisterRoutesWithGroup 注册资产路由到指定路由组
// 用于外部已配置好中间件的路由组
func (h *AssetHandler) RegisterRoutesWithGroup(assetsGroup *gin.RouterGroup) {
	h.registerAssetRoutes(assetsGroup)
}

// registerAssetRoutes 内部路由注册方法
func (h *AssetHandler) registerAssetRoutes(assetsGroup *gin.RouterGroup) {
	// 统一搜索
	assetsGroup.GET("/search", h.Search)

	// ECS 云虚拟机
	assetsGroup.GET("/ecs", h.ListECS)
	assetsGroup.GET("/ecs/:asset_id", h.GetECS)

	// RDS 关系型数据库
	assetsGroup.GET("/rds", h.ListRDS)
	assetsGroup.GET("/rds/:asset_id", h.GetRDS)

	// Redis 缓存
	assetsGroup.GET("/redis", h.ListRedis)
	assetsGroup.GET("/redis/:asset_id", h.GetRedis)

	// MongoDB 文档数据库
	assetsGroup.GET("/mongodb", h.ListMongoDB)
	assetsGroup.GET("/mongodb/:asset_id", h.GetMongoDB)

	// VPC 虚拟私有云
	assetsGroup.GET("/vpc", h.ListVPC)
	assetsGroup.GET("/vpc/:asset_id", h.GetVPC)

	// EIP 弹性公网IP
	assetsGroup.GET("/eip", h.ListEIP)
	assetsGroup.GET("/eip/:asset_id", h.GetEIP)

	// NAS 文件存储
	assetsGroup.GET("/nas", h.ListNAS)
	assetsGroup.GET("/nas/:asset_id", h.GetNAS)

	// OSS 对象存储
	assetsGroup.GET("/oss", h.ListOSS)
	assetsGroup.GET("/oss/:asset_id", h.GetOSS)

	// Kafka 消息队列
	assetsGroup.GET("/kafka", h.ListKafka)
	assetsGroup.GET("/kafka/:asset_id", h.GetKafka)

	// Elasticsearch 搜索服务
	assetsGroup.GET("/elasticsearch", h.ListElasticsearch)
	assetsGroup.GET("/elasticsearch/:asset_id", h.GetElasticsearch)
}

// ==================== ECS 云虚拟机 ====================

// Search 统一搜索资产
// @Summary 统一搜索资产
// @Description 跨资产类型搜索，支持按关键词匹配资产ID、名称、IP地址等。返回匹配信息供前端高亮显示
// @Tags 资产管理-搜索
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param keyword query string true "搜索关键词(匹配资产ID、名称、IP等)"
// @Param types query string false "资产类型(逗号分隔: ecs,rds,redis,mongodb,vpc,eip)"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param account_id query int false "云账号ID"
// @Param region query string false "地域"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} SearchResultResponse "成功"
// @Failure 400 {object} ErrorResponse "参数错误"
// @Router /cam/assets/search [get]
func (h *AssetHandler) Search(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	keyword := ctx.Query("keyword")
	typesStr := ctx.Query("types")
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	accountIDStr := ctx.Query("account_id")

	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	var accountID int64
	if accountIDStr != "" {
		accountID, _ = strconv.ParseInt(accountIDStr, 10, 64)
	}

	// 解析资产类型
	var assetTypes []string
	if typesStr != "" {
		assetTypes = strings.Split(typesStr, ",")
	}

	filter := domain.SearchFilter{
		TenantID:   tenantID,
		Keyword:    keyword,
		AssetTypes: assetTypes,
		Provider:   provider,
		AccountID:  accountID,
		Region:     region,
		Offset:     int64(offset),
		Limit:      int64(limit),
	}

	instances, total, err := h.instanceSvc.Search(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	// 转换为搜索结果，包含匹配信息
	items := h.toSearchResultVOs(instances, keyword)

	ctx.JSON(200, Result(SearchListResp{
		Items:   items,
		Total:   total,
		Keyword: keyword,
	}))
}

// ListECS 获取ECS实例列表
// @Summary 获取云虚拟机列表
// @Description 从数据库获取已同步的云虚拟机实例列表，支持按IP地址过滤
// @Tags 资产管理-ECS
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "实例状态"
// @Param name query string false "实例名称(模糊搜索)"
// @Param private_ip query string false "内网IP"
// @Param public_ip query string false "公网IP"
// @Param vpc_id query string false "VPC ID"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} AssetListResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/assets/ecs [get]
func (h *AssetHandler) ListECS(ctx *gin.Context) {
	// 从中间件注入的上下文获取租户ID
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	status := ctx.Query("status")
	name := ctx.Query("name")
	accountIDStr := ctx.Query("account_id")

	// ECS 特有过滤参数
	privateIP := ctx.Query("private_ip")
	publicIP := ctx.Query("public_ip")
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
	// ECS 特有属性过滤
	if privateIP != "" {
		attributes["private_ip"] = privateIP
	}
	if publicIP != "" {
		attributes["public_ip"] = publicIP
	}
	if vpcID != "" {
		attributes["vpc_id"] = vpcID
	}

	filter := domain.InstanceFilter{
		ModelUID:   "ecs",
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

// GetECS 获取ECS实例详情
// @Summary 获取云虚拟机详情
// @Description 从数据库获取指定云虚拟机实例的详细信息
// @Tags 资产管理-ECS
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param asset_id path string true "资产ID(云厂商实例ID)"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} AssetDetailResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Failure 404 {object} ErrorResponse "实例不存在"
// @Router /cam/assets/ecs/{asset_id} [get]
func (h *AssetHandler) GetECS(ctx *gin.Context) {
	h.getAsset(ctx, "ecs")
}

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

// ==================== VPC 虚拟私有云 ====================

// ListVPC 获取VPC列表
// @Summary 获取VPC列表
// @Description 从数据库获取已同步的VPC列表
// @Tags 资产管理-VPC
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "状态"
// @Param name query string false "VPC名称(模糊搜索)"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} AssetListResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/assets/vpc [get]
func (h *AssetHandler) ListVPC(ctx *gin.Context) {
	h.listAssets(ctx, "vpc")
}

// GetVPC 获取VPC详情
// @Summary 获取VPC详情
// @Description 从数据库获取指定VPC的详细信息
// @Tags 资产管理-VPC
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param asset_id path string true "资产ID(VPC ID)"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} AssetDetailResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Failure 404 {object} ErrorResponse "VPC不存在"
// @Router /cam/assets/vpc/{asset_id} [get]
func (h *AssetHandler) GetVPC(ctx *gin.Context) {
	h.getAsset(ctx, "vpc")
}

// ==================== EIP 弹性公网IP ====================

// ListEIP 获取EIP列表
// @Summary 获取弹性公网IP列表
// @Description 从数据库获取已同步的弹性公网IP列表，支持按绑定实例类型、IP地址、VPC等过滤
// @Tags 资产管理-EIP
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "状态(InUse/Available)"
// @Param name query string false "EIP名称(模糊搜索)"
// @Param ip_address query string false "IP地址(精确匹配)"
// @Param instance_id query string false "绑定的实例ID"
// @Param instance_type query string false "绑定的实例类型(EcsInstance/SlbInstance/Nat/HaVip/NetworkInterface)"
// @Param vpc_id query string false "VPC ID"
// @Param isp query string false "线路类型(BGP/BGP_PRO/ChinaTelecom/ChinaUnicom/ChinaMobile)"
// @Param bindable query string false "绑定状态: bound(已绑定)/unbound(未绑定)"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} AssetListResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/assets/eip [get]
func (h *AssetHandler) ListEIP(ctx *gin.Context) {
	// 从中间件注入的上下文获取租户ID
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	status := ctx.Query("status")
	name := ctx.Query("name")
	accountIDStr := ctx.Query("account_id")

	// EIP 特有过滤参数
	ipAddress := ctx.Query("ip_address")
	instanceID := ctx.Query("instance_id")
	instanceType := ctx.Query("instance_type")
	vpcID := ctx.Query("vpc_id")
	isp := ctx.Query("isp")
	bindable := ctx.Query("bindable")

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
	// EIP 特有属性过滤
	if ipAddress != "" {
		attributes["ip_address"] = ipAddress
	}
	if instanceID != "" {
		attributes["instance_id"] = instanceID
	}
	if instanceType != "" {
		attributes["instance_type"] = instanceType
	}
	if vpcID != "" {
		attributes["vpc_id"] = vpcID
	}
	if isp != "" {
		attributes["isp"] = isp
	}
	// 绑定状态过滤: bound -> InUse, unbound -> Available
	if bindable == "bound" {
		attributes["status"] = "InUse"
	} else if bindable == "unbound" {
		attributes["status"] = "Available"
	}

	filter := domain.InstanceFilter{
		ModelUID:   "eip",
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

// GetEIP 获取EIP详情
// @Summary 获取弹性公网IP详情
// @Description 从数据库获取指定弹性公网IP的详细信息
// @Tags 资产管理-EIP
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param asset_id path string true "资产ID(EIP Allocation ID)"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} AssetDetailResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Failure 404 {object} ErrorResponse "EIP不存在"
// @Router /cam/assets/eip/{asset_id} [get]
func (h *AssetHandler) GetEIP(ctx *gin.Context) {
	h.getAsset(ctx, "eip")
}

// ==================== NAS 文件存储 ====================

// ListNAS 获取NAS文件系统列表
// @Summary 获取NAS文件系统列表
// @Description 从数据库获取已同步的NAS文件系统列表
// @Tags 资产管理-NAS
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "状态"
// @Param name query string false "文件系统名称(模糊搜索)"
// @Param file_system_type query string false "文件系统类型(standard/extreme/cpfs)"
// @Param protocol_type query string false "协议类型(NFS/SMB)"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} AssetListResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/assets/nas [get]
func (h *AssetHandler) ListNAS(ctx *gin.Context) {
	// 从中间件注入的上下文获取租户ID
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	status := ctx.Query("status")
	name := ctx.Query("name")
	accountIDStr := ctx.Query("account_id")

	// NAS 特有过滤参数
	fileSystemType := ctx.Query("file_system_type")
	protocolType := ctx.Query("protocol_type")

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
	if fileSystemType != "" {
		attributes["file_system_type"] = fileSystemType
	}
	if protocolType != "" {
		attributes["protocol_type"] = protocolType
	}

	filter := domain.InstanceFilter{
		ModelUID:   "nas",
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

// GetNAS 获取NAS文件系统详情
// @Summary 获取NAS文件系统详情
// @Description 从数据库获取指定NAS文件系统的详细信息
// @Tags 资产管理-NAS
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param asset_id path string true "资产ID(文件系统ID)"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} AssetDetailResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Failure 404 {object} ErrorResponse "文件系统不存在"
// @Router /cam/assets/nas/{asset_id} [get]
func (h *AssetHandler) GetNAS(ctx *gin.Context) {
	h.getAsset(ctx, "nas")
}

// ==================== OSS 对象存储 ====================

// ListOSS 获取OSS存储桶列表
// @Summary 获取OSS存储桶列表
// @Description 从数据库获取已同步的OSS存储桶列表
// @Tags 资产管理-OSS
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param name query string false "存储桶名称(模糊搜索)"
// @Param storage_class query string false "存储类型(Standard/IA/Archive)"
// @Param acl query string false "访问权限(private/public-read/public-read-write)"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} AssetListResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/assets/oss [get]
func (h *AssetHandler) ListOSS(ctx *gin.Context) {
	// 从中间件注入的上下文获取租户ID
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	name := ctx.Query("name")
	accountIDStr := ctx.Query("account_id")

	// OSS 特有过滤参数
	storageClass := ctx.Query("storage_class")
	acl := ctx.Query("acl")

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
	if storageClass != "" {
		attributes["storage_class"] = storageClass
	}
	if acl != "" {
		attributes["acl"] = acl
	}

	filter := domain.InstanceFilter{
		ModelUID:   "oss",
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

// GetOSS 获取OSS存储桶详情
// @Summary 获取OSS存储桶详情
// @Description 从数据库获取指定OSS存储桶的详细信息
// @Tags 资产管理-OSS
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param asset_id path string true "资产ID(存储桶名称)"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} AssetDetailResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Failure 404 {object} ErrorResponse "存储桶不存在"
// @Router /cam/assets/oss/{asset_id} [get]
func (h *AssetHandler) GetOSS(ctx *gin.Context) {
	h.getAsset(ctx, "oss")
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

// ==================== 通用方法 ====================

// listAssets 通用的资产列表查询
func (h *AssetHandler) listAssets(ctx *gin.Context, assetType string) {
	// 从中间件注入的上下文获取租户ID
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	status := ctx.Query("status")
	name := ctx.Query("name")
	accountIDStr := ctx.Query("account_id")

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

	filter := domain.InstanceFilter{
		ModelUID:   assetType, // DAO层会自动转换为正则匹配
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

// getAsset 通用的资产详情查询
func (h *AssetHandler) getAsset(ctx *gin.Context, assetType string) {
	assetID := ctx.Param("asset_id")
	if assetID == "" {
		ctx.JSON(400, ErrorResultWithMsg(errs.ParamsError, "asset_id is required"))
		return
	}

	// 从中间件注入的上下文获取租户ID
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")

	// 通过 asset_id 搜索
	filter := domain.InstanceFilter{
		AssetID:  assetID,
		TenantID: tenantID,
		Provider: provider,
		Limit:    10,
	}

	instances, _, err := h.instanceSvc.List(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	// 过滤出匹配资产类型的实例
	for _, inst := range instances {
		if matchAssetType(inst.ModelUID, assetType) {
			ctx.JSON(200, Result(h.toUnifiedAssetVO(inst)))
			return
		}
	}

	ctx.JSON(404, ErrorResult(errs.InstanceNotFound))
}

// matchAssetType 检查 model_uid 是否匹配资产类型
func matchAssetType(modelUID, assetType string) bool {
	switch assetType {
	case "ecs":
		return modelUID == "ecs" || modelUID == "cloud_vm" ||
			modelUID == "aliyun_ecs" ||
			modelUID == "aws_ecs" ||
			modelUID == "huawei_ecs" ||
			modelUID == "tencent_ecs" ||
			modelUID == "volcano_ecs"
	case "rds":
		return modelUID == "rds" || modelUID == "cloud_rds" ||
			modelUID == "aliyun_rds" ||
			modelUID == "aws_rds" ||
			modelUID == "huawei_rds" ||
			modelUID == "tencent_rds" ||
			modelUID == "volcano_rds"
	case "redis":
		return modelUID == "redis" || modelUID == "cloud_redis" ||
			modelUID == "aliyun_redis" ||
			modelUID == "aws_redis" ||
			modelUID == "huawei_redis" ||
			modelUID == "tencent_redis" ||
			modelUID == "volcano_redis"
	case "mongodb":
		return modelUID == "mongodb" || modelUID == "cloud_mongodb" ||
			modelUID == "aliyun_mongodb" ||
			modelUID == "aws_mongodb" ||
			modelUID == "huawei_mongodb" ||
			modelUID == "tencent_mongodb" ||
			modelUID == "volcano_mongodb"
	case "vpc":
		return modelUID == "vpc" || modelUID == "cloud_vpc" ||
			modelUID == "aliyun_vpc" ||
			modelUID == "aws_vpc" ||
			modelUID == "huawei_vpc" ||
			modelUID == "tencent_vpc" ||
			modelUID == "volcano_vpc"
	case "eip":
		return modelUID == "eip" || modelUID == "cloud_eip" ||
			modelUID == "aliyun_eip" ||
			modelUID == "aws_eip" ||
			modelUID == "huawei_eip" ||
			modelUID == "tencent_eip" ||
			modelUID == "volcano_eip"
	case "nas":
		return modelUID == "nas" || modelUID == "cloud_nas" ||
			modelUID == "aliyun_nas" ||
			modelUID == "aws_nas" ||
			modelUID == "huawei_nas" ||
			modelUID == "tencent_nas" ||
			modelUID == "volcano_nas"
	case "oss":
		return modelUID == "oss" || modelUID == "cloud_oss" ||
			modelUID == "aliyun_oss" ||
			modelUID == "aws_oss" ||
			modelUID == "huawei_oss" ||
			modelUID == "tencent_oss" ||
			modelUID == "volcano_oss"
	case "kafka":
		return modelUID == "kafka" || modelUID == "cloud_kafka" ||
			modelUID == "aliyun_kafka" ||
			modelUID == "aws_kafka" ||
			modelUID == "huawei_kafka" ||
			modelUID == "tencent_kafka" ||
			modelUID == "volcano_kafka"
	case "elasticsearch":
		return modelUID == "elasticsearch" || modelUID == "cloud_elasticsearch" ||
			modelUID == "aliyun_elasticsearch" ||
			modelUID == "aws_elasticsearch" ||
			modelUID == "huawei_elasticsearch" ||
			modelUID == "tencent_elasticsearch" ||
			modelUID == "volcano_elasticsearch"
	}
	return false
}

// ==================== 响应结构体 ====================

// UnifiedAssetListResp 统一资产列表响应
type UnifiedAssetListResp struct {
	Items []UnifiedAssetVO `json:"items"`
	Total int64            `json:"total"`
}

// UnifiedAssetVO 统一资产视图对象
type UnifiedAssetVO struct {
	ID         int64                  `json:"id"`
	AssetID    string                 `json:"asset_id"`
	AssetName  string                 `json:"asset_name"`
	AssetType  string                 `json:"asset_type"`
	TenantID   string                 `json:"tenant_id"`
	AccountID  int64                  `json:"account_id"`
	Provider   string                 `json:"provider"`
	Region     string                 `json:"region"`
	Status     string                 `json:"status"`
	Attributes map[string]interface{} `json:"attributes"`
	CreateTime int64                  `json:"create_time"`
	UpdateTime int64                  `json:"update_time"`
}

func (h *AssetHandler) toUnifiedAssetVOs(instances []domain.Instance) []UnifiedAssetVO {
	vos := make([]UnifiedAssetVO, len(instances))
	for i, inst := range instances {
		vos[i] = h.toUnifiedAssetVO(inst)
	}
	return vos
}

func (h *AssetHandler) toUnifiedAssetVO(inst domain.Instance) UnifiedAssetVO {
	provider := ""
	region := ""
	status := ""
	assetType := extractAssetType(inst.ModelUID)

	if inst.Attributes != nil {
		if p, ok := inst.Attributes["provider"].(string); ok {
			provider = p
		}
		if r, ok := inst.Attributes["region"].(string); ok {
			region = r
		}
		if s, ok := inst.Attributes["status"].(string); ok {
			status = s
		}
	}

	return UnifiedAssetVO{
		ID:         inst.ID,
		AssetID:    inst.AssetID,
		AssetName:  inst.AssetName,
		AssetType:  assetType,
		TenantID:   inst.TenantID,
		AccountID:  inst.AccountID,
		Provider:   provider,
		Region:     region,
		Status:     status,
		Attributes: inst.Attributes,
		CreateTime: inst.CreateTime.UnixMilli(),
		UpdateTime: inst.UpdateTime.UnixMilli(),
	}
}

// extractAssetType 从 model_uid 提取资产类型
func extractAssetType(modelUID string) string {
	// cloud_vm -> ecs, cloud_rds -> rds, etc.
	switch modelUID {
	case "cloud_vm":
		return "ecs"
	case "cloud_rds":
		return "rds"
	case "cloud_redis":
		return "redis"
	case "cloud_mongodb":
		return "mongodb"
	case "cloud_vpc":
		return "vpc"
	case "cloud_eip":
		return "eip"
	case "cloud_nas":
		return "nas"
	case "cloud_oss":
		return "oss"
	case "cloud_kafka":
		return "kafka"
	case "cloud_elasticsearch":
		return "elasticsearch"
	}
	// aliyun_ecs -> ecs, aws_rds -> rds, etc.
	for _, suffix := range []string{"_ecs", "_rds", "_redis", "_mongodb", "_vpc", "_eip", "_nas", "_oss", "_kafka", "_elasticsearch"} {
		if len(modelUID) > len(suffix) && modelUID[len(modelUID)-len(suffix):] == suffix {
			return suffix[1:] // 去掉前缀下划线
		}
	}
	return modelUID
}

// ==================== 搜索结果结构体 ====================

// SearchListResp 搜索列表响应
type SearchListResp struct {
	Items   []SearchResultVO `json:"items"`
	Total   int64            `json:"total"`
	Keyword string           `json:"keyword"` // 返回搜索关键词，方便前端高亮
}

// SearchResultVO 搜索结果视图对象
type SearchResultVO struct {
	UnifiedAssetVO
	Matches []MatchInfo `json:"matches"` // 匹配信息，用于前端高亮
}

// MatchInfo 匹配信息
type MatchInfo struct {
	Field string `json:"field"` // 匹配的字段名
	Value string `json:"value"` // 匹配的字段值
	Label string `json:"label"` // 字段显示名称
}

// SearchResultResponse 搜索结果响应（用于 Swagger）
type SearchResultResponse struct {
	Code int            `json:"code" example:"0"`
	Msg  string         `json:"msg" example:"success"`
	Data SearchListResp `json:"data"`
}

// toSearchResultVOs 转换为搜索结果，包含匹配信息
func (h *AssetHandler) toSearchResultVOs(instances []domain.Instance, keyword string) []SearchResultVO {
	vos := make([]SearchResultVO, len(instances))
	for i, inst := range instances {
		vos[i] = h.toSearchResultVO(inst, keyword)
	}
	return vos
}

// toSearchResultVO 转换单个实例为搜索结果
func (h *AssetHandler) toSearchResultVO(inst domain.Instance, keyword string) SearchResultVO {
	baseVO := h.toUnifiedAssetVO(inst)
	matches := h.findMatches(inst, keyword)

	return SearchResultVO{
		UnifiedAssetVO: baseVO,
		Matches:        matches,
	}
}

// findMatches 查找匹配的字段
func (h *AssetHandler) findMatches(inst domain.Instance, keyword string) []MatchInfo {
	if keyword == "" {
		return nil
	}

	var matches []MatchInfo
	keywordLower := strings.ToLower(keyword)

	// 检查 asset_id
	if strings.Contains(strings.ToLower(inst.AssetID), keywordLower) {
		matches = append(matches, MatchInfo{
			Field: "asset_id",
			Value: inst.AssetID,
			Label: "资产ID",
		})
	}

	// 检查 asset_name
	if strings.Contains(strings.ToLower(inst.AssetName), keywordLower) {
		matches = append(matches, MatchInfo{
			Field: "asset_name",
			Value: inst.AssetName,
			Label: "资产名称",
		})
	}

	// 检查 attributes 中的常用字段
	if inst.Attributes != nil {
		// 内网IP
		if privateIP, ok := inst.Attributes["private_ip"].(string); ok {
			if strings.Contains(strings.ToLower(privateIP), keywordLower) {
				matches = append(matches, MatchInfo{
					Field: "private_ip",
					Value: privateIP,
					Label: "内网IP",
				})
			}
		}

		// 公网IP
		if publicIP, ok := inst.Attributes["public_ip"].(string); ok {
			if strings.Contains(strings.ToLower(publicIP), keywordLower) {
				matches = append(matches, MatchInfo{
					Field: "public_ip",
					Value: publicIP,
					Label: "公网IP",
				})
			}
		}

		// IP地址 (EIP)
		if ipAddress, ok := inst.Attributes["ip_address"].(string); ok {
			if strings.Contains(strings.ToLower(ipAddress), keywordLower) {
				matches = append(matches, MatchInfo{
					Field: "ip_address",
					Value: ipAddress,
					Label: "IP地址",
				})
			}
		}

		// 连接串 (数据库)
		if connStr, ok := inst.Attributes["connection_string"].(string); ok {
			if strings.Contains(strings.ToLower(connStr), keywordLower) {
				matches = append(matches, MatchInfo{
					Field: "connection_string",
					Value: connStr,
					Label: "连接地址",
				})
			}
		}

		// CIDR块 (VPC)
		if cidr, ok := inst.Attributes["cidr_block"].(string); ok {
			if strings.Contains(strings.ToLower(cidr), keywordLower) {
				matches = append(matches, MatchInfo{
					Field: "cidr_block",
					Value: cidr,
					Label: "CIDR块",
				})
			}
		}
	}

	return matches
}
