package governance

import "context"

// MockTenantGovernancePort 测试适配器：单元测试经 GetGovernanceFunc 预置
// 治理配置或错误，替代真实 eiam HTTP 调用（跨服务调用不连真实 eiam）。
// GetGovernanceFunc 为 nil 时按"零值配置"（配额 0、白名单空，即无约束）放行，
// 并照常记录调用，便于断言"治理端口被查询过/未被查询过"。
type MockTenantGovernancePort struct {
	// GetGovernanceFunc 预置行为；nil 时返回零值配置
	GetGovernanceFunc func(ctx context.Context, tenantID int64) (int64, []string, error)

	// Calls 记录每次查询的租户 ID（按调用顺序追加）
	Calls []int64
}

// GetGovernance 实现 TenantGovernancePort。
func (m *MockTenantGovernancePort) GetGovernance(ctx context.Context, tenantID int64) (int64, []string, error) {
	m.Calls = append(m.Calls, tenantID)
	if m.GetGovernanceFunc == nil {
		return 0, nil, nil
	}
	return m.GetGovernanceFunc(ctx, tenantID)
}
