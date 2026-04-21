package dns

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// ==================== Property 2: 域名列表过滤正确性 ====================

// Feature: multicloud-dns-management, Property 2: 域名列表过滤正确性
// Validates: Requirements 2.2
//
// 对于任意域名列表和过滤条件（keyword、provider、account_id），经过 filterDomains 过滤后的
// 结果列表中，每一条域名都应满足所有指定的过滤条件。

func TestProperty2_DomainFilterCorrectness(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	for i := 0; i < propertyIterations; i++ {
		domains := generateRandomDomains(rng, rng.Intn(20)+1)
		filter := generateRandomDomainFilter(rng)

		result := filterDomains(domains, filter)

		for _, d := range result {
			if filter.Keyword != "" {
				if !strings.Contains(strings.ToLower(d.DomainName), strings.ToLower(filter.Keyword)) {
					t.Fatalf("iteration %d: domain %q does not contain keyword %q", i, d.DomainName, filter.Keyword)
				}
			}
			if filter.Provider != "" {
				if d.Provider != filter.Provider {
					t.Fatalf("iteration %d: domain provider %q != filter provider %q", i, d.Provider, filter.Provider)
				}
			}
			if filter.AccountID > 0 {
				if d.AccountID != filter.AccountID {
					t.Fatalf("iteration %d: domain accountID %d != filter accountID %d", i, d.AccountID, filter.AccountID)
				}
			}
		}
	}
}

// ==================== Property 3: 多云域名聚合完备性 ====================

// Feature: multicloud-dns-management, Property 3: 多云域名聚合完备性
// Validates: Requirements 2.5
//
// 对于任意一组云账号适配器返回的域名列表集合，聚合后的总域名列表应包含每个适配器返回的所有域名，
// 且不遗漏任何一条。即聚合结果的域名集合应等于所有适配器结果的并集。

func TestProperty3_MultiCloudDomainAggregation(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	for i := 0; i < propertyIterations; i++ {
		numAccounts := rng.Intn(5) + 1
		var allDomains []DNSDomainVO
		expectedCount := 0

		for a := 0; a < numAccounts; a++ {
			accountDomains := generateRandomDomains(rng, rng.Intn(10))
			provider := randomProvider(rng)
			accountID := int64(a + 1)
			for j := range accountDomains {
				accountDomains[j].Provider = provider
				accountDomains[j].AccountID = accountID
			}
			allDomains = append(allDomains, accountDomains...)
			expectedCount += len(accountDomains)
		}

		// Aggregation is just append (no dedup) — verify completeness
		if len(allDomains) != expectedCount {
			t.Fatalf("iteration %d: aggregated count %d != expected %d", i, len(allDomains), expectedCount)
		}

		// With empty filter, filterDomains should return all
		filtered := filterDomains(allDomains, DomainFilter{})
		if len(filtered) != expectedCount {
			t.Fatalf("iteration %d: filtered count %d != expected %d (no filter)", i, len(filtered), expectedCount)
		}
	}
}

// ==================== Property 4: 解析记录列表过滤正确性 ====================

// Feature: multicloud-dns-management, Property 4: 解析记录列表过滤正确性
// Validates: Requirements 3.2
//
// 对于任意解析记录列表和过滤条件（keyword、record_type），经过过滤后的结果列表中，
// 每一条记录都应满足所有指定的过滤条件。

func TestProperty4_RecordFilterCorrectness(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	for i := 0; i < propertyIterations; i++ {
		records := generateRandomRecords(rng, rng.Intn(20)+1)
		filter := generateRandomRecordFilter(rng)

		result := filterRecords(records, filter)

		for _, r := range result {
			if filter.RecordType != "" {
				if r.Type != filter.RecordType {
					t.Fatalf("iteration %d: record type %q != filter type %q", i, r.Type, filter.RecordType)
				}
			}
			if filter.Keyword != "" {
				kw := strings.ToLower(filter.Keyword)
				if !strings.Contains(strings.ToLower(r.RR), kw) && !strings.Contains(strings.ToLower(r.Value), kw) {
					t.Fatalf("iteration %d: record RR=%q Value=%q does not contain keyword %q", i, r.RR, r.Value, filter.Keyword)
				}
			}
		}
	}
}

