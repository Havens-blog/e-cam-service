package web

import (
	"net/http"

	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/gin-gonic/gin"
)

// DiscoveryNotAfterPending 未登记条目 notAfter 占位显示（SC-2：inLedger 条目
// 为台账值，未登记条目占位显示——cert_references 无 notAfter 字段且本功能
// 不改其表结构）。
const DiscoveryNotAfterPending = "—（导入后补全）"

// DiscoveryHandler 云端发现导入只读端点（cert-cloud-discovery-import 任务 3）：
// 发现预览 + 快照状态查询（供无快照引导轮询）。
type DiscoveryHandler struct {
	svc service.DiscoveryPreviewService
}

// NewDiscoveryHandler 创建发现导入查询 handler。
func NewDiscoveryHandler(svc service.DiscoveryPreviewService) *DiscoveryHandler {
	return &DiscoveryHandler{svc: svc}
}

// RegisterRoutes 注册发现导入查询端点（导入类端点沿用 RoleOpsEngineer，
// 权限矩阵同 /reverse、/:id/scan）：
//
//	GET /api/v1/certs/discovery/preview          发现预览（纯 DB 聚合）
//	GET /api/v1/certs/discovery/snapshot-status  最近快照状态（引导轮询）
//
// 注意 Gin 通配顺序：/discovery 静态段先于 ledger /:id 注册（与 /reverse 同理）。
func (h *DiscoveryHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/discovery/preview", RequireRoles(RoleOpsEngineer), h.Preview)
	g.GET("/discovery/snapshot-status", RequireRoles(RoleOpsEngineer), h.SnapshotStatus)
}

// DiscoveryPreviewEntryVO 预览唯一证书条目（AC 七类字段：cloud/accountKey/
// cloudCertId/refCount/inLedger/notAfter/parseable）。
type DiscoveryPreviewEntryVO struct {
	Cloud       string `json:"cloud"`
	AccountKey  string `json:"accountKey"`
	CloudCertID string `json:"cloudCertId"`
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
