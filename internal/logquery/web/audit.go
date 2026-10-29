package web

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/logquery/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// errInvalidWindow 窗口越界(超出 /types 声明 max_window_days)的审计失败原因。
var errInvalidWindow = errors.New("time window exceeds max_window_days")

// LogAuditSink 日志检索审计落袋端(消费方接口)。实现由组合根(ioc)基于
// internal/audit 领域 DAO 注入,使 logquery 域无需 import internal/audit
// (与 shared/middleware 的 AuditSink 同构思路:shared 与 logquery 都保持叶子)。
type LogAuditSink interface {
	WriteLogQuery(ctx context.Context, e LogQueryAudit) error
}

// LogQueryAudit 结构化日志检索/导出审计记录(纯基本类型)。
// 刻意不携带关键字(query)与字段筛选值(filters)——二者可能含敏感检索词,
// 审计只保留查询范围与结果量,避免把敏感日志内容/凭据落库。
type LogQueryAudit struct {
	// Operation 操作类型判别: "search"(默认) 或 "export"。由落袋端据此
	// 翻译为 internal/audit 领域的 log_search / log_export。
	Operation    string
	OperatorID   string
	OperatorName string
	TenantID     int64
	LogType      string
	Clouds       []string
	AccountIDs   []int64
	Resources    []string
	StartTime    int64
	EndTime      int64
	ResultCount  int
	Partial      bool
	Success      bool
	DurationMs   int64
	Exported     bool
	ClientIP     string
	UserAgent    string
	RequestID    string
	Ctime        int64
}

// Summary 审计体的脱敏摘要(request_body 落库用):只保留查询范围,不落
// query/filters。空切片用 nil 输出 null 而非 [],保持审计体稳定可读。
func (e LogQueryAudit) Summary() string {
	b, err := json.Marshal(struct {
		LogType     string   `json:"log_type"`
		Clouds      []string `json:"clouds,omitempty"`
		AccountIDs  []int64  `json:"account_ids,omitempty"`
		Resources   []string `json:"resources,omitempty"`
		StartTime   int64    `json:"start_time"`
		EndTime     int64    `json:"end_time"`
		ResultCount int      `json:"result_count"`
		Exported    bool     `json:"exported"`
	}{
		LogType:     e.LogType,
		Clouds:      e.Clouds,
		AccountIDs:  e.AccountIDs,
		Resources:   e.Resources,
		StartTime:   e.StartTime,
		EndTime:     e.EndTime,
		ResultCount: e.ResultCount,
		Exported:    e.Exported,
	})
	if err != nil {
		return ""
	}
	return string(b)
}

// SetAuditSink 设置审计落袋端(可选:ioc 组合根注入;nil 时审计为 no-op)。
func (h *LogQueryHandler) SetAuditSink(sink LogAuditSink) {
	h.audit = sink
}

// recordSearchAudit 异步写入一条检索审计(不阻塞查询响应;写失败仅告警,
// 不把审计故障上抛为查询故障)。
func (h *LogQueryHandler) recordSearchAudit(c *gin.Context, req searchRequest, resp *service.SearchResponse, err error, started time.Time) {
	if h.audit == nil {
		return
	}
	entry := LogQueryAudit{
		Operation:    "search",
		OperatorID:   strconv.FormatInt(middleware.GetUid(c), 10),
		OperatorName: middleware.GetUsername(c),
		TenantID:     middleware.GetTenantID(c),
		LogType:      req.LogType,
		Clouds:       append([]string(nil), req.Clouds...),
		AccountIDs:   append([]int64(nil), req.AccountIDs...),
		Resources:    append([]string(nil), req.Resources...),
		StartTime:    req.StartTime,
		EndTime:      req.EndTime,
		DurationMs:   time.Since(started).Milliseconds(),
		ClientIP:     c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
		RequestID:    middleware.GetRequestID(c),
		Ctime:        time.Now().UnixMilli(),
	}
	if err != nil {
		entry.Success = false
	} else if resp != nil {
		entry.Success = true
		entry.ResultCount = resp.Total
		entry.Partial = hasPartialFailure(resp.Sources)
	}

	h.writeAudit(entry)
}

// recordExportAudit 异步写入一条导出审计(导出 CSV 仍由前端生成,本端点只落
// 审计记录;同样不携带关键字与字段筛选值)。
func (h *LogQueryHandler) recordExportAudit(c *gin.Context, req exportAuditRequest) {
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
		ResultCount:  req.Count,
		Success:      true,
		Exported:     true,
		ClientIP:     c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
		RequestID:    middleware.GetRequestID(c),
		Ctime:        time.Now().UnixMilli(),
	}
	h.writeAudit(entry)
}

// writeAudit 异步落袋一条审计(不阻塞请求;写失败仅告警)。
func (h *LogQueryHandler) writeAudit(entry LogQueryAudit) {
	go func() {
		ctx := context.WithoutCancel(context.Background())
		if werr := h.audit.WriteLogQuery(ctx, entry); werr != nil {
			elog.DefaultLogger.Warn("写入日志审计失败",
				elog.FieldErr(werr),
				elog.String("operation", entry.Operation),
				elog.String("log_type", entry.LogType),
				elog.Int64("tenant_id", entry.TenantID))
		}
	}()
}

// hasPartialFailure 联邦检索是否部分成功:至少一个源成功且至少一个源失败。
// sources 为空(无账号)视为成功(空结果),不算部分失败。
func hasPartialFailure(sources []service.SourceOutcome) bool {
	var ok, failed bool
	for _, s := range sources {
		if s.Error != "" {
			failed = true
		} else {
			ok = true
		}
	}
	return ok && failed
}
