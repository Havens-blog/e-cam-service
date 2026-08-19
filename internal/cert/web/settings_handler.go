package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/k8s"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
)

// SettingsHandler 全局配置端点（任务 4.5）：settings CRUD、exemptions 增删、
// crds 登记（接 3.4 service）、thresholds 界值校验（400）、test 测试告警。
// 鉴权：settings 面整体限运维主管/审计（RequireRoles；403 FORBIDDEN 否则）。
type SettingsHandler struct {
	settings service.SettingsService
	crds     service.CrdRegistrationService
}

// NewSettingsHandler 创建全局配置 handler。
func NewSettingsHandler(settings service.SettingsService, crds service.CrdRegistrationService) *SettingsHandler {
	return &SettingsHandler{settings: settings, crds: crds}
}

// RegisterRoutes 注册全局配置端点（组级角色门卫，dashboard 全角色不在此列）：
//
//	GET    /settings                    告警配置+阈值+豁免清单
//	PUT    /settings                    更新（thresholds 越界 400 整体拒绝）
//	POST   /settings/exemptions         添加豁免
//	DELETE /settings/exemptions/:domain 移除豁免
//	POST   /settings/test               发送测试告警
//	POST   /settings/crds               登记自定义 CRD（重复 409）
//	GET    /settings/crds               登记列表（含 enabled）
//	DELETE /settings/crds/:id           删除登记
//
// 注意 Gin 通配顺序：/settings 静态段先于 ledger /:id 注册。
func (h *SettingsHandler) RegisterRoutes(g *gin.RouterGroup) {
	s := g.Group("/settings", RequireRoles(RoleOpsSupervisor, RoleAuditor))
	s.GET("", h.GetSettings)
	s.PUT("", h.UpdateSettings)
	s.POST("/exemptions", h.AddExemption)
	s.DELETE("/exemptions/:domain", h.RemoveExemption)
	s.POST("/test", h.SendTestAlert)
	s.POST("/crds", h.RegisterCrd)
	s.GET("/crds", h.ListCrds)
	s.DELETE("/crds/:id", h.DeleteCrd)
}

// CodeCrdDuplicateRegistration CRD 登记重复（uk_cluster_group_kind 冲突 → 409）。
const CodeCrdDuplicateRegistration = "CRD_DUPLICATE_REGISTRATION"

// ---------------------------------------------------------------------
// settings CRUD
// ---------------------------------------------------------------------

// ThresholdsVO thresholds 载荷（界值注释见 domain.ValidateThresholds；
// GET/PUT 同构）。
type ThresholdsVO struct {
	ScanFreshnessHours          int   `json:"scanFreshnessHours"`          // 1~72
	VerifyWindowHours           int   `json:"verifyWindowHours"`           // 2~24
	RollbackProtectDays         int   `json:"rollbackProtectDays"`         // 7~14
	VerifyConfirmProbes         int   `json:"verifyConfirmProbes"`         // 1~10
	VerifyProbeIntervalMinutes  int   `json:"verifyProbeIntervalMinutes"`  // 5~60
	PauseTimeoutHours           int   `json:"pauseTimeoutHours"`           // 24~168
	RecheckDelayMinutes         int   `json:"recheckDelayMinutes"`         // 1~60
	ItemHeartbeatTimeoutMinutes int   `json:"itemHeartbeatTimeoutMinutes"` // 5~180
	ScanTimeoutHours            int   `json:"scanTimeoutHours"`            // 1~12
	ExpiryLevels                []int `json:"expiryLevels"`                // 1~5 项、各项 1~90、去重
}

// VerifyWindowRouteVO 验证窗口告警路由。
type VerifyWindowRouteVO struct {
	Enabled     bool     `json:"enabled"`
	WebhookURLs []string `json:"webhookUrls"`
	EmailGroup  []string `json:"emailGroup"`
}

// ExemptionVO 豁免条目。
type ExemptionVO struct {
	Domain    string `json:"domain"`
	Reason    string `json:"reason,omitempty"`
	Operator  string `json:"operator,omitempty"`
	CreatedAt string `json:"createdAt"`
}

