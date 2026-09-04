package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

// DiscoveryNotAfterPending 未登记条目 notAfter 占位显示（SC-2：inLedger 条目
// 为台账值，未登记条目占位显示——cert_references 无 notAfter 字段且本功能
// 不改其表结构）。
const DiscoveryNotAfterPending = "—（导入后补全）"

// DiscoveryHandler 云端发现导入端点（cert-cloud-discovery-import 任务 3 查询面 +
// 任务 5 会话面）：发现预览/快照状态查询 + 勾选条目导入会话/进度轮询。
type DiscoveryHandler struct {
	svc     service.DiscoveryPreviewService
	imports service.DiscoveryImportService
}

// NewDiscoveryHandler 创建发现导入 handler（imports 为任务 4 会话编排服务，
// 任务 5 装配注入）。
func NewDiscoveryHandler(svc service.DiscoveryPreviewService, imports service.DiscoveryImportService) *DiscoveryHandler {
	return &DiscoveryHandler{svc: svc, imports: imports}
}

// RegisterRoutes 注册发现导入端点（导入类端点沿用 RoleOpsEngineer，权限矩阵
// 同 /reverse、/:id/scan；SC-8 四端点 preview/snapshot-status/import/progress
// 均限运维工程师）：
//
//	GET  /api/v1/certs/discovery/preview            发现预览（纯 DB 聚合）
//	GET  /api/v1/certs/discovery/snapshot-status    最近快照状态（引导轮询）
//	POST /api/v1/certs/discovery/import             勾选条目创建导入会话（202）
//	GET  /api/v1/certs/discovery/import/:sessionId  会话进度轮询
//
// 注意 Gin 通配顺序：/discovery 静态段先于 ledger /:id 注册（与 /reverse 同理）。
func (h *DiscoveryHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/discovery/preview", RequireRoles(RoleOpsEngineer), h.Preview)
	g.GET("/discovery/snapshot-status", RequireRoles(RoleOpsEngineer), h.SnapshotStatus)
	g.POST("/discovery/import", RequireRoles(RoleOpsEngineer), h.Import)
	g.GET("/discovery/import/:sessionId", RequireRoles(RoleOpsEngineer), h.ImportProgress)
}

// DiscoveryPreviewEntryVO 预览唯一证书条目（AC 七类字段：cloud/accountKey/
// cloudCertId/refCount/inLedger/notAfter/parseable；label 为可读名补充）。
type DiscoveryPreviewEntryVO struct {
	Cloud       string `json:"cloud"`
	AccountKey  string `json:"accountKey"`
	CloudCertID string `json:"cloudCertId"`
	Label       string `json:"label,omitempty"` // 引用 resourceId 采样（cas=证书名称、cdn/waf=域名、alb/nlb=复合 ID）
	RefCount    int    `json:"refCount"`
	InLedger    bool   `json:"inLedger"`
	NotAfter    string `json:"notAfter"` // 台账 RFC3339；未登记为占位文案
	Parseable   bool   `json:"parseable"`
	ParseReason string `json:"parseReason,omitempty"` // unsupported_cloud/iam_hosted/deferred_parse
}

// DiscoveryPreviewVO 预览响应（另含 snapshotStartedAt，前端按超 7 天重扫提示）。
type DiscoveryPreviewVO struct {
	SnapshotID        string                    `json:"snapshotId"`
	SnapshotStartedAt string                    `json:"snapshotStartedAt"`
	Count             int                       `json:"count"`
	Items             []DiscoveryPreviewEntryVO `json:"items"`
}

// DiscoverySnapshotStatusVO 快照状态响应。零快照空态：hasSnapshot=false
// （200 空态，区别于 preview 的 NO_SNAPSHOT 409——空态引导"触发首次扫描"，
// NO_SNAPSHOT 引导"等待/重扫后进入预览"，见 service 层实现注记）。
type DiscoverySnapshotStatusVO struct {
	HasSnapshot     bool                   `json:"hasSnapshot"`
	SnapshotID      string                 `json:"snapshotId,omitempty"`
	Status          string                 `json:"status,omitempty"` // running/done/failed
	StartedAt       string                 `json:"startedAt,omitempty"`
	FailReason      string                 `json:"failReason,omitempty"`
	PartialFailures []ScanChannelFailureVO `json:"partialFailures"`
}

