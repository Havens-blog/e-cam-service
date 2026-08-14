package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

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
