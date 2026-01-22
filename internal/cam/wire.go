//go:build wireinject

package cam

import (
	"sync"

	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/sync/service/adapters"
	"github.com/Havens-blog/e-cam-service/internal/cam/task"
	taskservice "github.com/Havens-blog/e-cam-service/internal/cam/task/service"
	taskweb "github.com/Havens-blog/e-cam-service/internal/cam/task/web"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/google/wire"
	"github.com/gotomicro/ego/core/elog"
)

var (
	camInitOnce sync.Once
)

// InitCollectionOnce 初始化数据库集合和索引（只执行一次）
func InitCollectionOnce(db *mongox.Mongo) {
	camInitOnce.Do(func() {
		// 初始化索引
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
	return taskx.NewMongoRepository(db, "tasks")
}

// ProviderSet Wire依赖注入集合
var ProviderSet = wire.NewSet(
	// DAO层
	InitAssetDAO,
	InitCloudAccountDAO,
	InitModelDAO,
	InitModelFieldDAO,
	InitModelFieldGroupDAO,
	InitInstanceDAO,
	InitInstanceRelationDAO,

	// Repository层
	repository.NewAssetRepository,
	repository.NewCloudAccountRepository,
	repository.NewModelRepository,
	repository.NewModelFieldRepository,
	repository.NewModelFieldGroupRepository,
	repository.NewInstanceRepository,
	repository.NewInstanceRelationRepository,

	// Task Repository
	InitTaskRepository,

	// Sync层
	adapters.NewAdapterFactory,

	// Service层
	service.NewService,
	service.NewCloudAccountService,
	service.NewModelService,
	service.NewInstanceService,

	// Task层
	task.InitModule,
	wire.FieldsOf(new(*task.Module), "Queue"),
	taskservice.NewTaskService,
	taskweb.NewTaskHandler,

	// Logger
	ProvideLogger,

	// Web层
	web.NewHandler,
	web.NewInstanceHandler,

	// Module (排除 IAMModule，手动初始化)
	wire.Struct(new(Module), "Hdl", "InstanceHdl", "Svc", "AccountSvc", "ModelSvc", "InstanceSvc", "TaskModule", "TaskSvc", "TaskHdl"),
)

// InitModule 初始化CAM模块
func InitModule(db *mongox.Mongo) (*Module, error) {
	wire.Build(ProviderSet)
	return &Module{}, nil
}

// ProvideLogger 提供默认logger
// 使用可读的时间格式和调用者信息
func ProvideLogger() *elog.Component {
	// 优先使用已初始化的 DefaultLogger
	if elog.DefaultLogger != nil {
		return elog.DefaultLogger
	}
	// 使用 ego 的 Load 方法创建，配置名为 "logger.default"
	return elog.Load("logger.default").Build()
}
