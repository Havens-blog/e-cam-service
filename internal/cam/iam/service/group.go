package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	iamrepo "github.com/Havens-blog/e-cam-service/internal/cam/iam/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
	"go.mongodb.org/mongo-driver/mongo"
)

// PermissionGroupService 权限组服务接口
type PermissionGroupService interface {
	CreateGroup(ctx context.Context, req *domain.CreatePermissionGroupRequest) (*domain.PermissionGroup, error)
	GetGroup(ctx context.Context, id int64) (*domain.PermissionGroup, error)
	ListGroups(ctx context.Context, filter domain.PermissionGroupFilter) ([]*domain.PermissionGroup, int64, error)
	UpdateGroup(ctx context.Context, id int64, req *domain.UpdatePermissionGroupRequest) error
	DeleteGroup(ctx context.Context, id int64) error
	UpdatePolicies(ctx context.Context, groupID int64, policies []domain.PermissionPolicy) error
}

type permissionGroupService struct {
	groupRepo    iamrepo.PermissionGroupRepository
	userRepo     iamrepo.CloudUserRepository
	syncTaskRepo iamrepo.SyncTaskRepository
	auditRepo    iamrepo.AuditLogRepository
	logger       *elog.Component
}

func NewPermissionGroupService(
	groupRepo iamrepo.PermissionGroupRepository,
	userRepo iamrepo.CloudUserRepository,
	syncTaskRepo iamrepo.SyncTaskRepository,
	auditRepo iamrepo.AuditLogRepository,
	logger *elog.Component,
) PermissionGroupService {
	return &permissionGroupService{
		groupRepo:    groupRepo,
		userRepo:     userRepo,
		syncTaskRepo: syncTaskRepo,
		auditRepo:    auditRepo,
		logger:       logger,
	}
}

func (s *permissionGroupService) CreateGroup(ctx context.Context, req *domain.CreatePermissionGroupRequest) (*domain.PermissionGroup, error) {
	s.logger.Info("创建权限组",
		elog.String("name", req.Name),
		elog.String("tenant_id", req.TenantID))

	if err := s.validateCreateGroupRequest(req); err != nil {
		s.logger.Error("创建权限组参数验证失败", elog.FieldErr(err))
		return nil, err
	}

	// 检查权限组名称是否已存在
	existingGroup, err := s.groupRepo.GetByName(ctx, req.Name, req.TenantID)
	if err == nil && existingGroup.ID > 0 {
		return nil, errs.PermissionGroupAlreadyExist
	}
	if err != nil && err != mongo.ErrNoDocuments {
		s.logger.Error("检查权限组名称失败", elog.FieldErr(err))
		return nil, fmt.Errorf("检查权限组名称失败: %w", err)
	}

	now := time.Now()
	group := domain.PermissionGroup{
		Name:           req.Name,
		Description:    req.Description,
		Policies:       req.Policies,
		CloudPlatforms: req.CloudPlatforms,
		UserCount:      0,
		TenantID:       req.TenantID,
		CreateTime:     now,
		UpdateTime:     now,
		CTime:          now.Unix(),
		UTime:          now.Unix(),
	}

	if err := group.Validate(); err != nil {
		s.logger.Error("权限组数据验证失败", elog.FieldErr(err))
		return nil, errs.ParamsError
	}

	id, err := s.groupRepo.Create(ctx, group)
	if err != nil {
		s.logger.Error("创建权限组失败", elog.FieldErr(err))
		return nil, fmt.Errorf("创建权限组失败: %w", err)
	}

	group.ID = id

	s.logger.Info("创建权限组成功",
		elog.Int64("group_id", id),
		elog.String("name", req.Name))

	return &group, nil
}

func (s *permissionGroupService) GetGroup(ctx context.Context, id int64) (*domain.PermissionGroup, error) {
	s.logger.Debug("获取权限组详情", elog.Int64("group_id", id))

	group, err := s.groupRepo.GetByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.PermissionGroupNotFound
		}
		s.logger.Error("获取权限组失败", elog.Int64("group_id", id), elog.FieldErr(err))
		return nil, fmt.Errorf("获取权限组失败: %w", err)
	}

	return &group, nil
}

func (s *permissionGroupService) ListGroups(ctx context.Context, filter domain.PermissionGroupFilter) ([]*domain.PermissionGroup, int64, error) {
	s.logger.Debug("获取权限组列表",
		elog.String("tenant_id", filter.TenantID),
		elog.String("keyword", filter.Keyword))

	if filter.Limit == 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}

	groups, total, err := s.groupRepo.List(ctx, filter)
	if err != nil {
		s.logger.Error("获取权限组列表失败", elog.FieldErr(err))
		return nil, 0, fmt.Errorf("获取权限组列表失败: %w", err)
	}

	groupPtrs := make([]*domain.PermissionGroup, len(groups))
	for i := range groups {
		groupPtrs[i] = &groups[i]
	}

	s.logger.Debug("获取权限组列表成功",
		elog.Int64("total", total),
		elog.Int("count", len(groups)))

	return groupPtrs, total, nil
}

