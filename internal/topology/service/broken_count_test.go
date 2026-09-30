package service

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/topology/domain"
	"github.com/stretchr/testify/assert"
)

func TestComputeBrokenCount(t *testing.T) {
	t.Run("no broken links", func(t *testing.T) {
		nodes := []domain.TopoNode{{ID: "a"}, {ID: "b"}, {ID: "c"}}
		edges := []domain.TopoEdge{{ID: "e1", SourceID: "a", TargetID: "b"}, {ID: "e2", SourceID: "b", TargetID: "c"}}
		assert.Equal(t, 0, computeBrokenCount(nodes, edges))
	})

	t.Run("target missing counts as broken", func(t *testing.T) {
		nodes := []domain.TopoNode{{ID: "a"}}
		edges := []domain.TopoEdge{{ID: "e1", SourceID: "a", TargetID: "missing", Status: domain.EdgeStatusActive}}
		assert.Equal(t, 1, computeBrokenCount(nodes, edges))
	})

	t.Run("source missing counts as broken", func(t *testing.T) {
		nodes := []domain.TopoNode{{ID: "b"}}
		edges := []domain.TopoEdge{{ID: "e1", SourceID: "missing", TargetID: "b", Status: domain.EdgeStatusActive}}
		assert.Equal(t, 1, computeBrokenCount(nodes, edges))
	})

	t.Run("pending edge counts as broken", func(t *testing.T) {
		nodes := []domain.TopoNode{{ID: "a"}}
		edges := []domain.TopoEdge{{ID: "e1", SourceID: "a", TargetID: "x", Status: domain.EdgeStatusPending}}
		assert.Equal(t, 1, computeBrokenCount(nodes, edges))
	})

	t.Run("both endpoints missing counts once", func(t *testing.T) {
		nodes := []domain.TopoNode{{ID: "a"}}
		edges := []domain.TopoEdge{{ID: "e1", SourceID: "x", TargetID: "y", Status: domain.EdgeStatusActive}}
		assert.Equal(t, 1, computeBrokenCount(nodes, edges))
	})

	t.Run("empty graph is zero", func(t *testing.T) {
		assert.Equal(t, 0, computeBrokenCount(nil, nil))
	})
}

func TestTenantBrokenCount_IgnoresViewFilters(t *testing.T) {
	nodeRepo := newMockNodeRepo()
	nodeRepo.nodes["a"] = domain.TopoNode{ID: "a"}
	nodeRepo.nodes["b"] = domain.TopoNode{ID: "b"}
	edgeRepo := newMockEdgeRepo()
	edgeRepo.edges = []domain.TopoEdge{
		{ID: "e1", SourceID: "a", TargetID: "b", SourceCollector: domain.SourceAPM},
		{ID: "e2", SourceID: "a", TargetID: "ghost", SourceCollector: domain.SourceDNSAPI},
	}
	svc := &topologyService{nodeRepo: nodeRepo, edgeRepo: edgeRepo}
	assert.Equal(t, 1, svc.tenantBrokenCount(context.Background(), 1))
}
