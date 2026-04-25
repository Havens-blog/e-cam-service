package cam

import (
	"sync"

	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam/scheduler"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/task"
	taskservice "github.com/Havens-blog/e-cam-service/internal/cam/task/service"
	taskweb "github.com/Havens-blog/e-cam-service/internal/cam/task/web"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"

	// 注册各云厂商适配器
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/aliyun"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/asset"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/aws"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/huawei"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/tencent"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/volcano"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
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
	instanceRelationDAO := InitInstanceRelationDAO(db)
	instanceRelationRepository := repository.NewInstanceRelationRepository(instanceRelationDAO)

	// 适配器工厂
	component := logger
	adapterFactory := asset.NewAdapterFactory(component)
	cloudxAdapterFactory := cloudx.NewAdapterFactory(component)

	// Service 层
	serviceService := service.NewService(assetRepository, cloudAccountRepository, adapterFactory, component)
	modelService := service.NewModelService(modelRepository, modelFieldRepository, modelFieldGroupRepository)
	instanceService := service.NewInstanceService(instanceRepository)
	assetSyncService := service.NewAssetSyncService(instanceRepository, instanceRelationRepository, cloudAccountRepository, cloudxAdapterFactory, component)

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
	autoSyncScheduler := scheduler.NewAutoSyncScheduler(cloudAccountRepository, queue, component)

	// Web 层
	handler := web.NewHandler(serviceService, cloudAccountService, modelService)
	instanceHandler := web.NewInstanceHandler(instanceService)
	databaseHandler := web.NewDatabaseHandler(instanceService)
	assetHandler := web.NewAssetHandler(instanceService)

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
		AssetSyncSvc:  assetSyncService,
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