func (s *permissionGroupService) UpdateGroup(ctx context.Context, id int64, req *domain.UpdatePermissionGroupRequest) error {
	s.logger.Info("更新权限组", elog.Int64("group_id", id))

	group, err := s.groupRepo.GetByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.PermissionGroupNotFound
		}
		s.logger.Error("获取权限组失败", elog.FieldErr(err))
		return fmt.Errorf("获取权限组失败: %w", err)
	}

	updated := false
	if req.Name != nil && *req.Name != group.Name {
		// 检查新名称是否已存在
		existingGroup, err := s.groupRepo.GetByName(ctx, *req.Name, group.TenantID)
		if err == nil && existingGroup.ID != id {
			return errs.PermissionGroupAlreadyExist
		}
		if err != nil && err != mongo.ErrNoDocuments {
			s.logger.Error("检查权限组名称失败", elog.FieldErr(err))
			return fmt.Errorf("检查权限组名称失败: %w", err)
		}
		group.Name = *req.Name
		updated = true
	}
	if req.Description != nil && *req.Description != group.Description {
		group.Description = *req.Description
		updated = true
	}
	if req.CloudPlatforms != nil && len(req.CloudPlatforms) > 0 {
		group.CloudPlatforms = req.CloudPlatforms
		updated = true
	}

	if !updated {
		s.logger.Debug("权限组信息无变更", elog.Int64("group_id", id))
		return nil
	}

	now := time.Now()
	group.UpdateTime = now
	group.UTime = now.Unix()

	if err := s.groupRepo.Update(ctx, group); err != nil {
		s.logger.Error("更新权限组失败", elog.FieldErr(err))
		return fmt.Errorf("更新权限组失败: %w", err)
	}

	s.logger.Info("更新权限组成功", elog.Int64("group_id", id))

	return nil
}

func (s *permissionGroupService) DeleteGroup(ctx context.Context, id int64) error {
	s.logger.Info("删除权限组", elog.Int64("group_id", id))

	group, err := s.groupRepo.GetByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.PermissionGroupNotFound
		}
		s.logger.Error("获取权限组失败", elog.FieldErr(err))
		return fmt.Errorf("获取权限组失败: %w", err)
	}

	// 检查是否有用户使用该权限组
	if group.UserCount > 0 {
		return errs.PermissionGroupHasUsers
	}

	if err := s.groupRepo.Delete(ctx, id); err != nil {
		s.logger.Error("删除权限组失败", elog.FieldErr(err))
		return fmt.Errorf("删除权限组失败: %w", err)
	}

	s.logger.Info("删除权限组成功", elog.Int64("group_id", id))

	return nil
}

func (s *permissionGroupService) validateCreateGroupRequest(req *domain.CreatePermissionGroupRequest) error {
	if req.Name == "" {
		return fmt.Errorf("权限组名称不能为空")
	}
	if len(req.CloudPlatforms) == 0 {
		return fmt.Errorf("云平台列表不能为空")
	}
	if req.TenantID == "" {
		return fmt.Errorf("租户ID不能为空")
	}
	return nil
}

func (s *permissionGroupService) UpdatePolicies(ctx context.Context, groupID int64, policies []domain.PermissionPolicy) error {
	s.logger.Info("更新权限组策略",
		elog.Int64("group_id", groupID),
		elog.Int("policy_count", len(policies)))

	// 获取权限组
	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.PermissionGroupNotFound
		}
		s.logger.Error("获取权限组失败", elog.FieldErr(err))
		return fmt.Errorf("获取权限组失败: %w", err)
	}

	// 验证权限策略
	if err := s.validatePolicies(policies); err != nil {
		s.logger.Error("权限策略验证失败", elog.FieldErr(err))
		return err
	}

	// 记录变更前的策略（用于审计）
	oldPolicies := group.Policies

	// 更新权限策略
	if err := s.groupRepo.UpdatePolicies(ctx, groupID, policies); err != nil {
		s.logger.Error("更新权限策略失败", elog.FieldErr(err))
		return fmt.Errorf("更新权限策略失败: %w", err)
	}

	// 记录审计日志
	if err := s.logPolicyUpdate(ctx, groupID, group.Name, oldPolicies, policies); err != nil {
		s.logger.Warn("记录审计日志失败", elog.FieldErr(err))
	}

	// 触发权限同步任务
	if err := s.triggerPermissionSync(ctx, groupID, group.TenantID); err != nil {
		s.logger.Warn("触发权限同步失败", elog.FieldErr(err))
	}

	s.logger.Info("更新权限组策略成功",
		elog.Int64("group_id", groupID),
		elog.Int("policy_count", len(policies)))

	return nil
}

