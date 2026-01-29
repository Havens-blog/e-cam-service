package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree/service"
	"github.com/Havens-blog/e-cam-service/pkg/ginx"
	"github.com/gin-gonic/gin"
)

// Handler 服务树 HTTP 处理器
type Handler struct {
	treeSvc    service.TreeService
	bindingSvc service.BindingService
	ruleSvc    service.RuleEngineService
}

// NewHandler 创建处理器
func NewHandler(
	treeSvc service.TreeService,
	bindingSvc service.BindingService,
	ruleSvc service.RuleEngineService,
) *Handler {
	return &Handler{
		treeSvc:    treeSvc,
		bindingSvc: bindingSvc,
		ruleSvc:    ruleSvc,
	}
}

// RegisterRoutes 注册路由
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	// 节点管理
	rg.POST("/nodes", ginx.WrapBody(h.CreateNode))
	rg.GET("/nodes", ginx.WrapBody(h.ListNodes))
	rg.GET("/nodes/:id", ginx.Wrap(h.GetNode))
	rg.PUT("/nodes/:id", ginx.WrapBody(h.UpdateNode))
	rg.DELETE("/nodes/:id", ginx.Wrap(h.DeleteNode))
	rg.PUT("/nodes/:id/move", ginx.WrapBody(h.MoveNode))
	rg.GET("/tree", ginx.Wrap(h.GetTree))
}

func (h *Handler) RegisterBindingRoutes(rg *gin.RouterGroup) {
	// 资源绑定
	rg.POST("/nodes/:id/bindings", ginx.WrapBody(h.BindResource))
	rg.POST("/nodes/:id/bindings/batch", ginx.WrapBody(h.BatchBindResource))
	rg.GET("/nodes/:id/bindings", ginx.WrapBody(h.ListNodeBindings))
	rg.DELETE("/bindings/:id", ginx.Wrap(h.UnbindResource))
	rg.GET("/resources/:type/:id/node", ginx.Wrap(h.GetResourceNode))
}

func (h *Handler) RegisterRuleRoutes(rg *gin.RouterGroup) {
	// 绑定规则
	rg.POST("/rules", ginx.WrapBody(h.CreateRule))
	rg.GET("/rules", ginx.WrapBody(h.ListRules))
	rg.GET("/rules/:id", ginx.Wrap(h.GetRule))
	rg.PUT("/rules/:id", ginx.WrapBody(h.UpdateRule))
	rg.DELETE("/rules/:id", ginx.Wrap(h.DeleteRule))
	rg.POST("/rules/execute", ginx.Wrap(h.ExecuteRules))
}

func (h *Handler) getTenantID(c *gin.Context) string {
	return c.GetHeader("X-Tenant-ID")
}

func (h *Handler) getIDParam(c *gin.Context) (int64, error) {
	return strconv.ParseInt(c.Param("id"), 10, 64)
}

// CreateNode 创建节点
// @Summary 创建服务树节点
// @Tags 服务树
// @Param X-Tenant-ID header string true "租户ID"
// @Param body body CreateNodeReq true "节点信息"
// @Success 200 {object} ginx.Result{data=int64}
// @Router /api/v1/cam/service-tree/nodes [post]
func (h *Handler) CreateNode(c *gin.Context, req CreateNodeReq) (ginx.Result, error) {
	tenantID := h.getTenantID(c)
	node := domain.ServiceTreeNode{
		UID:         req.UID,
		Name:        req.Name,
		ParentID:    req.ParentID,
		TenantID:    tenantID,
		Owner:       req.Owner,
		Team:        req.Team,
		Description: req.Description,
		Tags:        req.Tags,
		Order:       req.Order,
	}

	id, err := h.treeSvc.CreateNode(c.Request.Context(), node)
	if err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}
	return ginx.Result{Data: id}, nil
}

// GetNode 获取节点详情
// @Summary 获取节点详情
// @Tags 服务树
// @Param id path int true "节点ID"
// @Success 200 {object} ginx.Result{data=NodeVO}
// @Router /api/v1/cam/service-tree/nodes/{id} [get]
func (h *Handler) GetNode(c *gin.Context) (ginx.Result, error) {
	id, err := h.getIDParam(c)
	if err != nil {
		return ginx.Result{Code: 400, Msg: "无效的节点ID"}, nil
	}

	node, err := h.treeSvc.GetNode(c.Request.Context(), id)
	if err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}
	return ginx.Result{Data: h.toNodeVO(node)}, nil
}

