package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/gotomicro/ego/core/elog"
)

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
