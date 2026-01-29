package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/pkg/ginx"
	"github.com/gin-gonic/gin"
)

// InstanceHandler 资产实例HTTP处理器
type InstanceHandler struct {
	svc service.InstanceService
}

// NewInstanceHandler 创建实例处理器
func NewInstanceHandler(svc service.InstanceService) *InstanceHandler {
	return &InstanceHandler{svc: svc}
}

// RegisterRoutes 注册实例相关路由
func (h *InstanceHandler) RegisterRoutes(r *gin.RouterGroup) {
	instanceGroup := r.Group("/instances")
	{
		instanceGroup.POST("", ginx.WrapBody[CreateInstanceReq](h.Create))
		instanceGroup.POST("/batch", ginx.WrapBody[CreateBatchInstanceReq](h.CreateBatch))
		instanceGroup.POST("/upsert", ginx.WrapBody[UpsertInstanceReq](h.Upsert))
		instanceGroup.POST("/upsert-batch", ginx.WrapBody[UpsertBatchInstanceReq](h.UpsertBatch))
		instanceGroup.GET("/:id", h.GetByID)
		instanceGroup.GET("", h.List)
		instanceGroup.PUT("/:id", ginx.WrapBody[UpdateInstanceReq](h.Update))
		instanceGroup.DELETE("/:id", h.Delete)
	}
}

// ==================== 请求/响应结构体 ====================

// CreateInstanceReq 创建实例请求
type CreateInstanceReq struct {
	ModelUID   string                 `json:"model_uid" binding:"required"`
	AssetID    string                 `json:"asset_id" binding:"required"`
	AssetName  string                 `json:"asset_name"`
	TenantID   string                 `json:"tenant_id" binding:"required"`
	AccountID  int64                  `json:"account_id"`
	Attributes map[string]interface{} `json:"attributes"`
}

// CreateBatchInstanceReq 批量创建实例请求
type CreateBatchInstanceReq struct {
	Instances []CreateInstanceReq `json:"instances" binding:"required"`
}

// UpsertInstanceReq 更新或插入实例请求
type UpsertInstanceReq struct {
	ModelUID   string                 `json:"model_uid" binding:"required"`
	AssetID    string                 `json:"asset_id" binding:"required"`
	AssetName  string                 `json:"asset_name"`
	TenantID   string                 `json:"tenant_id" binding:"required"`
	AccountID  int64                  `json:"account_id"`
	Attributes map[string]interface{} `json:"attributes"`
}

// UpsertBatchInstanceReq 批量更新或插入实例请求
type UpsertBatchInstanceReq struct {
	Instances []UpsertInstanceReq `json:"instances" binding:"required"`
}

// UpdateInstanceReq 更新实例请求
type UpdateInstanceReq struct {
	AssetName  string                 `json:"asset_name"`
	Attributes map[string]interface{} `json:"attributes"`
}

// InstanceVO 实例视图对象
type InstanceVO struct {
	ID         int64                  `json:"id"`
	ModelUID   string                 `json:"model_uid"`
	AssetID    string                 `json:"asset_id"`
	AssetName  string                 `json:"asset_name"`
	TenantID   string                 `json:"tenant_id"`
	AccountID  int64                  `json:"account_id"`
	Attributes map[string]interface{} `json:"attributes"`
	CreateTime int64                  `json:"create_time"`
	UpdateTime int64                  `json:"update_time"`
}

// InstanceListResp 实例列表响应
type InstanceListResp struct {
	Instances []InstanceVO `json:"instances"`
	Total     int64        `json:"total"`
}

// ==================== 处理器方法 ====================

