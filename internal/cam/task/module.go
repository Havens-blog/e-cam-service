package task

import (
	"context"

	accountservice "github.com/Havens-blog/e-cam-service/internal/account/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/normalizer"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	camrepository "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/asset"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/gotomicro/ego/core/elog"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/mongo"
)

// Module 任务模块
type Module struct {
	Queue              *taskx.Queue
	TaskRepo           taskx.TaskRepository
	syncAssetsExecutor *executor.SyncAssetsExecutor
	nasMetricsExecutor *executor.SyncNASMetricsExecutor
}

// InitModule 初始化任务模块
func InitModule(
	db *mongox.Mongo,
	accountRepo camrepository.CloudAccountRepository,
	instanceRepo camrepository.InstanceRepository,
	adapterFactory *asset.AdapterFactory,
	logger *elog.Component,
) (*Module, error) {
	// 初始化任务仓储
	taskRepo := taskx.NewMongoRepository(db, "ecam_task")

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

	// 注册 CAM 模块的任务执行器
	syncAssetsExecutor := executor.NewSyncAssetsExecutor(accountRepo, instanceRepo, adapterFactory, taskRepo, logger)
	taskQueue.RegisterExecutor(syncAssetsExecutor)

	// 注册 CDN 指标采集执行器(每日带宽/流量/命中率指标采集)
	metricDAO := dao.NewCDNMetricDAO(db)
	taskQueue.RegisterExecutor(executor.NewSyncCDNMetricsExecutor(accountRepo, metricDAO, taskRepo, logger))
	logger.Info("CDN指标采集执行器已注册")

	// 注册 NAS 指标采集执行器(每日容量/使用率指标采集;今日行首写生效、
	// 昨日行覆盖更新,实例枚举以 ecam_instance 为准,不依赖 EnableAutoSync)
	nasMetricDAO := dao.NewNASMetricDAO(db)
	nasMetricsExecutor := executor.NewSyncNASMetricsExecutor(accountRepo, instanceRepo, nasMetricDAO, taskRepo, logger)
	taskQueue.RegisterExecutor(nasMetricsExecutor)
	logger.Info("NAS指标采集执行器已注册")

	// 启动任务队列
	taskQueue.Start()

	return &Module{
		Queue:              taskQueue,
		TaskRepo:           taskRepo,
		syncAssetsExecutor: syncAssetsExecutor,
		nasMetricsExecutor: nasMetricsExecutor,
	}, nil
}

// SetNASHealthAlerter 注入 NAS 自我健康监控告警桥(与持久化日闸故障告警
// 共用同一 schedulerGateAlerter 实现,cam/wire.go 装配;任务 6)
func (m *Module) SetNASHealthAlerter(a executor.NASHealthAlerter) {
	if m.nasMetricsExecutor != nil {
		m.nasMetricsExecutor.SetNASHealthAlerter(a)
	}
}

// SetDNSCollections 设置 DNS 专用集合（在 DNS 模块初始化后调用）
func (m *Module) SetDNSCollections(domainColl, recordColl *mongo.Collection) {
	if m.syncAssetsExecutor != nil {
		m.syncAssetsExecutor.SetDNSCollections(domainColl, recordColl)
	}
}

// SetChangeTracker 注入资产同步变更追踪（同步收敛 Phase 2 S3a，nil 关闭）
func (m *Module) SetChangeTracker(t executor.ChangeTracker) {
	if m.syncAssetsExecutor != nil {
		m.syncAssetsExecutor.SetChangeTracker(t)
	}
}

// RegisterBillingExecutor 注册账单采集执行器（在成本模块初始化后调用）
func (m *Module) RegisterBillingExecutor(
	normalizerSvc *normalizer.NormalizerService,
	billDAO repository.BillDAO,
	collectLogDAO repository.CollectLogDAO,
	accountSvc accountservice.CloudAccountService,
	redisClient redis.Cmdable,
	logger *elog.Component,
) {
	billingExecutor := executor.NewSyncBillingExecutor(
		normalizerSvc, billDAO, collectLogDAO, accountSvc, redisClient, m.TaskRepo, logger,
	)
	m.Queue.RegisterExecutor(billingExecutor)
	logger.Info("账单采集执行器已注册")
}

// Stop 停止任务模块
func (m *Module) Stop() {
	if m.Queue != nil {
		m.Queue.Stop()
	}
}
