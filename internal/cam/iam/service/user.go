package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	iamrepo "github.com/Havens-blog/e-cam-service/internal/cam/iam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/iam"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
	"go.mongodb.org/mongo-driver/mongo"
)

// CloudUserService 云用户服务接口
type CloudUserService interface {
	CreateUser(ctx context.Context, req *domain.CreateCloudUserRequest) (*domain.CloudUser, error)
	GetUser(ctx context.Context, id int64) (*domain.CloudUser, error)
	ListUsers(ctx context.Context, filter domain.CloudUserFilter) ([]*domain.CloudUser, int64, error)
	UpdateUser(ctx context.Context, id int64, req *domain.UpdateCloudUserRequest) error
	DeleteUser(ctx context.Context, id int64) error
	SyncUsers(ctx context.Context, cloudAccountID int64) (*UserSyncResult, error)
	SyncUsersAsync(ctx context.Context, cloudAccountID int64) (*domain.SyncTask, error)
	ExecuteBatchUserSync(ctx context.Context, taskID int64) error
	AssignPermissionGroups(ctx context.Context, userIDs []int64, groupIDs []int64) error
}

type cloudUserService struct {
	userRepo       iamrepo.CloudUserRepository
	groupRepo      iamrepo.UserGroupRepository
	syncTaskRepo   iamrepo.SyncTaskRepository
	accountRepo    repository.CloudAccountRepository
	adapterFactory iam.CloudIAMAdapterFactory
	logger         *elog.Component
}

func NewCloudUserService(
	userRepo iamrepo.CloudUserRepository,
	groupRepo iamrepo.UserGroupRepository,
	syncTaskRepo iamrepo.SyncTaskRepository,
	accountRepo repository.CloudAccountRepository,
	adapterFactory iam.CloudIAMAdapterFactory,
	logger *elog.Component,
) CloudUserService {
	return &cloudUserService{
		userRepo:       userRepo,
		groupRepo:      groupRepo,
		syncTaskRepo:   syncTaskRepo,
		accountRepo:    accountRepo,
		adapterFactory: adapterFactory,
		logger:         logger,
	}
}

func (s *cloudUserService) CreateUser(ctx context.Context, req *domain.CreateCloudUserRequest) (*domain.CloudUser, error) {
	s.logger.Info("创建云用户",
		elog.String("username", req.Username),
		elog.String("user_type", string(req.UserType)),
		elog.Int64("cloud_account_id", req.CloudAccountID))

	if err := s.validateCreateUserRequest(req); err != nil {
		s.logger.Error("创建用户参数验证失败", elog.FieldErr(err))
		return nil, err
	}

	account, err := s.accountRepo.GetByID(ctx, req.CloudAccountID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.AccountNotFound
		}
		s.logger.Error("获取云账号失败", elog.FieldErr(err))
		return nil, fmt.Errorf("获取云账号失败: %w", err)
	}

	if len(req.UserGroups) > 0 {
		if err := s.validatePermissionGroups(ctx, req.UserGroups, req.TenantID); err != nil {
			return nil, err
		}
	}

	now := time.Now()
	user := domain.CloudUser{
		Username:       req.Username,
		UserType:       req.UserType,
		CloudAccountID: req.CloudAccountID,
		Provider:       account.Provider,
		DisplayName:    req.DisplayName,
		Email:          req.Email,
		UserGroups:     req.UserGroups,
		Metadata: domain.CloudUserMetadata{
			Tags: make(map[string]string),
		},
		Status:     domain.CloudUserStatusActive,
		TenantID:   req.TenantID,
		CreateTime: now,
		UpdateTime: now,
		CTime:      now.Unix(),
		UTime:      now.Unix(),
	}

	if err := user.Validate(); err != nil {
		s.logger.Error("用户数据验证失败", elog.FieldErr(err))
		return nil, errs.ParamsError
	}

	id, err := s.userRepo.Create(ctx, user)
	if err != nil {
		s.logger.Error("创建用户失败", elog.FieldErr(err))
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}

	user.ID = id

	if len(req.UserGroups) > 0 {
		for _, groupID := range req.UserGroups {
			if err := s.groupRepo.IncrementUserCount(ctx, groupID, 1); err != nil {
				s.logger.Warn("更新权限组用户数量失败",
					elog.Int64("group_id", groupID),
					elog.FieldErr(err))
			}
		}
	}

	s.logger.Info("创建云用户成功",
		elog.Int64("user_id", id),
		elog.String("username", req.Username))

	return &user, nil
}

