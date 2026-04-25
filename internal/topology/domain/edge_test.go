package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTopoEdge_Validate(t *testing.T) {
	tests := []struct {
		name    string
		edge    TopoEdge
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid edge",
			edge: TopoEdge{
				ID: "e1", SourceID: "n1", TargetID: "n2",
				Relation: RelationRoute, Direction: DirectionOutbound,
				TenantID: "t1",
			},
			wantErr: false,
		},
		{
			name:    "missing id",
			edge:    TopoEdge{SourceID: "n1", TargetID: "n2", Relation: RelationRoute, TenantID: "t1"},
			wantErr: true, errMsg: "edge id is required",
		},
		{
			name:    "missing source_id",
			edge:    TopoEdge{ID: "e1", TargetID: "n2", Relation: RelationRoute, TenantID: "t1"},
			wantErr: true, errMsg: "source_id is required",
		},
		{
			name:    "missing target_id",
			edge:    TopoEdge{ID: "e1", SourceID: "n1", Relation: RelationRoute, TenantID: "t1"},
			wantErr: true, errMsg: "target_id is required",
		},
		{
			name:    "invalid relation",
			edge:    TopoEdge{ID: "e1", SourceID: "n1", TargetID: "n2", Relation: "invalid", TenantID: "t1"},
			wantErr: true, errMsg: "invalid relation: invalid",
		},
		{
			name:    "invalid direction",
			edge:    TopoEdge{ID: "e1", SourceID: "n1", TargetID: "n2", Relation: RelationRoute, Direction: "invalid", TenantID: "t1"},
			wantErr: true, errMsg: "invalid direction: invalid",
		},
		{
			name:    "missing tenant_id",
			edge:    TopoEdge{ID: "e1", SourceID: "n1", TargetID: "n2", Relation: RelationRoute},
			wantErr: true, errMsg: "tenant_id is required",
		},
		{
			name: "empty direction is ok",
			edge: TopoEdge{
				ID: "e1", SourceID: "n1", TargetID: "n2",
				Relation: RelationBelongsTo, TenantID: "t1",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.edge.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTopoEdge_Validate_DefaultStatus(t *testing.T) {
	edge := TopoEdge{
		ID: "e1", SourceID: "n1", TargetID: "n2",
		Relation: RelationRoute, TenantID: "t1",
	}
	err := edge.Validate()
	assert.NoError(t, err)
	assert.Equal(t, EdgeStatusActive, edge.Status)
}

func TestTopoEdge_IsPending(t *testing.T) {
	assert.True(t, (&TopoEdge{Status: EdgeStatusPending}).IsPending())
	assert.False(t, (&TopoEdge{Status: EdgeStatusActive}).IsPending())
}

func TestTopoEdge_IsSilent(t *testing.T) {
	threshold := 24 * time.Hour

	// No last_seen_at → not silent
	assert.False(t, (&TopoEdge{}).IsSilent(threshold))

	// Recent → not silent
	recent := time.Now().Add(-1 * time.Hour)
	assert.False(t, (&TopoEdge{LastSeenAt: &recent}).IsSilent(threshold))

	// Old → silent
	old := time.Now().Add(-48 * time.Hour)
	assert.True(t, (&TopoEdge{LastSeenAt: &old}).IsSilent(threshold))
}

func TestTopoEdge_IsFromLog(t *testing.T) {
	assert.True(t, (&TopoEdge{SourceCollector: SourceLog}).IsFromLog())
	assert.False(t, (&TopoEdge{SourceCollector: SourceCloudAPI}).IsFromLog())
}

func TestValidRelations_ContainsCalls(t *testing.T) {
	assert.True(t, ValidRelations[RelationCalls], "ValidRelations should contain 'calls'")
	assert.Equal(t, "calls", RelationCalls)
}

func TestTopoEdge_Validate_CallsRelation(t *testing.T) {
	edge := TopoEdge{
		ID: "e-apm-1", SourceID: "k8s-cluster1-ns1-svc-a", TargetID: "k8s-cluster1-ns1-svc-b",
		Relation: RelationCalls, Direction: DirectionOutbound,
		SourceCollector: SourceAPM, TenantID: "t1",
	}
	err := edge.Validate()
	assert.NoError(t, err, "TopoEdge with relation 'calls' should pass validation")
}
