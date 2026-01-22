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

// initECSModel 初始化云主机模型
func (i *ModelInitializer) initECSModel(ctx context.Context) error {
	modelUID := "cloud_ecs"

	// 检查模型是否已存在
	exists, err := i.modelRepo.ModelExists(ctx, modelUID)
	if err != nil {
		return err
	}
	if exists {
		i.logger.Info("云主机模型已存在，跳过初始化", elog.String("model_uid", modelUID))
		return nil
	}

	i.logger.Info("创建云主机模型", elog.String("model_uid", modelUID))

	// 创建模型
	model := domain.Model{
		UID:          modelUID,
		Name:         "云主机",
		ModelGroupID: 1, // 计算资源
		ParentUID:    "",
		Category:     "compute",
		Level:        1,
		Icon:         "server",
		Description:  "云服务器实例（ECS/EC2/VM）",
		Provider:     "all",
		Extensible:   true,
	}

	modelID, err := i.modelRepo.CreateModel(ctx, model)
	if err != nil {
		return fmt.Errorf("创建模型失败: %w", err)
	}

	// 创建字段分组
	groups := []domain.ModelFieldGroup{
		{
			ModelUID: modelUID,
			Name:     "基本信息",
			Index:    1,
		},
		{
			ModelUID: modelUID,
			Name:     "配置信息",
			Index:    2,
		},
		{
			ModelUID: modelUID,
			Name:     "网络信息",
			Index:    3,
		},
		{
			ModelUID: modelUID,
			Name:     "计费信息",
			Index:    4,
		},
	}

	groupIDs := make(map[string]int64)
	for _, group := range groups {
		groupID, err := i.groupRepo.CreateGroup(ctx, group)
		if err != nil {
			return fmt.Errorf("创建字段分组失败: %w", err)
		}
		groupIDs[group.Name] = groupID
	}

	// 创建字段
	fields := []domain.ModelField{
		// 基本信息
		{
			FieldUID:    "ecs_instance_id",
			FieldName:   "instance_id",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["基本信息"],
			DisplayName: "实例ID",
			Display:     true,
			Index:       1,
			Required:    true,
			Secure:      false,
		},
		{
			FieldUID:    "ecs_instance_name",
			FieldName:   "instance_name",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["基本信息"],
			DisplayName: "实例名称",
			Display:     true,
			Index:       2,
			Required:    true,
			Secure:      false,
		},
		{
			FieldUID:    "ecs_status",
			FieldName:   "status",
			FieldType:   domain.FieldTypeEnum,
			ModelUID:    modelUID,
			GroupID:     groupIDs["基本信息"],
			DisplayName: "实例状态",
			Display:     true,
			Index:       3,
			Required:    true,
			Secure:      false,
			Option:      `{"values":["running","stopped","starting","stopping"]}`,
		},
		{
			FieldUID:    "ecs_region",
			FieldName:   "region",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["基本信息"],
			DisplayName: "地域",
			Display:     true,
			Index:       4,
			Required:    true,
			Secure:      false,
		},
		{
			FieldUID:    "ecs_zone",
			FieldName:   "zone",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["基本信息"],
			DisplayName: "可用区",
			Display:     true,
			Index:       5,
			Required:    false,
			Secure:      false,
		},
		// 配置信息
		{
			FieldUID:    "ecs_instance_type",
			FieldName:   "instance_type",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["配置信息"],
			DisplayName: "实例规格",
			Display:     true,
			Index:       1,
			Required:    false,
			Secure:      false,
		},
		{
			FieldUID:    "ecs_cpu",
			FieldName:   "cpu",
			FieldType:   domain.FieldTypeInt,
			ModelUID:    modelUID,
			GroupID:     groupIDs["配置信息"],
			DisplayName: "CPU核数",
			Display:     true,
			Index:       2,
			Required:    false,
			Secure:      false,
		},
		{
			FieldUID:    "ecs_memory",
			FieldName:   "memory",
			FieldType:   domain.FieldTypeInt,
			ModelUID:    modelUID,
			GroupID:     groupIDs["配置信息"],
			DisplayName: "内存大小(GB)",
			Display:     true,
			Index:       3,
			Required:    false,
			Secure:      false,
		},
		{
			FieldUID:    "ecs_os_type",
			FieldName:   "os_type",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["配置信息"],
			DisplayName: "操作系统类型",
			Display:     true,
			Index:       4,
			Required:    false,
			Secure:      false,
		},
		{
			FieldUID:    "ecs_os_name",
			FieldName:   "os_name",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["配置信息"],
			DisplayName: "操作系统名称",
			Display:     true,
			Index:       5,
			Required:    false,
			Secure:      false,
		},
		// 网络信息
		{
			FieldUID:    "ecs_public_ip",
			FieldName:   "public_ip",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["网络信息"],
			DisplayName: "公网IP",
			Display:     true,
			Index:       1,
			Required:    false,
			Secure:      false,
		},
		{
			FieldUID:    "ecs_private_ip",
			FieldName:   "private_ip",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["网络信息"],
			DisplayName: "私网IP",
			Display:     true,
			Index:       2,
			Required:    false,
			Secure:      false,
		},
		{
			FieldUID:    "ecs_vpc_id",
			FieldName:   "vpc_id",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["网络信息"],
			DisplayName: "VPC ID",
			Display:     true,
			Index:       3,
			Required:    false,
			Secure:      false,
		},
		// 计费信息
		{
			FieldUID:    "ecs_charge_type",
			FieldName:   "charge_type",
			FieldType:   domain.FieldTypeEnum,
			ModelUID:    modelUID,
			GroupID:     groupIDs["计费信息"],
			DisplayName: "计费方式",
			Display:     true,
			Index:       1,
			Required:    false,
			Secure:      false,
			Option:      `{"values":["PostPaid","PrePaid"]}`,
		},
		{
			FieldUID:    "ecs_expired_time",
			FieldName:   "expired_time",
			FieldType:   domain.FieldTypeDateTime,
			ModelUID:    modelUID,
			GroupID:     groupIDs["计费信息"],
			DisplayName: "到期时间",
			Display:     true,
			Index:       2,
			Required:    false,
			Secure:      false,
		},
		{
			FieldUID:    "ecs_creation_time",
			FieldName:   "creation_time",
			FieldType:   domain.FieldTypeDateTime,
			ModelUID:    modelUID,
			GroupID:     groupIDs["计费信息"],
			DisplayName: "创建时间",
			Display:     true,
			Index:       3,
			Required:    false,
			Secure:      false,
		},
	}

	for _, field := range fields {
		if _, err := i.fieldRepo.CreateField(ctx, field); err != nil {
			return fmt.Errorf("创建字段失败 %s: %w", field.FieldUID, err)
		}
	}

	i.logger.Info("云主机模型创建成功",
		elog.Int64("model_id", modelID),
		elog.Int("field_count", len(fields)),
		elog.Int("group_count", len(groups)))

	return nil
}