func (s *cloudUserService) GetUser(ctx context.Context, id int64) (*domain.CloudUser, error) {
	s.logger.Debug("获取云用户详情", elog.Int64("user_id", id))

	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.UserNotFound
		}
		s.logger.Error("获取用户失败", elog.Int64("user_id", id), elog.FieldErr(err))
		return nil, fmt.Errorf("获取用户失败: %w", err)
	}

	return &user, nil
}

func (s *cloudUserService) ListUsers(ctx context.Context, filter domain.CloudUserFilter) ([]*domain.CloudUser, int64, error) {
	s.logger.Debug("获取云用户列表",
		elog.String("provider", string(filter.Provider)),
		elog.String("user_type", string(filter.UserType)),
		elog.String("status", string(filter.Status)))

	if filter.Limit == 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}

	users, total, err := s.userRepo.List(ctx, filter)
	if err != nil {
		s.logger.Error("获取用户列表失败", elog.FieldErr(err))
		return nil, 0, fmt.Errorf("获取用户列表失败: %w", err)
	}

	userPtrs := make([]*domain.CloudUser, len(users))
	for i := range users {
		userPtrs[i] = &users[i]
	}

	s.logger.Debug("获取云用户列表成功",
		elog.Int64("total", total),
		elog.Int("count", len(users)))

	return userPtrs, total, nil
}

func (s *cloudUserService) UpdateUser(ctx context.Context, id int64, req *domain.UpdateCloudUserRequest) error {
	s.logger.Info("更新云用户", elog.Int64("user_id", id))

	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.UserNotFound
		}
		s.logger.Error("获取用户失败", elog.FieldErr(err))
		return fmt.Errorf("获取用户失败: %w", err)
	}

	oldGroupIDs := user.UserGroups

	updated := false
	if req.DisplayName != nil && *req.DisplayName != user.DisplayName {
		user.DisplayName = *req.DisplayName
		updated = true
	}
	if req.Email != nil && *req.Email != user.Email {
		user.Email = *req.Email
		updated = true
	}
	if req.Status != nil && *req.Status != user.Status {
		user.Status = *req.Status
		updated = true
	}
	if req.UserGroups != nil {
		if len(req.UserGroups) > 0 {
			if err := s.validatePermissionGroups(ctx, req.UserGroups, user.TenantID); err != nil {
				return err
			}
		}
		user.UserGroups = req.UserGroups
		updated = true
	}

	if !updated {
		s.logger.Debug("用户信息无变更", elog.Int64("user_id", id))
		return nil
	}

	now := time.Now()
	user.UpdateTime = now
	user.UTime = now.Unix()

	if err := s.userRepo.Update(ctx, user); err != nil {
		s.logger.Error("更新用户失败", elog.FieldErr(err))
		return fmt.Errorf("更新用户失败: %w", err)
	}

	if req.UserGroups != nil {
		s.updateGroupUserCounts(ctx, oldGroupIDs, req.UserGroups)
	}

	s.logger.Info("更新云用户成功", elog.Int64("user_id", id))

	return nil
}

func (s *cloudUserService) DeleteUser(ctx context.Context, id int64) error {
	s.logger.Info("删除云用户", elog.Int64("user_id", id))

	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.UserNotFound
		}
		s.logger.Error("获取用户失败", elog.FieldErr(err))
		return fmt.Errorf("获取用户失败: %w", err)
	}

	if err := s.userRepo.Delete(ctx, id); err != nil {
		s.logger.Error("删除用户失败", elog.FieldErr(err))
		return fmt.Errorf("删除用户失败: %w", err)
	}

	if len(user.UserGroups) > 0 {
		for _, groupID := range user.UserGroups {
			if err := s.groupRepo.IncrementUserCount(ctx, groupID, -1); err != nil {
				s.logger.Warn("更新权限组用户数量失败",
					elog.Int64("group_id", groupID),
					elog.FieldErr(err))
			}
		}
	}

	s.logger.Info("删除云用户成功", elog.Int64("user_id", id))

	return nil
}

