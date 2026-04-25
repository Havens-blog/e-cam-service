package main

import (
	"fmt"
	"sort"
	"testing"

	"pgregory.net/rapid"
)

// Feature: arms-apm-topology, Property 4: 域名沿 trace 链传播
// **Validates: Requirements 3.1, 3.2, 3.5**

// genServiceName generates a plausible service name.
func genServiceName() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		prefix := rapid.SampledFrom([]string{"svc-", "app-", "api-", ""}).Draw(t, "prefix")
		name := rapid.StringMatching(`[a-z][a-z0-9]{1,10}`).Draw(t, "name")
		return prefix + name
	})
}

// genDomain generates a plausible domain name.
func genDomain() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		sub := rapid.StringMatching(`[a-z]{2,8}`).Draw(t, "sub")
		tld := rapid.SampledFrom([]string{"com", "cn", "io", "net"}).Draw(t, "tld")
		return sub + "." + tld
	})
}

// buildLinearTrace creates a trace with a linear chain of spans across distinct services.
// root(svc0) → child(svc1) → child(svc2) → ...
func buildLinearTrace(traceID string, services []string, domain string) Trace {
	spans := make([]Span, len(services))
	for i, svc := range services {
		spans[i] = Span{
			SpanID:       fmt.Sprintf("span-%d", i),
			ParentSpanID: "",
			ServiceName:  svc,
			Tags:         make(map[string]string),
		}
		if i > 0 {
			spans[i].ParentSpanID = fmt.Sprintf("span-%d", i-1)
		}
	}
	if domain != "" {
		spans[0].Tags["http.host"] = domain
	}
	return Trace{TraceID: traceID, Spans: spans}
}

func TestProperty4_DomainPropagationAlongTrace(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		propagator := NewDefaultDomainPropagator()

		// Generate a set of distinct service names (at least 2 for cross-service edges)
		numServices := rapid.IntRange(2, 6).Draw(t, "numServices")
		serviceSet := make(map[string]bool)
		services := make([]string, 0, numServices)
		for len(services) < numServices {
			svc := genServiceName().Draw(t, fmt.Sprintf("svc_%d", len(services)))
			if !serviceSet[svc] {
				serviceSet[svc] = true
				services = append(services, svc)
			}
		}

		hasDomain := rapid.Bool().Draw(t, "hasDomain")
		var domain string
		if hasDomain {
			domain = genDomain().Draw(t, "domain")
		}

		trace := buildLinearTrace("trace-1", services, domain)
		edgeDomains := propagator.Propagate([]Trace{trace})

		if hasDomain {
			// Property 4a: All cross-service call edges should contain domain D
			for i := 0; i < len(services)-1; i++ {
				key := EdgeKey(services[i], services[i+1])
				domains, exists := edgeDomains[key]
				if !exists {
					t.Errorf("edge %s should exist in edgeDomains", key)
					continue
				}
				found := false
				for _, d := range domains {
					if d == domain {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("edge %s domains %v should contain %q", key, domains, domain)
				}
			}
		} else {
			// Property 4b: If root span has no http.host, no domains should be contributed
			for key, domains := range edgeDomains {
				if len(domains) > 0 {
					t.Errorf("edge %s should have no domains (root has no http.host), got %v", key, domains)
				}
			}
		}
	})
}

// Feature: arms-apm-topology, Property 5: 域名合并去重
// **Validates: Requirements 3.3**

func TestProperty5_DomainMergeDedup(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		propagator := NewDefaultDomainPropagator()

		// Two fixed services forming one edge
		svcA := "gateway"
		svcB := "backend"

		// Generate multiple traces through the same edge, each with a different domain
		numTraces := rapid.IntRange(1, 10).Draw(t, "numTraces")
		traces := make([]Trace, numTraces)
		allDomains := make(map[string]bool)

		for i := 0; i < numTraces; i++ {
			domain := genDomain().Draw(t, fmt.Sprintf("domain_%d", i))
			allDomains[domain] = true
			traces[i] = buildLinearTrace(fmt.Sprintf("trace-%d", i), []string{svcA, svcB}, domain)
		}

		edgeDomains := propagator.Propagate(traces)
		key := EdgeKey(svcA, svcB)
		domains := edgeDomains[key]

		// Property 5a: No duplicates in domains list
		seen := make(map[string]bool)
		for _, d := range domains {
			if seen[d] {
				t.Errorf("duplicate domain %q in edge %s domains", d, key)
			}
			seen[d] = true
		}

		// Property 5b: Length equals number of distinct domains
		if len(domains) != len(allDomains) {
			t.Errorf("edge %s: expected %d unique domains, got %d", key, len(allDomains), len(domains))
		}

		// Property 5c: All input domains are present
		for d := range allDomains {
			found := false
			for _, dd := range domains {
				if dd == d {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("domain %q missing from edge %s domains %v", d, key, domains)
			}
		}
	})
}

