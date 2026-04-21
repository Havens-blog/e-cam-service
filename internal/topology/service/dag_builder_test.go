package service

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/topology/domain"
	"github.com/stretchr/testify/assert"
)

func TestDagBuilder_ComputeDepths_SingleChain(t *testing.T) {
	// DNS → CDN → WAF → SLB → Gateway → Service → Deploy → RDS
	builder := NewDagBuilder()
	nodes := []domain.TopoNode{
		{ID: "dns-1", Type: domain.NodeTypeDNSRecord},
		{ID: "cdn-1", Type: domain.NodeTypeCDN},
		{ID: "waf-1", Type: domain.NodeTypeWAF},
		{ID: "slb-1", Type: domain.NodeTypeSLB},
		{ID: "gw-1", Type: domain.NodeTypeGateway},
		{ID: "svc-1", Type: domain.NodeTypeK8sService},
		{ID: "dep-1", Type: domain.NodeTypeK8sDeployment},
		{ID: "rds-1", Type: domain.NodeTypeRDS},
	}
	edges := []domain.TopoEdge{
		{ID: "e1", SourceID: "dns-1", TargetID: "cdn-1", Status: domain.EdgeStatusActive},
		{ID: "e2", SourceID: "cdn-1", TargetID: "waf-1", Status: domain.EdgeStatusActive},
		{ID: "e3", SourceID: "waf-1", TargetID: "slb-1", Status: domain.EdgeStatusActive},
		{ID: "e4", SourceID: "slb-1", TargetID: "gw-1", Status: domain.EdgeStatusActive},
		{ID: "e5", SourceID: "gw-1", TargetID: "svc-1", Status: domain.EdgeStatusActive},
		{ID: "e6", SourceID: "svc-1", TargetID: "dep-1", Status: domain.EdgeStatusActive},
		{ID: "e7", SourceID: "dep-1", TargetID: "rds-1", Status: domain.EdgeStatusActive},
	}

	builder.ComputeDepths(nodes, edges)

	assert.Equal(t, 0, nodes[0].DagDepth) // dns
	assert.Equal(t, 1, nodes[1].DagDepth) // cdn
	assert.Equal(t, 2, nodes[2].DagDepth) // waf
	assert.Equal(t, 3, nodes[3].DagDepth) // slb
	assert.Equal(t, 4, nodes[4].DagDepth) // gw
	assert.Equal(t, 5, nodes[5].DagDepth) // svc
	assert.Equal(t, 6, nodes[6].DagDepth) // dep
	assert.Equal(t, 7, nodes[7].DagDepth) // rds
}

func TestDagBuilder_ComputeDepths_ShortChain(t *testing.T) {
	// DNS → CDN → OSS (only 3 nodes)
	builder := NewDagBuilder()
	nodes := []domain.TopoNode{
		{ID: "dns-1", Type: domain.NodeTypeDNSRecord},
		{ID: "cdn-1", Type: domain.NodeTypeCDN},
		{ID: "oss-1", Type: domain.NodeTypeOSS},
	}
	edges := []domain.TopoEdge{
		{ID: "e1", SourceID: "dns-1", TargetID: "cdn-1", Status: domain.EdgeStatusActive},
		{ID: "e2", SourceID: "cdn-1", TargetID: "oss-1", Status: domain.EdgeStatusActive},
	}

	builder.ComputeDepths(nodes, edges)

	assert.Equal(t, 0, nodes[0].DagDepth)
	assert.Equal(t, 1, nodes[1].DagDepth)
	assert.Equal(t, 2, nodes[2].DagDepth)
}

func TestDagBuilder_ComputeDepths_SharedNode(t *testing.T) {
	// Two DNS entries share the same SLB:
	// dns-1 → cdn-1 → slb-1 → svc-1
	// dns-2 → waf-1 → slb-1 → svc-1
	// slb-1 should have depth = max(2, 2) = 2
	builder := NewDagBuilder()
	nodes := []domain.TopoNode{
		{ID: "dns-1", Type: domain.NodeTypeDNSRecord},
		{ID: "dns-2", Type: domain.NodeTypeDNSRecord},
		{ID: "cdn-1", Type: domain.NodeTypeCDN},
		{ID: "waf-1", Type: domain.NodeTypeWAF},
		{ID: "slb-1", Type: domain.NodeTypeSLB},
		{ID: "svc-1", Type: domain.NodeTypeK8sService},
	}
	edges := []domain.TopoEdge{
		{ID: "e1", SourceID: "dns-1", TargetID: "cdn-1", Status: domain.EdgeStatusActive},
		{ID: "e2", SourceID: "cdn-1", TargetID: "slb-1", Status: domain.EdgeStatusActive},
		{ID: "e3", SourceID: "dns-2", TargetID: "waf-1", Status: domain.EdgeStatusActive},
		{ID: "e4", SourceID: "waf-1", TargetID: "slb-1", Status: domain.EdgeStatusActive},
		{ID: "e5", SourceID: "slb-1", TargetID: "svc-1", Status: domain.EdgeStatusActive},
	}

	builder.ComputeDepths(nodes, edges)

	assert.Equal(t, 0, nodes[0].DagDepth) // dns-1
	assert.Equal(t, 0, nodes[1].DagDepth) // dns-2
	assert.Equal(t, 1, nodes[2].DagDepth) // cdn-1
	assert.Equal(t, 1, nodes[3].DagDepth) // waf-1
	assert.Equal(t, 2, nodes[4].DagDepth) // slb-1 (max of cdn path and waf path)
	assert.Equal(t, 3, nodes[5].DagDepth) // svc-1
}