func (s *cloudUserService) validateCreateUserRequest(req *domain.CreateCloudUserRequest) error {
	if req.Username == "" {
		return fmt.Errorf("用户名不能为空")
	}
	if req.UserType == "" {
		return errs.UserInvalidType
	}
	if req.CloudAccountID == 0 {
		return fmt.Errorf("云账号ID不能为空")
	}
	if req.TenantID == "" {
		return fmt.Errorf("租户ID不能为空")
	}

	validTypes := map[domain.CloudUserType]bool{
		domain.CloudUserTypeAPIKey:    true,
		domain.CloudUserTypeAccessKey: true,
		domain.CloudUserTypeRAMUser:   true,
		domain.CloudUserTypeIAMUser:   true,
	}
	if !validTypes[req.UserType] {
		return errs.UserInvalidType
	}

	return nil
}

func (s *cloudUserService) validatePermissionGroups(ctx context.Context, groupIDs []int64, tenantID string) error {
	for _, groupID := range groupIDs {
		group, err := s.groupRepo.GetByID(ctx, groupID)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				return errs.PermissionGroupNotFound
			}
			return fmt.Errorf("获取权限组失败: %w", err)
		}
		if group.TenantID != tenantID {
			return errs.PermissionGroupNotFound
		}
	}
	return nil
}

func (s *cloudUserService) updateGroupUserCounts(ctx context.Context, oldGroupIDs, newGroupIDs []int64) {
	oldGroups := make(map[int64]bool)
	for _, id := range oldGroupIDs {
		oldGroups[id] = true
	}

	newGroups := make(map[int64]bool)
	for _, id := range newGroupIDs {
		newGroups[id] = true
	}

	for _, id := range oldGroupIDs {
		if !newGroups[id] {
			if err := s.groupRepo.IncrementUserCount(ctx, id, -1); err != nil {
				s.logger.Warn("减少权限组用户数量失败",
					elog.Int64("group_id", id),
					elog.FieldErr(err))
			}
		}
	}

	for _, id := range newGroupIDs {
		if !oldGroups[id] {
			if err := s.groupRepo.IncrementUserCount(ctx, id, 1); err != nil {
				s.logger.Warn("增加权限组用户数量失败",
					elog.Int64("group_id", id),
					elog.FieldErr(err))
			}
		}
	}
}

type UserSyncResult struct {
	TotalCount     int             `json:"total_count"`
	AddedCount     int             `json:"added_count"`
	UpdatedCount   int             `json:"updated_count"`
	DeletedCount   int             `json:"deleted_count"`
	UnchangedCount int             `json:"unchanged_count"`
	ErrorCount     int             `json:"error_count"`
	Errors         []UserSyncError `json:"errors"`
	Duration       time.Duration   `json:"duration"`
}

type UserSyncError struct {
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	Error     string    `json:"error"`
	Timestamp time.Time `json:"timestamp"`
}

