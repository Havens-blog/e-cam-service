// Package logquery 多云统一日志查询功能域(Phase 1)。
//
// 装配入口(照 internal/cert/module.go 模式):service(联邦编排)+ web(三接口)。
// 只读域:无 repository(Phase A 不落库,ADR D1);云账号凭证经
// account 仓储读取路径解密,仅内存传递。
//
// 路由挂载:/api/v1/cam/logs(见 web 包);租户边界由组级 RequireTenant 承接。
package logquery

import (
	accountrepo "github.com/Havens-blog/e-cam-service/internal/account/repository"
	"github.com/Havens-blog/e-cam-service/internal/logquery/service"
	"github.com/Havens-blog/e-cam-service/internal/logquery/web"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// Module 日志查询功能域模块。
type Module struct {
	// Svc 联邦编排服务(暴露给潜在的任务面/脚本调用;HTTP 面为主)。
	Svc *service.FederationService
	Hdl *web.LogQueryHandler
}

// InitLogQueryModule 装配日志查询功能域。
//
// deps:
//   - accounts:云账号仓储(凭证解密在仓储读取路径完成)
//   - logger:日志组件(nil 回退默认)
func InitLogQueryModule(accounts accountrepo.CloudAccountRepository, logger *elog.Component) (*Module, error) {
	if accounts == nil {
		return nil, errNilAccounts
	}
	svc := service.NewFederationService(accounts, logger)
	return &Module{
		Svc: svc,
		Hdl: web.NewLogQueryHandler(svc),
	}, nil
}

// RegisterRoutes 在 server 上挂载日志查询路由(/api/v1/cam/logs 前缀,
// 组级 RequireTenant 租户边界,见 ioc/gin.go)。
func (m *Module) RegisterRoutes(g *gin.RouterGroup) {
	if g == nil || m == nil {
		return
	}
	m.Hdl.RegisterRoutes(g)
}
