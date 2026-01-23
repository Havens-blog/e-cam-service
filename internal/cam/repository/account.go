package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// CloudAccountRepository 云账号仓储接口
type CloudAccountRepository interface {
	// Create 创建云账号
	Create(ctx context.Context, account domain.CloudAccount) (int64, error)

	// GetByID 根据ID获取云账号
	GetByID(ctx context.Context, id int64) (domain.CloudAccount, error)

	// GetByName 根据名称和租户ID获取云账号
	GetByName(ctx context.Context, name, tenantID string) (domain.CloudAccount, error)

	// List 获取云账号列表
	List(ctx context.Context, filter domain.CloudAccountFilter) ([]domain.CloudAccount, int64, error)

	// Update 更新云账号
	Update(ctx context.Context, account domain.CloudAccount) error

	// Delete 删除云账号
	Delete(ctx context.Context, id int64) error

	// UpdateStatus 更新账号状态
	UpdateStatus(ctx context.Context, id int64, status domain.CloudAccountStatus) error

	// UpdateSyncTime 更新同步时间和资产数量
	UpdateSyncTime(ctx context.Context, id int64, syncTime time.Time, assetCount int64) error

	// UpdateTestTime 更新测试时间和状态
	UpdateTestTime(ctx context.Context, id int64, testTime time.Time, status domain.CloudAccountStatus, errorMsg string) error
}

type cloudAccountRepository struct {
	dao dao.CloudAccountDAO
}

// NewCloudAccountRepository 创建云账号仓储
func NewCloudAccountRepository(dao dao.CloudAccountDAO) CloudAccountRepository {
	return &cloudAccountRepository{
		dao: dao,
	}
}

// Create 创建云账号
func (repo *cloudAccountRepository) Create(ctx context.Context, account domain.CloudAccount) (int64, error) {
	daoAccount := repo.toEntity(account)
	return repo.dao.CreateAccount(ctx, daoAccount)
}

// GetByID 根据ID获取云账号
func (repo *cloudAccountRepository) GetByID(ctx context.Context, id int64) (domain.CloudAccount, error) {
	daoAccount, err := repo.dao.GetAccountById(ctx, id)
	if err != nil {
		return domain.CloudAccount{}, err
	}
	return repo.toDomain(daoAccount), nil
}

// GetByName 根据名称和租户ID获取云账号
func (repo *cloudAccountRepository) GetByName(ctx context.Context, name, tenantID string) (domain.CloudAccount, error) {
	daoAccount, err := repo.dao.GetAccountByName(ctx, name, tenantID)
	if err != nil {
		return domain.CloudAccount{}, err
	}
	return repo.toDomain(daoAccount), nil
}

// List 获取云账号列表
func (repo *cloudAccountRepository) List(ctx context.Context, filter domain.CloudAccountFilter) ([]domain.CloudAccount, int64, error) {
	daoFilter := dao.CloudAccountFilter{
		Provider:    dao.CloudProvider(filter.Provider),
		Environment: dao.Environment(filter.Environment),
		Status:      dao.CloudAccountStatus(filter.Status),
		TenantID:    filter.TenantID,
		Offset:      filter.Offset,
		Limit:       filter.Limit,
	}

	daoAccounts, err := repo.dao.ListAccounts(ctx, daoFilter)
	if err != nil {
		return nil, 0, err
	}

	count, err := repo.dao.CountAccounts(ctx, daoFilter)
	if err != nil {
		return nil, 0, err
	}

	accounts := make([]domain.CloudAccount, len(daoAccounts))
	for i, daoAccount := range daoAccounts {
		accounts[i] = repo.toDomain(daoAccount)
	}

	return accounts, count, nil
}

// Update 更新云账号
func (repo *cloudAccountRepository) Update(ctx context.Context, account domain.CloudAccount) error {
	daoAccount := repo.toEntity(account)
	return repo.dao.UpdateAccount(ctx, daoAccount)
}

// Delete 删除云账号
func (repo *cloudAccountRepository) Delete(ctx context.Context, id int64) error {
	return repo.dao.DeleteAccount(ctx, id)
}

