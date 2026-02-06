// Package repository 云账号仓储层
package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/account/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/crypto"
)

// CloudAccountRepository 云账号仓储接口
type CloudAccountRepository interface {
	Create(ctx context.Context, account domain.CloudAccount) (int64, error)
	GetByID(ctx context.Context, id int64) (domain.CloudAccount, error)
	GetByName(ctx context.Context, name, tenantID string) (domain.CloudAccount, error)
	List(ctx context.Context, filter domain.CloudAccountFilter) ([]domain.CloudAccount, int64, error)
	Update(ctx context.Context, account domain.CloudAccount) error
	Delete(ctx context.Context, id int64) error
	UpdateStatus(ctx context.Context, id int64, status domain.CloudAccountStatus) error
	UpdateSyncTime(ctx context.Context, id int64, syncTime time.Time, assetCount int64) error
	UpdateTestTime(ctx context.Context, id int64, testTime time.Time, status domain.CloudAccountStatus, errorMsg string) error
}

type cloudAccountRepository struct {
	dao dao.CloudAccountDAO
}

// NewCloudAccountRepository 创建云账号仓储
func NewCloudAccountRepository(dao dao.CloudAccountDAO) CloudAccountRepository {
	return &cloudAccountRepository{dao: dao}
}

func (repo *cloudAccountRepository) Create(ctx context.Context, account domain.CloudAccount) (int64, error) {
	return repo.dao.CreateAccount(ctx, repo.toEntity(account))
}

func (repo *cloudAccountRepository) GetByID(ctx context.Context, id int64) (domain.CloudAccount, error) {
	daoAccount, err := repo.dao.GetAccountById(ctx, id)
	if err != nil {
		return domain.CloudAccount{}, err
	}
	return repo.toDomain(daoAccount), nil
}

func (repo *cloudAccountRepository) GetByName(ctx context.Context, name, tenantID string) (domain.CloudAccount, error) {
	daoAccount, err := repo.dao.GetAccountByName(ctx, name, tenantID)
	if err != nil {
		return domain.CloudAccount{}, err
	}
	return repo.toDomain(daoAccount), nil
}

func (repo *cloudAccountRepository) List(ctx context.Context, filter domain.CloudAccountFilter) ([]domain.CloudAccount, int64, error) {
	daoFilter := dao.CloudAccountFilter{
		Provider:    filter.Provider,
		Environment: filter.Environment,
		Status:      filter.Status,
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

func (repo *cloudAccountRepository) Update(ctx context.Context, account domain.CloudAccount) error {
	return repo.dao.UpdateAccount(ctx, repo.toEntity(account))
}

func (repo *cloudAccountRepository) Delete(ctx context.Context, id int64) error {
	return repo.dao.DeleteAccount(ctx, id)
}

func (repo *cloudAccountRepository) UpdateStatus(ctx context.Context, id int64, status domain.CloudAccountStatus) error {
	return repo.dao.UpdateAccountStatus(ctx, id, status)
}

func (repo *cloudAccountRepository) UpdateSyncTime(ctx context.Context, id int64, syncTime time.Time, assetCount int64) error {
	return repo.dao.UpdateSyncTime(ctx, id, syncTime, assetCount)
}

func (repo *cloudAccountRepository) UpdateTestTime(ctx context.Context, id int64, testTime time.Time, status domain.CloudAccountStatus, errorMsg string) error {
	return repo.dao.UpdateTestTime(ctx, id, testTime, status, errorMsg)
}

func (repo *cloudAccountRepository) toDomain(daoAccount dao.CloudAccount) domain.CloudAccount {
	decryptedSecret := daoAccount.AccessKeySecret
	if daoAccount.AccessKeySecret != "" {
		if decrypted, err := crypto.DecryptSecret(daoAccount.AccessKeySecret); err == nil {
			decryptedSecret = decrypted
		}
	}

	return domain.CloudAccount{
		ID:              daoAccount.ID,
		Name:            daoAccount.Name,
		Provider:        daoAccount.Provider,
		Environment:     daoAccount.Environment,
		AccessKeyID:     daoAccount.AccessKeyID,
		AccessKeySecret: decryptedSecret,
		Regions:         daoAccount.Regions,
		Description:     daoAccount.Description,
		Status:          daoAccount.Status,
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

func (repo *cloudAccountRepository) toEntity(account domain.CloudAccount) dao.CloudAccount {
	encryptedSecret := account.AccessKeySecret
	if account.AccessKeySecret != "" {
		if encrypted, err := crypto.EncryptSecret(account.AccessKeySecret); err == nil {
			encryptedSecret = encrypted
		}
	}

	return dao.CloudAccount{
		ID:              account.ID,
		Name:            account.Name,
		Provider:        account.Provider,
		Environment:     account.Environment,
		AccessKeyID:     account.AccessKeyID,
		AccessKeySecret: encryptedSecret,
		Regions:         account.Regions,
		Description:     account.Description,
		Status:          account.Status,
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