// SettingsVO GET /settings 响应（Hard Rule：不含任何私钥/凭据字段）。
type SettingsVO struct {
	WebhookURLs            []string             `json:"webhookUrls"`
	EmailGroup             []string             `json:"emailGroup"`
	ChannelConfirmed       bool                 `json:"channelConfirmed"`
	VerifyWindowRoute      *VerifyWindowRouteVO `json:"verifyWindowRoute"`
	WildcardProbeOverrides map[string]string    `json:"wildcardProbeOverrides"`
	Thresholds             ThresholdsVO         `json:"thresholds"`
	Exemptions             []ExemptionVO        `json:"exemptions"`
}

// UpdateSettingsRequest PUT /settings 请求体（channelConfirmed 不在更新面：
// 仅由测试告警成功确认）。
type UpdateSettingsRequest struct {
	WebhookURLs            []string             `json:"webhookUrls"`
	EmailGroup             []string             `json:"emailGroup"`
	VerifyWindowRoute      *VerifyWindowRouteVO `json:"verifyWindowRoute"`
	WildcardProbeOverrides map[string]string    `json:"wildcardProbeOverrides"`
	Thresholds             *ThresholdsVO        `json:"thresholds"`
}

// GetSettings GET /api/v1/certs/settings —— 告警配置+阈值+豁免清单。
func (h *SettingsHandler) GetSettings(c *gin.Context) {
	view, err := h.settings.GetSettings(c.Request.Context())
	if err != nil {
		WriteError(c, err)
		return
	}
	WriteOK(c, http.StatusOK, toSettingsVO(view), nil)
}

// UpdateSettings PUT /api/v1/certs/settings —— thresholds 界值服务端校验
// （Hard Rule：越界 400 整体拒绝，不做部分写入；界值见 ThresholdsVO 注释）。
func (h *SettingsHandler) UpdateSettings(c *gin.Context) {
	var req UpdateSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest, "invalid request body")
		return
	}
	if req.Thresholds == nil {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest,
			"thresholds is required")
		return
	}
	view, err := h.settings.UpdateSettings(c.Request.Context(), service.UpdateSettingsInput{
		WebhookURLs:            req.WebhookURLs,
		EmailGroup:             req.EmailGroup,
		VerifyWindowRoute:      toDomainVerifyWindowRoute(req.VerifyWindowRoute),
		WildcardProbeOverrides: req.WildcardProbeOverrides,
		Thresholds:             toDomainThresholds(*req.Thresholds),
	})
	if err != nil {
		var invalid *domain.ThresholdsInvalidError
		if errors.As(err, &invalid) {
			WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest, invalid.Error())
			return
		}
		WriteError(c, err)
		return
	}
	WriteOK(c, http.StatusOK, toSettingsVO(view), nil)
}

// ---------------------------------------------------------------------
// exemptions
// ---------------------------------------------------------------------

// AddExemptionRequest 豁免添加请求体。
type AddExemptionRequest struct {
	Domain string `json:"domain"`
	Reason string `json:"reason"`
}

// AddExemption POST /api/v1/certs/settings/exemptions —— 添加豁免
// （operator 取认证上下文用户名，供 7.2 审计留痕）。
func (h *SettingsHandler) AddExemption(c *gin.Context) {
	var req AddExemptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Domain) == "" {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest, "domain is required")
		return
	}
	e, err := h.settings.AddExemption(c.Request.Context(), service.ExemptionInput{
		Domain:   req.Domain,
		Reason:   req.Reason,
		Operator: operator(c),
	})
	if err != nil {
		WriteError(c, err)
		return
	}
	WriteOK(c, http.StatusOK, toExemptionVO(e), nil)
}

// RemoveExemption DELETE /api/v1/certs/settings/exemptions/:domain —— 移除豁免。
func (h *SettingsHandler) RemoveExemption(c *gin.Context) {
	domainName := strings.TrimSpace(c.Param("domain"))
	if domainName == "" {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest, "domain is required")
		return
	}
	if err := h.settings.RemoveExemption(c.Request.Context(), domainName); err != nil {
		WriteError(c, err)
		return
	}
	WriteOK(c, http.StatusOK, gin.H{"domain": domainName, "removed": true}, nil)
}

// ---------------------------------------------------------------------
// crds（接 3.4 CrdRegistrationService，handler 仅做 HTTP 映射）
// ---------------------------------------------------------------------

