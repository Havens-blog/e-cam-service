package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/gin-gonic/gin"
)

// ReferenceHandler 引用关系端点（任务 3.6）：正向分组视图、反向查询、立即扫描。
type ReferenceHandler struct {
	svc service.ReferenceQueryService
}

// NewReferenceHandler 创建引用关系 handler。
func NewReferenceHandler(svc service.ReferenceQueryService) *ReferenceHandler {
	return &ReferenceHandler{svc: svc}
}

// RegisterRoutes 注册引用关系端点（角色门卫按 api-handbook Auth 列，7.2：
// 引用/反向/扫描均限运维工程师）：
//
//	GET  /api/v1/certs/reverse?domain=  反向查询（域名/资源→证书）
//	GET  /api/v1/certs/:id/references   正向引用（分组+覆盖率+盲区声明）
//	POST /api/v1/certs/:id/scan         立即扫描（防重 409 SCAN_IN_PROGRESS）
//
// 注意 Gin 通配顺序：/reverse 静态段先于 /:id 注册（与 ledger /stats 同理）。
func (h *ReferenceHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/reverse", RequireRoles(RoleOpsEngineer), h.ReverseQuery)
	g.GET("/:id/references", RequireRoles(RoleOpsEngineer), h.References)
	g.POST("/:id/scan", RequireRoles(RoleOpsEngineer), h.TriggerScan)
}

// ReferenceItemVO 正向视图单条引用（AC 白名单字段）。
type ReferenceItemVO struct {
	ResourceID            string `json:"resourceId"`
	ReferencedCloudCertID string `json:"referencedCloudCertId"`
	AccountKey            string `json:"accountKey,omitempty"`
	Namespace             string `json:"namespace,omitempty"` // K8s 引用
	Kind                  string `json:"kind,omitempty"`      // K8s 引用
}

// ReferenceGroupVO 按云/产品/集群分组的引用集合。
type ReferenceGroupVO struct {
	Cloud      string            `json:"cloud"`
	Product    string            `json:"product"`
	ClusterID  string            `json:"clusterId,omitempty"`
	References []ReferenceItemVO `json:"references"`
}

// CoverageVO 覆盖率元数据（各云各产品；total=-1 → 分母不可用标记，非 0%）。
type CoverageVO struct {
	Cloud                string `json:"cloud"`
	Product              string `json:"product"`
	Covered              int    `json:"covered"`
	Total                int    `json:"total"`
	DenominatorAvailable bool   `json:"denominatorAvailable"`
	DenominatorNote      string `json:"denominatorNote,omitempty"`
	Lagging              bool   `json:"lagging,omitempty"` // asset 盘点滞后（covered>total）
}

// ReferenceViewVO 正向引用视图响应（Hard Rule：coverageBoundary 不可省略）。
type ReferenceViewVO struct {
	CertID           string             `json:"certId"`
	Fingerprint      string             `json:"fingerprint"`
	ReferenceStatus  string             `json:"referenceStatus"`
	RefCount         int                `json:"refCount"`
	Reason           string             `json:"reason,omitempty"`
	LastScanAt       *string            `json:"lastScanAt"` // null=无成功快照（blind_spot 区分依据）
	SnapshotID       string             `json:"snapshotId,omitempty"`
	Groups           []ReferenceGroupVO `json:"groups"`
	Coverage         []CoverageVO       `json:"coverage"`
	CoverageBoundary string             `json:"coverageBoundary"`
}

// ReverseRefVO 反向查询单条引用。
type ReverseRefVO struct {
	Cloud                 string `json:"cloud,omitempty"`
	Product               string `json:"product,omitempty"`
	ClusterID             string `json:"clusterId,omitempty"`
	Namespace             string `json:"namespace,omitempty"`
	Kind                  string `json:"kind,omitempty"`
	ResourceID            string `json:"resourceId"`
	ReferencedCloudCertID string `json:"referencedCloudCertId"`
	AccountKey            string `json:"accountKey,omitempty"`
}

