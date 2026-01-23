package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
)

// ModelFieldRepository 字段仓储接口
type ModelFieldRepository interface {
	// CreateField 创建字段
	CreateField(ctx context.Context, field domain.ModelField) (int64, error)

	// GetFieldByUID 根据UID获取字段
	GetFieldByUID(ctx context.Context, fieldUID string) (domain.ModelField, error)

	// GetFieldByID 根据ID获取字段
	GetFieldByID(ctx context.Context, id int64) (domain.ModelField, error)

	// ListFields 获取字段列表
	ListFields(ctx context.Context, filter domain.ModelFieldFilter) ([]domain.ModelField, error)

	// GetFieldsByModelUID 获取模型的所有字段
	GetFieldsByModelUID(ctx context.Context, modelUID string) ([]domain.ModelField, error)

	// GetFieldsByGroupID 获取分组的所有字段
	GetFieldsByGroupID(ctx context.Context, groupID int64) ([]domain.ModelField, error)

	// UpdateField 更新字段
	UpdateField(ctx context.Context, field domain.ModelField) error

	// DeleteField 删除字段
	DeleteField(ctx context.Context, fieldUID string) error

	// DeleteFieldsByModelUID 删除模型的所有字段
	DeleteFieldsByModelUID(ctx context.Context, modelUID string) error

	// FieldExists 检查字段是否存在
	FieldExists(ctx context.Context, fieldUID string) (bool, error)
}

type modelFieldRepository struct {
	dao dao.ModelFieldDAO
}

// NewModelFieldRepository 创建字段仓储
func NewModelFieldRepository(dao dao.ModelFieldDAO) ModelFieldRepository {
	return &modelFieldRepository{
		dao: dao,
	}
}

// CreateField 创建字段
func (r *modelFieldRepository) CreateField(ctx context.Context, field domain.ModelField) (int64, error) {
	daoField := r.toEntity(field)
	return r.dao.CreateField(ctx, daoField)
}

// GetFieldByUID 根据UID获取字段
func (r *modelFieldRepository) GetFieldByUID(ctx context.Context, fieldUID string) (domain.ModelField, error) {
	daoField, err := r.dao.GetFieldByUID(ctx, fieldUID)
	if err != nil {
		return domain.ModelField{}, err
	}
	return r.toDomain(daoField), nil
}

// GetFieldByID 根据ID获取字段
func (r *modelFieldRepository) GetFieldByID(ctx context.Context, id int64) (domain.ModelField, error) {
	daoField, err := r.dao.GetFieldByID(ctx, id)
	if err != nil {
		return domain.ModelField{}, err
	}
	return r.toDomain(daoField), nil
}

// ListFields 获取字段列表
func (r *modelFieldRepository) ListFields(ctx context.Context, filter domain.ModelFieldFilter) ([]domain.ModelField, error) {
	daoFilter := dao.ModelFieldFilter{
		ModelUID:  filter.ModelUID,
		GroupID:   filter.GroupID,
		FieldType: filter.FieldType,
		Required:  filter.Required,
		Secure:    filter.Secure,
		Offset:    filter.Offset,
		Limit:     filter.Limit,
	}

	daoFields, err := r.dao.ListFields(ctx, daoFilter)
	if err != nil {
		return nil, err
	}

	fields := make([]domain.ModelField, len(daoFields))
	for i, daoField := range daoFields {
		fields[i] = r.toDomain(daoField)
	}

	return fields, nil
}

// GetFieldsByModelUID 获取模型的所有字段
func (r *modelFieldRepository) GetFieldsByModelUID(ctx context.Context, modelUID string) ([]domain.ModelField, error) {
	daoFields, err := r.dao.GetFieldsByModelUID(ctx, modelUID)
	if err != nil {
		return nil, err
	}

	fields := make([]domain.ModelField, len(daoFields))
	for i, daoField := range daoFields {
		fields[i] = r.toDomain(daoField)
	}

	return fields, nil
}

// GetFieldsByGroupID 获取分组的所有字段
func (r *modelFieldRepository) GetFieldsByGroupID(ctx context.Context, groupID int64) ([]domain.ModelField, error) {
	daoFields, err := r.dao.GetFieldsByGroupID(ctx, groupID)
	if err != nil {
		return nil, err
	}

	fields := make([]domain.ModelField, len(daoFields))
	for i, daoField := range daoFields {
		fields[i] = r.toDomain(daoField)
	}

	return fields, nil
}

// UpdateField 更新字段
func (r *modelFieldRepository) UpdateField(ctx context.Context, field domain.ModelField) error {
	daoField := r.toEntity(field)
	return r.dao.UpdateField(ctx, daoField)
}

// DeleteField 删除字段
func (r *modelFieldRepository) DeleteField(ctx context.Context, fieldUID string) error {
	return r.dao.DeleteField(ctx, fieldUID)
}

// DeleteFieldsByModelUID 删除模型的所有字段
func (r *modelFieldRepository) DeleteFieldsByModelUID(ctx context.Context, modelUID string) error {
	return r.dao.DeleteFieldsByModelUID(ctx, modelUID)
}

// FieldExists 检查字段是否存在
func (r *modelFieldRepository) FieldExists(ctx context.Context, fieldUID string) (bool, error) {
	return r.dao.FieldExists(ctx, fieldUID)
}

// toDomain 将DAO对象转换为领域对象
func (r *modelFieldRepository) toDomain(daoField dao.ModelField) domain.ModelField {
	return domain.ModelField{
		ID:          daoField.ID,
		FieldUID:    daoField.FieldUID,
		FieldName:   daoField.FieldName,
		FieldType:   daoField.FieldType,
		ModelUID:    daoField.ModelUID,
		GroupID:     daoField.GroupID,
		DisplayName: daoField.DisplayName,
		Display:     daoField.Display,
		Index:       daoField.Index,
		Required:    daoField.Required,
		Secure:      daoField.Secure,
		Link:        daoField.Link,
		LinkModel:   daoField.LinkModel,
		Option:      daoField.Option,
		CreateTime:  time.UnixMilli(daoField.Ctime),
		UpdateTime:  time.UnixMilli(daoField.Utime),
	}
}

// toEntity 将领域对象转换为DAO对象
func (r *modelFieldRepository) toEntity(field domain.ModelField) dao.ModelField {
	return dao.ModelField{
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
		Ctime:       field.CreateTime.UnixMilli(),
		Utime:       field.UpdateTime.UnixMilli(),
	}
}
