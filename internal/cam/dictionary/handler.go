package dictionary

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/middleware"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"
	"github.com/gin-gonic/gin"
)

var codeRegexp = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// UpdateStatusReq 更新状态请求
type UpdateStatusReq struct {
	Status string `json:"status"`
}

// DictHandler 数据字典 HTTP 处理器
type DictHandler struct {
	svc DictService
}

// NewDictHandler 创建数据字典处理器
func NewDictHandler(svc DictService) *DictHandler {
	return &DictHandler{svc: svc}
}

// RegisterRoutes 注册数据字典路由
func (h *DictHandler) RegisterRoutes(g *gin.RouterGroup) {
	dict := g.Group("/dict")
	// 字典类型
	dict.POST("/types", h.CreateType)
	dict.PUT("/types/:id", h.UpdateType)
	dict.DELETE("/types/:id", h.DeleteType)
	dict.GET("/types", h.ListTypes)
	dict.PUT("/types/:id/status", h.UpdateTypeStatus)
	// 字典项
	dict.POST("/types/:type_id/items", h.CreateItem)
	dict.GET("/types/:type_id/items", h.ListItems)
	dict.PUT("/items/:id", h.UpdateItem)
	dict.DELETE("/items/:id", h.DeleteItem)
	dict.PUT("/items/:id/status", h.UpdateItemStatus)
	// 数据查询
	dict.GET("/data/:code", h.GetByCode)
	dict.GET("/data/batch", h.BatchGetByCodes)
}

// ==================== 字典类型处理 ====================

// CreateType 创建字典类型
func (h *DictHandler) CreateType(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	var req CreateTypeReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}

	// 校验 code
	if req.Code == "" {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "code is required"))
		return
	}
	if !codeRegexp.MatchString(req.Code) {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "code must be alphanumeric and underscore"))
		return
	}

	// 校验 name
	if req.Name == "" {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "name is required"))
		return
	}

	dt, err := h.svc.CreateType(ctx.Request.Context(), tenantID, req)
	if err != nil {
		if errors.Is(err, ErrDictTypeCodeExists) {
			ctx.JSON(http.StatusOK, web.ErrorResult(errs.DictTypeCodeExists))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(dt))
}

// UpdateType 更新字典类型
func (h *DictHandler) UpdateType(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid id"))
		return
	}

	var req UpdateTypeReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}

	if err := h.svc.UpdateType(ctx.Request.Context(), tenantID, id, req); err != nil {
		if errors.Is(err, ErrDictTypeNotFound) {
			ctx.JSON(http.StatusOK, web.ErrorResult(errs.DictTypeNotFound))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(nil))
}

// DeleteType 删除字典类型
func (h *DictHandler) DeleteType(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid id"))
		return
	}

	if err := h.svc.DeleteType(ctx.Request.Context(), tenantID, id); err != nil {
		if errors.Is(err, ErrDictTypeHasItems) {
			ctx.JSON(http.StatusOK, web.ErrorResult(errs.DictTypeHasItems))
			return
		}
		if errors.Is(err, ErrDictTypeNotFound) {
			ctx.JSON(http.StatusOK, web.ErrorResult(errs.DictTypeNotFound))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(nil))
}

// ListTypes 查询字典类型列表
func (h *DictHandler) ListTypes(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	keyword := ctx.Query("keyword")
	status := ctx.Query("status")
	offset, _ := strconv.ParseInt(ctx.DefaultQuery("offset", "0"), 10, 64)
	limit, _ := strconv.ParseInt(ctx.DefaultQuery("limit", "20"), 10, 64)

	filter := TypeFilter{
		Keyword: keyword,
		Status:  status,
		Offset:  offset,
		Limit:   limit,
	}

	types, total, err := h.svc.ListTypes(ctx.Request.Context(), tenantID, filter)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(gin.H{
		"items": types,
		"total": total,
	}))
}

// UpdateTypeStatus 更新字典类型状态
func (h *DictHandler) UpdateTypeStatus(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid id"))
		return
	}

	var req UpdateStatusReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}

	if err := h.svc.UpdateTypeStatus(ctx.Request.Context(), tenantID, id, req.Status); err != nil {
		if errors.Is(err, ErrDictTypeNotFound) {
			ctx.JSON(http.StatusOK, web.ErrorResult(errs.DictTypeNotFound))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(nil))
}

// ==================== 字典项处理 ====================

// CreateItem 创建字典项
func (h *DictHandler) CreateItem(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	typeID, err := strconv.ParseInt(ctx.Param("type_id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid type_id"))
		return
	}

	var req CreateItemReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}

	item, err := h.svc.CreateItem(ctx.Request.Context(), tenantID, typeID, req)
	if err != nil {
		if errors.Is(err, ErrDictItemValueExists) {
			ctx.JSON(http.StatusOK, web.ErrorResult(errs.DictItemValueExists))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(item))
}

// ListItems 查询字典项列表
func (h *DictHandler) ListItems(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	typeID, err := strconv.ParseInt(ctx.Param("type_id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid type_id"))
		return
	}

	items, err := h.svc.ListItems(ctx.Request.Context(), tenantID, typeID)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(items))
}

// UpdateItem 更新字典项
func (h *DictHandler) UpdateItem(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid id"))
		return
	}

	var req UpdateItemReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}

	if err := h.svc.UpdateItem(ctx.Request.Context(), tenantID, id, req); err != nil {
		if errors.Is(err, ErrDictItemNotFound) {
			ctx.JSON(http.StatusOK, web.ErrorResult(errs.DictItemNotFound))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(nil))
}

// DeleteItem 删除字典项
func (h *DictHandler) DeleteItem(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid id"))
		return
	}

	if err := h.svc.DeleteItem(ctx.Request.Context(), tenantID, id); err != nil {
		if errors.Is(err, ErrDictItemNotFound) {
			ctx.JSON(http.StatusOK, web.ErrorResult(errs.DictItemNotFound))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(nil))
}

// UpdateItemStatus 更新字典项状态
func (h *DictHandler) UpdateItemStatus(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid id"))
		return
	}

	var req UpdateStatusReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}

	if err := h.svc.UpdateItemStatus(ctx.Request.Context(), tenantID, id, req.Status); err != nil {
		if errors.Is(err, ErrDictItemNotFound) {
			ctx.JSON(http.StatusOK, web.ErrorResult(errs.DictItemNotFound))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(nil))
}

// ==================== 数据查询处理 ====================

// GetByCode 按 code 获取启用的字典项
func (h *DictHandler) GetByCode(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	code := ctx.Param("code")

	items, err := h.svc.GetByCode(ctx.Request.Context(), tenantID, code)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(items))
}

// BatchGetByCodes 批量按 codes 获取字典项映射
func (h *DictHandler) BatchGetByCodes(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	codesStr := ctx.Query("codes")

	if codesStr == "" {
		ctx.JSON(http.StatusOK, web.Result(map[string][]DictItem{}))
		return
	}

	codes := strings.Split(codesStr, ",")
	result, err := h.svc.BatchGetByCodes(ctx.Request.Context(), tenantID, codes)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(result))
}
