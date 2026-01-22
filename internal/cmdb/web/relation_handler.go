package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cmdb/domain"
	"github.com/Havens-blog/e-cam-service/internal/cmdb/errs"
	"github.com/Havens-blog/e-cam-service/internal/cmdb/service"
	"github.com/Havens-blog/e-cam-service/pkg/ginx"
	"github.com/gin-gonic/gin"
)

// RelationHandler 关系HTTP处理器
type RelationHandler struct {
	modelRelSvc service.ModelRelationTypeService
	relationSvc service.RelationService
	topologySvc service.TopologyService
}

// NewRelationHandler 创建关系处理器
func NewRelationHandler(
	modelRelSvc service.ModelRelationTypeService,
	relationSvc service.RelationService,
	topologySvc service.TopologyService,
) *RelationHandler {
	return &RelationHandler{
		modelRelSvc: modelRelSvc,
		relationSvc: relationSvc,
		topologySvc: topologySvc,
	}
}

// RegisterRoutes 注册关系相关路由
func (h *RelationHandler) RegisterRoutes(r *gin.RouterGroup) {
	// 模型关系类型
	modelRelGroup := r.Group("/model-relations")
	{
		modelRelGroup.POST("", ginx.WrapBody[CreateModelRelationReq](h.CreateModelRelation))
		modelRelGroup.GET("", h.ListModelRelations)
		modelRelGroup.GET("/:uid", h.GetModelRelation)
		modelRelGroup.PUT("/:uid", ginx.WrapBody[UpdateModelRelationReq](h.UpdateModelRelation))
		modelRelGroup.DELETE("/:uid", h.DeleteModelRelation)
	}

	// 实例关系
	instRelGroup := r.Group("/instance-relations")
	{
		instRelGroup.POST("", ginx.WrapBody[CreateInstanceRelationReq](h.CreateInstanceRelation))
		instRelGroup.POST("/batch", ginx.WrapBody[CreateBatchInstanceRelationReq](h.CreateBatchInstanceRelation))
		instRelGroup.GET("", h.ListInstanceRelations)
		instRelGroup.DELETE("/:id", h.DeleteInstanceRelation)
	}

	// 拓扑
	topoGroup := r.Group("/topology")
	{
		topoGroup.GET("/instance/:id", h.GetInstanceTopology)
		topoGroup.GET("/model", h.GetModelTopology)
		topoGroup.GET("/related/:id", h.GetRelatedInstances)
	}
}

// ==================== 请求/响应结构体 ====================

// CreateModelRelationReq 创建模型关系类型请求
type CreateModelRelationReq struct {
	UID            string `json:"uid" binding:"required"`
	Name           string `json:"name" binding:"required"`
	SourceModelUID string `json:"source_model_uid" binding:"required"`
	TargetModelUID string `json:"target_model_uid" binding:"required"`
	RelationType   string `json:"relation_type" binding:"required"`
	Direction      string `json:"direction"`
	SourceToTarget string `json:"source_to_target"`
	TargetToSource string `json:"target_to_source"`
	Description    string `json:"description"`
}

// UpdateModelRelationReq 更新模型关系类型请求
type UpdateModelRelationReq struct {
	Name           string `json:"name"`
	SourceToTarget string `json:"source_to_target"`
	TargetToSource string `json:"target_to_source"`
	Description    string `json:"description"`
}

// CreateInstanceRelationReq 创建实例关系请求
type CreateInstanceRelationReq struct {
	SourceInstanceID int64  `json:"source_instance_id" binding:"required"`
	TargetInstanceID int64  `json:"target_instance_id" binding:"required"`
	RelationTypeUID  string `json:"relation_type_uid" binding:"required"`
	TenantID         string `json:"tenant_id" binding:"required"`
}

// CreateBatchInstanceRelationReq 批量创建实例关系请求
type CreateBatchInstanceRelationReq struct {
	Relations []CreateInstanceRelationReq `json:"relations" binding:"required"`
}

// ModelRelationVO 模型关系类型视图对象
type ModelRelationVO struct {
	ID             int64  `json:"id"`
	UID            string `json:"uid"`
	Name           string `json:"name"`
	SourceModelUID string `json:"source_model_uid"`
	TargetModelUID string `json:"target_model_uid"`
	RelationType   string `json:"relation_type"`
	Direction      string `json:"direction"`
	SourceToTarget string `json:"source_to_target"`
	TargetToSource string `json:"target_to_source"`
	Description    string `json:"description"`
	CreateTime     int64  `json:"create_time"`
	UpdateTime     int64  `json:"update_time"`
}

