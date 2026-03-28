package template

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/middleware"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/gin-gonic/gin"
)

// TemplateHandler 主机模板 HTTP 处理器
type TemplateHandler struct {
	svc             TemplateService
	accountProvider AccountProvider
	adapterFactory  AdapterFactory
}

// NewTemplateHandler 创建处理器
func NewTemplateHandler(svc TemplateService, accountProvider AccountProvider, adapterFactory AdapterFactory) *TemplateHandler {
	return &TemplateHandler{
		svc:             svc,
		accountProvider: accountProvider,
		adapterFactory:  adapterFactory,
	}
}

// RegisterRoutes 注册路由
func (h *TemplateHandler) RegisterRoutes(g *gin.RouterGroup) {
	// 模板 CRUD
	tmpl := g.Group("/templates")
	tmpl.POST("", h.CreateTemplate)
	tmpl.GET("", h.ListTemplates)
	tmpl.GET("/:id", h.GetTemplate)
	tmpl.PUT("/:id", h.UpdateTemplate)
	tmpl.DELETE("/:id", h.DeleteTemplate)
	tmpl.POST("/:id/provision", h.ProvisionFromTemplate)

	// 直接创建
	g.POST("/provision", h.DirectProvision)

	// 创建任务
	tasks := g.Group("/provision-tasks")
	tasks.GET("", h.ListProvisionTasks)
	tasks.GET("/:id", h.GetProvisionTask)

	// 云资源查询
	res := g.Group("/cloud-resources")
	res.GET("/regions", h.ListRegions)
	res.GET("/instance-types", h.ListInstanceTypes)
	res.GET("/images", h.ListImages)
	res.GET("/vpcs", h.ListVPCs)
	res.GET("/subnets", h.ListSubnets)
	res.GET("/security-groups", h.ListSecurityGroups)
}

// ==================== 模板 CRUD ====================

func (h *TemplateHandler) CreateTemplate(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	var req CreateTemplateReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}
	tmpl, err := h.svc.CreateTemplate(ctx.Request.Context(), tenantID, req)
	if err != nil {
		h.handleError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, web.Result(tmpl))
}

func (h *TemplateHandler) GetTemplate(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid id"))
		return
	}
	tmpl, err := h.svc.GetTemplate(ctx.Request.Context(), tenantID, id)
	if err != nil {
		h.handleError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, web.Result(tmpl))
}

func (h *TemplateHandler) ListTemplates(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	offset, _ := strconv.ParseInt(ctx.DefaultQuery("offset", "0"), 10, 64)
	limit, _ := strconv.ParseInt(ctx.DefaultQuery("limit", "20"), 10, 64)
	accountID, _ := strconv.ParseInt(ctx.DefaultQuery("cloud_account_id", "0"), 10, 64)

	filter := TemplateFilter{
		Name:           ctx.Query("name"),
		Provider:       ctx.Query("provider"),
		CloudAccountID: accountID,
		Offset:         offset,
		Limit:          limit,
	}
	list, total, err := h.svc.ListTemplates(ctx.Request.Context(), tenantID, filter)
	if err != nil {
		h.handleError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, web.Result(gin.H{"items": list, "total": total}))
}

func (h *TemplateHandler) UpdateTemplate(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid id"))
		return
	}
	var req UpdateTemplateReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}
	if err := h.svc.UpdateTemplate(ctx.Request.Context(), tenantID, id, req); err != nil {
		h.handleError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, web.Result(nil))
}

func (h *TemplateHandler) DeleteTemplate(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid id"))
		return
	}
	if err := h.svc.DeleteTemplate(ctx.Request.Context(), tenantID, id); err != nil {
		h.handleError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, web.Result(nil))
}

// ==================== 创建主机 ====================

func (h *TemplateHandler) ProvisionFromTemplate(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid id"))
		return
	}
	var req ProvisionReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}
	taskID, err := h.svc.ProvisionFromTemplate(ctx.Request.Context(), tenantID, tenantID, id, req)
	if err != nil {
		h.handleError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, web.Result(gin.H{"task_id": taskID}))
}

func (h *TemplateHandler) DirectProvision(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	var req DirectProvisionReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}
	taskID, err := h.svc.DirectProvision(ctx.Request.Context(), tenantID, tenantID, req)
	if err != nil {
		h.handleError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, web.Result(gin.H{"task_id": taskID}))
}

// ==================== 创建任务查询 ====================

func (h *TemplateHandler) ListProvisionTasks(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	offset, _ := strconv.ParseInt(ctx.DefaultQuery("offset", "0"), 10, 64)
	limit, _ := strconv.ParseInt(ctx.DefaultQuery("limit", "20"), 10, 64)
	templateID, _ := strconv.ParseInt(ctx.DefaultQuery("template_id", "0"), 10, 64)
	startTime, _ := strconv.ParseInt(ctx.DefaultQuery("start_time", "0"), 10, 64)
	endTime, _ := strconv.ParseInt(ctx.DefaultQuery("end_time", "0"), 10, 64)

	filter := ProvisionTaskFilter{
		TemplateID: templateID,
		Status:     ctx.Query("status"),
		Source:     ctx.Query("source"),
		StartTime:  startTime,
		EndTime:    endTime,
		Offset:     offset,
		Limit:      limit,
	}
	list, total, err := h.svc.ListProvisionTasks(ctx.Request.Context(), tenantID, filter)
	if err != nil {
		h.handleError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, web.Result(gin.H{"items": list, "total": total}))
}

