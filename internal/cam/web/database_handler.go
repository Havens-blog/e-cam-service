package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/gin-gonic/gin"
)

// DatabaseHandler 数据库资源HTTP处理器
// 从本地数据库读取已同步的数据库实例
type DatabaseHandler struct {
	instanceSvc service.InstanceService
}

// NewDatabaseHandler 创建数据库资源处理器
func NewDatabaseHandler(instanceSvc service.InstanceService) *DatabaseHandler {
	return &DatabaseHandler{
		instanceSvc: instanceSvc,
	}
}

// RegisterRoutes 注册数据库资源路由
func (h *DatabaseHandler) RegisterRoutes(rg *gin.RouterGroup) {
	dbGroup := rg.Group("/databases")
	{
		// RDS 路由
		dbGroup.GET("/rds", h.ListRDSInstances)
		dbGroup.GET("/rds/:asset_id", h.GetRDSInstance)

		// Redis 路由
		dbGroup.GET("/redis", h.ListRedisInstances)
		dbGroup.GET("/redis/:asset_id", h.GetRedisInstance)

		// MongoDB 路由
		dbGroup.GET("/mongodb", h.ListMongoDBInstances)
		dbGroup.GET("/mongodb/:asset_id", h.GetMongoDBInstance)
	}
}

// ==================== RDS 处理器 ====================

// ListRDSInstances 获取RDS实例列表
// @Summary 获取RDS实例列表
// @Description 从数据库获取已同步的RDS实例列表
// @Tags 数据库管理
// @Accept json
// @Produce json
// @Param tenant_id query string false "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "实例状态"
// @Param asset_name query string false "实例名称(模糊搜索)"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} ginx.Result{data=DatabaseInstanceListResp} "成功"
// @Router /cam/databases/rds [get]
func (h *DatabaseHandler) ListRDSInstances(ctx *gin.Context) {
	h.listDatabaseInstances(ctx, "rds")
}

// GetRDSInstance 获取RDS实例详情
// @Summary 获取RDS实例详情
// @Description 从数据库获取指定RDS实例的详细信息
// @Tags 数据库管理
// @Accept json
// @Produce json
// @Param asset_id path string true "资产ID(云厂商实例ID)"
// @Param tenant_id query string false "租户ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} ginx.Result{data=DatabaseInstanceVO} "成功"
// @Failure 404 {object} ginx.Result "实例不存在"
// @Router /cam/databases/rds/{asset_id} [get]
func (h *DatabaseHandler) GetRDSInstance(ctx *gin.Context) {
	h.getDatabaseInstance(ctx, "rds")
}

// ==================== Redis 处理器 ====================

// ListRedisInstances 获取Redis实例列表
// @Summary 获取Redis实例列表
// @Description 从数据库获取已同步的Redis实例列表
// @Tags 数据库管理
// @Accept json
// @Produce json
// @Param tenant_id query string false "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "实例状态"
// @Param asset_name query string false "实例名称(模糊搜索)"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} ginx.Result{data=DatabaseInstanceListResp} "成功"
// @Router /cam/databases/redis [get]
func (h *DatabaseHandler) ListRedisInstances(ctx *gin.Context) {
	h.listDatabaseInstances(ctx, "redis")
}

// GetRedisInstance 获取Redis实例详情
// @Summary 获取Redis实例详情
// @Description 从数据库获取指定Redis实例的详细信息
// @Tags 数据库管理
// @Accept json
// @Produce json
// @Param asset_id path string true "资产ID(云厂商实例ID)"
// @Param tenant_id query string false "租户ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} ginx.Result{data=DatabaseInstanceVO} "成功"
// @Failure 404 {object} ginx.Result "实例不存在"
// @Router /cam/databases/redis/{asset_id} [get]
func (h *DatabaseHandler) GetRedisInstance(ctx *gin.Context) {
	h.getDatabaseInstance(ctx, "redis")
}

// ==================== MongoDB 处理器 ====================

// ListMongoDBInstances 获取MongoDB实例列表
// @Summary 获取MongoDB实例列表
// @Description 从数据库获取已同步的MongoDB实例列表
// @Tags 数据库管理
// @Accept json
// @Produce json
// @Param tenant_id query string false "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "实例状态"
// @Param asset_name query string false "实例名称(模糊搜索)"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} ginx.Result{data=DatabaseInstanceListResp} "成功"
// @Router /cam/databases/mongodb [get]
func (h *DatabaseHandler) ListMongoDBInstances(ctx *gin.Context) {
	h.listDatabaseInstances(ctx, "mongodb")
}

// GetMongoDBInstance 获取MongoDB实例详情
// @Summary 获取MongoDB实例详情
// @Description 从数据库获取指定MongoDB实例的详细信息
// @Tags 数据库管理
// @Accept json
// @Produce json
// @Param asset_id path string true "资产ID(云厂商实例ID)"
// @Param tenant_id query string false "租户ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} ginx.Result{data=DatabaseInstanceVO} "成功"
// @Failure 404 {object} ginx.Result "实例不存在"
// @Router /cam/databases/mongodb/{asset_id} [get]
func (h *DatabaseHandler) GetMongoDBInstance(ctx *gin.Context) {
	h.getDatabaseInstance(ctx, "mongodb")
}