// InstanceRelationVO 实例关系视图对象
type InstanceRelationVO struct {
	ID               int64  `json:"id"`
	SourceInstanceID int64  `json:"source_instance_id"`
	TargetInstanceID int64  `json:"target_instance_id"`
	RelationTypeUID  string `json:"relation_type_uid"`
	TenantID         string `json:"tenant_id"`
	CreateTime       int64  `json:"create_time"`
}

// ==================== 模型关系类型处理方法 ====================

func (h *RelationHandler) CreateModelRelation(ctx *gin.Context, req CreateModelRelationReq) (ginx.Result, error) {
	rel := domain.ModelRelationType{
		UID:            req.UID,
		Name:           req.Name,
		SourceModelUID: req.SourceModelUID,
		TargetModelUID: req.TargetModelUID,
		RelationType:   req.RelationType,
		Direction:      req.Direction,
		SourceToTarget: req.SourceToTarget,
		TargetToSource: req.TargetToSource,
		Description:    req.Description,
	}

	id, err := h.modelRelSvc.Create(ctx.Request.Context(), rel)
	if err != nil {
		if err == errs.ErrRelationExists {
			return ErrorResultWithMsg(errs.ParamsError, "relation type already exists"), nil
		}
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(map[string]interface{}{"id": id}), nil
}

func (h *RelationHandler) ListModelRelations(ctx *gin.Context) {
	sourceModelUID := ctx.Query("source_model_uid")
	targetModelUID := ctx.Query("target_model_uid")
	relationType := ctx.Query("relation_type")
	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	filter := domain.ModelRelationTypeFilter{
		SourceModelUID: sourceModelUID,
		TargetModelUID: targetModelUID,
		RelationType:   relationType,
		Offset:         offset,
		Limit:          limit,
	}

	rels, total, err := h.modelRelSvc.List(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	vos := make([]ModelRelationVO, len(rels))
	for i, rel := range rels {
		vos[i] = h.toModelRelationVO(rel)
	}

	ctx.JSON(200, Result(map[string]interface{}{"relations": vos, "total": total}))
}

func (h *RelationHandler) GetModelRelation(ctx *gin.Context) {
	uid := ctx.Param("uid")

	rel, err := h.modelRelSvc.GetByUID(ctx.Request.Context(), uid)
	if err != nil {
		if err == errs.ErrRelationNotFound {
			ctx.JSON(404, ErrorResult(errs.RelationNotFound))
			return
		}
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(h.toModelRelationVO(rel)))
}

func (h *RelationHandler) UpdateModelRelation(ctx *gin.Context, req UpdateModelRelationReq) (ginx.Result, error) {
	uid := ctx.Param("uid")

	existing, err := h.modelRelSvc.GetByUID(ctx.Request.Context(), uid)
	if err != nil {
		if err == errs.ErrRelationNotFound {
			return ErrorResult(errs.RelationNotFound), nil
		}
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.SourceToTarget != "" {
		existing.SourceToTarget = req.SourceToTarget
	}
	if req.TargetToSource != "" {
		existing.TargetToSource = req.TargetToSource
	}
	if req.Description != "" {
		existing.Description = req.Description
	}

	err = h.modelRelSvc.Update(ctx.Request.Context(), existing)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(nil), nil
}

func (h *RelationHandler) DeleteModelRelation(ctx *gin.Context) {
	uid := ctx.Param("uid")

	err := h.modelRelSvc.Delete(ctx.Request.Context(), uid)
	if err != nil {
		if err == errs.ErrRelationNotFound {
			ctx.JSON(404, ErrorResult(errs.RelationNotFound))
			return
		}
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(nil))
}

// ==================== 实例关系处理方法 ====================

func (h *RelationHandler) CreateInstanceRelation(ctx *gin.Context, req CreateInstanceRelationReq) (ginx.Result, error) {
	rel := domain.InstanceRelation{
		SourceInstanceID: req.SourceInstanceID,
		TargetInstanceID: req.TargetInstanceID,
		RelationTypeUID:  req.RelationTypeUID,
		TenantID:         req.TenantID,
	}

	id, err := h.relationSvc.Create(ctx.Request.Context(), rel)
	if err != nil {
		if err == errs.ErrRelationExists {
			return ErrorResultWithMsg(errs.ParamsError, "relation already exists"), nil
		}
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(map[string]interface{}{"id": id}), nil
}

func (h *RelationHandler) CreateBatchInstanceRelation(ctx *gin.Context, req CreateBatchInstanceRelationReq) (ginx.Result, error) {
	rels := make([]domain.InstanceRelation, len(req.Relations))
	for i, r := range req.Relations {
		rels[i] = domain.InstanceRelation{
			SourceInstanceID: r.SourceInstanceID,
			TargetInstanceID: r.TargetInstanceID,
			RelationTypeUID:  r.RelationTypeUID,
			TenantID:         r.TenantID,
		}
	}

	count, err := h.relationSvc.CreateBatch(ctx.Request.Context(), rels)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(map[string]interface{}{"count": count}), nil
}

func (h *RelationHandler) ListInstanceRelations(ctx *gin.Context) {
	sourceIDStr := ctx.Query("source_instance_id")
	targetIDStr := ctx.Query("target_instance_id")
	relationTypeUID := ctx.Query("relation_type_uid")
	tenantID := ctx.Query("tenant_id")
	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	var sourceID, targetID int64
	if sourceIDStr != "" {
		sourceID, _ = strconv.ParseInt(sourceIDStr, 10, 64)
	}
	if targetIDStr != "" {
		targetID, _ = strconv.ParseInt(targetIDStr, 10, 64)
	}

	filter := domain.InstanceRelationFilter{
		SourceInstanceID: sourceID,
		TargetInstanceID: targetID,
		RelationTypeUID:  relationTypeUID,
		TenantID:         tenantID,
		Offset:           int64(offset),
		Limit:            int64(limit),
	}

	rels, total, err := h.relationSvc.List(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	vos := make([]InstanceRelationVO, len(rels))
	for i, rel := range rels {
		vos[i] = InstanceRelationVO{
			ID:               rel.ID,
			SourceInstanceID: rel.SourceInstanceID,
			TargetInstanceID: rel.TargetInstanceID,
			RelationTypeUID:  rel.RelationTypeUID,
			TenantID:         rel.TenantID,
			CreateTime:       rel.CreateTime.UnixMilli(),
		}
	}

	ctx.JSON(200, Result(map[string]interface{}{"relations": vos, "total": total}))
}

func (h *RelationHandler) DeleteInstanceRelation(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		ctx.JSON(400, ErrorResult(errs.ParamsError))
		return
	}

	err = h.relationSvc.Delete(ctx.Request.Context(), id)
	if err != nil {
		if err == errs.ErrRelationNotFound {
			ctx.JSON(404, ErrorResult(errs.RelationNotFound))
			return
		}
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(nil))
}

// ==================== 拓扑处理方法 ====================

func (h *RelationHandler) GetInstanceTopology(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		ctx.JSON(400, ErrorResult(errs.ParamsError))
		return
	}

	depthStr := ctx.DefaultQuery("depth", "1")
	depth, _ := strconv.Atoi(depthStr)
	direction := ctx.DefaultQuery("direction", "both")
	modelUID := ctx.Query("model_uid")
	tenantID := ctx.Query("tenant_id")

	query := domain.TopologyQuery{
		InstanceID: id,
		ModelUID:   modelUID,
		TenantID:   tenantID,
		Depth:      depth,
		Direction:  direction,
	}

	graph, err := h.topologySvc.GetInstanceTopology(ctx.Request.Context(), query)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(graph))
}

func (h *RelationHandler) GetModelTopology(ctx *gin.Context) {
	provider := ctx.Query("provider")

	graph, err := h.topologySvc.GetModelTopology(ctx.Request.Context(), provider)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(graph))
}

func (h *RelationHandler) GetRelatedInstances(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		ctx.JSON(400, ErrorResult(errs.ParamsError))
		return
	}

	relationTypeUID := ctx.Query("relation_type_uid")

	instances, err := h.topologySvc.GetRelatedInstances(ctx.Request.Context(), id, relationTypeUID)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	vos := make([]InstanceVO, len(instances))
	for i, inst := range instances {
		vos[i] = InstanceVO{
			ID:         inst.ID,
			ModelUID:   inst.ModelUID,
			AssetID:    inst.AssetID,
			AssetName:  inst.AssetName,
			TenantID:   inst.TenantID,
			AccountID:  inst.AccountID,
			Attributes: inst.Attributes,
			CreateTime: inst.CreateTime.UnixMilli(),
			UpdateTime: inst.UpdateTime.UnixMilli(),
		}
	}

	ctx.JSON(200, Result(map[string]interface{}{"instances": vos, "total": len(vos)}))
}

func (h *RelationHandler) toModelRelationVO(rel domain.ModelRelationType) ModelRelationVO {
	return ModelRelationVO{
		ID:             rel.ID,
		UID:            rel.UID,
		Name:           rel.Name,
		SourceModelUID: rel.SourceModelUID,
		TargetModelUID: rel.TargetModelUID,
		RelationType:   rel.RelationType,
		Direction:      rel.Direction,
		SourceToTarget: rel.SourceToTarget,
		TargetToSource: rel.TargetToSource,
		Description:    rel.Description,
		CreateTime:     rel.CreateTime.UnixMilli(),
		UpdateTime:     rel.UpdateTime.UnixMilli(),
	}
}
