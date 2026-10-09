package cam

import (
	"sync"

	alertdao "github.com/Havens-blog/e-cam-service/internal/alert/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/task"
	taskservice "github.com/Havens-blog/e-cam-service/internal/cam/task/service"
	taskweb "github.com/Havens-blog/e-cam-service/internal/cam/task/web"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"

	"github.com/Havens-blog/e-cloudx-sdk"
	"github.com/Havens-blog/e-cloudx-sdk/asset"
	"github.com/Havens-blog/e-common-go/mongox"
	"github.com/Havens-blog/e-common-go/taskx"
	"github.com/gotomicro/ego/core/elog"
)

var (
	camInitOnce sync.Once
)

// InitCollectionOnce 初始化数据库集合和索引（只执行一次）
func InitCollectionOnce(db *mongox.Mongo) {
	camInitOnce.Do(func() {
		if err := dao.InitIndexes(db); err != nil {
			panic("failed to init cam indexes: " + err.Error())
		}
	})
}

// InitAssetDAO 初始化资产DAO
func InitAssetDAO(db *mongox.Mongo) dao.AssetDAO {
	InitCollectionOnce(db)
	return dao.NewAssetDAO(db)
}

// InitCloudAccountDAO 初始化云账号DAO
func InitCloudAccountDAO(db *mongox.Mongo) dao.CloudAccountDAO {
	InitCollectionOnce(db)
	return dao.NewCloudAccountDAO(db)
}

// InitModelDAO 初始化模型DAO
func InitModelDAO(db *mongox.Mongo) dao.ModelDAO {
	InitCollectionOnce(db)
	return dao.NewModelDAO(db)
}

// InitModelFieldDAO 初始化字段DAO
func InitModelFieldDAO(db *mongox.Mongo) dao.ModelFieldDAO {
	InitCollectionOnce(db)
	return dao.NewModelFieldDAO(db)
}

// InitModelFieldGroupDAO 初始化字段分组DAO
func InitModelFieldGroupDAO(db *mongox.Mongo) dao.ModelFieldGroupDAO {
	InitCollectionOnce(db)
	return dao.NewModelFieldGroupDAO(db)
}

// InitInstanceDAO 初始化实例DAO
func InitInstanceDAO(db *mongox.Mongo) dao.InstanceDAO {
	InitCollectionOnce(db)
	return dao.NewInstanceDAO(db)
}

// InitInstanceRelationDAO 初始化实例关系DAO
func InitInstanceRelationDAO(db *mongox.Mongo) dao.InstanceRelationDAO {
	InitCollectionOnce(db)
	return dao.NewInstanceRelationDAO(db)
}

// InitTaskRepository 初始化任务仓储
func InitTaskRepository(db *mongox.Mongo) taskx.TaskRepository {
	return taskx.NewMongoRepository(db, "ecam_task")
}