// RegisterCrdRequest 自定义 CRD 登记请求体。
type RegisterCrdRequest struct {
	ClusterID     string `json:"clusterId"`
	APIGroup      string `json:"apiGroup"`
	Kind          string `json:"kind"`
	CertFieldPath string `json:"certFieldPath"`
}

// CrdRegistrationVO 登记视图（Builtin 标记内置固定枚举项）。
type CrdRegistrationVO struct {
	ID            string `json:"id"`
	ClusterID     string `json:"clusterId"`
	APIGroup      string `json:"apiGroup"`
	Kind          string `json:"kind"`
	CertFieldPath string `json:"certFieldPath"`
	Enabled       bool   `json:"enabled"`
	Builtin       bool   `json:"builtin"`
	Operator      string `json:"operator"`
	CreatedAt     string `json:"createdAt"`
}

// RegisterCrd POST /api/v1/certs/settings/crds —— 登记自定义 CRD
// （仅限 spec 含云托管证书引用字段的网关类资源；重复登记 409）。
func (h *SettingsHandler) RegisterCrd(c *gin.Context) {
	var req RegisterCrdRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest, "invalid request body")
		return
	}
	view, err := h.crds.Register(c.Request.Context(), service.RegisterCrdInput{
		ClusterID:     req.ClusterID,
		APIGroup:      req.APIGroup,
		Kind:          req.Kind,
		CertFieldPath: req.CertFieldPath,
		Operator:      operator(c),
	})
	if err != nil {
		writeCrdError(c, err)
		return
	}
	WriteOK(c, http.StatusOK, toCrdRegistrationVO(view), nil)
}

// ListCrds GET /api/v1/certs/settings/crds —— 登记列表（含 enabled 状态）。
func (h *SettingsHandler) ListCrds(c *gin.Context) {
	views, err := h.crds.List(c.Request.Context())
	if err != nil {
		WriteError(c, err)
		return
	}
	out := make([]CrdRegistrationVO, 0, len(views))
	for _, v := range views {
		out = append(out, toCrdRegistrationVO(v))
	}
	WriteOK(c, http.StatusOK, out, nil)
}

// DeleteCrd DELETE /api/v1/certs/settings/crds/:id —— 删除登记
// （该 CRD 回归扫描盲区并在视图声明；内置固定枚举项不可删除）。
func (h *SettingsHandler) DeleteCrd(c *gin.Context) {
	id := c.Param("id")
	if err := h.crds.Delete(c.Request.Context(), id); err != nil {
		writeCrdError(c, err)
		return
	}
	WriteOK(c, http.StatusOK, gin.H{"id": id, "deleted": true}, nil)
}

// writeCrdError CRD service 错误 → HTTP 映射：重复登记 409；非法 certFieldPath
// 与内置项删除 400；非法 ID 400 / 未命中 404 由 WriteError 兜底。
func writeCrdError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrDuplicateCrdRegistration):
		WriteAPIError(c, http.StatusConflict, CodeCrdDuplicateRegistration,
			"crd registration already exists for cluster/apiGroup/kind")
	case errors.Is(err, k8s.ErrInvalidCertFieldPath):
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest, err.Error())
	case errors.Is(err, domain.ErrBuiltinCrdRegistration):
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest,
			"builtin crd registration cannot be deleted (disable it instead)")
	case errors.Is(err, domain.ErrInvalidID):
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidID, "invalid document id")
	case errors.Is(err, mongo.ErrNoDocuments):
		WriteAPIError(c, http.StatusNotFound, CodeNotFound, "resource not found")
	default:
		WriteError(c, err)
	}
}

// ---------------------------------------------------------------------
// test 告警
// ---------------------------------------------------------------------

// TestAlertVO POST /settings/test 响应（成功/失败原因）。
type TestAlertVO struct {
	Sent   bool   `json:"sent"`
	Reason string `json:"reason,omitempty"`
}

// SendTestAlert POST /api/v1/certs/settings/test —— 经 4.3 通道发测试告警；
// 成功即确认渠道（channelConfirmed=true）。
func (h *SettingsHandler) SendTestAlert(c *gin.Context) {
	res, err := h.settings.SendTestAlert(c.Request.Context(), operator(c))
	if err != nil {
		WriteError(c, err)
		return
	}
	WriteOK(c, http.StatusOK, TestAlertVO{Sent: res.Sent, Reason: res.Reason}, nil)
}

