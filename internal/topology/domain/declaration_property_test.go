package domain

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

// Feature: arms-apm-topology, Property 8: APM 声明处理属性传递与来源标识
// Validates: Requirements 4.1, 4.2
//
// 对任意 source 为 "arms-apm" 的 LinkDeclaration，验证 ToTopoEdges() 生成的边
// source_collector == "apm" 且 Attributes 与 link 的 Attributes 键值对一一对应。
// 同时验证 ToTopoNode() 生成的节点 source_collector == "apm"。

func TestProperty8_APMDeclarationAttributePassingAndSourceIdentification(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	for i := 0; i < pbtIterations; i++ {
		decl := generateAPMDeclaration(rng)

		// Verify ToTopoNode source_collector
		node := decl.ToTopoNode()
		if node.SourceCollector != SourceAPM {
			t.Fatalf("iteration %d: ToTopoNode() source_collector = %q, want %q",
				i, node.SourceCollector, SourceAPM)
		}

		// Verify ToTopoEdges
		edges := decl.ToTopoEdges()
		if len(edges) != len(decl.Links) {
			t.Fatalf("iteration %d: ToTopoEdges() returned %d edges, want %d",
				i, len(edges), len(decl.Links))
		}

		for j, edge := range edges {
			link := decl.Links[j]

			// source_collector must be "apm"
			if edge.SourceCollector != SourceAPM {
				t.Fatalf("iteration %d, edge %d: source_collector = %q, want %q",
					i, j, edge.SourceCollector, SourceAPM)
			}

			// Attributes must match link Attributes
			if len(edge.Attributes) != len(link.Attributes) {
				t.Fatalf("iteration %d, edge %d: Attributes length %d != link Attributes length %d",
					i, j, len(edge.Attributes), len(link.Attributes))
			}

			for key, linkVal := range link.Attributes {
				edgeVal, exists := edge.Attributes[key]
				if !exists {
					t.Fatalf("iteration %d, edge %d: missing Attributes key %q", i, j, key)
				}
				// Compare float64 values with tolerance, string values directly
				switch lv := linkVal.(type) {
				case float64:
					ev, ok := edgeVal.(float64)
					if !ok {
						t.Fatalf("iteration %d, edge %d: Attributes[%q] type mismatch: want float64, got %T",
							i, j, key, edgeVal)
					}
					if math.Abs(lv-ev) > 1e-9 {
						t.Fatalf("iteration %d, edge %d: Attributes[%q] = %v, want %v",
							i, j, key, ev, lv)
					}
				case string:
					ev, ok := edgeVal.(string)
					if !ok || lv != ev {
						t.Fatalf("iteration %d, edge %d: Attributes[%q] = %v, want %v",
							i, j, key, edgeVal, linkVal)
					}
				case []string:
					ev, ok := edgeVal.([]string)
					if !ok {
						t.Fatalf("iteration %d, edge %d: Attributes[%q] type mismatch: want []string, got %T",
							i, j, key, edgeVal)
					}
					if len(lv) != len(ev) {
						t.Fatalf("iteration %d, edge %d: Attributes[%q] length %d != %d",
							i, j, key, len(ev), len(lv))
					}
					for k := range lv {
						if lv[k] != ev[k] {
							t.Fatalf("iteration %d, edge %d: Attributes[%q][%d] = %q, want %q",
								i, j, key, k, ev[k], lv[k])
						}
					}
				default:
					// For other types (maps, etc.), check pointer equality (same reference)
					if fmt.Sprintf("%v", linkVal) != fmt.Sprintf("%v", edgeVal) {
						t.Fatalf("iteration %d, edge %d: Attributes[%q] = %v, want %v",
							i, j, key, edgeVal, linkVal)
					}
				}
			}
		}
	}
}

// generateAPMDeclaration generates a random LinkDeclaration with source "arms-apm"
func generateAPMDeclaration(rng *rand.Rand) LinkDeclaration {
	numLinks := 1 + rng.Intn(5)
	links := make([]DeclarationLink, numLinks)
	for i := 0; i < numLinks; i++ {
		attrs := generateRandomLinkAttributes(rng)
		links[i] = DeclarationLink{
			Target:     fmt.Sprintf("k8s-cluster1-ns1-svc-%d", rng.Intn(100)),
			TargetType: "k8s_deployment",
			Relation:   RelationCalls,
			Direction:  DirectionOutbound,
			Attributes: attrs,
		}
	}

	return LinkDeclaration{
		Source:    "arms-apm",
		Collector: "api",
		Node: DeclarationNode{
			ID:       fmt.Sprintf("k8s-cluster1-ns1-svc-%d", rng.Intn(100)),
			Name:     fmt.Sprintf("svc-%d", rng.Intn(100)),
			Type:     NodeTypeK8sDeployment,
			Category: CategoryContainer,
		},
		Links:    links,
		TenantID: "t1",
	}
}

// generateRandomLinkAttributes generates random APM-style attributes for a link
func generateRandomLinkAttributes(rng *rand.Rand) map[string]interface{} {
	numDomains := rng.Intn(4)
	domains := make([]string, numDomains)
	for i := 0; i < numDomains; i++ {
		domains[i] = fmt.Sprintf("%s.example.com", randomLabel(rng))
	}

	attrs := map[string]interface{}{
		"qps":         math.Round(rng.Float64()*10000*100) / 100,
		"latency_p99": math.Round(rng.Float64()*5000*100) / 100,
		"error_rate":  math.Round(rng.Float64()*100*100) / 100,
		"last_seen_at": fmt.Sprintf("2025-01-01T%02d:%02d:%02dZ",
			rng.Intn(24), rng.Intn(60), rng.Intn(60)),
	}

	if numDomains > 0 {
		attrs["domains"] = domains
	}

	return attrs
}