func (s *permissionGroupService) validatePolicies(policies []domain.PermissionPolicy) error {
	if len(policies) == 0 {
		return errs.PermissionGroupPolicyInvalid
	}

	// 验证每个策略
	for i, policy := range policies {
		if policy.PolicyID == "" {
			return fmt.Errorf("策略%d的PolicyID不能为空", i+1)
		}
		if policy.PolicyName == "" {
			return fmt.Errorf("策略%d的PolicyName不能为空", i+1)
		}
		if policy.Provider == "" {
			return fmt.Errorf("策略%d的Provider不能为空", i+1)
		}

		// 验证云厂商是否支持
		validProviders := map[domain.CloudProvider]bool{
			domain.CloudProviderAliyun:  true,
			domain.CloudProviderAWS:     true,
			domain.CloudProviderHuawei:  true,
			domain.CloudProviderTencent: true,
			domain.CloudProviderAzure:   true,
		}
		if !validProviders[policy.Provider] {
			return fmt.Errorf("策略%d的云厂商不支持: %s", i+1, policy.Provider)
		}

		// 验证策略类型
		if policy.PolicyType != domain.PolicyTypeSystem && policy.PolicyType != domain.PolicyTypeCustom {
			return fmt.Errorf("策略%d的类型无效: %s", i+1, policy.PolicyType)
		}
	}

	return nil
}

func (s *permissionGroupService) logPolicyUpdate(ctx context.Context, groupID int64, groupName string, oldPolicies, newPolicies []domain.PermissionPolicy) error {
	// 构建变更前后的值（简化为JSON字符串）
	beforeValue := fmt.Sprintf("策略数量: %d", len(oldPolicies))
	afterValue := fmt.Sprintf("策略数量: %d", len(newPolicies))

	now := time.Now()
	auditLog := domain.AuditLog{
		OperationType: domain.AuditOpUpdateGroup,
		OperatorID:    "system", // TODO: 从context中获取操作人信息
		OperatorName:  "system",
		TargetType:    "group",
		TargetID:      groupID,
		TargetName:    groupName,
		BeforeValue:   beforeValue,
		AfterValue:    afterValue,
		Result:        domain.AuditResultSuccess,
		TenantID:      "", // TODO: 从context中获取租户ID
		CreateTime:    now,
		CTime:         now.Unix(),
	}

	_, err := s.auditRepo.Create(ctx, auditLog)
	return err
}

func (s *permissionGroupService) triggerPermissionSync(ctx context.Context, groupID int64, tenantID string) error {
	// 获取使用该权限组的所有用户
	filter := domain.CloudUserFilter{
		TenantID: tenantID,
		Limit:    1000,
	}

	users, _, err := s.userRepo.List(ctx, filter)
	if err != nil {
		return fmt.Errorf("获取用户列表失败: %w", err)
	}

	// 筛选出包含该权限组的用户
	affectedUsers := make([]domain.CloudUser, 0)
	for _, user := range users {
		for _, gid := range user.PermissionGroups {
			if gid == groupID {
				affectedUsers = append(affectedUsers, user)
				break
			}
		}
	}

	s.logger.Info("触发权限同步",
		elog.Int64("group_id", groupID),
		elog.Int("affected_users", len(affectedUsers)))

	// 为每个受影响的用户创建同步任务
	now := time.Now()
	for _, user := range affectedUsers {
		syncTask := domain.SyncTask{
			TaskType:       domain.SyncTaskTypePermissionSync,
			TargetType:     domain.SyncTargetTypeUser,
			TargetID:       user.ID,
			CloudAccountID: user.CloudAccountID,
			Provider:       user.Provider,
			Status:         domain.SyncTaskStatusPending,
			Progress:       0,
			RetryCount:     0,
			MaxRetries:     3,
			CreateTime:     now,
			UpdateTime:     now,
			CTime:          now.Unix(),
			UTime:          now.Unix(),
		}

		if _, err := s.syncTaskRepo.Create(ctx, syncTask); err != nil {
			s.logger.Warn("创建同步任务失败",
				elog.Int64("user_id", user.ID),
				elog.FieldErr(err))
		}
	}

	return nil
}
