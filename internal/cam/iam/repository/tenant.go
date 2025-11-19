package repository

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/cam/iam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// TenantRepository tenant repository interface
type TenantRepository interface {
	// Create creates tenant
	Create(ctx context.Context, tenant domain.Tenant) error

	// GetByID gets tenant by ID
	GetByID(ctx context.Context, tenantID string) (domain.Tenant, error)

	// GetByName gets tenant by name
	GetByName(ctx context.Context, name string) (domain.Tenant, error)

	// List gets tenant list
	List(ctx context.Context, filter domain.TenantFilter) ([]domain.Tenant, int64, error)

	// Update updates tenant
	Update(ctx context.Context, tenant domain.Tenant) error

	// Delete deletes tenant
	Delete(ctx context.Context, tenantID string) error
}

type tenantRepository struct {
	dao dao.TenantDAO
}

// NewTenantRepository creates tenant repository
func NewTenantRepository(dao dao.TenantDAO) TenantRepository {
	return &tenantRepository{
		dao: dao,
	}
}

// Create creates tenant
func (repo *tenantRepository) Create(ctx context.Context, tenant domain.Tenant) error {
	daoTenant := repo.toEntity(tenant)
	return repo.dao.Create(ctx, daoTenant)
}

// GetByID gets tenant by ID
func (repo *tenantRepository) GetByID(ctx context.Context, tenantID string) (domain.Tenant, error) {
	daoTenant, err := repo.dao.GetByID(ctx, tenantID)
	if err != nil {
		return domain.Tenant{}, err
	}
	return repo.toDomain(daoTenant), nil
}

// GetByName gets tenant by name
func (repo *tenantRepository) GetByName(ctx context.Context, name string) (domain.Tenant, error) {
	daoTenant, err := repo.dao.GetByName(ctx, name)
	if err != nil {
		return domain.Tenant{}, err
	}
	return repo.toDomain(daoTenant), nil
}

// List gets tenant list
func (repo *tenantRepository) List(ctx context.Context, filter domain.TenantFilter) ([]domain.Tenant, int64, error) {
	daoFilter := dao.TenantFilter{
		Keyword:  filter.Keyword,
		Status:   string(filter.Status),
		Industry: filter.Industry,
		Region:   filter.Region,
		Offset:   filter.Offset,
		Limit:    filter.Limit,
	}

	daoTenants, err := repo.dao.List(ctx, daoFilter)
	if err != nil {
		return nil, 0, err
	}

	count, err := repo.dao.Count(ctx, daoFilter)
	if err != nil {
		return nil, 0, err
	}

	tenants := make([]domain.Tenant, len(daoTenants))
	for i, daoTenant := range daoTenants {
		tenants[i] = repo.toDomain(daoTenant)
	}

	return tenants, count, nil
}

// Update updates tenant
func (repo *tenantRepository) Update(ctx context.Context, tenant domain.Tenant) error {
	daoTenant := repo.toEntity(tenant)
	return repo.dao.Update(ctx, daoTenant)
}

// Delete deletes tenant
func (repo *tenantRepository) Delete(ctx context.Context, tenantID string) error {
	return repo.dao.Delete(ctx, tenantID)
}

// toDomain converts DAO entity to domain model
func (repo *tenantRepository) toDomain(daoTenant dao.Tenant) domain.Tenant {
	return domain.Tenant{
		ID:          daoTenant.ID,
		Name:        daoTenant.Name,
		DisplayName: daoTenant.DisplayName,
		Description: daoTenant.Description,
		Status:      domain.TenantStatus(daoTenant.Status),
		Settings: domain.TenantSettings{
			MaxCloudAccounts: daoTenant.Settings.MaxCloudAccounts,
			MaxUsers:         daoTenant.Settings.MaxUsers,
			MaxUserGroups:    daoTenant.Settings.MaxUserGroups,
			AllowedProviders: convertToCloudProviders(daoTenant.Settings.AllowedProviders),
			Features:         daoTenant.Settings.Features,
			CustomFields:     daoTenant.Settings.CustomFields,
		},
		Metadata: domain.TenantMetadata{
			Owner:        daoTenant.Metadata.Owner,
			ContactEmail: daoTenant.Metadata.ContactEmail,
			ContactPhone: daoTenant.Metadata.ContactPhone,
			CompanyName:  daoTenant.Metadata.CompanyName,
			Industry:     daoTenant.Metadata.Industry,
			Region:       daoTenant.Metadata.Region,
			Tags:         daoTenant.Metadata.Tags,
		},
		CreateTime: daoTenant.CreateTime,
		UpdateTime: daoTenant.UpdateTime,
		CTime:      daoTenant.CTime,
		UTime:      daoTenant.UTime,
	}
}

// toEntity converts domain model to DAO entity
func (repo *tenantRepository) toEntity(tenant domain.Tenant) dao.Tenant {
	return dao.Tenant{
		ID:          tenant.ID,
		Name:        tenant.Name,
		DisplayName: tenant.DisplayName,
		Description: tenant.Description,
		Status:      string(tenant.Status),
		Settings: dao.TenantSettings{
			MaxCloudAccounts: tenant.Settings.MaxCloudAccounts,
			MaxUsers:         tenant.Settings.MaxUsers,
			MaxUserGroups:    tenant.Settings.MaxUserGroups,
			AllowedProviders: convertFromCloudProviders(tenant.Settings.AllowedProviders),
			Features:         tenant.Settings.Features,
			CustomFields:     tenant.Settings.CustomFields,
		},
		Metadata: dao.TenantMetadata{
			Owner:        tenant.Metadata.Owner,
			ContactEmail: tenant.Metadata.ContactEmail,
			ContactPhone: tenant.Metadata.ContactPhone,
			CompanyName:  tenant.Metadata.CompanyName,
			Industry:     tenant.Metadata.Industry,
			Region:       tenant.Metadata.Region,
			Tags:         tenant.Metadata.Tags,
		},
		CreateTime: tenant.CreateTime,
		UpdateTime: tenant.UpdateTime,
		CTime:      tenant.CTime,
		UTime:      tenant.UTime,
	}
}

// convertToCloudProviders converts string slice to CloudProvider slice
func convertToCloudProviders(providers []string) []domain.CloudProvider {
	result := make([]domain.CloudProvider, len(providers))
	for i, p := range providers {
		result[i] = domain.CloudProvider(p)
	}
	return result
}

// convertFromCloudProviders converts CloudProvider slice to string slice
func convertFromCloudProviders(providers []domain.CloudProvider) []string {
	result := make([]string, len(providers))
	for i, p := range providers {
		result[i] = string(p)
	}
	return result
}
