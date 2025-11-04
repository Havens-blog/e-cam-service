package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"go.mongodb.org/mongo-driver/mongo"
)

// ModelService 模型服务接口
type ModelService interface {
	// CreateModel 创建模型
	CreateModel(ctx context.Context, model *domain.Model) (*domain.Model, error)

	// GetModel 获取模型详情（包含字段和分组）
	GetModel(ctx context.Context, uid string) (*domain.ModelDetail, error)

	// GetModelByUID 根据UID获取模型
	GetModelByUID(ctx context.Context, uid string) (*domain.Model, error)

	// ListModels 获取模型列表
	ListModels(ctx context.Context, filter domain.ModelFilter) ([]*domain.Model, int64, error)

	// UpdateModel 更新模型
	UpdateModel(ctx context.Context, uid string, model *domain.Model) error

	// DeleteModel 删除模型（级联删除字段和分组）
	DeleteModel(ctx context.Context, uid string) error

	// AddField 添加字段
	AddField(ctx context.Context, field *domain.ModelField) (*domain.ModelField, error)

	// UpdateField 更新字段
	UpdateField(ctx context.Context, fieldUID string, field *domain.ModelField) error

	// DeleteField 删除字段
	DeleteField(ctx context.Context, fieldUID string) error

	// GetModelFields 获取模型的所有字段
	GetModelFields(ctx context.Context, modelUID string) ([]*domain.ModelField, error)

	// AddFieldGroup 添加字段分组
	AddFieldGroup(ctx context.Context, group *domain.ModelFieldGroup) (*domain.ModelFieldGroup, error)

	// UpdateFieldGroup 更新字段分组
	UpdateFieldGroup(ctx context.Context, id int64, group *domain.ModelFieldGroup) error

	// DeleteFieldGroup 删除字段分组
	DeleteFieldGroup(ctx context.Context, id int64) error

	// GetModelFieldGroups 获取模型的所有分组
	GetModelFieldGroups(ctx context.Context, modelUID string) ([]*domain.ModelFieldGroup, error)
}

type modelService struct {
	modelRepo      repository.ModelRepository
	fieldRepo      repository.ModelFieldRepository
	fieldGroupRepo repository.ModelFieldGroupRepository
}

// NewModelService 创建模型服务
func NewModelService(
	modelRepo repository.ModelRepository,
	fieldRepo repository.ModelFieldRepository,
	fieldGroupRepo repository.ModelFieldGroupRepository,
) ModelService {
	return &modelService{
		modelRepo:      modelRepo,
		fieldRepo:      fieldRepo,
		fieldGroupRepo: fieldGroupRepo,
	}
}

// CreateModel 创建模型
func (s *modelService) CreateModel(ctx context.Context, model *domain.Model) (*domain.Model, error) {
	// 验证模型数据
	if err := model.Validate(); err != nil {
		return nil, fmt.Errorf("model validation failed: %w", err)
	}

	// 检查模型是否已存在
	exists, err := s.modelRepo.ModelExists(ctx, model.UID)
	if err != nil {
		return nil, fmt.Errorf("check model exists failed: %w", err)
	}
	if exists {
		return nil, errs.ModelAlreadyExist
	}

	// 如果有父模型，检查父模型是否存在
	if model.ParentUID != "" {
		parentExists, err := s.modelRepo.ModelExists(ctx, model.ParentUID)
		if err != nil {
			return nil, fmt.Errorf("check parent model exists failed: %w", err)
		}
		if !parentExists {
			return nil, fmt.Errorf("parent model %s not found", model.ParentUID)
		}
	}

	// 创建模型
	id, err := s.modelRepo.CreateModel(ctx, *model)
	if err != nil {
		return nil, fmt.Errorf("create model failed: %w", err)
	}

	// 获取创建后的模型
	createdModel, err := s.modelRepo.GetModelByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get created model failed: %w", err)
	}

	return &createdModel, nil
}

// GetModel 获取模型详情（包含字段和分组）
func (s *modelService) GetModel(ctx context.Context, uid string) (*domain.ModelDetail, error) {
	// 获取模型
	model, err := s.modelRepo.GetModelByUID(ctx, uid)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.ModelNotFound
		}
		return nil, fmt.Errorf("get model failed: %w", err)
	}

	// 获取分组
	groups, err := s.fieldGroupRepo.GetGroupsByModelUID(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("get field groups failed: %w", err)
	}

	// 获取所有字段
	fields, err := s.fieldRepo.GetFieldsByModelUID(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("get fields failed: %w", err)
	}

	// 组装字段到分组
	fieldGroupsWithFields := make([]*domain.FieldGroupWithFields, len(groups))
	for i, group := range groups {
		// 筛选属于该分组的字段
		groupFields := make([]*domain.ModelField, 0)
		for j := range fields {
			if fields[j].GroupID == group.ID {
				groupFields = append(groupFields, &fields[j])
			}
		}

		fieldGroupsWithFields[i] = &domain.FieldGroupWithFields{
			Group:  &group,
			Fields: groupFields,
		}
	}

	return &domain.ModelDetail{
		Model:       &model,
		FieldGroups: fieldGroupsWithFields,
	}, nil
}

