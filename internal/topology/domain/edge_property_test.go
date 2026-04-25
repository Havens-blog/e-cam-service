package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"
)

// Feature: arms-apm-topology, Property 1: APM 边属性序列化往返保持不变
// Validates: Requirements 1.3, 1.4, 1.5
//
// 生成随机的 APM 边 attributes（qps、latency_p99、error_rate、last_seen_at、domains、domain_metrics），
// JSON 序列化再反序列化后验证所有字段值相等。

const pbtIterations = 200

func TestProperty1_APMEdgeAttributesRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	for i := 0; i < pbtIterations; i++ {
		attrs := generateAPMAttributes(rng)

		// JSON serialize
		data, err := json.Marshal(attrs)
		if err != nil {
			t.Fatalf("iteration %d: marshal error: %v", i, err)
		}

		// JSON deserialize
		var restored map[string]interface{}
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("iteration %d: unmarshal error: %v", i, err)
		}

		// Verify qps
		if !floatEqual(attrs["qps"].(float64), restored["qps"].(float64)) {
			t.Fatalf("iteration %d: qps mismatch: %v != %v", i, attrs["qps"], restored["qps"])
		}

		// Verify latency_p99
		if !floatEqual(attrs["latency_p99"].(float64), restored["latency_p99"].(float64)) {
			t.Fatalf("iteration %d: latency_p99 mismatch: %v != %v", i, attrs["latency_p99"], restored["latency_p99"])
		}

		// Verify error_rate
		if !floatEqual(attrs["error_rate"].(float64), restored["error_rate"].(float64)) {
			t.Fatalf("iteration %d: error_rate mismatch: %v != %v", i, attrs["error_rate"], restored["error_rate"])
		}

		// Verify last_seen_at
		if attrs["last_seen_at"] != restored["last_seen_at"] {
			t.Fatalf("iteration %d: last_seen_at mismatch: %v != %v", i, attrs["last_seen_at"], restored["last_seen_at"])
		}

		// Verify domains
		origDomains := attrs["domains"].([]string)
		restoredDomains, ok := restored["domains"].([]interface{})
		if !ok {
			t.Fatalf("iteration %d: domains type assertion failed", i)
		}
		if len(origDomains) != len(restoredDomains) {
			t.Fatalf("iteration %d: domains length mismatch: %d != %d", i, len(origDomains), len(restoredDomains))
		}
		for j, d := range origDomains {
			if d != fmt.Sprint(restoredDomains[j]) {
				t.Fatalf("iteration %d: domains[%d] mismatch: %v != %v", i, j, d, restoredDomains[j])
			}
		}

		// Verify domain_metrics
		origMetrics := attrs["domain_metrics"].(map[string]interface{})
		restoredMetrics, ok := restored["domain_metrics"].(map[string]interface{})
		if !ok {
			t.Fatalf("iteration %d: domain_metrics type assertion failed", i)
		}
		if len(origMetrics) != len(restoredMetrics) {
			t.Fatalf("iteration %d: domain_metrics length mismatch: %d != %d", i, len(origMetrics), len(restoredMetrics))
		}
		for domain, origVal := range origMetrics {
			restoredVal, exists := restoredMetrics[domain]
			if !exists {
				t.Fatalf("iteration %d: domain_metrics missing key %q", i, domain)
			}
			origM := origVal.(map[string]interface{})
			restoredM := restoredVal.(map[string]interface{})
			if !floatEqual(origM["qps"].(float64), restoredM["qps"].(float64)) {
				t.Fatalf("iteration %d: domain_metrics[%s].qps mismatch", i, domain)
			}
			if !floatEqual(origM["latency_p99"].(float64), restoredM["latency_p99"].(float64)) {
				t.Fatalf("iteration %d: domain_metrics[%s].latency_p99 mismatch", i, domain)
			}
			if !floatEqual(origM["error_rate"].(float64), restoredM["error_rate"].(float64)) {
				t.Fatalf("iteration %d: domain_metrics[%s].error_rate mismatch", i, domain)
			}
		}
	}
}

// generateAPMAttributes 生成随机的 APM 边属性
func generateAPMAttributes(rng *rand.Rand) map[string]interface{} {
	// Generate domains
	numDomains := rng.Intn(5)
	domains := make([]string, numDomains)
	for j := 0; j < numDomains; j++ {
		domains[j] = fmt.Sprintf("%s.example.com", randomLabel(rng))
	}

	// Generate domain_metrics
	domainMetrics := make(map[string]interface{})
	for _, d := range domains {
		domainMetrics[d] = map[string]interface{}{
			"qps":         math.Round(rng.Float64()*1000*100) / 100,
			"latency_p99": math.Round(rng.Float64()*5000*100) / 100,
			"error_rate":  math.Round(rng.Float64()*100*100) / 100,
		}
	}

	return map[string]interface{}{
		"qps":            math.Round(rng.Float64()*10000*100) / 100,
		"latency_p99":    math.Round(rng.Float64()*5000*100) / 100,
		"error_rate":     math.Round(rng.Float64()*100*100) / 100,
		"last_seen_at":   time.Now().Add(-time.Duration(rng.Intn(3600)) * time.Second).Format(time.RFC3339),
		"domains":        domains,
		"domain_metrics": domainMetrics,
	}
}

func randomLabel(rng *rand.Rand) string {
	labels := []string{"api", "web", "admin", "gateway", "auth", "order", "payment", "user", "search", "notify"}
	return labels[rng.Intn(len(labels))]
}

func floatEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
