package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

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
// @Failure 400 {object} ErrorResponse "租户ID不��为空"
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

// ==================== ENI 弹性网卡 ====================

// ListENI 获取弹性网卡列表
// @Summary 获取弹性网卡列表
// @Description 从数据库获取已同步的弹性网卡列表
// @Tags 资产管理-ENI
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param account_id query int false "云账号ID"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Param region query string false "地域"
// @Param status query string false "状态(available/in_use/attaching/detaching)"
// @Param name query string false "网卡名称(模糊搜索)"
// @Param vpc_id query string false "VPC ID"
// @Param subnet_id query string false "子网/交换机ID"
// @Param instance_id query string false "绑定的ECS实例ID"
// @Param type query string false "网卡类型(Primary/Secondary)"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} AssetListResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Router /cam/assets/eni [get]
func (h *AssetHandler) ListENI(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	provider := ctx.Query("provider")
	region := ctx.Query("region")
	status := ctx.Query("status")
	name := ctx.Query("name")
	accountIDStr := ctx.Query("account_id")

	// ENI 特有过滤参数
	vpcID := ctx.Query("vpc_id")
	subnetID := ctx.Query("subnet_id")
	instanceID := ctx.Query("instance_id")
	eniType := ctx.Query("type")

	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	var accountID int64
	if accountIDStr != "" {
		accountID, _ = strconv.ParseInt(accountIDStr, 10, 64)
	}

	// 构建属性���滤条件
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
	if subnetID != "" {
		attributes["subnet_id"] = subnetID
	}
	if instanceID != "" {
		attributes["instance_id"] = instanceID
	}
	if eniType != "" {
		attributes["type"] = eniType
	}

	filter := domain.InstanceFilter{
		ModelUID:   "eni",
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

// GetENI 获取弹性网卡详情
// @Summary 获取弹性网卡详情
// @Description 从数据库获取指定弹性网卡的详细信息
// @Tags 资产管理-ENI
// @Accept json
// @Produce json
// @Param X-Tenant-ID header string true "租户ID"
// @Param asset_id path string true "资产ID(ENI ID)"
// @Param provider query string false "云厂商" Enums(aliyun,aws,huawei,tencent,volcano)
// @Success 200 {object} AssetDetailResult "成功"
// @Failure 400 {object} ErrorResponse "租户ID不能为空"
// @Failure 404 {object} ErrorResponse "弹性网卡不存在"
// @Router /cam/assets/eni/{asset_id} [get]
func (h *AssetHandler) GetENI(ctx *gin.Context) {
	h.getAsset(ctx, "eni")
}
