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

// SyncService 同步服务接口
type SyncService interface {
	CreateSyncTask(ctx context.Context, req *CreateSyncTaskRequest) (*domain.SyncTask, error)
	ExecuteSyncTask(ctx context.Context, taskID int64) error
	GetSyncTaskStatus(ctx context.Context, taskID int64) (*domain.SyncTask, error)
	ListSyncTasks(ctx context.Context, filter domain.SyncTaskFilter) ([]*domain.SyncTask, int64, error)
	RetrySyncTask(ctx context.Context, taskID int64) error
	SyncPermissionChanges(ctx context.Context, groupID int64, userIDs []int64) ([]*domain.SyncTask, error)
}

type syncService struct {
	syncTaskRepo   iamrepo.SyncTaskRepository
	userRepo       iamrepo.CloudUserRepository
	groupRepo      iamrepo.UserGroupRepository
	accountRepo    repository.CloudAccountRepository
	adapterFactory iam.CloudIAMAdapterFactory
	logger         *elog.Component
}

// CreateSyncTaskRequest 创建同步任务请求
type CreateSyncTaskRequest struct {
	TaskType       domain.SyncTaskType   `json:"task_type"`
	TargetType     domain.SyncTargetType `json:"target_type"`
	TargetID       int64                 `json:"target_id"`
	CloudAccountID int64                 `json:"cloud_account_id"`
	Provider       domain.CloudProvider  `json:"provider"`
}

func NewSyncService(
	syncTaskRepo iamrepo.SyncTaskRepository,
	userRepo iamrepo.CloudUserRepository,
	groupRepo iamrepo.UserGroupRepository,
	accountRepo repository.CloudAccountRepository,
	adapterFactory iam.CloudIAMAdapterFactory,
	logger *elog.Component,
) SyncService {
	return &syncService{
		syncTaskRepo:   syncTaskRepo,
		userRepo:       userRepo,
		groupRepo:      groupRepo,
		accountRepo:    accountRepo,
		adapterFactory: adapterFactory,
		logger:         logger,
	}
}

func (s *syncService) CreateSyncTask(ctx context.Context, req *CreateSyncTaskRequest) (*domain.SyncTask, error) {
	s.logger.Info("创建同步任务",
		elog.String("task_type", string(req.TaskType)),
		elog.String("target_type", string(req.TargetType)),
		elog.Int64("target_id", req.TargetID))

	if err := s.validateCreateSyncTaskRequest(req); err != nil {
		s.logger.Error("创建同步任务参数验证失败", elog.FieldErr(err))
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

	if !account.IsActive() {
		return nil, errs.AccountDisabled
	}

	now := time.Now()
	task := domain.SyncTask{
		TaskType:       req.TaskType,
		TargetType:     req.TargetType,
		TargetID:       req.TargetID,
		CloudAccountID: req.CloudAccountID,
		Provider:       req.Provider,
		Status:         domain.SyncTaskStatusPending,
		Progress:       0,
		RetryCount:     0,
		MaxRetries:     3,
		CreateTime:     now,
		UpdateTime:     now,
		CTime:          now.Unix(),
		UTime:          now.Unix(),
	}

	id, err := s.syncTaskRepo.Create(ctx, task)
	if err != nil {
		s.logger.Error("创建同步任务失败", elog.FieldErr(err))
		return nil, fmt.Errorf("创建同步任务失败: %w", err)
	}

	task.ID = id

	s.logger.Info("创建同步任务成功",
		elog.Int64("task_id", id),
		elog.String("task_type", string(req.TaskType)))

	return &task, nil
}

func (s *syncService) GetSyncTaskStatus(ctx context.Context, taskID int64) (*domain.SyncTask, error) {
	s.logger.Debug("获取同步任务状态", elog.Int64("task_id", taskID))

	task, err := s.syncTaskRepo.GetByID(ctx, taskID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.SyncTaskNotFound
		}
		s.logger.Error("获取同步任务失败", elog.Int64("task_id", taskID), elog.FieldErr(err))
		return nil, fmt.Errorf("获取同步任务失败: %w", err)
	}

	return &task, nil
}

