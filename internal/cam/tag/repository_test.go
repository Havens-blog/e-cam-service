package tag

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTagDAO 用于测试的 TagDAO mock
type mockTagDAO struct {
	insertPolicyFn  func(ctx context.Context, policy TagPolicy) (int64, error)
	updatePolicyFn  func(ctx context.Context, policy TagPolicy) error
	deletePolicyFn  func(ctx context.Context, id int64) error
	getPolicyByIDFn func(ctx context.Context, id int64) (TagPolicy, error)
	listPoliciesFn  func(ctx context.Context, filter PolicyFilter) ([]TagPolicy, int64, error)
}

func (m *mockTagDAO) InsertPolicy(ctx context.Context, policy TagPolicy) (int64, error) {
	if m.insertPolicyFn != nil {
		return m.insertPolicyFn(ctx, policy)
	}
	return 1, nil
}

func (m *mockTagDAO) UpdatePolicy(ctx context.Context, policy TagPolicy) error {
	if m.updatePolicyFn != nil {
		return m.updatePolicyFn(ctx, policy)
	}
	return nil
}

func (m *mockTagDAO) DeletePolicy(ctx context.Context, id int64) error {
	if m.deletePolicyFn != nil {
		return m.deletePolicyFn(ctx, id)
	}
	return nil
}

func (m *mockTagDAO) GetPolicyByID(ctx context.Context, id int64) (TagPolicy, error) {
	if m.getPolicyByIDFn != nil {
		return m.getPolicyByIDFn(ctx, id)
	}
	return TagPolicy{}, nil
}

func (m *mockTagDAO) ListPolicies(ctx context.Context, filter PolicyFilter) ([]TagPolicy, int64, error) {
	if m.listPoliciesFn != nil {
		return m.listPoliciesFn(ctx, filter)
	}
	return nil, 0, nil
}

// Rule stubs
func (m *mockTagDAO) InsertRule(_ context.Context, _ TagRule) (int64, error)  { return 1, nil }
func (m *mockTagDAO) UpdateRule(_ context.Context, _ TagRule) error           { return nil }
func (m *mockTagDAO) DeleteRule(_ context.Context, _ int64) error             { return nil }
func (m *mockTagDAO) GetRuleByID(_ context.Context, _ int64) (TagRule, error) { return TagRule{}, nil }
func (m *mockTagDAO) ListRules(_ context.Context, _ RuleFilter) ([]TagRule, int64, error) {
	return nil, 0, nil
}
func (m *mockTagDAO) ListEnabledRules(_ context.Context, _ string) ([]TagRule, error) {
	return nil, nil
}

// ==================== TagDAO 接口契约测试 ====================

