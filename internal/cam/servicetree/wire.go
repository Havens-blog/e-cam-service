//go:build wireinject

package servicetree

import (
	camrepo "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/web"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/google/wire"
	"github.com/gotomicro/ego/core/elog"
)

// ProviderSet 服务树模块依赖注入集合
var ProviderSet = wire.NewSet(
	// DAO
	dao.NewNodeDAO,
	dao.NewBindingDAO,
	dao.NewRuleDAO,
	dao.NewEnvironmentDAO,

	// Repository
	repository.NewNodeRepository,
	repository.NewBindingRepository,
	repository.NewRuleRepository,
	repository.NewEnvironmentRepository,

	// Service
	service.NewTreeService,
	service.NewBindingService,
	service.NewRuleEngineService,
	service.NewEnvironmentService,

	// Handler
	web.NewHandler,
	web.NewEnvHandler,
)

// InitModule 初始化服务树模块
// instanceRepo 从 cam 模块注入，用于规则引擎查询实例
func InitModule(db *mongox.Mongo, instanceRepo camrepo.InstanceRepository, logger *elog.Component) (*Module, error) {
	wire.Build(
		ProviderSet,
		NewModule,
	)
	return nil, nil
}
