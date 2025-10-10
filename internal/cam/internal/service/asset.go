package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/internal/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/internal/repository"
)

// Service CAM服务接口
type Service interface {
	// 资产管理
	CreateAsset(ctx context.Context, asset domain.CloudAsset) (int64, error)
	CreateMultiAssets(ctx context.Context, assets []domain.CloudAsset) (int64, error)
	UpdateAsset(ctx context.Context, asset domain.CloudAsset) error
	GetAssetById(ctx context.Context, id int64) (domain.CloudAsset, error)
	GetAssetByAssetId(ctx context.Context, assetId string) (domain.CloudAsset, error)
	ListAssets(ctx context.Context, filter domain.AssetFilter) ([]domain.CloudAsset, int64, error)
	DeleteAsset(ctx context.Context, id int64) error

	// 资产发现
	DiscoverAssets(ctx context.Context, provider, region string) ([]domain.CloudAsset, error)
	SyncAssets(ctx context.Context, provider string) error

	// 统计分析
	GetAssetStatistics(ctx context.Context) (AssetStatistics, error)
	GetCostAnalysis(ctx context.Context, provider string, days int) (CostAnalysis, error)
}

// AssetStatistics 资产统计信息
type AssetStatistics struct {
	TotalAssets      int64            `json:"total_assets"`
	ProviderStats    map[string]int64 `json:"provider_stats"`
	AssetTypeStats   map[string]int64 `json:"asset_type_stats"`
	RegionStats      map[string]int64 `json:"region_stats"`
	StatusStats      map[string]int64 `json:"status_stats"`
	TotalCost        float64          `json:"total_cost"`
	LastDiscoverTime time.Time        `json:"last_discover_time"`
}

// CostAnalysis 成本分析
type CostAnalysis struct {
	Provider    string             `json:"provider"`
	TotalCost   float64            `json:"total_cost"`
	DailyCosts  []DailyCost        `json:"daily_costs"`
	AssetCosts  []AssetCost        `json:"asset_costs"`
	RegionCosts map[string]float64 `json:"region_costs"`
}

// DailyCost 每日成本
type DailyCost struct {
	Date string  `json:"date"`
	Cost float64 `json:"cost"`
}

// AssetCost 资产成本
type AssetCost struct {
	AssetId   string  `json:"asset_id"`
	AssetName string  `json:"asset_name"`
	AssetType string  `json:"asset_type"`
	Cost      float64 `json:"cost"`
}

type service struct {
	repo repository.AssetRepository
}

// NewService 创建CAM服务
func NewService(repo repository.AssetRepository) Service {
	return &service{
		repo: repo,
	}
}

// CreateAsset 创建单个资产
func (s *service) CreateAsset(ctx context.Context, asset domain.CloudAsset) (int64, error) {
	// 检查资产是否已存在
	if asset.AssetId != "" {
		_, err := s.repo.GetAssetByAssetId(ctx, asset.AssetId)
		if err == nil {
			return 0, fmt.Errorf("asset with id %s already exists", asset.AssetId)
		}
	}

	// 设置时间戳
	now := time.Now()
	if asset.CreateTime.IsZero() {
		asset.CreateTime = now
	}
	if asset.UpdateTime.IsZero() {
		asset.UpdateTime = now
	}
	if asset.DiscoverTime.IsZero() {
		asset.DiscoverTime = now
	}

	return s.repo.CreateAsset(ctx, asset)
}

// CreateMultiAssets 批量创建资产
func (s *service) CreateMultiAssets(ctx context.Context, assets []domain.CloudAsset) (int64, error) {
	if len(assets) == 0 {
		return 0, nil
	}

	now := time.Now()
	for i := range assets {
		if assets[i].CreateTime.IsZero() {
			assets[i].CreateTime = now
		}
		if assets[i].UpdateTime.IsZero() {
			assets[i].UpdateTime = now
		}
		if assets[i].DiscoverTime.IsZero() {
			assets[i].DiscoverTime = now
		}
	}

	return s.repo.CreateMultiAssets(ctx, assets)
}

// UpdateAsset 更新资产
func (s *service) UpdateAsset(ctx context.Context, asset domain.CloudAsset) error {
	// 检查资产是否存在
	_, err := s.repo.GetAssetById(ctx, asset.Id)
	if err != nil {
		return fmt.Errorf("asset not found: %w", err)
	}

	// 更新时间戳
	asset.UpdateTime = time.Now()

	return s.repo.UpdateAsset(ctx, asset)
}

// GetAssetById 根据ID获取资产
func (s *service) GetAssetById(ctx context.Context, id int64) (domain.CloudAsset, error) {
	return s.repo.GetAssetById(ctx, id)
}

// GetAssetByAssetId 根据资产ID获取资产
func (s *service) GetAssetByAssetId(ctx context.Context, assetId string) (domain.CloudAsset, error) {
	return s.repo.GetAssetByAssetId(ctx, assetId)
}

// ListAssets 获取资产列表
func (s *service) ListAssets(ctx context.Context, filter domain.AssetFilter) ([]domain.CloudAsset, int64, error) {
	// 设置默认分页参数
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}

	assets, err := s.repo.ListAssets(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	total, err := s.repo.CountAssets(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	return assets, total, nil
}

// DeleteAsset 删除资产
func (s *service) DeleteAsset(ctx context.Context, id int64) error {
	// 检查资产是否存在
	_, err := s.repo.GetAssetById(ctx, id)
	if err != nil {
		return fmt.Errorf("asset not found: %w", err)
	}

	return s.repo.DeleteAsset(ctx, id)
}

// DiscoverAssets 发现资产 (暂时返回空实现，后续扩展)
func (s *service) DiscoverAssets(ctx context.Context, provider, region string) ([]domain.CloudAsset, error) {
	// TODO: 实现云厂商资产发现逻辑
	return []domain.CloudAsset{}, nil
}

// SyncAssets 同步资产 (暂时返回空实现，后续扩展)
func (s *service) SyncAssets(ctx context.Context, provider string) error {
	// TODO: 实现资产同步逻辑
	return nil
}

// GetAssetStatistics 获取资产统计信息
func (s *service) GetAssetStatistics(ctx context.Context) (AssetStatistics, error) {
	// 获取总数
	total, err := s.repo.CountAssets(ctx, domain.AssetFilter{})
	if err != nil {
		return AssetStatistics{}, err
	}

	// TODO: 实现详细统计逻辑
	stats := AssetStatistics{
		TotalAssets:      total,
		ProviderStats:    make(map[string]int64),
		AssetTypeStats:   make(map[string]int64),
		RegionStats:      make(map[string]int64),
		StatusStats:      make(map[string]int64),
		TotalCost:        0,
		LastDiscoverTime: time.Now(),
	}

	return stats, nil
}

// GetCostAnalysis 获取成本分析
func (s *service) GetCostAnalysis(ctx context.Context, provider string, days int) (CostAnalysis, error) {
	// TODO: 实现成本分析逻辑
	analysis := CostAnalysis{
		Provider:    provider,
		TotalCost:   0,
		DailyCosts:  []DailyCost{},
		AssetCosts:  []AssetCost{},
		RegionCosts: make(map[string]float64),
	}

	return analysis, nil
}
