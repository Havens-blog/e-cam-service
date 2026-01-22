package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/iam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// SyncTaskRepository 同步任务仓储接口
type SyncTaskRepository interface {
	// Create 创建同步任务
	Create(ctx context.Context, task domain.SyncTask) (int64, error)

	// GetByID 根据ID获取同步任务
	GetByID(ctx context.Context, id int64) (domain.SyncTask, error)

	// List 获取同步任务列表
	List(ctx context.Context, filter domain.SyncTaskFilter) ([]domain.SyncTask, int64, error)

	// Update 更新同步任务
	Update(ctx context.Context, task domain.SyncTask) error

	// UpdateStatus 更新任务状态
	UpdateStatus(ctx context.Context, id int64, status domain.SyncTaskStatus) error

	// UpdateProgress 更新任务进度
	UpdateProgress(ctx context.Context, id int64, progress int) error

	// MarkAsRunning 标记任务为执行中
	MarkAsRunning(ctx context.Context, id int64, startTime time.Time) error

	// MarkAsSuccess 标记任务为成功
	MarkAsSuccess(ctx context.Context, id int64, endTime time.Time) error

	// MarkAsFailed 标记任务为失败
	MarkAsFailed(ctx context.Context, id int64, endTime time.Time, errorMsg string) error

	// IncrementRetry 增加重试次数
	IncrementRetry(ctx context.Context, id int64) error

	// ListPendingTasks 获取待执行的任务列表
	ListPendingTasks(ctx context.Context, limit int64) ([]domain.SyncTask, error)

	// ListFailedRetryableTasks 获取失败但可重试的任务列表
	ListFailedRetryableTasks(ctx context.Context, limit int64) ([]domain.SyncTask, error)
}

type syncTaskRepository struct {
	dao dao.SyncTaskDAO
}

// NewSyncTaskRepository 创建同步任务仓储
func NewSyncTaskRepository(dao dao.SyncTaskDAO) SyncTaskRepository {
	return &syncTaskRepository{
		dao: dao,
	}
}

// Create 创建同步任务
func (repo *syncTaskRepository) Create(ctx context.Context, task domain.SyncTask) (int64, error) {
	daoTask := repo.toEntity(task)
	return repo.dao.Create(ctx, daoTask)
}

// GetByID 根据ID获取同步任务
func (repo *syncTaskRepository) GetByID(ctx context.Context, id int64) (domain.SyncTask, error) {
	daoTask, err := repo.dao.GetByID(ctx, id)
	if err != nil {
		return domain.SyncTask{}, err
	}
	return repo.toDomain(daoTask), nil
}

// List 获取同步任务列表
func (repo *syncTaskRepository) List(ctx context.Context, filter domain.SyncTaskFilter) ([]domain.SyncTask, int64, error) {
	daoFilter := dao.SyncTaskFilter{
		TaskType:       dao.SyncTaskType(filter.TaskType),
		Status:         dao.SyncTaskStatus(filter.Status),
		CloudAccountID: filter.CloudAccountID,
		Provider:       dao.CloudProvider(filter.Provider),
		Offset:         filter.Offset,
		Limit:          filter.Limit,
	}

	daoTasks, err := repo.dao.List(ctx, daoFilter)
	if err != nil {
		return nil, 0, err
	}

	count, err := repo.dao.Count(ctx, daoFilter)
	if err != nil {
		return nil, 0, err
	}

	tasks := make([]domain.SyncTask, len(daoTasks))
	for i, daoTask := range daoTasks {
		tasks[i] = repo.toDomain(daoTask)
	}

	return tasks, count, nil
}

// Update 更新同步任务
func (repo *syncTaskRepository) Update(ctx context.Context, task domain.SyncTask) error {
	daoTask := repo.toEntity(task)
	return repo.dao.Update(ctx, daoTask)
}

// UpdateStatus 更新任务状态
func (repo *syncTaskRepository) UpdateStatus(ctx context.Context, id int64, status domain.SyncTaskStatus) error {
	return repo.dao.UpdateStatus(ctx, id, dao.SyncTaskStatus(status))
}

