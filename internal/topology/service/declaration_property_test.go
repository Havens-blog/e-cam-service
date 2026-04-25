package service

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/topology/domain"
	"github.com/stretchr/testify/assert"
)

// Feature: arms-apm-topology, Property 9: 已有节点来源保护
// Validates: Requirements 4.4
//
// 对任意已存在的 K8s 节点（source_collector 为 "k8s_api"），当收到引用相同节点 ID 的
// APM 声明时，验证节点的 source_collector 保持为 "k8s_api" 不变。

const pbtIterations = 200

// mockNodeRepo implements repository.NodeRepository for testing
type mockNodeRepo struct {
	nodes    map[string]domain.TopoNode
	upserted []domain.TopoNode
}

func newMockNodeRepo() *mockNodeRepo {
	return &mockNodeRepo{
		nodes:    make(map[string]domain.TopoNode),
		upserted: make([]domain.TopoNode, 0),
	}
}

func (m *mockNodeRepo) Upsert(_ context.Context, node domain.TopoNode) error {
	m.nodes[node.ID] = node
	m.upserted = append(m.upserted, node)
	return nil
}

func (m *mockNodeRepo) UpsertMany(_ context.Context, nodes []domain.TopoNode) error {
	for _, n := range nodes {
		m.nodes[n.ID] = n
		m.upserted = append(m.upserted, n)
	}
	return nil
}

func (m *mockNodeRepo) FindByID(_ context.Context, id string) (domain.TopoNode, error) {
	if n, ok := m.nodes[id]; ok {
		return n, nil
	}
	return domain.TopoNode{}, nil
}

func (m *mockNodeRepo) FindByIDs(_ context.Context, ids []string) ([]domain.TopoNode, error) {
	var result []domain.TopoNode
	for _, id := range ids {
		if n, ok := m.nodes[id]; ok {
			result = append(result, n)
		}
	}
	return result, nil
}

func (m *mockNodeRepo) Find(_ context.Context, _ domain.NodeFilter) ([]domain.TopoNode, error) {
	return nil, nil
}

func (m *mockNodeRepo) Count(_ context.Context, _ domain.NodeFilter) (int64, error) {
	return 0, nil
}

func (m *mockNodeRepo) Delete(_ context.Context, id string) error {
	delete(m.nodes, id)
	return nil
}

