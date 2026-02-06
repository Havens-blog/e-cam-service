// Package task 异步任务模块
// 这是从 internal/cam/task 重构后的独立模块
package task

import (
	"context"

	accountrepo "github.com/Havens-blog/e-cam-service/internal/account/repository"
	assetrepo "github.com/Havens-blog/e-cam-service/internal/asset/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/asset"
	"github.com/Havens-blog/e-cam-service/internal/task/executor"
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
	accountRepo accountrepo.CloudAccountRepository,
	instanceRepo assetrepo.InstanceRepository,
	adapterFactory *asset.AdapterFactory,
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
		WorkerNum:  5,
		BufferSize: 100,
	}
	taskQueue := taskx.NewQueue(taskRepo, logger, queueConfig)

	// 注册任务执行器
	syncAssetsExecutor := executor.NewSyncAssetsExecutor(accountRepo, instanceRepo, adapterFactory, taskRepo, logger)
	taskQueue.RegisterExecutor(syncAssetsExecutor)

	// 启动任务队列
	taskQueue.Start()

	return &Module{Queue: taskQueue}, nil
}

// Stop 停止任务模块
func (m *Module) Stop() {
	if m.Queue != nil {
		m.Queue.Stop()
	}
}
