package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/gin-gonic/gin"
)

// LedgerHandler 台账查询/删除拦截/统计端点（任务 2.3）。
type LedgerHandler struct {
	svc service.LedgerService
}

// NewLedgerHandler 创建台账 handler。
func NewLedgerHandler(svc service.LedgerService) *LedgerHandler {
	return &LedgerHandler{svc: svc}
}

// RegisterRoutes 注册台账读取面端点（角色门卫按 api-handbook Auth 列，7.2）：
//
//	GET    /api/v1/certs        列表（运维工程师/主管）
//	GET    /api/v1/certs/stats  台账统计（运维工程师/主管）
//	GET    /api/v1/certs/:id    详情（工程师/主管+只读查看者——看板抽屉
//	                            「查看证书详情」链路，任务 AC"详情只读"口径）
//	DELETE /api/v1/certs/:id    删除（运维工程师；拦截 409 CERT_HAS_REFS）
func (h *LedgerHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("", RequireRoles(RoleOpsEngineer, RoleOpsSupervisor), h.ListCerts)
	g.GET("/stats", RequireRoles(RoleOpsEngineer, RoleOpsSupervisor), h.Stats)
	g.GET("/:id", RequireRoles(RoleOpsEngineer, RoleOpsSupervisor, RoleViewer), h.GetCert)
	g.DELETE("/:id", RequireRoles(RoleOpsEngineer), h.DeleteCert)
}

// CertListItemVO 列表项（AC 白名单字段；Hard Rule：不含任何私钥字段，
// encryptedPrivateKey 密文/明文与 certPem 均不得出现）。时间字段 RFC3339。
type CertListItemVO struct {
	ID            string   `json:"id"`
	Fingerprint   string   `json:"fingerprint"`
	CommonName    string   `json:"commonName"`
	Sans          []string `json:"sans"`
	Issuer        string   `json:"issuer"`
	NotAfter      string   `json:"notAfter"`
	DaysLeft      int      `json:"daysLeft"`
	HostingStatus string   `json:"hostingStatus"`
	MaterialIssue string   `json:"materialIssue,omitempty"` // 盘点容忍标记：expired/chain_incomplete（缺省=正常）
	ProtectUntil  *string  `json:"protectUntil"`
	RefCount      int      `json:"refCount"`
}

// listCertsVO 列表 data 载荷（前端 ListCertsResponse 契约：分页信息随载荷返回，
// unwrapCertEnvelope 成功路径只取 data）。
type listCertsVO struct {
	Items    []CertListItemVO `json:"items"`
	Total    int64            `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"pageSize"`
}

// CertDetailVO 详情（全要素；encryptedPrivateKey 以 hasKey 布尔呈现"已加密托管"语义）。
type CertDetailVO struct {
	ID               string   `json:"id"`
	Fingerprint      string   `json:"fingerprint"`
	CommonName       string   `json:"commonName"`
	Sans             []string `json:"sans"`
	Issuer           string   `json:"issuer"`
	SerialNumber     string   `json:"serialNumber"`
	NotBefore        string   `json:"notBefore"`
	NotAfter         string   `json:"notAfter"`
	DaysLeft         int      `json:"daysLeft"`
	KeyAlgorithm     string   `json:"keyAlgorithm"`
	HostingStatus    string   `json:"hostingStatus"`
	MaterialIssue    string   `json:"materialIssue,omitempty"` // 盘点容忍标记：expired/chain_incomplete（缺省=正常）
	HasKey           bool     `json:"hasKey"`
	ExpectedDomain   string   `json:"expectedDomain,omitempty"`
	ProtectUntil     *string  `json:"protectUntil"`
	ExpiryAlertLevel string   `json:"expiryAlertLevel"`
	CreatedAt        string   `json:"createdAt"`
	RefCount         int      `json:"refCount"`
	ReferenceStatus  string   `json:"referenceStatus"`
}

