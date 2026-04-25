package main

import (
	"fmt"
	"testing"

	"pgregory.net/rapid"
)

// Feature: arms-apm-topology, Property 3: ARMS 数据到 LinkDeclaration 转换完整性
// **Validates: Requirements 2.5, 2.6**

func TestProperty3_ARMSToLinkDeclarationCompleteness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		tenantID := "tenant-test"
		generator := NewDefaultDeclarationGenerator(tenantID)

		// Generate random mappable service dependencies
		numDeps := rapid.IntRange(1, 10).Draw(t, "numDeps")
		nameMapping := make(map[string]string)
		deps := make([]ServiceDependency, numDeps)

		for i := 0; i < numDeps; i++ {
			callerARMS := fmt.Sprintf("arms-caller-%d", i)
			calleeARMS := fmt.Sprintf("arms-callee-%d", i)
			callerNodeID := fmt.Sprintf("k8s-cluster-ns-caller%d", i)
			calleeNodeID := fmt.Sprintf("k8s-cluster-ns-callee%d", i)

			nameMapping[callerARMS] = callerNodeID
			nameMapping[calleeARMS] = calleeNodeID

			deps[i] = ServiceDependency{
				CallerServiceName: callerARMS,
				CalleeServiceName: calleeARMS,
				QPS:               rapid.Float64Range(0.1, 10000.0).Draw(t, fmt.Sprintf("qps_%d", i)),
				LatencyP99:        rapid.Float64Range(0.1, 5000.0).Draw(t, fmt.Sprintf("p99_%d", i)),
				ErrorRate:         rapid.Float64Range(0.0, 100.0).Draw(t, fmt.Sprintf("err_%d", i)),
			}
		}

		edgeDomains := make(map[string][]string)
		domainMetrics := make(map[string]map[string]DomainMetric)

		declarations := generator.Generate(deps, nameMapping, edgeDomains, domainMetrics)

		// Collect all generated links across all declarations
		type generatedLink struct {
			callerNodeID string
			link         DeclarationLink
			decl         LinkDeclaration
		}
		var allLinks []generatedLink
		for _, decl := range declarations {
			for _, link := range decl.Links {
				allLinks = append(allLinks, generatedLink{
					callerNodeID: decl.Node.ID,
					link:         link,
					decl:         decl,
				})
			}
		}

		// Property 3a: Every mappable dependency should produce a link
		if len(allLinks) != numDeps {
			t.Errorf("expected %d links for %d mappable deps, got %d", numDeps, numDeps, len(allLinks))
		}

		for _, decl := range declarations {
			// Property 3b: source must be "arms-apm"
			if decl.Source != "arms-apm" {
				t.Errorf("declaration source should be 'arms-apm', got %q", decl.Source)
			}

			// Property 3c: collector must be "api"
			if decl.Collector != "api" {
				t.Errorf("declaration collector should be 'api', got %q", decl.Collector)
			}

			// Property 3d: node ID must be a mapped K8s node ID
			found := false
			for _, nodeID := range nameMapping {
				if nodeID == decl.Node.ID {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("declaration node ID %q is not a mapped K8s node ID", decl.Node.ID)
			}

			for _, link := range decl.Links {
				// Property 3e: relation must be "calls"
				if link.Relation != "calls" {
					t.Errorf("link relation should be 'calls', got %q", link.Relation)
				}

				// Property 3f: attributes must contain qps, latency_p99, error_rate, last_seen_at
				requiredAttrs := []string{"qps", "latency_p99", "error_rate", "last_seen_at"}
				for _, attr := range requiredAttrs {
					if _, ok := link.Attributes[attr]; !ok {
						t.Errorf("link attributes missing required field %q", attr)
					}
				}
			}

			// Property 3g: tenant_id must be set
			if decl.TenantID != tenantID {
				t.Errorf("declaration tenant_id should be %q, got %q", tenantID, decl.TenantID)
			}
		}
	})
}

func TestProperty3_UnmappedServicesSkipped(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		tenantID := "tenant-test"
		generator := NewDefaultDeclarationGenerator(tenantID)

		// Create deps where some callers/callees are NOT in the mapping
		numMapped := rapid.IntRange(1, 5).Draw(t, "numMapped")
		numUnmapped := rapid.IntRange(1, 5).Draw(t, "numUnmapped")

		nameMapping := make(map[string]string)
		var deps []ServiceDependency

		// Mapped deps
		for i := 0; i < numMapped; i++ {
			caller := fmt.Sprintf("mapped-caller-%d", i)
			callee := fmt.Sprintf("mapped-callee-%d", i)
			nameMapping[caller] = fmt.Sprintf("k8s-c-ns-caller%d", i)
			nameMapping[callee] = fmt.Sprintf("k8s-c-ns-callee%d", i)
			deps = append(deps, ServiceDependency{
				CallerServiceName: caller,
				CalleeServiceName: callee,
				QPS:               10.0,
				LatencyP99:        100.0,
				ErrorRate:         1.0,
			})
		}

		// Unmapped deps (caller not in mapping)
		for i := 0; i < numUnmapped; i++ {
			deps = append(deps, ServiceDependency{
				CallerServiceName: fmt.Sprintf("unmapped-caller-%d", i),
				CalleeServiceName: fmt.Sprintf("mapped-callee-0"),
				QPS:               10.0,
				LatencyP99:        100.0,
				ErrorRate:         1.0,
			})
		}

		declarations := generator.Generate(deps, nameMapping, nil, nil)

		// Count total links
		totalLinks := 0
		for _, decl := range declarations {
			totalLinks += len(decl.Links)
		}

		// Only mapped deps should produce links
		if totalLinks != numMapped {
			t.Errorf("expected %d links (mapped only), got %d", numMapped, totalLinks)
		}
	})
}