// InitModule 初始化CAM模块
func InitModule(db *mongox.Mongo) (*Module, error) {
	logger := ProvideLogger()

	// DAO 层
	assetDAO := InitAssetDAO(db)
	assetRepository := repository.NewAssetRepository(assetDAO)
	cloudAccountDAO := InitCloudAccountDAO(db)
	cloudAccountRepository := repository.NewCloudAccountRepository(cloudAccountDAO)
	instanceDAO := InitInstanceDAO(db)
	instanceRepository := repository.NewInstanceRepository(instanceDAO)
	modelDAO := InitModelDAO(db)
	modelRepository := repository.NewModelRepository(modelDAO)
	modelFieldDAO := InitModelFieldDAO(db)
	modelFieldRepository := repository.NewModelFieldRepository(modelFieldDAO)
	modelFieldGroupDAO := InitModelFieldGroupDAO(db)
	modelFieldGroupRepository := repository.NewModelFieldGroupRepository(modelFieldGroupDAO)

	// 适配器工厂
	component := logger
	adapterFactory := asset.NewAdapterFactory(component)
	cloudxAdapterFactory := cloudx.NewAdapterFactory(component)

	// Service 层
	serviceService := service.NewService(assetRepository, cloudAccountRepository, adapterFactory, component)
	modelService := service.NewModelService(modelRepository, modelFieldRepository, modelFieldGroupRepository)
	instanceService := service.NewInstanceService(instanceRepository)

	// Task 模块
	taskModule, err := task.InitModule(db, cloudAccountRepository, instanceRepository, adapterFactory, component)
	if err != nil {
		return nil, err
	}
	queue := taskModule.Queue
	cloudAccountService := service.NewCloudAccountService(cloudAccountRepository, instanceRepository, adapterFactory, queue, component)

	// Task Service
	taskRepository := InitTaskRepository(db)
	taskService := taskservice.NewTaskService(queue, taskRepository, component)
	taskHandler := taskweb.NewTaskHandler(taskService)

	// Dashboard
	dashboardDAO := dao.NewDashboardDAO(db)
	dashboardService := service.NewDashboardService(dashboardDAO)
	dashboardHandler := web.NewDashboardHandler(dashboardService)

	// Scheduler
	// 持久化日闸(scheduler_state):NAS/CDN 每日采集的原子认领入口(唯一提交入口,
	// 写失败指数退避重试+升级告警、读失败 ≥5 分钟退避,见 scheduler/daily_gate.go)。
	// 告警通道落 alert 告警事件(T6 健康监控/T8 CDN 迁移共用,勿重复造)。
	// 特性开关 SCHEDULER_PERSISTENT_GATE_ENABLED 默认开启;mongo 日闸不可恢复
	// 故障时可显式关闭,一键回滚内存闸(scheduler/feature_flag.go)。
	schedulerStateDAO := dao.NewSchedulerStateDAO(db)
	gateAlerter := NewSchedulerGateAlerter(alertdao.NewAlertDAO(db))
	dailyGate := scheduler.NewPersistentDailyGate(schedulerStateDAO, gateAlerter, component)
	autoSyncScheduler := scheduler.NewAutoSyncScheduler(cloudAccountRepository, queue, component, dailyGate, scheduler.IsPersistentGateEnabled())

	// NAS/OSS/Disk 自我健康监控(任务 6):与日闸告警共用同一告警桥实例
	taskModule.SetNASHealthAlerter(gateAlerter)
	taskModule.SetOSSHealthAlerter(gateAlerter)
	taskModule.SetDiskHealthAlerter(gateAlerter)

	// Web 层
	handler := web.NewHandler(serviceService, cloudAccountService, modelService)
	instanceHandler := web.NewInstanceHandler(instanceService)
	databaseHandler := web.NewDatabaseHandler(instanceService)
	cdnQueryService := service.NewCDNQueryService(cloudAccountRepository, cloudxAdapterFactory, dao.NewCDNMetricDAO(db), component)
	nasQueryService := service.NewNASQueryService(cloudAccountRepository, dao.NewNASMetricDAO(db), component)
	ossQueryService := service.NewOSSQueryService(cloudAccountRepository, dao.NewOSSMetricDAO(db), component)
	diskQueryService := service.NewDiskQueryService(cloudAccountRepository, dao.NewDiskMetricDAO(db), component)
	// RDS 指标读取:租户校验依赖账号仓储,停用态甄别依赖实例仓储
	// (assets/rds/metrics 的 zero_exception 甄别),指标仅读本地 DAO。
	rdsQueryService := service.NewRDSQueryService(cloudAccountRepository, instanceRepository, dao.NewRDSMetricDAO(db), component)
	assetHandler := web.NewAssetHandler(instanceService, dao.NewStatsSnapshotDAO(db), cdnQueryService, nasQueryService, ossQueryService, diskQueryService).SetRDSQueryService(rdsQueryService)

	camModule := &Module{
		Hdl:           handler,
		InstanceHdl:   instanceHandler,
		DatabaseHdl:   databaseHandler,
		AssetHdl:      assetHandler,
		DashboardHdl:  dashboardHandler,
		Svc:           serviceService,
		AccountSvc:    cloudAccountService,
		ModelSvc:      modelService,
		InstanceSvc:   instanceService,
		TaskModule:    taskModule,
		TaskSvc:       taskService,
		TaskHdl:       taskHandler,
		AutoScheduler: autoSyncScheduler,
		Logger:        component,
	}
	return camModule, nil
}

// ProvideLogger 提供默认logger
func ProvideLogger() *elog.Component {
	if elog.DefaultLogger != nil {
		return elog.DefaultLogger
	}
	return elog.Load("logger.default").Build()
}
