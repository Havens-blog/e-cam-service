package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/logquery/export"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-cloudx-sdk/logquery"
	"github.com/ecodeclub/ginx/session"
	"github.com/gin-gonic/gin"
)

// createExportRequest POST /export 请求体(含关键字/字段筛选,仅内存传递,不落库)。
type createExportRequest struct {
	LogType    string                 `json:"log_type" binding:"required"`
	StartTime  int64                  `json:"start_time" binding:"required"`
	EndTime    int64                  `json:"end_time" binding:"required"`
	Query      string                 `json:"query"`
	Clouds     []string               `json:"clouds"`
	AccountIDs []int64                `json:"account_ids"`
	Regions    []string               `json:"regions"`
	Resources  []string               `json:"resources"`
	Filters    []logquery.FieldFilter `json:"filters"`
}

// CreateExport POST /export 创建异步导出任务(校验导出权限 + 落 log_export 审计)。
func (h *LogQueryHandler) CreateExport(c *gin.Context) {
	if h.exportSvc == nil {
		writeError(c, http.StatusServiceUnavailable, "export service unavailable")
		return
	}
	if !h.canExport(c) {
		writeError(c, http.StatusForbidden, "no permission to export logs")
		return
	}
	var req createExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		rejectBadRequest(c, "bind", "invalid request: "+err.Error())
		return
	}
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	if !checkWindow(c, req.LogType, req.StartTime, req.EndTime) {
		return
	}

	taskID, err := h.exportSvc.Create(c.Request.Context(), tenantID,
		strconv.FormatInt(middleware.GetUid(c), 10), middleware.GetUsername(c), export.CreateRequest{
			LogType:    req.LogType,
			StartTime:  req.StartTime,
			EndTime:    req.EndTime,
			Query:      req.Query,
			Clouds:     req.Clouds,
			AccountIDs: req.AccountIDs,
			Regions:    req.Regions,
			Resources:  req.Resources,
			Filters:    req.Filters,
		})
	if err != nil {
		rejectBadRequest(c, "validate", err.Error())
		return
	}

	// 落一条 log_export 审计(任务创建即记;最终条数在任务台账里)。
	h.recordExportTaskAudit(c, req, taskID)

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": gin.H{"task_id": taskID}})
}

// GetExport GET /export/:id 查询任务状态。
func (h *LogQueryHandler) GetExport(c *gin.Context) {
	if h.exportSvc == nil {
		writeError(c, http.StatusServiceUnavailable, "export service unavailable")
		return
	}
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	task, err := h.exportSvc.Get(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": task})
}

// CancelExport POST /export/:id/cancel 取消任务。
func (h *LogQueryHandler) CancelExport(c *gin.Context) {
	if h.exportSvc == nil {
		writeError(c, http.StatusServiceUnavailable, "export service unavailable")
		return
	}
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	if err := h.exportSvc.Cancel(c.Request.Context(), tenantID, c.Param("id")); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": nil})
}

// DownloadExport GET /export/:id/download 下载导出文件。
func (h *LogQueryHandler) DownloadExport(c *gin.Context) {
	if h.exportSvc == nil {
		writeError(c, http.StatusServiceUnavailable, "export service unavailable")
		return
	}
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	path, err := h.exportSvc.FilePath(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+c.Param("id")+`.csv"`)
	c.File(path)
}

// canExport 导出权限判定:admin / 持 cam:logs:export / 空清单默认放行。
func (h *LogQueryHandler) canExport(c *gin.Context) bool {
	isAdmin := false
	var codes []string
	if v, ok := c.Get(session.CtxSessionKey); ok {
		if sess, ok := v.(session.Session); ok {
			data := sess.Claims().Data
			isAdmin = data["is_admin"] == "true"
			codes = parseClaimCodes(data["authorized_codes"], data["permissions"])
		}
	}
	return export.CanExport(isAdmin, codes)
}

// recordExportTaskAudit 异步落 log_export 审计(任务创建即记,含脱敏范围)。
func (h *LogQueryHandler) recordExportTaskAudit(c *gin.Context, req createExportRequest, taskID string) {
	if h.audit == nil {
		return
	}
	entry := LogQueryAudit{
		Operation:    "export",
		OperatorID:   strconv.FormatInt(middleware.GetUid(c), 10),
		OperatorName: middleware.GetUsername(c),
		TenantID:     middleware.GetTenantID(c),
		LogType:      req.LogType,
		Clouds:       append([]string(nil), req.Clouds...),
		AccountIDs:   append([]int64(nil), req.AccountIDs...),
		Resources:    append([]string(nil), req.Resources...),
		StartTime:    req.StartTime,
		EndTime:      req.EndTime,
		Success:      true,
		Exported:     true,
		RequestID:    taskID,
		ClientIP:     c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
		Ctime:        time.Now().UnixMilli(),
	}
	h.writeAudit(entry)
}

// parseClaimCodes 解析能力码声明(JSON 数组/逗号分隔)。
func parseClaimCodes(raws ...string) []string {
	var codes []string
	for _, raw := range raws {
		// 必须 TrimSpace：能力码可能来自 header/claims，带首尾空格时
		// CanExport 的精确匹配（c == "cam:logs:export"）会失败，导致有权限的用户被拒。
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if raw[0] == '[' {
			var arr []string
			if err := json.Unmarshal([]byte(raw), &arr); err == nil {
				codes = append(codes, arr...)
				continue
			}
		}
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				codes = append(codes, part)
			}
		}
	}
	return codes
}
