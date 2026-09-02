package web

import (
	"errors"
	"net/http"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/gin-gonic/gin"
)

// DashboardHandler 到期看板端点（任务 4.5，全角色含只读）+ 探测结果列表/触发。
type DashboardHandler struct {
	svc   service.DashboardService
	probe service.ProbeService // 立即探测触发（DNS 源 ProbeAllTenantDNS）
}

// NewDashboardHandler 创建看板 handler。probe 可空（未装配时 /probes/scan 返回 503）。
func NewDashboardHandler(svc service.DashboardService, probe service.ProbeService) *DashboardHandler {
	return &DashboardHandler{svc: svc, probe: probe}
}

// RegisterRoutes 注册看板端点（api-handbook Auth：全角色含只读——4.5 既定
// 契约：不做角色门卫，认证由全局链承接；7.2 CertRoleMiddleware 为已认证
// 会话兜底写入 viewer 角色，未设置角色=未认证在上游 401）。只读查看者的
// 端点级差异化拦截在 GET /certs/:id（详情只读）与台账/变更/配置面。
// 注意 Gin 通配顺序：/dashboard 静态段先于 ledger /:id 注册（与 /stats、
// /reverse 同理）。
func (h *DashboardHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dashboard", h.Dashboard)
	g.GET("/probes", h.ListProbes)
	g.POST("/probes/scan", h.TriggerProbe)
}

// DashboardSummaryVO 看板汇总（api-handbook 到期看板契约字段）。
type DashboardSummaryVO struct {
	// CountsByLevel 5 个互斥分桶计数，数组序与 UI 总览卡一致：
	// [0]=>30 天、[1]=>30 天、[2]=>14 天、[3]=>7 天、[4]=>已过期。
	CountsByLevel        []int   `json:"countsByLevel"`
	DiffAlertCount       int     `json:"diffAlertCount"`
	ExemptCount          int     `json:"exemptCount"`
	WildcardSkippedCount int     `json:"wildcardSkippedCount"`
	RegistrationRate     float64 `json:"registrationRate"`
	ReplaceableRate      float64 `json:"replaceableRate"`
	FingerprintOnlyRate  float64 `json:"fingerprintOnlyRate"`
}

// DashboardItemVO 看板子域名行（Hard Rule：不含任何私钥/凭证字段）。
// certId/fingerprint/lastProbeAt/onlineFingerprint 为探测详情抽屉字段
// （任务 6.4 增量：抽屉线上/台账指纹比对与「查看证书详情」链接；未探测时
// lastProbeAt=null、onlineFingerprint 空串）。
type DashboardItemVO struct {
	Domain            string   `json:"domain"`
	DaysLeft          int      `json:"daysLeft"`
	Level             string   `json:"level"`             // gt30|le30|le14|le7|expired（互斥桶，同台账筛选分档）
	HostingType       string   `json:"hostingType"`       // complete|fingerprint_only
	ProbeStatus       string   `json:"probeStatus"`       // 6 值枚举；空串=尚未探测
	ReferencedClouds  []string `json:"referencedClouds"`  // 所属云去重集合（K8s 记 "k8s"）
	CertID            string   `json:"certId"`            // 归属证书 ID（抽屉跳转 /certs/:id）
	Fingerprint       string   `json:"fingerprint"`       // 归属证书台账指纹
	LastProbeAt       *string  `json:"lastProbeAt"`       // 最近探测时点；未探测 null
	OnlineFingerprint string   `json:"onlineFingerprint"` // 线上指纹；不可达/跳过等无值为空串
}

// DashboardVO GET /dashboard 响应。
type DashboardVO struct {
	Summary          DashboardSummaryVO `json:"summary"`
	Items            []DashboardItemVO  `json:"items"`
	LastInspectionAt *string            `json:"lastInspectionAt"` // 4.4 巡检记录；未接线为 null
}

// Dashboard GET /api/v1/certs/dashboard —— 全角色（含只读）。
func (h *DashboardHandler) Dashboard(c *gin.Context) {
	view, err := h.svc.Dashboard(c.Request.Context())
	if err != nil {
		WriteError(c, err)
		return
	}
	counts := append([]int(nil), view.Summary.CountsByLevel[:]...)
	items := make([]DashboardItemVO, 0, len(view.Items))
	for _, it := range view.Items {
		items = append(items, DashboardItemVO{
			Domain:            it.Domain,
			DaysLeft:          it.DaysLeft,
			Level:             string(it.Level),
			HostingType:       string(it.HostingType),
			ProbeStatus:       string(it.ProbeStatus),
			ReferencedClouds:  it.ReferencedClouds,
			CertID:            it.CertID,
			Fingerprint:       it.Fingerprint,
			LastProbeAt:       formatTimePtr(it.LastProbeAt),
			OnlineFingerprint: it.OnlineFingerprint,
		})
	}
	WriteOK(c, http.StatusOK, DashboardVO{
		Summary: DashboardSummaryVO{
			CountsByLevel:        counts,
			DiffAlertCount:       view.Summary.DiffAlertCount,
			ExemptCount:          view.Summary.ExemptCount,
			WildcardSkippedCount: view.Summary.WildcardSkippedCount,
			RegistrationRate:     view.Summary.RegistrationRate,
			ReplaceableRate:      view.Summary.ReplaceableRate,
			FingerprintOnlyRate:  view.Summary.FingerprintOnlyRate,
		},
		Items:            items,
		LastInspectionAt: formatTimePtr(view.LastInspectionAt),
	}, nil)
}

