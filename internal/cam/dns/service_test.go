package dns

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ==================== Mock 实现 ====================

// mockCloudAccountService 模拟云账号服务
type mockCloudAccountService struct {
	accounts []*domain.CloudAccount
	err      error
}

func (m *mockCloudAccountService) GetAccountWithCredentials(_ context.Context, id int64) (*domain.CloudAccount, error) {
	if m.err != nil {
		return nil, m.err
	}
	for _, a := range m.accounts {
		if a.ID == id {
			return a, nil
		}
	}
	return nil, errors.New("account not found")
}

func (m *mockCloudAccountService) ListAccounts(_ context.Context, filter domain.CloudAccountFilter) ([]*domain.CloudAccount, int64, error) {
	if m.err != nil {
		return nil, 0, m.err
	}
	var result []*domain.CloudAccount
	for _, a := range m.accounts {
		if filter.TenantID != "" && a.TenantID != filter.TenantID {
			continue
		}
		result = append(result, a)
	}
	return result, int64(len(result)), nil
}

// mockDNSAdapter 模拟 DNS 适配器
type mockDNSAdapter struct {
	domains        []types.DNSDomain
	records        []types.DNSRecord
	createdRec     *types.DNSRecord
	updatedRec     *types.DNSRecord
	listDomainsErr error
	listRecordsErr error
	createErr      error
	updateErr      error
	deleteErr      error
}

func (m *mockDNSAdapter) ListDomains(_ context.Context) ([]types.DNSDomain, error) {
	return m.domains, m.listDomainsErr
}

func (m *mockDNSAdapter) ListRecords(_ context.Context, _ string) ([]types.DNSRecord, error) {
	return m.records, m.listRecordsErr
}

func (m *mockDNSAdapter) GetRecord(_ context.Context, _, _ string) (*types.DNSRecord, error) {
	if len(m.records) > 0 {
		return &m.records[0], nil
	}
	return nil, errors.New("not found")
}

func (m *mockDNSAdapter) CreateRecord(_ context.Context, _ string, _ types.CreateDNSRecordRequest) (*types.DNSRecord, error) {
	return m.createdRec, m.createErr
}

func (m *mockDNSAdapter) UpdateRecord(_ context.Context, _, _ string, _ types.UpdateDNSRecordRequest) (*types.DNSRecord, error) {
	return m.updatedRec, m.updateErr
}

func (m *mockDNSAdapter) DeleteRecord(_ context.Context, _, _ string) error {
	return m.deleteErr
}

// mockCloudAdapter 模拟 CloudAdapter
type mockCloudAdapter struct {
	dnsAdapter cloudx.DNSAdapter
	provider   domain.CloudProvider
}

func (m *mockCloudAdapter) GetProvider() domain.CloudProvider { return m.provider }
func (m *mockCloudAdapter) DNS() cloudx.DNSAdapter            { return m.dnsAdapter }

// 以下方法为 CloudAdapter 接口的空实现
func (m *mockCloudAdapter) Asset() cloudx.AssetAdapter                  { return nil }
func (m *mockCloudAdapter) ECS() cloudx.ECSAdapter                      { return nil }
func (m *mockCloudAdapter) SecurityGroup() cloudx.SecurityGroupAdapter  { return nil }
func (m *mockCloudAdapter) Image() cloudx.ImageAdapter                  { return nil }
func (m *mockCloudAdapter) Disk() cloudx.DiskAdapter                    { return nil }
func (m *mockCloudAdapter) Snapshot() cloudx.SnapshotAdapter            { return nil }
func (m *mockCloudAdapter) RDS() cloudx.RDSAdapter                      { return nil }
func (m *mockCloudAdapter) Redis() cloudx.RedisAdapter                  { return nil }
func (m *mockCloudAdapter) MongoDB() cloudx.MongoDBAdapter              { return nil }
func (m *mockCloudAdapter) VPC() cloudx.VPCAdapter                      { return nil }
func (m *mockCloudAdapter) EIP() cloudx.EIPAdapter                      { return nil }
func (m *mockCloudAdapter) VSwitch() cloudx.VSwitchAdapter              { return nil }
func (m *mockCloudAdapter) LB() cloudx.LBAdapter                        { return nil }
func (m *mockCloudAdapter) CDN() cloudx.CDNAdapter                      { return nil }
func (m *mockCloudAdapter) WAF() cloudx.WAFAdapter                      { return nil }
func (m *mockCloudAdapter) NAS() cloudx.NASAdapter                      { return nil }
func (m *mockCloudAdapter) OSS() cloudx.OSSAdapter                      { return nil }
func (m *mockCloudAdapter) Kafka() cloudx.KafkaAdapter                  { return nil }
func (m *mockCloudAdapter) Elasticsearch() cloudx.ElasticsearchAdapter  { return nil }
func (m *mockCloudAdapter) IAM() cloudx.IAMAdapter                      { return nil }
func (m *mockCloudAdapter) Tag() cloudx.TagAdapter                      { return nil }
func (m *mockCloudAdapter) ECSCreate() cloudx.ECSCreateAdapter          { return nil }
func (m *mockCloudAdapter) ResourceQuery() cloudx.ResourceQueryAdapter  { return nil }
func (m *mockCloudAdapter) ValidateCredentials(_ context.Context) error { return nil }