func (s *cloudUserService) SyncUsers(ctx context.Context, cloudAccountID int64) (*UserSyncResult, error) {
	s.logger.Info("开始同步云用户", elog.Int64("cloud_account_id", cloudAccountID))

	startTime := time.Now()
	result := &UserSyncResult{
		Errors: make([]UserSyncError, 0),
	}

	account, err := s.accountRepo.GetByID(ctx, cloudAccountID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.AccountNotFound
		}
		s.logger.Error("获取云账号失败", elog.FieldErr(err))
		return nil, fmt.Errorf("获取云账号失败: %w", err)
	}

	if !account.IsActive() {
		return nil, errs.AccountDisabled
	}

	adapter, err := s.adapterFactory.CreateAdapter(account.Provider)
	if err != nil {
		s.logger.Error("创建适配器失败", elog.FieldErr(err))
		return nil, fmt.Errorf("创建适配器失败: %w", err)
	}

	cloudUsers, err := adapter.ListUsers(ctx, &account)
	if err != nil {
		s.logger.Error("获取云平台用户列表失败", elog.FieldErr(err))
		return nil, errs.UserSyncFailed
	}

	result.TotalCount = len(cloudUsers)

	filter := domain.CloudUserFilter{
		CloudAccountID: cloudAccountID,
		Limit:          1000,
	}
	existingUsers, _, err := s.userRepo.List(ctx, filter)
	if err != nil {
		s.logger.Error("获取数据库用户列表失败", elog.FieldErr(err))
		return nil, fmt.Errorf("获取数据库用户列表失败: %w", err)
	}

	existingUserMap := make(map[string]*domain.CloudUser)
	for i := range existingUsers {
		existingUserMap[existingUsers[i].CloudUserID] = &existingUsers[i]
	}

	cloudUserMap := make(map[string]*domain.CloudUser)
	for _, user := range cloudUsers {
		cloudUserMap[user.CloudUserID] = user
	}

	for _, cloudUser := range cloudUsers {
		existingUser, exists := existingUserMap[cloudUser.CloudUserID]
		if !exists {
			if err := s.createSyncedUser(ctx, cloudUser, &account); err != nil {
				s.logger.Error("创建同步用户失败",
					elog.String("cloud_user_id", cloudUser.CloudUserID),
					elog.FieldErr(err))
				result.ErrorCount++
				result.Errors = append(result.Errors, UserSyncError{
					UserID:    cloudUser.CloudUserID,
					Username:  cloudUser.Username,
					Error:     err.Error(),
					Timestamp: time.Now(),
				})
			} else {
				result.AddedCount++
			}
		} else {
			if s.isUserChanged(existingUser, cloudUser) {
				if err := s.updateSyncedUser(ctx, existingUser, cloudUser); err != nil {
					s.logger.Error("更新同步用户失败",
						elog.String("cloud_user_id", cloudUser.CloudUserID),
						elog.FieldErr(err))
					result.ErrorCount++
					result.Errors = append(result.Errors, UserSyncError{
						UserID:    cloudUser.CloudUserID,
						Username:  cloudUser.Username,
						Error:     err.Error(),
						Timestamp: time.Now(),
					})
				} else {
					result.UpdatedCount++
				}
			} else {
				result.UnchangedCount++
			}
		}
	}

	for cloudUserID, existingUser := range existingUserMap {
		if _, exists := cloudUserMap[cloudUserID]; !exists {
			if err := s.userRepo.UpdateStatus(ctx, existingUser.ID, domain.CloudUserStatusDeleted); err != nil {
				s.logger.Error("标记用户为已删除失败",
					elog.String("cloud_user_id", cloudUserID),
					elog.FieldErr(err))
				result.ErrorCount++
				result.Errors = append(result.Errors, UserSyncError{
					UserID:    cloudUserID,
					Username:  existingUser.Username,
					Error:     err.Error(),
					Timestamp: time.Now(),
				})
			} else {
				result.DeletedCount++
			}
		}
	}

	result.Duration = time.Since(startTime)

	s.logger.Info("云用户同步完成",
		elog.Int64("cloud_account_id", cloudAccountID),
		elog.Int("total", result.TotalCount),
		elog.Int("added", result.AddedCount),
		elog.Int("updated", result.UpdatedCount),
		elog.Int("deleted", result.DeletedCount),
		elog.Int("unchanged", result.UnchangedCount),
		elog.Int("errors", result.ErrorCount),
		elog.Duration("duration", result.Duration))

	return result, nil
}

func (s *cloudUserService) createSyncedUser(ctx context.Context, cloudUser *domain.CloudUser, account *domain.CloudAccount) error {
	now := time.Now()
	cloudUser.CloudAccountID = account.ID
	cloudUser.Provider = account.Provider
	cloudUser.Status = domain.CloudUserStatusActive
	cloudUser.TenantID = account.TenantID
	cloudUser.CreateTime = now
	cloudUser.UpdateTime = now
	cloudUser.CTime = now.Unix()
	cloudUser.UTime = now.Unix()

	cloudUser.Metadata.LastSyncTime = &now

	// 获取用户的个人权限策略
	adapter, err := s.adapterFactory.CreateAdapter(account.Provider)
	if err == nil {
		policies, err := adapter.GetUserPolicies(ctx, account, cloudUser.CloudUserID)
		if err != nil {
			s.logger.Warn("获取用户个人权限失败",
				elog.String("cloud_user_id", cloudUser.CloudUserID),
				elog.String("username", cloudUser.Username),
				elog.FieldErr(err))
			// 权限获取失败不影响用户同步，使用空列表
			cloudUser.Policies = []domain.PermissionPolicy{}
		} else {
			cloudUser.Policies = policies
			s.logger.Info("获取用户个人权限成功",
				elog.String("cloud_user_id", cloudUser.CloudUserID),
				elog.String("username", cloudUser.Username),
				elog.Int("policy_count", len(policies)))
		}
	} else {
		s.logger.Warn("创建适配器失败，跳过个人权限同步",
			elog.String("cloud_user_id", cloudUser.CloudUserID),
			elog.FieldErr(err))
		cloudUser.Policies = []domain.PermissionPolicy{}
	}

	id, err := s.userRepo.Create(ctx, *cloudUser)
	if err != nil {
		return err
	}

	cloudUser.ID = id
	return nil
}

