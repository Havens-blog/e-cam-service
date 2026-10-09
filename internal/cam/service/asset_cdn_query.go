package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cloudx-sdk"
	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/gotomicro/ego/core/elog"
)

// Top 域名查询参数边界(与 DAO 侧收敛值一致)
const (
	defaultTopDomainsLimit = 10
	maxTopDomainsLimit     = 100
)

// CDNMetricReader CDN 指标只读接口(service 层消费;dao.CDNMetricDAO 天然满足)。
// 二期指标读取只查本地指标表,不走厂商 API。
type CDNMetricReader interface {
	ListByDomain(ctx context.Context, domain string, days int, accountID int64) ([]types.CDNMetric, error)
	TopByBytes(ctx context.Context, days, limit int, accountID int64) ([]types.CDNMetricTopRow, error)
}

// CDNQueryService CDN 实时查询服务。
// 缓存配置等派生数据不随同步落库,详情页打开时按需经厂商 API 实时查询,
// 读取快照即可展示最新配置,同步链路保持轻量。
// 指标类查询(GetDomainMetrics/GetTopDomains)为纯读,只查 ecam_cdn_metric。
type CDNQueryService struct {
	accountRepo    repository.CloudAccountRepository
	adapterFactory *cloudx.AdapterFactory
	metrics        CDNMetricReader
	logger         *elog.Component
}

// NewCDNQueryService 创建 CDN 实时查询服务
func NewCDNQueryService(
	accountRepo repository.CloudAccountRepository,
	adapterFactory *cloudx.AdapterFactory,
	metrics CDNMetricReader,
	logger *elog.Component,
) *CDNQueryService {
	return &CDNQueryService{
		accountRepo:    accountRepo,
		adapterFactory: adapterFactory,
		metrics:        metrics,
		logger:         logger,
	}
}

// GetCacheConfig 查询指定账号下 CDN 域名的缓存规则。
// 按账号构建适配器,CDNAdapter 若实现了 CDNCacheQuerier 能力则实时查询。
func (s *CDNQueryService) GetCacheConfig(
	ctx context.Context,
	tenantID, accountID int64,
	domainName, domainID string,
) ([]types.CDNCacheRule, error) {
	if accountID <= 0 {
		return nil, fmt.Errorf("缺少云账号参数")
	}

	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("获取云账号失败: %w", err)
	}
	if account.TenantID != tenantID {
		return nil, fmt.Errorf("云账号不存在")
	}

	adapter, err := s.adapterFactory.CreateAdapter(&account)
	if err != nil {
		return nil, fmt.Errorf("创建云厂商适配器失败: %w", err)
	}
	cdnAdapter := adapter.CDN()
	if cdnAdapter == nil {
		return nil, fmt.Errorf("该云厂商不支持CDN")
	}
	querier, ok := cdnAdapter.(cloudx.CDNCacheQuerier)
	if !ok {
		return nil, fmt.Errorf("该云厂商暂不支持缓存配置查询")
	}

	rules, err := querier.GetCacheConfig(ctx, domainName, domainID)
	if err != nil {
		s.logger.Warn("查询CDN缓存配置失败",
			elog.String("provider", string(account.Provider)),
			elog.String("domain", domainName),
			elog.FieldErr(err))
		return nil, err
	}
	return rules, nil
}

// GetDomainSettings 查询指定账号下 CDN 域名的功能配置全景(性能优化/
// 访问控制/流量限制/HTTPS/重定向/回源)。
func (s *CDNQueryService) GetDomainSettings(
	ctx context.Context,
	tenantID, accountID int64,
	domainName, domainID string,
) (*types.CDNDomainSettings, error) {
	if accountID <= 0 {
		return nil, fmt.Errorf("缺少云账号参数")
	}

	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("获取云账号失败: %w", err)
	}
	if account.TenantID != tenantID {
		return nil, fmt.Errorf("云账号不存在")
	}

	adapter, err := s.adapterFactory.CreateAdapter(&account)
	if err != nil {
		return nil, fmt.Errorf("创建云厂商适配器失败: %w", err)
	}
	cdnAdapter := adapter.CDN()
	if cdnAdapter == nil {
		return nil, fmt.Errorf("该云厂商不支持CDN")
	}
	querier, ok := cdnAdapter.(cloudx.CDNSettingsQuerier)
	if !ok {
		return nil, fmt.Errorf("该云厂商暂不支持功能配置查询")
	}

	settings, err := querier.GetDomainSettings(ctx, domainName, domainID)
	if err != nil {
		s.logger.Warn("查询CDN功能配置失败",
			elog.String("provider", string(account.Provider)),
			elog.String("domain", domainName),
			elog.FieldErr(err))
		return nil, err
	}
	return settings, nil
}

// GetDomainMetrics 查询租户内指定 CDN 域名近 N 天的单日指标。
// 纯读指标表(采集任务落库),不走厂商 API。指标表未记录租户字段,
// 通过「先取租户全部云账号 → 逐账号过滤查询」实现租户隔离。
func (s *CDNQueryService) GetDomainMetrics(ctx context.Context, tenantID int64, domainName string, days int) ([]types.CDNMetric, error) {
	if domainName == "" {
		return nil, fmt.Errorf("缺少域名参数")
	}
	accountIDs, err := s.tenantAccountIDs(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if len(accountIDs) == 0 {
		return []types.CDNMetric{}, nil
	}

	// 域名归属唯一账号,逐账号过滤后合并;date 降序
	var merged []types.CDNMetric
	for _, id := range accountIDs {
		items, err := s.metrics.ListByDomain(ctx, domainName, days, id)
		if err != nil {
			return nil, fmt.Errorf("查询域名指标失败: %w", err)
		}
		merged = append(merged, items...)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Date > merged[j].Date })
	if merged == nil {
		merged = []types.CDNMetric{}
	}
	return merged, nil
}

// GetTopDomains 查询租户内近 N 天流量 Top 域名(字节求和)。
// 纯读指标表,租户隔离同 GetDomainMetrics。
func (s *CDNQueryService) GetTopDomains(ctx context.Context, tenantID int64, days, limit int) ([]types.CDNMetricTopRow, error) {
	accountIDs, err := s.tenantAccountIDs(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if len(accountIDs) == 0 {
		return []types.CDNMetricTopRow{}, nil
	}
	if limit <= 0 {
		limit = defaultTopDomainsLimit
	}
	if limit > maxTopDomainsLimit {
		limit = maxTopDomainsLimit
	}

	merged := make([]types.CDNMetricTopRow, 0, len(accountIDs))
	for _, id := range accountIDs {
		rows, err := s.metrics.TopByBytes(ctx, days, limit, id)
		if err != nil {
			return nil, fmt.Errorf("查询 Top 域名失败: %w", err)
		}
		merged = append(merged, rows...)
	}
	// 域名只归属一个账号,账号间不会重复计同一域名;合并后降序截断
	sort.Slice(merged, func(i, j int) bool { return merged[i].Bytes > merged[j].Bytes })
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

// tenantAccountIDs 取租户全部云账号 ID;租户无账号时返回空切片
func (s *CDNQueryService) tenantAccountIDs(ctx context.Context, tenantID int64) ([]int64, error) {
	accounts, _, err := s.accountRepo.List(ctx, domain.CloudAccountFilter{TenantID: tenantID})
	if err != nil {
		return nil, fmt.Errorf("获取租户云账号失败: %w", err)
	}
	ids := make([]int64, 0, len(accounts))
	for _, a := range accounts {
		ids = append(ids, a.ID)
	}
	return ids, nil
}