// StatsVO GET /stats 响应（api-handbook 台账统计字段，查询时实时聚合）。
type StatsVO struct {
	Total                int                  `json:"total"`
	Complete             int                  `json:"complete"`
	FingerprintOnly      int                  `json:"fingerprintOnly"`
	MissingRegistrations int                  `json:"missingRegistrations"`
	RegistrationRate     float64              `json:"registrationRate"`
	ReplaceableRate      float64              `json:"replaceableRate"`
	FingerprintOnlyRate  float64              `json:"fingerprintOnlyRate"`
	Denominator          int                  `json:"denominator"`
	DenominatorSources   DenominatorSourcesVO `json:"denominatorSources"`
}

// DenominatorSourcesVO 分母构成明细。
type DenominatorSourcesVO struct {
	ScannedUniqueFingerprints int `json:"scannedUniqueFingerprints"`
	ManualOnlyFingerprints    int `json:"manualOnlyFingerprints"`
}

// pageMeta 列表分页元数据（响应 meta 字段）。
type pageMeta struct {
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

// deleteResultVO 删除成功响应。
type deleteResultVO struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

// deleteBlockedMeta 删除拦截结构化原因（盲区原因/引用计数/保护期截止时间）。
type deleteBlockedMeta struct {
	ReferenceStatus string  `json:"referenceStatus"`
	RefCount        int     `json:"refCount"`
	Reason          string  `json:"reason"`
	ProtectUntil    *string `json:"protectUntil,omitempty"`
}

// ListCerts GET /api/v1/certs —— 服务端分页（默认每页 20）+ 筛选。
//
// Query 参数：
//
//	page          页码（1 起，缺省 1）
//	pageSize      每页条数（缺省 20，上限 100）
//	hostingStatus complete | fingerprint_only
//	daysLeft      gt30 | le30 | le14 | le7 | expired（与前端筛选器分档对齐）
//	search        域名/SAN/指纹片段子串（不区分大小写）
func (h *LedgerHandler) ListCerts(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("pageSize"))

	status := domain.HostingStatus(strings.TrimSpace(c.Query("hostingStatus")))
	if status != "" && status != domain.HostingStatusComplete && status != domain.HostingStatusFingerprintOnly {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest,
			"hostingStatus must be complete or fingerprint_only")
		return
	}
	tier, ok := service.ParseDaysLeftTier(strings.TrimSpace(c.Query("daysLeft")))
	if !ok {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest,
			"daysLeft must be one of gt30, le30, le14, le7, expired")
		return
	}

	res, err := h.svc.ListCerts(c.Request.Context(), service.ListCertsQuery{
		Page:          page,
		PageSize:      pageSize,
		HostingStatus: status,
		DaysLeft:      tier,
		Search:        strings.TrimSpace(c.Query("search")),
	})
	if err != nil {
		WriteError(c, err)
		return
	}
	// data 载荷携带 {items,total,page,pageSize}（前端 unwrapCertEnvelope 只取 data，
	// 分页信息随载荷返回；meta 同步保留供通用客户端）。
	WriteOK(c, http.StatusOK, listCertsVO{
		Items: toListItemVOs(res.Items), Total: res.Total, Page: res.Page, PageSize: res.PageSize,
	}, pageMeta{
		Total: res.Total, Page: res.Page, PageSize: res.PageSize,
	})
}

// GetCert GET /api/v1/certs/:id —— 全要素详情（不含私钥，hasKey 语义）。
func (h *LedgerHandler) GetCert(c *gin.Context) {
	d, err := h.svc.GetCert(c.Request.Context(), c.Param("id"))
	if err != nil {
		WriteError(c, err)
		return
	}
	WriteOK(c, http.StatusOK, toDetailVO(d), nil)
}

// DeleteCert DELETE /api/v1/certs/:id —— 删除拦截（409 CERT_HAS_REFS 附结构化原因）。
func (h *LedgerHandler) DeleteCert(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.DeleteCert(c.Request.Context(), id); err != nil {
		var blocked *domain.DeleteBlockedError
		if errors.As(err, &blocked) {
			WriteAPIErrorWithMeta(c, http.StatusConflict, domain.CodeCertHasRefs, toBlockedMeta(blocked))
			return
		}
		WriteError(c, err)
		return
	}
	WriteOK(c, http.StatusOK, deleteResultVO{ID: id, Deleted: true}, nil)
}

