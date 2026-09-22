package service

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/repository"
	"github.com/gotomicro/ego/core/elog"
)

// 节点路径修复单测：CreateNode 路径回填真实 ID + RebuildPaths 修复历史脏 path。

type treeStubNodeRepo struct {
	repository.NodeRepository
	createReturnID  int64
	getByIDFn       func(ctx context.Context, id int64) (domain.ServiceTreeNode, error)
	listFn          func(ctx context.Context, filter domain.NodeFilter) ([]domain.ServiceTreeNode, error)
	updatePathCalls []struct {
		id   int64
		path string
	}
}

func (s *treeStubNodeRepo) Create(ctx context.Context, node domain.ServiceTreeNode) (int64, error) {
	return s.createReturnID, nil
}

func (s *treeStubNodeRepo) GetByID(ctx context.Context, id int64) (domain.ServiceTreeNode, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(ctx, id)
	}
	return domain.ServiceTreeNode{}, domain.ErrNodeNotFound
}

func (s *treeStubNodeRepo) List(ctx context.Context, filter domain.NodeFilter) ([]domain.ServiceTreeNode, error) {
	return s.listFn(ctx, filter)
}

func (s *treeStubNodeRepo) UpdatePath(ctx context.Context, id int64, path string) error {
	s.updatePathCalls = append(s.updatePathCalls, struct {
		id   int64
		path string
	}{id, path})
	return nil
}

func newTestTreeService(nodeRepo *treeStubNodeRepo) TreeService {
	return NewTreeService(nodeRepo, &stubBindingRepo{}, elog.DefaultLogger)
}

// TestCreateNodeBuildsPathWithRealID 创建节点路径用回填后的真实 ID（而非恒 0）
func TestCreateNodeBuildsPathWithRealID(t *testing.T) {
	// Arrange
	repo := &treeStubNodeRepo{
		createReturnID: 5,
		getByIDFn: func(ctx context.Context, id int64) (domain.ServiceTreeNode, error) {
			return domain.ServiceTreeNode{ID: 4, Path: "/4/", Level: 1}, nil
		},
	}
	s := newTestTreeService(repo)

	// Act
	_, err := s.CreateNode(context.Background(), domain.ServiceTreeNode{
		TenantID: 3,
		Name:     "SMT-贴片技术平台",
		ParentID: 4,
	})

	// Assert
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}
	if len(repo.updatePathCalls) != 1 {
		t.Fatalf("UpdatePath 调用次数 = %d, want 1", len(repo.updatePathCalls))
	}
	call := repo.updatePathCalls[0]
	if call.id != 5 || call.path != "/4/5/" {
		t.Errorf("UpdatePath = (%d, %q), want (5, \"/4/5/\")", call.id, call.path)
	}
}

// TestCreateNodeRootBuildsPath 根节点（无父）路径为 /id/
func TestCreateNodeRootBuildsPath(t *testing.T) {
	repo := &treeStubNodeRepo{createReturnID: 4}
	s := newTestTreeService(repo)

	_, err := s.CreateNode(context.Background(), domain.ServiceTreeNode{
		TenantID: 3,
		Name:     "嘉立创",
		ParentID: 0,
	})

	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}
	if len(repo.updatePathCalls) != 1 || repo.updatePathCalls[0].path != "/4/" {
		t.Fatalf("UpdatePath = %+v, want path \"/4/\"", repo.updatePathCalls)
	}
}

// TestRebuildPathsFixesStalePaths 修复兄弟节点 path 相同的脏数据（父先于子重算）
func TestRebuildPathsFixesStalePaths(t *testing.T) {
	// Arrange: 历史脏数据——根/子/叶子 path 均为 0 填充
	repo := &treeStubNodeRepo{
		listFn: func(ctx context.Context, filter domain.NodeFilter) ([]domain.ServiceTreeNode, error) {
			return []domain.ServiceTreeNode{
				{ID: 4, ParentID: 0, Level: 1, Path: "/0/"},
				{ID: 1, ParentID: 4, Level: 2, Path: "/0/0/"}, // CPP（父 4 根）
				{ID: 5, ParentID: 4, Level: 2, Path: "/0/0/"}, // SMT（父 4 根）
				{ID: 10, ParentID: 5, Level: 3, Path: "/0/0/0/"},
			}, nil
		},
	}
	s := newTestTreeService(repo)

	// Act
	fixed, err := s.RebuildPaths(context.Background(), 3)

	// Assert
	if err != nil {
		t.Fatalf("RebuildPaths() error = %v", err)
	}
	if fixed != 4 {
		t.Errorf("fixed = %d, want 4（全部脏路径）", fixed)
	}
	got := map[int64]string{}
	for _, c := range repo.updatePathCalls {
		got[c.id] = c.path
	}
	want := map[int64]string{
		4:  "/4/",
		1:  "/4/1/",
		5:  "/4/5/",
		10: "/4/5/10/",
	}
	for id, p := range want {
		if got[id] != p {
			t.Errorf("node %d path = %q, want %q", id, got[id], p)
		}
	}
}

// TestRebuildPathsIdempotent 已正确路径不重复更新
func TestRebuildPathsIdempotent(t *testing.T) {
	repo := &treeStubNodeRepo{
		listFn: func(ctx context.Context, filter domain.NodeFilter) ([]domain.ServiceTreeNode, error) {
			return []domain.ServiceTreeNode{
				{ID: 4, ParentID: 0, Level: 1, Path: "/4/"},
				{ID: 5, ParentID: 4, Level: 2, Path: "/4/5/"},
			}, nil
		},
	}
	s := newTestTreeService(repo)

	fixed, err := s.RebuildPaths(context.Background(), 3)
	if err != nil {
		t.Fatalf("RebuildPaths() error = %v", err)
	}
	if fixed != 0 {
		t.Errorf("fixed = %d, want 0（路径已正确）", fixed)
	}
	if len(repo.updatePathCalls) != 0 {
		t.Errorf("UpdatePath 调用次数 = %d, want 0", len(repo.updatePathCalls))
	}
}