// UpdateStatus 更新账号状态
func (repo *cloudAccountRepository) UpdateStatus(ctx context.Context, id int64, status domain.CloudAccountStatus) error {
	return repo.dao.UpdateAccountStatus(ctx, id, dao.CloudAccountStatus(status))
}

// UpdateSyncTime 更新同步时间和资产数量
func (repo *cloudAccountRepository) UpdateSyncTime(ctx context.Context, id int64, syncTime time.Time, assetCount int64) error {
	return repo.dao.UpdateSyncTime(ctx, id, syncTime, assetCount)
}

// UpdateTestTime 更新测试时间和状态
func (repo *cloudAccountRepository) UpdateTestTime(ctx context.Context, id int64, testTime time.Time, status domain.CloudAccountStatus, errorMsg string) error {
	return repo.dao.UpdateTestTime(ctx, id, testTime, dao.CloudAccountStatus(status), errorMsg)
}

// toDomain 转换为领域模型
func (repo *cloudAccountRepository) toDomain(daoAccount dao.CloudAccount) domain.CloudAccount {
	return domain.CloudAccount{
		ID:              daoAccount.ID,
		Name:            daoAccount.Name,
		Provider:        domain.CloudProvider(daoAccount.Provider),
		Environment:     domain.Environment(daoAccount.Environment),
		AccessKeyID:     daoAccount.AccessKeyID,
		AccessKeySecret: daoAccount.AccessKeySecret,
		Regions:         daoAccount.Regions,
		Description:     daoAccount.Description,
		Status:          domain.CloudAccountStatus(daoAccount.Status),
		Config: domain.CloudAccountConfig{
			EnableAutoSync:       daoAccount.Config.EnableAutoSync,
			SyncInterval:         daoAccount.Config.SyncInterval,
			ReadOnly:             daoAccount.Config.ReadOnly,
			ShowSubAccounts:      daoAccount.Config.ShowSubAccounts,
			EnableCostMonitoring: daoAccount.Config.EnableCostMonitoring,
			SupportedRegions:     daoAccount.Config.SupportedRegions,
			SupportedAssetTypes:  daoAccount.Config.SupportedAssetTypes,
		},
		TenantID:     daoAccount.TenantID,
		LastSyncTime: daoAccount.LastSyncTime,
		LastTestTime: daoAccount.LastTestTime,
		AssetCount:   daoAccount.AssetCount,
		ErrorMessage: daoAccount.ErrorMessage,
		CreateTime:   daoAccount.CreateTime,
		UpdateTime:   daoAccount.UpdateTime,
		CTime:        daoAccount.CTime,
		UTime:        daoAccount.UTime,
	}
}

// toEntity 转换为DAO实体
func (repo *cloudAccountRepository) toEntity(account domain.CloudAccount) dao.CloudAccount {
	return dao.CloudAccount{
		ID:              account.ID,
		Name:            account.Name,
		Provider:        dao.CloudProvider(account.Provider),
		Environment:     dao.Environment(account.Environment),
		AccessKeyID:     account.AccessKeyID,
		AccessKeySecret: account.AccessKeySecret,
		Regions:         account.Regions,
		Description:     account.Description,
		Status:          dao.CloudAccountStatus(account.Status),
		Config: dao.CloudAccountConfig{
			EnableAutoSync:       account.Config.EnableAutoSync,
			SyncInterval:         account.Config.SyncInterval,
			ReadOnly:             account.Config.ReadOnly,
			ShowSubAccounts:      account.Config.ShowSubAccounts,
			EnableCostMonitoring: account.Config.EnableCostMonitoring,
			SupportedRegions:     account.Config.SupportedRegions,
			SupportedAssetTypes:  account.Config.SupportedAssetTypes,
		},
		TenantID:     account.TenantID,
		LastSyncTime: account.LastSyncTime,
		LastTestTime: account.LastTestTime,
		AssetCount:   account.AssetCount,
		ErrorMessage: account.ErrorMessage,
		CreateTime:   account.CreateTime,
		UpdateTime:   account.UpdateTime,
		CTime:        account.CTime,
		UTime:        account.UTime,
	}
}