func (m *mockNodeRepo) DeleteBySource(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

func (m *mockNodeRepo) FindDNSEntries(_ context.Context, _ string) ([]domain.TopoNode, error) {
	return nil, nil
}

func (m *mockNodeRepo) InitIndexes(_ context.Context) error {
	return nil
}

// mockEdgeRepo implements repository.EdgeRepository for testing
type mockEdgeRepo struct {
	edges []domain.TopoEdge
}

func newMockEdgeRepo() *mockEdgeRepo {
	return &mockEdgeRepo{edges: make([]domain.TopoEdge, 0)}
}

func (m *mockEdgeRepo) Upsert(_ context.Context, edge domain.TopoEdge) error {
	m.edges = append(m.edges, edge)
	return nil
}

func (m *mockEdgeRepo) UpsertMany(_ context.Context, edges []domain.TopoEdge) error {
	m.edges = append(m.edges, edges...)
	return nil
}

func (m *mockEdgeRepo) Find(_ context.Context, _ domain.EdgeFilter) ([]domain.TopoEdge, error) {
	return m.edges, nil
}

func (m *mockEdgeRepo) FindBySourceID(_ context.Context, _, _ string) ([]domain.TopoEdge, error) {
	return nil, nil
}

func (m *mockEdgeRepo) FindByTargetID(_ context.Context, _, _ string) ([]domain.TopoEdge, error) {
	return nil, nil
}

func (m *mockEdgeRepo) FindByNodeID(_ context.Context, _, _ string) ([]domain.TopoEdge, error) {
	return nil, nil
}

func (m *mockEdgeRepo) Count(_ context.Context, _ domain.EdgeFilter) (int64, error) {
	return 0, nil
}

func (m *mockEdgeRepo) Delete(_ context.Context, _ string) error {
	return nil
}

func (m *mockEdgeRepo) DeleteBySource(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

func (m *mockEdgeRepo) DeleteByNodeID(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

func (m *mockEdgeRepo) UpdatePendingEdges(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

func (m *mockEdgeRepo) CountPending(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

func (m *mockEdgeRepo) InitIndexes(_ context.Context) error {
	return nil
}

// mockDeclRepo implements repository.DeclarationRepository for testing
type mockDeclRepo struct {
	decls []domain.LinkDeclaration
}

func newMockDeclRepo() *mockDeclRepo {
	return &mockDeclRepo{decls: make([]domain.LinkDeclaration, 0)}
}

func (m *mockDeclRepo) Upsert(_ context.Context, decl domain.LinkDeclaration) error {
	m.decls = append(m.decls, decl)
	return nil
}

func (m *mockDeclRepo) FindBySource(_ context.Context, _, _ string) ([]domain.LinkDeclaration, error) {
	return nil, nil
}

func (m *mockDeclRepo) FindAll(_ context.Context, _ string) ([]domain.LinkDeclaration, error) {
	return m.decls, nil
}

func (m *mockDeclRepo) DeleteBySource(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

func (m *mockDeclRepo) InitIndexes(_ context.Context) error {
	return nil
}

func TestProperty9_ExistingNodeSourceProtection(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	for i := 0; i < pbtIterations; i++ {
		// Setup: create a pre-existing K8s node
		nodeID := fmt.Sprintf("k8s-cluster1-ns1-deploy-%d", rng.Intn(1000))
		existingNode := domain.TopoNode{
			ID:              nodeID,
			Name:            fmt.Sprintf("deploy-%d", rng.Intn(1000)),
			Type:            domain.NodeTypeK8sDeployment,
			Category:        domain.CategoryContainer,
			SourceCollector: domain.SourceK8sAPI,
			TenantID:        "t1",
		}

		nodeRepo := newMockNodeRepo()
		edgeRepo := newMockEdgeRepo()
		declRepo := newMockDeclRepo()

		// Pre-populate the existing K8s node
		nodeRepo.nodes[nodeID] = existingNode

		svc := NewDeclarationService(declRepo, nodeRepo, edgeRepo)

		// Create an APM declaration referencing the same node ID
		targetID := fmt.Sprintf("k8s-cluster1-ns1-svc-%d", rng.Intn(1000))
		// Pre-populate target node so edge won't be pending
		nodeRepo.nodes[targetID] = domain.TopoNode{
			ID:              targetID,
			Name:            "target",
			Type:            domain.NodeTypeK8sDeployment,
			Category:        domain.CategoryContainer,
			SourceCollector: domain.SourceK8sAPI,
			TenantID:        "t1",
		}

		decl := domain.LinkDeclaration{
			Source:    "arms-apm",
			Collector: "api",
			Node: domain.DeclarationNode{
				ID:       nodeID,
				Name:     existingNode.Name,
				Type:     domain.NodeTypeK8sDeployment,
				Category: domain.CategoryContainer,
			},
			Links: []domain.DeclarationLink{
				{
					Target:   targetID,
					Relation: domain.RelationCalls,
				},
			},
			TenantID: "t1",
		}

		err := svc.Register(context.Background(), decl)
		assert.NoError(t, err, "iteration %d: Register should not error", i)

		// Verify: the node's source_collector must still be "k8s_api"
		finalNode := nodeRepo.nodes[nodeID]
		assert.Equal(t, domain.SourceK8sAPI, finalNode.SourceCollector,
			"iteration %d: existing K8s node source_collector should remain k8s_api, got %s",
			i, finalNode.SourceCollector)

		// Verify: edges were still created
		assert.NotEmpty(t, edgeRepo.edges,
			"iteration %d: edges should still be created even when node upsert is skipped", i)
	}
}