// UpdateNode 更新节点
// @Summary 更新节点
// @Tags 服务树
// @Param id path int true "节点ID"
// @Param body body UpdateNodeReq true "节点信息"
// @Success 200 {object} ginx.Result
// @Router /api/v1/cam/service-tree/nodes/{id} [put]
func (h *Handler) UpdateNode(c *gin.Context, req UpdateNodeReq) (ginx.Result, error) {
	id, err := h.getIDParam(c)
	if err != nil {
		return ginx.Result{Code: 400, Msg: "无效的节点ID"}, nil
	}

	node := domain.ServiceTreeNode{
		ID:          id,
		UID:         req.UID,
		Name:        req.Name,
		Owner:       req.Owner,
		Team:        req.Team,
		Description: req.Description,
		Tags:        req.Tags,
		Order:       req.Order,
		Status:      req.Status,
	}

	if err := h.treeSvc.UpdateNode(c.Request.Context(), node); err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}
	return ginx.Result{Msg: "更新成功"}, nil
}

// DeleteNode 删除节点
// @Summary 删除节点
// @Tags 服务树
// @Param id path int true "节点ID"
// @Success 200 {object} ginx.Result
// @Router /api/v1/cam/service-tree/nodes/{id} [delete]
func (h *Handler) DeleteNode(c *gin.Context) (ginx.Result, error) {
	id, err := h.getIDParam(c)
	if err != nil {
		return ginx.Result{Code: 400, Msg: "无效的节点ID"}, nil
	}

	if err := h.treeSvc.DeleteNode(c.Request.Context(), id); err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}
	return ginx.Result{Msg: "删除成功"}, nil
}

// MoveNode 移动节点
// @Summary 移动节点
// @Tags 服务树
// @Param id path int true "节点ID"
// @Param body body MoveNodeReq true "目标父节点"
// @Success 200 {object} ginx.Result
// @Router /api/v1/cam/service-tree/nodes/{id}/move [put]
func (h *Handler) MoveNode(c *gin.Context, req MoveNodeReq) (ginx.Result, error) {
	id, err := h.getIDParam(c)
	if err != nil {
		return ginx.Result{Code: 400, Msg: "无效的节点ID"}, nil
	}

	if err := h.treeSvc.MoveNode(c.Request.Context(), id, req.NewParentID); err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}
	return ginx.Result{Msg: "移动成功"}, nil
}

// ListNodes 获取节点列表
// @Summary 获取节点列表
// @Tags 服务树
// @Param X-Tenant-ID header string true "租户ID"
// @Param parent_id query int false "父节点ID"
// @Param level query int false "层级"
// @Param name query string false "名称"
// @Success 200 {object} ginx.Result{data=[]NodeVO}
// @Router /api/v1/cam/service-tree/nodes [get]
func (h *Handler) ListNodes(c *gin.Context, req ListNodeReq) (ginx.Result, error) {
	tenantID := h.getTenantID(c)
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	filter := domain.NodeFilter{
		TenantID: tenantID,
		ParentID: req.ParentID,
		Level:    req.Level,
		Status:   req.Status,
		Name:     req.Name,
		Owner:    req.Owner,
		Team:     req.Team,
		Offset:   int64((req.Page - 1) * req.PageSize),
		Limit:    int64(req.PageSize),
	}

	nodes, total, err := h.treeSvc.ListNodes(c.Request.Context(), filter)
	if err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}

	vos := make([]NodeVO, len(nodes))
	for i, n := range nodes {
		vos[i] = h.toNodeVO(n)
	}

	return ginx.Result{Data: map[string]any{"list": vos, "total": total}}, nil
}

// GetTree 获取树结构
// @Summary 获取服务树结构
// @Tags 服务树
// @Param X-Tenant-ID header string true "租户ID"
// @Param root_id query int false "根节点ID"
// @Success 200 {object} ginx.Result{data=TreeNodeVO}
// @Router /api/v1/cam/service-tree/tree [get]
func (h *Handler) GetTree(c *gin.Context) (ginx.Result, error) {
	tenantID := h.getTenantID(c)
	rootID, _ := strconv.ParseInt(c.Query("root_id"), 10, 64)

	tree, err := h.treeSvc.GetTree(c.Request.Context(), tenantID, rootID)
	if err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}

	return ginx.Result{Data: h.toTreeNodeVO(tree)}, nil
}

