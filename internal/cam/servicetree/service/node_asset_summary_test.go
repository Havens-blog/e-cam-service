package service

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/repository"
	cmdbdomain "github.com/Havens-blog/e-cam-service/internal/cmdb/domain"
	cmdbrepo "github.com/Havens-blog/e-cam-service/internal/cmdb/repository"
	"github.com/gotomicro/ego/core/elog"
)

// ---- GetNodeAssetSummary 测试桩：嵌入接口，仅实现被调用方法 ----
// 命名避开同包既有 stubNodeRepo/stubBindingRepo/stubInstanceRepo

type summaryNodeRepo struct {
	repository.NodeRepository
	getByIDFn    func(ctx context.Context, id int64) (domain.ServiceTreeNode, error)
	listByPathFn func(ctx context.Context, tenantID int64, pathPrefix string) ([]domain.ServiceTreeNode, error)
}

func (s *summaryNodeRepo) GetByID(ctx context.Context, id int64) (domain.ServiceTreeNode, error) {
	return s.getByIDFn(ctx, id)
}

func (s *summaryNodeRepo) ListByPath(ctx context.Context, tenantID int64, pathPrefix string) ([]domain.ServiceTreeNode, error) {
	return s.listByPathFn(ctx, tenantID, pathPrefix)
}

type summaryBindingRepo struct {
	repository.BindingRepository
	listByNodeIDsFn func(ctx context.Context, filter domain.NodeIDsBindingFilter) ([]domain.ResourceBinding, error)
	capturedFilter  domain.NodeIDsBindingFilter
}

func (s *summaryBindingRepo) ListByNodeIDs(ctx context.Context, filter domain.NodeIDsBindingFilter) ([]domain.ResourceBinding, error) {
	s.capturedFilter = filter
	return s.listByNodeIDsFn(ctx, filter)
}

type summaryEnvRepo struct {
	repository.EnvironmentRepository
	listFn func(ctx context.Context, filter domain.EnvironmentFilter) ([]domain.Environment, error)
}

func (s *summaryEnvRepo) List(ctx context.Context, filter domain.EnvironmentFilter) ([]domain.Environment, error) {
	return s.listFn(ctx, filter)
}

type summaryCmdbRepo struct {
	cmdbrepo.InstanceRepository
	listByIDsFn func(ctx context.Context, ids []int64) ([]cmdbdomain.Instance, error)
}

func (s *summaryCmdbRepo) ListByIDs(ctx context.Context, ids []int64) ([]cmdbdomain.Instance, error) {
	return s.listByIDsFn(ctx, ids)
}

func newSummaryFixture() (*summaryNodeRepo, *summaryBindingRepo, *summaryEnvRepo, *summaryCmdbRepo) {
	nodeRepo := &summaryNodeRepo{
		getByIDFn: func(ctx context.Context, id int64) (domain.ServiceTreeNode, error) {
			return domain.ServiceTreeNode{ID: 1, TenantID: 1, Path: "/1/"}, nil
		},
		listByPathFn: func(ctx context.Context, tenantID int64, pathPrefix string) ([]domain.ServiceTreeNode, error) {
			// 子树含自身 + 二级 + 三级
			return []domain.ServiceTreeNode{
				{ID: 1, TenantID: 1, Path: "/1/"},
				{ID: 2, TenantID: 1, Path: "/1/2/"},
				{ID: 3, TenantID: 1, Path: "/1/2/3/"},
			}, nil
		},
	}
	bindingRepo := &summaryBindingRepo{
		listByNodeIDsFn: func(ctx context.Context, filter domain.NodeIDsBindingFilter) ([]domain.ResourceBinding, error) {
			return []domain.ResourceBinding{
				{ID: 101, NodeID: 1, EnvID: 11, ResourceType: domain.ResourceTypeInstance, ResourceID: 1001, BindType: domain.BindTypeRule},
				{ID: 102, NodeID: 2, EnvID: 11, ResourceType: domain.ResourceTypeInstance, ResourceID: 1002, BindType: domain.BindTypeManual},
				{ID: 103, NodeID: 3, EnvID: 12, ResourceType: domain.ResourceTypeInstance, ResourceID: 1003, BindType: domain.BindTypeRule},
			}, nil
		},
	}
	envRepo := &summaryEnvRepo{
		listFn: func(ctx context.Context, filter domain.EnvironmentFilter) ([]domain.Environment, error) {
			return []domain.Environment{
				{ID: 11, Code: domain.EnvCodeProd},
				{ID: 12, Code: domain.EnvCodeDev},
			}, nil
		},
	}
	cmdbRepo := &summaryCmdbRepo{
		listByIDsFn: func(ctx context.Context, ids []int64) ([]cmdbdomain.Instance, error) {
			return []cmdbdomain.Instance{
				{ID: 1001, AssetID: "i-prod-1", AssetName: "smt-prod-web-01", ModelUID: "aliyun_ecs",
					Attributes: map[string]any{"provider": "aliyun"}},
				{ID: 1002, AssetID: "i-prod-2", AssetName: "smt-prod-db-01", ModelUID: "cloud_rds",
					Attributes: map[string]any{"provider": "tencent"}},
				{ID: 1003, AssetID: "i-dev-1", AssetName: "smt-dev-redis-01", ModelUID: "volcengine_redis",
					Attributes: map[string]any{"provider": "volcengine"}},
			}, nil
		},
	}
	return nodeRepo, bindingRepo, envRepo, cmdbRepo
}

