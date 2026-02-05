package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	iamrepo "github.com/Havens-blog/e-cam-service/internal/iam/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/iam"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/errs"
	"github.com/gotomicro/ego/core/elog"
	"go.mongodb.org/mongo-driver/mongo"
)

// CloudIAMAdapterFactory 云平台IAM适配器工厂接口（避免循环导入）
type CloudIAMAdapterFactory = iam.CloudIAMAdapterFactory

// CloudIAMAdapter 云平台IAM适配器接口（避免循环导入）
type CloudIAMAdapter = iam.CloudIAMAdapter

// UserGroupService 用户组服务接口
type UserGroupService interface {
	CreateGroup(ctx context.Context, req *domain.CreateUserGroupRequest) (*domain.UserGroup, error)
	GetGroup(ctx context.Context, id int64) (*domain.UserGroup, error)
	ListGroups(ctx context.Context, filter domain.UserGroupFilter) ([]*domain.UserGroup, int64, error)
	UpdateGroup(ctx context.Context, id int64, req *domain.UpdateUserGroupRequest) error
	DeleteGroup(ctx context.Context, id int64) error
	UpdatePolicies(ctx context.Context, groupID int64, policies []domain.PermissionPolicy) error
	SyncGroups(ctx context.Context, cloudAccountID int64) (*GroupSyncResult, error)
	GetGroupMembers(ctx context.Context, groupID int64) ([]*domain.CloudUser, error)
}

// GroupSyncResult 用户组同步结果
type GroupSyncResult struct {
	TotalGroups   int `json:"total_groups"`
	CreatedGroups int `json:"created_groups"`
	UpdatedGroups int `json:"updated_groups"`
	FailedGroups  int `json:"failed_groups"`
	TotalMembers  int `json:"total_members"`
	SyncedMembers int `json:"synced_members"`
	FailedMembers int `json:"failed_members"`
}

type userGroupService struct {
	groupRepo      iamrepo.UserGroupRepository
	userRepo       iamrepo.CloudUserRepository
	syncTaskRepo   iamrepo.SyncTaskRepository
	auditRepo      iamrepo.AuditLogRepository
	accountRepo    repository.CloudAccountRepository
	adapterFactory CloudIAMAdapterFactory
	logger         *elog.Component
}

func NewUserGroupService(
	groupRepo iamrepo.UserGroupRepository,
	userRepo iamrepo.CloudUserRepository,
	syncTaskRepo iamrepo.SyncTaskRepository,
	auditRepo iamrepo.AuditLogRepository,
	accountRepo repository.CloudAccountRepository,
	adapterFactory CloudIAMAdapterFactory,
	logger *elog.Component,
) UserGroupService {
	return &userGroupService{
		groupRepo:      groupRepo,
		userRepo:       userRepo,
		syncTaskRepo:   syncTaskRepo,
		auditRepo:      auditRepo,
		accountRepo:    accountRepo,
		adapterFactory: adapterFactory,
		logger:         logger,
	}
}

func (s *userGroupService) CreateGroup(ctx context.Context, req *domain.CreateUserGroupRequest) (*domain.UserGroup, error) {
	s.logger.Info("创建用户组",
		elog.String("name", req.Name),
		elog.String("tenant_id", req.TenantID))

	if err := s.validateCreateGroupRequest(req); err != nil {
		s.logger.Error("创建用户组参数验证失败", elog.FieldErr(err))
		return nil, err
	}

	// 检查用户组名称是否已存在
	existingGroup, err := s.groupRepo.GetByName(ctx, req.Name, req.TenantID)
	if err == nil && existingGroup.ID > 0 {
		return nil, errs.PermissionGroupAlreadyExist
	}
	if err != nil && err != mongo.ErrNoDocuments {
		s.logger.Error("检查用户组名称失败", elog.FieldErr(err))
		return nil, fmt.Errorf("检查用户组名称失败: %w", err)
	}

	now := time.Now()
	group := domain.UserGroup{
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
		s.logger.Error("用户组数据验证失败", elog.FieldErr(err))
		return nil, errs.ParamsError
	}

	id, err := s.groupRepo.Create(ctx, group)
	if err != nil {
		s.logger.Error("创建用户组失败", elog.FieldErr(err))
		return nil, fmt.Errorf("创建用户组失败: %w", err)
	}

	group.ID = id

	s.logger.Info("创建用户组成功",
		elog.Int64("group_id", id),
		elog.String("name", req.Name))

	return &group, nil
}

