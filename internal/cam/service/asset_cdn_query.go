package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/gotomicro/ego/core/elog"
)

// CDNQueryService CDN 实时查询服务。
// 缓存配置等派生数据不随同步落库,详情页打开时按需经厂商 API 实时查询,
// 读取快照即可展示最新配置,同步链路保持轻量。
type CDNQueryService struct {
	accountRepo    repository.CloudAccountRepository
	adapterFactory *cloudx.AdapterFactory
	logger         *elog.Component
}

// NewCDNQueryService 创建 CDN 实时查询服务
func NewCDNQueryService(
	accountRepo repository.CloudAccountRepository,
	adapterFactory *cloudx.AdapterFactory,
	logger *elog.Component,
) *CDNQueryService {
	return &CDNQueryService{
		accountRepo:    accountRepo,
		adapterFactory: adapterFactory,
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