func (s *cloudUserService) updateSyncedUser(ctx context.Context, existingUser, cloudUser *domain.CloudUser) error {
	cloudUser.ID = existingUser.ID
	cloudUser.UserGroups = existingUser.UserGroups
	cloudUser.TenantID = existingUser.TenantID
	cloudUser.CreateTime = existingUser.CreateTime
	cloudUser.CTime = existingUser.CTime

	now := time.Now()
	cloudUser.UpdateTime = now
	cloudUser.UTime = now.Unix()

	cloudUser.Metadata.LastSyncTime = &now

	// 获取用户的个人权限策略
	account, err := s.accountRepo.GetByID(ctx, cloudUser.CloudAccountID)
	if err == nil {
		adapter, err := s.adapterFactory.CreateAdapter(account.Provider)
		if err == nil {
			policies, err := adapter.GetUserPolicies(ctx, &account, cloudUser.CloudUserID)
			if err != nil {
				s.logger.Warn("获取用户个人权限失败",
					elog.String("cloud_user_id", cloudUser.CloudUserID),
					elog.String("username", cloudUser.Username),
					elog.FieldErr(err))
				// 权限获取失败时保留原有权限
				cloudUser.Policies = existingUser.Policies
			} else {
				cloudUser.Policies = policies
				s.logger.Debug("获取用户个人权限成功",
					elog.String("cloud_user_id", cloudUser.CloudUserID),
					elog.String("username", cloudUser.Username),
					elog.Int("policy_count", len(policies)))
			}
		} else {
			s.logger.Warn("创建适配器失败，保留原有权限",
				elog.String("cloud_user_id", cloudUser.CloudUserID),
				elog.FieldErr(err))
			cloudUser.Policies = existingUser.Policies
		}
	} else {
		s.logger.Warn("获取云账号失败，保留原有权限",
			elog.String("cloud_user_id", cloudUser.CloudUserID),
			elog.FieldErr(err))
		cloudUser.Policies = existingUser.Policies
	}

	return s.userRepo.Update(ctx, *cloudUser)
}

func (s *cloudUserService) isUserChanged(old, new *domain.CloudUser) bool {
	if old.Username != new.Username {
		return true
	}
	if old.DisplayName != new.DisplayName {
		return true
	}
	if old.Email != new.Email {
		return true
	}
	if old.UserType != new.UserType {
		return true
	}

	if old.Metadata.AccessKeyCount != new.Metadata.AccessKeyCount {
		return true
	}
	if old.Metadata.MFAEnabled != new.Metadata.MFAEnabled {
		return true
	}

	if len(old.Metadata.Tags) != len(new.Metadata.Tags) {
		return true
	}
	for k, v := range old.Metadata.Tags {
		if newV, ok := new.Metadata.Tags[k]; !ok || newV != v {
			return true
		}
	}

	// 检查个人权限策略是否变化
	if len(old.Policies) != len(new.Policies) {
		return true
	}
	oldPolicies := make(map[string]bool)
	for _, policy := range old.Policies {
		oldPolicies[policy.PolicyID] = true
	}
	for _, policy := range new.Policies {
		if !oldPolicies[policy.PolicyID] {
			return true
		}
	}

	return false
}

