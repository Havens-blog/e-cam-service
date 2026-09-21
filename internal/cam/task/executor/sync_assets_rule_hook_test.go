package executor

import (
	"testing"

	"github.com/gotomicro/ego/core/elog"
)

// stubRuleAuto RuleAutoExecutor 桩：记录触发的租户
type stubRuleAuto struct {
	tenantIDs []int64
}

func (s *stubRuleAuto) ExecuteRulesAsync(tenantID int64) {
	s.tenantIDs = append(s.tenantIDs, tenantID)
}

// newHookTestExecutor 构造仅用于挂点测试的执行器（依赖仓储不参与该路径，传 nil）
func newHookTestExecutor() *SyncAssetsExecutor {
	return NewSyncAssetsExecutor(nil, nil, nil, nil, elog.DefaultLogger)
}

// TestTriggerRulesAfterSync 同步完成后规则引擎挂点：nil 关闭、按租户触发、空集合 no-op。
func TestTriggerRulesAfterSync(t *testing.T) {
	t.Run("规则引擎未注入：no-op 不 panic", func(t *testing.T) {
		e := newHookTestExecutor()
		e.triggerRulesAfterSync(map[int64]struct{}{1: {}})
	})

	t.Run("按租户触发且去重", func(t *testing.T) {
		e := newHookTestExecutor()
		stub := &stubRuleAuto{}
		e.SetRuleExecutor(stub)

		e.triggerRulesAfterSync(map[int64]struct{}{1: {}, 2: {}})
		if len(stub.tenantIDs) != 2 {
			t.Fatalf("触发租户数 = %d, want 2", len(stub.tenantIDs))
		}
		seen := map[int64]bool{}
		for _, id := range stub.tenantIDs {
			seen[id] = true
		}
		if !seen[1] || !seen[2] {
			t.Errorf("触发租户集合不符: %v", stub.tenantIDs)
		}
	})

	t.Run("空租户集合：no-op", func(t *testing.T) {
		e := newHookTestExecutor()
		stub := &stubRuleAuto{}
		e.SetRuleExecutor(stub)

		e.triggerRulesAfterSync(map[int64]struct{}{})
		if len(stub.tenantIDs) != 0 {
			t.Errorf("空集合不应触发, got %v", stub.tenantIDs)
		}
	})
}