// Preview GET /api/v1/certs/discovery/preview —— 基于最近 done 快照的
// 唯一证书清单（无 done 快照 → 409 NO_SNAPSHOT）。
func (h *DiscoveryHandler) Preview(c *gin.Context) {
	v, err := h.svc.Preview(c.Request.Context())
	if err != nil {
		WriteError(c, err)
		return
	}
	WriteOK(c, http.StatusOK, toDiscoveryPreviewVO(v), nil)
}

// SnapshotStatus GET /api/v1/certs/discovery/snapshot-status —— 最近快照
// 状态查询（无快照引导轮询：running→done 进预览 / failed 展示 partialFailures）。
func (h *DiscoveryHandler) SnapshotStatus(c *gin.Context) {
	v, err := h.svc.SnapshotStatus(c.Request.Context())
	if err != nil {
		WriteError(c, err)
		return
	}
	WriteOK(c, http.StatusOK, toDiscoverySnapshotStatusVO(v), nil)
}

// ---------------------------------------------------------------------
// 会话面端点（任务 5）
// ---------------------------------------------------------------------

// discoveryImportRequest POST /discovery/import 请求体（预览勾选条目清单）。
type discoveryImportRequest struct {
	Items []discoveryImportItemRequest `json:"items"`
}

// discoveryImportItemRequest 勾选条目三元组（cloud/accountKey/cloudCertId，
// 与预览条目及会话实体定位口径一致）。
type discoveryImportItemRequest struct {
	Cloud       string `json:"cloud"`
	AccountKey  string `json:"accountKey"`
	CloudCertID string `json:"cloudCertId"`
}

// DiscoveryImportSessionVO 发现导入会话同构响应（POST /discovery/import 202 与
// GET /discovery/import/:sessionId 共用，BatchVO 同构风格：会话句柄/状态/进度/
// 条目；字段名对齐任务 2 会话实体与前端 cert.ts DiscoveryImportSession）。
type DiscoveryImportSessionVO struct {
	SessionID  string                    `json:"sessionId"`
	Status     string                    `json:"status"` // running/completed/partial_failed
	Items      []DiscoveryImportItemVO   `json:"items"`
	Progress   DiscoveryImportProgressVO `json:"progress"`
	CreatedAt  string                    `json:"createdAt"`
	FinishedAt *string                   `json:"finishedAt,omitempty"` // 终态时点（终态可判依据之一）
}

// DiscoveryImportItemVO 发现导入逐条目结果（result=success 时 mappedCertId 有值、
// errorReason 承载幂等重放说明；result=failed 时 errorReason 为错误码+静态文案）。
type DiscoveryImportItemVO struct {
	Cloud        string `json:"cloud"`
	AccountKey   string `json:"accountKey"`
	CloudCertID  string `json:"cloudCertId"`
	Result       string `json:"result"` // pending/success/failed
	MappedCertID string `json:"mappedCertId,omitempty"`
	ErrorReason  string `json:"errorReason,omitempty"`
}

// DiscoveryImportProgressVO 发现导入进度 {total, succeeded, failed}。
type DiscoveryImportProgressVO struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

// Import POST /api/v1/certs/discovery/import —— 勾选条目创建导入会话。
// 202 语义（ImportBatch 同构）：会话先持久化再异步执行（浏览器中断不丢结果），
// 响应为 pending 初始快照，逐条结果与终态经进度端点轮询获取。空清单/条目缺
// 三元组字段返回结构化 400。
func (h *DiscoveryHandler) Import(c *gin.Context) {
	var req discoveryImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest, "invalid request body")
		return
	}
	if len(req.Items) == 0 {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest, "items is required")
		return
	}
	items := make([]service.DiscoveryImportItemInput, 0, len(req.Items))
	for _, it := range req.Items {
		if strings.TrimSpace(it.Cloud) == "" || strings.TrimSpace(it.AccountKey) == "" || strings.TrimSpace(it.CloudCertID) == "" {
			WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest,
				"items entry requires cloud, accountKey and cloudCertId")
			return
		}
		items = append(items, service.DiscoveryImportItemInput{
			Cloud: it.Cloud, AccountKey: it.AccountKey, CloudCertID: it.CloudCertID,
		})
	}

	operator := middleware.GetUsername(c)
	if operator == "" {
		operator = "unknown"
	}
	sessionID, err := h.imports.ImportFromDiscovery(c.Request.Context(), items, operator)
	if err != nil {
		WriteError(c, err)
		return
	}

	// 202 初始快照：按请求构造 pending 形态（异步执行已在途，读回会话可能已
	// 含部分结果——初始快照语义取确定形态，与 ImportBatch 一致）。
	vo := DiscoveryImportSessionVO{
		SessionID: sessionID,
		Status:    string(domain.DiscoveryImportRunning),
		Items:     make([]DiscoveryImportItemVO, 0, len(items)),
		Progress:  DiscoveryImportProgressVO{Total: len(items)},
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	for _, it := range items {
		vo.Items = append(vo.Items, DiscoveryImportItemVO{
			Cloud: it.Cloud, AccountKey: it.AccountKey, CloudCertID: it.CloudCertID,
			Result: string(domain.DiscoveryItemPending),
		})
	}
	WriteOK(c, http.StatusAccepted, vo, nil)
}