func (s *syncService) ListSyncTasks(ctx context.Context, filter domain.SyncTaskFilter) ([]*domain.SyncTask, int64, error) {
	s.logger.Debug("获取同步任务列表",
		elog.String("task_type", string(filter.TaskType)),
		elog.String("status", string(filter.Status)))

	if filter.Limit == 0 {
		filter.Limit = 20
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}

	tasks, total, err := s.syncTaskRepo.List(ctx, filter)
	if err != nil {
		s.logger.Error("获取同步任务列表失败", elog.FieldErr(err))
		return nil, 0, fmt.Errorf("获取同步任务列表失败: %w", err)
	}

	taskPtrs := make([]*domain.SyncTask, len(tasks))
	for i := range tasks {
		taskPtrs[i] = &tasks[i]
	}

	s.logger.Debug("获取同步任务列表成功",
		elog.Int64("total", total),
		elog.Int("count", len(tasks)))

	return taskPtrs, total, nil
}

func (s *syncService) RetrySyncTask(ctx context.Context, taskID int64) error {
	s.logger.Info("重试同步任务", elog.Int64("task_id", taskID))

	task, err := s.syncTaskRepo.GetByID(ctx, taskID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.SyncTaskNotFound
		}
		s.logger.Error("获取同步任务失败", elog.FieldErr(err))
		return fmt.Errorf("获取同步任务失败: %w", err)
	}

	if task.Status != domain.SyncTaskStatusFailed {
		return fmt.Errorf("只能重试失败的任务")
	}

	if task.RetryCount >= task.MaxRetries {
		return errs.SyncTaskMaxRetries
	}

	if err := s.syncTaskRepo.IncrementRetry(ctx, taskID); err != nil {
		s.logger.Error("增加重试次数失败", elog.FieldErr(err))
		return fmt.Errorf("增加重试次数失败: %w", err)
	}

	if err := s.syncTaskRepo.UpdateStatus(ctx, taskID, domain.SyncTaskStatusPending); err != nil {
		s.logger.Error("更新任务状态失败", elog.FieldErr(err))
		return fmt.Errorf("更新任务状态失败: %w", err)
	}

	s.logger.Info("重试同步任务成功", elog.Int64("task_id", taskID))

	return nil
}

func (s *syncService) validateCreateSyncTaskRequest(req *CreateSyncTaskRequest) error {
	if req.TaskType == "" {
		return fmt.Errorf("任务类型不能为空")
	}
	if req.TargetType == "" {
		return fmt.Errorf("目标类型不能为空")
	}
	if req.TargetID == 0 {
		return fmt.Errorf("目标ID不能为空")
	}
	if req.CloudAccountID == 0 {
		return fmt.Errorf("云账号ID不能为空")
	}
	if req.Provider == "" {
		return fmt.Errorf("云厂商不能为空")
	}

	validTaskTypes := map[domain.SyncTaskType]bool{
		domain.SyncTaskTypeUserSync:       true,
		domain.SyncTaskTypePermissionSync: true,
		domain.SyncTaskTypeBatchUserSync:  true,
	}
	if !validTaskTypes[req.TaskType] {
		return fmt.Errorf("无效的任务类型")
	}

	validTargetTypes := map[domain.SyncTargetType]bool{
		domain.SyncTargetTypeUser:    true,
		domain.SyncTargetTypeGroup:   true,
		domain.SyncTargetTypeAccount: true,
	}
	if !validTargetTypes[req.TargetType] {
		return fmt.Errorf("无效的目标类型")
	}

	return nil
}