// TestGetNodeAssetSummaryMultiLevelAggregation 多级子树聚合正确：含自身共 3 节点、3 绑定，
// 按环境/云平台/类型/绑定来源分布与子树节点 ID 集合均正确。
func TestGetNodeAssetSummaryMultiLevelAggregation(t *testing.T) {
	nodeRepo, bindingRepo, envRepo, cmdbRepo := newSummaryFixture()
	s := NewNodeAssetService(bindingRepo, nodeRepo, cmdbRepo, envRepo, elog.DefaultLogger)

	summary, err := s.GetNodeAssetSummary(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("GetNodeAssetSummary() error = %v", err)
	}

	// 子树节点 ID 集合应含自身 + 后代，一次 IN 查询（非 N+1）
	if len(bindingRepo.capturedFilter.NodeIDs) != 3 {
		t.Errorf("NodeIDs = %v, want 长度 3 (含自身)", bindingRepo.capturedFilter.NodeIDs)
	}
	if summary.Total != 3 {
		t.Errorf("Total = %d, want 3", summary.Total)
	}
	if summary.ByEnvironment[domain.EnvCodeProd] != 2 || summary.ByEnvironment[domain.EnvCodeDev] != 1 {
		t.Errorf("ByEnvironment = %v, want prod:2 dev:1", summary.ByEnvironment)
	}
	if summary.ByProvider["aliyun"] != 1 || summary.ByProvider["tencent"] != 1 || summary.ByProvider["volcengine"] != 1 {
		t.Errorf("ByProvider = %v", summary.ByProvider)
	}
	if summary.ByType["ecs"] != 1 || summary.ByType["rds"] != 1 || summary.ByType["redis"] != 1 {
		t.Errorf("ByType = %v", summary.ByType)
	}
	if summary.ByBindType[domain.BindTypeRule] != 2 || summary.ByBindType[domain.BindTypeManual] != 1 {
		t.Errorf("ByBindType = %v, want rule:2 manual:1", summary.ByBindType)
	}
	// 全部命名/环境一致，无疑异
	if len(summary.Suspicious) != 0 {
		t.Errorf("Suspicious = %v, want 空", summary.Suspicious)
	}
}

// TestGetNodeAssetSummaryEmptySubtree 空子树（无绑定）：零值分布 + 非 nil 集合，不报错。
func TestGetNodeAssetSummaryEmptySubtree(t *testing.T) {
	nodeRepo := &summaryNodeRepo{
		getByIDFn: func(ctx context.Context, id int64) (domain.ServiceTreeNode, error) {
			return domain.ServiceTreeNode{ID: 9, TenantID: 1, Path: "/9/"}, nil
		},
		listByPathFn: func(ctx context.Context, tenantID int64, pathPrefix string) ([]domain.ServiceTreeNode, error) {
			return []domain.ServiceTreeNode{{ID: 9, TenantID: 1, Path: "/9/"}}, nil
		},
	}
	bindingRepo := &summaryBindingRepo{
		listByNodeIDsFn: func(ctx context.Context, filter domain.NodeIDsBindingFilter) ([]domain.ResourceBinding, error) {
			return nil, nil
		},
	}
	envRepo := &summaryEnvRepo{listFn: func(ctx context.Context, filter domain.EnvironmentFilter) ([]domain.Environment, error) {
		return nil, nil
	}}
	cmdbRepo := &summaryCmdbRepo{}
	s := NewNodeAssetService(bindingRepo, nodeRepo, cmdbRepo, envRepo, elog.DefaultLogger)

	summary, err := s.GetNodeAssetSummary(context.Background(), 1, 9)
	if err != nil {
		t.Fatalf("GetNodeAssetSummary() error = %v", err)
	}
	if summary.Total != 0 {
		t.Errorf("Total = %d, want 0", summary.Total)
	}
	if summary.ByEnvironment == nil || summary.ByProvider == nil || summary.ByType == nil || summary.ByBindType == nil {
		t.Error("分布 map 不应为 nil（前端可直接遍历）")
	}
	if summary.Suspicious == nil {
		t.Error("Suspicious 不应为 nil")
	}
}