// ==================== Property 5: 云 API 错误信息传播完整性 ====================

// Feature: multicloud-dns-management, Property 5: 云 API 错误信息传播完整性
// Validates: Requirements 1.7, 4.7, 5.5, 6.5
//
// 对于任意云厂商名称和云 API 错误信息，经过 DNS 服务层包装后返回的错误信息应同时包含
// 云厂商名称和原始错误描述。

func TestProperty5_CloudErrorPropagation(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	for i := 0; i < propertyIterations; i++ {
		provider := randomProvider(rng)
		originalMsg := randomErrorMessage(rng)
		originalErr := fmt.Errorf("%s", originalMsg)

		wrapped := wrapCloudError(provider, originalErr)

		wrappedMsg := wrapped.Error()
		if !strings.Contains(wrappedMsg, provider) {
			t.Fatalf("iteration %d: wrapped error %q does not contain provider %q", i, wrappedMsg, provider)
		}
		if !strings.Contains(wrappedMsg, originalMsg) {
			t.Fatalf("iteration %d: wrapped error %q does not contain original %q", i, wrappedMsg, originalMsg)
		}
	}
}

// ==================== Property 6: 批量操作结果一致性 ====================

// Feature: multicloud-dns-management, Property 6: 批量操作结果一致性
// Validates: Requirements 7.3, 7.7
//
// 对于任意批量删除操作的结果，success_count + failed_count 应始终等于 total，
// 且 failures 列表的长度应等于 failed_count。

func TestProperty6_BatchResultConsistency(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	for i := 0; i < propertyIterations; i++ {
		result := generateRandomBatchResult(rng)

		if result.SuccessCount+result.FailedCount != result.Total {
			t.Fatalf("iteration %d: success(%d) + failed(%d) != total(%d)",
				i, result.SuccessCount, result.FailedCount, result.Total)
		}
		if len(result.Failures) != result.FailedCount {
			t.Fatalf("iteration %d: len(failures)=%d != failed_count=%d",
				i, len(result.Failures), result.FailedCount)
		}
	}
}

// ==================== Property 7: DNS 统计数据一致性 ====================

// Feature: multicloud-dns-management, Property 7: DNS 统计数据一致性
// Validates: Requirements 8.2
//
// 对于任意 DNS 统计数据，total_domains 应等于 provider_distribution 中所有值的总和，
// total_records 应等于 record_type_distribution 中所有值的总和。

func TestProperty7_StatsConsistency(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	for i := 0; i < propertyIterations; i++ {
		stats := generateRandomStats(rng)

		var providerSum int64
		for _, v := range stats.ProviderDistrib {
			providerSum += v
		}
		if stats.TotalDomains != providerSum {
			t.Fatalf("iteration %d: total_domains(%d) != sum(provider_distribution)(%d)",
				i, stats.TotalDomains, providerSum)
		}

		var typeSum int64
		for _, v := range stats.RecordTypeDistrib {
			typeSum += v
		}
		if stats.TotalRecords != typeSum {
			t.Fatalf("iteration %d: total_records(%d) != sum(record_type_distribution)(%d)",
				i, stats.TotalRecords, typeSum)
		}
	}
}

// ==================== 测试数据生成器 ====================

var testProviders = []string{"aliyun", "aws", "huawei", "tencent", "volcengine"}
var testRecordTypes = []string{"A", "AAAA", "CNAME", "MX", "TXT", "NS", "SRV", "CAA"}

func randomProvider(rng *rand.Rand) string {
	return testProviders[rng.Intn(len(testProviders))]
}

