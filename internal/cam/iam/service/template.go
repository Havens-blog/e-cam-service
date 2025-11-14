package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	iamrepo "github.com/Havens-blog/e-cam-service/internal/cam/iam/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
	"go.mongodb.org/mongo-driver/mongo"
)

type PolicyTemplateService interface {
	CreateTemplate(ctx context.Context, req *domain.CreateTemplateRequest) (*domain.PolicyTemplate, error)
	GetTemplate(ctx context.Context, id int64) (*domain.PolicyTemplate, error)
	ListTemplates(ctx context.Context, filter domain.TemplateFilter) ([]*domain.PolicyTemplate, int64, error)
	UpdateTemplate(ctx context.Context, id int64, req *domain.UpdateTemplateRequest) error
	DeleteTemplate(ctx context.Context, id int64) error
	CreateFromGroup(ctx context.Context, groupID int64, name, description string) (*domain.PolicyTemplate, error)
}

type policyTemplateService struct {
	templateRepo iamrepo.PolicyTemplateRepository
	groupRepo    iamrepo.PermissionGroupRepository
	logger       *elog.Component
}

func NewPolicyTemplateService(
	templateRepo iamrepo.PolicyTemplateRepository,
	groupRepo iamrepo.PermissionGroupRepository,
	logger *elog.Component,
) PolicyTemplateService {
	return &policyTemplateService{
		templateRepo: templateRepo,
		groupRepo:    groupRepo,
		logger:       logger,
	}
}

func (s *policyTemplateService) CreateTemplate(ctx context.Context, req *domain.CreateTemplateRequest) (*domain.PolicyTemplate, error) {
	s.logger.Info("创建策略模板",
		elog.String("name", req.Name),
		elog.String("category", string(req.Category)))

	if err := s.validateCreateTemplateRequest(req); err != nil {
		s.logger.Error("创建模板参数验证失败", elog.FieldErr(err))
		return nil, err
	}

	existing, err := s.templateRepo.GetByName(ctx, req.Name, req.TenantID)
	if err == nil && existing.ID > 0 {
		return nil, errs.TemplateNotFound
	}

	now := time.Now()
	template := domain.PolicyTemplate{
		Name:           req.Name,
		Description:    req.Description,
		Category:       req.Category,
		Policies:       req.Policies,
		CloudPlatforms: req.CloudPlatforms,
		IsBuiltIn:      false,
		TenantID:       req.TenantID,
		CreateTime:     now,
		UpdateTime:     now,
		CTime:          now.Unix(),
		UTime:          now.Unix(),
	}

	if err := template.Validate(); err != nil {
		s.logger.Error("模板数据验证失败", elog.FieldErr(err))
		return nil, errs.ParamsError
	}

	id, err := s.templateRepo.Create(ctx, template)
	if err != nil {
		s.logger.Error("创建模板失败", elog.FieldErr(err))
		return nil, fmt.Errorf("创建模板失败: %w", err)
	}

	template.ID = id

	s.logger.Info("创建策略模板成功",
		elog.Int64("template_id", id),
		elog.String("name", req.Name))

	return &template, nil
}

func (s *policyTemplateService) GetTemplate(ctx context.Context, id int64) (*domain.PolicyTemplate, error) {
	s.logger.Debug("获取策略模板详情", elog.Int64("template_id", id))

	template, err := s.templateRepo.GetByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.TemplateNotFound
		}
		s.logger.Error("获取模板失败", elog.Int64("template_id", id), elog.FieldErr(err))
		return nil, fmt.Errorf("获取模板失败: %w", err)
	}

	return &template, nil
}

func (s *policyTemplateService) ListTemplates(ctx context.Context, filter domain.TemplateFilter) ([]*domain.PolicyTemplate, int64, error) {
	s.logger.Debug("获取策略模板列表",
		elog.String("category", string(filter.Category)),
		elog.String("tenant_id", filter.TenantID))

	if filter.Limit == 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}

	templates, total, err := s.templateRepo.List(ctx, filter)
	if err != nil {
		s.logger.Error("获取模板列表失败", elog.FieldErr(err))
		return nil, 0, fmt.Errorf("获取模板列表失败: %w", err)
	}

	templatePtrs := make([]*domain.PolicyTemplate, len(templates))
	for i := range templates {
		templatePtrs[i] = &templates[i]
	}

	s.logger.Debug("获取策略模板列表成功",
		elog.Int64("total", total),
		elog.Int("count", len(templates)))

	return templatePtrs, total, nil
}

