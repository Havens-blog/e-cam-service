//go:build wireinject

package servicetree

import (
	camrepo "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/adapter"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/web"
	"github.com/Havens-blog/e-common-go/mongox"
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

	// Port Adapter（CMDB 解耦层，配置驱动的拆分开关）
	// NewCMDBPort 按 cmdb.remote_url 配置选择本地/远程实现，返回 port.CMDBPort 接口，
	// 无需 wire.Bind。配了远程地址即走 HTTP 调用独立 e-cmdb-service。
	adapter.NewCMDBPort,

	// Service
	service.NewTreeService,
	service.NewBindingService,
	service.NewRuleEngineService,
	service.NewEnvironmentService,
	service.NewNodeAssetService,

	// Handler
	web.NewHandler,
	web.NewEnvHandler,
)

// InitModule 初始化服务树模块
// instanceRepo 从 cam 模块注入，用于规则引擎查询实例；
// CMDB 数据经 adapter.NewCMDBPort 走远程 e-cmdb-service（配置 cmdb.remote_url）。
func InitModule(db *mongox.Mongo, instanceRepo camrepo.InstanceRepository, logger *elog.Component) (*Module, error) {
	wire.Build(
		ProviderSet,
		NewModule,
	)
	return nil, nil
}
