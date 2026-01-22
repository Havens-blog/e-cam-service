package task

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/gotomicro/ego/core/elog"
)

// Module 任务模块
type Module struct {
	Queue *taskx.Queue
}

// InitModule 初始化任务模块
func InitModule(
	db *mongox.Mongo,
	assetService service.Service,
	logger *elog.Component,
) (*Module, error) {
	// 初始化任务仓储
	taskRepo := taskx.NewMongoRepository(db, "tasks")

	// 初始化索引
	if err := taskRepo.InitIndexes(context.Background()); err != nil {
		return nil, err
	}

	// 初始化任务队列
	queueConfig := taskx.Config{
		WorkerNum:  5,   // 5个worker
		BufferSize: 100, // 缓存冲100个任务
	}
	taskQueue := taskx.NewQueue(taskRepo, logger, queueConfig)

	// 注册 CAM 模块的任务执行器
	syncAssetsExecutor := executor.NewSyncAssetsExecutor(assetService, taskRepo, logger)
	taskQueue.RegisterExecutor(syncAssetsExecutor)

	// 启动任务队列
	taskQueue.Start()

	return &Module{
		Queue: taskQueue,
	}, nil
}

// Stop 停止任务模块
func (m *Module) Stop() {
	if m.Queue != nil {
		m.Queue.Stop()
	}
}