func (s *syncService) ExecuteSyncTask(ctx context.Context, taskID int64) error {
	s.logger.Info("执行同步任务", elog.Int64("task_id", taskID))

	task, err := s.syncTaskRepo.GetByID(ctx, taskID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.SyncTaskNotFound
		}
		s.logger.Error("获取同步任务失败", elog.FieldErr(err))
		return fmt.Errorf("获取同步任务失败: %w", err)
	}

	if task.Status == domain.SyncTaskStatusRunning {
		return errs.SyncTaskRunning
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	startTime := time.Now()
	if err := s.syncTaskRepo.MarkAsRunning(ctx, taskID, startTime); err != nil {
		s.logger.Error("标记任务为执行中失败", elog.FieldErr(err))
		return fmt.Errorf("标记任务为执行中失败: %w", err)
	}

	var execErr error
	switch task.TaskType {
	case domain.SyncTaskTypeUserSync:
		execErr = s.executeUserSync(ctx, &task)
	case domain.SyncTaskTypePermissionSync:
		execErr = s.executePermissionSync(ctx, &task)
	default:
		execErr = fmt.Errorf("不支持的任务类型: %s", task.TaskType)
	}

	endTime := time.Now()
	if execErr != nil {
		s.logger.Error("同步任务执行失败",
			elog.Int64("task_id", taskID),
			elog.FieldErr(execErr))

		if err := s.syncTaskRepo.MarkAsFailed(ctx, taskID, endTime, execErr.Error()); err != nil {
			s.logger.Error("标记任务为失败状态失败", elog.FieldErr(err))
		}

		return execErr
	}

	if err := s.syncTaskRepo.MarkAsSuccess(ctx, taskID, endTime); err != nil {
		s.logger.Error("标记任务为成功状态失败", elog.FieldErr(err))
		return fmt.Errorf("标记任务为成功状态失败: %w", err)
	}

	s.logger.Info("同步任务执行成功",
		elog.Int64("task_id", taskID),
		elog.Duration("duration", endTime.Sub(startTime)))

	return nil
}

func (s *syncService) executeUserSync(ctx context.Context, task *domain.SyncTask) error {
	s.logger.Info("执行用户同步",
		elog.Int64("task_id", task.ID),
		elog.Int64("target_id", task.TargetID))

	user, err := s.userRepo.GetByID(ctx, task.TargetID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.UserNotFound
		}
		return fmt.Errorf("获取用户失败: %w", err)
	}

	account, err := s.accountRepo.GetByID(ctx, task.CloudAccountID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.AccountNotFound
		}
		return fmt.Errorf("获取云账号失败: %w", err)
	}

	adapter, err := s.adapterFactory.CreateAdapter(task.Provider)
	if err != nil {
		return fmt.Errorf("创建适配器失败: %w", err)
	}

	if err := s.syncTaskRepo.UpdateProgress(ctx, task.ID, 30); err != nil {
		s.logger.Warn("更新任务进度失败", elog.FieldErr(err))
	}

	cloudUser, err := adapter.GetUser(ctx, &account, user.CloudUserID)
	if err != nil {
		return fmt.Errorf("获取云平台用户失败: %w", err)
	}

	if err := s.syncTaskRepo.UpdateProgress(ctx, task.ID, 60); err != nil {
		s.logger.Warn("更新任务进度失败", elog.FieldErr(err))
	}

	user.DisplayName = cloudUser.DisplayName
	user.Email = cloudUser.Email
	user.Metadata = cloudUser.Metadata

	now := time.Now()
	user.UpdateTime = now
	user.UTime = now.Unix()
	user.Metadata.LastSyncTime = &now

	if err := s.userRepo.Update(ctx, user); err != nil {
		return fmt.Errorf("更新用户失败: %w", err)
	}

	if err := s.syncTaskRepo.UpdateProgress(ctx, task.ID, 100); err != nil {
		s.logger.Warn("更新任务进度失败", elog.FieldErr(err))
	}

	s.logger.Info("用户同步完成",
		elog.Int64("task_id", task.ID),
		elog.Int64("user_id", user.ID))

	return nil
}