// ---------------------------------------------------------------------
// VO 转换（白名单序列化）
// ---------------------------------------------------------------------

// toSettingsVO 服务视图 → 响应 VO。
func toSettingsVO(v service.SettingsView) SettingsVO {
	vo := SettingsVO{
		WebhookURLs:            v.WebhookURLs,
		EmailGroup:             v.EmailGroup,
		ChannelConfirmed:       v.ChannelConfirmed,
		WildcardProbeOverrides: v.WildcardProbeOverrides,
		Thresholds:             toThresholdsVO(v.Thresholds),
		Exemptions:             make([]ExemptionVO, 0, len(v.Exemptions)),
	}
	if v.VerifyWindowRoute != nil {
		vo.VerifyWindowRoute = &VerifyWindowRouteVO{
			Enabled:     v.VerifyWindowRoute.Enabled,
			WebhookURLs: v.VerifyWindowRoute.WebhookURLs,
			EmailGroup:  v.VerifyWindowRoute.EmailGroup,
		}
	}
	for _, e := range v.Exemptions {
		vo.Exemptions = append(vo.Exemptions, toExemptionVO(e))
	}
	return vo
}

// toExemptionVO 豁免文档 → VO（时间 RFC3339）。
func toExemptionVO(e domain.Exemption) ExemptionVO {
	return ExemptionVO{
		Domain:    e.Domain,
		Reason:    e.Reason,
		Operator:  e.Operator,
		CreatedAt: formatTime(e.CreatedAt),
	}
}

// toCrdRegistrationVO 登记视图 → VO。
func toCrdRegistrationVO(v service.CrdRegistrationView) CrdRegistrationVO {
	return CrdRegistrationVO{
		ID:            v.ID,
		ClusterID:     v.ClusterID,
		APIGroup:      v.APIGroup,
		Kind:          v.Kind,
		CertFieldPath: v.CertFieldPath,
		Enabled:       v.Enabled,
		Builtin:       v.Builtin,
		Operator:      v.Operator,
		CreatedAt:     formatTime(v.CreatedAt),
	}
}

// toThresholdsVO 领域阈值 → VO。
func toThresholdsVO(t domain.Thresholds) ThresholdsVO {
	return ThresholdsVO{
		ScanFreshnessHours:          t.ScanFreshnessHours,
		VerifyWindowHours:           t.VerifyWindowHours,
		RollbackProtectDays:         t.RollbackProtectDays,
		VerifyConfirmProbes:         t.VerifyConfirmProbes,
		VerifyProbeIntervalMinutes:  t.VerifyProbeIntervalMinutes,
		PauseTimeoutHours:           t.PauseTimeoutHours,
		RecheckDelayMinutes:         t.RecheckDelayMinutes,
		ItemHeartbeatTimeoutMinutes: t.ItemHeartbeatTimeoutMinutes,
		ScanTimeoutHours:            t.ScanTimeoutHours,
		ExpiryLevels:                t.ExpiryLevels,
	}
}

// toDomainThresholds VO → 领域阈值。
func toDomainThresholds(t ThresholdsVO) domain.Thresholds {
	return domain.Thresholds{
		ScanFreshnessHours:          t.ScanFreshnessHours,
		VerifyWindowHours:           t.VerifyWindowHours,
		RollbackProtectDays:         t.RollbackProtectDays,
		VerifyConfirmProbes:         t.VerifyConfirmProbes,
		VerifyProbeIntervalMinutes:  t.VerifyProbeIntervalMinutes,
		PauseTimeoutHours:           t.PauseTimeoutHours,
		RecheckDelayMinutes:         t.RecheckDelayMinutes,
		ItemHeartbeatTimeoutMinutes: t.ItemHeartbeatTimeoutMinutes,
		ScanTimeoutHours:            t.ScanTimeoutHours,
		ExpiryLevels:                t.ExpiryLevels,
	}
}

// toDomainVerifyWindowRoute VO → 领域验证窗口路由（nil 透传）。
func toDomainVerifyWindowRoute(v *VerifyWindowRouteVO) *domain.VerifyWindowRoute {
	if v == nil {
		return nil
	}
	return &domain.VerifyWindowRoute{
		Enabled:     v.Enabled,
		WebhookURLs: v.WebhookURLs,
		EmailGroup:  v.EmailGroup,
	}
}
