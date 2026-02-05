// Package asset 资产管理模块
// 这是从 internal/cam 重构后的独立模块
// 当前阶段：别名模式，重新导出 cam 的实现
package asset

import (
	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	camrepo "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	camservice "github.com/Havens-blog/e-cam-service/internal/cam/service"
)

// 重新导出 domain 类型
type (
	Instance        = camdomain.Instance
	InstanceFilter  = camdomain.InstanceFilter
	Model           = camdomain.Model
	ModelFilter     = camdomain.ModelFilter
	ModelField      = camdomain.ModelField
	ModelFieldGroup = camdomain.ModelFieldGroup
)

// 重新导出 repository 类型
type (
	InstanceRepository = camrepo.InstanceRepository
	ModelRepository    = camrepo.ModelRepository
)

// 重新导出 service 类型
type (
	InstanceService = camservice.InstanceService
	ModelService    = camservice.ModelService
)

// NewInstanceRepository 创建实例仓储
var NewInstanceRepository = camrepo.NewInstanceRepository

// NewModelRepository 创建模型仓储
var NewModelRepository = camrepo.NewModelRepository

// NewInstanceService 创建实例服务
var NewInstanceService = camservice.NewInstanceService

// NewModelService 创建模型服务
var NewModelService = camservice.NewModelService