// initCDNModel 初始化CDN模型
func (i *ModelInitializer) initCDNModel(ctx context.Context) error {
	modelUID := "cloud_cdn"

	exists, err := i.modelRepo.ModelExists(ctx, modelUID)
	if err != nil {
		return err
	}
	if exists {
		i.logger.Info("CDN模型已存在，跳过初始化", elog.String("model_uid", modelUID))
		return nil
	}

	i.logger.Info("创建CDN模型", elog.String("model_uid", modelUID))

	model := domain.Model{
		UID:          modelUID,
		Name:         "CDN",
		ModelGroupID: 2, // 网络资源
		ParentUID:    "",
		Category:     "network",
		Level:        1,
		Icon:         "cdn",
		Description:  "内容分发网络（CDN）",
		Provider:     "all",
		Extensible:   true,
	}

	modelID, err := i.modelRepo.CreateModel(ctx, model)
	if err != nil {
		return fmt.Errorf("创建模型失败: %w", err)
	}

	groups := []domain.ModelFieldGroup{
		{ModelUID: modelUID, Name: "基本信息", Index: 1},
		{ModelUID: modelUID, Name: "配置信息", Index: 2},
		{ModelUID: modelUID, Name: "域名信息", Index: 3},
	}

	groupIDs := make(map[string]int64)
	for _, group := range groups {
		groupID, err := i.groupRepo.CreateGroup(ctx, group)
		if err != nil {
			return fmt.Errorf("创建字段分组失败: %w", err)
		}
		groupIDs[group.Name] = groupID
	}

	fields := []domain.ModelField{
		{
			FieldUID:    "cdn_domain_id",
			FieldName:   "domain_id",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["基本信息"],
			DisplayName: "域名ID",
			Display:     true,
			Index:       1,
			Required:    true,
		},
		{
			FieldUID:    "cdn_domain_name",
			FieldName:   "domain_name",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["基本信息"],
			DisplayName: "加速域名",
			Display:     true,
			Index:       2,
			Required:    true,
		},
		{
			FieldUID:    "cdn_status",
			FieldName:   "status",
			FieldType:   domain.FieldTypeEnum,
			ModelUID:    modelUID,
			GroupID:     groupIDs["基本信息"],
			DisplayName: "状态",
			Display:     true,
			Index:       3,
			Required:    true,
			Option:      `{"values":["online","offline","configuring"]}`,
		},
		{
			FieldUID:    "cdn_cname",
			FieldName:   "cname",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["域名信息"],
			DisplayName: "CNAME",
			Display:     true,
			Index:       1,
			Required:    false,
		},
		{
			FieldUID:    "cdn_origin_type",
			FieldName:   "origin_type",
			FieldType:   domain.FieldTypeEnum,
			ModelUID:    modelUID,
			GroupID:     groupIDs["配置信息"],
			DisplayName: "源站类型",
			Display:     true,
			Index:       1,
			Required:    false,
			Option:      `{"values":["ipaddr","domain","oss"]}`,
		},
		{
			FieldUID:    "cdn_origin_address",
			FieldName:   "origin_address",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["配置信息"],
			DisplayName: "源站地址",
			Display:     true,
			Index:       2,
			Required:    false,
		},
	}

	for _, field := range fields {
		if _, err := i.fieldRepo.CreateField(ctx, field); err != nil {
			return fmt.Errorf("创建字段失败 %s: %w", field.FieldUID, err)
		}
	}

	i.logger.Info("CDN模型创建成功",
		elog.Int64("model_id", modelID),
		elog.Int("field_count", len(fields)),
		elog.Int("group_count", len(groups)))

	return nil
}