var _ cloudx.CloudAdapter = (*mockCloudAdapter)(nil)

// ==================== 纯函数单元测试（不依赖 MongoDB） ====================

func TestFilterDomains(t *testing.T) {
	domains := []DNSDomainVO{
		{DomainName: "example.com", Provider: "aliyun", AccountID: 1},
		{DomainName: "test.org", Provider: "aws", AccountID: 2},
		{DomainName: "Example.net", Provider: "aliyun", AccountID: 1},
	}

	t.Run("keyword filter case-insensitive", func(t *testing.T) {
		result := filterDomains(domains, DomainFilter{Keyword: "example"})
		assert.Len(t, result, 2)
	})

	t.Run("provider filter", func(t *testing.T) {
		result := filterDomains(domains, DomainFilter{Provider: "aws"})
		assert.Len(t, result, 1)
		assert.Equal(t, "test.org", result[0].DomainName)
	})

	t.Run("account_id filter", func(t *testing.T) {
		result := filterDomains(domains, DomainFilter{AccountID: 2})
		assert.Len(t, result, 1)
	})

	t.Run("combined filters", func(t *testing.T) {
		result := filterDomains(domains, DomainFilter{Keyword: "example", Provider: "aliyun"})
		assert.Len(t, result, 2)
	})

	t.Run("no filter returns all", func(t *testing.T) {
		result := filterDomains(domains, DomainFilter{})
		assert.Len(t, result, 3)
	})
}

func TestFilterRecords(t *testing.T) {
	records := []DNSRecordVO{
		{RR: "www", Type: "A", Value: "1.2.3.4"},
		{RR: "mail", Type: "MX", Value: "mail.example.com"},
		{RR: "cdn", Type: "CNAME", Value: "cdn.aliyuncs.com"},
	}

	t.Run("record_type filter", func(t *testing.T) {
		result := filterRecords(records, RecordFilter{RecordType: "A"})
		assert.Len(t, result, 1)
		assert.Equal(t, "www", result[0].RR)
	})

	t.Run("keyword matches RR", func(t *testing.T) {
		result := filterRecords(records, RecordFilter{Keyword: "mail"})
		assert.Len(t, result, 1)
	})

	t.Run("keyword matches Value", func(t *testing.T) {
		result := filterRecords(records, RecordFilter{Keyword: "aliyun"})
		assert.Len(t, result, 1)
	})
}

func TestPaginateDomains(t *testing.T) {
	domains := make([]DNSDomainVO, 50)
	for i := range domains {
		domains[i] = DNSDomainVO{DomainName: "d"}
	}

	t.Run("default limit", func(t *testing.T) {
		result := paginateDomains(domains, 0, 0)
		assert.Len(t, result, 20)
	})

	t.Run("offset beyond total", func(t *testing.T) {
		result := paginateDomains(domains, 100, 10)
		assert.Len(t, result, 0)
	})

	t.Run("negative offset treated as 0", func(t *testing.T) {
		result := paginateDomains(domains, -5, 10)
		assert.Len(t, result, 10)
	})

	t.Run("last page partial", func(t *testing.T) {
		result := paginateDomains(domains, 45, 10)
		assert.Len(t, result, 5)
	})
}

func TestWrapCloudError(t *testing.T) {
	t.Run("with provider", func(t *testing.T) {
		err := wrapCloudError("aliyun", errors.New("timeout"))
		assert.Contains(t, err.Error(), "aliyun")
		assert.Contains(t, err.Error(), "timeout")
	})

	t.Run("empty provider", func(t *testing.T) {
		err := wrapCloudError("", errors.New("timeout"))
		assert.Contains(t, err.Error(), "timeout")
		assert.Contains(t, err.Error(), "cloud DNS API error")
	})
}