// TestGetNodeAssetSummaryNodeNotFound 节点不存在时报错。
func TestGetNodeAssetSummaryNodeNotFound(t *testing.T) {
	nodeRepo := &summaryNodeRepo{
		getByIDFn: func(ctx context.Context, id int64) (domain.ServiceTreeNode, error) {
			return domain.ServiceTreeNode{}, domain.ErrNodeNotFound
		},
	}
	bindingRepo := &summaryBindingRepo{}
	envRepo := &summaryEnvRepo{}
	cmdbRepo := &summaryCmdbRepo{}
	s := NewNodeAssetService(bindingRepo, nodeRepo, cmdbRepo, envRepo, elog.DefaultLogger)

	if _, err := s.GetNodeAssetSummary(context.Background(), 1, 404); err == nil {
		t.Error("节点不存在应返回错误")
	}
}

// TestGetNodeAssetSummarySuspicious 错绑检测命中：命名模式与 tag.env 双信号，
// 矛盾即入 suspicious（只提示不改绑），一致资产不误报。
func TestGetNodeAssetSummarySuspicious(t *testing.T) {
	nodeRepo := &summaryNodeRepo{
		getByIDFn: func(ctx context.Context, id int64) (domain.ServiceTreeNode, error) {
			return domain.ServiceTreeNode{ID: 1, TenantID: 1, Path: "/1/"}, nil
		},
		listByPathFn: func(ctx context.Context, tenantID int64, pathPrefix string) ([]domain.ServiceTreeNode, error) {
			return []domain.ServiceTreeNode{{ID: 1, TenantID: 1, Path: "/1/"}}, nil
		},
	}
	bindingRepo := &summaryBindingRepo{
		listByNodeIDsFn: func(ctx context.Context, filter domain.NodeIDsBindingFilter) ([]domain.ResourceBinding, error) {
			return []domain.ResourceBinding{
				// 命名 -prod- 绑 dev：命名信号矛盾
				{ID: 1, NodeID: 1, EnvID: 12, ResourceType: domain.ResourceTypeInstance, ResourceID: 2001, BindType: domain.BindTypeManual},
				// tag.env=prod 绑 test：tag 信号矛盾
				{ID: 2, NodeID: 1, EnvID: 13, ResourceType: domain.ResourceTypeInstance, ResourceID: 2002, BindType: domain.BindTypeRule},
				// 命名 -uat- 绑 staging：uat 归一化为 staging，不矛盾
				{ID: 3, NodeID: 1, EnvID: 14, ResourceType: domain.ResourceTypeInstance, ResourceID: 2003, BindType: domain.BindTypeRule},
				// 双信号均为 prod 绑 prod：不矛盾
				{ID: 4, NodeID: 1, EnvID: 11, ResourceType: domain.ResourceTypeInstance, ResourceID: 2004, BindType: domain.BindTypeManual},
				// 无命名信号、无 tag.env：不判定
				{ID: 5, NodeID: 1, EnvID: 12, ResourceType: domain.ResourceTypeInstance, ResourceID: 2005, BindType: domain.BindTypeManual},
			}, nil
		},
	}
	envRepo := &summaryEnvRepo{
		listFn: func(ctx context.Context, filter domain.EnvironmentFilter) ([]domain.Environment, error) {
			return []domain.Environment{
				{ID: 11, Code: domain.EnvCodeProd},
				{ID: 12, Code: domain.EnvCodeDev},
				{ID: 13, Code: domain.EnvCodeTest},
				{ID: 14, Code: domain.EnvCodeStaging},
			}, nil
		},
	}
	cmdbRepo := &summaryCmdbRepo{
		listByIDsFn: func(ctx context.Context, ids []int64) ([]cmdbdomain.Instance, error) {
			return []cmdbdomain.Instance{
				{ID: 2001, AssetID: "i-a", AssetName: "order-prod-app-01", ModelUID: "aliyun_ecs"},
				{ID: 2002, AssetID: "i-b", AssetName: "web-02", ModelUID: "aliyun_ecs",
					Attributes: map[string]any{"tags": map[string]any{"env": "prod"}}},
				{ID: 2003, AssetID: "i-c", AssetName: "order-uat-app-01", ModelUID: "aliyun_ecs"},
				{ID: 2004, AssetID: "i-d", AssetName: "pay-prod-core-01", ModelUID: "aliyun_ecs",
					Attributes: map[string]any{"tags": map[string]any{"env": "prod"}}},
				{ID: 2005, AssetID: "i-e", AssetName: "web-05", ModelUID: "aliyun_ecs"},
			}, nil
		},
	}
	s := NewNodeAssetService(bindingRepo, nodeRepo, cmdbRepo, envRepo, elog.DefaultLogger)

	summary, err := s.GetNodeAssetSummary(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("GetNodeAssetSummary() error = %v", err)
	}
	if summary.Total != 5 {
		t.Errorf("Total = %d, want 5", summary.Total)
	}
	if len(summary.Suspicious) != 2 {
		t.Fatalf("Suspicious 数量 = %d (%v), want 2", len(summary.Suspicious), summary.Suspicious)
	}

	first := summary.Suspicious[0]
	if first.AssetID != "i-a" || first.AssetName != "order-prod-app-01" {
		t.Errorf("suspicious[0] 资产信息错误: %+v", first)
	}
	if first.BoundEnvCode != domain.EnvCodeDev {
		t.Errorf("suspicious[0] 绑定环境 = %q, want dev", first.BoundEnvCode)
	}
	if first.Reason == "" {
		t.Error("suspicious[0] 应含疑异原因")
	}

	second := summary.Suspicious[1]
	if second.AssetID != "i-b" || second.BoundEnvCode != domain.EnvCodeTest {
		t.Errorf("suspicious[1] 资产/绑定环境错误: %+v", second)
	}
	if second.Reason == "" {
		t.Error("suspicious[1] 应含疑异原因")
	}
}