func (s *policyTemplateService) UpdateTemplate(ctx context.Context, id int64, req *domain.UpdateTemplateRequest) error {
	s.logger.Info("更新策略模板", elog.Int64("template_id", id))

	template, err := s.templateRepo.GetByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.TemplateNotFound
		}
		s.logger.Error("获取模板失败", elog.FieldErr(err))
		return fmt.Errorf("获取模板失败: %w", err)
	}

	if !template.IsEditable() {
		return errs.TemplateBuiltIn
	}

	updated := false
	if req.Name != nil && *req.Name != template.Name {
		template.Name = *req.Name
		updated = true
	}
	if req.Description != nil && *req.Description != template.Description {
		template.Description = *req.Description
		updated = true
	}
	if req.Policies != nil {
		template.Policies = req.Policies
		updated = true
	}
	if req.CloudPlatforms != nil {
		template.CloudPlatforms = req.CloudPlatforms
		updated = true
	}

	if !updated {
		s.logger.Debug("模板信息无变更", elog.Int64("template_id", id))
		return nil
	}

	now := time.Now()
	template.UpdateTime = now
	template.UTime = now.Unix()

	if err := s.templateRepo.Update(ctx, template); err != nil {
		s.logger.Error("更新模板失败", elog.FieldErr(err))
		return fmt.Errorf("更新模板失败: %w", err)
	}

	s.logger.Info("更新策略模板成功", elog.Int64("template_id", id))

	return nil
}

func (s *policyTemplateService) DeleteTemplate(ctx context.Context, id int64) error {
	s.logger.Info("删除策略模板", elog.Int64("template_id", id))

	template, err := s.templateRepo.GetByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.TemplateNotFound
		}
		s.logger.Error("获取模板失败", elog.FieldErr(err))
		return fmt.Errorf("获取模板失败: %w", err)
	}

	if !template.IsEditable() {
		return errs.TemplateBuiltIn
	}

	if err := s.templateRepo.Delete(ctx, id); err != nil {
		s.logger.Error("删除模板失败", elog.FieldErr(err))
		return fmt.Errorf("删除模板失败: %w", err)
	}

	s.logger.Info("删除策略模板成功", elog.Int64("template_id", id))

	return nil
}

func (s *policyTemplateService) CreateFromGroup(ctx context.Context, groupID int64, name, description string) (*domain.PolicyTemplate, error) {
	s.logger.Info("从权限组创建模板",
		elog.Int64("group_id", groupID),
		elog.String("name", name))

	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.PermissionGroupNotFound
		}
		s.logger.Error("获取权限组失败", elog.FieldErr(err))
		return nil, fmt.Errorf("获取权限组失败: %w", err)
	}

	if name == "" {
		return nil, fmt.Errorf("模板名称不能为空")
	}

	existing, err := s.templateRepo.GetByName(ctx, name, group.TenantID)
	if err == nil && existing.ID > 0 {
		return nil, fmt.Errorf("模板名称已存在")
	}

	now := time.Now()
	template := domain.PolicyTemplate{
		Name:           name,
		Description:    description,
		Category:       domain.TemplateCategoryCustom,
		Policies:       group.Policies,
		CloudPlatforms: group.CloudPlatforms,
		IsBuiltIn:      false,
		TenantID:       group.TenantID,
		CreateTime:     now,
		UpdateTime:     now,
		CTime:          now.Unix(),
		UTime:          now.Unix(),
	}

	if err := template.Validate(); err != nil {
		s.logger.Error("模板数据验证失败", elog.FieldErr(err))
		return nil, errs.ParamsError
	}

	id, err := s.templateRepo.Create(ctx, template)
	if err != nil {
		s.logger.Error("创建模板失败", elog.FieldErr(err))
		return nil, fmt.Errorf("创建模板失败: %w", err)
	}

	template.ID = id

	s.logger.Info("从权限组创建模板成功",
		elog.Int64("template_id", id),
		elog.Int64("group_id", groupID),
		elog.String("name", name))

	return &template, nil
}

func (s *policyTemplateService) validateCreateTemplateRequest(req *domain.CreateTemplateRequest) error {
	if req.Name == "" {
		return fmt.Errorf("模板名称不能为空")
	}
	if req.Category == "" {
		return fmt.Errorf("模板分类不能为空")
	}
	if len(req.CloudPlatforms) == 0 {
		return fmt.Errorf("云平台列表不能为空")
	}
	if req.TenantID == "" {
		return fmt.Errorf("租户ID不能为空")
	}

	validCategories := map[domain.TemplateCategory]bool{
		domain.TemplateCategoryReadOnly:  true,
		domain.TemplateCategoryAdmin:     true,
		domain.TemplateCategoryDeveloper: true,
		domain.TemplateCategoryCustom:    true,
	}
	if !validCategories[req.Category] {
		return fmt.Errorf("无效的模板分类")
	}

	return nil
}

