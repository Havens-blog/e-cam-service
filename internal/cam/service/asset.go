package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	syncdomain "github.com/Havens-blog/e-cam-service/internal/cam/sync/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/sync/service/adapters"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
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
	DiscoverAssets(ctx context.Context, provider, region string, assetTypes []string) ([]domain.CloudAsset, error)
	SyncAssets(ctx context.Context, provider string, assetTypes []string) error

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
	repo           repository.AssetRepository
	accountRepo    repository.CloudAccountRepository
	adapterFactory *adapters.AdapterFactory
	logger         *elog.Component
}

// NewService 创建CAM服务
func NewService(
	repo repository.AssetRepository,
	accountRepo repository.CloudAccountRepository,
	adapterFactory *adapters.AdapterFactory,
	logger *elog.Component,
) Service {
	return &service{
		repo:           repo,
		accountRepo:    accountRepo,
		adapterFactory: adapterFactory,
		logger:         logger,
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

// DiscoverAssets 发现资产（不保存到数据库）
// assetTypes: 要发现的资源类型列表，为空则发现所有支持的类型
func (s *service) DiscoverAssets(ctx context.Context, provider, region string, assetTypes []string) ([]domain.CloudAsset, error) {
	// 如果未指定资源类型，默认发现所有支持的类型
	if len(assetTypes) == 0 {
		assetTypes = []string{"ecs"} // 默认只同步 ECS，后续可扩展
	}

	s.logger.Info("开始发现云资产",
		elog.String("provider", provider),
		elog.String("region", region),
		elog.Any("asset_types", assetTypes))

	// 获取该云厂商的第一个可用账号
	filter := shareddomain.CloudAccountFilter{
		Provider: shareddomain.CloudProvider(provider),
		Status:   shareddomain.CloudAccountStatusActive,
		Limit:    1,
	}

	accounts, _, err := s.accountRepo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("获取云账号失败: %w", err)
	}

	if len(accounts) == 0 {
		return nil, fmt.Errorf("未找到可用的%s云账号", provider)
	}

	account := accounts[0]

	// 获取默认区域（使用第一个区域）
	defaultRegion := ""
	if len(account.Regions) > 0 {
		defaultRegion = account.Regions[0]
	}

	// 转换为同步域的账号格式
	syncAccount := &syncdomain.CloudAccount{
		ID:              account.ID,
		Name:            account.Name,
		Provider:        syncdomain.CloudProvider(account.Provider),
		AccessKeyID:     account.AccessKeyID,
		AccessKeySecret: account.AccessKeySecret,
		DefaultRegion:   defaultRegion,
		Enabled:         account.Status == shareddomain.CloudAccountStatusActive,
	}

	// 创建适配器
	adapter, err := s.adapterFactory.CreateAdapter(syncAccount)
	if err != nil {
		return nil, fmt.Errorf("创建适配器失败: %w", err)
	}

	// 根据资源类型发现资产
	var allAssets []domain.CloudAsset

	for _, assetType := range assetTypes {
		switch assetType {
		case "ecs":
			// 获取 ECS 实例
			instances, err := adapter.GetECSInstances(ctx, region)
			if err != nil {
				s.logger.Error("获取ECS实例失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}

			// 转换为资产格式
			for _, inst := range instances {
				asset, err := s.convertECSToAsset(inst)
				if err != nil {
					s.logger.Warn("转换ECS实例失败",
						elog.String("instance_id", inst.InstanceID),
						elog.FieldErr(err))
					continue
				}
				allAssets = append(allAssets, asset)
			}

		// TODO: 添加其他资源类型的支持
		// case "rds":
		// case "oss":
		// case "slb":
		default:
			s.logger.Warn("不支持的资源类型",
				elog.String("asset_type", assetType))
		}
	}

	s.logger.Info("云资产发现完成",
		elog.String("provider", provider),
		elog.String("region", region),
		elog.Any("asset_types", assetTypes),
		elog.Int("count", len(allAssets)))

	return allAssets, nil
}

// SyncAssets 同步资产到数据库
// assetTypes: 要同步的资源类型列表，为空则同步所有支持的类型
func (s *service) SyncAssets(ctx context.Context, provider string, assetTypes []string) error {
	// 如果未指定资源类型，默认同步所有支持的类型
	if len(assetTypes) == 0 {
		assetTypes = []string{"ecs"} // 默认只同步 ECS，后续可扩展
	}

	s.logger.Info("开始同步云资产",
		elog.String("provider", provider),
		elog.Any("asset_types", assetTypes))

	// 获取该云厂商的所有可用账号
	filter := shareddomain.CloudAccountFilter{
		Provider: shareddomain.CloudProvider(provider),
		Status:   shareddomain.CloudAccountStatusActive,
		Limit:    100,
	}

	accounts, _, err := s.accountRepo.List(ctx, filter)
	if err != nil {
		return fmt.Errorf("获取云账号失败: %w", err)
	}

	if len(accounts) == 0 {
		return fmt.Errorf("未找到可用的%s云账号", provider)
	}

	// 同步每个账号的资产
	totalSynced := 0
	for i := range accounts {
		synced, err := s.syncAccountAssets(ctx, &accounts[i], assetTypes)
		if err != nil {
			s.logger.Error("同步账号资产失败",
				elog.String("account", accounts[i].Name),
				elog.FieldErr(err))
			continue
		}
		totalSynced += synced
	}

	s.logger.Info("云资产同步完成",
		elog.String("provider", provider),
		elog.Any("asset_types", assetTypes),
		elog.Int("total_synced", totalSynced))

	return nil
}

// syncAccountAssets 同步单个账号的资产
func (s *service) syncAccountAssets(ctx context.Context, account *shareddomain.CloudAccount, assetTypes []string) (int, error) {
	s.logger.Info("同步账号资产",
		elog.String("account", account.Name),
		elog.Any("asset_types", assetTypes))

	// 获取默认区域（使用第一个区域）
	defaultRegion := ""
	if len(account.Regions) > 0 {
		defaultRegion = account.Regions[0]
	}

	// 转换为同步域的账号格式
	syncAccount := &syncdomain.CloudAccount{
		ID:              account.ID,
		Name:            account.Name,
		Provider:        syncdomain.CloudProvider(account.Provider),
		AccessKeyID:     account.AccessKeyID,
		AccessKeySecret: account.AccessKeySecret,
		DefaultRegion:   defaultRegion,
		Enabled:         account.Status == shareddomain.CloudAccountStatusActive,
	}

	// 创建适配器
	adapter, err := s.adapterFactory.CreateAdapter(syncAccount)
	if err != nil {
		return 0, fmt.Errorf("创建适配器失败: %w", err)
	}

	// 获取所有地域
	regions, err := adapter.GetRegions(ctx)
	if err != nil {
		return 0, fmt.Errorf("获取地域列表失败: %w", err)
	}

	// 如果账号配置了支持的地域，则只同步这些地域
	supportedRegions := account.Config.SupportedRegions
	if len(supportedRegions) > 0 {
		regionMap := make(map[string]bool)
		for _, r := range supportedRegions {
			regionMap[r] = true
		}

		filteredRegions := make([]syncdomain.Region, 0)
		for _, r := range regions {
			if regionMap[r.ID] {
				filteredRegions = append(filteredRegions, r)
			}
		}
		regions = filteredRegions
	}

	// 同步每个地域的资产
	totalSynced := 0
	for _, region := range regions {
		synced, err := s.syncRegionAssets(ctx, adapter, account, region.ID, assetTypes)
		if err != nil {
			s.logger.Error("同步地域资产失败",
				elog.String("region", region.ID),
				elog.FieldErr(err))
			continue
		}
		totalSynced += synced
	}

	// 更新账号的最后同步时间
	now := time.Now()
	err = s.accountRepo.UpdateSyncTime(ctx, account.ID, now, int64(totalSynced))
	if err != nil {
		s.logger.Warn("更新账号同步时间失败", elog.FieldErr(err))
	}

	return totalSynced, nil
}

// syncRegionAssets 同步单个地域的资产
func (s *service) syncRegionAssets(
	ctx context.Context,
	adapter syncdomain.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
	assetTypes []string,
) (int, error) {
	s.logger.Info("同步地域资产",
		elog.String("account", account.Name),
		elog.String("region", region),
		elog.Any("asset_types", assetTypes))

	totalSynced := 0

	// 根据资源类型同步不同的资产
	for _, assetType := range assetTypes {
		switch assetType {
		case "ecs":
			synced, err := s.syncRegionECSInstances(ctx, adapter, account, region)
			if err != nil {
				s.logger.Error("同步地域ECS实例失败",
					elog.String("region", region),
					elog.FieldErr(err))
				continue
			}
			totalSynced += synced

		// TODO: 添加其他资源类型的支持
		// case "rds":
		//     synced, err := s.syncRegionRDSInstances(ctx, adapter, account, region)
		// case "oss":
		//     synced, err := s.syncRegionOSSBuckets(ctx, adapter, account, region)
		// case "slb":
		//     synced, err := s.syncRegionSLBInstances(ctx, adapter, account, region)

		default:
			s.logger.Warn("不支持的资源类型",
				elog.String("asset_type", assetType),
				elog.String("region", region))
		}
	}

	s.logger.Info("地域资产同步完成",
		elog.String("region", region),
		elog.Any("asset_types", assetTypes),
		elog.Int("synced", totalSynced))

	return totalSynced, nil
}

// syncRegionECSInstances 同步单个地域的 ECS 实例
func (s *service) syncRegionECSInstances(
	ctx context.Context,
	adapter syncdomain.CloudAdapter,
	account *shareddomain.CloudAccount,
	region string,
) (int, error) {
	s.logger.Debug("同步地域ECS实例",
		elog.String("account", account.Name),
		elog.String("region", region))

	// 获取 ECS 实例
	instances, err := adapter.GetECSInstances(ctx, region)
	if err != nil {
		return 0, fmt.Errorf("获取ECS实例失败: %w", err)
	}

	if len(instances) == 0 {
		s.logger.Debug("该地域没有ECS实例", elog.String("region", region))
		return 0, nil
	}

	// 转换为资产格式
	assets := make([]domain.CloudAsset, 0, len(instances))
	for _, inst := range instances {
		asset, err := s.convertECSToAsset(inst)
		if err != nil {
			s.logger.Warn("转换ECS实例失败",
				elog.String("instance_id", inst.InstanceID),
				elog.FieldErr(err))
			continue
		}
		assets = append(assets, asset)
	}

	// 批量保存或更新资产
	synced := 0
	for _, asset := range assets {
		// 检查资产是否已存在
		existing, err := s.repo.GetAssetByAssetId(ctx, asset.AssetId)
		if err != nil {
			// 资产不存在，创建新资产
			_, err = s.CreateAsset(ctx, asset)
			if err != nil {
				s.logger.Error("创建资产失败",
					elog.String("asset_id", asset.AssetId),
					elog.FieldErr(err))
				continue
			}
			synced++
		} else {
			// 资产已存在，更新资产
			asset.Id = existing.Id
			asset.CreateTime = existing.CreateTime
			err = s.UpdateAsset(ctx, asset)
			if err != nil {
				s.logger.Error("更新资产失败",
					elog.String("asset_id", asset.AssetId),
					elog.FieldErr(err))
				continue
			}
			synced++
		}
	}

	s.logger.Debug("地域ECS实例同步完成",
		elog.String("region", region),
		elog.Int("total", len(instances)),
		elog.Int("synced", synced))

	return synced, nil
}

// convertECSToAsset 将 ECS 实例转换为资产
func (s *service) convertECSToAsset(inst syncdomain.ECSInstance) (domain.CloudAsset, error) {
	// 转换标签
	tags := make([]domain.Tag, 0, len(inst.Tags))
	for k, v := range inst.Tags {
		tags = append(tags, domain.Tag{
			Key:   k,
			Value: v,
		})
	}

	// 将实例详细信息序列化为 JSON 作为元数据
	metadata, err := json.Marshal(inst)
	if err != nil {
		return domain.CloudAsset{}, fmt.Errorf("序列化元数据失败: %w", err)
	}

	// 解析创建时间
	createTime, _ := time.Parse("2006-01-02T15:04:05Z", inst.CreationTime)
	if createTime.IsZero() {
		createTime = time.Now()
	}

	return domain.CloudAsset{
		AssetId:      inst.InstanceID,
		AssetName:    inst.InstanceName,
		AssetType:    "ecs",
		Provider:     inst.Provider,
		Region:       inst.Region,
		Zone:         inst.Zone,
		Status:       inst.Status,
		Tags:         tags,
		Metadata:     string(metadata),
		Cost:         0, // TODO: 获取实际成本
		CreateTime:   createTime,
		UpdateTime:   time.Now(),
		DiscoverTime: time.Now(),
	}, nil
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
