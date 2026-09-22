package service

import (
	"context"
	"sync"
	"testing"
	"time"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	stdomain "github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
)

// ---- 有状态绑定桩：模拟落库后的绑定集合，用于幂等性验证 ----

type statefulBindingRepo struct {
	stubBindingRepo
	mu       sync.Mutex
	bindings []stdomain.ResourceBinding
}

func (s *statefulBindingRepo) List(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]stdomain.ResourceBinding, len(s.bindings))
	copy(out, s.bindings)
	return out, nil
}

func (s *statefulBindingRepo) CreateBatch(ctx context.Context, bindings []stdomain.ResourceBinding) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createBatchHits++
	s.bindings = append(s.bindings, bindings...)
	return int64(len(bindings)), nil
}

// TestExecuteRulesIdempotentRepeat 幂等单测：同一资产重复执行零重复绑定
// （内存去重对齐 DB 资源级唯一键 tenant_id+resource_type+resource_id）。
func TestExecuteRulesIdempotentRepeat(t *testing.T) {
	rule := stdomain.BindingRule{
		ID: 100, NodeID: 11, EnvID: 5, TenantID: 1, Enabled: true, Name: "web规则",
		Conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "web"}},
	}
	ruleRepo := &stubRuleRepo{
		listEnabledFn: func(ctx context.Context, tenantID int64) ([]stdomain.BindingRule, error) {
			return []stdomain.BindingRule{rule}, nil
		},
	}
	bindingRepo := &statefulBindingRepo{}
	instanceRepo := &stubInstanceRepo{
		listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
			return []camdomain.Instance{
				{ID: 7, AssetName: "web-01"},
				{ID: 8, AssetName: "web-02"},
			}, nil
		},
	}
	s := newTestRuleEngine(ruleRepo, &stubNodeRepo{}, bindingRepo, instanceRepo, nil)

	// Act: 第一轮执行创建绑定
	count1, err := s.ExecuteRules(context.Background(), 1)
	if err != nil {
		t.Fatalf("第一次 ExecuteRules() error = %v", err)
	}
	// 第二轮执行（模拟同步后再次触发 / 手动按钮）
	count2, err := s.ExecuteRules(context.Background(), 1)
	if err != nil {
		t.Fatalf("第二次 ExecuteRules() error = %v", err)
	}
	// 第三轮，进一步锁定
	count3, err := s.ExecuteRules(context.Background(), 1)
	if err != nil {
		t.Fatalf("第三次 ExecuteRules() error = %v", err)
	}

	// Assert
	if count1 != 2 {
		t.Errorf("第一次新建绑定 = %d, want 2", count1)
	}
	if count2 != 0 || count3 != 0 {
		t.Errorf("重复执行应零新建绑定, got %d / %d", count2, count3)
	}
	if bindingRepo.createBatchHits != 1 {
		t.Errorf("CreateBatch 调用次数 = %d, want 1（重复执行不落重复绑定）", bindingRepo.createBatchHits)
	}
	if len(bindingRepo.bindings) != 2 {
		t.Errorf("绑定总数 = %d, want 2", len(bindingRepo.bindings))
	}
}

// TestExecuteRulesManualBindingPriority 手动绑定优先单测：已手动绑定资产规则执行跳过（locked 语义）。
// 资源级 locked：手动绑定在任意环境均阻断规则绑定（对齐 DB 资源级唯一键）。
func TestExecuteRulesManualBindingPriority(t *testing.T) {
	rule := stdomain.BindingRule{
		ID: 100, NodeID: 11, EnvID: 5, TenantID: 1, Enabled: true, Name: "web规则",
		Conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "web"}},
	}
	tests := []struct {
		name             string
		existingBindings []stdomain.ResourceBinding
		wantNewBindings  int64
		wantCreateBatch  bool
	}{
		{
			name: "手动绑定在同环境：规则跳过",
			existingBindings: []stdomain.ResourceBinding{
				{EnvID: 5, ResourceID: 7, ResourceType: stdomain.ResourceTypeInstance, BindType: stdomain.BindTypeManual},
			},
		},
		{
			name: "手动绑定在异环境：同样跳过（资源级 locked）",
			existingBindings: []stdomain.ResourceBinding{
				{EnvID: 8, ResourceID: 7, ResourceType: stdomain.ResourceTypeInstance, BindType: stdomain.BindTypeManual},
			},
		},
		{
			name: "规则已绑定的资产：不重复绑定",
			existingBindings: []stdomain.ResourceBinding{
				{EnvID: 8, ResourceID: 7, ResourceType: stdomain.ResourceTypeInstance, BindType: stdomain.BindTypeRule, RuleID: 99},
			},
		},
		{
			name:             "未绑定资产：正常创建规则绑定（对照组）",
			existingBindings: nil,
			wantNewBindings:  1,
			wantCreateBatch:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			ruleRepo := &stubRuleRepo{
				listEnabledFn: func(ctx context.Context, tenantID int64) ([]stdomain.BindingRule, error) {
					return []stdomain.BindingRule{rule}, nil
				},
			}
			bindingRepo := &stubBindingRepo{
				listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
					return tt.existingBindings, nil
				},
				createBatchFn: func(ctx context.Context, bindings []stdomain.ResourceBinding) (int64, error) {
					return int64(len(bindings)), nil
				},
			}
			instanceRepo := &stubInstanceRepo{
				listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
					return []camdomain.Instance{{ID: 7, AssetName: "web-01"}}, nil
				},
			}
			s := newTestRuleEngine(ruleRepo, &stubNodeRepo{}, bindingRepo, instanceRepo, nil)

			// Act
			count, err := s.ExecuteRules(context.Background(), 1)
			if err != nil {
				t.Fatalf("ExecuteRules() error = %v", err)
			}

			// Assert
			if count != tt.wantNewBindings {
				t.Errorf("新建绑定 = %d, want %d", count, tt.wantNewBindings)
			}
			if (bindingRepo.createBatchHits > 0) != tt.wantCreateBatch {
				t.Errorf("CreateBatch 调用 = %d, wantCreateBatch %v", bindingRepo.createBatchHits, tt.wantCreateBatch)
			}
		})
	}
}