func (s *cloudUserService) AssignPermissionGroups(ctx context.Context, userIDs []int64, groupIDs []int64) error {
	s.logger.Info("批量分配权限组",
		elog.Int("user_count", len(userIDs)),
		elog.Int("group_count", len(groupIDs)))

	if len(userIDs) == 0 {
		return fmt.Errorf("用户ID列表不能为空")
	}
	if len(groupIDs) == 0 {
		return fmt.Errorf("权限组ID列表不能为空")
	}

	var tenantID string
	userGroupMap := make(map[int64][]int64)
	for _, userID := range userIDs {
		user, err := s.userRepo.GetByID(ctx, userID)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				return errs.UserNotFound
			}
			return fmt.Errorf("获取用户失败: %w", err)
		}
		if tenantID == "" {
			tenantID = user.TenantID
		} else if tenantID != user.TenantID {
			return fmt.Errorf("用户必须属于同一租户")
		}
		userGroupMap[userID] = user.UserGroups
	}

	if err := s.validatePermissionGroups(ctx, groupIDs, tenantID); err != nil {
		return err
	}

	if err := s.userRepo.BatchUpdatePermissionGroups(ctx, userIDs, groupIDs); err != nil {
		s.logger.Error("批量更新用户权限组失败", elog.FieldErr(err))
		return fmt.Errorf("批量更新用户权限组失败: %w", err)
	}

	s.updateBatchGroupUserCounts(ctx, userGroupMap, groupIDs)

	for _, userID := range userIDs {
		user, err := s.userRepo.GetByID(ctx, userID)
		if err != nil {
			s.logger.Warn("获取用户失败，跳过创建同步任务",
				elog.Int64("user_id", userID),
				elog.FieldErr(err))
			continue
		}

		now := time.Now()
		syncTask := domain.SyncTask{
			TaskType:       domain.SyncTaskTypePermissionSync,
			TargetType:     domain.SyncTargetTypeUser,
			TargetID:       userID,
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
				elog.Int64("user_id", userID),
				elog.FieldErr(err))
		}
	}

	s.logger.Info("批量分配权限组成功",
		elog.Int("user_count", len(userIDs)),
		elog.Int("group_count", len(groupIDs)))

	return nil
}

func (s *cloudUserService) updateBatchGroupUserCounts(ctx context.Context, userGroupMap map[int64][]int64, newGroupIDs []int64) {
	groupDelta := make(map[int64]int)

	newGroups := make(map[int64]bool)
	for _, id := range newGroupIDs {
		newGroups[id] = true
	}

	for _, oldGroupIDs := range userGroupMap {
		oldGroups := make(map[int64]bool)
		for _, id := range oldGroupIDs {
			oldGroups[id] = true
		}

		for _, id := range oldGroupIDs {
			if !newGroups[id] {
				groupDelta[id]--
			}
		}

		for _, id := range newGroupIDs {
			if !oldGroups[id] {
				groupDelta[id]++
			}
		}
	}

	for groupID, delta := range groupDelta {
		if delta != 0 {
			if err := s.groupRepo.IncrementUserCount(ctx, groupID, delta); err != nil {
				s.logger.Warn("更新权限组用户数量失败",
					elog.Int64("group_id", groupID),
					elog.Int("delta", delta),
					elog.FieldErr(err))
			}
		}
	}
}

