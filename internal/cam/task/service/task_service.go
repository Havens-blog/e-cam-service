package service

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/task"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/google/uuid"
	"github.com/gotomicro/ego/core/elog"
)

// TaskService 任务服务接口
type TaskService interface {
	// SubmitSyncAssetsTask 提交同步资产任务
	SubmitSyncAssetsTask(ctx context.Context, params task.SyncAssetsParams, createdBy string) (string, error)

	// SubmitDiscoverAssetsTask 提交发现资产任务
	SubmitDiscoverAssetsTask(ctx context.Context, params task.DiscoverAssetsParams, createdBy string) (string, error)

	// GetTask 获取任务
	GetTask(ctx context.Context, taskID string) (*taskx.Task, error)

	// ListTasks 获取任务列表
	ListTasks(ctx context.Context, filter taskx.TaskFilter) ([]taskx.Task, int64, error)

	// CancelTask 取消任务
	CancelTask(ctx context.Context, taskID string) error

	// DeleteTask 删除任务
	DeleteTask(ctx context.Context, taskID string) error
}

type taskService struct {
	queue  *taskx.Queue
	repo   taskx.TaskRepository
	logger *elog.Component
}

// NewTaskService 创建任务服务
func NewTaskService(
	queue *taskx.Queue,
	repo taskx.TaskRepository,
	logger *elog.Component,
) TaskService {
	return &taskService{
		queue:  queue,
		repo:   repo,
		logger: logger,
	}
}

// SubmitSyncAssetsTask 提交同步资产任务
func (s *taskService) SubmitSyncAssetsTask(ctx context.Context, params task.SyncAssetsParams, createdBy string) (string, error) {
	s.logger.Info("提交同步资产任务",
		elog.String("provider", params.Provider),
		elog.Any("asset_types", params.AssetTypes),
		elog.String("created_by", createdBy))

	// 生成任务ID
	taskID := uuid.New().String()

	// 将参数转换为 map
	paramsMap := map[string]interface{}{
		"provider":    params.Provider,
		"asset_types": params.AssetTypes,
		"regions":     params.Regions,
		"account_id":  params.AccountID,
	}

	// 创建任务
	t := &taskx.Task{
		ID:        taskID,
		Type:      task.TaskTypeSyncAssets,
		Status:    taskx.TaskStatusPending,
		Params:    paramsMap,
		Progress:  0,
		Message:   "任务已创建，等待执行",
		CreatedBy: createdBy,
	}

	// 提交任务到队列
	if err := s.queue.Submit(t); err != nil {
		return "", fmt.Errorf("提交任务失败: %w", err)
	}

	s.logger.Info("同步资产任务已提交",
		elog.String("task_id", taskID))

	return taskID, nil
}

// SubmitDiscoverAssetsTask 提交发现资产任务
func (s *taskService) SubmitDiscoverAssetsTask(ctx context.Context, params task.DiscoverAssetsParams, createdBy string) (string, error) {
	s.logger.Info("提交发现资产任务",
		elog.String("provider", params.Provider),
		elog.String("region", params.Region),
		elog.Any("asset_types", params.AssetTypes),
		elog.String("created_by", createdBy))

	// 生成任务ID
	taskID := uuid.New().String()

	// 将参数转换为 map
	paramsMap := map[string]interface{}{
		"provider":    params.Provider,
		"region":      params.Region,
		"asset_types": params.AssetTypes,
		"account_id":  params.AccountID,
	}

	// 创建任务
	t := &taskx.Task{
		ID:        taskID,
		Type:      task.TaskTypeDiscoverAssets,
		Status:    taskx.TaskStatusPending,
		Params:    paramsMap,
		Progress:  0,
		Message:   "任务已创建，等待执行",
		CreatedBy: createdBy,
	}

	// 提交任务到队列
	if err := s.queue.Submit(t); err != nil {
		return "", fmt.Errorf("提交任务失败: %w", err)
	}

	s.logger.Info("发现资产任务已提交",
		elog.String("task_id", taskID))

	return taskID, nil
}

// GetTask 获取任务
func (s *taskService) GetTask(ctx context.Context, taskID string) (*taskx.Task, error) {
	return s.queue.GetTaskStatus(taskID)
}

// ListTasks 获取任务列表
func (s *taskService) ListTasks(ctx context.Context, filter taskx.TaskFilter) ([]taskx.Task, int64, error) {
	return s.queue.ListTasks(filter)
}

// CancelTask 取消任务
func (s *taskService) CancelTask(ctx context.Context, taskID string) error {
	return s.queue.CancelTask(taskID)
}

// DeleteTask 删除任务
func (s *taskService) DeleteTask(ctx context.Context, taskID string) error {
	return s.repo.Delete(ctx, taskID)
}