// TestExecuteRulesAsync 异步挂点：不阻塞调用方、完成后落地绑定、panic 恢复、同租户 in-flight 去重。
func TestExecuteRulesAsync(t *testing.T) {
	t.Run("异步执行完成且不阻塞调用方", func(t *testing.T) {
		rule := stdomain.BindingRule{
			ID: 100, NodeID: 11, EnvID: 5, TenantID: 1, Enabled: true,
			Conditions: []stdomain.RuleCondition{{Field: "name", Operator: stdomain.OperatorContains, Value: "web"}},
		}
		ruleRepo := &stubRuleRepo{
			listEnabledFn: func(ctx context.Context, tenantID int64) ([]stdomain.BindingRule, error) {
				return []stdomain.BindingRule{rule}, nil
			},
		}
		done := make(chan struct{})
		bindingRepo := &stubBindingRepo{
			listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
				return nil, nil
			},
			createBatchFn: func(ctx context.Context, bindings []stdomain.ResourceBinding) (int64, error) {
				close(done)
				return int64(len(bindings)), nil
			},
		}
		instanceRepo := &stubInstanceRepo{
			listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
				return []camdomain.Instance{{ID: 7, AssetName: "web-01"}}, nil
			},
		}
		s := newTestRuleEngine(ruleRepo, &stubNodeRepo{}, bindingRepo, instanceRepo, nil)

		start := time.Now()
		s.ExecuteRulesAsync(1) // 应立即返回，不阻塞
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("ExecuteRulesAsync 阻塞了调用方: %v", elapsed)
		}
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("异步规则执行未完成")
		}
	})

	t.Run("panic 被恢复不崩溃", func(t *testing.T) {
		ruleRepo := &stubRuleRepo{
			listEnabledFn: func(ctx context.Context, tenantID int64) ([]stdomain.BindingRule, error) {
				panic("模拟规则执行 panic")
			},
		}
		s := newTestRuleEngine(ruleRepo, &stubNodeRepo{}, &stubBindingRepo{}, &stubInstanceRepo{}, nil)

		s.ExecuteRulesAsync(1)
		waitInflightClear(t, s, 3*time.Second) // panic 恢复后 in-flight 标记应被清理
	})

	t.Run("同租户执行中跳过重复触发", func(t *testing.T) {
		var mu sync.Mutex
		started := make(chan struct{})
		release := make(chan struct{})
		calls := 0
		ruleRepo := &stubRuleRepo{
			listEnabledFn: func(ctx context.Context, tenantID int64) ([]stdomain.BindingRule, error) {
				mu.Lock()
				calls++
				mu.Unlock()
				if calls == 1 {
					close(started)
					<-release // 阻塞第一次执行，制造 in-flight 窗口
				}
				return nil, nil
			},
		}
		bindingRepo := &stubBindingRepo{
			listFn: func(ctx context.Context, filter stdomain.BindingFilter) ([]stdomain.ResourceBinding, error) {
				return nil, nil
			},
		}
		instanceRepo := &stubInstanceRepo{
			listFn: func(ctx context.Context, filter camdomain.InstanceFilter) ([]camdomain.Instance, error) {
				return nil, nil
			},
		}
		s := newTestRuleEngine(ruleRepo, &stubNodeRepo{}, bindingRepo, instanceRepo, nil)

		s.ExecuteRulesAsync(1)
		<-started
		s.ExecuteRulesAsync(1) // in-flight，应被跳过
		close(release)
		waitInflightClear(t, s, 3*time.Second)

		mu.Lock()
		defer mu.Unlock()
		if calls != 1 {
			t.Errorf("ExecuteRules 实际执行次数 = %d, want 1（重复触发应跳过）", calls)
		}
	})
	t.Run("执行失败仅记录不向上传播", func(t *testing.T) {
		ruleRepo := &stubRuleRepo{
			listEnabledFn: func(ctx context.Context, tenantID int64) ([]stdomain.BindingRule, error) {
				return nil, context.DeadlineExceeded
			},
		}
		s := newTestRuleEngine(ruleRepo, &stubNodeRepo{}, &stubBindingRepo{}, &stubInstanceRepo{}, nil)

		s.ExecuteRulesAsync(1) // 失败不应 panic / 不影响调用方
		waitInflightClear(t, s, 3*time.Second)
	})
}

// waitInflightClear 轮询等待异步执行的 in-flight 标记清理
func waitInflightClear(t *testing.T, s RuleEngineService, timeout time.Duration) {
	t.Helper()
	impl, ok := s.(*ruleEngineService)
	if !ok {
		t.Fatalf("unexpected implementation %T", s)
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		empty := true
		impl.asyncRunning.Range(func(_, _ any) bool {
			empty = false
			return false
		})
		if empty {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("in-flight 标记未被清理（异步执行未结束/未恢复）")
}
