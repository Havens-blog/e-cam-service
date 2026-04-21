package dns

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// Feature: multicloud-dns-management, Property 8: CNAME/A 记录关联资源识别正确性
// Validates: Requirements 9.3, 9.4, 9.5, 9.6
//
// 对于任意 CNAME 类型的解析记录值，若该值匹配已知 CDN 域名后缀列表中的任一后缀，
// 则关联资源类型应为 "cdn"；若匹配已知 WAF 域名后缀，则应为 "waf"；
// 若不匹配任何已知后缀，则关联资源应为 nil。

func TestProperty8_CNAMELinkedResourceIdentification(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	linker := NewResourceLinker()

	for i := 0; i < propertyIterations; i++ {
		value, expectedType := generateCNAMETestCase(rng)

		result := linker.Identify("CNAME", value)

		switch expectedType {
		case "cdn":
			if result == nil || result.Type != "cdn" {
				t.Fatalf("iteration %d: CNAME value %q should be cdn, got %v", i, value, result)
			}
		case "waf":
			if result == nil || result.Type != "waf" {
				t.Fatalf("iteration %d: CNAME value %q should be waf, got %v", i, value, result)
			}
		case "":
			if result != nil {
				t.Fatalf("iteration %d: CNAME value %q should be nil, got %v", i, value, result)
			}
		}
	}
}

func TestProperty8_ARecordLinkedResource(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	linker := NewResourceLinker()

	// A records currently return nil (simplified implementation)
	for i := 0; i < propertyIterations; i++ {
		ip := fmt.Sprintf("%d.%d.%d.%d", rng.Intn(256), rng.Intn(256), rng.Intn(256), rng.Intn(256))
		result := linker.Identify("A", ip)
		if result != nil {
			t.Fatalf("iteration %d: A record %q should return nil, got %v", i, ip, result)
		}
	}
}

func TestProperty8_NonCNAMERecordTypes(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	linker := NewResourceLinker()

	otherTypes := []string{"AAAA", "MX", "TXT", "NS", "SRV", "CAA"}
	for i := 0; i < propertyIterations; i++ {
		rt := otherTypes[rng.Intn(len(otherTypes))]
		value := "some.cdn.aliyuncs.com" // Even with CDN suffix, non-CNAME types should return nil
		result := linker.Identify(rt, value)
		if result != nil {
			t.Fatalf("iteration %d: type %q should return nil, got %v", i, rt, result)
		}
	}
}

// generateCNAMETestCase 生成 CNAME 测试用例，返回值和期望的资源类型
func generateCNAMETestCase(rng *rand.Rand) (string, string) {
	switch rng.Intn(3) {
	case 0:
		// CDN suffix
		suffix := cdnSuffixes[rng.Intn(len(cdnSuffixes))]
		prefix := randomCNAMEPrefix(rng)
		return prefix + suffix, "cdn"
	case 1:
		// WAF suffix
		suffix := wafSuffixes[rng.Intn(len(wafSuffixes))]
		prefix := randomCNAMEPrefix(rng)
		return prefix + suffix, "waf"
	default:
		// No known suffix
		return randomCNAMEPrefix(rng) + ".example.com", ""
	}
}

func randomCNAMEPrefix(rng *rand.Rand) string {
	parts := rng.Intn(3) + 1
	var segments []string
	for i := 0; i < parts; i++ {
		segments = append(segments, randomString(rng, rng.Intn(8)+3))
	}
	return strings.Join(segments, ".")
}
