package repository

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
)

// AssetRepository 资产仓储接口
type AssetRepository interface {
	CreateAsset(ctx context.Context, asset domain.CloudAsset) (int64, error)
	CreateMultiAssets(ctx context.Context, assets []domain.CloudAsset) (int64, error)
	UpdateAsset(ctx context.Context, asset domain.CloudAsset) error
	GetAssetById(ctx context.Context, id int64) (domain.CloudAsset, error)
	GetAssetByAssetId(ctx context.Context, assetId string) (domain.CloudAsset, error)
	ListAssets(ctx context.Context, filter domain.AssetFilter) ([]domain.CloudAsset, error)
	CountAssets(ctx context.Context, filter domain.AssetFilter) (int64, error)
	DeleteAsset(ctx context.Context, id int64) error
}

type assetRepository struct {
	dao dao.AssetDAO
}

// NewAssetRepository 创建资产仓储
func NewAssetRepository(dao dao.AssetDAO) AssetRepository {
	return &assetRepository{
		dao: dao,
	}
}

// CreateAsset 创建单个资产
func (repo *assetRepository) CreateAsset(ctx context.Context, asset domain.CloudAsset) (int64, error) {
	daoAsset := repo.toEntity(asset)
	return repo.dao.CreateAsset(ctx, daoAsset)
}

// CreateMultiAssets 批量创建资产
func (repo *assetRepository) CreateMultiAssets(ctx context.Context, assets []domain.CloudAsset) (int64, error) {
	daoAssets := make([]dao.Asset, len(assets))
	for i, asset := range assets {
		daoAssets[i] = repo.toEntity(asset)
	}
	return repo.dao.CreateMultiAssets(ctx, daoAssets)
}

// UpdateAsset 更新资产
func (repo *assetRepository) UpdateAsset(ctx context.Context, asset domain.CloudAsset) error {
	daoAsset := repo.toEntity(asset)
	return repo.dao.UpdateAsset(ctx, daoAsset)
}

// GetAssetById 根据ID获取资产
func (repo *assetRepository) GetAssetById(ctx context.Context, id int64) (domain.CloudAsset, error) {
	daoAsset, err := repo.dao.GetAssetById(ctx, id)
	if err != nil {
		return domain.CloudAsset{}, err
	}
	return repo.toDomain(daoAsset), nil
}

// GetAssetByAssetId 根据资产ID获取资产
func (repo *assetRepository) GetAssetByAssetId(ctx context.Context, assetId string) (domain.CloudAsset, error) {
	daoAsset, err := repo.dao.GetAssetByAssetId(ctx, assetId)
	if err != nil {
		return domain.CloudAsset{}, err
	}
	return repo.toDomain(daoAsset), nil
}

// ListAssets 获取资产列表
func (repo *assetRepository) ListAssets(ctx context.Context, filter domain.AssetFilter) ([]domain.CloudAsset, error) {
	daoFilter := dao.AssetFilter{
		Provider:  filter.Provider,
		AssetType: filter.AssetType,
		Region:    filter.Region,
		Status:    filter.Status,
		AssetName: filter.AssetName,
		Offset:    filter.Offset,
		Limit:     filter.Limit,
	}

	daoAssets, err := repo.dao.ListAssets(ctx, daoFilter)
	if err != nil {
		return nil, err
	}

	assets := make([]domain.CloudAsset, len(daoAssets))
	for i, daoAsset := range daoAssets {
		assets[i] = repo.toDomain(daoAsset)
	}

	return assets, nil
}

// CountAssets 统计资产数量
func (repo *assetRepository) CountAssets(ctx context.Context, filter domain.AssetFilter) (int64, error) {
	daoFilter := dao.AssetFilter{
		Provider:  filter.Provider,
		AssetType: filter.AssetType,
		Region:    filter.Region,
		Status:    filter.Status,
		AssetName: filter.AssetName,
	}

	return repo.dao.CountAssets(ctx, daoFilter)
}

// DeleteAsset 删除资产
func (repo *assetRepository) DeleteAsset(ctx context.Context, id int64) error {
	return repo.dao.DeleteAsset(ctx, id)
}

// toDomain 转换为领域模型
func (repo *assetRepository) toDomain(daoAsset dao.Asset) domain.CloudAsset {
	tags := make([]domain.Tag, len(daoAsset.Tags))
	for i, tag := range daoAsset.Tags {
		tags[i] = domain.Tag{
			Key:   tag.Key,
			Value: tag.Value,
		}
	}

	return domain.CloudAsset{
		Id:           daoAsset.Id,
		AssetId:      daoAsset.AssetId,
		AssetName:    daoAsset.AssetName,
		AssetType:    daoAsset.AssetType,
		Provider:     daoAsset.Provider,
		Region:       daoAsset.Region,
		Zone:         daoAsset.Zone,
		Status:       daoAsset.Status,
		Tags:         tags,
		Metadata:     daoAsset.Metadata,
		Cost:         daoAsset.Cost,
		CreateTime:   daoAsset.CreateTime,
		UpdateTime:   daoAsset.UpdateTime,
		DiscoverTime: daoAsset.DiscoverTime,
	}
}

// toEntity 转换为DAO实体
func (repo *assetRepository) toEntity(asset domain.CloudAsset) dao.Asset {
	tags := make([]dao.Tag, len(asset.Tags))
	for i, tag := range asset.Tags {
		tags[i] = dao.Tag{
			Key:   tag.Key,
			Value: tag.Value,
		}
	}

	return dao.Asset{
		Id:           asset.Id,
		AssetId:      asset.AssetId,
		AssetName:    asset.AssetName,
		AssetType:    asset.AssetType,
		Provider:     asset.Provider,
		Region:       asset.Region,
		Zone:         asset.Zone,
		Status:       asset.Status,
		Tags:         tags,
		Metadata:     asset.Metadata,
		Cost:         asset.Cost,
		CreateTime:   asset.CreateTime,
		UpdateTime:   asset.UpdateTime,
		DiscoverTime: asset.DiscoverTime,
	}
}
