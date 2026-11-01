package service

import (
	"context"
	"reflect"
	"testing"

	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/gotomicro/ego/core/elog"

	"github.com/Havens-blog/e-cam-service/internal/cam/iam/repository"
)

// --- stubs（内嵌接口：未用方法被调用即 panic，能暴露误用）---

type stubAssignUserRepo struct {
	repository.CloudUserRepository
	users   map[int64]domain.CloudUser
	updated map[int64][]int64
	batched [][]int64
}

func (s *stubAssignUserRepo) GetByID(_ context.Context, id int64) (domain.CloudUser, error) {
	return s.users[id], nil
}

func (s *stubAssignUserRepo) UpdatePermissionGroups(_ context.Context, id int64, groupIDs []int64) error {
	if s.updated == nil {
		s.updated = map[int64][]int64{}
	}
	s.updated[id] = append([]int64(nil), groupIDs...)
	return nil
}

func (s *stubAssignUserRepo) BatchUpdatePermissionGroups(_ context.Context, _ []int64, groupIDs []int64) error {
	s.batched = append(s.batched, append([]int64(nil), groupIDs...))
	return nil
}

type stubAssignGroupRepo struct {
	repository.UserGroupRepository
	counts map[int64]int
}

func (s *stubAssignGroupRepo) GetByID(_ context.Context, id int64) (domain.UserGroup, error) {
	return domain.UserGroup{ID: id, TenantID: 3}, nil
}

func (s *stubAssignGroupRepo) IncrementUserCount(_ context.Context, id int64, delta int) error {
	if s.counts == nil {
		s.counts = map[int64]int{}
	}
	s.counts[id] += delta
	return nil
}

type stubAssignTaskRepo struct{ repository.SyncTaskRepository }

//nolint:gocritic // hugeParam: 必须与 repository.SyncTaskRepository.Create 的签名一致（值传递）
func (s *stubAssignTaskRepo) Create(_ context.Context, _ domain.SyncTask) (int64, error) {
	return 1, nil
}

func newAssignTestService(users map[int64]domain.CloudUser) (*cloudUserService, *stubAssignUserRepo, *stubAssignGroupRepo) {
	userRepo := &stubAssignUserRepo{users: users}
	groupRepo := &stubAssignGroupRepo{}
	return &cloudUserService{userRepo: userRepo, groupRepo: groupRepo, syncTaskRepo: &stubAssignTaskRepo{}, logger: elog.DefaultLogger}, userRepo, groupRepo
}

// 回归（2026-11-01）：前端批量分配默认 **追加模式**（文案："在用户现有权限组基础上添加新的权限组"），
// 但此前 VO/handler/service 都没有 mode —— 无论选哪种模式都是 `$set` 覆盖 ⇒
// 选"追加"会**静默删掉用户原有其它权限组**。此处钉住 append 的合并语义。
func TestAssignPermissionGroupsAppendMergesExisting(t *testing.T) {
	svc, userRepo, groupRepo := newAssignTestService(map[int64]domain.CloudUser{
		130: {ID: 130, TenantID: 3, UserGroups: []int64{1, 2}},
		131: {ID: 131, TenantID: 3, UserGroups: []int64{2}},
	})

	if err := svc.AssignPermissionGroups(context.Background(), []int64{130, 131}, []int64{3}, "append"); err != nil {
		t.Fatalf("append 分配失败: %v", err)
	}

	if got := userRepo.updated[130]; !reflect.DeepEqual(got, []int64{1, 2, 3}) {
		t.Fatalf("用户 130 追加后应为 [1 2 3]，实际 %v", got)
	}
	if got := userRepo.updated[131]; !reflect.DeepEqual(got, []int64{2, 3}) {
		t.Fatalf("用户 131 追加后应为 [2 3]，实际 %v", got)
	}
	if len(userRepo.batched) != 0 {
		t.Fatalf("append 模式不应调用整体覆盖，实际调用 %d 次", len(userRepo.batched))
	}
	// 用户组人数：只有新增的 3 号组 +1/人；1、2 号组人数不变（不应出现负 delta）
	if groupRepo.counts[3] != 2 {
		t.Fatalf("3 号组人数应 +2，实际 %d", groupRepo.counts[3])
	}
	if groupRepo.counts[1] != 0 || groupRepo.counts[2] != 0 {
		t.Fatalf("原有组人数不应变化，实际 counts=%v", groupRepo.counts)
	}
}

// 兼容性：mode 为空/其它值 ⇒ 保持历史"整体覆盖"行为（老调用方不受影响）。
func TestAssignPermissionGroupsDefaultKeepsReplace(t *testing.T) {
	for _, mode := range []string{"", "replace"} {
		svc, userRepo, _ := newAssignTestService(map[int64]domain.CloudUser{
			130: {ID: 130, TenantID: 3, UserGroups: []int64{1, 2}},
		})
		if err := svc.AssignPermissionGroups(context.Background(), []int64{130}, []int64{3}, mode); err != nil {
			t.Fatalf("mode=%q 分配失败: %v", mode, err)
		}
		if len(userRepo.batched) != 1 || !reflect.DeepEqual(userRepo.batched[0], []int64{3}) {
			t.Fatalf("mode=%q 应走整体覆盖 [3]，实际 %v", mode, userRepo.batched)
		}
		if len(userRepo.updated) != 0 {
			t.Fatalf("mode=%q 不应走逐用户追加，实际 %v", mode, userRepo.updated)
		}
	}
}

func TestMergeGroupIDs(t *testing.T) {
	cases := []struct {
		existing, given, want []int64
	}{
		{[]int64{1, 2}, []int64{3}, []int64{1, 2, 3}},
		{[]int64{2, 1}, []int64{1, 4}, []int64{2, 1, 4}},
		{nil, []int64{5}, []int64{5}},
		{[]int64{7}, nil, []int64{7}},
	}
	for _, c := range cases {
		if got := mergeGroupIDs(c.existing, c.given); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("mergeGroupIDs(%v, %v) = %v，期望 %v", c.existing, c.given, got, c.want)
		}
	}
}