// BindResource 绑定资源
// @Summary 绑定资源到节点
// @Tags 服务树
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "节点ID"
// @Param body body BindResourceReq true "资源信息"
// @Success 200 {object} ginx.Result{data=int64}
// @Router /api/v1/cam/service-tree/nodes/{id}/bindings [post]
func (h *Handler) BindResource(c *gin.Context, req BindResourceReq) (ginx.Result, error) {
	tenantID := h.getTenantID(c)
	nodeID, err := h.getIDParam(c)
	if err != nil {
		return ginx.Result{Code: 400, Msg: "无效的节点ID"}, nil
	}

	id, err := h.bindingSvc.BindResource(c.Request.Context(), nodeID, req.EnvID, req.ResourceType, req.ResourceID, tenantID)
	if err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}
	return ginx.Result{Data: id}, nil
}

// BatchBindResource 批量绑定资源
// @Summary 批量绑定资源
// @Tags 服务树
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "节点ID"
// @Param body body BatchBindReq true "资源列表"
// @Success 200 {object} ginx.Result{data=int64}
// @Router /api/v1/cam/service-tree/nodes/{id}/bindings/batch [post]
func (h *Handler) BatchBindResource(c *gin.Context, req BatchBindReq) (ginx.Result, error) {
	tenantID := h.getTenantID(c)
	nodeID, err := h.getIDParam(c)
	if err != nil {
		return ginx.Result{Code: 400, Msg: "无效的节点ID"}, nil
	}

	count, err := h.bindingSvc.BindResourceBatch(c.Request.Context(), domain.BatchBindRequest{
		NodeID:       nodeID,
		EnvID:        req.EnvID,
		ResourceType: req.ResourceType,
		ResourceIDs:  req.ResourceIDs,
		TenantID:     tenantID,
	})
	if err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}
	return ginx.Result{Data: count, Msg: "绑定成功"}, nil
}

// ListNodeBindings 获取节点绑定列表
// @Summary 获取节点绑定的资源
// @Tags 服务树
// @Param X-Tenant-ID header string true "租户ID"
// @Param id path int true "节点ID"
// @Param env_id query int false "环境ID"
// @Success 200 {object} ginx.Result{data=[]BindingVO}
// @Router /api/v1/cam/service-tree/nodes/{id}/bindings [get]
func (h *Handler) ListNodeBindings(c *gin.Context, req ListBindingReq) (ginx.Result, error) {
	tenantID := h.getTenantID(c)
	nodeID, err := h.getIDParam(c)
	if err != nil {
		return ginx.Result{Code: 400, Msg: "无效的节点ID"}, nil
	}

	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	filter := domain.BindingFilter{
		TenantID:     tenantID,
		NodeID:       nodeID,
		EnvID:        req.EnvID,
		ResourceType: req.ResourceType,
		BindType:     req.BindType,
		Offset:       int64((req.Page - 1) * req.PageSize),
		Limit:        int64(req.PageSize),
	}

	bindings, total, err := h.bindingSvc.ListBindings(c.Request.Context(), filter)
	if err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}

	vos := make([]BindingVO, len(bindings))
	for i, b := range bindings {
		vos[i] = h.toBindingVO(b)
	}

	return ginx.Result{Data: map[string]any{"list": vos, "total": total}}, nil
}

// UnbindResource 解绑资源
// @Summary 解绑资源
// @Tags 服务树
// @Param id path int true "绑定ID"
// @Success 200 {object} ginx.Result
// @Router /api/v1/cam/service-tree/bindings/{id} [delete]
func (h *Handler) UnbindResource(c *gin.Context) (ginx.Result, error) {
	id, err := h.getIDParam(c)
	if err != nil {
		return ginx.Result{Code: 400, Msg: "无效的绑定ID"}, nil
	}

	if err := h.bindingSvc.UnbindByID(c.Request.Context(), id); err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}
	return ginx.Result{Msg: "解绑成功"}, nil
}