func (s *syncService) executePermissionSync(ctx context.Context, task *domain.SyncTask) error {
	s.logger.Info("执行权限同步",
		elog.Int64("task_id", task.ID),
		elog.Int64("target_id", task.TargetID))

	var user domain.CloudUser
	var err error

	switch task.TargetType {
	case domain.SyncTargetTypeUser:
		user, err = s.userRepo.GetByID(ctx, task.TargetID)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				return errs.UserNotFound
			}
			return fmt.Errorf("获取用户失败: %w", err)
		}
	default:
		return fmt.Errorf("不支持的目标类型: %s", task.TargetType)
	}

	account, err := s.accountRepo.GetByID(ctx, task.CloudAccountID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errs.AccountNotFound
		}
		return fmt.Errorf("获取云账号失败: %w", err)
	}

	adapter, err := s.adapterFactory.CreateAdapter(task.Provider)
	if err != nil {
		return fmt.Errorf("创建适配器失败: %w", err)
	}

	if err := s.syncTaskRepo.UpdateProgress(ctx, task.ID, 20); err != nil {
		s.logger.Warn("更新任务进度失败", elog.FieldErr(err))
	}

	var allPolicies []domain.PermissionPolicy
	for _, groupID := range user.UserGroups {
		group, err := s.groupRepo.GetByID(ctx, groupID)
		if err != nil {
			s.logger.Warn("获取权限组失败",
				elog.Int64("group_id", groupID),
				elog.FieldErr(err))
			continue
		}

		for _, policy := range group.Policies {
			if policy.Provider == task.Provider {
				allPolicies = append(allPolicies, policy)
			}
		}
	}

	if err := s.syncTaskRepo.UpdateProgress(ctx, task.ID, 50); err != nil {
		s.logger.Warn("更新任务进度失败", elog.FieldErr(err))
	}

	if err := adapter.UpdateUserPermissions(ctx, &account, user.CloudUserID, allPolicies); err != nil {
		return fmt.Errorf("更新云平台用户权限失败: %w", err)
	}

	if err := s.syncTaskRepo.UpdateProgress(ctx, task.ID, 100); err != nil {
		s.logger.Warn("更新任务进度失败", elog.FieldErr(err))
	}

	s.logger.Info("权限同步完成",
		elog.Int64("task_id", task.ID),
		elog.Int64("user_id", user.ID),
		elog.Int("policy_count", len(allPolicies)))

	return nil
}

func (s *syncService) SyncPermissionChanges(ctx context.Context, groupID int64, userIDs []int64) ([]*domain.SyncTask, error) {
	s.logger.Info("批量同步权限变更",
		elog.Int64("group_id", groupID),
		elog.Int("user_count", len(userIDs)))

	if len(userIDs) == 0 {
		return nil, fmt.Errorf("用户ID列表不能为空")
	}

	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errs.PermissionGroupNotFound
		}
		return nil, fmt.Errorf("获取权限组失败: %w", err)
	}

	tasks := make([]*domain.SyncTask, 0, len(userIDs))

	for _, userID := range userIDs {
		user, err := s.userRepo.GetByID(ctx, userID)
		if err != nil {
			s.logger.Warn("获取用户失败，跳过",
				elog.Int64("user_id", userID),
				elog.FieldErr(err))
			continue
		}

		hasGroup := false
		for _, gid := range user.UserGroups {
			if gid == groupID {
				hasGroup = true
				break
			}
		}

		if !hasGroup {
			s.logger.Debug("用户不属于该权限组，跳过",
				elog.Int64("user_id", userID),
				elog.Int64("group_id", groupID))
			continue
		}

		for _, platform := range group.CloudPlatforms {
			req := &CreateSyncTaskRequest{
				TaskType:       domain.SyncTaskTypePermissionSync,
				TargetType:     domain.SyncTargetTypeUser,
				TargetID:       userID,
				CloudAccountID: user.CloudAccountID,
				Provider:       platform,
			}

			task, err := s.CreateSyncTask(ctx, req)
			if err != nil {
				s.logger.Warn("创建同步任务失败",
					elog.Int64("user_id", userID),
					elog.String("provider", string(platform)),
					elog.FieldErr(err))
				continue
			}

			tasks = append(tasks, task)
		}
	}

	s.logger.Info("批量同步权限变更完成",
		elog.Int64("group_id", groupID),
		elog.Int("task_count", len(tasks)))

	return tasks, nil
}

