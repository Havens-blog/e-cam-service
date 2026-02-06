package web

import (
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/servicetree/domain"
	"github.com/Havens-blog/e-cam-service/internal/servicetree/service"
	"github.com/Havens-blog/e-cam-service/pkg/ginx"
	"github.com/gin-gonic/gin"
)

// EnvHandler 环境 HTTP 处理器
type EnvHandler struct {
	envSvc service.EnvironmentService
}

// NewEnvHandler 创建环境处理器
func NewEnvHandler(envSvc service.EnvironmentService) *EnvHandler {
	return &EnvHandler{envSvc: envSvc}
}

// RegisterRoutes 注册环境路由
func (h *EnvHandler) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/environments")
	{
		g.POST("", ginx.WrapBody(h.CreateEnv))
		g.GET("", ginx.WrapBody(h.ListEnvs))
		g.GET("/:id", ginx.Wrap(h.GetEnv))
		g.PUT("/:id", ginx.WrapBody(h.UpdateEnv))
		g.DELETE("/:id", ginx.Wrap(h.DeleteEnv))
		g.POST("/init", ginx.Wrap(h.InitDefaultEnvs))
	}
}

func (h *EnvHandler) getTenantID(c *gin.Context) string {
	return c.GetHeader("X-Tenant-ID")
}

func (h *EnvHandler) CreateEnv(c *gin.Context, req CreateEnvReq) (ginx.Result, error) {
	tenantID := h.getTenantID(c)
	env := domain.Environment{
		Code:        req.Code,
		Name:        req.Name,
		TenantID:    tenantID,
		Description: req.Description,
		Color:       req.Color,
		Order:       req.Order,
	}

	id, err := h.envSvc.Create(c.Request.Context(), env)
	if err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}
	return ginx.Result{Data: id}, nil
}

func (h *EnvHandler) GetEnv(c *gin.Context) (ginx.Result, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return ginx.Result{Code: 400, Msg: "无效的环境ID"}, nil
	}

	env, err := h.envSvc.GetByID(c.Request.Context(), id)
	if err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}
	return ginx.Result{Data: h.toEnvVO(env)}, nil
}

func (h *EnvHandler) UpdateEnv(c *gin.Context, req UpdateEnvReq) (ginx.Result, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return ginx.Result{Code: 400, Msg: "无效的环境ID"}, nil
	}

	tenantID := h.getTenantID(c)
	env := domain.Environment{
		ID:          id,
		Code:        req.Code,
		Name:        req.Name,
		TenantID:    tenantID,
		Description: req.Description,
		Color:       req.Color,
		Order:       req.Order,
		Status:      req.Status,
	}

	if err := h.envSvc.Update(c.Request.Context(), env); err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}
	return ginx.Result{Msg: "更新成功"}, nil
}

func (h *EnvHandler) DeleteEnv(c *gin.Context) (ginx.Result, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return ginx.Result{Code: 400, Msg: "无效的环境ID"}, nil
	}

	if err := h.envSvc.Delete(c.Request.Context(), id); err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}
	return ginx.Result{Msg: "删除成功"}, nil
}

func (h *EnvHandler) ListEnvs(c *gin.Context, req ListEnvReq) (ginx.Result, error) {
	tenantID := h.getTenantID(c)
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	filter := domain.EnvironmentFilter{
		TenantID: tenantID,
		Code:     req.Code,
		Status:   req.Status,
		Offset:   int64((req.Page - 1) * req.PageSize),
		Limit:    int64(req.PageSize),
	}

	envs, total, err := h.envSvc.List(c.Request.Context(), filter)
	if err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}

	vos := make([]EnvironmentVO, len(envs))
	for i, e := range envs {
		vos[i] = h.toEnvVO(e)
	}

	return ginx.Result{Data: map[string]any{"list": vos, "total": total}}, nil
}

func (h *EnvHandler) InitDefaultEnvs(c *gin.Context) (ginx.Result, error) {
	tenantID := h.getTenantID(c)
	if tenantID == "" {
		return ginx.Result{Code: 400, Msg: "租户ID不能为空"}, nil
	}

	if err := h.envSvc.InitDefaultEnvs(c.Request.Context(), tenantID); err != nil {
		return ginx.Result{Code: 500, Msg: err.Error()}, nil
	}
	return ginx.Result{Msg: "初始化成功"}, nil
}

func (h *EnvHandler) toEnvVO(env domain.Environment) EnvironmentVO {
	return EnvironmentVO{
		ID:          env.ID,
		Code:        env.Code,
		Name:        env.Name,
		Description: env.Description,
		Color:       env.Color,
		Order:       env.Order,
		Status:      env.Status,
		CreateTime:  env.CreateTime.UnixMilli(),
		UpdateTime:  env.UpdateTime.UnixMilli(),
	}
}