func (h *TemplateHandler) GetProvisionTask(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	taskID := ctx.Param("id")
	task, err := h.svc.GetProvisionTask(ctx.Request.Context(), tenantID, taskID)
	if err != nil {
		h.handleError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, web.Result(task))
}

// ==================== 云资源查询 ====================

func (h *TemplateHandler) ListRegions(ctx *gin.Context) {
	accountIDStr := ctx.Query("cloud_account_id")
	if accountIDStr == "" {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "cloud_account_id is required"))
		return
	}
	accountID, err := strconv.ParseInt(accountIDStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid cloud_account_id"))
		return
	}

	account, err := h.accountProvider.GetByID(ctx.Request.Context(), accountID)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.AccountNotFound, "cloud account not found"))
		return
	}

	adapter, err := h.adapterFactory.GetAdapter(account)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(ErrCloudAPIError, "failed to create adapter"))
		return
	}

	ecsAdapter := adapter.ECS()
	if ecsAdapter == nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(ErrCloudAPIError, "ECS adapter not available"))
		return
	}

	regions, err := ecsAdapter.GetRegions(ctx.Request.Context())
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(ErrCloudAPIError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(regions))
}

func (h *TemplateHandler) ListInstanceTypes(ctx *gin.Context) {
	rq, err := h.getResourceQueryAdapter(ctx)
	if err != nil {
		return
	}
	region := ctx.Query("region")
	if region == "" {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "region is required"))
		return
	}
	result, err := rq.ListAvailableInstanceTypes(ctx.Request.Context(), region)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(ErrCloudAPIError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(result))
}

func (h *TemplateHandler) ListImages(ctx *gin.Context) {
	rq, err := h.getResourceQueryAdapter(ctx)
	if err != nil {
		return
	}
	region := ctx.Query("region")
	if region == "" {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "region is required"))
		return
	}
	result, err := rq.ListAvailableImages(ctx.Request.Context(), region)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(ErrCloudAPIError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(result))
}

func (h *TemplateHandler) ListVPCs(ctx *gin.Context) {
	rq, err := h.getResourceQueryAdapter(ctx)
	if err != nil {
		return
	}
	region := ctx.Query("region")
	if region == "" {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "region is required"))
		return
	}
	result, err := rq.ListVPCs(ctx.Request.Context(), region)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(ErrCloudAPIError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(result))
}

func (h *TemplateHandler) ListSubnets(ctx *gin.Context) {
	rq, err := h.getResourceQueryAdapter(ctx)
	if err != nil {
		return
	}
	region := ctx.Query("region")
	vpcID := ctx.Query("vpc_id")
	if region == "" || vpcID == "" {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "region and vpc_id are required"))
		return
	}
	result, err := rq.ListSubnets(ctx.Request.Context(), region, vpcID)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(ErrCloudAPIError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(result))
}

func (h *TemplateHandler) ListSecurityGroups(ctx *gin.Context) {
	rq, err := h.getResourceQueryAdapter(ctx)
	if err != nil {
		return
	}
	region := ctx.Query("region")
	vpcID := ctx.Query("vpc_id")
	if region == "" || vpcID == "" {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "region and vpc_id are required"))
		return
	}
	result, err := rq.ListSecurityGroups(ctx.Request.Context(), region, vpcID)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(ErrCloudAPIError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(result))
}

// ==================== 辅助方法 ====================

// getResourceQueryAdapter 从请求参数中获取云资源查询适配器
func (h *TemplateHandler) getResourceQueryAdapter(ctx *gin.Context) (cloudx.ResourceQueryAdapter, error) {
	accountIDStr := ctx.Query("cloud_account_id")
	if accountIDStr == "" {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "cloud_account_id is required"))
		return nil, errors.New("cloud_account_id is required")
	}
	accountID, err := strconv.ParseInt(accountIDStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid cloud_account_id"))
		return nil, err
	}

	account, err := h.accountProvider.GetByID(ctx.Request.Context(), accountID)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.AccountNotFound, "cloud account not found"))
		return nil, err
	}

	adapter, err := h.adapterFactory.GetAdapter(account)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(ErrCloudAPIError, "failed to create adapter"))
		return nil, err
	}

	rq := adapter.ResourceQuery()
	if rq == nil {
		// 厂商未实现专用 ResourceQueryAdapter，使用通用适配器（复用已有的 VPC/VSwitch/SecurityGroup/Image 适配器）
		rq = cloudx.NewGenericResourceQueryAdapter(adapter)
	}
	return rq, nil
}

// handleError 统一错误处理
func (h *TemplateHandler) handleError(ctx *gin.Context, err error) {
	var ec errs.ErrorCode
	if errors.As(err, &ec) {
		ctx.JSON(http.StatusOK, web.ErrorResult(ec))
		return
	}
	ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
}