func TestDagBuilder_ComputeDepths_SkipDNS_DirectToSLB(t *testing.T) {
	// DNS → SLB → ECS (no CDN/WAF, direct A record)
	builder := NewDagBuilder()
	nodes := []domain.TopoNode{
		{ID: "dns-1", Type: domain.NodeTypeDNSRecord},
		{ID: "slb-1", Type: domain.NodeTypeSLB},
		{ID: "ecs-1", Type: domain.NodeTypeECS},
	}
	edges := []domain.TopoEdge{
		{ID: "e1", SourceID: "dns-1", TargetID: "slb-1", Status: domain.EdgeStatusActive},
		{ID: "e2", SourceID: "slb-1", TargetID: "ecs-1", Status: domain.EdgeStatusActive},
	}

	builder.ComputeDepths(nodes, edges)

	assert.Equal(t, 0, nodes[0].DagDepth)
	assert.Equal(t, 1, nodes[1].DagDepth)
	assert.Equal(t, 2, nodes[2].DagDepth)
}

func TestDagBuilder_ComputeDepths_NoDNSEntry(t *testing.T) {
	// No DNS node, should use in-degree=0 nodes as entry
	builder := NewDagBuilder()
	nodes := []domain.TopoNode{
		{ID: "slb-1", Type: domain.NodeTypeSLB},
		{ID: "svc-1", Type: domain.NodeTypeK8sService},
	}
	edges := []domain.TopoEdge{
		{ID: "e1", SourceID: "slb-1", TargetID: "svc-1", Status: domain.EdgeStatusActive},
	}

	builder.ComputeDepths(nodes, edges)

	assert.Equal(t, 0, nodes[0].DagDepth) // slb (in-degree 0)
	assert.Equal(t, 1, nodes[1].DagDepth) // svc
}

func TestDagBuilder_ComputeDepths_EmptyGraph(t *testing.T) {
	builder := NewDagBuilder()
	builder.ComputeDepths(nil, nil)
	// Should not panic
}

func TestDagBuilder_ComputeDepths_PendingEdgesSkipped(t *testing.T) {
	builder := NewDagBuilder()
	nodes := []domain.TopoNode{
		{ID: "dns-1", Type: domain.NodeTypeDNSRecord},
		{ID: "cdn-1", Type: domain.NodeTypeCDN},
		{ID: "unknown-1", Type: domain.NodeTypeUnknown},
	}
	edges := []domain.TopoEdge{
		{ID: "e1", SourceID: "dns-1", TargetID: "cdn-1", Status: domain.EdgeStatusActive},
		{ID: "e2", SourceID: "cdn-1", TargetID: "unknown-1", Status: domain.EdgeStatusPending}, // should be skipped
	}

	builder.ComputeDepths(nodes, edges)

	assert.Equal(t, 0, nodes[0].DagDepth)
	assert.Equal(t, 1, nodes[1].DagDepth)
	assert.Equal(t, 0, nodes[2].DagDepth) // not reached via active edges
}

func TestDagBuilder_DetectBrokenLinks(t *testing.T) {
	builder := NewDagBuilder()

	t.Run("no broken links", func(t *testing.T) {
		nodes := []domain.TopoNode{
			{ID: "slb-1", Type: domain.NodeTypeSLB},
		}
		edges := []domain.TopoEdge{
			{ID: "e1", SourceID: "dns-1", TargetID: "slb-1", Status: domain.EdgeStatusActive},
			{ID: "e2", SourceID: "slb-1", TargetID: "svc-1", Status: domain.EdgeStatusActive},
		}
		assert.Equal(t, 0, builder.DetectBrokenLinks(nodes, edges))
	})

	t.Run("SLB with only inbound", func(t *testing.T) {
		nodes := []domain.TopoNode{
			{ID: "slb-1", Type: domain.NodeTypeSLB},
		}
		edges := []domain.TopoEdge{
			{ID: "e1", SourceID: "dns-1", TargetID: "slb-1", Status: domain.EdgeStatusActive},
			// no outbound from slb-1
		}
		assert.Equal(t, 1, builder.DetectBrokenLinks(nodes, edges))
	})

	t.Run("gateway with only outbound", func(t *testing.T) {
		nodes := []domain.TopoNode{
			{ID: "gw-1", Type: domain.NodeTypeGateway},
		}
		edges := []domain.TopoEdge{
			{ID: "e1", SourceID: "gw-1", TargetID: "svc-1", Status: domain.EdgeStatusActive},
			// no inbound to gw-1
		}
		assert.Equal(t, 1, builder.DetectBrokenLinks(nodes, edges))
	})

	t.Run("pending edges count as broken", func(t *testing.T) {
		nodes := []domain.TopoNode{}
		edges := []domain.TopoEdge{
			{ID: "e1", SourceID: "a", TargetID: "b", Status: domain.EdgeStatusPending},
			{ID: "e2", SourceID: "c", TargetID: "d", Status: domain.EdgeStatusPending},
		}
		assert.Equal(t, 2, builder.DetectBrokenLinks(nodes, edges))
	})

	t.Run("non-bidirectional node not counted", func(t *testing.T) {
		nodes := []domain.TopoNode{
			{ID: "rds-1", Type: domain.NodeTypeRDS}, // RDS is not bidirectional
		}
		edges := []domain.TopoEdge{
			{ID: "e1", SourceID: "dep-1", TargetID: "rds-1", Status: domain.EdgeStatusActive},
			// RDS only has inbound, but it's not a bidirectional type, so not broken
		}
		assert.Equal(t, 0, builder.DetectBrokenLinks(nodes, edges))
	})
}