// GetResourceNode 获取资源所属节点
// @Summary 获取资源所属节点
// @Tags 服务树
// @Param X-Tenant-ID header string true "租户ID"
// @Param type path string true "资源类型"
// @Param id path int true "资源ID"
// @Success 200 {object} ginx.Result{data=NodeVO}
// @Router /api/v1/cam/service-tree/resources/{type}/{id}/node [get]
func (h *Handler) GetResourceNode(c *gin.Context) (ginx.Result, error) {
	tenantID := h.getTenantID(c)
	resourceType := c.Param("type")
	resourceID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return ginx.Result{Code: 400, Msg: "无效的资源ID"}, nil
	}

	binding, err := h.bindingSvc.GetResourceBinding(c.Request.Context(), tenantID, resourceType, resourceID)
	if err != nil {
		return ginx.Result{Code: 404, Msg: "资源未绑定到任何节点"}, nil
	}

	node, err := h.treeSvc.GetNode(c.Request.Context(), binding.NodeID)
	if err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}

	return ginx.Result{Data: h.toNodeVO(node)}, nil
}

// CreateRule 创建规则
// @Summary 创建绑定规则
// @Tags 服务树
// @Param X-Tenant-ID header string true "租户ID"
// @Param body body CreateRuleReq true "规则信息"
// @Success 200 {object} ginx.Result{data=int64}
// @Router /api/v1/cam/service-tree/rules [post]
func (h *Handler) CreateRule(c *gin.Context, req CreateRuleReq) (ginx.Result, error) {
	tenantID := h.getTenantID(c)
	rule := domain.BindingRule{
		NodeID:      req.NodeID,
		EnvID:       req.EnvID,
		Name:        req.Name,
		TenantID:    tenantID,
		Priority:    req.Priority,
		Conditions:  req.Conditions,
		Enabled:     req.Enabled,
		Description: req.Description,
	}

	id, err := h.ruleSvc.CreateRule(c.Request.Context(), rule)
	if err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}
	return ginx.Result{Data: id}, nil
}

// GetRule 获取规则详情
// @Summary 获取规则详情
// @Tags 服务树
// @Param id path int true "规则ID"
// @Success 200 {object} ginx.Result{data=RuleVO}
// @Router /api/v1/cam/service-tree/rules/{id} [get]
func (h *Handler) GetRule(c *gin.Context) (ginx.Result, error) {
	id, err := h.getIDParam(c)
	if err != nil {
		return ginx.Result{Code: 400, Msg: "无效的规则ID"}, nil
	}

	rule, err := h.ruleSvc.GetRule(c.Request.Context(), id)
	if err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}
	return ginx.Result{Data: h.toRuleVO(rule)}, nil
}

// UpdateRule 更新规则
// @Summary 更新规则
// @Tags 服务树
// @Param id path int true "规则ID"
// @Param body body UpdateRuleReq true "规则信息"
// @Success 200 {object} ginx.Result
// @Router /api/v1/cam/service-tree/rules/{id} [put]
func (h *Handler) UpdateRule(c *gin.Context, req UpdateRuleReq) (ginx.Result, error) {
	id, err := h.getIDParam(c)
	if err != nil {
		return ginx.Result{Code: 400, Msg: "无效的规则ID"}, nil
	}

	tenantID := h.getTenantID(c)
	rule := domain.BindingRule{
		ID:          id,
		NodeID:      req.NodeID,
		EnvID:       req.EnvID,
		Name:        req.Name,
		TenantID:    tenantID,
		Priority:    req.Priority,
		Conditions:  req.Conditions,
		Enabled:     req.Enabled,
		Description: req.Description,
	}

	if err := h.ruleSvc.UpdateRule(c.Request.Context(), rule); err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}
	return ginx.Result{Msg: "更新成功"}, nil
}

// DeleteRule 删除规则
// @Summary 删除规则
// @Tags 服务树
// @Param id path int true "规则ID"
// @Success 200 {object} ginx.Result
// @Router /api/v1/cam/service-tree/rules/{id} [delete]
func (h *Handler) DeleteRule(c *gin.Context) (ginx.Result, error) {
	id, err := h.getIDParam(c)
	if err != nil {
		return ginx.Result{Code: 400, Msg: "无效的规则ID"}, nil
	}

	if err := h.ruleSvc.DeleteRule(c.Request.Context(), id); err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}
	return ginx.Result{Msg: "删除成功"}, nil
}

