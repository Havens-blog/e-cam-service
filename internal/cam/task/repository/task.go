package repository

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/cam/task/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/task/repository/dao"
)

// TaskRepository 任务仓储接口
type TaskRepository interface {
	// Create 创建任务
	Create(ctx context.Context, task domain.Task) error

	// GetByID 根据ID获取任务
	GetByID(ctx context.Context, id string) (domain.Task, error)

	// Update 更新任务
	Update(ctx context.Context, task domain.Task) error

	// UpdateStatus 更新任务状态
	UpdateStatus(ctx context.Context, id string, status domain.TaskStatus, message string) error

	// UpdateProgress 更新任务进度
	UpdateProgress(ctx context.Context, id string, progress int, message string) error

	// List 获取任务列表
	List(ctx context.Context, filter domain.TaskFilter) ([]domain.Task, int64, error)

	// Delete 删除任务
	Delete(ctx context.Context, id string) error
}

type taskRepository struct {
	dao dao.TaskDAO
}

// NewTaskRepository 创建任务仓储
func NewTaskRepository(dao dao.TaskDAO) TaskRepository {
	return &taskRepository{dao: dao}
}

// Create 创建任务
func (r *taskRepository) Create(ctx context.Context, task domain.Task) error {
	return r.dao.Create(ctx, task)
}

// GetByID 根据ID获取任务
func (r *taskRepository) GetByID(ctx context.Context, id string) (domain.Task, error) {
	return r.dao.GetByID(ctx, id)
}

// Update 更新任务
func (r *taskRepository) Update(ctx context.Context, task domain.Task) error {
	return r.dao.Update(ctx, task)
}

// UpdateStatus 更新任务状态
func (r *taskRepository) UpdateStatus(ctx context.Context, id string, status domain.TaskStatus, message string) error {
	return r.dao.UpdateStatus(ctx, id, status, message)
}

// UpdateProgress 更新任务进度
func (r *taskRepository) UpdateProgress(ctx context.Context, id string, progress int, message string) error {
	return r.dao.UpdateProgress(ctx, id, progress, message)
}

// List 获取任务列表
func (r *taskRepository) List(ctx context.Context, filter domain.TaskFilter) ([]domain.Task, int64, error) {
	tasks, err := r.dao.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	count, err := r.dao.Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	return tasks, count, nil
}

// Delete 删除任务
func (r *taskRepository) Delete(ctx context.Context, id string) error {
	return r.dao.Delete(ctx, id)
}
