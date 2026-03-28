package web

import (
	"strconv"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/middleware"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// AssetHandler 资产HTTP处理器
// 按资产类型提供RESTful风格的API
type AssetHandler struct {
	instanceSvc service.InstanceService
	logger      *elog.Component
}

// NewAssetHandler 创建资产处理器
func NewAssetHandler(instanceSvc service.InstanceService) *AssetHandler {
	return &AssetHandler{
		instanceSvc: instanceSvc,
		logger:      elog.DefaultLogger,
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
	assetsGroup.GET("/ecs/:asset_id/relations", h.GetECSRelations)

	// 云盘
	assetsGroup.GET("/disk", h.ListDisk)
	assetsGroup.GET("/disk/:asset_id", h.GetDisk)

	// 快照
	assetsGroup.GET("/snapshot", h.ListSnapshot)
	assetsGroup.GET("/snapshot/:asset_id", h.GetSnapshot)

	// 安全组
	assetsGroup.GET("/security-group", h.ListSecurityGroup)
	assetsGroup.GET("/security-group/:asset_id", h.GetSecurityGroup)

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

	// VSwitch 交换机/子网
	assetsGroup.GET("/vswitch", h.ListVSwitch)
	assetsGroup.GET("/vswitch/:asset_id", h.GetVSwitch)

	// LB 负载均衡
	assetsGroup.GET("/lb", h.ListLB)
	assetsGroup.GET("/lb/:asset_id", h.GetLB)

	// CDN 内容分发网络
	assetsGroup.GET("/cdn", h.ListCDN)
	assetsGroup.GET("/cdn/:asset_id", h.GetCDN)

	// WAF Web应用防火墙
	assetsGroup.GET("/waf", h.ListWAF)
	assetsGroup.GET("/waf/:asset_id", h.GetWAF)

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

	// 镜像
	assetsGroup.GET("/image", h.ListImage)
	assetsGroup.GET("/image/stats", h.GetImageStats)
	assetsGroup.GET("/image/:asset_id", h.GetImage)
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
// @Param charge_type query string false "计费类型(PrePaid/PostPaid)"
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
	chargeType := ctx.Query("charge_type")

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
	if chargeType != "" {
		attributes["charge_type"] = chargeType
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

// GetECSRelations 获取ECS实例关联资源
// @Summary 获取ECS实例关联资源
// @Description 获取指定ECS实例关联的云盘、快照、安全组、VPC、子网等资源
// @Tags 资产管理-ECS
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param asset_id path string true "资产ID(云厂商实例ID)"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} ECSRelationsResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Failure 404 {object} ErrorResponse "实例不存在"
// @Router /cam/assets/ecs/{asset_id}/relations [get]
func (h *AssetHandler) GetECSRelations(ctx *gin.Context) {
	assetID := ctx.Param("asset_id")
	if assetID == "" {
		ctx.JSON(400, ErrorResultWithMsg(errs.ParamsError, "asset_id is required"))
		return
	}

	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")

	h.logger.Info("GetECSRelations 请求参数",
		elog.String("asset_id", assetID),
		elog.String("tenant_id", tenantID),
		elog.String("provider", provider))

	// 1. 先获取 ECS 实例
	ecsFilter := domain.InstanceFilter{
		AssetID:  assetID,
		TenantID: tenantID,
		Provider: provider,
		Limit:    10,
	}

	ecsInstances, _, err := h.instanceSvc.List(ctx.Request.Context(), ecsFilter)
	if err != nil {
		h.logger.Error("查询ECS实例失败", elog.FieldErr(err))
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	h.logger.Info("查询ECS实例结果",
		elog.Int("count", len(ecsInstances)))
	for _, inst := range ecsInstances {
		h.logger.Info("ECS实例",
			elog.String("model_uid", inst.ModelUID),
			elog.String("asset_id", inst.AssetID),
			elog.String("tenant_id", inst.TenantID),
			elog.Int64("account_id", inst.AccountID))
	}

	// 找到匹配的 ECS 实例
	var ecsInstance *domain.Instance
	for i, inst := range ecsInstances {
		if matchAssetType(inst.ModelUID, "ecs") {
			ecsInstance = &ecsInstances[i]
			break
		}
	}

	if ecsInstance == nil {
		// 如果带 provider 查不到，尝试不带 provider 查询
		if provider != "" {
			h.logger.Warn("带provider查不到ECS实例，尝试不带provider查询",
				elog.String("asset_id", assetID))
			ecsFilter.Provider = ""
			ecsInstances, _, err = h.instanceSvc.List(ctx.Request.Context(), ecsFilter)
			if err == nil {
				for i, inst := range ecsInstances {
					if matchAssetType(inst.ModelUID, "ecs") {
						ecsInstance = &ecsInstances[i]
						break
					}
				}
			}
		}
		if ecsInstance == nil {
			ctx.JSON(404, ErrorResult(errs.InstanceNotFound))
			return
		}
	}

	// 从 ECS 实例属性中获取 region 和 account_id
	region := ""
	if r, ok := ecsInstance.Attributes["region"].(string); ok {
		region = r
	}
	accountID := ecsInstance.AccountID
	// 使用 ECS 实例自身的 provider（更可靠）
	instanceProvider := ""
	if p, ok := ecsInstance.Attributes["provider"].(string); ok {
		instanceProvider = p
	}

	h.logger.Info("ECS实例详情",
		elog.String("model_uid", ecsInstance.ModelUID),
		elog.String("region", region),
		elog.Int64("account_id", accountID),
		elog.String("instance_provider", instanceProvider),
		elog.Any("security_group_ids", ecsInstance.Attributes["security_group_ids"]),
		elog.Any("security_groups", ecsInstance.Attributes["security_groups"]))

	// 获取 VPC ID 和 子网 ID
	vpcID := ""
	subnetID := ""
	if v, ok := ecsInstance.Attributes["vpc_id"].(string); ok {
		vpcID = v
	}
	// 子网字段可能是 vswitch_id (阿里云) 或 subnet_id (其他云)
	if s, ok := ecsInstance.Attributes["vswitch_id"].(string); ok {
		subnetID = s
	} else if s, ok := ecsInstance.Attributes["subnet_id"].(string); ok {
		subnetID = s
	}

	// 2. 查询关联的云盘 (通过 instance_id 属性)
	diskFilter := domain.InstanceFilter{
		ModelUID:  "disk",
		TenantID:  tenantID,
		AccountID: accountID,
		Attributes: map[string]interface{}{
			"instance_id": assetID,
		},
		Limit: 100,
	}
	if region != "" {
		diskFilter.Attributes["region"] = region
	}

	disks, _, _ := h.instanceSvc.List(ctx.Request.Context(), diskFilter)
	h.logger.Info("查询关联云盘结果", elog.Int("count", len(disks)))

	// 3. 查询关联的快照 (通过云盘ID查询)
	var snapshots []domain.Instance
	for _, disk := range disks {
		snapshotFilter := domain.InstanceFilter{
			ModelUID:  "snapshot",
			TenantID:  tenantID,
			AccountID: accountID,
			Attributes: map[string]interface{}{
				"source_disk_id": disk.AssetID,
			},
			Limit: 100,
		}
		if region != "" {
			snapshotFilter.Attributes["region"] = region
		}
		diskSnapshots, _, _ := h.instanceSvc.List(ctx.Request.Context(), snapshotFilter)
		snapshots = append(snapshots, diskSnapshots...)
	}
	// 如果通过磁盘没找到快照，尝试通过 source_instance_id 直接查询
	if len(snapshots) == 0 {
		snapshotByInstFilter := domain.InstanceFilter{
			ModelUID:  "snapshot",
			TenantID:  tenantID,
			AccountID: accountID,
			Attributes: map[string]interface{}{
				"source_instance_id": assetID,
			},
			Limit: 100,
		}
		if region != "" {
			snapshotByInstFilter.Attributes["region"] = region
		}
		snapshots, _, _ = h.instanceSvc.List(ctx.Request.Context(), snapshotByInstFilter)
	}
	h.logger.Info("查询关联快照结果", elog.Int("count", len(snapshots)))

	// 4. 查询关联的安全组 (通过 ECS 的 security_group_ids 或 security_groups 属性)
	var securityGroups []domain.Instance
	var sgIDs []string

	// 从 security_group_ids 提取 (支持 []interface{} 和 primitive.A)
	sgIDs = extractStringArray(ecsInstance.Attributes["security_group_ids"])

	// 如果 security_group_ids 为空，尝试从 security_groups 提取
	if len(sgIDs) == 0 {
		sgIDs = extractSecurityGroupIDs(ecsInstance.Attributes["security_groups"])
	}

	h.logger.Info("提取安全组ID", elog.Any("sg_ids", sgIDs))

	// 根据安全组 ID 查询安全组实例
	for _, sgID := range sgIDs {
		sgFilter := domain.InstanceFilter{
			ModelUID:  "security_group",
			AssetID:   sgID,
			TenantID:  tenantID,
			AccountID: accountID,
			Limit:     10,
		}
		sgInstances, _, sgErr := h.instanceSvc.List(ctx.Request.Context(), sgFilter)
		h.logger.Info("查询安全组",
			elog.String("sg_id", sgID),
			elog.Int("result_count", len(sgInstances)),
			elog.FieldErr(sgErr))
		if len(sgInstances) == 0 {
			// 回退: 不带 AccountID 查询
			sgFilter2 := domain.InstanceFilter{
				ModelUID: "security_group",
				AssetID:  sgID,
				TenantID: tenantID,
				Limit:    10,
			}
			sgInstances, _, sgErr = h.instanceSvc.List(ctx.Request.Context(), sgFilter2)
			h.logger.Info("回退查询安全组(不带account_id)",
				elog.String("sg_id", sgID),
				elog.Int("result_count", len(sgInstances)),
				elog.FieldErr(sgErr))
		}
		for _, inst := range sgInstances {
			if matchAssetType(inst.ModelUID, "security_group") {
				securityGroups = append(securityGroups, inst)
				break
			}
		}
	}
	h.logger.Info("查询关联安全组结果", elog.Int("count", len(securityGroups)))

	// 如果数据库中没有找到安全组，从 ECS 实例属性中构建基本信息
	if len(securityGroups) == 0 && len(sgIDs) > 0 {
		h.logger.Info("安全组未在数据库中找到，从ECS属性构建基本信息")
		sgList := ecsInstance.Attributes["security_groups"]

		// 统一转换为 []interface{}
		var sgItems []interface{}
		switch arr := sgList.(type) {
		case []interface{}:
			sgItems = arr
		case primitive.A:
			sgItems = arr
		}

		for _, item := range sgItems {
			sgID := ""
			sgName := ""
			sgDesc := ""

			switch sgMap := item.(type) {
			case map[string]interface{}:
				if id, ok := sgMap["id"].(string); ok {
					sgID = id
				}
				if name, ok := sgMap["name"].(string); ok {
					sgName = name
				}
				if desc, ok := sgMap["description"].(string); ok {
					sgDesc = desc
				}
			case primitive.M:
				if id, ok := sgMap["id"].(string); ok {
					sgID = id
				}
				if name, ok := sgMap["name"].(string); ok {
					sgName = name
				}
				if desc, ok := sgMap["description"].(string); ok {
					sgDesc = desc
				}
			}

			if sgID != "" {
				securityGroups = append(securityGroups, domain.Instance{
					ModelUID:  instanceProvider + "_security_group",
					AssetID:   sgID,
					AssetName: sgName,
					TenantID:  tenantID,
					AccountID: accountID,
					Attributes: map[string]interface{}{
						"provider":    instanceProvider,
						"region":      region,
						"description": sgDesc,
						"_from_ecs":   true,
					},
				})
			}
		}
		h.logger.Info("从ECS属性构建安全组", elog.Int("count", len(securityGroups)))
	}

	// 5. 查询关联的 VPC
	var vpcInstance *domain.Instance
	if vpcID != "" {
		vpcFilter := domain.InstanceFilter{
			ModelUID:  "vpc",
			AssetID:   vpcID,
			TenantID:  tenantID,
			AccountID: accountID,
			Limit:     10,
		}
		vpcInstances, _, _ := h.instanceSvc.List(ctx.Request.Context(), vpcFilter)
		h.logger.Info("查询关联VPC", elog.String("vpc_id", vpcID), elog.Int("result_count", len(vpcInstances)))
		for i, inst := range vpcInstances {
			if matchAssetType(inst.ModelUID, "vpc") {
				vpcInstance = &vpcInstances[i]
				break
			}
		}
	}

	// 6. 查询关联的子网/交换机
	var subnetInstance *domain.Instance
	if subnetID != "" {
		subnetFilter := domain.InstanceFilter{
			ModelUID:  "subnet",
			AssetID:   subnetID,
			TenantID:  tenantID,
			AccountID: accountID,
			Limit:     10,
		}
		subnetInstances, _, _ := h.instanceSvc.List(ctx.Request.Context(), subnetFilter)
		h.logger.Info("查询关联子网", elog.String("subnet_id", subnetID), elog.Int("result_count", len(subnetInstances)))
		for i, inst := range subnetInstances {
			if matchAssetType(inst.ModelUID, "subnet") || matchAssetType(inst.ModelUID, "vswitch") {
				subnetInstance = &subnetInstances[i]
				break
			}
		}

		// 子网兜底: 如果数据库中没有子网数据，从 ECS 实例属性中构建基本信息
		if subnetInstance == nil {
			h.logger.Info("子网未在数据库中找到，从ECS属性构建基本信息",
				elog.String("subnet_id", subnetID))
			subnetName := ""
			if n, ok := ecsInstance.Attributes["vswitch_name"].(string); ok {
				subnetName = n
			}
			if subnetName == "" {
				if n, ok := ecsInstance.Attributes["subnet_name"].(string); ok {
					subnetName = n
				}
			}
			zone := ""
			if z, ok := ecsInstance.Attributes["zone"].(string); ok {
				zone = z
			}
			fallbackSubnet := domain.Instance{
				ModelUID:  instanceProvider + "_subnet",
				AssetID:   subnetID,
				AssetName: subnetName,
				TenantID:  tenantID,
				AccountID: accountID,
				Attributes: map[string]interface{}{
					"provider":  instanceProvider,
					"region":    region,
					"zone":      zone,
					"vpc_id":    vpcID,
					"_from_ecs": true,
				},
			}
			subnetInstance = &fallbackSubnet
		}
	}

	// 7. 构建响应
	ecsVO := h.toUnifiedAssetVO(*ecsInstance)
	resp := ECSRelationsResp{
		ECS:            &ecsVO,
		Disks:          h.toUnifiedAssetVOs(disks),
		Snapshots:      h.toUnifiedAssetVOs(snapshots),
		SecurityGroups: h.toUnifiedAssetVOs(securityGroups),
	}

	if vpcInstance != nil {
		vpcVO := h.toUnifiedAssetVO(*vpcInstance)
		resp.VPC = &vpcVO
	}
	if subnetInstance != nil {
		subnetVO := h.toUnifiedAssetVO(*subnetInstance)
		resp.Subnet = &subnetVO
	}

	ctx.JSON(200, Result(resp))
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

// ==================== VSwitch 交换机/子网 ====================

// ListVSwitch 获取交换机/子网列表
func (h *AssetHandler) ListVSwitch(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	status := ctx.Query("status")
	name := ctx.Query("name")
	accountIDStr := ctx.Query("account_id")

	// VSwitch 特有过滤参数
	vpcID := ctx.Query("vpc_id")
	zone := ctx.Query("zone")

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
	if vpcID != "" {
		attributes["vpc_id"] = vpcID
	}
	if zone != "" {
		attributes["zone"] = zone
	}

	filter := domain.InstanceFilter{
		ModelUID:   "vswitch",
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

// GetVSwitch 获取交换机/子网详情
func (h *AssetHandler) GetVSwitch(ctx *gin.Context) {
	h.getAsset(ctx, "vswitch")
}

// ==================== LB 负载均衡 ====================

// ListLB 获取负载均衡实例列表
// @Summary 获取负载均衡实例列表
// @Description 从数据库获取已同步的负载均衡实例列表（SLB/ALB/NLB）
// @Tags 资产管理-LB
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "状态"
// @Param name query string false "实例名称(模糊搜索)"
// @Param lb_type query string false "负载均衡类型(slb/alb/nlb)"
// @Param address_type query string false "地址类型(internet/intranet)"
// @Param vpc_id query string false "VPC ID"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} AssetListResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/assets/lb [get]
func (h *AssetHandler) ListLB(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	status := ctx.Query("status")
	name := ctx.Query("name")
	accountIDStr := ctx.Query("account_id")

	// LB 特有过滤参数
	lbType := ctx.Query("lb_type")
	addressType := ctx.Query("address_type")
	vpcID := ctx.Query("vpc_id")

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
	if lbType != "" {
		attributes["load_balancer_type"] = lbType
	}
	if addressType != "" {
		attributes["address_type"] = addressType
	}
	if vpcID != "" {
		attributes["vpc_id"] = vpcID
	}

	filter := domain.InstanceFilter{
		ModelUID:   "lb",
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

// GetLB 获取负载均衡实例详情
// @Summary 获取负载均衡实例详情
// @Description 获取单个负载均衡实例的详细信息
// @Tags 资产管理-LB
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param asset_id path string true "资产ID"
// @Param provider query string false "云厂商"
// @Success 200 {object} AssetDetailResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Failure 404 {object} ErrorResponse "负载均衡实例不存在"
// @Router /cam/assets/lb/{asset_id} [get]
func (h *AssetHandler) GetLB(ctx *gin.Context) {
	h.getAsset(ctx, "lb")
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
	if businessType != "" {
		attributes["business_type"] = businessType
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

// ==================== 镜像 Image ====================

// ListImage 获取镜像列表
// @Summary 获取镜像列表
// @Description 从数据库获取已同步的镜像列表，支持按镜像类型、操作系统、架构等过滤
// @Tags 资产管理-镜像
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "镜像状态"
// @Param name query string false "镜像名称(模糊搜索)"
// @Param image_owner_alias query string false "镜像类型(system/self/others/marketplace)"
// @Param os_type query string false "操作系统类型(linux/windows)"
// @Param platform query string false "操作系统平台(CentOS/Ubuntu/Windows Server等)"
// @Param architecture query string false "架构(x86_64/arm64)"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} AssetListResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/assets/image [get]
func (h *AssetHandler) ListImage(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	status := ctx.Query("status")
	name := ctx.Query("name")
	accountIDStr := ctx.Query("account_id")

	// 镜像特有过滤参数
	imageOwnerAlias := ctx.Query("image_owner_alias")
	osType := ctx.Query("os_type")
	platform := ctx.Query("platform")
	architecture := ctx.Query("architecture")

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
	if imageOwnerAlias != "" {
		attributes["image_owner_alias"] = imageOwnerAlias
	}
	if osType != "" {
		attributes["os_type"] = osType
	}
	if platform != "" {
		attributes["platform"] = platform
	}
	if architecture != "" {
		attributes["architecture"] = architecture
	}

	filter := domain.InstanceFilter{
		ModelUID:   "image",
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

// GetImage 获取镜像详情
// @Summary 获取镜像详情
// @Description 从数据库获取指定镜像的详细信息
// @Tags 资产管理-镜像
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param asset_id path string true "资产ID(镜像ID)"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} AssetDetailResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Failure 404 {object} ErrorResponse "镜像不存在"
// @Router /cam/assets/image/{asset_id} [get]
func (h *AssetHandler) GetImage(ctx *gin.Context) {
	h.getAsset(ctx, "image")
}

// GetImageStats 获取镜像统计数据
// @Summary 获取镜像统计数据
// @Description 按镜像类型（公共/自定义/共享）聚合统计各类型镜像数量
// @Tags 资产管理-镜像
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} ImageStatsResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/assets/image/stats [get]
func (h *AssetHandler) GetImageStats(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	if tenantID == "" {
		ctx.JSON(400, ErrorResultWithMsg(errs.ParamsError, "X-Tenant-ID is required"))
		return
	}

	accountIDStr := ctx.Query("account_id")
	provider := ctx.Query("provider")

	var accountID int64
	if accountIDStr != "" {
		accountID, _ = strconv.ParseInt(accountIDStr, 10, 64)
	}

	// 查询各类型镜像数量
	stats := ImageStatsResp{}
	for _, alias := range []string{"", "system", "self", "others"} {
		filter := domain.InstanceFilter{
			ModelUID: "image",
			TenantID: tenantID,
			Provider: provider,
			Limit:    1,
		}
		if accountID > 0 {
			filter.AccountID = accountID
		}
		if alias != "" {
			filter.Attributes = map[string]interface{}{"image_owner_alias": alias}
		}
		_, total, _ := h.instanceSvc.List(ctx.Request.Context(), filter)
		switch alias {
		case "":
			stats.Total = total
		case "system":
			stats.System = total
		case "self":
			stats.Custom = total
		case "others":
			stats.Shared = total
		}
	}

	ctx.JSON(200, Result(stats))
}

// ==================== 云盘 Disk ====================

// ListDisk 获取云盘列表
// @Summary 获取云盘列表
// @Description 从数据库获取已同步的云盘列表
// @Tags 资产管理-云盘
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "云盘状态"
// @Param name query string false "云盘名称(模糊搜索)"
// @Param disk_type query string false "云盘类型(system/data)"
// @Param instance_id query string false "挂载的实例ID"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} AssetListResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /api/v1/cam/assets/disk [get]
func (h *AssetHandler) ListDisk(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	status := ctx.Query("status")
	name := ctx.Query("name")
	accountIDStr := ctx.Query("account_id")
	diskType := ctx.Query("disk_type")
	instanceID := ctx.Query("instance_id")

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
	if diskType != "" {
		attributes["disk_type"] = diskType
	}
	if instanceID != "" {
		attributes["instance_id"] = instanceID
	}

	filter := domain.InstanceFilter{
		ModelUID:   "disk",
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

// GetDisk 获取云盘详情
// @Summary 获取云盘详情
// @Description 从数据库获取指定云盘的详细信息
// @Tags 资产管理-云盘
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param asset_id path string true "资产ID(云盘ID)"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} AssetDetailResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Failure 404 {object} ErrorResponse "云盘不存在"
// @Router /api/v1/cam/assets/disk/{asset_id} [get]
func (h *AssetHandler) GetDisk(ctx *gin.Context) {
	h.getAsset(ctx, "disk")
}

// ==================== 快照 Snapshot ====================

// ListSnapshot 获取快照列表
// @Summary 获取快照列表
// @Description 从数据库获取已同步的快照列表
// @Tags 资产管理-快照
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "快照状态"
// @Param name query string false "快照名称(模糊搜索)"
// @Param source_disk_id query string false "源磁盘ID"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} AssetListResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /api/v1/cam/assets/snapshot [get]
func (h *AssetHandler) ListSnapshot(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	status := ctx.Query("status")
	name := ctx.Query("name")
	accountIDStr := ctx.Query("account_id")
	sourceDiskID := ctx.Query("source_disk_id")

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
	if sourceDiskID != "" {
		attributes["source_disk_id"] = sourceDiskID
	}

	filter := domain.InstanceFilter{
		ModelUID:   "snapshot",
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

// GetSnapshot 获取快照详情
// @Summary 获取快照详情
// @Description 从数据库获取指定快照的详细信息
// @Tags 资产管理-快照
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param asset_id path string true "资产ID(快照ID)"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} AssetDetailResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Failure 404 {object} ErrorResponse "快照不存在"
// @Router /api/v1/cam/assets/snapshot/{asset_id} [get]
func (h *AssetHandler) GetSnapshot(ctx *gin.Context) {
	h.getAsset(ctx, "snapshot")
}

// ==================== 安全组 SecurityGroup ====================

// ListSecurityGroup 获取安全组列表
// @Summary 获取安全组列表
// @Description 从数据库获取已同步的安全组列表
// @Tags 资产管理-安全组
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param name query string false "安全组名称(模糊搜索)"
// @Param vpc_id query string false "VPC ID"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} AssetListResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /api/v1/cam/assets/security-group [get]
func (h *AssetHandler) ListSecurityGroup(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	name := ctx.Query("name")
	accountIDStr := ctx.Query("account_id")
	vpcID := ctx.Query("vpc_id")

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
	if vpcID != "" {
		attributes["vpc_id"] = vpcID
	}

	filter := domain.InstanceFilter{
		ModelUID:   "security_group",
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

// GetSecurityGroup 获取安全组详情
// @Summary 获取安全组详情
// @Description 从数据库获取指定安全组的详细信息
// @Tags 资产管理-安全组
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param asset_id path string true "资产ID(安全组ID)"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} AssetDetailResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Failure 404 {object} ErrorResponse "安全组不存在"
// @Router /api/v1/cam/assets/security-group/{asset_id} [get]
func (h *AssetHandler) GetSecurityGroup(ctx *gin.Context) {
	h.getAsset(ctx, "security_group")
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
	case "disk":
		return modelUID == "disk" || modelUID == "cloud_disk" ||
			modelUID == "aliyun_disk" ||
			modelUID == "aws_disk" ||
			modelUID == "huawei_disk" ||
			modelUID == "tencent_disk" ||
			modelUID == "volcano_disk"
	case "snapshot":
		return modelUID == "snapshot" || modelUID == "cloud_snapshot" ||
			modelUID == "aliyun_snapshot" ||
			modelUID == "aws_snapshot" ||
			modelUID == "huawei_snapshot" ||
			modelUID == "tencent_snapshot" ||
			modelUID == "volcano_snapshot"
	case "security_group":
		return modelUID == "security_group" || modelUID == "cloud_security_group" ||
			modelUID == "aliyun_security_group" ||
			modelUID == "aws_security_group" ||
			modelUID == "huawei_security_group" ||
			modelUID == "tencent_security_group" ||
			modelUID == "volcano_security_group"
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
	case "subnet", "vswitch":
		return modelUID == "subnet" || modelUID == "vswitch" ||
			modelUID == "cloud_subnet" || modelUID == "cloud_vswitch" ||
			modelUID == "aliyun_vswitch" ||
			modelUID == "aws_subnet" ||
			modelUID == "huawei_subnet" ||
			modelUID == "tencent_subnet" ||
			modelUID == "volcano_subnet"
	case "eip":
		return modelUID == "eip" || modelUID == "cloud_eip" ||
			modelUID == "aliyun_eip" ||
			modelUID == "aws_eip" ||
			modelUID == "huawei_eip" ||
			modelUID == "tencent_eip" ||
			modelUID == "volcano_eip"
	case "lb":
		return modelUID == "lb" || modelUID == "cloud_lb" ||
			modelUID == "slb" || modelUID == "alb" || modelUID == "nlb" ||
			modelUID == "aliyun_lb" || modelUID == "aliyun_slb" || modelUID == "aliyun_alb" || modelUID == "aliyun_nlb" ||
			modelUID == "aws_lb" || modelUID == "aws_elb" || modelUID == "aws_alb" || modelUID == "aws_nlb" ||
			modelUID == "huawei_lb" || modelUID == "huawei_elb" ||
			modelUID == "tencent_lb" || modelUID == "tencent_clb" ||
			modelUID == "volcano_lb"
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
	case "cdn":
		return modelUID == "cdn" || modelUID == "cloud_cdn" ||
			modelUID == "aliyun_cdn" ||
			modelUID == "aws_cdn" ||
			modelUID == "huawei_cdn" ||
			modelUID == "tencent_cdn" ||
			modelUID == "volcano_cdn"
	case "waf":
		return modelUID == "waf" || modelUID == "cloud_waf" ||
			modelUID == "aliyun_waf" ||
			modelUID == "aws_waf" ||
			modelUID == "huawei_waf" ||
			modelUID == "tencent_waf" ||
			modelUID == "volcano_waf"
	case "image":
		return modelUID == "image" || modelUID == "cloud_image" ||
			modelUID == "aliyun_image" ||
			modelUID == "aws_image" ||
			modelUID == "huawei_image" ||
			modelUID == "tencent_image" ||
			modelUID == "volcano_image"
	}
	return false
}

// ==================== 响应结构体 ====================

// ImageStatsResp 镜像统计响应
type ImageStatsResp struct {
	Total  int64 `json:"total"`
	System int64 `json:"system"`
	Custom int64 `json:"custom"`
	Shared int64 `json:"shared"`
}

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
	case "cloud_disk":
		return "disk"
	case "cloud_snapshot":
		return "snapshot"
	case "cloud_security_group":
		return "security_group"
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
	case "cloud_lb", "cloud_slb", "cloud_alb", "cloud_nlb":
		return "lb"
	case "cloud_cdn":
		return "cdn"
	case "cloud_waf":
		return "waf"
	case "cloud_image":
		return "image"
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
	for _, suffix := range []string{"_ecs", "_disk", "_snapshot", "_security_group", "_rds", "_redis", "_mongodb", "_vpc", "_eip", "_lb", "_slb", "_alb", "_nlb", "_cdn", "_waf", "_image", "_nas", "_oss", "_kafka", "_elasticsearch"} {
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

// ==================== 辅助函数 ====================

// extractStringArray 从 interface{} 提取字符串数组
// 支持 []interface{}, []string, primitive.A 等类型
func extractStringArray(v interface{}) []string {
	if v == nil {
		return nil
	}

	var result []string

	switch arr := v.(type) {
	case []string:
		return arr
	case []interface{}:
		for _, item := range arr {
			if s, ok := item.(string); ok && s != "" {
				result = append(result, s)
			}
		}
	case primitive.A:
		for _, item := range arr {
			if s, ok := item.(string); ok && s != "" {
				result = append(result, s)
			}
		}
	}

	return result
}

// extractSecurityGroupIDs 从 security_groups 属性提取安全组ID列表
// 支持 []interface{}, primitive.A 等类型，每个元素可能是 string 或 map
func extractSecurityGroupIDs(v interface{}) []string {
	if v == nil {
		return nil
	}

	var result []string
	var items []interface{}

	switch arr := v.(type) {
	case []interface{}:
		items = arr
	case primitive.A:
		items = arr
	default:
		return nil
	}

	for _, item := range items {
		var sgID string
		switch sg := item.(type) {
		case string:
			sgID = sg
		case map[string]interface{}:
			// JSON 字段名是小写 "id"
			if id, ok := sg["id"].(string); ok {
				sgID = id
			} else if id, ok := sg["ID"].(string); ok {
				sgID = id
			}
		case primitive.M:
			if id, ok := sg["id"].(string); ok {
				sgID = id
			} else if id, ok := sg["ID"].(string); ok {
				sgID = id
			}
		}
		if sgID != "" {
			result = append(result, sgID)
		}
	}

	return result
}