// ListRules 获取规则列表
// @Summary 获取规则列表
// @Tags 服务树
// @Param X-Tenant-ID header string true "租户ID"
// @Param node_id query int false "节点ID"
// @Param enabled query bool false "是否启用"
// @Success 200 {object} ginx.Result{data=[]RuleVO}
// @Router /api/v1/cam/service-tree/rules [get]
func (h *Handler) ListRules(c *gin.Context, req ListRuleReq) (ginx.Result, error) {
	tenantID := h.getTenantID(c)
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	filter := domain.RuleFilter{
		TenantID: tenantID,
		NodeID:   req.NodeID,
		Enabled:  req.Enabled,
		Name:     req.Name,
		Offset:   int64((req.Page - 1) * req.PageSize),
		Limit:    int64(req.PageSize),
	}

	rules, total, err := h.ruleSvc.ListRules(c.Request.Context(), filter)
	if err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}

	vos := make([]RuleVO, len(rules))
	for i, r := range rules {
		vos[i] = h.toRuleVO(r)
	}

	return ginx.Result{Data: map[string]any{"list": vos, "total": total}}, nil
}

// ExecuteRules 执行规则匹配
// @Summary 执行规则匹配
// @Description 手动触发规则引擎，对未绑定资源执行自动匹配，绑定到规则指定的环境
// @Tags 服务树
// @Param X-Tenant-ID header string true "租户ID"
// @Success 200 {object} ginx.Result{data=int64}
// @Router /api/v1/cam/service-tree/rules/execute [post]
func (h *Handler) ExecuteRules(c *gin.Context) (ginx.Result, error) {
	tenantID := h.getTenantID(c)
	if tenantID == "" {
		return ginx.Result{Code: 400, Msg: "租户ID不能为空"}, nil
	}

	count, err := h.ruleSvc.ExecuteRules(c.Request.Context(), tenantID)
	if err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}
	return ginx.Result{Data: count, Msg: "规则执行完成"}, nil
}

// toNodeVO 转换节点为 VO
func (h *Handler) toNodeVO(node domain.ServiceTreeNode) NodeVO {
	return NodeVO{
		ID:          node.ID,
		UID:         node.UID,
		Name:        node.Name,
		ParentID:    node.ParentID,
		Level:       node.Level,
		Path:        node.Path,
		Owner:       node.Owner,
		Team:        node.Team,
		Description: node.Description,
		Tags:        node.Tags,
		Order:       node.Order,
		Status:      node.Status,
		CreateTime:  node.CreateTime.UnixMilli(),
		UpdateTime:  node.UpdateTime.UnixMilli(),
	}
}

// toTreeNodeVO 转换树节点为 VO
func (h *Handler) toTreeNodeVO(node *domain.NodeWithChildren) *TreeNodeVO {
	if node == nil {
		return nil
	}

	vo := &TreeNodeVO{
		NodeVO:        h.toNodeVO(node.ServiceTreeNode),
		ResourceCount: node.ResourceCount,
	}

	if len(node.Children) > 0 {
		vo.Children = make([]*TreeNodeVO, len(node.Children))
		for i, child := range node.Children {
			vo.Children[i] = h.toTreeNodeVO(child)
		}
	}

	return vo
}

// toBindingVO 转换绑定为 VO
func (h *Handler) toBindingVO(binding domain.ResourceBinding) BindingVO {
	return BindingVO{
		ID:           binding.ID,
		NodeID:       binding.NodeID,
		EnvID:        binding.EnvID,
		ResourceType: binding.ResourceType,
		ResourceID:   binding.ResourceID,
		BindType:     binding.BindType,
		RuleID:       binding.RuleID,
		CreateTime:   binding.CreateTime.UnixMilli(),
	}
}

// toRuleVO 转换规则为 VO
func (h *Handler) toRuleVO(rule domain.BindingRule) RuleVO {
	return RuleVO{
		ID:          rule.ID,
		NodeID:      rule.NodeID,
		EnvID:       rule.EnvID,
		Name:        rule.Name,
		Priority:    rule.Priority,
		Conditions:  rule.Conditions,
		Enabled:     rule.Enabled,
		Description: rule.Description,
		CreateTime:  rule.CreateTime.UnixMilli(),
		UpdateTime:  rule.UpdateTime.UnixMilli(),
	}
}
