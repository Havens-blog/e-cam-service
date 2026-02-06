// Package asset 资产管理模块
// 这是从 internal/cam 重构后的独立模块
package asset

import (
	"github.com/Havens-blog/e-cam-service/internal/asset/domain"
	"github.com/Havens-blog/e-cam-service/internal/asset/repository"
	"github.com/Havens-blog/e-cam-service/internal/asset/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/asset/service"

	// 兼容旧代码，导出 cam 的 Model 相关类型
	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	camrepo "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	camservice "github.com/Havens-blog/e-cam-service/internal/cam/service"
)

// Instance 相关类型 (独立实现)
type (
	Instance       = domain.Instance
	InstanceFilter = domain.InstanceFilter
	TagFilter      = domain.TagFilter
	SearchFilter   = domain.SearchFilter
	SearchResult   = domain.SearchResult
)

// Model/Field 相关类型 (独立实现)
type (
	Model                 = domain.Model
	ModelFilter           = domain.ModelFilter
	ModelField            = domain.ModelField
	ModelFieldFilter      = domain.ModelFieldFilter
	ModelFieldGroup       = domain.ModelFieldGroup
	ModelFieldGroupFilter = domain.ModelFieldGroupFilter
)

// 字段类型常量
const (
	FieldTypeString   = domain.FieldTypeString
	FieldTypeInt      = domain.FieldTypeInt
	FieldTypeFloat    = domain.FieldTypeFloat
	FieldTypeBool     = domain.FieldTypeBool
	FieldTypeDateTime = domain.FieldTypeDateTime
	FieldTypeArray    = domain.FieldTypeArray
	FieldTypeObject   = domain.FieldTypeObject
	FieldTypeEnum     = domain.FieldTypeEnum
	FieldTypeLink     = domain.FieldTypeLink
)

// DAO 类型
type InstanceDAO = dao.InstanceDAO

// Repository 类型
type InstanceRepository = repository.InstanceRepository

// Service 类型
type InstanceService = service.InstanceService

// 构造函数 (独立实现)
var (
	NewInstanceDAO        = dao.NewInstanceDAO
	NewInstanceRepository = repository.NewInstanceRepository
	NewInstanceService    = service.NewInstanceService
)

// ============ 兼容旧代码：Model 相关 (仍使用 cam 实现) ============

// Model 相关 Repository 类型 (别名到 cam)
type ModelRepository = camrepo.ModelRepository

// Model 相关 Service 类型 (别名到 cam)
type ModelService = camservice.ModelService

// Model 相关构造函数 (别名到 cam)
var (
	NewModelRepository = camrepo.NewModelRepository
	NewModelService    = camservice.NewModelService
)

// ============ 兼容旧代码：其他 cam domain 类型 ============

// 其他 cam domain 类型 (别名)
type (
	InstanceRelation       = camdomain.InstanceRelation
	InstanceRelationFilter = camdomain.InstanceRelationFilter
	ModelGroup             = camdomain.ModelGroup
	RelationType           = camdomain.RelationType
	ModelRelation          = camdomain.ModelRelation
)

// 关系映射常量
const (
	MappingOneToOne   = camdomain.MappingOneToOne
	MappingOneToMany  = camdomain.MappingOneToMany
	MappingManyToMany = camdomain.MappingManyToMany
)