// ReverseEntryVO 反向查询单证书条目（按指纹严格区分，不做同域名合并）。
type ReverseEntryVO struct {
	Fingerprint    string         `json:"fingerprint"`
	CertID         string         `json:"certId,omitempty"` // 未登记为空
	Registered     bool           `json:"registered"`
	CommonName     string         `json:"commonName,omitempty"`
	Sans           []string       `json:"sans,omitempty"`
	HostingStatus  string         `json:"hostingStatus,omitempty"` // 未登记为空
	ReferenceCount int            `json:"referenceCount"`
	References     []ReverseRefVO `json:"references"`
}

// ReverseResultVO 反向查询响应（无匹配 → items 空数组，区别于错误）。
type ReverseResultVO struct {
	Query string           `json:"query"`
	Count int              `json:"count"`
	Items []ReverseEntryVO `json:"items"`
}

// ScanChannelFailureVO 扫描通道部分失败记录。
type ScanChannelFailureVO struct {
	Cloud   string `json:"cloud,omitempty"`
	Product string `json:"product"`
	Account string `json:"account,omitempty"`
	Reason  string `json:"reason"`
}

// ScanResultVO 立即扫描结果（异步触发返回 running 态 + snapshotId/startedAt；
// 空范围同步失败返回 failed 态；终态字段仅在同步路径或后台收敛后有意义）。
type ScanResultVO struct {
	SnapshotID        string                 `json:"snapshotId"`
	Status            string                 `json:"status"`
	FailReason        string                 `json:"failReason,omitempty"`
	ReferencesWritten int                    `json:"referencesWritten"`
	ChannelsAttempted int                    `json:"channelsAttempted"`
	ChannelsFailed    int                    `json:"channelsFailed"`
	PartialFailures   []ScanChannelFailureVO `json:"partialFailures"`
	Coverage          []CoverageVO           `json:"coverage"`
	StartedAt         string                 `json:"startedAt,omitempty"` // running 态：快照启动时点（前端轮询基线）
}

// scanInProgressMeta 409 SCAN_IN_PROGRESS 附进行中快照信息。
type scanInProgressMeta struct {
	SnapshotID string `json:"snapshotId"`
	StartedAt  string `json:"startedAt"`
}

// References GET /api/v1/certs/:id/references —— 正向引用视图。
func (h *ReferenceHandler) References(c *gin.Context) {
	v, err := h.svc.References(c.Request.Context(), c.Param("id"))
	if err != nil {
		WriteError(c, err)
		return
	}
	WriteOK(c, http.StatusOK, toReferenceViewVO(v), nil)
}

// ReverseQuery GET /api/v1/certs/reverse?domain= —— 反向查询。
// domain 为域名（SAN/CN 覆盖，通配符单标签）或资源名（resourceId 精确）。
func (h *ReferenceHandler) ReverseQuery(c *gin.Context) {
	q := strings.TrimSpace(c.Query("domain"))
	if q == "" {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest,
			"domain query parameter is required")
		return
	}
	items, err := h.svc.ReverseQuery(c.Request.Context(), q)
	if err != nil {
		WriteError(c, err)
		return
	}
	WriteOK(c, http.StatusOK, toReverseResultVO(q, items), nil)
}

// TriggerScan POST /api/v1/certs/:id/scan —— 立即扫描触发（异步：running 态 202
// + snapshotId/startedAt；防重 409 附进行中快照信息；空范围同步失败 200 failed）。
func (h *ReferenceHandler) TriggerScan(c *gin.Context) {
	res, err := h.svc.TriggerScan(c.Request.Context(), c.Param("id"))
	if err != nil {
		var inProgress *service.ScanInProgressError
		if errors.As(err, &inProgress) {
			WriteAPIErrorWithMeta(c, http.StatusConflict, domain.CodeScanInProgress, scanInProgressMeta{
				SnapshotID: inProgress.SnapshotID,
				StartedAt:  formatTime(inProgress.StartedAt),
			})
			return
		}
		WriteError(c, err)
		return
	}
	// 异步触发：running 快照已建、后台 goroutine 收敛，前端据 startedAt 轮询 /references。
	if res.Status == domain.ScanStatusRunning {
		WriteOK(c, http.StatusAccepted, toScanResultVO(res), nil)
		return
	}
	WriteOK(c, http.StatusOK, toScanResultVO(res), nil)
}

// ---------------------------------------------------------------------
// VO 转换
// ---------------------------------------------------------------------