// Feature: arms-apm-topology, Property 6: 域名维度指标聚合
// **Validates: Requirements 3.4**

func TestProperty6_DomainMetricsAggregation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		propagator := NewDefaultDomainPropagator()

		svcA := "gateway"
		svcB := "backend"
		edgeKey := EdgeKey(svcA, svcB)

		// Generate 2 distinct domains with different trace counts
		domain1 := "api.example.com"
		domain2 := "web.example.com"

		count1 := rapid.IntRange(1, 10).Draw(t, "count1")
		count2 := rapid.IntRange(1, 10).Draw(t, "count2")

		var traces []Trace
		traceIdx := 0
		for i := 0; i < count1; i++ {
			traces = append(traces, buildLinearTrace(fmt.Sprintf("trace-%d", traceIdx), []string{svcA, svcB}, domain1))
			traceIdx++
		}
		for i := 0; i < count2; i++ {
			traces = append(traces, buildLinearTrace(fmt.Sprintf("trace-%d", traceIdx), []string{svcA, svcB}, domain2))
			traceIdx++
		}

		totalQPS := rapid.Float64Range(1.0, 1000.0).Draw(t, "totalQPS")
		totalErrorRate := rapid.Float64Range(0.0, 100.0).Draw(t, "totalErrorRate")
		latencyP99 := rapid.Float64Range(1.0, 5000.0).Draw(t, "latencyP99")

		deps := []ServiceDependency{
			{
				CallerServiceName: svcA,
				CalleeServiceName: svcB,
				QPS:               totalQPS,
				LatencyP99:        latencyP99,
				ErrorRate:         totalErrorRate,
			},
		}

		domainMetrics := propagator.AggregateDomainMetrics(traces, deps)

		metrics, ok := domainMetrics[edgeKey]
		if !ok {
			t.Fatalf("expected domain metrics for edge %s", edgeKey)
		}

		// Property 6a: Both domains should have metrics
		if _, ok := metrics[domain1]; !ok {
			t.Errorf("missing metrics for domain %s", domain1)
		}
		if _, ok := metrics[domain2]; !ok {
			t.Errorf("missing metrics for domain %s", domain2)
		}

		// Property 6b: QPS should be distributed proportionally
		totalCount := float64(count1 + count2)
		expectedQPS1 := totalQPS * float64(count1) / totalCount
		expectedQPS2 := totalQPS * float64(count2) / totalCount

		if !approxEqual(metrics[domain1].QPS, expectedQPS1, 0.001) {
			t.Errorf("domain1 QPS: expected %.3f, got %.3f", expectedQPS1, metrics[domain1].QPS)
		}
		if !approxEqual(metrics[domain2].QPS, expectedQPS2, 0.001) {
			t.Errorf("domain2 QPS: expected %.3f, got %.3f", expectedQPS2, metrics[domain2].QPS)
		}

		// Property 6c: Different domains' metrics should not affect each other
		// Sum of domain QPS should equal total QPS
		sumQPS := metrics[domain1].QPS + metrics[domain2].QPS
		if !approxEqual(sumQPS, totalQPS, 0.001) {
			t.Errorf("sum of domain QPS (%.3f) should equal total QPS (%.3f)", sumQPS, totalQPS)
		}

		// Property 6d: Domain metrics keys should match propagated domains
		edgeDomains := propagator.Propagate(traces)
		propagatedDomains := edgeDomains[edgeKey]
		metricDomains := make([]string, 0, len(metrics))
		for d := range metrics {
			metricDomains = append(metricDomains, d)
		}
		sort.Strings(propagatedDomains)
		sort.Strings(metricDomains)
		if len(propagatedDomains) != len(metricDomains) {
			t.Errorf("propagated domains %v != metric domains %v", propagatedDomains, metricDomains)
		}
	})
}

func approxEqual(a, b, epsilon float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < epsilon
}
