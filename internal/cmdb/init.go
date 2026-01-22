package cmdb

import (
	"github.com/Havens-blog/e-cam-service/internal/cmdb/repository"
	"github.com/Havens-blog/e-cam-service/internal/cmdb/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cmdb/service"
	"github.com/Havens-blog/e-cam-service/internal/cmdb/web"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/gin-gonic/gin"
)

// Module CMDB模块
type Module struct {
	InstanceHandler   *web.InstanceHandler
	ModelHandler      *web.ModelHandler
	RelationHandler   *web.RelationHandler
	ModelGroupHandler *web.ModelGroupHandler
	AttributeHandler  *web.AttributeHandler
}

// InitModule 初始化CMDB模块
func InitModule(db *mongox.Mongo) *Module {
	// DAO
	instanceDAO := dao.NewInstanceDAO(db)
	modelDAO := dao.NewModelDAO(db)
	relationDAO := dao.NewInstanceRelationDAO(db)
	modelRelDAO := dao.NewModelRelationTypeDAO(db)
	modelGroupDAO := dao.NewModelGroupDAO(db)
	attributeDAO := dao.NewAttributeDAO(db)
	attributeGroupDAO := dao.NewAttributeGroupDAO(db)

	// Repository
	instanceRepo := repository.NewInstanceRepository(instanceDAO)
	modelRepo := repository.NewModelRepository(modelDAO)
	relationRepo := repository.NewInstanceRelationRepository(relationDAO)
	modelRelRepo := repository.NewModelRelationTypeRepository(modelRelDAO)
	modelGroupRepo := repository.NewModelGroupRepository(modelGroupDAO)
	attributeRepo := repository.NewAttributeRepository(attributeDAO)
	attributeGroupRepo := repository.NewAttributeGroupRepository(attributeGroupDAO)

	// Service
	instanceSvc := service.NewInstanceService(instanceRepo)
	modelSvc := service.NewModelService(modelRepo)
	relationSvc := service.NewRelationService(relationRepo)
	modelRelSvc := service.NewModelRelationTypeService(modelRelRepo)
	topologySvc := service.NewTopologyService(instanceRepo, relationRepo, modelRepo, modelRelRepo)
	modelGroupSvc := service.NewModelGroupService(modelGroupRepo, modelRepo)
	attributeSvc := service.NewAttributeService(attributeRepo, attributeGroupRepo, modelRepo)

	// Handler
	instanceHandler := web.NewInstanceHandler(instanceSvc)
	modelHandler := web.NewModelHandler(modelSvc)
	relationHandler := web.NewRelationHandler(modelRelSvc, relationSvc, topologySvc)
	modelGroupHandler := web.NewModelGroupHandler(modelGroupSvc)
	attributeHandler := web.NewAttributeHandler(attributeSvc)

	return &Module{
		InstanceHandler:   instanceHandler,
		ModelHandler:      modelHandler,
		RelationHandler:   relationHandler,
		ModelGroupHandler: modelGroupHandler,
		AttributeHandler:  attributeHandler,
	}
}

// RegisterRoutes 注册CMDB路由
func (m *Module) RegisterRoutes(r *gin.RouterGroup) {
	cmdbGroup := r.Group("/cmdb")
	m.InstanceHandler.RegisterRoutes(cmdbGroup)
	m.ModelHandler.RegisterRoutes(cmdbGroup)
	m.RelationHandler.RegisterRoutes(cmdbGroup)
	m.ModelGroupHandler.RegisterRoutes(cmdbGroup)
	m.AttributeHandler.RegisterRoutes(cmdbGroup)
}
