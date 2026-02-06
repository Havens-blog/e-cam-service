//go:build wireinject

package iam

import (
	"sync"

	// 使用 cam 的 repository，因为接口相同且已有 wire 配置
	camrepo "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	camdao "github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/iam/repository"
	"github.com/Havens-blog/e-cam-service/internal/iam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/iam/service"
	"github.com/Havens-blog/e-cam-service/internal/iam/web"
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

// InitUserGroupDAO 初始化用户组DAO
func InitUserGroupDAO(db *mongox.Mongo) dao.UserGroupDAO {
	InitCollectionOnce(db)
	return dao.NewUserGroupDAO(db)
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

// InitTenantDAO 初始化租户DAO
func InitTenantDAO(db *mongox.Mongo) dao.TenantDAO {
	InitCollectionOnce(db)
	return dao.NewTenantDAO(db)
}

// InitCloudAccountDAO 初始化云账号DAO（使用 cam 模块）
func InitCloudAccountDAO(db *mongox.Mongo) camdao.CloudAccountDAO {
	return camdao.NewCloudAccountDAO(db)
}

// InitCloudAccountRepository 初始化云账号Repository（使用 cam 模块）
func InitCloudAccountRepository(accountDAO camdao.CloudAccountDAO) camrepo.CloudAccountRepository {
	return camrepo.NewCloudAccountRepository(accountDAO)
}

// ProviderSet Wire依赖注入集合
var ProviderSet = wire.NewSet(
	// DAO层
	InitCloudUserDAO,
	InitUserGroupDAO,
	InitSyncTaskDAO,
	InitAuditLogDAO,
	InitPolicyTemplateDAO,
	InitTenantDAO,

	// Repository层
	repository.NewCloudUserRepository,
	repository.NewUserGroupRepository,
	repository.NewSyncTaskRepository,
	repository.NewAuditLogRepository,
	repository.NewPolicyTemplateRepository,
	repository.NewTenantRepository,

	// Account模块的Repository（使用 cam 模块）
	InitCloudAccountDAO,
	InitCloudAccountRepository,

	// 云平台适配器工厂
	iam.New,

	// Service层
	service.NewCloudUserService,
	service.NewUserGroupService,
	service.NewPermissionService,
	service.NewSyncService,
	service.NewAuditService,
	service.NewPolicyTemplateService,
	service.NewTenantService,

	// Logger
	ProvideLogger,

	// Web层
	web.NewUserHandler,
	web.NewUserGroupHandler,
	web.NewPermissionHandler,
	web.NewSyncHandler,
	web.NewAuditHandler,
	web.NewTemplateHandler,
	web.NewTenantHandler,

	// Module
	NewModule,
)

// InitModule 初始化IAM模块
func InitModule(db *mongox.Mongo) (*Module, error) {
	wire.Build(ProviderSet)
	return &Module{}, nil
}

// ProvideLogger 提供默认logger
func ProvideLogger() *elog.Component {
	logger := elog.DefaultLogger
	if logger == nil {
		// 如果DefaultLogger未初始化，使用 ego 的 Load 方法创建
		logger = elog.Load("default").Build()
	}
	return logger
}