// UpdateProgress 更新任务进度
func (repo *syncTaskRepository) UpdateProgress(ctx context.Context, id int64, progress int) error {
	return repo.dao.UpdateProgress(ctx, id, progress)
}

// MarkAsRunning 标记任务为执行中
func (repo *syncTaskRepository) MarkAsRunning(ctx context.Context, id int64, startTime time.Time) error {
	return repo.dao.MarkAsRunning(ctx, id, startTime)
}

// MarkAsSuccess 标记任务为成功
func (repo *syncTaskRepository) MarkAsSuccess(ctx context.Context, id int64, endTime time.Time) error {
	return repo.dao.MarkAsSuccess(ctx, id, endTime)
}

// MarkAsFailed 标记任务为失败
func (repo *syncTaskRepository) MarkAsFailed(ctx context.Context, id int64, endTime time.Time, errorMsg string) error {
	return repo.dao.MarkAsFailed(ctx, id, endTime, errorMsg)
}

// IncrementRetry 增加重试次数
func (repo *syncTaskRepository) IncrementRetry(ctx context.Context, id int64) error {
	return repo.dao.IncrementRetry(ctx, id)
}

// ListPendingTasks 获取待执行的任务列表
func (repo *syncTaskRepository) ListPendingTasks(ctx context.Context, limit int64) ([]domain.SyncTask, error) {
	daoTasks, err := repo.dao.ListPendingTasks(ctx, limit)
	if err != nil {
		return nil, err
	}

	tasks := make([]domain.SyncTask, len(daoTasks))
	for i, daoTask := range daoTasks {
		tasks[i] = repo.toDomain(daoTask)
	}

	return tasks, nil
}

// ListFailedRetryableTasks 获取失败但可重试的任务列表
func (repo *syncTaskRepository) ListFailedRetryableTasks(ctx context.Context, limit int64) ([]domain.SyncTask, error) {
	daoTasks, err := repo.dao.ListFailedRetryableTasks(ctx, limit)
	if err != nil {
		return nil, err
	}

	tasks := make([]domain.SyncTask, len(daoTasks))
	for i, daoTask := range daoTasks {
		tasks[i] = repo.toDomain(daoTask)
	}

	return tasks, nil
}

// toDomain 转换为领域模型
func (repo *syncTaskRepository) toDomain(daoTask dao.SyncTask) domain.SyncTask {
	return domain.SyncTask{
		ID:             daoTask.ID,
		TaskType:       domain.SyncTaskType(daoTask.TaskType),
		TargetType:     domain.SyncTargetType(daoTask.TargetType),
		TargetID:       daoTask.TargetID,
		CloudAccountID: daoTask.CloudAccountID,
		Provider:       domain.CloudProvider(daoTask.Provider),
		Status:         domain.SyncTaskStatus(daoTask.Status),
		Progress:       daoTask.Progress,
		RetryCount:     daoTask.RetryCount,
		MaxRetries:     daoTask.MaxRetries,
		ErrorMessage:   daoTask.ErrorMessage,
		StartTime:      daoTask.StartTime,
		EndTime:        daoTask.EndTime,
		CreateTime:     daoTask.CreateTime,
		UpdateTime:     daoTask.UpdateTime,
		CTime:          daoTask.CTime,
		UTime:          daoTask.UTime,
	}
}

// toEntity 转换为DAO实体
func (repo *syncTaskRepository) toEntity(task domain.SyncTask) dao.SyncTask {
	return dao.SyncTask{
		ID:             task.ID,
		TaskType:       dao.SyncTaskType(task.TaskType),
		TargetType:     dao.SyncTargetType(task.TargetType),
		TargetID:       task.TargetID,
		CloudAccountID: task.CloudAccountID,
		Provider:       dao.CloudProvider(task.Provider),
		Status:         dao.SyncTaskStatus(task.Status),
		Progress:       task.Progress,
		RetryCount:     task.RetryCount,
		MaxRetries:     task.MaxRetries,
		ErrorMessage:   task.ErrorMessage,
		StartTime:      task.StartTime,
		EndTime:        task.EndTime,
		CreateTime:     task.CreateTime,
		UpdateTime:     task.UpdateTime,
		CTime:          task.CTime,
		UTime:          task.UTime,
	}
}