func (s *userGroupService) GetGroup(ctx context.Context, id int64) (*domain.UserGroup, error) {
	s.logger.Debug("获取用户组详情", elog.Int64("group_id", id))

	group, err := s.groupRepo.GetByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.PermissionGroupNotFound
		}
		s.logger.Error("获取用户组失败", elog.Int64("group_id", id), elog.FieldErr(err))
		return nil, fmt.Errorf("获取用户组失败: %w", err)
	}

	return &group, nil
}

func (s *userGroupService) ListGroups(ctx context.Context, filter domain.UserGroupFilter) ([]*domain.UserGroup, int64, error) {
	s.logger.Debug("获取用户组列表",
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
		s.logger.Error("获取用户组列表失败", elog.FieldErr(err))
		return nil, 0, fmt.Errorf("获取用户组列表失败: %w", err)
	}

	groupPtrs := make([]*domain.UserGroup, len(groups))
	for i := range groups {
		groupPtrs[i] = &groups[i]
	}

	s.logger.Debug("获取用户组列表成功",
		elog.Int64("total", total),
		elog.Int("count", len(groups)))

	return groupPtrs, total, nil
}

func (s *userGroupService) UpdateGroup(ctx context.Context, id int64, req *domain.UpdateUserGroupRequest) error {
	s.logger.Info("更新用户组", elog.Int64("group_id", id))

	group, err := s.groupRepo.GetByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.PermissionGroupNotFound
		}
		s.logger.Error("获取用户组失败", elog.FieldErr(err))
		return fmt.Errorf("获取用户组失败: %w", err)
	}

	updated := false
	if req.Name != nil && *req.Name != group.Name {
		// 检查新名称是否已存在
		existingGroup, err := s.groupRepo.GetByName(ctx, *req.Name, group.TenantID)
		if err == nil && existingGroup.ID != id {
			return errs.PermissionGroupAlreadyExist
		}
		if err != nil && err != mongo.ErrNoDocuments {
			s.logger.Error("检查用户组名称失败", elog.FieldErr(err))
			return fmt.Errorf("检查用户组名称失败: %w", err)
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
		s.logger.Debug("用户组信息无变更", elog.Int64("group_id", id))
		return nil
	}

	now := time.Now()
	group.UpdateTime = now
	group.UTime = now.Unix()

	if err := s.groupRepo.Update(ctx, group); err != nil {
		s.logger.Error("更新用户组失败", elog.FieldErr(err))
		return fmt.Errorf("更新用户组失败: %w", err)
	}

	s.logger.Info("更新用户组成功", elog.Int64("group_id", id))

	return nil
}

func (s *userGroupService) DeleteGroup(ctx context.Context, id int64) error {
	s.logger.Info("删除用户组", elog.Int64("group_id", id))

	group, err := s.groupRepo.GetByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.PermissionGroupNotFound
		}
		s.logger.Error("获取用户组失败", elog.FieldErr(err))
		return fmt.Errorf("获取用户组失败: %w", err)
	}

	// 检查是否有用户使用该用户组
	if group.UserCount > 0 {
		return errs.PermissionGroupHasUsers
	}

	if err := s.groupRepo.Delete(ctx, id); err != nil {
		s.logger.Error("删除用户组失败", elog.FieldErr(err))
		return fmt.Errorf("删除用户组失败: %w", err)
	}

	s.logger.Info("删除用户组成功", elog.Int64("group_id", id))

	return nil
}

func (s *userGroupService) validateCreateGroupRequest(req *domain.CreateUserGroupRequest) error {
	if req.Name == "" {
		return fmt.Errorf("用户组名称不能为空")
	}
	if len(req.CloudPlatforms) == 0 {
		return fmt.Errorf("云平台列表不能为空")
	}
	if req.TenantID == "" {
		return fmt.Errorf("租户ID不能为空")
	}
	return nil
}

func (s *userGroupService) UpdatePolicies(ctx context.Context, groupID int64, policies []domain.PermissionPolicy) error {
	s.logger.Info("更新用户组策略",
		elog.Int64("group_id", groupID),
		elog.Int("policy_count", len(policies)))

	// 获取用户组
	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.PermissionGroupNotFound
		}
		s.logger.Error("获取用户组失败", elog.FieldErr(err))
		return fmt.Errorf("获取用户组失败: %w", err)
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

	s.logger.Info("更新用户组策略成功",
		elog.Int64("group_id", groupID),
		elog.Int("policy_count", len(policies)))

	return nil
}