func TestBatchDeleteResult_Consistency(t *testing.T) {
	result := &BatchDeleteResult{
		Total:        5,
		SuccessCount: 3,
		FailedCount:  2,
		Failures: []FailureDetail{
			{RecordID: "r1", Error: "err1"},
			{RecordID: "r2", Error: "err2"},
		},
	}
	assert.Equal(t, result.Total, result.SuccessCount+result.FailedCount)
	assert.Equal(t, result.FailedCount, len(result.Failures))
}

func TestRecordToVO(t *testing.T) {
	r := types.DNSRecord{
		RecordID: "rid-1",
		Domain:   "example.com",
		RR:       "www",
		Type:     "A",
		Value:    "1.2.3.4",
		TTL:      600,
		Status:   "enable",
	}
	vo := recordToVO(r, "aliyun", 1)
	assert.Equal(t, "rid-1", vo.RecordID)
	assert.Equal(t, "aliyun", vo.Provider)
	assert.Equal(t, int64(1), vo.AccountID)
	assert.Equal(t, "www", vo.RR)
}

func TestLinkerUnit(t *testing.T) {
	linker := NewResourceLinker()

	t.Run("CDN CNAME", func(t *testing.T) {
		r := linker.Identify("CNAME", "abc.cdn.aliyuncs.com")
		require.NotNil(t, r)
		assert.Equal(t, "cdn", r.Type)
	})

	t.Run("WAF CNAME", func(t *testing.T) {
		r := linker.Identify("CNAME", "abc.waf.aliyuncs.com")
		require.NotNil(t, r)
		assert.Equal(t, "waf", r.Type)
	})

	t.Run("CloudFront CNAME", func(t *testing.T) {
		r := linker.Identify("CNAME", "d123.cloudfront.net")
		require.NotNil(t, r)
		assert.Equal(t, "cdn", r.Type)
	})

	t.Run("unknown CNAME", func(t *testing.T) {
		r := linker.Identify("CNAME", "other.example.com")
		assert.Nil(t, r)
	})

	t.Run("A record returns nil", func(t *testing.T) {
		r := linker.Identify("A", "1.2.3.4")
		assert.Nil(t, r)
	})

	t.Run("TXT record returns nil", func(t *testing.T) {
		r := linker.Identify("TXT", "v=spf1 include:example.com")
		assert.Nil(t, r)
	})
}

// ==================== DAO 文档转换函数测试 ====================

func TestDomainDocToVO(t *testing.T) {
	t.Run("normal conversion", func(t *testing.T) {
		doc := DnsDomainDoc{
			DomainID:    "did-123",
			DomainName:  "example.com",
			Provider:    "aliyun",
			AccountID:   1,
			AccountName: "prod-aliyun",
			TenantID:    "tenant-1",
			RecordCount: 10,
			Status:      "normal",
		}
		vo := domainDocToVO(doc)
		assert.Equal(t, "example.com", vo.DomainName)
		assert.Equal(t, "aliyun", vo.Provider)
		assert.Equal(t, int64(1), vo.AccountID)
		assert.Equal(t, "prod-aliyun", vo.AccountName)
		assert.Equal(t, "did-123", vo.DomainID)
		assert.Equal(t, "normal", vo.Status)
		assert.Equal(t, int64(10), vo.RecordCount)
	})
}

func TestRecordDocToVO(t *testing.T) {
	t.Run("normal conversion", func(t *testing.T) {
		doc := DnsRecordDoc{
			RecordID:  "rid-456",
			Domain:    "example.com",
			RR:        "www",
			Type:      "A",
			Value:     "1.2.3.4",
			TTL:       600,
			Priority:  0,
			Line:      "default",
			Status:    "enable",
			Provider:  "aws",
			AccountID: 2,
			TenantID:  "tenant-1",
		}
		vo := recordDocToVO(doc)
		assert.Equal(t, "rid-456", vo.RecordID)
		assert.Equal(t, "example.com", vo.Domain)
		assert.Equal(t, "www", vo.RR)
		assert.Equal(t, "A", vo.Type)
		assert.Equal(t, "1.2.3.4", vo.Value)
		assert.Equal(t, 600, vo.TTL)
		assert.Equal(t, "aws", vo.Provider)
		assert.Equal(t, int64(2), vo.AccountID)
	})
}