// SyncUsersAsync 异步同步云用户
// 创建同步任务并立即返回任务ID，后台异步执行同步
func (s *cloudUserService) SyncUsersAsync(ctx context.Context, cloudAccountID int64) (*domain.SyncTask, error) {
	s.logger.Info("创建异步用户同步任务", elog.Int64("cloud_account_id", cloudAccountID))

	// 验证云账号
	account, err := s.accountRepo.GetByID(ctx, cloudAccountID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.AccountNotFound
		}
		s.logger.Error("获取云账号失败", elog.FieldErr(err))
		return nil, fmt.Errorf("获取云账号失败: %w", err)
	}

	if !account.IsActive() {
		return nil, errs.AccountDisabled
	}

	// 创建同步任务
	now := time.Now()
	task := domain.SyncTask{
		TaskType:       domain.SyncTaskTypeBatchUserSync,
		TargetType:     domain.SyncTargetTypeAccount,
		TargetID:       cloudAccountID,
		CloudAccountID: cloudAccountID,
		Provider:       account.Provider,
		Status:         domain.SyncTaskStatusPending,
		Progress:       0,
		RetryCount:     0,
		MaxRetries:     3,
		CreateTime:     now,
		UpdateTime:     now,
		CTime:          now.Unix(),
		UTime:          now.Unix(),
	}

	taskID, err := s.syncTaskRepo.Create(ctx, task)
	if err != nil {
		s.logger.Error("创建同步任务失败", elog.FieldErr(err))
		return nil, fmt.Errorf("创建同步任务失败: %w", err)
	}

	task.ID = taskID

	// 启动后台 goroutine 执行同步
	go func() {
		bgCtx := context.Background()
		if err := s.ExecuteBatchUserSync(bgCtx, taskID); err != nil {
			s.logger.Error("异步用户同步执行失败",
				elog.Int64("task_id", taskID),
				elog.FieldErr(err))
		}
	}()

	s.logger.Info("异步用户同步任务创建成功",
		elog.Int64("task_id", taskID),
		elog.Int64("cloud_account_id", cloudAccountID))

	return &task, nil
}

// ExecuteBatchUserSync 执行批量用户同步任务
func (s *cloudUserService) ExecuteBatchUserSync(ctx context.Context, taskID int64) error {
	s.logger.Info("开始执行批量用户同步任务", elog.Int64("task_id", taskID))

	// 获取任务信息
	task, err := s.syncTaskRepo.GetByID(ctx, taskID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.SyncTaskNotFound
		}
		s.logger.Error("获取同步任务失败", elog.FieldErr(err))
		return fmt.Errorf("获取同步任务失败: %w", err)
	}

	// 检查任务状态
	if task.Status == domain.SyncTaskStatusRunning {
		return errs.SyncTaskRunning
	}

	// 标记任务为执行中
	startTime := time.Now()
	if err := s.syncTaskRepo.MarkAsRunning(ctx, taskID, startTime); err != nil {
		s.logger.Error("标记任务为执行中失败", elog.FieldErr(err))
		return fmt.Errorf("标记任务为执行中失败: %w", err)
	}

	// 执行同步
	result, execErr := s.doSyncUsers(ctx, taskID, task.CloudAccountID)

	endTime := time.Now()
	if execErr != nil {
		s.logger.Error("批量用户同步执行失败",
			elog.Int64("task_id", taskID),
			elog.FieldErr(execErr))

		if err := s.syncTaskRepo.MarkAsFailed(ctx, taskID, endTime, execErr.Error()); err != nil {
			s.logger.Error("标记任务为失败状态失败", elog.FieldErr(err))
		}

		return execErr
	}

	// 标记任务成功
	if err := s.syncTaskRepo.MarkAsSuccess(ctx, taskID, endTime); err != nil {
		s.logger.Error("标记任务为成功状态失败", elog.FieldErr(err))
		return fmt.Errorf("标记任务为成功状态失败: %w", err)
	}

	s.logger.Info("批量用户同步任务执行成功",
		elog.Int64("task_id", taskID),
		elog.Int("total", result.TotalCount),
		elog.Int("added", result.AddedCount),
		elog.Int("updated", result.UpdatedCount),
		elog.Duration("duration", result.Duration))

	return nil
}