// initWAFModel 初始化WAF模型
func (i *ModelInitializer) initWAFModel(ctx context.Context) error {
	modelUID := "cloud_waf"

	exists, err := i.modelRepo.ModelExists(ctx, modelUID)
	if err != nil {
		return err
	}
	if exists {
		i.logger.Info("WAF模型已存在，跳过初始化", elog.String("model_uid", modelUID))
		return nil
	}

	i.logger.Info("创建WAF模型", elog.String("model_uid", modelUID))

	model := domain.Model{
		UID:          modelUID,
		Name:         "WAF",
		ModelGroupID: 4, // 安全资源
		ParentUID:    "",
		Category:     "security",
		Level:        1,
		Icon:         "shield",
		Description:  "Web应用防火墙（WAF）",
		Provider:     "all",
		Extensible:   true,
	}

	modelID, err := i.modelRepo.CreateModel(ctx, model)
	if err != nil {
		return fmt.Errorf("创建模型失败: %w", err)
	}

	groups := []domain.ModelFieldGroup{
		{ModelUID: modelUID, Name: "基本信息", Index: 1},
		{ModelUID: modelUID, Name: "防护配置", Index: 2},
		{ModelUID: modelUID, Name: "域名信息", Index: 3},
	}

	groupIDs := make(map[string]int64)
	for _, group := range groups {
		groupID, err := i.groupRepo.CreateGroup(ctx, group)
		if err != nil {
			return fmt.Errorf("创建字段分组失败: %w", err)
		}
		groupIDs[group.Name] = groupID
	}

	fields := []domain.ModelField{
		{
			FieldUID:    "waf_instance_id",
			FieldName:   "instance_id",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["基本信息"],
			DisplayName: "实例ID",
			Display:     true,
			Index:       1,
			Required:    true,
		},
		{
			FieldUID:    "waf_instance_name",
			FieldName:   "instance_name",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["基本信息"],
			DisplayName: "实例名称",
			Display:     true,
			Index:       2,
			Required:    true,
		},
		{
			FieldUID:    "waf_status",
			FieldName:   "status",
			FieldType:   domain.FieldTypeEnum,
			ModelUID:    modelUID,
			GroupID:     groupIDs["基本信息"],
			DisplayName: "状态",
			Display:     true,
			Index:       3,
			Required:    true,
			Option:      `{"values":["active","inactive","configuring"]}`,
		},
		{
			FieldUID:    "waf_region",
			FieldName:   "region",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["基本信息"],
			DisplayName: "地域",
			Display:     true,
			Index:       4,
			Required:    true,
		},
		{
			FieldUID:    "waf_domain",
			FieldName:   "domain",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["域名信息"],
			DisplayName: "防护域名",
			Display:     true,
			Index:       1,
			Required:    false,
		},
		{
			FieldUID:    "waf_protection_mode",
			FieldName:   "protection_mode",
			FieldType:   domain.FieldTypeEnum,
			ModelUID:    modelUID,
			GroupID:     groupIDs["防护配置"],
			DisplayName: "防护模式",
			Display:     true,
			Index:       1,
			Required:    false,
			Option:      `{"values":["block","monitor","off"]}`,
		},
		{
			FieldUID:    "waf_rule_count",
			FieldName:   "rule_count",
			FieldType:   domain.FieldTypeInt,
			ModelUID:    modelUID,
			GroupID:     groupIDs["防护配置"],
			DisplayName: "规则数量",
			Display:     true,
			Index:       2,
			Required:    false,
		},
	}

	for _, field := range fields {
		if _, err := i.fieldRepo.CreateField(ctx, field); err != nil {
			return fmt.Errorf("创建字段失败 %s: %w", field.FieldUID, err)
		}
	}

	i.logger.Info("WAF模型创建成功",
		elog.Int64("model_id", modelID),
		elog.Int("field_count", len(fields)),
		elog.Int("group_count", len(groups)))

	return nil
}

