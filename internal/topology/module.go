package topology

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/topology/repository"
	"github.com/Havens-blog/e-cam-service/internal/topology/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/topology/service"
	"github.com/Havens-blog/e-cam-service/internal/topology/web"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// Module 拓扑模块
type Module struct {
	Handler *web.TopologyHandler
	logger  *elog.Component
}

// NewModule 创建拓扑模块
func NewModule(db *mongox.Mongo) *Module {
	// DAO 层
	nodeDAO := dao.NewNodeDAO(db)
	edgeDAO := dao.NewEdgeDAO(db)
	declDAO := dao.NewDeclarationDAO(db)

	// Repository 层
	nodeRepo := repository.NewNodeRepository(nodeDAO)
	edgeRepo := repository.NewEdgeRepository(edgeDAO)
	declRepo := repository.NewDeclarationRepository(declDAO)

	// Service 层
	topoSvc := service.NewTopologyService(nodeRepo, edgeRepo, service.NewLiveTopologyBuilder(db))
	declSvc := service.NewDeclarationService(declRepo, nodeRepo, edgeRepo)

	// Web 层
	handler := web.NewTopologyHandler(topoSvc, declSvc)

	return &Module{
		Handler: handler,
		logger:  elog.DefaultLogger,
	}
}

// InitIndexes 初始化 MongoDB 索引
func (m *Module) InitIndexes(ctx context.Context, db *mongox.Mongo) error {
	nodeDAO := dao.NewNodeDAO(db)
	edgeDAO := dao.NewEdgeDAO(db)
	declDAO := dao.NewDeclarationDAO(db)

	if err := nodeDAO.InitIndexes(ctx); err != nil {
		m.logger.Error("failed to init topo_nodes indexes", elog.FieldErr(err))
		return err
	}
	if err := edgeDAO.InitIndexes(ctx); err != nil {
		m.logger.Error("failed to init topo_edges indexes", elog.FieldErr(err))
		return err
	}
	if err := declDAO.InitIndexes(ctx); err != nil {
		m.logger.Error("failed to init topo_declarations indexes", elog.FieldErr(err))
		return err
	}

	m.logger.Info("topology indexes initialized")
	return nil
}

// RegisterRoutes 注册路由到 Gin 引擎
func (m *Module) RegisterRoutes(server *gin.Engine) {
	m.Handler.RegisterRoutes(server)
}
