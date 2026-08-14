package web

import (
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-cam-service/pkg/ginx"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// Handler CAM HTTP处理器
type Handler struct {
	svc        service.Service
	accountSvc service.CloudAccountService
	modelSvc   service.ModelService
}

// NewHandler 创建CAM处理器
func NewHandler(svc service.Service, accountSvc service.CloudAccountService, modelSvc service.ModelService) *Handler {
	return &Handler{
		svc:        svc,
		accountSvc: accountSvc,
		modelSvc:   modelSvc,
	}
}

// PrivateRoutes 注册私有路由
func (h *Handler) PrivateRoutes(server *gin.Engine) {
	camGroup := server.Group("/api/v1/cam")
	{
		// 资产管理
		camGroup.POST("/assets", ginx.WrapBody[CreateAssetReq](h.CreateAsset))
		camGroup.POST("/assets/batch", ginx.WrapBody[CreateMultiAssetsReq](h.CreateMultiAssets))
		camGroup.PUT("/assets/:id", ginx.WrapBody[UpdateAssetReq](h.UpdateAsset))
		camGroup.GET("/assets/:id", h.GetAssetById)
		camGroup.GET("/assets", h.ListAssets)
		camGroup.DELETE("/assets/:id", h.DeleteAsset)

		// 资产发现
		camGroup.POST("/assets/discover", ginx.WrapBody[DiscoverAssetsReq](h.DiscoverAssets))
		camGroup.POST("/assets/sync", ginx.WrapBody[SyncAssetsReq](h.SyncAssets))

		// 统计分析
		camGroup.GET("/assets/statistics", h.GetAssetStatistics)
		camGroup.GET("/assets/cost-analysis", h.GetCostAnalysis)

		// 云账号管理
		//
		// 本组是**混合组**：33 条路由中只有下面两条真正依赖会话租户，故按路由挂
		// RequireTenant，而不是整组挂 —— 整组会把 /menus、/models、/assets 这些
		// 与租户无关的合法端点一并变成 403。逐条判定见报告 §18。
		//
		//   POST /cloud-accounts —— 以会话租户写入归属（:522 TenantID: GetTenantID）。
		//     租户为 0 时会造出一批归属「未选定租户」的孤儿账号。
		//   GET  /cloud-accounts —— 以会话租户构造 CloudAccountFilter（:584），而
		//     DAO（cam/repository/dao/account.go:191,231 与
		//     account/repository/dao/account.go:147,185）是
		//     `if filter.TenantID != 0 { 加租户谓词 }`：租户为 0 时谓词被整体丢弃，
		//     会返回**全部租户**的云账号。按设计 §4.3，0 必须在边界拒绝。
		camGroup.POST("/cloud-accounts",
			middleware.RequireTenant(elog.DefaultLogger),
			ginx.WrapBody[CreateCloudAccountReq](h.CreateCloudAccount))
		camGroup.GET("/cloud-accounts/:id", h.GetCloudAccount)
		camGroup.GET("/cloud-accounts",
			middleware.RequireTenant(elog.DefaultLogger),
			h.ListCloudAccounts)
		camGroup.PUT("/cloud-accounts/:id", ginx.WrapBody[UpdateCloudAccountReq](h.UpdateCloudAccount))
		camGroup.DELETE("/cloud-accounts/:id", h.DeleteCloudAccount)

		// 云账号操作
		camGroup.POST("/cloud-accounts/:id/test-connection", h.TestCloudAccountConnection)
		camGroup.POST("/cloud-accounts/:id/enable", h.EnableCloudAccount)
		camGroup.POST("/cloud-accounts/:id/disable", h.DisableCloudAccount)
		camGroup.POST("/cloud-accounts/:id/sync", ginx.WrapBody[SyncAccountReq](h.SyncCloudAccount))

		// 模型管理
		camGroup.POST("/models", ginx.WrapBody[CreateModelReq](h.CreateModel))
		camGroup.GET("/models/:uid", h.GetModel)
		camGroup.GET("/models", h.ListModels)
		camGroup.PUT("/models/:uid", ginx.WrapBody[UpdateModelReq](h.UpdateModel))
		camGroup.DELETE("/models/:uid", h.DeleteModel)

		// 字段管理
		camGroup.POST("/models/:uid/fields", ginx.WrapBody[CreateFieldReq](h.AddField))
		camGroup.GET("/models/:uid/fields", h.GetModelFields)
		camGroup.PUT("/fields/:field_uid", ginx.WrapBody[UpdateFieldReq](h.UpdateField))
		camGroup.DELETE("/fields/:field_uid", h.DeleteField)

		// 字段分组管理
		camGroup.POST("/models/:uid/field-groups", ginx.WrapBody[CreateFieldGroupReq](h.AddFieldGroup))
		camGroup.GET("/models/:uid/field-groups", h.GetModelFieldGroups)
		camGroup.PUT("/field-groups/:id", ginx.WrapBody[UpdateFieldGroupReq](h.UpdateFieldGroup))
		camGroup.DELETE("/field-groups/:id", h.DeleteFieldGroup)

		// 菜单管理
		camGroup.GET("/menus", h.GetMenus)
	}
}