// toReferenceViewVO 服务结果 → 正向视图 VO（groups/coverage 空集为 [] 而非 null）。
func toReferenceViewVO(v service.ReferenceView) ReferenceViewVO {
	groups := make([]ReferenceGroupVO, 0, len(v.Groups))
	for _, g := range v.Groups {
		items := make([]ReferenceItemVO, 0, len(g.References))
		for _, it := range g.References {
			items = append(items, ReferenceItemVO{
				ResourceID:            it.ResourceID,
				ReferencedCloudCertID: it.ReferencedCloudCertID,
				AccountKey:            it.AccountKey,
				Namespace:             it.Namespace,
				Kind:                  it.Kind,
			})
		}
		groups = append(groups, ReferenceGroupVO{
			Cloud:      g.Cloud,
			Product:    g.Product,
			ClusterID:  g.ClusterID,
			References: items,
		})
	}
	return ReferenceViewVO{
		CertID:           v.CertID,
		Fingerprint:      v.Fingerprint,
		ReferenceStatus:  string(v.ReferenceStatus),
		RefCount:         v.RefCount,
		Reason:           v.Reason,
		LastScanAt:       formatTimePtr(v.LastScanAt),
		SnapshotID:       v.SnapshotID,
		Groups:           groups,
		Coverage:         toCoverageVOs(v.Coverage),
		CoverageBoundary: v.CoverageBoundary,
	}
}

// toCoverageVOs coverageMeta → VO（total=-1 输出"分母不可用"标记）。
func toCoverageVOs(meta []domain.CoverageMeta) []CoverageVO {
	out := make([]CoverageVO, 0, len(meta))
	for _, m := range meta {
		vo := CoverageVO{
			Cloud:                m.Cloud,
			Product:              m.Product,
			Covered:              m.Covered,
			Total:                m.Total,
			DenominatorAvailable: m.Total >= 0,
			Lagging:              m.Lagging,
		}
		if m.Total < 0 {
			vo.DenominatorNote = service.DenominatorUnavailableNote
		}
		out = append(out, vo)
	}
	return out
}

// toReverseResultVO 服务结果 → 反向查询 VO（按指纹分组互不混淆）。
func toReverseResultVO(query string, entries []service.ReverseCertEntry) ReverseResultVO {
	items := make([]ReverseEntryVO, 0, len(entries))
	for _, e := range entries {
		refs := make([]ReverseRefVO, 0, len(e.References))
		for _, r := range e.References {
			refs = append(refs, ReverseRefVO{
				Cloud:                 r.Cloud,
				Product:               r.Product,
				ClusterID:             r.ClusterID,
				Namespace:             r.Namespace,
				Kind:                  r.Kind,
				ResourceID:            r.ResourceID,
				ReferencedCloudCertID: r.ReferencedCloudCertID,
				AccountKey:            r.AccountKey,
			})
		}
		items = append(items, ReverseEntryVO{
			Fingerprint:    e.Fingerprint,
			CertID:         e.CertID,
			Registered:     e.Registered,
			CommonName:     e.CommonName,
			Sans:           e.Sans,
			HostingStatus:  string(e.HostingStatus),
			ReferenceCount: e.ReferenceCount,
			References:     refs,
		})
	}
	return ReverseResultVO{Query: query, Count: len(items), Items: items}
}

// toScanResultVO 扫描结果 → VO。
func toScanResultVO(res service.ScanResult) ScanResultVO {
	partials := make([]ScanChannelFailureVO, 0, len(res.PartialFailures))
	for _, p := range res.PartialFailures {
		partials = append(partials, ScanChannelFailureVO{
			Cloud:   p.Cloud,
			Product: p.Product,
			Account: p.Account,
			Reason:  p.Reason,
		})
	}
	return ScanResultVO{
		SnapshotID:        res.SnapshotID,
		Status:            string(res.Status),
		FailReason:        res.FailReason,
		ReferencesWritten: res.ReferencesWritten,
		ChannelsAttempted: res.ChannelsAttempted,
		ChannelsFailed:    res.ChannelsFailed,
		PartialFailures:   partials,
		Coverage:          toCoverageVOs(res.CoverageMeta),
		StartedAt:         formatTime(res.StartedAt),
	}
}
