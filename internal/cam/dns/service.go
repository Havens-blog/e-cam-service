package dns

import (
	"context"
	"fmt"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// CloudAccountService 云账号服务接口（用于获取账号信息以路由到正确的适配器）
type CloudAccountService interface {
	GetAccountWithCredentials(ctx context.Context, id int64) (*shareddomain.CloudAccount, error)
	ListAccounts(ctx context.Context, filter shareddomain.CloudAccountFilter) ([]*shareddomain.CloudAccount, int64, error)
}

// DNSService DNS 管理业务逻辑层接口
type DNSService interface {
	ListDomains(ctx context.Context, tenantID string, filter DomainFilter) ([]DNSDomainVO, int64, error)
	ListRecords(ctx context.Context, tenantID string, domainName string, filter RecordFilter) ([]DNSRecordVO, int64, error)
	SearchRecords(ctx context.Context, tenantID string, keyword string, limit int64) ([]DNSRecordVO, int64, error)
	CreateRecord(ctx context.Context, tenantID string, domainName string, req CreateRecordReq) (*DNSRecordVO, error)
	UpdateRecord(ctx context.Context, tenantID string, domainName string, recordID string, req UpdateRecordReq) (*DNSRecordVO, error)
	DeleteRecord(ctx context.Context, tenantID string, domainName string, recordID string) error
	BatchDeleteRecords(ctx context.Context, tenantID string, domainName string, recordIDs []string) (*BatchDeleteResult, error)
	GetStats(ctx context.Context, tenantID string) (*DNSStats, error)
}

// dnsService DNSService 实现
type dnsService struct {
	accountSvc     CloudAccountService
	adapterFactory *cloudx.AdapterFactory
	domainDAO      *DnsDomainDAO
	recordDAO      *DnsRecordDAO
	linker         *ResourceLinker
	logger         *elog.Component
}

// NewDNSService 创建 DNSService 实例
func NewDNSService(accountSvc CloudAccountService, adapterFactory *cloudx.AdapterFactory, domainDAO *DnsDomainDAO, recordDAO *DnsRecordDAO) DNSService {
	logger := elog.DefaultLogger
	if logger == nil {
		logger = elog.EgoLogger
	}
	return &dnsService{
		accountSvc:     accountSvc,
		adapterFactory: adapterFactory,
		domainDAO:      domainDAO,
		recordDAO:      recordDAO,
		linker:         NewResourceLinker(),
		logger:         logger,
	}
}

// Ensure compile-time interface compliance
var _ DNSService = (*dnsService)(nil)

// ListDomains 查询域名列表：从本地 MongoDB c_dns_domain 查询已同步的 DNS 域名
func (s *dnsService) ListDomains(ctx context.Context, tenantID string, filter DomainFilter) ([]DNSDomainVO, int64, error) {
	docs, total, err := s.domainDAO.ListDomains(ctx, tenantID, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("query dns domains: %w", err)
	}

	var result []DNSDomainVO
	for _, doc := range docs {
		result = append(result, domainDocToVO(doc))
	}
	if result == nil {
		result = []DNSDomainVO{}
	}
	return result, total, nil
}

// ListRecords 查询解析记录列表：从本地 MongoDB c_dns_record 查询已同步的 DNS 记录
func (s *dnsService) ListRecords(ctx context.Context, tenantID string, domainName string, filter RecordFilter) ([]DNSRecordVO, int64, error) {
	docs, total, err := s.recordDAO.ListRecords(ctx, tenantID, domainName, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("query dns records: %w", err)
	}

	var result []DNSRecordVO
	for _, doc := range docs {
		vo := recordDocToVO(doc)
		vo.LinkedResource = s.linker.Identify(vo.Type, vo.Value)
		result = append(result, vo)
	}
	if result == nil {
		result = []DNSRecordVO{}
	}
	return result, total, nil
}

// SearchRecords 跨域名搜索解析记录（按 RR 或完整子域名模糊匹配所有域名下的记录）
func (s *dnsService) SearchRecords(ctx context.Context, tenantID string, keyword string, limit int64) ([]DNSRecordVO, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	docs, total, err := s.recordDAO.SearchRecords(ctx, tenantID, keyword, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("search dns records: %w", err)
	}

	var result []DNSRecordVO
	for _, doc := range docs {
		vo := recordDocToVO(doc)
		vo.LinkedResource = s.linker.Identify(vo.Type, vo.Value)
		result = append(result, vo)
	}
	if result == nil {
		result = []DNSRecordVO{}
	}
	return result, total, nil
}

// CreateRecord 创建解析记录（实时调用云 API）
func (s *dnsService) CreateRecord(ctx context.Context, tenantID string, domainName string, req CreateRecordReq) (*DNSRecordVO, error) {
	if err := ValidateRecord(req.Type, req.RR, req.Value, req.TTL, req.Priority); err != nil {
		return nil, err
	}

	account, adapter, err := s.findAdapterByAccountID(ctx, req.AccountID)
	if err != nil {
		return nil, err
	}

	created, err := adapter.CreateRecord(ctx, domainName, types.CreateDNSRecordRequest{
		RR:       req.RR,
		Type:     req.Type,
		Value:    req.Value,
		TTL:      req.TTL,
		Priority: req.Priority,
		Line:     req.Line,
	})
	if err != nil {
		return nil, wrapCloudError(string(account.Provider), err)
	}

	vo := recordToVO(*created, string(account.Provider), account.ID)
	vo.LinkedResource = s.linker.Identify(created.Type, created.Value)
	return &vo, nil
}

// UpdateRecord 修改解析记录（实时调用云 API）
func (s *dnsService) UpdateRecord(ctx context.Context, tenantID string, domainName string, recordID string, req UpdateRecordReq) (*DNSRecordVO, error) {
	account, adapter, err := s.findAdapterByAccountID(ctx, req.AccountID)
	if err != nil {
		return nil, err
	}

	updated, err := adapter.UpdateRecord(ctx, domainName, recordID, types.UpdateDNSRecordRequest{
		RR:       req.RR,
		Type:     req.Type,
		Value:    req.Value,
		TTL:      req.TTL,
		Priority: req.Priority,
		Line:     req.Line,
	})
	if err != nil {
		return nil, wrapCloudError(string(account.Provider), err)
	}

	vo := recordToVO(*updated, string(account.Provider), account.ID)
	vo.LinkedResource = s.linker.Identify(updated.Type, updated.Value)
	return &vo, nil
}

// DeleteRecord 删除解析记录（实时调用云 API）
func (s *dnsService) DeleteRecord(ctx context.Context, tenantID string, domainName string, recordID string) error {
	_, adapter, err := s.findAdapterForDomain(ctx, tenantID, domainName)
	if err != nil {
		return err
	}

	if err := adapter.DeleteRecord(ctx, domainName, recordID); err != nil {
		return wrapCloudError("", err)
	}
	return nil
}

// BatchDeleteRecords 批量删除解析记录
func (s *dnsService) BatchDeleteRecords(ctx context.Context, tenantID string, domainName string, recordIDs []string) (*BatchDeleteResult, error) {
	result := &BatchDeleteResult{
		Total:    len(recordIDs),
		Failures: make([]FailureDetail, 0),
	}

	for _, id := range recordIDs {
		err := s.DeleteRecord(ctx, tenantID, domainName, id)
		if err != nil {
			result.FailedCount++
			result.Failures = append(result.Failures, FailureDetail{
				RecordID: id,
				Error:    err.Error(),
			})
		} else {
			result.SuccessCount++
		}
	}

	return result, nil
}

// GetStats 获取 DNS 统计数据：从本地 MongoDB 聚合
func (s *dnsService) GetStats(ctx context.Context, tenantID string) (*DNSStats, error) {
	stats := &DNSStats{
		ProviderDistrib:   make(map[string]int64),
		RecordTypeDistrib: make(map[string]int64),
	}

	// 统计域名总数
	totalDomains, err := s.domainDAO.CountDomains(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("count dns domains: %w", err)
	}
	stats.TotalDomains = totalDomains

	// 按云厂商统计域名数
	providerDistrib, err := s.domainDAO.CountDomainsByProvider(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("count dns domains by provider: %w", err)
	}
	stats.ProviderDistrib = providerDistrib

	// 按记录类型统计
	recordTypeDistrib, err := s.recordDAO.CountRecordsByType(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("count dns records by type: %w", err)
	}
	stats.RecordTypeDistrib = recordTypeDistrib

	// 计算总记录数
	for _, count := range recordTypeDistrib {
		stats.TotalRecords += count
	}

	return stats, nil
}

// ==================== 内部辅助方法 ====================

// domainDocToVO 将 DnsDomainDoc 转换为 DNSDomainVO
func domainDocToVO(doc DnsDomainDoc) DNSDomainVO {
	return DNSDomainVO{
		DomainName:  doc.DomainName,
		Provider:    doc.Provider,
		AccountID:   doc.AccountID,
		AccountName: doc.AccountName,
		RecordCount: doc.RecordCount,
		Status:      doc.Status,
		DomainID:    doc.DomainID,
	}
}

// recordDocToVO 将 DnsRecordDoc 转换为 DNSRecordVO
func recordDocToVO(doc DnsRecordDoc) DNSRecordVO {
	return DNSRecordVO{
		RecordID:  doc.RecordID,
		Domain:    doc.Domain,
		RR:        doc.RR,
		Type:      doc.Type,
		Value:     doc.Value,
		TTL:       doc.TTL,
		Priority:  doc.Priority,
		Line:      doc.Line,
		Status:    doc.Status,
		Provider:  doc.Provider,
		AccountID: doc.AccountID,
	}
}

// findAdapterForDomain 找到域名所属的云账号和 DNS 适配器
// 通过查询 c_dns_domain 中的域名记录找到对应的 account_id
func (s *dnsService) findAdapterForDomain(ctx context.Context, tenantID string, domainName string) (*shareddomain.CloudAccount, cloudx.DNSAdapter, error) {
	// 先从 c_dns_domain 查找域名对应的账号
	docs, _, err := s.domainDAO.ListDomains(ctx, tenantID, DomainFilter{
		Keyword: domainName,
		Limit:   1,
	})
	if err == nil && len(docs) > 0 {
		// 精确匹配域名
		for _, doc := range docs {
			if doc.DomainName == domainName {
				return s.findAdapterByAccountID(ctx, doc.AccountID)
			}
		}
	}

	// 回退：遍历所有账号查找
	accounts, err := s.getTenantAccounts(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}

	for _, account := range accounts {
		adapter, err := s.adapterFactory.CreateAdapter(account)
		if err != nil {
			continue
		}
		dnsAdapter := adapter.DNS()
		if dnsAdapter == nil {
			continue
		}
		domains, err := dnsAdapter.ListDomains(ctx)
		if err != nil {
			continue
		}
		for _, d := range domains {
			if d.DomainName == domainName {
				return account, dnsAdapter, nil
			}
		}
	}
	return nil, nil, ErrDNSAccountNotFound
}

// findAdapterByAccountID 根据账号 ID 获取 DNS 适配器
func (s *dnsService) findAdapterByAccountID(ctx context.Context, accountID int64) (*shareddomain.CloudAccount, cloudx.DNSAdapter, error) {
	account, err := s.accountSvc.GetAccountWithCredentials(ctx, accountID)
	if err != nil {
		return nil, nil, ErrDNSAccountNotFound
	}

	adapter, err := s.adapterFactory.CreateAdapter(account)
	if err != nil {
		return nil, nil, wrapCloudError(string(account.Provider), err)
	}

	dnsAdapter := adapter.DNS()
	if dnsAdapter == nil {
		return nil, nil, fmt.Errorf("DNS adapter not supported for provider: %s", account.Provider)
	}

	return account, dnsAdapter, nil
}

// getTenantAccounts 获取租户下所有活跃云账号
func (s *dnsService) getTenantAccounts(ctx context.Context, tenantID string) ([]*shareddomain.CloudAccount, error) {
	accounts, _, err := s.accountSvc.ListAccounts(ctx, shareddomain.CloudAccountFilter{
		TenantID: tenantID,
		Status:   shareddomain.CloudAccountStatusActive,
		Limit:    1000,
	})
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	return accounts, nil
}

// ==================== 过滤与分页（导出以便属性测试） ====================

// filterDomains 按条件过滤域名列表
func filterDomains(domains []DNSDomainVO, filter DomainFilter) []DNSDomainVO {
	var result []DNSDomainVO
	for _, d := range domains {
		if filter.Keyword != "" && !strings.Contains(strings.ToLower(d.DomainName), strings.ToLower(filter.Keyword)) {
			continue
		}
		if filter.Provider != "" && d.Provider != filter.Provider {
			continue
		}
		if filter.AccountID > 0 && d.AccountID != filter.AccountID {
			continue
		}
		result = append(result, d)
	}
	return result
}

// paginateDomains 对域名列表分页
func paginateDomains(domains []DNSDomainVO, offset, limit int64) []DNSDomainVO {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 20
	}
	total := int64(len(domains))
	if offset >= total {
		return []DNSDomainVO{}
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return domains[offset:end]
}

// filterRecords 按条件过滤解析记录列表
func filterRecords(records []DNSRecordVO, filter RecordFilter) []DNSRecordVO {
	var result []DNSRecordVO
	for _, r := range records {
		if filter.RecordType != "" && r.Type != filter.RecordType {
			continue
		}
		if filter.Keyword != "" {
			kw := strings.ToLower(filter.Keyword)
			if !strings.Contains(strings.ToLower(r.RR), kw) && !strings.Contains(strings.ToLower(r.Value), kw) {
				continue
			}
		}
		result = append(result, r)
	}
	return result
}

// paginateRecords 对解析记录列表分页
func paginateRecords(records []DNSRecordVO, offset, limit int64) []DNSRecordVO {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 20
	}
	total := int64(len(records))
	if offset >= total {
		return []DNSRecordVO{}
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return records[offset:end]
}

// recordToVO 将 cloudx types.DNSRecord 转换为 DNSRecordVO
func recordToVO(r types.DNSRecord, provider string, accountID int64) DNSRecordVO {
	return DNSRecordVO{
		RecordID:  r.RecordID,
		Domain:    r.Domain,
		RR:        r.RR,
		Type:      r.Type,
		Value:     r.Value,
		TTL:       r.TTL,
		Priority:  r.Priority,
		Line:      r.Line,
		Status:    r.Status,
		Provider:  provider,
		AccountID: accountID,
	}
}

// wrapCloudError 包装云 API 错误，包含云厂商名称
func wrapCloudError(provider string, err error) error {
	if provider == "" {
		return fmt.Errorf("cloud DNS API error: %w", err)
	}
	return fmt.Errorf("[%s] cloud DNS API error: %w", provider, err)
}
