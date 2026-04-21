package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLinkDeclaration_Validate(t *testing.T) {
	validDecl := LinkDeclaration{
		Source:    "custom-gateway",
		Collector: "api",
		Node: DeclarationNode{
			ID: "gw-01", Name: "BizGateway", Type: NodeTypeGateway, Category: CategoryGateway,
		},
		Links: []DeclarationLink{
			{Target: "user-svc", Relation: RelationRoute, Direction: DirectionOutbound},
		},
		TenantID: "t1",
	}

	t.Run("valid declaration", func(t *testing.T) {
		assert.NoError(t, validDecl.Validate())
	})

	t.Run("missing source", func(t *testing.T) {
		d := validDecl
		d.Source = ""
		assert.ErrorContains(t, d.Validate(), "source is required")
	})

	t.Run("invalid collector", func(t *testing.T) {
		d := validDecl
		d.Collector = "invalid"
		assert.ErrorContains(t, d.Validate(), "invalid collector type")
	})

	t.Run("missing tenant_id", func(t *testing.T) {
		d := validDecl
		d.TenantID = ""
		assert.ErrorContains(t, d.Validate(), "tenant_id is required")
	})

	t.Run("missing node.id", func(t *testing.T) {
		d := validDecl
		d.Node.ID = ""
		assert.ErrorContains(t, d.Validate(), "node.id is required")
	})

	t.Run("missing node.name", func(t *testing.T) {
		d := validDecl
		d.Node.Name = ""
		assert.ErrorContains(t, d.Validate(), "node.name is required")
	})

	t.Run("invalid node.type", func(t *testing.T) {
		d := validDecl
		d.Node.Type = "invalid"
		assert.ErrorContains(t, d.Validate(), "invalid node.type")
	})

	t.Run("invalid node.category", func(t *testing.T) {
		d := validDecl
		d.Node.Category = "invalid"
		assert.ErrorContains(t, d.Validate(), "invalid node.category")
	})

	t.Run("missing link target", func(t *testing.T) {
		d := validDecl
		d.Links = []DeclarationLink{{Relation: RelationRoute}}
		assert.ErrorContains(t, d.Validate(), "links[0].target is required")
	})

	t.Run("invalid link relation", func(t *testing.T) {
		d := validDecl
		d.Links = []DeclarationLink{{Target: "svc", Relation: "invalid"}}
		assert.ErrorContains(t, d.Validate(), "links[0].relation is invalid")
	})

	t.Run("invalid link direction", func(t *testing.T) {
		d := validDecl
		d.Links = []DeclarationLink{{Target: "svc", Relation: RelationRoute, Direction: "invalid"}}
		assert.ErrorContains(t, d.Validate(), "links[0].direction is invalid")
	})

	t.Run("empty collector is ok", func(t *testing.T) {
		d := validDecl
		d.Collector = ""
		assert.NoError(t, d.Validate())
	})
}

func TestLinkDeclaration_ToTopoNode(t *testing.T) {
	decl := LinkDeclaration{
		Source: "gw", Collector: "api",
		Node: DeclarationNode{
			ID: "gw-01", Name: "Gateway", Type: NodeTypeGateway,
			Category: CategoryGateway, Provider: ProviderSelfHosted, Region: "cn-hangzhou",
		},
		TenantID: "t1",
	}

	node := decl.ToTopoNode()
	assert.Equal(t, "gw-01", node.ID)
	assert.Equal(t, "Gateway", node.Name)
	assert.Equal(t, NodeTypeGateway, node.Type)
	assert.Equal(t, SourceDeclaration, node.SourceCollector)
	assert.Equal(t, "t1", node.TenantID)
}

func TestLinkDeclaration_ToTopoNode_LogCollector(t *testing.T) {
	decl := LinkDeclaration{
		Source: "log-parser", Collector: "log",
		Node: DeclarationNode{
			ID: "svc-01", Name: "user-svc", Type: NodeTypeK8sService, Category: CategoryContainer,
		},
		TenantID: "t1",
	}

	node := decl.ToTopoNode()
	assert.Equal(t, SourceLog, node.SourceCollector)
}

func TestLinkDeclaration_ToTopoEdges(t *testing.T) {
	decl := LinkDeclaration{
		Source: "gw", Collector: "api",
		Node: DeclarationNode{ID: "gw-01", Name: "Gateway", Type: NodeTypeGateway, Category: CategoryGateway},
		Links: []DeclarationLink{
			{Target: "user-svc", Relation: RelationRoute, Direction: DirectionOutbound},
			{Target: "order-svc", Relation: RelationRoute, Direction: DirectionOutbound},
		},
		TenantID: "t1",
	}

	edges := decl.ToTopoEdges()
	assert.Len(t, edges, 2)
	assert.Equal(t, "gw-01", edges[0].SourceID)
	assert.Equal(t, "user-svc", edges[0].TargetID)
	assert.Equal(t, RelationRoute, edges[0].Relation)
	assert.Equal(t, SourceDeclaration, edges[0].SourceCollector)
	assert.Equal(t, EdgeStatusActive, edges[0].Status)
	assert.Equal(t, "order-svc", edges[1].TargetID)
}
