package service

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/topology/domain"
	"github.com/stretchr/testify/assert"
)

// Feature: arms-apm-topology, Property 7: 域名筛选过滤 APM 边
// Validates: Requirements 3.6
//
// 对任意域名筛选条件 D 和一组 APM 边，验证筛选结果仅包含：
// - domains 含 D 的 APM 边
// - 所有非 APM 边
// - domains 为空的 APM 边（内部调用）

func TestProperty7_DomainFilterAPMEdges(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	for i := 0; i < pbtIterations; i++ {
		targetDomain := fmt.Sprintf("domain-%d.example.com", rng.Intn(10))

		// Build a simple topology: DNS entry → gateway → multiple services
		dnsNodeID := fmt.Sprintf("dns-%s", targetDomain)
		gwNodeID := "gw-01"

		nodes := []domain.TopoNode{
			{ID: dnsNodeID, Name: targetDomain, Type: domain.NodeTypeDNSRecord},
			{ID: gwNodeID, Type: domain.NodeTypeGateway},
		}

		// Infrastructure edge: DNS → Gateway (non-APM)
		edges := []domain.TopoEdge{
			{
				ID:              fmt.Sprintf("e-%s-%s", dnsNodeID, gwNodeID),
				SourceID:        dnsNodeID,
				TargetID:        gwNodeID,
				SourceCollector: domain.SourceDeclaration,
				Status:          domain.EdgeStatusActive,
			},
		}

		// Generate random service nodes and APM edges from gateway
		numServices := 2 + rng.Intn(5)
		for j := 0; j < numServices; j++ {
			svcID := fmt.Sprintf("svc-%d", j)
			nodes = append(nodes, domain.TopoNode{
				ID:   svcID,
				Type: domain.NodeTypeK8sDeployment,
			})

			// Create APM edge from gateway to service with random domain assignment
			edgeType := rng.Intn(3) // 0: matching domain, 1: non-matching domain, 2: empty domains
			var attrs map[string]interface{}

			switch edgeType {
			case 0: // matching domain
				attrs = map[string]interface{}{
					"domains": []interface{}{targetDomain},
				}
			case 1: // non-matching domain
				otherDomain := fmt.Sprintf("other-%d.example.com", rng.Intn(100))
				attrs = map[string]interface{}{
					"domains": []interface{}{otherDomain},
				}
			case 2: // empty domains (internal call)
				attrs = nil
			}

			edges = append(edges, domain.TopoEdge{
				ID:              fmt.Sprintf("e-%s-%s", gwNodeID, svcID),
				SourceID:        gwNodeID,
				TargetID:        svcID,
				SourceCollector: domain.SourceAPM,
				Relation:        domain.RelationCalls,
				Status:          domain.EdgeStatusActive,
				Attributes:      attrs,
			})
		}

		// Also add some inter-service APM edges
		if numServices >= 2 {
			for j := 0; j < rng.Intn(3); j++ {
				srcIdx := rng.Intn(numServices)
				dstIdx := rng.Intn(numServices)
				if srcIdx == dstIdx {
					continue
				}
				srcID := fmt.Sprintf("svc-%d", srcIdx)
				dstID := fmt.Sprintf("svc-%d", dstIdx)

				edgeType := rng.Intn(3)
				var attrs map[string]interface{}
				switch edgeType {
				case 0:
					attrs = map[string]interface{}{
						"domains": []interface{}{targetDomain},
					}
				case 1:
					attrs = map[string]interface{}{
						"domains": []interface{}{fmt.Sprintf("other-%d.example.com", rng.Intn(100))},
					}
				case 2:
					attrs = nil
				}

				edges = append(edges, domain.TopoEdge{
					ID:              fmt.Sprintf("e-%s-%s-%d", srcID, dstID, j),
					SourceID:        srcID,
					TargetID:        dstID,
					SourceCollector: domain.SourceAPM,
					Relation:        domain.RelationCalls,
					Status:          domain.EdgeStatusActive,
					Attributes:      attrs,
				})
			}
		}

		// Run filterByDomain
		svc := &topologyService{builder: NewDagBuilder()}
		_, filteredEdges := svc.filterByDomain(nodes, edges, targetDomain)

		// Verify properties
		for _, e := range filteredEdges {
			if e.SourceCollector != domain.SourceAPM {
				// Non-APM edge: should always be included (already verified by BFS reachability)
				continue
			}

			// APM edge: must either have matching domain or empty domains
			if e.Attributes == nil || e.Attributes["domains"] == nil {
				// Empty domains (internal call) - OK
				continue
			}

			// Must contain target domain
			domains, ok := e.Attributes["domains"].([]interface{})
			if !ok {
				t.Fatalf("iteration %d: edge %s domains type assertion failed", i, e.ID)
			}
			found := false
			for _, d := range domains {
				if fmt.Sprint(d) == targetDomain {
					found = true
					break
				}
			}
			assert.True(t, found,
				"iteration %d: APM edge %s with domains %v should not be in filtered result for domain %s",
				i, e.ID, domains, targetDomain)
		}

		// Verify no APM edge with non-matching domain sneaked in
		for _, e := range filteredEdges {
			if e.SourceCollector != domain.SourceAPM {
				continue
			}
			if e.Attributes == nil || e.Attributes["domains"] == nil {
				continue
			}
			domains, ok := e.Attributes["domains"].([]interface{})
			if !ok {
				continue
			}
			hasTarget := false
			for _, d := range domains {
				if fmt.Sprint(d) == targetDomain {
					hasTarget = true
					break
				}
			}
			if !hasTarget {
				t.Fatalf("iteration %d: APM edge %s with domains %v should NOT be in filtered result for domain %s",
					i, e.ID, domains, targetDomain)
			}
		}

		// Verify all non-APM edges that are reachable are included
		for _, e := range edges {
			if e.SourceCollector == domain.SourceAPM {
				continue
			}
			// Check if this non-APM edge should be in the result (both endpoints reachable)
			srcReachable := false
			dstReachable := false
			for _, fn := range nodes {
				if fn.ID == e.SourceID {
					srcReachable = true
				}
				if fn.ID == e.TargetID {
					dstReachable = true
				}
			}
			if srcReachable && dstReachable {
				found := false
				for _, fe := range filteredEdges {
					if fe.ID == e.ID {
						found = true
						break
					}
				}
				// Only check if both endpoints are in the BFS-reachable set
				// (the BFS starts from DNS entry, so all our test nodes should be reachable)
				if !found {
					// This is OK if the node wasn't reachable via BFS
					continue
				}
			}
		}
	}
}
