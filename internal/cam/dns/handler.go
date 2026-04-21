package dns

import (
	"net/http"
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/middleware"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// DNSHandler DNS 管理 HTTP 处理器
type DNSHandler struct {
	svc DNSService
}

// NewDNSHandler 创建 DNS 处理器
func NewDNSHandler(svc DNSService) *DNSHandler {
	return &DNSHandler{svc: svc}
}

// RegisterRoutes 注册 DNS 路由
func (h *DNSHandler) RegisterRoutes(g *gin.RouterGroup) {
	dns := g.Group("/dns")
	dns.GET("/domains", h.ListDomains)
	dns.GET("/domains/:domain/records", h.ListRecords)
	dns.POST("/domains/:domain/records", h.CreateRecord)
	dns.PUT("/domains/:domain/records/:record_id", h.UpdateRecord)
	dns.DELETE("/domains/:domain/records/:record_id", h.DeleteRecord)
	dns.POST("/domains/:domain/records/batch-delete", h.BatchDeleteRecords)
	dns.GET("/records/search", h.SearchRecords)
	dns.GET("/stats", h.GetStats)
}

// ListDomains 查询域名列表
func (h *DNSHandler) ListDomains(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	logger := elog.DefaultLogger
	logger.Info("DNS ListDomains 请求",
		elog.String("tenantID", tenantID),
		elog.String("X-Tenant-ID-header", ctx.GetHeader("X-Tenant-ID")))

	offset, _ := strconv.ParseInt(ctx.DefaultQuery("offset", "0"), 10, 64)
	limit, _ := strconv.ParseInt(ctx.DefaultQuery("limit", "20"), 10, 64)
	accountID, _ := strconv.ParseInt(ctx.DefaultQuery("account_id", "0"), 10, 64)

	filter := DomainFilter{
		Keyword:   ctx.Query("keyword"),
		Provider:  ctx.Query("provider"),
		AccountID: accountID,
		Offset:    offset,
		Limit:     limit,
	}

	items, total, err := h.svc.ListDomains(ctx.Request.Context(), tenantID, filter)
	if err != nil {
		logger.Error("DNS ListDomains 查询失败", elog.FieldErr(err))
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	logger.Info("DNS ListDomains 查询结果",
		elog.Int64("total", total),
		elog.Int("items", len(items)))
	ctx.JSON(http.StatusOK, web.Result(gin.H{
		"items": items,
		"total": total,
	}))
}

// ListRecords 查询解析记录列表
func (h *DNSHandler) ListRecords(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	domainName := ctx.Param("domain")

	offset, _ := strconv.ParseInt(ctx.DefaultQuery("offset", "0"), 10, 64)
	limit, _ := strconv.ParseInt(ctx.DefaultQuery("limit", "20"), 10, 64)

	filter := RecordFilter{
		Keyword:    ctx.Query("keyword"),
		RecordType: ctx.Query("record_type"),
		Offset:     offset,
		Limit:      limit,
	}

	items, total, err := h.svc.ListRecords(ctx.Request.Context(), tenantID, domainName, filter)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(gin.H{
		"items": items,
		"total": total,
	}))
}

// CreateRecord 创建解析记录
func (h *DNSHandler) CreateRecord(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	domainName := ctx.Param("domain")

	var req CreateRecordReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}

	record, err := h.svc.CreateRecord(ctx.Request.Context(), tenantID, domainName, req)
	if err != nil {
		if isValidationError(err) {
			ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, err.Error()))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(record))
}

// UpdateRecord 修改解析记录
func (h *DNSHandler) UpdateRecord(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	domainName := ctx.Param("domain")
	recordID := ctx.Param("record_id")

	var req UpdateRecordReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}

	record, err := h.svc.UpdateRecord(ctx.Request.Context(), tenantID, domainName, recordID, req)
	if err != nil {
		if isValidationError(err) {
			ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, err.Error()))
			return
		}
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(record))
}

// DeleteRecord 删除解析记录
func (h *DNSHandler) DeleteRecord(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	domainName := ctx.Param("domain")
	recordID := ctx.Param("record_id")

	if err := h.svc.DeleteRecord(ctx.Request.Context(), tenantID, domainName, recordID); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(nil))
}

// BatchDeleteRecords 批量删除解析记录
func (h *DNSHandler) BatchDeleteRecords(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	domainName := ctx.Param("domain")

	var body struct {
		RecordIDs []string `json:"record_ids"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "invalid request body"))
		return
	}
	if len(body.RecordIDs) == 0 {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.ParamsError, "record_ids cannot be empty"))
		return
	}

	result, err := h.svc.BatchDeleteRecords(ctx.Request.Context(), tenantID, domainName, body.RecordIDs)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(result))
}

// SearchRecords 跨域名搜索解析记录（用于拓扑入口域名选择）
func (h *DNSHandler) SearchRecords(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	keyword := ctx.Query("keyword")
	limit, _ := strconv.ParseInt(ctx.DefaultQuery("limit", "50"), 10, 64)

	if keyword == "" {
		ctx.JSON(http.StatusOK, web.Result(gin.H{"items": []interface{}{}, "total": 0}))
		return
	}

	items, total, err := h.svc.SearchRecords(ctx.Request.Context(), tenantID, keyword, limit)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(gin.H{
		"items": items,
		"total": total,
	}))
}

// GetStats 查询 DNS 统计数据
func (h *DNSHandler) GetStats(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	stats, err := h.svc.GetStats(ctx.Request.Context(), tenantID)
	if err != nil {
		ctx.JSON(http.StatusOK, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(stats))
}

// isValidationError 判断是否为校验错误
func isValidationError(err error) bool {
	validationErrors := []error{
		ErrDNSRecordInvalid, ErrDNSRecordRREmpty, ErrDNSRecordValueEmpty,
		ErrDNSRecordTTLRange, ErrDNSMXPriority, ErrDNSInvalidIPv4,
		ErrDNSInvalidIPv6, ErrDNSInvalidDomain, ErrDNSTXTTooLong,
	}
	for _, ve := range validationErrors {
		if err.Error() == ve.Error() {
			return true
		}
	}
	return false
}
