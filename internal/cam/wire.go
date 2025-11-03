//go:build wireinject

package cam

import (
	"sync"

	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/sync/service/adapters"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
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

// ProviderSet Wire依赖注入集合
var ProviderSet = wire.NewSet(
	// DAO层
	InitAssetDAO,
	InitCloudAccountDAO,
	InitModelDAO,
	InitModelFieldDAO,
	InitModelFieldGroupDAO,

	// Repository层
	repository.NewAssetRepository,
	repository.NewCloudAccountRepository,
	repository.NewModelRepository,
	repository.NewModelFieldRepository,
	repository.NewModelFieldGroupRepository,

	// Sync层
	adapters.NewAdapterFactory,

	// Service层
	service.NewService,
	service.NewCloudAccountService,
	service.NewModelService,

	// Logger
	ProvideLogger,

	// Web层
	web.NewHandler,

	// Module
	wire.Struct(new(Module), "*"),
)

// InitModule 初始化CAM模块
func InitModule(db *mongox.Mongo) (*Module, error) {
	wire.Build(ProviderSet)
	return &Module{}, nil
}

// ProvideLogger 提供默认logger
func ProvideLogger() *elog.Component {
	return elog.DefaultLogger
}