// GetModelByUID 根据UID获取模型
func (s *modelService) GetModelByUID(ctx context.Context, uid string) (*domain.Model, error) {
	model, err := s.modelRepo.GetModelByUID(ctx, uid)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.ModelNotFound
		}
		return nil, fmt.Errorf("get model failed: %w", err)
	}
	return &model, nil
}

// ListModels 获取模型列表
func (s *modelService) ListModels(ctx context.Context, filter domain.ModelFilter) ([]*domain.Model, int64, error) {
	// 获取模型列表
	models, err := s.modelRepo.ListModels(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("list models failed: %w", err)
	}

	// 获取总数
	total, err := s.modelRepo.CountModels(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count models failed: %w", err)
	}

	// 转换为指针切片
	result := make([]*domain.Model, len(models))
	for i := range models {
		result[i] = &models[i]
	}

	return result, total, nil
}

// UpdateModel 更新模型
func (s *modelService) UpdateModel(ctx context.Context, uid string, model *domain.Model) error {
	// 检查模型是否存在
	exists, err := s.modelRepo.ModelExists(ctx, uid)
	if err != nil {
		return fmt.Errorf("check model exists failed: %w", err)
	}
	if !exists {
		return errs.ModelNotFound
	}

	// 验证模型数据
	if err := model.Validate(); err != nil {
		return fmt.Errorf("model validation failed: %w", err)
	}

	// 确保UID不变
	model.UID = uid

	// 更新模型
	if err := s.modelRepo.UpdateModel(ctx, *model); err != nil {
		return fmt.Errorf("update model failed: %w", err)
	}

	return nil
}

// DeleteModel 删除模型（级联删除字段和分组）
func (s *modelService) DeleteModel(ctx context.Context, uid string) error {
	// 检查模型是否存在
	exists, err := s.modelRepo.ModelExists(ctx, uid)
	if err != nil {
		return fmt.Errorf("check model exists failed: %w", err)
	}
	if !exists {
		return errs.ModelNotFound
	}

	// 检查是否有子模型
	childModels, err := s.modelRepo.ListModels(ctx, domain.ModelFilter{
		ParentUID: uid,
		Limit:     1,
	})
	if err != nil {
		return fmt.Errorf("check child models failed: %w", err)
	}
	if len(childModels) > 0 {
		return fmt.Errorf("cannot delete model with child models")
	}

	// 删除所有字段
	if err := s.fieldRepo.DeleteFieldsByModelUID(ctx, uid); err != nil {
		return fmt.Errorf("delete fields failed: %w", err)
	}

	// 删除所有分组
	if err := s.fieldGroupRepo.DeleteGroupsByModelUID(ctx, uid); err != nil {
		return fmt.Errorf("delete field groups failed: %w", err)
	}

	// 删除模型
	if err := s.modelRepo.DeleteModel(ctx, uid); err != nil {
		return fmt.Errorf("delete model failed: %w", err)
	}

	return nil
}

// AddField 添加字段
func (s *modelService) AddField(ctx context.Context, field *domain.ModelField) (*domain.ModelField, error) {
	// 验证字段数据
	if err := field.Validate(); err != nil {
		return nil, fmt.Errorf("field validation failed: %w", err)
	}

	// 检查模型是否存在
	exists, err := s.modelRepo.ModelExists(ctx, field.ModelUID)
	if err != nil {
		return nil, fmt.Errorf("check model exists failed: %w", err)
	}
	if !exists {
		return nil, errs.ModelNotFound
	}

	// 检查字段是否已存在
	fieldExists, err := s.fieldRepo.FieldExists(ctx, field.FieldUID)
	if err != nil {
		return nil, fmt.Errorf("check field exists failed: %w", err)
	}
	if fieldExists {
		return nil, errs.FieldAlreadyExist
	}

	// 如果指定了分组，检查分组是否存在
	if field.GroupID > 0 {
		_, err := s.fieldGroupRepo.GetGroupByID(ctx, field.GroupID)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				return nil, errs.GroupNotFound
			}
			return nil, fmt.Errorf("check group exists failed: %w", err)
		}
	}

	// 如果是关联字段，检查关联的模型是否存在
	if field.IsLinkField() {
		linkExists, err := s.modelRepo.ModelExists(ctx, field.LinkModel)
		if err != nil {
			return nil, fmt.Errorf("check link model exists failed: %w", err)
		}
		if !linkExists {
			return nil, fmt.Errorf("link model %s not found", field.Link)
		}
	}

	// 创建字段
	id, err := s.fieldRepo.CreateField(ctx, *field)
	if err != nil {
		return nil, fmt.Errorf("create field failed: %w", err)
	}

	// 获取创建后的字段
	createdField, err := s.fieldRepo.GetFieldByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get created field failed: %w", err)
	}

	return &createdField, nil
}