// ProbeResultVO 探测结果行（含 DNS 源探测的子域名行：tenantId/linkedResource）。
type ProbeResultVO struct {
	Domain            string  `json:"domain"`
	Status            string  `json:"status"`
	OnlineFingerprint string  `json:"onlineFingerprint,omitempty"`
	OnlineNotAfter    *string `json:"onlineNotAfter,omitempty"`
	ProbeAt           string  `json:"probeAt"`
	TenantID          int64   `json:"tenantId,omitempty"`
	LinkedResource    string  `json:"linkedResource,omitempty"` // cdn/waf/external（DNS 源探测链路分层）
	RecordType        string  `json:"recordType,omitempty"`     // A/AAAA/CNAME（DNS 源探测；SAN 探测缺省）
	RecordValue       string  `json:"recordValue,omitempty"`    // 解析地址（记录值：IP / CNAME 目标）
	TLSVersion        string  `json:"tlsVersion,omitempty"`     // 协商 TLS 版本（unreachable 缺省）
}

// ListProbes GET /api/v1/certs/probes —— 每域最近一次探测结果（全角色含只读）。
// 含 DNS 源探测的子域名行，供子域名探测结果列表视图消费。
func (h *DashboardHandler) ListProbes(c *gin.Context) {
	results, err := h.svc.ListProbeResults(c.Request.Context())
	if err != nil {
		WriteError(c, err)
		return
	}
	items := make([]ProbeResultVO, 0, len(results))
	for _, r := range results {
		items = append(items, ProbeResultVO{
			Domain:            r.Domain,
			Status:            string(r.Status),
			OnlineFingerprint: r.OnlineFingerprint,
			OnlineNotAfter:    formatTimePtr(r.OnlineNotAfter),
			ProbeAt:           formatTime(r.ProbeAt),
			TenantID:          r.TenantID,
			LinkedResource:    r.LinkedResource,
			RecordType:        r.RecordType,
			RecordValue:       r.RecordValue,
			TLSVersion:        r.TLSVersion,
		})
	}
	WriteOK(c, http.StatusOK, items, nil)
}

// TriggerProbe POST /api/v1/certs/probes/scan —— 触发一轮 DNS 源探测（异步，
// 后台 goroutine）。可选 JSON body {"rootDomain":"x.com"}：仅拨测该根域下目标
// （定向刷新）；缺省/空 body = 全量。202 + 已触发；409 已在跑；404 根域无目标；
// 503 dnsSource 未装配。
func (h *DashboardHandler) TriggerProbe(c *gin.Context) {
	if h.probe == nil {
		WriteAPIError(c, http.StatusServiceUnavailable, CodeInternalError, "探测服务未装配（DNS 源未注入）")
		return
	}
	var req struct {
		RootDomain string `json:"rootDomain"`
	}
	_ = c.ShouldBindJSON(&req) // 空 body/非 JSON = 全量触发（不拦截）
	var err error
	if req.RootDomain == "" {
		err = h.probe.TriggerProbeAsync(c.Request.Context())
	} else {
		err = h.probe.TriggerProbeRootAsync(c.Request.Context(), req.RootDomain)
	}
	if err != nil {
		switch {
		case errors.Is(err, service.ErrProbeRunning):
			WriteAPIError(c, http.StatusConflict, domain.CodeScanInProgress, "探测正在进行中")
		case errors.Is(err, service.ErrNoDNSSource):
			WriteAPIError(c, http.StatusServiceUnavailable, CodeInternalError, "DNS 记录源未装配，请先完成 DNS 同步")
		case errors.Is(err, service.ErrNoProbeTargets):
			WriteAPIError(c, http.StatusNotFound, domain.CodeProbeNoTargets, "该根域名下无可拨测目标（DNS 记录未同步或域名不存在）")
		default:
			WriteError(c, err)
		}
		return
	}
	WriteOK(c, http.StatusAccepted, map[string]any{"triggered": true}, nil)
}