func (s *syncService) ProcessFailedTasks(ctx context.Context) error {
	s.logger.Info("开始处理失败的同步任务")

	tasks, err := s.syncTaskRepo.ListFailedRetryableTasks(ctx, 10)
	if err != nil {
		s.logger.Error("获取失败任务列表失败", elog.FieldErr(err))
		return fmt.Errorf("获取失败任务列表失败: %w", err)
	}

	if len(tasks) == 0 {
		s.logger.Debug("没有需要重试的失败任务")
		return nil
	}

	s.logger.Info("找到需要重试的失败任务", elog.Int("count", len(tasks)))

	for _, task := range tasks {
		if task.RetryCount >= task.MaxRetries {
			s.logger.Debug("任务已达到最大重试次数，跳过",
				elog.Int64("task_id", task.ID),
				elog.Int("retry_count", task.RetryCount))
			continue
		}

		backoffDuration := s.calculateBackoff(task.RetryCount)
		if task.EndTime != nil {
			timeSinceFailure := time.Since(*task.EndTime)
			if timeSinceFailure < backoffDuration {
				s.logger.Debug("任务还在退避期内，跳过",
					elog.Int64("task_id", task.ID),
					elog.Duration("wait_time", backoffDuration-timeSinceFailure))
				continue
			}
		}

		s.logger.Info("重试失败任务",
			elog.Int64("task_id", task.ID),
			elog.Int("retry_count", task.RetryCount+1))

		if err := s.RetrySyncTask(ctx, task.ID); err != nil {
			s.logger.Error("重试任务失败",
				elog.Int64("task_id", task.ID),
				elog.FieldErr(err))
			continue
		}

		if err := s.ExecuteSyncTask(ctx, task.ID); err != nil {
			s.logger.Error("执行重试任务失败",
				elog.Int64("task_id", task.ID),
				elog.FieldErr(err))
		}
	}

	s.logger.Info("处理失败任务完成")

	return nil
}

func (s *syncService) calculateBackoff(retryCount int) time.Duration {
	baseDelay := 10 * time.Second
	maxDelay := 5 * time.Minute

	delay := baseDelay * time.Duration(1<<uint(retryCount))

	if delay > maxDelay {
		delay = maxDelay
	}

	return delay
}

func (s *syncService) ProcessPendingTasks(ctx context.Context, maxConcurrent int) error {
	s.logger.Info("开始处理待执行的同步任务", elog.Int("max_concurrent", maxConcurrent))

	tasks, err := s.syncTaskRepo.ListPendingTasks(ctx, int64(maxConcurrent))
	if err != nil {
		s.logger.Error("获取待执行任务列表失败", elog.FieldErr(err))
		return fmt.Errorf("获取待执行任务列表失败: %w", err)
	}

	if len(tasks) == 0 {
		s.logger.Debug("没有待执行的任务")
		return nil
	}

	s.logger.Info("找到待执行的任务", elog.Int("count", len(tasks)))

	for _, task := range tasks {
		s.logger.Info("执行待处理任务", elog.Int64("task_id", task.ID))

		if err := s.ExecuteSyncTask(ctx, task.ID); err != nil {
			s.logger.Error("执行任务失败",
				elog.Int64("task_id", task.ID),
				elog.FieldErr(err))
		}
	}

	s.logger.Info("处理待执行任务完成")

	return nil
}