// UpdateField 更新字段
func (s *modelService) UpdateField(ctx context.Context, fieldUID string, field *domain.ModelField) error {
	// 检查字段是否存在
	exists, err := s.fieldRepo.FieldExists(ctx, fieldUID)
	if err != nil {
		return fmt.Errorf("check field exists failed: %w", err)
	}
	if !exists {
		return errs.FieldNotFound
	}

	// 验证字段数据
	if err := field.Validate(); err != nil {
		return fmt.Errorf("field validation failed: %w", err)
	}

	// 确保FieldUID不变
	field.FieldUID = fieldUID

	// 更新字段
	if err := s.fieldRepo.UpdateField(ctx, *field); err != nil {
		return fmt.Errorf("update field failed: %w", err)
	}

	return nil
}

// DeleteField 删除字段
func (s *modelService) DeleteField(ctx context.Context, fieldUID string) error {
	// 检查字段是否存在
	exists, err := s.fieldRepo.FieldExists(ctx, fieldUID)
	if err != nil {
		return fmt.Errorf("check field exists failed: %w", err)
	}
	if !exists {
		return errs.FieldNotFound
	}

	// 删除字段
	if err := s.fieldRepo.DeleteField(ctx, fieldUID); err != nil {
		return fmt.Errorf("delete field failed: %w", err)
	}

	return nil
}

// GetModelFields 获取模型的所有字段
func (s *modelService) GetModelFields(ctx context.Context, modelUID string) ([]*domain.ModelField, error) {
	fields, err := s.fieldRepo.GetFieldsByModelUID(ctx, modelUID)
	if err != nil {
		return nil, fmt.Errorf("get model fields failed: %w", err)
	}

	// 转换为指针切片
	result := make([]*domain.ModelField, len(fields))
	for i := range fields {
		result[i] = &fields[i]
	}

	return result, nil
}

// AddFieldGroup 添加字段分组
func (s *modelService) AddFieldGroup(ctx context.Context, group *domain.ModelFieldGroup) (*domain.ModelFieldGroup, error) {
	// 验证分组数据
	if err := group.Validate(); err != nil {
		return nil, fmt.Errorf("group validation failed: %w", err)
	}

	// 检查模型是否存在
	exists, err := s.modelRepo.ModelExists(ctx, group.ModelUID)
	if err != nil {
		return nil, fmt.Errorf("check model exists failed: %w", err)
	}
	if !exists {
		return nil, errs.ModelNotFound
	}

	// 创建分组
	id, err := s.fieldGroupRepo.CreateGroup(ctx, *group)
	if err != nil {
		return nil, fmt.Errorf("create group failed: %w", err)
	}

	// 获取创建后的分组
	createdGroup, err := s.fieldGroupRepo.GetGroupByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get created group failed: %w", err)
	}

	return &createdGroup, nil
}

// UpdateFieldGroup 更新字段分组
func (s *modelService) UpdateFieldGroup(ctx context.Context, id int64, group *domain.ModelFieldGroup) error {
	// 检查分组是否存在
	_, err := s.fieldGroupRepo.GetGroupByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.GroupNotFound
		}
		return fmt.Errorf("check group exists failed: %w", err)
	}

	// 验证分组数据
	if err := group.Validate(); err != nil {
		return fmt.Errorf("group validation failed: %w", err)
	}

	// 确保ID不变
	group.ID = id

	// 更新分组
	if err := s.fieldGroupRepo.UpdateGroup(ctx, *group); err != nil {
		return fmt.Errorf("update group failed: %w", err)
	}

	return nil
}

// DeleteFieldGroup 删除字段分组
func (s *modelService) DeleteFieldGroup(ctx context.Context, id int64) error {
	// 检查分组是否存在
	_, err := s.fieldGroupRepo.GetGroupByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.GroupNotFound
		}
		return fmt.Errorf("check group exists failed: %w", err)
	}

	// 检查分组下是否有字段
	fields, err := s.fieldRepo.GetFieldsByGroupID(ctx, id)
	if err != nil {
		return fmt.Errorf("check group fields failed: %w", err)
	}
	if len(fields) > 0 {
		return fmt.Errorf("cannot delete group with fields")
	}

	// 删除分组
	if err := s.fieldGroupRepo.DeleteGroup(ctx, id); err != nil {
		return fmt.Errorf("delete group failed: %w", err)
	}

	return nil
}

// GetModelFieldGroups 获取模型的所有分组
func (s *modelService) GetModelFieldGroups(ctx context.Context, modelUID string) ([]*domain.ModelFieldGroup, error) {
	groups, err := s.fieldGroupRepo.GetGroupsByModelUID(ctx, modelUID)
	if err != nil {
		return nil, fmt.Errorf("get model field groups failed: %w", err)
	}

	// 转换为指针切片
	result := make([]*domain.ModelFieldGroup, len(groups))
	for i := range groups {
		result[i] = &groups[i]
	}

	return result, nil
}