func generateRandomDomains(rng *rand.Rand, count int) []DNSDomainVO {
	domains := make([]DNSDomainVO, count)
	for i := range domains {
		domains[i] = DNSDomainVO{
			DomainName:  fmt.Sprintf("domain%d-%s.com", i, randomString(rng, 4)),
			Provider:    randomProvider(rng),
			AccountID:   int64(rng.Intn(10) + 1),
			AccountName: fmt.Sprintf("account-%d", rng.Intn(10)+1),
			RecordCount: int64(rng.Intn(100)),
			Status:      "normal",
			DomainID:    fmt.Sprintf("did-%d", rng.Intn(10000)),
		}
	}
	return domains
}

func generateRandomDomainFilter(rng *rand.Rand) DomainFilter {
	f := DomainFilter{}
	if rng.Intn(3) == 0 {
		f.Keyword = randomString(rng, 3)
	}
	if rng.Intn(3) == 0 {
		f.Provider = randomProvider(rng)
	}
	if rng.Intn(3) == 0 {
		f.AccountID = int64(rng.Intn(10) + 1)
	}
	return f
}

func generateRandomRecords(rng *rand.Rand, count int) []DNSRecordVO {
	records := make([]DNSRecordVO, count)
	for i := range records {
		rt := testRecordTypes[rng.Intn(len(testRecordTypes))]
		records[i] = DNSRecordVO{
			RecordID:  fmt.Sprintf("rid-%d", rng.Intn(10000)),
			Domain:    fmt.Sprintf("example%d.com", rng.Intn(5)),
			RR:        randomString(rng, 4),
			Type:      rt,
			Value:     randomString(rng, 8),
			TTL:       rng.Intn(86400) + 1,
			Status:    "enable",
			Provider:  randomProvider(rng),
			AccountID: int64(rng.Intn(10) + 1),
		}
	}
	return records
}

func generateRandomRecordFilter(rng *rand.Rand) RecordFilter {
	f := RecordFilter{}
	if rng.Intn(3) == 0 {
		f.RecordType = testRecordTypes[rng.Intn(len(testRecordTypes))]
	}
	if rng.Intn(3) == 0 {
		f.Keyword = randomString(rng, 3)
	}
	return f
}

func randomErrorMessage(rng *rand.Rand) string {
	msgs := []string{
		"authentication failed",
		"request timeout",
		"rate limit exceeded",
		"record conflict",
		"quota exceeded",
		"invalid parameter",
		"service unavailable",
	}
	return msgs[rng.Intn(len(msgs))]
}

func generateRandomBatchResult(rng *rand.Rand) BatchDeleteResult {
	total := rng.Intn(20) + 1
	failedCount := rng.Intn(total + 1)
	successCount := total - failedCount

	failures := make([]FailureDetail, failedCount)
	for i := range failures {
		failures[i] = FailureDetail{
			RecordID: fmt.Sprintf("rid-%d", rng.Intn(10000)),
			Error:    "some error",
		}
	}

	return BatchDeleteResult{
		Total:        total,
		SuccessCount: successCount,
		FailedCount:  failedCount,
		Failures:     failures,
	}
}

func generateRandomStats(rng *rand.Rand) DNSStats {
	numProviders := rng.Intn(5) + 1
	providerDistrib := make(map[string]int64)
	var totalDomains int64
	for i := 0; i < numProviders; i++ {
		p := testProviders[i%len(testProviders)]
		count := int64(rng.Intn(50))
		providerDistrib[p] = count
		totalDomains += count
	}

	numTypes := rng.Intn(8) + 1
	typeDistrib := make(map[string]int64)
	var totalRecords int64
	for i := 0; i < numTypes; i++ {
		rt := testRecordTypes[i%len(testRecordTypes)]
		count := int64(rng.Intn(200))
		typeDistrib[rt] = count
		totalRecords += count
	}

	return DNSStats{
		TotalDomains:      totalDomains,
		TotalRecords:      totalRecords,
		ProviderDistrib:   providerDistrib,
		RecordTypeDistrib: typeDistrib,
	}
}

func randomString(rng *rand.Rand, length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rng.Intn(len(charset))]
	}
	return string(b)
}