// Create 创建实例
// @Summary 创建资产实例
// @Description 创建新的资产实例
// @Tags 资产实例
// @Accept json
// @Produce json
// @Param request body CreateInstanceReq true "实例信息"
// @Success 200 {object} ginx.Result{data=object} "成功"
// @Router /cam/instances [post]
func (h *InstanceHandler) Create(ctx *gin.Context, req CreateInstanceReq) (ginx.Result, error) {
	instance := domain.Instance{
		ModelUID:   req.ModelUID,
		AssetID:    req.AssetID,
		AssetName:  req.AssetName,
		TenantID:   req.TenantID,
		AccountID:  req.AccountID,
		Attributes: req.Attributes,
	}

	id, err := h.svc.Create(ctx.Request.Context(), instance)
	if err != nil {
		if err == errs.ErrInstanceExists {
			return ErrorResultWithMsg(errs.ParamsError, "instance already exists"), nil
		}
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(map[string]interface{}{"id": id}), nil
}

// CreateBatch 批量创建实例
// @Summary 批量创建资产实例
// @Description 批量创建多个资产实例
// @Tags 资产实例
// @Accept json
// @Produce json
// @Param request body CreateBatchInstanceReq true "实例列表"
// @Success 200 {object} ginx.Result{data=object} "成功"
// @Router /cam/instances/batch [post]
func (h *InstanceHandler) CreateBatch(ctx *gin.Context, req CreateBatchInstanceReq) (ginx.Result, error) {
	instances := make([]domain.Instance, len(req.Instances))
	for i, r := range req.Instances {
		instances[i] = domain.Instance{
			ModelUID:   r.ModelUID,
			AssetID:    r.AssetID,
			AssetName:  r.AssetName,
			TenantID:   r.TenantID,
			AccountID:  r.AccountID,
			Attributes: r.Attributes,
		}
	}

	count, err := h.svc.CreateBatch(ctx.Request.Context(), instances)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(map[string]interface{}{"count": count}), nil
}

// Upsert 更新或插入实例
// @Summary 更新或插入资产实例
// @Description 根据 tenant_id + model_uid + asset_id 更新或插入实例
// @Tags 资产实例
// @Accept json
// @Produce json
// @Param request body UpsertInstanceReq true "实例信息"
// @Success 200 {object} ginx.Result "成功"
// @Router /cam/instances/upsert [post]
func (h *InstanceHandler) Upsert(ctx *gin.Context, req UpsertInstanceReq) (ginx.Result, error) {
	instance := domain.Instance{
		ModelUID:   req.ModelUID,
		AssetID:    req.AssetID,
		AssetName:  req.AssetName,
		TenantID:   req.TenantID,
		AccountID:  req.AccountID,
		Attributes: req.Attributes,
	}

	err := h.svc.Upsert(ctx.Request.Context(), instance)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(nil), nil
}

// UpsertBatch 批量更新或插入实例
// @Summary 批量更新或插入资产实例
// @Description 批量根据 tenant_id + model_uid + asset_id 更新或插入实例
// @Tags 资产实例
// @Accept json
// @Produce json
// @Param request body UpsertBatchInstanceReq true "实例列表"
// @Success 200 {object} ginx.Result "成功"
// @Router /cam/instances/upsert-batch [post]
func (h *InstanceHandler) UpsertBatch(ctx *gin.Context, req UpsertBatchInstanceReq) (ginx.Result, error) {
	instances := make([]domain.Instance, len(req.Instances))
	for i, r := range req.Instances {
		instances[i] = domain.Instance{
			ModelUID:   r.ModelUID,
			AssetID:    r.AssetID,
			AssetName:  r.AssetName,
			TenantID:   r.TenantID,
			AccountID:  r.AccountID,
			Attributes: r.Attributes,
		}
	}

	err := h.svc.UpsertBatch(ctx.Request.Context(), instances)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(nil), nil
}

// GetByID 根据ID获取实例
// @Summary 获取资产实例详情
// @Description 根据ID获取资产实例详细信息
// @Tags 资产实例
// @Accept json
// @Produce json
// @Param id path int true "实例ID"
// @Success 200 {object} ginx.Result{data=InstanceVO} "成功"
// @Router /cam/instances/{id} [get]
func (h *InstanceHandler) GetByID(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		ctx.JSON(400, ErrorResult(errs.ParamsError))
		return
	}

	instance, err := h.svc.GetByID(ctx.Request.Context(), id)
	if err != nil {
		if err == errs.ErrInstanceNotFound {
			ctx.JSON(404, ErrorResult(errs.InstanceNotFound))
			return
		}
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(h.toVO(instance)))
}

