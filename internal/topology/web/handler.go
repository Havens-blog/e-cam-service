package web

import (
	"net/http"

	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-cam-service/internal/topology/service"
	"github.com/Havens-blog/e-cam-service/pkg/ginx"
	"github.com/gin-gonic/gin"
)

// TopologyHandler 拓扑 HTTP 处理器
type TopologyHandler struct {
	topoSvc service.TopologyService
	declSvc service.DeclarationService
}

// NewTopologyHandler 创建拓扑处理器
func NewTopologyHandler(topoSvc service.TopologyService, declSvc service.DeclarationService) *TopologyHandler {
	return &TopologyHandler{
		topoSvc: topoSvc,
		declSvc: declSvc,
	}
}

// RegisterRoutes 注册路由
func (h *TopologyHandler) RegisterRoutes(server *gin.Engine) {
	g := server.Group("/api/v1/cam/topology")
	{
		g.GET("", h.GetTopology)
		g.GET("/domains", h.GetDomains)
		g.GET("/node/:id", h.GetNodeDetail)
		g.GET("/stats", h.GetStats)
		g.POST("/declarations", ginx.WrapBody[DeclarationRequestVO](h.CreateDeclaration))
		g.GET("/declarations", h.ListDeclarations)
		g.DELETE("/declarations/:source", h.DeleteDeclaration)
	}
}

// machineTenantIDFromHeader 仅供 auth 白名单内的机器端点使用。
//
// 该端点（/api/v1/cam/topology/declarations）跳过认证中间件，故 context 中
// 不存在会话租户，只能由调用方（cmd/apm-push）经 X-Tenant-ID 头指定。
//
// 这是一个已知的信任缺口：任何能访问本服务的人都可写入任意租户的拓扑数据。
// 机器调用方的租户来源待裁定，见设计文档 §4.7.3。此函数不得用于任何
// 经认证的端点 —— 那些端点必须用 middleware.GetTenantID。
//
// 注意此处不再回退到 "default"：缺头时返回空串，由 handler 拒绝请求，
// 而不是把数据静默写入一个虚构租户。
func machineTenantIDFromHeader(ctx *gin.Context) string {
	return ctx.GetHeader("X-Tenant-ID")
}

// GetTopology 查询拓扑图
// @Summary 查询拓扑图
// @Description 查询业务链路拓扑（mode=business）或实例归属拓扑（mode=instance）
// @Tags 拓扑视图
// @Produce json
// @Param mode query string false "查询模式: business(默认) / instance"
// @Param domain query string false "按域名筛选（仅 business 模式）"
// @Param resource_id query string false "资源 ID（仅 instance 模式）"
// @Param provider query string false "云厂商过滤，逗号分隔"
// @Param region query string false "地域过滤"
// @Param type query string false "资源类型过滤"
// @Param source_collector query string false "数据来源过滤"
// @Param hide_silent query bool false "隐藏沉默链路"
// @Success 200 {object} ginx.Result{data=TopologyResponseVO}
// @Failure 400 {object} ginx.Result
// @Failure 500 {object} ginx.Result
// @Router /topology [get]
func (h *TopologyHandler) GetTopology(ctx *gin.Context) {
	var query TopologyQueryVO
	if err := ctx.ShouldBindQuery(&query); err != nil {
		ctx.JSON(http.StatusBadRequest, ginx.Result{Code: 400, Msg: err.Error()})
		return
	}

	tenantID := middleware.GetTenantID(ctx)
	params := query.ToParams(tenantID)

	var result interface{}
	var err error

	if params.Mode == "instance" {
		graph, e := h.topoSvc.GetInstanceTopology(ctx.Request.Context(), params)
		result, err = graph, e
	} else {
		graph, e := h.topoSvc.GetBusinessTopology(ctx.Request.Context(), params)
		result, err = graph, e
	}

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, ginx.Result{Code: 500, Msg: err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, ginx.Result{Code: 0, Msg: "success", Data: result})
}