// doSyncUsers 执行实际的用户同步逻辑
func (s *cloudUserService) doSyncUsers(ctx context.Context, taskID int64, cloudAccountID int64) (*UserSyncResult, error) {
	startTime := time.Now()
	result := &UserSyncResult{
		Errors: make([]UserSyncError, 0),
	}

	// 获取云账号
	account, err := s.accountRepo.GetByID(ctx, cloudAccountID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.AccountNotFound
		}
		return nil, fmt.Errorf("获取云账号失败: %w", err)
	}

	if !account.IsActive() {
		return nil, errs.AccountDisabled
	}

	// 更新进度 10%
	_ = s.syncTaskRepo.UpdateProgress(ctx, taskID, 10)

	// 创建适配器
	adapter, err := s.adapterFactory.CreateAdapter(account.Provider)
	if err != nil {
		return nil, fmt.Errorf("创建适配器失败: %w", err)
	}

	// 更新进度 20%
	_ = s.syncTaskRepo.UpdateProgress(ctx, taskID, 20)

	// 获取云平台用户列表
	cloudUsers, err := adapter.ListUsers(ctx, &account)
	if err != nil {
		return nil, errs.UserSyncFailed
	}

	result.TotalCount = len(cloudUsers)

	// 更新进度 40%
	_ = s.syncTaskRepo.UpdateProgress(ctx, taskID, 40)

	// 获取数据库中已有的用户
	filter := domain.CloudUserFilter{
		CloudAccountID: cloudAccountID,
		Limit:          10000,
	}
	existingUsers, _, err := s.userRepo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("获取数据库用户列表失败: %w", err)
	}

	existingUserMap := make(map[string]*domain.CloudUser)
	for i := range existingUsers {
		existingUserMap[existingUsers[i].CloudUserID] = &existingUsers[i]
	}

	cloudUserMap := make(map[string]*domain.CloudUser)
	for _, user := range cloudUsers {
		cloudUserMap[user.CloudUserID] = user
	}

	// 更新进度 50%
	_ = s.syncTaskRepo.UpdateProgress(ctx, taskID, 50)

	// 处理云平台用户
	processedCount := 0
	totalToProcess := len(cloudUsers)
	for _, cloudUser := range cloudUsers {
		existingUser, exists := existingUserMap[cloudUser.CloudUserID]
		if !exists {
			if err := s.createSyncedUser(ctx, cloudUser, &account); err != nil {
				s.logger.Error("创建同步用户失败",
					elog.String("cloud_user_id", cloudUser.CloudUserID),
					elog.FieldErr(err))
				result.ErrorCount++
				result.Errors = append(result.Errors, UserSyncError{
					UserID:    cloudUser.CloudUserID,
					Username:  cloudUser.Username,
					Error:     err.Error(),
					Timestamp: time.Now(),
				})
			} else {
				result.AddedCount++
			}
		} else {
			if s.isUserChanged(existingUser, cloudUser) {
				if err := s.updateSyncedUser(ctx, existingUser, cloudUser); err != nil {
					s.logger.Error("更新同步用户失败",
						elog.String("cloud_user_id", cloudUser.CloudUserID),
						elog.FieldErr(err))
					result.ErrorCount++
					result.Errors = append(result.Errors, UserSyncError{
						UserID:    cloudUser.CloudUserID,
						Username:  cloudUser.Username,
						Error:     err.Error(),
						Timestamp: time.Now(),
					})
				} else {
					result.UpdatedCount++
				}
			} else {
				result.UnchangedCount++
			}
		}

		// 更新进度 50% - 90%
		processedCount++
		progress := 50 + (processedCount * 40 / totalToProcess)
		if progress > 90 {
			progress = 90
		}
		_ = s.syncTaskRepo.UpdateProgress(ctx, taskID, progress)
	}

	// 处理已删除的用户
	for cloudUserID, existingUser := range existingUserMap {
		if _, exists := cloudUserMap[cloudUserID]; !exists {
			if err := s.userRepo.UpdateStatus(ctx, existingUser.ID, domain.CloudUserStatusDeleted); err != nil {
				s.logger.Error("标记用户为已删除失败",
					elog.String("cloud_user_id", cloudUserID),
					elog.FieldErr(err))
				result.ErrorCount++
				result.Errors = append(result.Errors, UserSyncError{
					UserID:    cloudUserID,
					Username:  existingUser.Username,
					Error:     err.Error(),
					Timestamp: time.Now(),
				})
			} else {
				result.DeletedCount++
			}
		}
	}

	// 更新进度 100%
	_ = s.syncTaskRepo.UpdateProgress(ctx, taskID, 100)

	result.Duration = time.Since(startTime)

	s.logger.Info("云用户同步完成",
		elog.Int64("cloud_account_id", cloudAccountID),
		elog.Int("total", result.TotalCount),
		elog.Int("added", result.AddedCount),
		elog.Int("updated", result.UpdatedCount),
		elog.Int("deleted", result.DeletedCount),
		elog.Int("unchanged", result.UnchangedCount),
		elog.Int("errors", result.ErrorCount),
		elog.Duration("duration", result.Duration))

	return result, nil
}