func (s *policyTemplateService) InitBuiltInTemplates(ctx context.Context) error {
	s.logger.Info("初始化内置策略模板")

	templates := s.getBuiltInTemplates()

	for _, template := range templates {
		existing, err := s.templateRepo.GetByName(ctx, template.Name, "")
		if err == nil && existing.ID > 0 {
			s.logger.Debug("内置模板已存在，跳过",
				elog.String("name", template.Name))
			continue
		}

		id, err := s.templateRepo.Create(ctx, template)
		if err != nil {
			s.logger.Error("创建内置模板失败",
				elog.String("name", template.Name),
				elog.FieldErr(err))
			continue
		}

		s.logger.Info("创建内置模板成功",
			elog.Int64("template_id", id),
			elog.String("name", template.Name))
	}

	s.logger.Info("初始化内置策略模板完成")

	return nil
}

func (s *policyTemplateService) getBuiltInTemplates() []domain.PolicyTemplate {
	now := time.Now()

	return []domain.PolicyTemplate{
		{
			Name:        "只读权限模板",
			Description: "提供只读访问权限，适用于查看资源但不能修改",
			Category:    domain.TemplateCategoryReadOnly,
			Policies: []domain.PermissionPolicy{
				{
					PolicyID:   "ReadOnlyAccess",
					PolicyName: "只读访问",
					PolicyDocument: `{
						"Version": "1",
						"Statement": [{
							"Effect": "Allow",
							"Action": ["*:Get*", "*:List*", "*:Describe*"],
							"Resource": "*"
						}]
					}`,
					Provider:   domain.CloudProviderAliyun,
					PolicyType: domain.PolicyTypeSystem,
				},
			},
			CloudPlatforms: []domain.CloudProvider{
				domain.CloudProviderAliyun,
				domain.CloudProviderAWS,
				domain.CloudProviderHuawei,
				domain.CloudProviderTencent,
			},
			IsBuiltIn:  true,
			TenantID:   "",
			CreateTime: now,
			UpdateTime: now,
			CTime:      now.Unix(),
			UTime:      now.Unix(),
		},
		{
			Name:        "管理员权限模板",
			Description: "提供完全管理权限，可以执行所有操作",
			Category:    domain.TemplateCategoryAdmin,
			Policies: []domain.PermissionPolicy{
				{
					PolicyID:   "AdministratorAccess",
					PolicyName: "管理员访问",
					PolicyDocument: `{
						"Version": "1",
						"Statement": [{
							"Effect": "Allow",
							"Action": "*",
							"Resource": "*"
						}]
					}`,
					Provider:   domain.CloudProviderAliyun,
					PolicyType: domain.PolicyTypeSystem,
				},
			},
			CloudPlatforms: []domain.CloudProvider{
				domain.CloudProviderAliyun,
				domain.CloudProviderAWS,
				domain.CloudProviderHuawei,
				domain.CloudProviderTencent,
			},
			IsBuiltIn:  true,
			TenantID:   "",
			CreateTime: now,
			UpdateTime: now,
			CTime:      now.Unix(),
			UTime:      now.Unix(),
		},
		{
			Name:        "开发者权限模板",
			Description: "提供开发所需的常用权限，包括计算、存储、网络等资源的管理",
			Category:    domain.TemplateCategoryDeveloper,
			Policies: []domain.PermissionPolicy{
				{
					PolicyID:   "DeveloperAccess",
					PolicyName: "开发者访问",
					PolicyDocument: `{
						"Version": "1",
						"Statement": [
							{
								"Effect": "Allow",
								"Action": [
									"ecs:*",
									"vpc:*",
									"slb:*",
									"rds:*",
									"oss:*"
								],
								"Resource": "*"
							}
						]
					}`,
					Provider:   domain.CloudProviderAliyun,
					PolicyType: domain.PolicyTypeSystem,
				},
			},
			CloudPlatforms: []domain.CloudProvider{
				domain.CloudProviderAliyun,
				domain.CloudProviderAWS,
				domain.CloudProviderHuawei,
				domain.CloudProviderTencent,
			},
			IsBuiltIn:  true,
			TenantID:   "",
			CreateTime: now,
			UpdateTime: now,
			CTime:      now.Unix(),
			UTime:      now.Unix(),
		},
	}
}
