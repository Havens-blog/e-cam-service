// Package servicetree 提供服务树功能
// 这是从 internal/cam/servicetree 重构后的独立模块
package servicetree

import (
	"github.com/Havens-blog/e-cam-service/internal/servicetree/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/servicetree/service"
	"github.com/Havens-blog/e-cam-service/internal/servicetree/web"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/gin-gonic/gin"
)

// Module 服务树模块
type Module struct {
	Handler            *web.Handler
	EnvHandler         *web.EnvHandler
	TreeService        service.TreeService
	BindingService     service.BindingService
	RuleService        service.RuleEngineService
	EnvironmentService service.EnvironmentService
}

// NewModule 创建服务树模块
func NewModule(
	handler *web.Handler,
	envHandler *web.EnvHandler,
	treeSvc service.TreeService,
	bindingSvc service.BindingService,
	ruleSvc service.RuleEngineService,
	envSvc service.EnvironmentService,
) *Module {
	return &Module{
		Handler:            handler,
		EnvHandler:         envHandler,
		TreeService:        treeSvc,
		BindingService:     bindingSvc,
		RuleService:        ruleSvc,
		EnvironmentService: envSvc,
	}
}

// RegisterRoutes 注册路由
func (m *Module) RegisterRoutes(rg *gin.RouterGroup) {
	m.Handler.RegisterRoutes(rg)
	m.Handler.RegisterBindingRoutes(rg)
	m.Handler.RegisterRuleRoutes(rg)
	m.EnvHandler.RegisterRoutes(rg)
}

// InitIndexes 初始化数据库索引
func InitIndexes(db *mongox.Mongo) error {
	return dao.InitIndexes(db)
}
