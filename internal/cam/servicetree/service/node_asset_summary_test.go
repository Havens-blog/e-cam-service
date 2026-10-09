package service

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/port"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/repository"
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

// summaryCmdbPort 实现 port.CMDBPort 接口的测试桩
type summaryCmdbPort struct {
	listByIDsFn func(ctx context.Context, ids []int64) ([]port.CMDBInstance, error)
}

func (s *summaryCmdbPort) ListByIDs(ctx context.Context, ids []int64) ([]port.CMDBInstance, error) {
	return s.listByIDsFn(ctx, ids)
}

func (s *summaryCmdbPort) ListUnbound(ctx context.Context, tenantID int64, offset, limit int64) ([]port.CMDBInstance, error) {
	return nil, nil
}

func (s *summaryCmdbPort) CountUnbound(ctx context.Context, tenantID int64) (int64, error) {
	return 0, nil
}

func (s *summaryCmdbPort) AggregateStatsByIDs(ctx context.Context, ids []int64) (*port.AssetStatsResult, error) {
	return &port.AssetStatsResult{}, nil
}

func (s *summaryCmdbPort) AggregateAllStats(ctx context.Context, tenantID int64) (*port.AssetStatsResult, error) {
	return &port.AssetStatsResult{}, nil
}

func (s *summaryCmdbPort) AggregateUnboundStats(ctx context.Context, tenantID int64) (*port.AssetStatsResult, error) {
	return &port.AssetStatsResult{}, nil
}

func newSummaryFixture() (*summaryNodeRepo, *summaryBindingRepo, *summaryEnvRepo, *summaryCmdbPort) {
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
	cmdbPort := &summaryCmdbPort{
		listByIDsFn: func(ctx context.Context, ids []int64) ([]port.CMDBInstance, error) {
			return []port.CMDBInstance{
				{ID: 1001, AssetID: "i-prod-1", AssetName: "smt-prod-web-01", ModelUID: "aliyun_ecs",
					Attributes: map[string]any{"provider": "aliyun"}},
				{ID: 1002, AssetID: "i-prod-2", AssetName: "smt-prod-db-01", ModelUID: "cloud_rds",
					Attributes: map[string]any{"provider": "tencent"}},
				{ID: 1003, AssetID: "i-dev-1", AssetName: "smt-dev-redis-01", ModelUID: "volcengine_redis",
					Attributes: map[string]any{"provider": "volcengine"}},
			}, nil
		},
	}
	return nodeRepo, bindingRepo, envRepo, cmdbPort
}

// TestGetNodeAssetSummaryMultiLevelAggregation 多级子树聚合正确：含自身共 3 节点、3 绑定，
// 按环境/云平台/类型/绑定来源分布与子树节点 ID 集合均正确。
func TestGetNodeAssetSummaryMultiLevelAggregation(t *testing.T) {
	nodeRepo, bindingRepo, envRepo, cmdbPort := newSummaryFixture()
	s := NewNodeAssetService(bindingRepo, nodeRepo, cmdbPort, envRepo, elog.DefaultLogger)

	summary, err := s.GetNodeAssetSummary(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("GetNodeAssetSummary() error = %v", err)
	}

	// 子树节点 ID 集合应含自身 + 后代，一次 IN 查询（非 N+1）
	if len(bindingRepo.capturedFilter.NodeIDs) != 3 {
		t.Errorf("NodeIDs = %v, want 长度 3 (含自身)", bindingRepo.capturedFilter.NodeIDs)
	}

	// 总计
	if summary.Total != 3 {
		t.Errorf("Total = %d, want 3", summary.Total)
	}

	// 环境分布（prod/dev）
	if summary.ByEnvironment[domain.EnvCodeProd] != 2 {
		t.Errorf("ByEnvironment[prod] = %d, want 2", summary.ByEnvironment[domain.EnvCodeProd])
	}
	if summary.ByEnvironment[domain.EnvCodeDev] != 1 {
		t.Errorf("ByEnvironment[dev] = %d, want 1", summary.ByEnvironment[domain.EnvCodeDev])
	}

	// 类型分布（ecs/rds/redis）
	if summary.ByType["ecs"] != 1 {
		t.Errorf("ByType[ecs] = %d, want 1", summary.ByType["ecs"])
	}
	if summary.ByType["rds"] != 1 {
		t.Errorf("ByType[rds] = %d, want 1", summary.ByType["rds"])
	}
	if summary.ByType["redis"] != 1 {
		t.Errorf("ByType[redis] = %d, want 1", summary.ByType["redis"])
	}

	// 绑定来源（rule/manual）
	if summary.ByBindType[domain.BindTypeRule] != 2 {
		t.Errorf("ByBindType[rule] = %d, want 2", summary.ByBindType[domain.BindTypeRule])
	}
	if summary.ByBindType[domain.BindTypeManual] != 1 {
		t.Errorf("ByBindType[manual] = %d, want 1", summary.ByBindType[domain.BindTypeManual])
	}
}

