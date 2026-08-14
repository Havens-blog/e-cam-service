package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/gotomicro/ego/core/elog"
)

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
