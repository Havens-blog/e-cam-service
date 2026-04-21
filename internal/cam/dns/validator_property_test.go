package dns

import (
	"fmt"
	"math/rand"
	"net"
	"strings"
	"testing"
)

// Feature: multicloud-dns-management, Property 1: DNS 记录校验的完备性
// Validates: Requirements 12.1, 12.2, 12.3, 12.4, 12.5, 12.6, 12.7, 4.4, 4.5
//
// 生成随机记录类型+值+TTL+优先级组合，验证 ValidateRecord 正确接受/拒绝。
// 使用手动随机生成（testing/quick 对复杂条件组合不够灵活）。

const propertyIterations = 200

func TestProperty1_ValidateRecordCompleteness(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	for i := 0; i < propertyIterations; i++ {
		tc := generateTestCase(rng)
		err := ValidateRecord(tc.recordType, tc.rr, tc.value, tc.ttl, tc.priority)

		// Determine expected outcome
		expected := expectedOutcome(tc)

		if expected.shouldPass && err != nil {
			t.Fatalf("iteration %d: expected pass but got error: %v\n  input: %+v", i, err, tc)
		}
		if !expected.shouldPass && err == nil {
			t.Fatalf("iteration %d: expected rejection but got nil\n  input: %+v\n  reason: %s", i, tc, expected.reason)
		}
	}
}

type testCase struct {
	recordType string
	rr         string
	value      string
	ttl        int
	priority   int
}

func (tc testCase) String() string {
	return fmt.Sprintf("{type=%q rr=%q value=%q ttl=%d priority=%d}", tc.recordType, tc.rr, tc.value, tc.ttl, tc.priority)
}

type outcome struct {
	shouldPass bool
	reason     string
}

// generateTestCase 生成一个随机测试用例，混合合法和非法输入
func generateTestCase(rng *rand.Rand) testCase {
	allTypes := []string{"A", "AAAA", "CNAME", "MX", "TXT", "NS", "SRV", "CAA", "INVALID", "PTR"}
	recordType := allTypes[rng.Intn(len(allTypes))]

	rr := randomRR(rng)
	value := randomValue(rng, recordType)
	ttl := randomTTL(rng)
	priority := randomPriority(rng)

	return testCase{
		recordType: recordType,
		rr:         rr,
		value:      value,
		ttl:        ttl,
		priority:   priority,
	}
}

func randomRR(rng *rand.Rand) string {
	choices := []string{"www", "@", "mail", "sub", "", "   ", "\t", "api", "*"}
	return choices[rng.Intn(len(choices))]
}

func randomValue(rng *rand.Rand, recordType string) string {
	switch rng.Intn(3) {
	case 0:
		// Generate a value appropriate for the record type
		return validValueForType(rng, recordType)
	case 1:
		// Generate a value inappropriate for the record type
		return invalidValueForType(rng, recordType)
	default:
		// Random edge cases
		edgeCases := []string{"", "   ", "abc", "1.2.3.4", "::1", "example.com", strings.Repeat("x", 513)}
		return edgeCases[rng.Intn(len(edgeCases))]
	}
}

func validValueForType(rng *rand.Rand, recordType string) string {
	switch recordType {
	case "A":
		return fmt.Sprintf("%d.%d.%d.%d", rng.Intn(256), rng.Intn(256), rng.Intn(256), rng.Intn(256))
	case "AAAA":
		return fmt.Sprintf("2001:db8::%x", rng.Intn(0xffff))
	case "CNAME", "NS", "SRV":
		labels := []string{"example.com", "sub.example.com", "ns1.dns.com", "sip.example.org"}
		return labels[rng.Intn(len(labels))]
	case "MX":
		labels := []string{"mail.example.com", "mx1.google.com", "smtp.example.org"}
		return labels[rng.Intn(len(labels))]
	case "TXT":
		length := rng.Intn(512) + 1
		return strings.Repeat("t", length)
	case "CAA":
		return "0 issue letsencrypt.org"
	default:
		return "some-value"
	}
}

func invalidValueForType(rng *rand.Rand, recordType string) string {
	switch recordType {
	case "A":
		invalids := []string{"not-ip", "::1", "999.999.999.999", "abc.def.ghi.jkl"}
		return invalids[rng.Intn(len(invalids))]
	case "AAAA":
		invalids := []string{"1.2.3.4", "not-ipv6", "gggg::zzzz"}
		return invalids[rng.Intn(len(invalids))]
	case "CNAME", "NS", "SRV", "MX":
		invalids := []string{"not a domain!", "-bad.com", "a", ""}
		return invalids[rng.Intn(len(invalids))]
	case "TXT":
		return strings.Repeat("x", 513+rng.Intn(100))
	default:
		return ""
	}
}

func randomTTL(rng *rand.Rand) int {
	switch rng.Intn(4) {
	case 0:
		return rng.Intn(86400) + 1 // valid range [1, 86400]
	case 1:
		return 0 // invalid
	case 2:
		return -rng.Intn(100) - 1 // negative
	default:
		return 86401 + rng.Intn(1000) // too large
	}
}

func randomPriority(rng *rand.Rand) int {
	switch rng.Intn(4) {
	case 0:
		return rng.Intn(65535) + 1 // valid [1, 65535]
	case 1:
		return 0 // invalid for MX
	case 2:
		return -rng.Intn(100) - 1 // negative
	default:
		return 65536 + rng.Intn(1000) // too large
	}
}

// expectedOutcome 根据输入确定 ValidateRecord 应该通过还是拒绝
func expectedOutcome(tc testCase) outcome {
	// 1. RR 为空或纯空白 → 拒绝
	if strings.TrimSpace(tc.rr) == "" {
		return outcome{false, "RR is empty/whitespace"}
	}
	// 2. Value 为空或纯空白 → 拒绝
	if strings.TrimSpace(tc.value) == "" {
		return outcome{false, "value is empty/whitespace"}
	}
	// 3. TTL 不在 [1, 86400] → 拒绝
	if tc.ttl < 1 || tc.ttl > 86400 {
		return outcome{false, "TTL out of range"}
	}
	// 4. 非法记录类型 → 拒绝
	if !ValidRecordTypes[tc.recordType] {
		return outcome{false, "invalid record type"}
	}

	// 5. 类型特定校验
	switch tc.recordType {
	case "A":
		ip := net.ParseIP(tc.value)
		if ip == nil || ip.To4() == nil {
			return outcome{false, "invalid IPv4"}
		}
	case "AAAA":
		ip := net.ParseIP(tc.value)
		if ip == nil || ip.To4() != nil {
			return outcome{false, "invalid IPv6"}
		}
	case "CNAME", "NS", "SRV":
		if !domainRegex.MatchString(tc.value) {
			return outcome{false, "invalid domain name"}
		}
	case "MX":
		if !domainRegex.MatchString(tc.value) {
			return outcome{false, "invalid domain name for MX"}
		}
		if tc.priority < 1 || tc.priority > 65535 {
			return outcome{false, "MX priority out of range"}
		}
	case "TXT":
		if len(tc.value) > 512 {
			return outcome{false, "TXT too long"}
		}
	case "CAA":
		// No additional validation beyond non-empty
	}

	return outcome{true, ""}
}
