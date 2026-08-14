package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/gotomicro/ego/core/elog"
)

// ModelInitializer 模型初始化器
type ModelInitializer struct {
	modelRepo        repository.ModelRepository
	fieldRepo        repository.ModelFieldRepository
	groupRepo        repository.ModelFieldGroupRepository
	modelGroupDAO    *dao.ModelGroupDAO
	relationTypeDAO  *dao.RelationTypeDAO
	modelRelationDAO *dao.ModelRelationDAO
	logger           *elog.Component
}

// NewModelInitializer 创建模型初始化器
func NewModelInitializer(
	modelRepo repository.ModelRepository,
	fieldRepo repository.ModelFieldRepository,
	groupRepo repository.ModelFieldGroupRepository,
	modelGroupDAO *dao.ModelGroupDAO,
	relationTypeDAO *dao.RelationTypeDAO,
	modelRelationDAO *dao.ModelRelationDAO,
	logger *elog.Component,
) *ModelInitializer {
	return &ModelInitializer{
		modelRepo:        modelRepo,
		fieldRepo:        fieldRepo,
		groupRepo:        groupRepo,
		modelGroupDAO:    modelGroupDAO,
		relationTypeDAO:  relationTypeDAO,
		modelRelationDAO: modelRelationDAO,
		logger:           logger,
	}
}

// InitializeModels 初始化所有预定义模型
func (i *ModelInitializer) InitializeModels(ctx context.Context) error {
	i.logger.Info("开始初始化云资源模型")

	// 1. 初始化模型分组
	if err := i.initModelGroups(ctx); err != nil {
		return fmt.Errorf("初始化模型分组失败: %w", err)
	}

	// 2. 初始化关系类型
	if err := i.initRelationTypes(ctx); err != nil {
		return fmt.Errorf("初始化关系类型失败: %w", err)
	}

	// 3. 初始化云主机模型
	if err := i.initECSModel(ctx); err != nil {
		return fmt.Errorf("初始化云主机模型失败: %w", err)
	}

	// 4. 初始化CDN模型
	if err := i.initCDNModel(ctx); err != nil {
		return fmt.Errorf("初始化CDN模型失败: %w", err)
	}

	// 5. 初始化WAF模型
	if err := i.initWAFModel(ctx); err != nil {
		return fmt.Errorf("初始化WAF模型失败: %w", err)
	}

	// 6. 初始化负载均衡模型
	if err := i.initLBModel(ctx); err != nil {
		return fmt.Errorf("初始化负载均衡模型失败: %w", err)
	}

	// 7. 初始化模型关系
	if err := i.initModelRelations(ctx); err != nil {
		return fmt.Errorf("初始化模型关系失败: %w", err)
	}

	i.logger.Info("云资源模型初始化完成")
	return nil
}

// initModelGroups 初始化模型分组
func (i *ModelInitializer) initModelGroups(ctx context.Context) error {
	i.logger.Info("初始化模型分组")

	groups := []domain.ModelGroup{
		{ID: 1, Name: "计算资源"},
		{ID: 2, Name: "网络资源"},
		{ID: 3, Name: "存储资源"},
		{ID: 4, Name: "安全资源"},
		{ID: 5, Name: "数据库资源"},
	}

	for _, group := range groups {
		exists, err := i.modelGroupDAO.Exists(ctx, group.ID)
		if err != nil {
			return fmt.Errorf("检查模型分组是否存在失败: %w", err)
		}
		if exists {
			i.logger.Info("模型分组已存在，跳过", elog.Int64("id", group.ID), elog.String("name", group.Name))
			continue
		}

		if _, err := i.modelGroupDAO.Create(ctx, group); err != nil {
			return fmt.Errorf("创建模型分组失败 %s: %w", group.Name, err)
		}
		i.logger.Info("创建模型分组成功", elog.Int64("id", group.ID), elog.String("name", group.Name))
	}

	return nil
}

// initRelationTypes 初始化关系类型
func (i *ModelInitializer) initRelationTypes(ctx context.Context) error {
	i.logger.Info("初始化关系类型")

	relationTypes := []domain.RelationType{
		{
			UID:            "deploy_on",
			Name:           "部署于",
			SourceDescribe: "部署在",
			TargetDescribe: "被部署",
		},
		{
			UID:            "connect_to",
			Name:           "连接到",
			SourceDescribe: "连接",
			TargetDescribe: "被连接",
		},
		{
			UID:            "protect_by",
			Name:           "防护于",
			SourceDescribe: "受保护于",
			TargetDescribe: "保护",
		},
		{
			UID:            "use",
			Name:           "使用",
			SourceDescribe: "使用",
			TargetDescribe: "被使用",
		},
		{
			UID:            "belong_to",
			Name:           "属于",
			SourceDescribe: "属于",
			TargetDescribe: "包含",
		},
	}

	for _, relationType := range relationTypes {
		exists, err := i.relationTypeDAO.Exists(ctx, relationType.UID)
		if err != nil {
			return fmt.Errorf("检查关系类型是否存在失败: %w", err)
		}
		if exists {
			i.logger.Info("关系类型已存在，跳过", elog.String("uid", relationType.UID), elog.String("name", relationType.Name))
			continue
		}

		if _, err := i.relationTypeDAO.Create(ctx, relationType); err != nil {
			return fmt.Errorf("创建关系类型失败 %s: %w", relationType.Name, err)
		}
		i.logger.Info("创建关系类型成功", elog.String("uid", relationType.UID), elog.String("name", relationType.Name))
	}

	return nil
}

// initModelRelations 初始化模型关系
func (i *ModelInitializer) initModelRelations(ctx context.Context) error {
	i.logger.Info("初始化模型关系")

	relations := []domain.ModelRelation{
		{
			SourceModelUID:  "cloud_ecs",
			TargetModelUID:  "cloud_lb",
			RelationTypeUID: "connect_to",
			RelationName:    "云主机-连接到-负载均衡",
			Mapping:         domain.MappingManyToMany,
		},
		{
			SourceModelUID:  "cloud_cdn",
			TargetModelUID:  "cloud_ecs",
			RelationTypeUID: "use",
			RelationName:    "CDN-使用-云主机",
			Mapping:         domain.MappingOneToMany,
		},
		{
			SourceModelUID:  "cloud_waf",
			TargetModelUID:  "cloud_cdn",
			RelationTypeUID: "protect_by",
			RelationName:    "WAF-防护-CDN",
			Mapping:         domain.MappingOneToMany,
		},
		{
			SourceModelUID:  "cloud_lb",
			TargetModelUID:  "cloud_ecs",
			RelationTypeUID: "connect_to",
			RelationName:    "负载均衡-连接到-云主机",
			Mapping:         domain.MappingOneToMany,
		},
	}

	for _, relation := range relations {
		exists, err := i.modelRelationDAO.Exists(ctx, relation.SourceModelUID, relation.TargetModelUID, relation.RelationTypeUID)
		if err != nil {
			return fmt.Errorf("检查模型关系是否存在失败: %w", err)
		}
		if exists {
			i.logger.Info("模型关系已存在，跳过", elog.String("relation", relation.RelationName))
			continue
		}

		if _, err := i.modelRelationDAO.Create(ctx, relation); err != nil {
			return fmt.Errorf("创建模型关系失败 %s: %w", relation.RelationName, err)
		}
		i.logger.Info("创建模型关系成功", elog.String("relation", relation.RelationName))
	}

	return nil
}
