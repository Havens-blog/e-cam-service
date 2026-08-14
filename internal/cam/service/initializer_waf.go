package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/gotomicro/ego/core/elog"
)

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
