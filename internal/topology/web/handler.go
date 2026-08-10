package web

import (
	"net/http"
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-cam-service/internal/topology/service"
	"github.com/Havens-blog/e-cam-service/pkg/ginx"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
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
		// 前 4 条**逐路由**挂 RequireTenant：它们用 middleware.GetTenantID 取会话租户，
		// 而 dao/node.go:142 与 dao/edge.go:178 是 `if filter.TenantID != 0 { 加谓词 }`
		// —— 租户为 0（eiam 的「等待选择租户」临时凭证）时谓词被丢弃，会读到**全部
		// 租户**的拓扑数据。这 4 条的路径均不在 auth 白名单内（白名单只有
		// /api/v1/cam/topology/declarations），故会话必然存在，加守卫不会误拒。
		//
		// **绝不可把守卫挂到 g 上**：下面 3 个 /declarations 动词是白名单机器端点
		// （apm-push 无用户会话），context 中永无会话租户，组级守卫会让它们一律 403,
		// 从而打断 APM 拓扑采集。它们的租户按裁定来自 X-Tenant-ID 头，
		// 由 machineTenantIDFromHeader 解析、handler 内以 400 拒绝 0 值。
		g.GET("", middleware.RequireTenant(elog.DefaultLogger), h.GetTopology)
		g.GET("/domains", middleware.RequireTenant(elog.DefaultLogger), h.GetDomains)
		g.GET("/node/:id", middleware.RequireTenant(elog.DefaultLogger), h.GetNodeDetail)
		g.GET("/stats", middleware.RequireTenant(elog.DefaultLogger), h.GetStats)

		g.POST("/declarations", ginx.WrapBody[DeclarationRequestVO](h.CreateDeclaration))
		g.GET("/declarations", h.ListDeclarations)
		g.DELETE("/declarations/:source", h.DeleteDeclaration)
	}
}

// machineTenantIDFromHeader 供 /declarations 资源的三个动词（POST/GET/DELETE）使用，
// 租户来自调用方的 X-Tenant-ID 头，而非会话。
//
// 为什么这三个都拿不到会话租户：
//
//  1. POST 与 GET —— 路径 /api/v1/cam/topology/declarations 同时位于 auth 与
//     policy 白名单（config/prod.yaml:82、:88）。matchWhitelist
//     （internal/shared/middleware/ecmdb_auth.go:80）是**方法无关**的精确路径
//     匹配，命中后执行 c.Next(); return —— 该早返回发生在写入租户**之前**。
//     故 POST 与 GET 命中同一条白名单，context 中从不存在会话租户。
//  2. DELETE —— 路径 /declarations/:source 不同，确实经过认证，本可用
//     middleware.GetTenantID。但它必须按 POST 写入时所用的租户来删除，
//     否则除非两者恰好相等，删除恒为 no-op（{"deleted": 0}），
//     apm-push 写入的声明将永远无法从 UI 清理。故与 POST 保持一致。
//
// 写入方 cmd/apm-push 无用户会话，租户取自环境变量 TENANT_ID 并经该头发送
// （config.go:59 已将其列为必需、pusher.go:81 总是设置）；前端读/删则由全局
// 拦截器设置该头（e-cam-web/src/api/request/index.ts:59-62）。
//
// 这是一个已知的信任缺口：任何能访问本服务的人都可读写任意租户的拓扑数据。
// 该缺口与改动前一致（这三个动词原本就共用同一个读头 helper），并非本次放大；
// 机器调用方的租户来源待裁定，见设计文档 §4.7.3。
//
// 此函数不得用于任何经认证且无此一致性约束的端点 —— 例如同文件的
// GetTopology/GetDomains/GetNodeDetail/GetStats，它们必须用 middleware.GetTenantID。
//
// 注意此处不再回退到 "default"：缺头或头值无法解析为 int64 时返回 0，
// 由 handler 返回 400 拒绝，而不是把数据静默读写到一个虚构租户。
// 租户类型已全仓迁移为 int64，故此处解析后再交给下游。
func machineTenantIDFromHeader(ctx *gin.Context) int64 {
	raw := ctx.GetHeader("X-Tenant-ID")
	if raw == "" {
		return 0
	}
	tenantID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return tenantID
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
	if tenantID == 0 {
		return ginx.Result{Code: 400, Msg: "X-Tenant-ID header is required and must be a valid tenant id"}, nil
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
	// 与 POST 同路径、同在 auth 白名单内（白名单匹配与 HTTP 方法无关），
	// 故此处无会话租户，只能读头。详见 machineTenantIDFromHeader。
	tenantID := machineTenantIDFromHeader(ctx)
	if tenantID == 0 {
		ctx.JSON(http.StatusBadRequest, ginx.Result{Code: 400, Msg: "X-Tenant-ID header is required and must be a valid tenant id"})
		return
	}
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
	// 本路径确实经过认证，但必须与 POST 写入时所用的租户一致，否则删除恒为
	// no-op。详见 machineTenantIDFromHeader。
	tenantID := machineTenantIDFromHeader(ctx)
	if tenantID == 0 {
		ctx.JSON(http.StatusBadRequest, ginx.Result{Code: 400, Msg: "X-Tenant-ID header is required and must be a valid tenant id"})
		return
	}

	count, err := h.declSvc.DeleteBySource(ctx.Request.Context(), tenantID, source)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, ginx.Result{Code: 500, Msg: err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, ginx.Result{Code: 0, Msg: "success", Data: map[string]int64{"deleted": count}})
}