// ==================== 通用方法 ====================

// listDatabaseInstances 通用的数据库实例列表查询
func (h *DatabaseHandler) listDatabaseInstances(ctx *gin.Context, dbType string) {
	tenantID := ctx.Query("tenant_id")
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	status := ctx.Query("status")
	assetName := ctx.Query("asset_name")
	accountIDStr := ctx.Query("account_id")

	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	var accountID int64
	if accountIDStr != "" {
		accountID, _ = strconv.ParseInt(accountIDStr, 10, 64)
	}

	// 构建模型UID，格式: {provider}_{dbType} 或 通用的 {dbType}
	modelUID := dbType
	if provider != "" {
		modelUID = provider + "_" + dbType
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
		ModelUID:   modelUID,
		TenantID:   tenantID,
		AccountID:  accountID,
		AssetName:  assetName,
		Provider:   provider,
		Attributes: attributes,
		Offset:     int64(offset),
		Limit:      int64(limit),
	}

	// 如果没有指定provider，则查询所有云厂商的该类型数据库
	if provider == "" {
		filter.ModelUID = "" // 清空modelUID，使用模糊匹配
		// 通过属性或其他方式过滤
	}

	instances, total, err := h.instanceSvc.List(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	// 如果没有指定provider，需要过滤出所有包含dbType的实例
	if provider == "" {
		var filtered []domain.Instance
		for _, inst := range instances {
			// 检查model_uid是否包含dbType
			if containsDBType(inst.ModelUID, dbType) {
				filtered = append(filtered, inst)
			}
		}
		instances = filtered
		total = int64(len(filtered))
	}

	ctx.JSON(200, Result(DatabaseInstanceListResp{
		Instances: h.toDatabaseInstanceVOs(instances),
		Total:     total,
	}))
}

// getDatabaseInstance 通用的数据库实例详情查询
func (h *DatabaseHandler) getDatabaseInstance(ctx *gin.Context, dbType string) {
	assetID := ctx.Param("asset_id")
	if assetID == "" {
		ctx.JSON(400, ErrorResultWithMsg(errs.ParamsError, "asset_id is required"))
		return
	}

	tenantID := ctx.Query("tenant_id")
	provider := ctx.Query("provider")

	// 构建模型UID
	modelUID := dbType
	if provider != "" {
		modelUID = provider + "_" + dbType
	}

	// 如果指定了provider，直接查询
	if provider != "" && tenantID != "" {
		instance, err := h.instanceSvc.GetByAssetID(ctx.Request.Context(), tenantID, modelUID, assetID)
		if err != nil {
			ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
			return
		}
		if instance.ID == 0 {
			ctx.JSON(404, ErrorResult(errs.InstanceNotFound))
			return
		}
		ctx.JSON(200, Result(h.toDatabaseInstanceVO(instance)))
		return
	}

	// 否则，通过asset_id搜索
	filter := domain.InstanceFilter{
		AssetID: assetID,
		Limit:   10,
	}

	instances, _, err := h.instanceSvc.List(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	// 过滤出匹配dbType的实例
	for _, inst := range instances {
		if containsDBType(inst.ModelUID, dbType) {
			ctx.JSON(200, Result(h.toDatabaseInstanceVO(inst)))
			return
		}
	}

	ctx.JSON(404, ErrorResult(errs.InstanceNotFound))
}

// containsDBType 检查model_uid是否包含指定的数据库类型
func containsDBType(modelUID, dbType string) bool {
	// 支持的格式: aliyun_rds, aws_rds, rds 等
	switch dbType {
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
	}
	return false
}

// ==================== 类型转换 ====================

// DatabaseInstanceListResp 数据库实例列表响应
type DatabaseInstanceListResp struct {
	Instances []DatabaseInstanceVO `json:"instances"`
	Total     int64                `json:"total"`
}

// DatabaseInstanceVO 数据库实例视图对象
type DatabaseInstanceVO struct {
	ID         int64                  `json:"id"`
	ModelUID   string                 `json:"model_uid"`
	AssetID    string                 `json:"asset_id"`
	AssetName  string                 `json:"asset_name"`
	TenantID   string                 `json:"tenant_id"`
	AccountID  int64                  `json:"account_id"`
	Provider   string                 `json:"provider"`
	Region     string                 `json:"region"`
	Status     string                 `json:"status"`
	Attributes map[string]interface{} `json:"attributes"`
	CreateTime int64                  `json:"create_time"`
	UpdateTime int64                  `json:"update_time"`
}

func (h *DatabaseHandler) toDatabaseInstanceVOs(instances []domain.Instance) []DatabaseInstanceVO {
	vos := make([]DatabaseInstanceVO, len(instances))
	for i, inst := range instances {
		vos[i] = h.toDatabaseInstanceVO(inst)
	}
	return vos
}

func (h *DatabaseHandler) toDatabaseInstanceVO(inst domain.Instance) DatabaseInstanceVO {
	provider := ""
	region := ""
	status := ""

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

	return DatabaseInstanceVO{
		ID:         inst.ID,
		ModelUID:   inst.ModelUID,
		AssetID:    inst.AssetID,
		AssetName:  inst.AssetName,
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