func (s *userGroupService) validatePolicies(policies []domain.PermissionPolicy) error {
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

func (s *userGroupService) logPolicyUpdate(ctx context.Context, groupID int64, groupName string, oldPolicies, newPolicies []domain.PermissionPolicy) error {
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

func (s *userGroupService) triggerPermissionSync(ctx context.Context, groupID int64, tenantID string) error {
	// 获取使用该用户组的所有用户
	filter := domain.CloudUserFilter{
		TenantID: tenantID,
		Limit:    1000,
	}

	users, _, err := s.userRepo.List(ctx, filter)
	if err != nil {
		return fmt.Errorf("获取用户列表失败: %w", err)
	}

	// 筛选出包含该用户组的用户
	affectedUsers := make([]domain.CloudUser, 0)
	for _, user := range users {
		for _, gid := range user.UserGroups {
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

// SyncGroups 同步云平台用户组
func (s *userGroupService) SyncGroups(ctx context.Context, cloudAccountID int64) (*GroupSyncResult, error) {
	s.logger.Info("开始同步云用户组", elog.Int64("cloud_account_id", cloudAccountID))

	// 获取云账号信息
	account, err := s.accountRepo.GetByID(ctx, cloudAccountID)
	if err != nil {
		s.logger.Error("获取云账号失败", elog.FieldErr(err))
		return nil, fmt.Errorf("获取云账号失败: %w", err)
	}

	// 获取云平台适配器
	adapter, err := s.adapterFactory.CreateAdapter(account.Provider)
	if err != nil {
		s.logger.Error("获取云平台适配器失败", elog.FieldErr(err))
		return nil, fmt.Errorf("获取云平台适配器失败: %w", err)
	}

	// 从云平台获取用户组列表
	cloudGroups, err := adapter.ListGroups(ctx, &account)
	if err != nil {
		s.logger.Error("从云平台获取用户组列表失败", elog.FieldErr(err))
		return nil, fmt.Errorf("从云平台获取用户组列表失败: %w", err)
	}

	result := &GroupSyncResult{
		TotalGroups: len(cloudGroups),
	}

	// 同步每个用户组
	for _, cloudGroup := range cloudGroups {
		groupID, isNew, err := s.syncSingleGroup(ctx, cloudGroup, &account)
		if err != nil {
			s.logger.Warn("同步用户组失败",
				elog.String("group_id", cloudGroup.CloudGroupID),
				elog.String("group_name", cloudGroup.GroupName),
				elog.FieldErr(err))
			result.FailedGroups++
			continue
		}

		if isNew {
			result.CreatedGroups++
		} else {
			result.UpdatedGroups++
		}

		// 同步用户组成员（内部会自动更新成员数量）
		memberResult := s.syncGroupMembers(ctx, adapter, &account, cloudGroup.CloudGroupID, groupID)
		result.TotalMembers += memberResult.Total
		result.SyncedMembers += memberResult.Synced
		result.FailedMembers += memberResult.Failed
	}

	s.logger.Info("用户组同步完成",
		elog.Int64("cloud_account_id", cloudAccountID),
		elog.Int("total_groups", result.TotalGroups),
		elog.Int("created_groups", result.CreatedGroups),
		elog.Int("updated_groups", result.UpdatedGroups),
		elog.Int("failed_groups", result.FailedGroups),
		elog.Int("total_members", result.TotalMembers),
		elog.Int("synced_members", result.SyncedMembers),
		elog.Int("failed_members", result.FailedMembers))

	return result, nil
}

// syncSingleGroup 同步单个用户组
// 返回: groupID, isNew, error
func (s *userGroupService) syncSingleGroup(ctx context.Context, cloudGroup *domain.UserGroup, account *domain.CloudAccount) (int64, bool, error) {
	// 检查用户组是否已存在
	existingGroup, err := s.groupRepo.GetByName(ctx, cloudGroup.GroupName, account.TenantID)
	if err != nil && err != mongo.ErrNoDocuments {
		return 0, false, fmt.Errorf("查询用户组失败: %w", err)
	}

	if err == mongo.ErrNoDocuments {
		// 创建新用户组
		groupID, err := s.createSyncedGroup(ctx, cloudGroup, account)
		return groupID, true, err
	}

	// 更新现有用户组
	err = s.updateSyncedGroup(ctx, &existingGroup, cloudGroup)
	return existingGroup.ID, false, err
}

// createSyncedGroup 创建同步的用户组
func (s *userGroupService) createSyncedGroup(ctx context.Context, cloudGroup *domain.UserGroup, account *domain.CloudAccount) (int64, error) {
	now := time.Now()
	cloudGroup.CloudAccountID = account.ID
	cloudGroup.Provider = account.Provider
	cloudGroup.TenantID = account.TenantID
	cloudGroup.CreateTime = now
	cloudGroup.UpdateTime = now
	cloudGroup.CTime = now.Unix()
	cloudGroup.UTime = now.Unix()

	// 如果没有名称，使用 GroupName
	if cloudGroup.Name == "" {
		cloudGroup.Name = cloudGroup.GroupName
	}

	id, err := s.groupRepo.Create(ctx, *cloudGroup)
	if err != nil {
		return 0, fmt.Errorf("创建用户组失败: %w", err)
	}

	s.logger.Info("创建同步用户组成功",
		elog.Int64("group_id", id),
		elog.String("group_name", cloudGroup.GroupName))

	return id, nil
}

// updateSyncedGroup 更新同步的用户组
func (s *userGroupService) updateSyncedGroup(ctx context.Context, existingGroup, cloudGroup *domain.UserGroup) error {
	// 保留本地数据
	cloudGroup.ID = existingGroup.ID
	cloudGroup.Name = existingGroup.Name
	cloudGroup.TenantID = existingGroup.TenantID
	cloudGroup.CreateTime = existingGroup.CreateTime
	cloudGroup.CTime = existingGroup.CTime

	// 保留用户数量（不从云端同步，由本地维护）
	cloudGroup.UserCount = existingGroup.UserCount

	// 更新时间
	now := time.Now()
	cloudGroup.UpdateTime = now
	cloudGroup.UTime = now.Unix()

	if err := s.groupRepo.Update(ctx, *cloudGroup); err != nil {
		return fmt.Errorf("更新用户组失败: %w", err)
	}

	s.logger.Info("更新同步用户组成功",
		elog.Int64("group_id", existingGroup.ID),
		elog.String("group_name", cloudGroup.GroupName),
		elog.Int64("cloud_account_id", cloudGroup.CloudAccountID))

	return nil
}

// GroupMemberSyncResult 用户组成员同步结果
type GroupMemberSyncResult struct {
	Total  int
	Synced int
	Failed int
}

// syncGroupMembers 同步用户组成员
func (s *userGroupService) syncGroupMembers(ctx context.Context, adapter CloudIAMAdapter, account *domain.CloudAccount, cloudGroupID string, localGroupID int64) *GroupMemberSyncResult {
	result := &GroupMemberSyncResult{}

	s.logger.Info("开始同步用户组成员",
		elog.String("cloud_group_id", cloudGroupID),
		elog.Int64("local_group_id", localGroupID))

	// 从云平台获取用户组成员列表
	cloudMembers, err := adapter.ListGroupUsers(ctx, account, cloudGroupID)
	if err != nil {
		s.logger.Error("获取云平台用户组成员失败",
			elog.String("cloud_group_id", cloudGroupID),
			elog.FieldErr(err))
		return result
	}

	result.Total = len(cloudMembers)
	s.logger.Debug("用户数量：", elog.Int("", result.Total))
	// 同步每个成员
	for _, cloudMember := range cloudMembers {
		s.logger.Debug("用户信息：", elog.Any("", cloudMember))
		if err := s.syncGroupMember(ctx, cloudMember, account, localGroupID); err != nil {
			s.logger.Warn("同步用户组成员失败",
				elog.String("cloud_user_id", cloudMember.CloudUserID),
				elog.String("username", cloudMember.Username),
				elog.FieldErr(err))
			result.Failed++
		} else {
			result.Synced++
		}
	}

	// 直接更新用户组的成员数量（使用同步成功的数量）
	if result.Synced > 0 {
		if err := s.updateGroupMemberCount(ctx, localGroupID, result.Synced); err != nil {
			s.logger.Warn("更新用户组成员数量失败",
				elog.Int64("group_id", localGroupID),
				elog.FieldErr(err))
		}
	}

	s.logger.Info("用户组成员同步完成",
		elog.String("cloud_group_id", cloudGroupID),
		elog.Int64("local_group_id", localGroupID),
		elog.Int("total", result.Total),
		elog.Int("synced", result.Synced),
		elog.Int("failed", result.Failed))

	return result
}

// syncGroupMember 同步单个用户组成员
func (s *userGroupService) syncGroupMember(ctx context.Context, cloudMember *domain.CloudUser, account *domain.CloudAccount, groupID int64) error {
	// 检查用户是否已存在
	existingUser, err := s.userRepo.GetByCloudUserID(ctx, cloudMember.CloudUserID, account.Provider)
	if err != nil && err != mongo.ErrNoDocuments {
		return fmt.Errorf("查询用户失败: %w", err)
	}
	// s.logger.Warn("同步用户信息查询", elog.String("usernameID", cloudMember.CloudUserID),
	// 	elog.Int64("GroupID:", groupID),
	// )
	var userID int64
	var needUpdateGroup bool

	if err == mongo.ErrNoDocuments {
		// 创建新用户
		now := time.Now()
		cloudMember.CloudAccountID = account.ID
		cloudMember.Provider = account.Provider
		cloudMember.TenantID = account.TenantID
		cloudMember.UserGroups = []int64{groupID}
		cloudMember.CreateTime = now
		cloudMember.UpdateTime = now
		cloudMember.CTime = now.Unix()
		cloudMember.UTime = now.Unix()

		userID, err = s.userRepo.Create(ctx, *cloudMember)
		if err != nil {
			return fmt.Errorf("创建用户失败: %w", err)
		}

		s.logger.Info("创建同步用户成功",
			elog.Int64("user_id", userID),
			elog.String("username", cloudMember.Username),
			elog.Int64("group_id", groupID))
	} else {
		userID = existingUser.ID
		// 检查用户是否已在该用户组中
		if !s.isUserInGroup(existingUser.UserGroups, groupID) {
			needUpdateGroup = true
		}
	}

	// 如果需要更新用户组
	if needUpdateGroup {
		existingUser.UserGroups = append(existingUser.UserGroups, groupID)
		now := time.Now()
		existingUser.UpdateTime = now
		existingUser.UTime = now.Unix()

		if err := s.userRepo.Update(ctx, existingUser); err != nil {
			return fmt.Errorf("更新用户用户组失败: %w", err)
		}

		s.logger.Info("将用户添加到用户组",
			elog.Int64("user_id", userID),
			elog.Int64("group_id", groupID))
	}

	return nil
}

// isUserInGroup 检查用户是否在用户组中
func (s *userGroupService) isUserInGroup(userGroups []int64, groupID int64) bool {
	for _, gid := range userGroups {
		if gid == groupID {
			return true
		}
	}
	return false
}

// getGroupMemberCount 获取用户组的实际成员数量
func (s *userGroupService) getGroupMemberCount(ctx context.Context, groupID int64) (int, error) {
	// 获取用户组信息
	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		return 0, fmt.Errorf("获取用户组失败: %w", err)
	}

	s.logger.Debug("开始查询用户组成员",
		elog.Int64("group_id", groupID),
		elog.String("tenant_id", group.TenantID))

	// 查询包含该用户组的所有用户
	members, err := s.userRepo.GetByGroupID(ctx, groupID, group.TenantID)
	if err != nil {
		s.logger.Error("查询用户组成员失败",
			elog.Int64("group_id", groupID),
			elog.FieldErr(err))
		return 0, fmt.Errorf("查询用户组成员失败: %w", err)
	}

	s.logger.Debug("查询用户组成员完成",
		elog.Int64("group_id", groupID),
		elog.Int("member_count", len(members)))

	return len(members), nil
}

// updateGroupMemberCount 更新用户组的成员数量（直接设置）
func (s *userGroupService) updateGroupMemberCount(ctx context.Context, groupID int64, memberCount int) error {
	// 获取用户组
	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		return fmt.Errorf("获取用户组失败: %w", err)
	}

	// 更新成员数量
	group.UserCount = memberCount
	now := time.Now()
	group.UpdateTime = now
	group.UTime = now.Unix()

	if err := s.groupRepo.Update(ctx, group); err != nil {
		return fmt.Errorf("更新用户组失败: %w", err)
	}

	s.logger.Info("更新用户组成员数量成功",
		elog.Int64("group_id", groupID),
		elog.Int("member_count", memberCount))

	return nil
}

// GetGroupMembers 获取用户组的成员列表
func (s *userGroupService) GetGroupMembers(ctx context.Context, groupID int64) ([]*domain.CloudUser, error) {
	s.logger.Debug("获取用户组成员列表", elog.Int64("group_id", groupID))

	// 检查用户组是否存在
	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.PermissionGroupNotFound
		}
		s.logger.Error("获取用户组失败", elog.FieldErr(err))
		return nil, fmt.Errorf("获取用户组失败: %w", err)
	}

	// 直接查询包含该用户组ID的所有用户
	members, err := s.userRepo.GetByGroupID(ctx, groupID, group.TenantID)
	if err != nil {
		s.logger.Error("查询用户组成员失败", elog.FieldErr(err))
		return nil, fmt.Errorf("查询用户组成员失败: %w", err)
	}

	// 转换为指针数组
	memberPtrs := make([]*domain.CloudUser, len(members))
	for i := range members {
		memberPtrs[i] = &members[i]
	}

	s.logger.Debug("获取用户组成员列表成功",
		elog.Int64("group_id", groupID),
		elog.Int("member_count", len(members)))

	return memberPtrs, nil
}
