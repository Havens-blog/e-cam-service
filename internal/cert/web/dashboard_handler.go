package web

import (
	"net/http"

	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/gin-gonic/gin"
)

// DashboardHandler 到期看板端点（任务 4.5，全角色含只读）。
type DashboardHandler struct {
	svc service.DashboardService
}

// NewDashboardHandler 创建看板 handler。
func NewDashboardHandler(svc service.DashboardService) *DashboardHandler {
	return &DashboardHandler{svc: svc}
}

// RegisterRoutes 注册看板端点（api-handbook Auth：全角色含只读——4.5 既定
// 契约：不做角色门卫，认证由全局链承接；7.2 CertRoleMiddleware 为已认证
// 会话兜底写入 viewer 角色，未设置角色=未认证在上游 401）。只读查看者的
// 端点级差异化拦截在 GET /certs/:id（详情只读）与台账/变更/配置面。
// 注意 Gin 通配顺序：/dashboard 静态段先于 ledger /:id 注册（与 /stats、
// /reverse 同理）。
func (h *DashboardHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dashboard", h.Dashboard)
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