// TestDetectEnvMismatch detectEnvMismatch 纯函数表驱动：双信号逐项覆盖。
func TestDetectEnvMismatch(t *testing.T) {
	tests := []struct {
		name        string
		assetName   string
		tagEnv      string
		boundCode   string
		wantSuspect bool
		wantSignals int // 矛盾信号数
	}{
		{name: "命名 prod 绑 dev 命中", assetName: "app-prod-web-01", boundCode: domain.EnvCodeDev, wantSuspect: true, wantSignals: 1},
		{name: "命名 uat 绑 staging 不命中(uat 归一化)", assetName: "app-uat-web-01", boundCode: domain.EnvCodeStaging, wantSuspect: false},
		{name: "命名 test 绑 prod 命中", assetName: "app-test-web-01", boundCode: domain.EnvCodeProd, wantSuspect: true, wantSignals: 1},
		{name: "命名 dev 绑 dev 不命中", assetName: "app-dev-web-01", boundCode: domain.EnvCodeDev, wantSuspect: false},
		{name: "tag.env prod 绑 test 命中", tagEnv: "prod", boundCode: domain.EnvCodeTest, wantSuspect: true, wantSignals: 1},
		{name: "tag.env uat 绑 staging 不命中", tagEnv: "uat", boundCode: domain.EnvCodeStaging, wantSuspect: false},
		{name: "双信号均矛盾时两个原因", assetName: "app-prod-web-01", tagEnv: "prod", boundCode: domain.EnvCodeDev, wantSuspect: true, wantSignals: 2},
		{name: "双信号一致绑 prod 不命中", assetName: "app-prod-web-01", tagEnv: "prod", boundCode: domain.EnvCodeProd, wantSuspect: false},
		{name: "无信号不判定", assetName: "web-01", boundCode: domain.EnvCodeDev, wantSuspect: false},
		{name: "绑定环境未知不判定", assetName: "app-prod-web-01", boundCode: "", wantSuspect: false},
		{name: "tag.env 非环境值不判定", tagEnv: "gray", boundCode: domain.EnvCodeDev, wantSuspect: false},
		{name: "命名匹配大小写不敏感", assetName: "APP-Prod-Web-01", boundCode: domain.EnvCodeDev, wantSuspect: true, wantSignals: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := detectEnvMismatch(tt.assetName, tt.tagEnv, tt.boundCode)
			if got := len(reasons) > 0; got != tt.wantSuspect {
				t.Errorf("suspect = %v (reasons=%v), want %v", got, reasons, tt.wantSuspect)
			}
			if len(reasons) != tt.wantSignals {
				t.Errorf("矛盾信号数 = %d (%v), want %d", len(reasons), reasons, tt.wantSignals)
			}
		})
	}
}