// List 获取实例列表
// @Summary 获取资产实例列表
// @Description 获取资产实例列表，支持按模型、租户、云账号等条件过滤，支持任意属性组合过滤
// @Tags 资产实例
// @Accept json
// @Produce json
// @Param model_uid query string false "模型UID"
// @Param tenant_id query string false "租户ID"
// @Param account_id query int false "云账号ID"
// @Param asset_name query string false "资产名称(模糊搜索)"
// @Param asset_id query string false "资产ID(精确匹配)"
// @Param provider query string false "云平台(aliyun/aws/huawei/tencent/volcengine)"
// @Param status query string false "状态"
// @Param region query string false "地域"
// @Param zone query string false "可用区"
// @Param vpc_id query string false "VPC ID"
// @Param instance_type query string false "实例规格"
// @Param os_type query string false "操作系统类型"
// @Param charge_type query string false "计费类型"
// @Param private_ip query string false "内网IP"
// @Param public_ip query string false "公网IP"
// @Param has_tags query string false "是否有标签(true/false)"
// @Param tag_key query string false "标签键(过滤包含此标签键的实例)"
// @Param tag_value query string false "标签值(需配合tag_key使用)"
// @Param offset query int false "偏移量" default(0)
// @Param limit query int false "限制数量" default(20)
// @Success 200 {object} ginx.Result{data=InstanceListResp} "成功"
// @Router /cam/instances [get]
func (h *InstanceHandler) List(ctx *gin.Context) {
	modelUID := ctx.Query("model_uid")
	tenantID := ctx.Query("tenant_id")
	accountIDStr := ctx.Query("account_id")
	assetName := ctx.Query("asset_name")
	assetID := ctx.Query("asset_id")
	provider := ctx.Query("provider")

	// 标签过滤参数
	hasTags := ctx.Query("has_tags")
	tagKey := ctx.Query("tag_key")
	tagValue := ctx.Query("tag_value")

	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	var accountID int64
	if accountIDStr != "" {
		accountID, _ = strconv.ParseInt(accountIDStr, 10, 64)
	}

	// 构建属性过滤条件 - 支持多种属性组合过滤
	attributes := make(map[string]interface{})

	// 常用过滤属性
	attrFilters := map[string]string{
		"status":        ctx.Query("status"),
		"region":        ctx.Query("region"),
		"zone":          ctx.Query("zone"),
		"vpc_id":        ctx.Query("vpc_id"),
		"instance_type": ctx.Query("instance_type"),
		"os_type":       ctx.Query("os_type"),
		"charge_type":   ctx.Query("charge_type"),
		"private_ip":    ctx.Query("private_ip"),
		"public_ip":     ctx.Query("public_ip"),
		"project_id":    ctx.Query("project_id"),
	}

	for key, value := range attrFilters {
		if value != "" {
			attributes[key] = value
		}
	}

	// 构建标签过滤条件
	var tagFilter *domain.TagFilter
	if hasTags != "" || tagKey != "" {
		tagFilter = &domain.TagFilter{}
		if hasTags == "true" {
			tagFilter.HasTags = true
		} else if hasTags == "false" {
			tagFilter.NoTags = true
		}
		if tagKey != "" {
			tagFilter.Key = tagKey
			tagFilter.Value = tagValue
		}
	}

	filter := domain.InstanceFilter{
		ModelUID:   modelUID,
		TenantID:   tenantID,
		AccountID:  accountID,
		AssetID:    assetID,
		AssetName:  assetName,
		Provider:   provider,
		TagFilter:  tagFilter,
		Attributes: attributes,
		Offset:     int64(offset),
		Limit:      int64(limit),
	}

	instances, total, err := h.svc.List(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	instanceVOs := make([]InstanceVO, len(instances))
	for i, inst := range instances {
		instanceVOs[i] = h.toVO(inst)
	}

	resp := InstanceListResp{
		Instances: instanceVOs,
		Total:     total,
	}

	ctx.JSON(200, Result(resp))
}

// Update 更新实例
// @Summary 更新资产实例
// @Description 更新指定ID的资产实例信息
// @Tags 资产实例
// @Accept json
// @Produce json
// @Param id path int true "实例ID"
// @Param request body UpdateInstanceReq true "更新信息"
// @Success 200 {object} ginx.Result "成功"
// @Router /cam/instances/{id} [put]
func (h *InstanceHandler) Update(ctx *gin.Context, req UpdateInstanceReq) (ginx.Result, error) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return ErrorResult(errs.ParamsError), nil
	}

	// 先获取现有实例
	existing, err := h.svc.GetByID(ctx.Request.Context(), id)
	if err != nil {
		if err == errs.ErrInstanceNotFound {
			return ErrorResult(errs.InstanceNotFound), nil
		}
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	// 更新字段
	if req.AssetName != "" {
		existing.AssetName = req.AssetName
	}
	if req.Attributes != nil {
		// 合并属性
		if existing.Attributes == nil {
			existing.Attributes = make(map[string]interface{})
		}
		for k, v := range req.Attributes {
			existing.Attributes[k] = v
		}
	}

	err = h.svc.Update(ctx.Request.Context(), existing)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(nil), nil
}

// Delete 删除实例
// @Summary 删除资产实例
// @Description 删除指定ID的资产实例
// @Tags 资产实例
// @Accept json
// @Produce json
// @Param id path int true "实例ID"
// @Success 200 {object} ginx.Result "成功"
// @Router /cam/instances/{id} [delete]
func (h *InstanceHandler) Delete(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		ctx.JSON(400, ErrorResult(errs.ParamsError))
		return
	}

	err = h.svc.Delete(ctx.Request.Context(), id)
	if err != nil {
		if err == errs.ErrInstanceNotFound {
			ctx.JSON(404, ErrorResult(errs.InstanceNotFound))
			return
		}
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(nil))
}

// toVO 转换为视图对象
func (h *InstanceHandler) toVO(instance domain.Instance) InstanceVO {
	return InstanceVO{
		ID:         instance.ID,
		ModelUID:   instance.ModelUID,
		AssetID:    instance.AssetID,
		AssetName:  instance.AssetName,
		TenantID:   instance.TenantID,
		AccountID:  instance.AccountID,
		Attributes: instance.Attributes,
		CreateTime: instance.CreateTime.UnixMilli(),
		UpdateTime: instance.UpdateTime.UnixMilli(),
	}
}
