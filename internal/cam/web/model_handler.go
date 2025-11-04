package web

import (
	"strconv"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/pkg/ginx"
	"github.com/gin-gonic/gin"
)

// ==================== 模型管理处理器 ====================

// CreateModel 创建模型
func (h *Handler) CreateModel(ctx *gin.Context, req CreateModelReq) (ginx.Result, error) {
	model := &domain.Model{
		UID:          req.UID,
		Name:         req.Name,
		ModelGroupID: req.ModelGroupID,
		ParentUID:    req.ParentUID,
		Category:     req.Category,
		Level:        req.Level,
		Icon:         req.Icon,
		Description:  req.Description,
		Provider:     req.Provider,
		Extensible:   req.Extensible,
		CreateTime:   time.Now(),
		UpdateTime:   time.Now(),
	}

	createdModel, err := h.modelSvc.CreateModel(ctx.Request.Context(), model)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(h.toModelVO(createdModel)), nil
}

// GetModel 获取模型详情
func (h *Handler) GetModel(ctx *gin.Context) {
	uid := ctx.Param("uid")

	modelDetail, err := h.modelSvc.GetModel(ctx.Request.Context(), uid)
	if err != nil {
		ctx.JSON(404, ErrorResultWithMsg(errs.ModelNotFound, err.Error()))
		return
	}

	ctx.JSON(200, Result(h.toModelDetailVO(modelDetail)))
}

// ListModels 获取模型列表
func (h *Handler) ListModels(ctx *gin.Context) {
	provider := ctx.Query("provider")
	category := ctx.Query("category")
	parentUID := ctx.Query("parent_uid")
	levelStr := ctx.Query("level")
	extensibleStr := ctx.Query("extensible")

	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	filter := domain.ModelFilter{
		Provider:  provider,
		Category:  category,
		ParentUID: parentUID,
		Offset:    offset,
		Limit:     limit,
	}

	if levelStr != "" {
		level, _ := strconv.Atoi(levelStr)
		filter.Level = level
	}

	if extensibleStr != "" {
		extensible := extensibleStr == "true"
		filter.Extensible = &extensible
	}

	models, total, err := h.modelSvc.ListModels(ctx.Request.Context(), filter)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	modelVOs := make([]*ModelVO, len(models))
	for i, model := range models {
		modelVOs[i] = h.toModelVO(model)
	}

	resp := ModelListResp{
		Models: modelVOs,
		Total:  total,
	}

	ctx.JSON(200, Result(resp))
}

// UpdateModel 更新模型
func (h *Handler) UpdateModel(ctx *gin.Context, req UpdateModelReq) (ginx.Result, error) {
	uid := ctx.Param("uid")

	// 获取现有模型
	existingModel, err := h.modelSvc.GetModelByUID(ctx.Request.Context(), uid)
	if err != nil {
		return ErrorResult(errs.ModelNotFound), nil
	}

	// 更新字段
	if req.Name != "" {
		existingModel.Name = req.Name
	}
	if req.ModelGroupID > 0 {
		existingModel.ModelGroupID = req.ModelGroupID
	}
	if req.Icon != "" {
		existingModel.Icon = req.Icon
	}
	if req.Description != "" {
		existingModel.Description = req.Description
	}
	existingModel.Extensible = req.Extensible
	existingModel.UpdateTime = time.Now()

	err = h.modelSvc.UpdateModel(ctx.Request.Context(), uid, existingModel)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(nil), nil
}

// DeleteModel 删除模型
func (h *Handler) DeleteModel(ctx *gin.Context) {
	uid := ctx.Param("uid")

	err := h.modelSvc.DeleteModel(ctx.Request.Context(), uid)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(nil))
}

// ==================== 字段管理处理器 ====================

// AddField 添加字段
func (h *Handler) AddField(ctx *gin.Context, req CreateFieldReq) (ginx.Result, error) {
	field := &domain.ModelField{
		FieldUID:    req.FieldUID,
		FieldName:   req.FieldName,
		FieldType:   req.FieldType,
		ModelUID:    req.ModelUID,
		GroupID:     req.GroupID,
		DisplayName: req.DisplayName,
		Display:     req.Display,
		Index:       req.Index,
		Required:    req.Required,
		Secure:      req.Secure,
		Link:        req.Link,
		LinkModel:   req.LinkModel,
		Option:      req.Option,
		CreateTime:  time.Now(),
		UpdateTime:  time.Now(),
	}

	createdField, err := h.modelSvc.AddField(ctx.Request.Context(), field)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(h.toFieldVO(createdField)), nil
}

// GetModelFields 获取模型的所有字段
func (h *Handler) GetModelFields(ctx *gin.Context) {
	uid := ctx.Param("uid")

	fields, err := h.modelSvc.GetModelFields(ctx.Request.Context(), uid)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	fieldVOs := make([]*ModelFieldVO, len(fields))
	for i, field := range fields {
		fieldVOs[i] = h.toFieldVO(field)
	}

	resp := FieldListResp{
		Fields: fieldVOs,
		Total:  int64(len(fieldVOs)),
	}

	ctx.JSON(200, Result(resp))
}