// initLBModel 初始化负载均衡模型
func (i *ModelInitializer) initLBModel(ctx context.Context) error {
	modelUID := "cloud_lb"

	exists, err := i.modelRepo.ModelExists(ctx, modelUID)
	if err != nil {
		return err
	}
	if exists {
		i.logger.Info("负载均衡模型已存在，跳过初始化", elog.String("model_uid", modelUID))
		return nil
	}

	i.logger.Info("创建负载均衡模型", elog.String("model_uid", modelUID))

	model := domain.Model{
		UID:          modelUID,
		Name:         "负载均衡",
		ModelGroupID: 2, // 网络资源
		ParentUID:    "",
		Category:     "network",
		Level:        1,
		Icon:         "loadbalancer",
		Description:  "负载均衡（SLB/ALB/ELB）",
		Provider:     "all",
		Extensible:   true,
	}

	modelID, err := i.modelRepo.CreateModel(ctx, model)
	if err != nil {
		return fmt.Errorf("创建模型失败: %w", err)
	}

	groups := []domain.ModelFieldGroup{
		{ModelUID: modelUID, Name: "基本信息", Index: 1},
		{ModelUID: modelUID, Name: "网络配置", Index: 2},
		{ModelUID: modelUID, Name: "监听器配置", Index: 3},
	}

	groupIDs := make(map[string]int64)
	for _, group := range groups {
		groupID, err := i.groupRepo.CreateGroup(ctx, group)
		if err != nil {
			return fmt.Errorf("创建字段分组失败: %w", err)
		}
		groupIDs[group.Name] = groupID
	}

	fields := []domain.ModelField{
		{
			FieldUID:    "lb_instance_id",
			FieldName:   "instance_id",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["基本信息"],
			DisplayName: "实例ID",
			Display:     true,
			Index:       1,
			Required:    true,
		},
		{
			FieldUID:    "lb_instance_name",
			FieldName:   "instance_name",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["基本信息"],
			DisplayName: "实例名称",
			Display:     true,
			Index:       2,
			Required:    true,
		},
		{
			FieldUID:    "lb_status",
			FieldName:   "status",
			FieldType:   domain.FieldTypeEnum,
			ModelUID:    modelUID,
			GroupID:     groupIDs["基本信息"],
			DisplayName: "状态",
			Display:     true,
			Index:       3,
			Required:    true,
			Option:      `{"values":["active","inactive","locked"]}`,
		},
		{
			FieldUID:    "lb_region",
			FieldName:   "region",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["基本信息"],
			DisplayName: "地域",
			Display:     true,
			Index:       4,
			Required:    true,
		},
		{
			FieldUID:    "lb_type",
			FieldName:   "lb_type",
			FieldType:   domain.FieldTypeEnum,
			ModelUID:    modelUID,
			GroupID:     groupIDs["基本信息"],
			DisplayName: "负载均衡类型",
			Display:     true,
			Index:       5,
			Required:    false,
			Option:      `{"values":["application","network","classic"]}`,
		},
		{
			FieldUID:    "lb_address",
			FieldName:   "address",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["网络配置"],
			DisplayName: "服务地址",
			Display:     true,
			Index:       1,
			Required:    false,
		},
		{
			FieldUID:    "lb_vpc_id",
			FieldName:   "vpc_id",
			FieldType:   domain.FieldTypeString,
			ModelUID:    modelUID,
			GroupID:     groupIDs["网络配置"],
			DisplayName: "VPC ID",
			Display:     true,
			Index:       2,
			Required:    false,
		},
		{
			FieldUID:    "lb_network_type",
			FieldName:   "network_type",
			FieldType:   domain.FieldTypeEnum,
			ModelUID:    modelUID,
			GroupID:     groupIDs["网络配置"],
			DisplayName: "网络类型",
			Display:     true,
			Index:       3,
			Required:    false,
			Option:      `{"values":["internet","intranet"]}`,
		},
		{
			FieldUID:    "lb_listener_count",
			FieldName:   "listener_count",
			FieldType:   domain.FieldTypeInt,
			ModelUID:    modelUID,
			GroupID:     groupIDs["监听器配置"],
			DisplayName: "监听器数量",
			Display:     true,
			Index:       1,
			Required:    false,
		},
		{
			FieldUID:    "lb_backend_count",
			FieldName:   "backend_count",
			FieldType:   domain.FieldTypeInt,
			ModelUID:    modelUID,
			GroupID:     groupIDs["监听器配置"],
			DisplayName: "后端服务器数量",
			Display:     true,
			Index:       2,
			Required:    false,
		},
	}

	for _, field := range fields {
		if _, err := i.fieldRepo.CreateField(ctx, field); err != nil {
			return fmt.Errorf("创建字段失败 %s: %w", field.FieldUID, err)
		}
	}

	i.logger.Info("负载均衡模型创建成功",
		elog.Int64("model_id", modelID),
		elog.Int("field_count", len(fields)),
		elog.Int("group_count", len(groups)))

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