// TestGetNodeAssetSummaryEnvMismatchDetection 环境错绑双信号检测
func TestGetNodeAssetSummaryEnvMismatchDetection(t *testing.T) {
	nodeRepo, bindingRepo, envRepo, _ := newSummaryFixture()
	// 注入命名或 tag 与绑定环境矛盾的实例
	cmdbPort := &summaryCmdbPort{
		listByIDsFn: func(ctx context.Context, ids []int64) ([]port.CMDBInstance, error) {
			return []port.CMDBInstance{
				// 命名 -dev- 但绑定 prod：命名信号矛盾
				{ID: 1001, AssetID: "i-1", AssetName: "smt-dev-web-01", ModelUID: "aliyun_ecs",
					Attributes: map[string]any{"provider": "aliyun"}},
				// tag.env=test 但绑定 prod：tag 信号矛盾
				{ID: 1002, AssetID: "i-2", AssetName: "neutral", ModelUID: "cloud_rds",
					Attributes: map[string]any{"provider": "tencent", "tags": map[string]any{"environment": "test"}}},
				// 正常
				{ID: 1003, AssetID: "i-3", AssetName: "smt-prod-redis", ModelUID: "volcengine_redis",
					Attributes: map[string]any{"provider": "volcengine"}},
			}, nil
		},
	}
	s := NewNodeAssetService(bindingRepo, nodeRepo, cmdbPort, envRepo, elog.DefaultLogger)

	summary, err := s.GetNodeAssetSummary(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("GetNodeAssetSummary() error = %v", err)
	}

	if len(summary.Suspicious) != 2 {
		t.Fatalf("Suspicious = %d, want 2", len(summary.Suspicious))
	}
	// 第一个疑异：命名含 -dev- 与 prod 矛盾
	if summary.Suspicious[0].AssetName != "smt-dev-web-01" {
		t.Errorf("Suspicious[0].AssetName = %s, want smt-dev-web-01", summary.Suspicious[0].AssetName)
	}
}

// TestGetNodeAssetSummaryEmptyTree 无绑定时返回空分布而非 nil
func TestGetNodeAssetSummaryEmptyTree(t *testing.T) {
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
			return nil, nil
		},
	}
	envRepo := &summaryEnvRepo{
		listFn: func(ctx context.Context, filter domain.EnvironmentFilter) ([]domain.Environment, error) {
			return nil, nil
		},
	}
	cmdbPort := &summaryCmdbPort{
		listByIDsFn: func(ctx context.Context, ids []int64) ([]port.CMDBInstance, error) {
			return nil, nil
		},
	}
	s := NewNodeAssetService(bindingRepo, nodeRepo, cmdbPort, envRepo, elog.DefaultLogger)

	summary, err := s.GetNodeAssetSummary(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("GetNodeAssetSummary() error = %v", err)
	}
	if summary.ByEnvironment == nil || summary.ByProvider == nil || summary.ByType == nil || summary.ByBindType == nil {
		t.Error("空子树应返回空 map，不应为 nil")
	}
}

// TestGetNodeAssetSummaryMissingCMDBInstance CMDB 缺失的绑定跳过
func TestGetNodeAssetSummaryMissingCMDBInstance(t *testing.T) {
	nodeRepo, bindingRepo, envRepo, _ := newSummaryFixture()
	cmdbPort := &summaryCmdbPort{
		listByIDsFn: func(ctx context.Context, ids []int64) ([]port.CMDBInstance, error) {
			// 只返回 1001，缺失 1002/1003
			return []port.CMDBInstance{
				{ID: 1001, AssetID: "i-prod-1", AssetName: "smt-prod-web-01", ModelUID: "aliyun_ecs",
					Attributes: map[string]any{"provider": "aliyun"}},
			}, nil
		},
	}
	s := NewNodeAssetService(bindingRepo, nodeRepo, cmdbPort, envRepo, elog.DefaultLogger)

	summary, err := s.GetNodeAssetSummary(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("GetNodeAssetSummary() error = %v", err)
	}
	// 仅命中 1 个实例
	if summary.Total != 1 {
		t.Errorf("Total = %d, want 1 (CMDB 缺失的绑定应跳过)", summary.Total)
	}
}