func TestMockTagDAO_InsertPolicy(t *testing.T) {
	dao := &mockTagDAO{
		insertPolicyFn: func(_ context.Context, policy TagPolicy) (int64, error) {
			assert.Equal(t, "基础标签规范", policy.Name)
			assert.Equal(t, "tenant1", policy.TenantID)
			assert.Equal(t, []string{"env", "team"}, policy.RequiredKeys)
			return 100, nil
		},
	}

	id, err := dao.InsertPolicy(context.Background(), TagPolicy{
		Name:         "基础标签规范",
		TenantID:     "tenant1",
		RequiredKeys: []string{"env", "team"},
		Status:       "enabled",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(100), id)
}

func TestMockTagDAO_InsertPolicy_Error(t *testing.T) {
	dao := &mockTagDAO{
		insertPolicyFn: func(_ context.Context, _ TagPolicy) (int64, error) {
			return 0, errors.New("duplicate key")
		},
	}

	_, err := dao.InsertPolicy(context.Background(), TagPolicy{Name: "test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate key")
}

func TestMockTagDAO_UpdatePolicy(t *testing.T) {
	var captured TagPolicy
	dao := &mockTagDAO{
		updatePolicyFn: func(_ context.Context, policy TagPolicy) error {
			captured = policy
			return nil
		},
	}

	err := dao.UpdatePolicy(context.Background(), TagPolicy{
		ID:           100,
		Name:         "更新后的策略",
		TenantID:     "tenant1",
		RequiredKeys: []string{"env", "team", "project"},
		Status:       "enabled",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(100), captured.ID)
	assert.Equal(t, "更新后的策略", captured.Name)
	assert.Equal(t, []string{"env", "team", "project"}, captured.RequiredKeys)
}

func TestMockTagDAO_UpdatePolicy_NotFound(t *testing.T) {
	dao := &mockTagDAO{
		updatePolicyFn: func(_ context.Context, _ TagPolicy) error {
			return errors.New("no documents")
		},
	}

	err := dao.UpdatePolicy(context.Background(), TagPolicy{ID: 999, TenantID: "tenant1"})
	assert.Error(t, err)
}

func TestMockTagDAO_DeletePolicy(t *testing.T) {
	deleted := false
	dao := &mockTagDAO{
		deletePolicyFn: func(_ context.Context, id int64) error {
			deleted = true
			assert.Equal(t, int64(100), id)
			return nil
		},
	}

	err := dao.DeletePolicy(context.Background(), 100)
	require.NoError(t, err)
	assert.True(t, deleted)
}

func TestMockTagDAO_DeletePolicy_Error(t *testing.T) {
	dao := &mockTagDAO{
		deletePolicyFn: func(_ context.Context, _ int64) error {
			return errors.New("delete failed")
		},
	}

	err := dao.DeletePolicy(context.Background(), 100)
	assert.Error(t, err)
}

func TestMockTagDAO_GetPolicyByID(t *testing.T) {
	dao := &mockTagDAO{
		getPolicyByIDFn: func(_ context.Context, id int64) (TagPolicy, error) {
			return TagPolicy{
				ID:           id,
				Name:         "基础标签规范",
				TenantID:     "tenant1",
				RequiredKeys: []string{"env"},
				Status:       "enabled",
			}, nil
		},
	}

	policy, err := dao.GetPolicyByID(context.Background(), 100)
	require.NoError(t, err)
	assert.Equal(t, int64(100), policy.ID)
	assert.Equal(t, "基础标签规范", policy.Name)
	assert.Equal(t, "tenant1", policy.TenantID)
}

func TestMockTagDAO_GetPolicyByID_NotFound(t *testing.T) {
	dao := &mockTagDAO{
		getPolicyByIDFn: func(_ context.Context, _ int64) (TagPolicy, error) {
			return TagPolicy{}, errors.New("no documents")
		},
	}

	_, err := dao.GetPolicyByID(context.Background(), 999)
	assert.Error(t, err)
}

func TestMockTagDAO_ListPolicies(t *testing.T) {
	dao := &mockTagDAO{
		listPoliciesFn: func(_ context.Context, filter PolicyFilter) ([]TagPolicy, int64, error) {
			assert.Equal(t, "tenant1", filter.TenantID)
			assert.Equal(t, int64(0), filter.Offset)
			assert.Equal(t, int64(20), filter.Limit)
			return []TagPolicy{
				{ID: 1, Name: "策略A", TenantID: "tenant1"},
				{ID: 2, Name: "策略B", TenantID: "tenant1"},
			}, 2, nil
		},
	}

	policies, total, err := dao.ListPolicies(context.Background(), PolicyFilter{
		TenantID: "tenant1",
		Offset:   0,
		Limit:    20,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, policies, 2)
	assert.Equal(t, "策略A", policies[0].Name)
	assert.Equal(t, "策略B", policies[1].Name)
}

func TestMockTagDAO_ListPolicies_Empty(t *testing.T) {
	dao := &mockTagDAO{
		listPoliciesFn: func(_ context.Context, _ PolicyFilter) ([]TagPolicy, int64, error) {
			return nil, 0, nil
		},
	}

	policies, total, err := dao.ListPolicies(context.Background(), PolicyFilter{
		TenantID: "tenant1",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Empty(t, policies)
}

func TestMockTagDAO_ListPolicies_Error(t *testing.T) {
	dao := &mockTagDAO{
		listPoliciesFn: func(_ context.Context, _ PolicyFilter) ([]TagPolicy, int64, error) {
			return nil, 0, errors.New("db error")
		},
	}

	_, _, err := dao.ListPolicies(context.Background(), PolicyFilter{TenantID: "tenant1"})
	assert.Error(t, err)
}

func TestMockTagDAO_DefaultBehavior(t *testing.T) {
	dao := &mockTagDAO{}

	id, err := dao.InsertPolicy(context.Background(), TagPolicy{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), id)

	err = dao.UpdatePolicy(context.Background(), TagPolicy{})
	require.NoError(t, err)

	err = dao.DeletePolicy(context.Background(), 1)
	require.NoError(t, err)

	policy, err := dao.GetPolicyByID(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, TagPolicy{}, policy)

	policies, total, err := dao.ListPolicies(context.Background(), PolicyFilter{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Nil(t, policies)
}

func TestTagPolicyCollection_Constant(t *testing.T) {
	assert.Equal(t, "c_tag_policy", TagPolicyCollection)
}

func TestMockTagDAO_InsertPolicy_WithConstraints(t *testing.T) {
	dao := &mockTagDAO{
		insertPolicyFn: func(_ context.Context, policy TagPolicy) (int64, error) {
			assert.Equal(t, map[string][]string{
				"env": {"production", "staging", "development"},
			}, policy.KeyValueConstraints)
			assert.Equal(t, []string{"ecs", "rds"}, policy.ResourceTypes)
			return 200, nil
		},
	}

	id, err := dao.InsertPolicy(context.Background(), TagPolicy{
		Name:         "完整策略",
		TenantID:     "tenant1",
		RequiredKeys: []string{"env"},
		KeyValueConstraints: map[string][]string{
			"env": {"production", "staging", "development"},
		},
		ResourceTypes: []string{"ecs", "rds"},
		Status:        "enabled",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(200), id)
}

func TestMockTagDAO_UpdatePolicy_AllFields(t *testing.T) {
	var captured TagPolicy
	dao := &mockTagDAO{
		updatePolicyFn: func(_ context.Context, policy TagPolicy) error {
			captured = policy
			return nil
		},
	}

	err := dao.UpdatePolicy(context.Background(), TagPolicy{
		ID:           100,
		Name:         "更新策略",
		Description:  "更新描述",
		TenantID:     "tenant1",
		RequiredKeys: []string{"env", "team"},
		KeyValueConstraints: map[string][]string{
			"env": {"prod", "dev"},
		},
		ResourceTypes: []string{"ecs"},
		Status:        "disabled",
	})
	require.NoError(t, err)
	assert.Equal(t, "更新策略", captured.Name)
	assert.Equal(t, "更新描述", captured.Description)
	assert.Equal(t, "disabled", captured.Status)
	assert.Equal(t, []string{"ecs"}, captured.ResourceTypes)
}