// UpdateField 更新字段
func (h *Handler) UpdateField(ctx *gin.Context, req UpdateFieldReq) (ginx.Result, error) {
	fieldUID := ctx.Param("field_uid")

	// 这里简化处理，实际应该先获取现有字段再更新
	field := &domain.ModelField{
		FieldUID:    fieldUID,
		FieldName:   req.FieldName,
		FieldType:   req.FieldType,
		GroupID:     req.GroupID,
		DisplayName: req.DisplayName,
		Display:     req.Display,
		Index:       req.Index,
		Required:    req.Required,
		Secure:      req.Secure,
		Link:        req.Link,
		LinkModel:   req.LinkModel,
		Option:      req.Option,
		UpdateTime:  time.Now(),
	}

	err := h.modelSvc.UpdateField(ctx.Request.Context(), fieldUID, field)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(nil), nil
}

// DeleteField 删除字段
func (h *Handler) DeleteField(ctx *gin.Context) {
	fieldUID := ctx.Param("field_uid")

	err := h.modelSvc.DeleteField(ctx.Request.Context(), fieldUID)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(nil))
}

// ==================== 字段分组管理处理器 ====================

// AddFieldGroup 添加字段分组
func (h *Handler) AddFieldGroup(ctx *gin.Context, req CreateFieldGroupReq) (ginx.Result, error) {
	group := &domain.ModelFieldGroup{
		ModelUID:   req.ModelUID,
		Name:       req.Name,
		Index:      req.Index,
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
	}

	createdGroup, err := h.modelSvc.AddFieldGroup(ctx.Request.Context(), group)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(h.toFieldGroupVO(createdGroup)), nil
}

// GetModelFieldGroups 获取模型的所有分组
func (h *Handler) GetModelFieldGroups(ctx *gin.Context) {
	uid := ctx.Param("uid")

	groups, err := h.modelSvc.GetModelFieldGroups(ctx.Request.Context(), uid)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	groupVOs := make([]*ModelFieldGroupVO, len(groups))
	for i, group := range groups {
		groupVOs[i] = h.toFieldGroupVO(group)
	}

	resp := FieldGroupListResp{
		Groups: groupVOs,
		Total:  int64(len(groupVOs)),
	}

	ctx.JSON(200, Result(resp))
}

// UpdateFieldGroup 更新字段分组
func (h *Handler) UpdateFieldGroup(ctx *gin.Context, req UpdateFieldGroupReq) (ginx.Result, error) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return ErrorResult(errs.ParamsError), nil
	}

	group := &domain.ModelFieldGroup{
		ID:         id,
		Name:       req.Name,
		Index:      req.Index,
		UpdateTime: time.Now(),
	}

	err = h.modelSvc.UpdateFieldGroup(ctx.Request.Context(), id, group)
	if err != nil {
		return ErrorResultWithMsg(errs.SystemError, err.Error()), nil
	}

	return Result(nil), nil
}

// DeleteFieldGroup 删除字段分组
func (h *Handler) DeleteFieldGroup(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		ctx.JSON(400, ErrorResult(errs.ParamsError))
		return
	}

	err = h.modelSvc.DeleteFieldGroup(ctx.Request.Context(), id)
	if err != nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}

	ctx.JSON(200, Result(nil))
}

// ==================== VO 转换方法 ====================

// toModelVO 将领域对象转换为VO
func (h *Handler) toModelVO(model *domain.Model) *ModelVO {
	return &ModelVO{
		ID:           model.ID,
		UID:          model.UID,
		Name:         model.Name,
		ModelGroupID: model.ModelGroupID,
		ParentUID:    model.ParentUID,
		Category:     model.Category,
		Level:        model.Level,
		Icon:         model.Icon,
		Description:  model.Description,
		Provider:     model.Provider,
		Extensible:   model.Extensible,
		CreateTime:   model.CreateTime,
		UpdateTime:   model.UpdateTime,
	}
}

// toFieldVO 将字段领域对象转换为VO
func (h *Handler) toFieldVO(field *domain.ModelField) *ModelFieldVO {
	return &ModelFieldVO{
		ID:          field.ID,
		FieldUID:    field.FieldUID,
		FieldName:   field.FieldName,
		FieldType:   field.FieldType,
		ModelUID:    field.ModelUID,
		GroupID:     field.GroupID,
		DisplayName: field.DisplayName,
		Display:     field.Display,
		Index:       field.Index,
		Required:    field.Required,
		Secure:      field.Secure,
		Link:        field.Link,
		LinkModel:   field.LinkModel,
		Option:      field.Option,
		CreateTime:  field.CreateTime,
		UpdateTime:  field.UpdateTime,
	}
}

// toFieldGroupVO 将分组领域对象转换为VO
func (h *Handler) toFieldGroupVO(group *domain.ModelFieldGroup) *ModelFieldGroupVO {
	return &ModelFieldGroupVO{
		ID:         group.ID,
		ModelUID:   group.ModelUID,
		Name:       group.Name,
		Index:      group.Index,
		CreateTime: group.CreateTime,
		UpdateTime: group.UpdateTime,
	}
}

// toModelDetailVO 将模型详情转换为VO
func (h *Handler) toModelDetailVO(detail *domain.ModelDetail) *ModelDetailVO {
	fieldGroups := make([]*FieldGroupWithFieldsVO, len(detail.FieldGroups))
	for i, fg := range detail.FieldGroups {
		fields := make([]*ModelFieldVO, len(fg.Fields))
		for j, f := range fg.Fields {
			fields[j] = h.toFieldVO(f)
		}

		fieldGroups[i] = &FieldGroupWithFieldsVO{
			Group:  h.toFieldGroupVO(fg.Group),
			Fields: fields,
		}
	}

	return &ModelDetailVO{
		Model:       h.toModelVO(detail.Model),
		FieldGroups: fieldGroups,
	}
}
