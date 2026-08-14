package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/gotomicro/ego/core/elog"
)

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
