package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/task/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/task/repository"
	"github.com/gotomicro/ego/core/elog"
)

// TaskQueue 任务队列
type TaskQueue struct {
	taskChan  chan *domain.Task
	executors map[domain.TaskType]domain.TaskExecutor
	repo      repository.TaskRepository
	logger    *elog.Component
	workerNum int
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.RWMutex
}

// Config 任务队列配置
type Config struct {
	WorkerNum  int // worker 数量
	BufferSize int // 队列缓存冲大小
}

// NewTaskQueue 创建任务队列
func NewTaskQueue(
	repo repository.TaskRepository,
	logger *elog.Component,
	config Config,
) *TaskQueue {
	if config.WorkerNum <= 0 {
		config.WorkerNum = 5 // 默认5个worker
	}

	if config.BufferSize <= 0 {
		config.BufferSize = 100 // 默认缓存冲100个任务
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &TaskQueue{
		taskChan:  make(chan *domain.Task, config.BufferSize),
		executors: make(map[domain.TaskType]domain.TaskExecutor),
		repo:      repo,
		logger:    logger,
		workerNum: config.WorkerNum,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// RegisterExecutor 注册任务执行器
func (q *TaskQueue) RegisterExecutor(executor domain.TaskExecutor) {
	q.mu.Lock()
	defer q.mu.Unlock()

	taskType := executor.GetType()
	q.executors[taskType] = executor
	q.logger.Info("注册任务执行器",
		elog.String("task_type", string(taskType)))
}

// Start 启动任务队列
func (q *TaskQueue) Start() {
	q.logger.Info("启动任务队列",
		elog.Int("worker_num", q.workerNum))

	// 启动worker
	for i := 0; i < q.workerNum; i++ {
		q.wg.Add(1)
		go q.worker(i)
	}
}

// Stop 停止任务队列
func (q *TaskQueue) Stop() {
	q.logger.Info("停止任务队列")

	q.cancel()
	close(q.taskChan)
	q.wg.Wait()

	q.logger.Info("任务队列已停止")
}

// Submit 提交任务
func (q *TaskQueue) Submit(task *domain.Task) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	// 检查是否有对应的执行器
	if _, ok := q.executors[task.Type]; !ok {
		return fmt.Errorf("未找到任务类型 %s 的执行器", task.Type)
	}

	// 设置任务初始状态
	if task.Status == "" {
		task.Status = domain.TaskStatusPending
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now()
	}

	// 保存任务到数据库
	if err := q.repo.Create(context.Background(), *task); err != nil {
		return fmt.Errorf("保存任务失败: %w", err)
	}

	q.logger.Info("提交任务",
		elog.String("task_id", task.ID),
		elog.String("task_type", string(task.Type)))

	// 将任务放入队列
	select {
	case q.taskChan <- task:
		return nil
	case <-q.ctx.Done():
		return fmt.Errorf("任务队列已关闭")
	default:
		return fmt.Errorf("任务队列已满")
	}
}

// worker 工作协程
func (q *TaskQueue) worker(id int) {
	defer q.wg.Done()

	q.logger.Info("启动worker", elog.Int("worker_id", id))

	for {
		select {
		case task, ok := <-q.taskChan:
			if !ok {
				q.logger.Info("worker退出", elog.Int("worker_id", id))
				return
			}

			q.executeTask(id, task)

		case <-q.ctx.Done():
			q.logger.Info("worker收到停止信号", elog.Int("worker_id", id))
			return
		}
	}
}

// executeTask 执行任务
func (q *TaskQueue) executeTask(workerID int, task *domain.Task) {
	q.logger.Info("开始执行任务",
		elog.Int("worker_id", workerID),
		elog.String("task_id", task.ID),
		elog.String("task_type", string(task.Type)))

	// 更新任务状态为运行中
	if err := q.repo.UpdateStatus(context.Background(), task.ID, domain.TaskStatusRunning, "任务开始执行"); err != nil {
		q.logger.Error("更新任务状态失败",
			elog.String("task_id", task.ID),
			elog.FieldErr(err))
	}

	// 获取执行器
	q.mu.RLock()
	executor, ok := q.executors[task.Type]
	q.mu.RUnlock()

	if !ok {
		q.logger.Error("未找到任务执行器",
			elog.String("task_id", task.ID),
			elog.String("task_type", string(task.Type)))

		q.repo.UpdateStatus(context.Background(), task.ID, domain.TaskStatusFailed, "未找到任务执行器")
		return
	}

	// 执行任务
	ctx := context.Background()
	err := executor.Execute(ctx, task)

	if err != nil {
		q.logger.Error("任务执行失败",
			elog.String("task_id", task.ID),
			elog.FieldErr(err))

		// 更新任务状态为失败
		task.Status = domain.TaskStatusFailed
		task.Error = err.Error()
		q.repo.UpdateStatus(ctx, task.ID, domain.TaskStatusFailed, err.Error())
	} else {
		q.logger.Info("任务执行成功",
			elog.String("task_id", task.ID))

		// 更新任务状态为完成
		task.Status = domain.TaskStatusCompleted
		q.repo.UpdateStatus(ctx, task.ID, domain.TaskStatusCompleted, "任务执行完成")
	}

	// 更新任务结果
	if err := q.repo.Update(ctx, *task); err != nil {
		q.logger.Error("更新任务结果失败",
			elog.String("task_id", task.ID),
			elog.FieldErr(err))
	}
}

// GetTaskStatus 获取任务状态
func (q *TaskQueue) GetTaskStatus(taskID string) (*domain.Task, error) {
	task, err := q.repo.GetByID(context.Background(), taskID)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// ListTasks 获取任务列表
func (q *TaskQueue) ListTasks(filter domain.TaskFilter) ([]domain.Task, int64, error) {
	return q.repo.List(context.Background(), filter)
}

// CancelTask 取消任务
func (q *TaskQueue) CancelTask(taskID string) error {
	// 获取任务
	task, err := q.repo.GetByID(context.Background(), taskID)
	if err != nil {
		return err
	}

	// 只能取消待执行的任务
	if task.Status != domain.TaskStatusPending {
		return fmt.Errorf("只能取消待执行的任务，当前状态: %s", task.Status)
	}

	// 更新任务状态为已取消
	return q.repo.UpdateStatus(context.Background(), taskID, domain.TaskStatusCancelled, "任务已取消")
}