// ImportProgress GET /api/v1/certs/discovery/import/:sessionId —— 会话进度轮询
// （POST 同构响应；终态 completed/partial_failed 由 status/finishedAt 可判）。
func (h *DiscoveryHandler) ImportProgress(c *gin.Context) {
	sess, err := h.imports.GetSession(c.Request.Context(), c.Param("sessionId"))
	if err != nil {
		WriteError(c, err)
		return
	}
	WriteOK(c, http.StatusOK, toDiscoveryImportSessionVO(sess), nil)
}

// ---------------------------------------------------------------------
// VO 转换
// ---------------------------------------------------------------------

// toDiscoveryPreviewVO 服务结果 → 预览 VO（未登记 notAfter 占位显示；
// items 空集为 [] 而非 null）。
func toDiscoveryPreviewVO(v service.DiscoveryPreview) DiscoveryPreviewVO {
	items := make([]DiscoveryPreviewEntryVO, 0, len(v.Items))
	for _, it := range v.Items {
		notAfter := DiscoveryNotAfterPending
		if it.NotAfter != nil {
			notAfter = formatTime(*it.NotAfter)
		}
		items = append(items, DiscoveryPreviewEntryVO{
			Cloud:       it.Cloud,
			AccountKey:  it.AccountKey,
			CloudCertID: it.CloudCertID,
			Label:       it.Label,
			RefCount:    it.RefCount,
			InLedger:    it.InLedger,
			NotAfter:    notAfter,
			Parseable:   it.Parseable,
			ParseReason: it.ParseReason,
		})
	}
	return DiscoveryPreviewVO{
		SnapshotID:        v.SnapshotID,
		SnapshotStartedAt: formatTime(v.SnapshotStartedAt),
		Count:             len(items),
		Items:             items,
	}
}

// toDiscoverySnapshotStatusVO 服务结果 → 快照状态 VO（partialFailures 空集
// 为 [] 而非 null）。
func toDiscoverySnapshotStatusVO(v service.DiscoverySnapshotStatus) DiscoverySnapshotStatusVO {
	partials := make([]ScanChannelFailureVO, 0, len(v.PartialFailures))
	for _, p := range v.PartialFailures {
		partials = append(partials, ScanChannelFailureVO{
			Cloud:   p.Cloud,
			Product: p.Product,
			Account: p.Account,
			Reason:  p.Reason,
		})
	}
	vo := DiscoverySnapshotStatusVO{HasSnapshot: v.HasSnapshot, PartialFailures: partials}
	if !v.HasSnapshot {
		return vo
	}
	vo.SnapshotID = v.SnapshotID
	vo.Status = string(v.Status)
	vo.StartedAt = formatTime(v.StartedAt)
	vo.FailReason = v.FailReason
	return vo
}

// toDiscoveryImportSessionVO 会话文档 → 同构响应 VO（items 空集为 [] 而非 null；
// finishedAt 仅终态有值）。
func toDiscoveryImportSessionVO(s domain.DiscoveryImportSession) DiscoveryImportSessionVO {
	items := make([]DiscoveryImportItemVO, 0, len(s.Items))
	for _, it := range s.Items {
		items = append(items, DiscoveryImportItemVO{
			Cloud:        it.Cloud,
			AccountKey:   it.AccountKey,
			CloudCertID:  it.CloudCertID,
			Result:       string(it.Result),
			MappedCertID: it.MappedCertID,
			ErrorReason:  it.ErrorReason,
		})
	}
	return DiscoveryImportSessionVO{
		SessionID:  s.ID.Hex(),
		Status:     string(s.Status),
		Items:      items,
		Progress:   DiscoveryImportProgressVO{Total: s.Progress.Total, Succeeded: s.Progress.Succeeded, Failed: s.Progress.Failed},
		CreatedAt:  formatTime(s.CreatedAt),
		FinishedAt: formatTimePtr(s.FinishedAt),
	}
}
