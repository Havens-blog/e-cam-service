package repository

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/iam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// PolicyTemplateRepository 策略模板仓储接口
type PolicyTemplateRepository interface {
	// Create 创建策略模板
	Create(ctx context.Context, template domain.PolicyTemplate) (int64, error)

	// GetByID 根据ID获取策略模板
	GetByID(ctx context.Context, id int64) (domain.PolicyTemplate, error)

	// GetByName 根据名称和租户ID获取策略模板
	GetByName(ctx context.Context, name, tenantID string) (domain.PolicyTemplate, error)

	// List 获取策略模板列表
	List(ctx context.Context, filter domain.TemplateFilter) ([]domain.PolicyTemplate, int64, error)

	// Update 更新策略模板
	Update(ctx context.Context, template domain.PolicyTemplate) error

	// Delete 删除策略模板
	Delete(ctx context.Context, id int64) error

	// ListBuiltInTemplates 获取所有内置模板
	ListBuiltInTemplates(ctx context.Context) ([]domain.PolicyTemplate, error)
}

type policyTemplateRepository struct {
	dao dao.PolicyTemplateDAO
}

// NewPolicyTemplateRepository 创建策略模板仓储
func NewPolicyTemplateRepository(dao dao.PolicyTemplateDAO) PolicyTemplateRepository {
	return &policyTemplateRepository{
		dao: dao,
	}
}

// Create 创建策略模板
func (repo *policyTemplateRepository) Create(ctx context.Context, template domain.PolicyTemplate) (int64, error) {
	daoTemplate := repo.toEntity(template)
	return repo.dao.Create(ctx, daoTemplate)
}

// GetByID 根据ID获取策略模板
func (repo *policyTemplateRepository) GetByID(ctx context.Context, id int64) (domain.PolicyTemplate, error) {
	daoTemplate, err := repo.dao.GetByID(ctx, id)
	if err != nil {
		return domain.PolicyTemplate{}, err
	}
	return repo.toDomain(daoTemplate), nil
}

// GetByName 根据名称和租户ID获取策略模板
func (repo *policyTemplateRepository) GetByName(ctx context.Context, name, tenantID string) (domain.PolicyTemplate, error) {
	daoTemplate, err := repo.dao.GetByName(ctx, name, tenantID)
	if err != nil {
		return domain.PolicyTemplate{}, err
	}
	return repo.toDomain(daoTemplate), nil
}

// List 获取策略模板列表
func (repo *policyTemplateRepository) List(ctx context.Context, filter domain.TemplateFilter) ([]domain.PolicyTemplate, int64, error) {
	daoFilter := dao.TemplateFilter{
		Category:  dao.TemplateCategory(filter.Category),
		IsBuiltIn: filter.IsBuiltIn,
		TenantID:  filter.TenantID,
		Keyword:   filter.Keyword,
		Offset:    filter.Offset,
		Limit:     filter.Limit,
	}

	daoTemplates, err := repo.dao.List(ctx, daoFilter)
	if err != nil {
		return nil, 0, err
	}

	count, err := repo.dao.Count(ctx, daoFilter)
	if err != nil {
		return nil, 0, err
	}

	templates := make([]domain.PolicyTemplate, len(daoTemplates))
	for i, daoTemplate := range daoTemplates {
		templates[i] = repo.toDomain(daoTemplate)
	}

	return templates, count, nil
}

// Update 更新策略模板
func (repo *policyTemplateRepository) Update(ctx context.Context, template domain.PolicyTemplate) error {
	daoTemplate := repo.toEntity(template)
	return repo.dao.Update(ctx, daoTemplate)
}

// Delete 删除策略模板
func (repo *policyTemplateRepository) Delete(ctx context.Context, id int64) error {
	return repo.dao.Delete(ctx, id)
}

// ListBuiltInTemplates 获取所有内置模板
func (repo *policyTemplateRepository) ListBuiltInTemplates(ctx context.Context) ([]domain.PolicyTemplate, error) {
	daoTemplates, err := repo.dao.ListBuiltInTemplates(ctx)
	if err != nil {
		return nil, err
	}

	templates := make([]domain.PolicyTemplate, len(daoTemplates))
	for i, daoTemplate := range daoTemplates {
		templates[i] = repo.toDomain(daoTemplate)
	}

	return templates, nil
}

// toDomain 转换为领域模型
func (repo *policyTemplateRepository) toDomain(daoTemplate dao.PolicyTemplate) domain.PolicyTemplate {
	policies := make([]domain.PermissionPolicy, len(daoTemplate.Policies))
	for i, policy := range daoTemplate.Policies {
		policies[i] = domain.PermissionPolicy{
			PolicyID:       policy.PolicyID,
			PolicyName:     policy.PolicyName,
			PolicyDocument: policy.PolicyDocument,
			Provider:       domain.CloudProvider(policy.Provider),
			PolicyType:     domain.PolicyType(policy.PolicyType),
		}
	}

	cloudPlatforms := make([]domain.CloudProvider, len(daoTemplate.CloudPlatforms))
	for i, platform := range daoTemplate.CloudPlatforms {
		cloudPlatforms[i] = domain.CloudProvider(platform)
	}

	return domain.PolicyTemplate{
		ID:             daoTemplate.ID,
		Name:           daoTemplate.Name,
		Description:    daoTemplate.Description,
		Category:       domain.TemplateCategory(daoTemplate.Category),
		Policies:       policies,
		CloudPlatforms: cloudPlatforms,
		IsBuiltIn:      daoTemplate.IsBuiltIn,
		TenantID:       daoTemplate.TenantID,
		CreateTime:     daoTemplate.CreateTime,
		UpdateTime:     daoTemplate.UpdateTime,
		CTime:          daoTemplate.CTime,
		UTime:          daoTemplate.UTime,
	}
}

// toEntity 转换为DAO实体
func (repo *policyTemplateRepository) toEntity(template domain.PolicyTemplate) dao.PolicyTemplate {
	policies := make([]dao.PermissionPolicy, len(template.Policies))
	for i, policy := range template.Policies {
		policies[i] = dao.PermissionPolicy{
			PolicyID:       policy.PolicyID,
			PolicyName:     policy.PolicyName,
			PolicyDocument: policy.PolicyDocument,
			Provider:       dao.CloudProvider(policy.Provider),
			PolicyType:     dao.PolicyType(policy.PolicyType),
		}
	}

	cloudPlatforms := make([]dao.CloudProvider, len(template.CloudPlatforms))
	for i, platform := range template.CloudPlatforms {
		cloudPlatforms[i] = dao.CloudProvider(platform)
	}

	return dao.PolicyTemplate{
		ID:             template.ID,
		Name:           template.Name,
		Description:    template.Description,
		Category:       dao.TemplateCategory(template.Category),
		Policies:       policies,
		CloudPlatforms: cloudPlatforms,
		IsBuiltIn:      template.IsBuiltIn,
		TenantID:       template.TenantID,
		CreateTime:     template.CreateTime,
		UpdateTime:     template.UpdateTime,
		CTime:          template.CTime,
		UTime:          template.UTime,
	}
}
