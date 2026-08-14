package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

// ==================== ECS 云虚拟机 ====================

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
	if tenantID == 0 {
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
