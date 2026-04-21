package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTopoNode_Validate(t *testing.T) {
	tests := []struct {
		name    string
		node    TopoNode
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid node",
			node: TopoNode{
				ID: "dns-api.example.com", Name: "api.example.com",
				Type: NodeTypeDNSRecord, Category: CategoryDNS,
				Provider: ProviderAliyun, SourceCollector: SourceDNSAPI,
				TenantID: "t1",
			},
			wantErr: false,
		},
		{
			name:    "missing id",
			node:    TopoNode{Name: "test", Type: NodeTypeCDN, Category: CategoryNetwork, TenantID: "t1"},
			wantErr: true, errMsg: "node id is required",
		},
		{
			name:    "missing name",
			node:    TopoNode{ID: "n1", Type: NodeTypeCDN, Category: CategoryNetwork, TenantID: "t1"},
			wantErr: true, errMsg: "node name is required",
		},
		{
			name:    "invalid type",
			node:    TopoNode{ID: "n1", Name: "test", Type: "invalid", Category: CategoryNetwork, TenantID: "t1"},
			wantErr: true, errMsg: "invalid node type: invalid",
		},
		{
			name:    "invalid category",
			node:    TopoNode{ID: "n1", Name: "test", Type: NodeTypeCDN, Category: "invalid", TenantID: "t1"},
			wantErr: true, errMsg: "invalid category: invalid",
		},
		{
			name:    "invalid provider",
			node:    TopoNode{ID: "n1", Name: "test", Type: NodeTypeCDN, Category: CategoryNetwork, Provider: "invalid", TenantID: "t1"},
			wantErr: true, errMsg: "invalid provider: invalid",
		},
		{
			name:    "invalid source_collector",
			node:    TopoNode{ID: "n1", Name: "test", Type: NodeTypeCDN, Category: CategoryNetwork, SourceCollector: "invalid", TenantID: "t1"},
			wantErr: true, errMsg: "invalid source_collector: invalid",
		},
		{
			name:    "missing tenant_id",
			node:    TopoNode{ID: "n1", Name: "test", Type: NodeTypeCDN, Category: CategoryNetwork},
			wantErr: true, errMsg: "tenant_id is required",
		},
		{
			name:    "empty provider is ok",
			node:    TopoNode{ID: "n1", Name: "test", Type: NodeTypeCDN, Category: CategoryNetwork, TenantID: "t1"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.node.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTopoNode_IsBidirectional(t *testing.T) {
	assert.True(t, (&TopoNode{Type: NodeTypeSLB}).IsBidirectional())
	assert.True(t, (&TopoNode{Type: NodeTypeGateway}).IsBidirectional())
	assert.True(t, (&TopoNode{Type: NodeTypeWAF}).IsBidirectional())
	assert.True(t, (&TopoNode{Type: NodeTypeCDN}).IsBidirectional())
	assert.False(t, (&TopoNode{Type: NodeTypeECS}).IsBidirectional())
	assert.False(t, (&TopoNode{Type: NodeTypeRDS}).IsBidirectional())
	assert.False(t, (&TopoNode{Type: NodeTypeDNSRecord}).IsBidirectional())
}

func TestTopoNode_IsDNSEntry(t *testing.T) {
	assert.True(t, (&TopoNode{Type: NodeTypeDNSRecord}).IsDNSEntry())
	assert.False(t, (&TopoNode{Type: NodeTypeCDN}).IsDNSEntry())
}
