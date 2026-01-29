package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
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

// RegisterRoutes 注册资产路由
func (h *AssetHandler) RegisterRoutes(rg *gin.RouterGroup) {
	assetsGroup := rg.Group("/assets")
	{
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
	}
}

// ==================== ECS 云虚拟机 ====================

// ListECS 获取ECS实例列表
// @Summary 获取云虚拟机列表
// @Description 从数据库获取已同步的云虚拟机实例列表
// @Tags 资产管理-ECS
// @Accept json
// @Produce json
// @Param tenant_id query string false "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "实例状态"
// @Param name query string false "实例名称(模糊搜索)"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} ginx.Result{data=UnifiedAssetListResp} "成功"
// @Router /cam/assets/ecs [get]
func (h *AssetHandler) ListECS(ctx *gin.Context) {
	h.listAssets(ctx, "ecs")
}

// GetECS 获取ECS实例详情
// @Summary 获取云虚拟机详情
// @Description 从数据库获取指定云虚拟机实例的详细信息
// @Tags 资产管理-ECS
// @Accept json
// @Produce json
// @Param asset_id path string true "资产ID(云厂商实例ID)"
// @Param tenant_id query string false "租户ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} ginx.Result{data=UnifiedAssetVO} "成功"
// @Failure 404 {object} ginx.Result "实例不存在"
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
// @Param tenant_id query string false "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "实例状态"
// @Param name query string false "实例名称(模糊搜索)"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} ginx.Result{data=UnifiedAssetListResp} "成功"
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
// @Param asset_id path string true "资产ID(云厂商实例ID)"
// @Param tenant_id query string false "租户ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} ginx.Result{data=UnifiedAssetVO} "成功"
// @Failure 404 {object} ginx.Result "实例不存在"
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
// @Param tenant_id query string false "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "实例状态"
// @Param name query string false "实例名称(模糊搜索)"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} ginx.Result{data=UnifiedAssetListResp} "成功"
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
// @Param asset_id path string true "资产ID(云厂商实例ID)"
// @Param tenant_id query string false "租户ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} ginx.Result{data=UnifiedAssetVO} "成功"
// @Failure 404 {object} ginx.Result "实例不存在"
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
// @Param tenant_id query string false "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "实例状态"
// @Param name query string false "实例名称(模糊搜索)"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} ginx.Result{data=UnifiedAssetListResp} "成功"
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
// @Param asset_id path string true "资产ID(云厂商实例ID)"
// @Param tenant_id query string false "租户ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} ginx.Result{data=UnifiedAssetVO} "成功"
// @Failure 404 {object} ginx.Result "实例不存在"
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
// @Param tenant_id query string false "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "状态"
// @Param name query string false "VPC名称(模糊搜索)"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} ginx.Result{data=UnifiedAssetListResp} "成功"
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
// @Param asset_id path string true "资产ID(VPC ID)"
// @Param tenant_id query string false "租户ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} ginx.Result{data=UnifiedAssetVO} "成功"
// @Failure 404 {object} ginx.Result "VPC不存在"
// @Router /cam/assets/vpc/{asset_id} [get]
func (h *AssetHandler) GetVPC(ctx *gin.Context) {
	h.getAsset(ctx, "vpc")
}

// ==================== EIP 弹性公网IP ====================

// ListEIP 获取EIP列表
// @Summary 获取弹性公网IP列表
// @Description 从数据库获取已同步的弹性公网IP列表
// @Tags 资产管理-EIP
// @Accept json
// @Produce json
// @Param tenant_id query string false "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "状态"
// @Param name query string false "EIP名称(模糊搜索)"
// @Param instance_id query string false "绑定的实例ID"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} ginx.Result{data=UnifiedAssetListResp} "成功"
// @Router /cam/assets/eip [get]
func (h *AssetHandler) ListEIP(ctx *gin.Context) {
	h.listAssets(ctx, "eip")
}

// GetEIP 获取EIP详情
// @Summary 获取弹性公网IP详情
// @Description 从数据库获取指定弹性公网IP的详细信息
// @Tags 资产管理-EIP
// @Accept json
// @Produce json
// @Param asset_id path string true "资产ID(EIP Allocation ID)"
// @Param tenant_id query string false "租户ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} ginx.Result{data=UnifiedAssetVO} "成功"
// @Failure 404 {object} ginx.Result "EIP不存在"
// @Router /cam/assets/eip/{asset_id} [get]
func (h *AssetHandler) GetEIP(ctx *gin.Context) {
	h.getAsset(ctx, "eip")
}

// ==================== 通用方法 ====================

// listAssets 通用的资产列表查询
func (h *AssetHandler) listAssets(ctx *gin.Context, assetType string) {
	tenantID := ctx.Query("tenant_id")
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

	tenantID := ctx.Query("tenant_id")
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
		return modelUID == "ecs" ||
			modelUID == "aliyun_ecs" ||
			modelUID == "aws_ecs" ||
			modelUID == "huawei_ecs" ||
			modelUID == "tencent_ecs" ||
			modelUID == "volcano_ecs"
	case "rds":
		return modelUID == "rds" ||
			modelUID == "aliyun_rds" ||
			modelUID == "aws_rds" ||
			modelUID == "huawei_rds" ||
			modelUID == "tencent_rds" ||
			modelUID == "volcano_rds"
	case "redis":
		return modelUID == "redis" ||
			modelUID == "aliyun_redis" ||
			modelUID == "aws_redis" ||
			modelUID == "huawei_redis" ||
			modelUID == "tencent_redis" ||
			modelUID == "volcano_redis"
	case "mongodb":
		return modelUID == "mongodb" ||
			modelUID == "aliyun_mongodb" ||
			modelUID == "aws_mongodb" ||
			modelUID == "huawei_mongodb" ||
			modelUID == "tencent_mongodb" ||
			modelUID == "volcano_mongodb"
	case "vpc":
		return modelUID == "vpc" ||
			modelUID == "aliyun_vpc" ||
			modelUID == "aws_vpc" ||
			modelUID == "huawei_vpc" ||
			modelUID == "tencent_vpc" ||
			modelUID == "volcano_vpc"
	case "eip":
		return modelUID == "eip" ||
			modelUID == "aliyun_eip" ||
			modelUID == "aws_eip" ||
			modelUID == "huawei_eip" ||
			modelUID == "tencent_eip" ||
			modelUID == "volcano_eip"
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
	// aliyun_ecs -> ecs
	// aws_rds -> rds
	for _, suffix := range []string{"_ecs", "_rds", "_redis", "_mongodb", "_vpc", "_eip"} {
		if len(modelUID) > len(suffix) && modelUID[len(modelUID)-len(suffix):] == suffix {
			return suffix[1:] // 去掉前缀下划线
		}
	}
	return modelUID
}
