package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cloudx-sdk/asset"
	shareddomain "github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/Havens-blog/e-cloudx-sdk/types"
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

	// 统计分析
	GetAssetStatistics(ctx context.Context, tenantID int64) (AssetStatistics, error)
	GetCostAnalysis(ctx context.Context, tenantID int64, provider string, days int) (CostAnalysis, error)
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
	dashboardSvc   DashboardService
	adapterFactory *asset.AdapterFactory
	logger         *elog.Component
}

// NewService 创建CAM服务
func NewService(
	repo repository.AssetRepository,
	accountRepo repository.CloudAccountRepository,
	dashboardSvc DashboardService,
	adapterFactory *asset.AdapterFactory,
	logger *elog.Component,
) Service {
	return &service{
		repo:           repo,
		accountRepo:    accountRepo,
		dashboardSvc:   dashboardSvc,
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

	// 创建适配器
	adapter, err := s.adapterFactory.CreateAdapterFromDomain(&account)
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
				cloudAsset, err := s.convertECSToAsset(inst)
				if err != nil {
					s.logger.Warn("转换ECS实例失败",
						elog.String("instance_id", inst.InstanceID),
						elog.FieldErr(err))
					continue
				}
				allAssets = append(allAssets, cloudAsset)
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

// convertECSToAsset 将 ECS 实例转换为资产格式（DiscoverAssets 使用，同步收敛 Phase 2 保留）
func (s *service) convertECSToAsset(inst types.ECSInstance) (domain.CloudAsset, error) {
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

func (s *service) GetAssetStatistics(ctx context.Context, tenantID int64) (AssetStatistics, error) {
	// 数据源：ecam_instance（真实资产 3.6w+，5 厂商），经 DashboardService 聚合，
	// 按 tenantID 过滤 + 排除附属资源（与 dashboard 口径一致）。
	// 不再用 ecam_cloud_asset 旧表（仅 760 aliyun 残留，口径错误）。
	overview, err := s.dashboardSvc.GetOverview(ctx, tenantID)
	if err != nil {
		s.logger.Warn("资产总览聚合失败", elog.FieldErr(err), elog.Int64("tenant_id", tenantID))
		return AssetStatistics{}, err
	}

	// region 分布：dashboard Overview 不含 region，单独查
	byRegion, err := s.dashboardSvc.CountByRegion(ctx, tenantID)
	if err != nil {
		s.logger.Warn("地域分布聚合失败", elog.FieldErr(err), elog.Int64("tenant_id", tenantID))
		return AssetStatistics{}, err
	}

	stats := AssetStatistics{
		TotalAssets:      overview.Total,
		ProviderStats:    groupCountsToMap(overview.ByProvider),
		AssetTypeStats:   mergeByAssetType(overview.ByType),
		RegionStats:      groupCountsToMap(byRegion),
		StatusStats:      groupCountsToMap(overview.ByStatus),
		TotalCost:        0, // 成本走 GetCostAnalysis / cost 模块；此处保留 0 兼容契约
		LastDiscoverTime: time.Now(),
	}
	return stats, nil
}

// mergeByAssetType 把 model_uid 维度的分布归并为「纯资产类型」分布。
//
// ecam_instance.model_uid 形如 "<provider>_<type>"（见 web/database_handler.go:180），
// 直接按 model_uid 出图会把同一类型按厂商拆成多段（aliyun_ecs / volcano_ecs / ...），
// 语义上「资产类型分布」应当是类型维度，故剥离首个下划线前缀后跨厂商合并计数。
// 无下划线的 key 原样保留；剥离后为空归 "_empty"。
func mergeByAssetType(groups []dao.GroupCount) map[string]int64 {
	m := make(map[string]int64, len(groups))
	for _, g := range groups {
		typ := g.Key
		if i := strings.Index(typ, "_"); i >= 0 {
			typ = typ[i+1:]
		}
		if typ == "" {
			typ = "_empty"
		}
		m[typ] += g.Count
	}
	return m
}

// groupCountsToMap 将 []dao.GroupCount 转为 map[key]count。
// 空 key（缺失字段）归一 "_empty"，保留「未归类」可见性。
func groupCountsToMap(groups []dao.GroupCount) map[string]int64 {
	m := make(map[string]int64, len(groups))
	for _, g := range groups {
		key := g.Key
		if key == "" {
			key = "_empty"
		}
		m[key] = g.Count
	}
	return m
}

// GetCostAnalysis 获取成本分析。
//
// 账期趋势来自 ecam_cost_unified_bill（明细表）：按 billing_date $sum amount_cny，
// tenant_id 恒定过滤。TotalCost = 各账期之和。
// AssetCosts/RegionCosts 工作台 sparkline 不消费，暂留空
// （后续若需资产级/地域级成本分布，可接 cost 模块 AggregateByField）。
//
// 窗口：明细表 billing_date 为账期粒度（月初），days<=0 归一为 90 天，
// 保证趋势图至少覆盖 3 个月度点而非退化成单点。
func (s *service) GetCostAnalysis(ctx context.Context, tenantID int64, provider string, days int) (CostAnalysis, error) {
	if days <= 0 {
		days = 90
	}
	rows, err := s.repo.AggregateDailyCost(ctx, tenantID, provider, days)
	if err != nil {
		s.logger.Warn("成本账期趋势聚合失败",
			elog.FieldErr(err),
			elog.Int64("tenant_id", tenantID),
			elog.String("provider", provider),
			elog.Int("days", days))
		return CostAnalysis{}, err
	}

	dailyCosts := make([]DailyCost, 0, len(rows))
	var total float64
	for _, r := range rows {
		dailyCosts = append(dailyCosts, DailyCost{Date: r.ID, Cost: r.AmountCNY})
		total += r.AmountCNY
	}

	return CostAnalysis{
		Provider:    provider,
		TotalCost:   total,
		DailyCosts:  dailyCosts,
		AssetCosts:  []AssetCost{},
		RegionCosts: make(map[string]float64),
	}, nil
}
