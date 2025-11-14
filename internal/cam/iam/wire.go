//go:build wireinject

package iam

import (
	"sync"

	camrepo "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	camdao "github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam/iam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/iam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam/iam/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/iam/web"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/iam"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/google/wire"
	"github.com/gotomicro/ego/core/elog"
)

var (
	iamInitOnce sync.Once
)

// InitCollectionOnce 初始化数据库集合和索引（只执行一次）
func InitCollectionOnce(db *mongox.Mongo) {
	iamInitOnce.Do(func() {
		// 初始化索引
		if err := dao.InitIndexes(db); err != nil {
			panic("failed to init iam indexes: " + err.Error())
		}
	})
}

// InitCloudUserDAO 初始化用户DAO
func InitCloudUserDAO(db *mongox.Mongo) dao.CloudUserDAO {
	InitCollectionOnce(db)
	return dao.NewCloudUserDAO(db)
}

// InitPermissionGroupDAO 初始化权限组DAO
func InitPermissionGroupDAO(db *mongox.Mongo) dao.PermissionGroupDAO {
	InitCollectionOnce(db)
	return dao.NewPermissionGroupDAO(db)
}

// InitSyncTaskDAO 初始化同步任务DAO
func InitSyncTaskDAO(db *mongox.Mongo) dao.SyncTaskDAO {
	InitCollectionOnce(db)
	return dao.NewSyncTaskDAO(db)
}

// InitAuditLogDAO 初始化审计日志DAO
func InitAuditLogDAO(db *mongox.Mongo) dao.AuditLogDAO {
	InitCollectionOnce(db)
	return dao.NewAuditLogDAO(db)
}

// InitPolicyTemplateDAO 初始化策略模板DAO
func InitPolicyTemplateDAO(db *mongox.Mongo) dao.PolicyTemplateDAO {
	InitCollectionOnce(db)
	return dao.NewPolicyTemplateDAO(db)
}

// InitCloudAccountRepository 初始化云账号Repository（从CAM模块）
func InitCloudAccountRepository(db *mongox.Mongo) camrepo.CloudAccountRepository {
	// 使用CAM模块的DAO
	camdao := camdao.NewCloudAccountDAO(db)
	return camrepo.NewCloudAccountRepository(camdao)
}

// ProviderSet Wire依赖注入集合
var ProviderSet = wire.NewSet(
	// DAO层
	InitCloudUserDAO,
	InitPermissionGroupDAO,
	InitSyncTaskDAO,
	InitAuditLogDAO,
	InitPolicyTemplateDAO,

	// Repository层
	repository.NewCloudUserRepository,
	repository.NewPermissionGroupRepository,
	repository.NewSyncTaskRepository,
	repository.NewAuditLogRepository,
	repository.NewPolicyTemplateRepository,
	
	// CAM模块的Repository
	InitCloudAccountRepository,

	// 云平台适配器工厂
	iam.NewCloudIAMAdapterFactory,

	// Service层
	service.NewCloudUserService,
	service.NewPermissionGroupService,
	service.NewSyncService,
	service.NewAuditService,
	service.NewPolicyTemplateService,

	// Logger
	ProvideLogger,

	// Web层
	web.NewUserHandler,
	web.NewGroupHandler,
	web.NewSyncHandler,
	web.NewAuditHandler,
	web.NewTemplateHandler,

	// Module
	wire.Struct(new(Module), "*"),
)

// InitModule 初始化IAM模块
func InitModule(db *mongox.Mongo) (*Module, error) {
	wire.Build(ProviderSet)
	return &Module{}, nil
}

// ProvideLogger 提供默认logger
func ProvideLogger() *elog.Component {
	return elog.DefaultLogger
}