// Stats GET /api/v1/certs/stats —— 双口径覆盖率（实时聚合，无存储快照）。
func (h *LedgerHandler) Stats(c *gin.Context) {
	st, err := h.svc.Stats(c.Request.Context())
	if err != nil {
		WriteError(c, err)
		return
	}
	WriteOK(c, http.StatusOK, StatsVO{
		Total:                st.Total,
		Complete:             st.Complete,
		FingerprintOnly:      st.FingerprintOnly,
		MissingRegistrations: st.MissingRegistrations,
		RegistrationRate:     st.RegistrationRate,
		ReplaceableRate:      st.ReplaceableRate,
		FingerprintOnlyRate:  st.FingerprintOnlyRate,
		Denominator:          st.Denominator,
		DenominatorSources: DenominatorSourcesVO{
			ScannedUniqueFingerprints: st.DenominatorSources.ScannedUniqueFingerprints,
			ManualOnlyFingerprints:    st.DenominatorSources.ManualOnlyFingerprints,
		},
	}, nil)
}

// toListItemVOs 服务结果 → 列表 VO（白名单序列化，空页为 [] 而非 null）。
func toListItemVOs(items []service.CertListItem) []CertListItemVO {
	vos := make([]CertListItemVO, 0, len(items))
	for _, it := range items {
		vos = append(vos, CertListItemVO{
			ID:            it.ID,
			Fingerprint:   it.Fingerprint,
			CommonName:    it.CommonName,
			Sans:          it.Sans,
			Issuer:        it.Issuer,
			NotAfter:      formatTime(it.NotAfter),
			DaysLeft:      it.DaysLeft,
			HostingStatus: string(it.HostingStatus),
			MaterialIssue: string(it.MaterialIssue),
			ProtectUntil:  formatTimePtr(it.ProtectUntil),
			RefCount:      it.RefCount,
		})
	}
	return vos
}

// toDetailVO 服务结果 → 详情 VO（白名单序列化，仅 hasKey 布尔承载托管语义）。
func toDetailVO(d service.CertDetail) CertDetailVO {
	return CertDetailVO{
		ID:               d.ID,
		Fingerprint:      d.Fingerprint,
		CommonName:       d.CommonName,
		Sans:             d.Sans,
		Issuer:           d.Issuer,
		SerialNumber:     d.SerialNumber,
		NotBefore:        formatTime(d.NotBefore),
		NotAfter:         formatTime(d.NotAfter),
		DaysLeft:         d.DaysLeft,
		KeyAlgorithm:     string(d.KeyAlgorithm),
		HostingStatus:    string(d.HostingStatus),
		MaterialIssue:    string(d.MaterialIssue),
		HasKey:           d.HasKey,
		ExpectedDomain:   d.ExpectedDomain,
		ProtectUntil:     formatTimePtr(d.ProtectUntil),
		ExpiryAlertLevel: string(d.ExpiryAlertLevel),
		CreatedAt:        formatTime(d.CreatedAt),
		RefCount:         d.RefCount,
		ReferenceStatus:  string(d.ReferenceStatus),
	}
}

// toBlockedMeta 拦截错误 → 结构化 meta（附盲区原因/保护期截止时间）。
func toBlockedMeta(b *domain.DeleteBlockedError) deleteBlockedMeta {
	m := deleteBlockedMeta{
		ReferenceStatus: string(b.ReferenceStatus),
		RefCount:        b.RefCount,
		Reason:          b.Reason,
	}
	if b.ProtectUntil != nil {
		s := b.ProtectUntil.UTC().Format(time.RFC3339)
		m.ProtectUntil = &s
	}
	return m
}

// formatTime 时间 → RFC3339 字符串；零值输出空串。
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// formatTimePtr 可空时间 → RFC3339 指针（nil 序列化为 null）。
func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}