// GetDomains 获取 DNS 入口域名列表
// @Summary 获取域名列表
// @Description 获取所有 DNS 入口域名列表
// @Tags 拓扑视图
// @Produce json
// @Success 200 {object} ginx.Result{data=DomainListResponseVO}
// @Router /topology/domains [get]
func (h *TopologyHandler) GetDomains(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	domains, err := h.topoSvc.GetDomains(ctx.Request.Context(), tenantID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, ginx.Result{Code: 500, Msg: err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, ginx.Result{Code: 0, Msg: "success", Data: DomainListResponseVO{Domains: domains}})
}

// GetNodeDetail 获取节点详情
// @Summary 获取节点详情
// @Description 获取单个拓扑节点的详细信息，包含上下游关系
// @Tags 拓扑视图
// @Produce json
// @Param id path string true "节点 ID"
// @Success 200 {object} ginx.Result{data=NodeDetailResponseVO}
// @Failure 404 {object} ginx.Result
// @Router /topology/node/{id} [get]
func (h *TopologyHandler) GetNodeDetail(ctx *gin.Context) {
	nodeID := ctx.Param("id")
	tenantID := middleware.GetTenantID(ctx)

	detail, err := h.topoSvc.GetNodeDetail(ctx.Request.Context(), tenantID, nodeID)
	if err != nil {
		ctx.JSON(http.StatusNotFound, ginx.Result{Code: 404, Msg: err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, ginx.Result{Code: 0, Msg: "success", Data: NodeDetailResponseVO{NodeDetail: *detail}})
}

// GetStats 获取拓扑统计
// @Summary 获取拓扑统计
// @Description 获取拓扑图的统计信息（节点数、边数、域名数、断链数）
// @Tags 拓扑视图
// @Produce json
// @Success 200 {object} ginx.Result{data=StatsResponseVO}
// @Router /topology/stats [get]
func (h *TopologyHandler) GetStats(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	stats, err := h.topoSvc.GetStats(ctx.Request.Context(), tenantID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, ginx.Result{Code: 500, Msg: err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, ginx.Result{Code: 0, Msg: "success", Data: StatsResponseVO{TopoStats: *stats}})
}

// CreateDeclaration 声明式注册拓扑数据
// @Summary 注册拓扑声明
// @Description 通过声明式协议注册拓扑节点和连线数据
// @Tags 拓扑声明
// @Accept json
// @Produce json
// @Param request body DeclarationRequestVO true "声明数据"
// @Success 200 {object} ginx.Result
// @Failure 400 {object} ginx.Result
// @Router /topology/declarations [post]
func (h *TopologyHandler) CreateDeclaration(ctx *gin.Context, req DeclarationRequestVO) (ginx.Result, error) {
	// 该端点在 auth 白名单内（无用户会话），租户只能来自调用方请求头。
	// 详见 machineTenantIDFromHeader 的说明。
	tenantID := machineTenantIDFromHeader(ctx)
	if tenantID == "" {
		return ginx.Result{Code: 400, Msg: "X-Tenant-ID header is required"}, nil
	}
	decl := req.ToDeclaration(tenantID)

	if err := h.declSvc.Register(ctx.Request.Context(), decl); err != nil {
		return ginx.Result{Code: 400, Msg: err.Error()}, nil
	}

	return ginx.Result{Code: 0, Msg: "success"}, nil
}

// ListDeclarations 查询声明数据
// @Summary 查询声明数据
// @Description 查询当前租户下所有已注册的声明数据
// @Tags 拓扑声明
// @Produce json
// @Success 200 {object} ginx.Result
// @Router /topology/declarations [get]
func (h *TopologyHandler) ListDeclarations(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	decls, err := h.declSvc.List(ctx.Request.Context(), tenantID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, ginx.Result{Code: 500, Msg: err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, ginx.Result{Code: 0, Msg: "success", Data: decls})
}

// DeleteDeclaration 按来源删除声明数据
// @Summary 删除声明数据
// @Description 按上报方标识批量删除声明数据及其对应的拓扑节点和边
// @Tags 拓扑声明
// @Produce json
// @Param source path string true "上报方标识"
// @Success 200 {object} ginx.Result
// @Router /topology/declarations/{source} [delete]
func (h *TopologyHandler) DeleteDeclaration(ctx *gin.Context) {
	source := ctx.Param("source")
	tenantID := middleware.GetTenantID(ctx)

	count, err := h.declSvc.DeleteBySource(ctx.Request.Context(), tenantID, source)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, ginx.Result{Code: 500, Msg: err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, ginx.Result{Code: 0, Msg: "success", Data: map[string]int64{"deleted": count}})
}
