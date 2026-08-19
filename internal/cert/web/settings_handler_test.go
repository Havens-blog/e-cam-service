package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// settingsVO settings 响应解码目标。
type settingsVO struct {
	WebhookURLs            []string          `json:"webhookUrls"`
	EmailGroup             []string          `json:"emailGroup"`
	ChannelConfirmed       bool              `json:"channelConfirmed"`
	VerifyWindowRoute      *verifyRouteVO    `json:"verifyWindowRoute"`
	WildcardProbeOverrides map[string]string `json:"wildcardProbeOverrides"`
	Thresholds             thresholdsVO      `json:"thresholds"`
	Exemptions             []struct {
		Domain   string `json:"domain"`
		Reason   string `json:"reason"`
		Operator string `json:"operator"`
	} `json:"exemptions"`
}

type verifyRouteVO struct {
	Enabled     bool     `json:"enabled"`
	WebhookURLs []string `json:"webhookUrls"`
	EmailGroup  []string `json:"emailGroup"`
}

type thresholdsVO struct {
	ScanFreshnessHours          int   `json:"scanFreshnessHours"`
	VerifyWindowHours           int   `json:"verifyWindowHours"`
	RollbackProtectDays         int   `json:"rollbackProtectDays"`
	VerifyConfirmProbes         int   `json:"verifyConfirmProbes"`
	VerifyProbeIntervalMinutes  int   `json:"verifyProbeIntervalMinutes"`
	PauseTimeoutHours           int   `json:"pauseTimeoutHours"`
	RecheckDelayMinutes         int   `json:"recheckDelayMinutes"`
	ItemHeartbeatTimeoutMinutes int   `json:"itemHeartbeatTimeoutMinutes"`
	ScanTimeoutHours            int   `json:"scanTimeoutHours"`
	ExpiryLevels                []int `json:"expiryLevels"`
}

// putThresholdsBody 构造合法 PUT 载荷（mutate 可覆盖单个阈值字段）。
func putThresholdsBody() map[string]any {
	return map[string]any{
		"webhookUrls":            []string{"https://hooks.example.com/cert"},
		"emailGroup":             []string{"ops@example.com"},
		"verifyWindowRoute":      map[string]any{"enabled": true, "webhookUrls": []string{"https://hooks.example.com/verify"}, "emailGroup": []string{"change@example.com"}},
		"wildcardProbeOverrides": map[string]string{"*.wild.example.com": "probe.wild.example.com"},
		"thresholds": map[string]any{
			"scanFreshnessHours":          24,
			"verifyWindowHours":           24,
			"rollbackProtectDays":         7,
			"verifyConfirmProbes":         2,
			"verifyProbeIntervalMinutes":  10,
			"pauseTimeoutHours":           72,
			"recheckDelayMinutes":         5,
			"itemHeartbeatTimeoutMinutes": 30,
			"scanTimeoutHours":            2,
			"expiryLevels":                []int{30, 14, 7},
		},
	}
}

// decodeSettings 解码 settings 响应 data。
func decodeSettings(t *testing.T, w *httptest.ResponseRecorder) settingsVO {
	t.Helper()
	var env envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	var vo settingsVO
	require.NoError(t, json.Unmarshal(env.Data, &vo))
	return vo
}

// ---------------------------------------------------------------------
// settings GET / PUT（AC：读写五字段组 + 越界 400 整体拒绝）
// ---------------------------------------------------------------------

// TestSettings_GetDefaults 文档不存在时返回 DEFAULT 填充视图（exemptions=[]）。
func TestSettings_GetDefaults(t *testing.T) {
	engine, _ := newDashSettingsRouter(t, RoleOpsSupervisor)
	w := doJSON(t, engine, http.MethodGet, "/api/v1/certs/settings", nil)
	require.Equal(t, http.StatusOK, w.Code)

	vo := decodeSettings(t, w)
	assert.Equal(t, []string{}, vo.WebhookURLs)
	assert.False(t, vo.ChannelConfirmed)
	assert.Nil(t, vo.VerifyWindowRoute)
	assert.Equal(t, []int{30, 14, 7}, vo.Thresholds.ExpiryLevels)
	assert.Equal(t, 24, vo.Thresholds.ScanFreshnessHours)
	assert.Empty(t, vo.Exemptions)
}

// TestSettings_PutRoundtrip 五字段组全量读写回显。
func TestSettings_PutRoundtrip(t *testing.T) {
	engine, _ := newDashSettingsRouter(t, RoleOpsSupervisor)

	w := doJSON(t, engine, http.MethodPut, "/api/v1/certs/settings", putThresholdsBody())
	require.Equal(t, http.StatusOK, w.Code)
	vo := decodeSettings(t, w)

	assert.Equal(t, []string{"https://hooks.example.com/cert"}, vo.WebhookURLs)
	assert.Equal(t, []string{"ops@example.com"}, vo.EmailGroup)
	require.NotNil(t, vo.VerifyWindowRoute)
	assert.True(t, vo.VerifyWindowRoute.Enabled)
	assert.Equal(t, map[string]string{"*.wild.example.com": "probe.wild.example.com"}, vo.WildcardProbeOverrides)
	assert.Equal(t, []int{30, 14, 7}, vo.Thresholds.ExpiryLevels)
	assert.Equal(t, 10, vo.Thresholds.VerifyProbeIntervalMinutes)

	// GET 回读一致
	w = doJSON(t, engine, http.MethodGet, "/api/v1/certs/settings", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, vo, decodeSettings(t, w))
}

// TestSettings_PutThresholdsBounds 各数值字段边界值：min-1/max+1 → 400，
// min/max 本值 → 200（界值与 schema.sql 1:1）。
func TestSettings_PutThresholdsBounds(t *testing.T) {
	bounds := []struct {
		field    string
		min, max int
	}{
		{"scanFreshnessHours", 1, 72},
		{"verifyWindowHours", 2, 24},
		{"rollbackProtectDays", 7, 14},
		{"verifyConfirmProbes", 1, 10},
		{"verifyProbeIntervalMinutes", 5, 60},
		{"pauseTimeoutHours", 24, 168},
		{"recheckDelayMinutes", 1, 60},
		{"itemHeartbeatTimeoutMinutes", 5, 180},
		{"scanTimeoutHours", 1, 12},
	}
	for _, b := range bounds {
		t.Run(b.field, func(t *testing.T) {
			for _, tc := range []struct {
				name  string
				value int
				code  int
			}{
				{"below", b.min - 1, http.StatusBadRequest},
				{"min", b.min, http.StatusOK},
				{"max", b.max, http.StatusOK},
				{"above", b.max + 1, http.StatusBadRequest},
			} {
				t.Run(tc.name, func(t *testing.T) {
					engine, _ := newDashSettingsRouter(t, RoleOpsSupervisor)
					body := putThresholdsBody()
					thr := body["thresholds"].(map[string]any)
					thr[b.field] = tc.value
					w := doJSON(t, engine, http.MethodPut, "/api/v1/certs/settings", body)
					require.Equal(t, tc.code, w.Code)
					if tc.code == http.StatusBadRequest {
						var env envelope
						require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
						require.False(t, env.Success)
						assert.Equal(t, CodeInvalidRequest, env.Error.Code)
						assert.Contains(t, env.Error.Message, b.field)
					}
				})
			}
		})
	}
}

// TestSettings_PutExpiryLevelsBounds expiryLevels：空/超项/越界值/重复 → 400；
// 1 项与 5 项合法 → 200。
func TestSettings_PutExpiryLevelsBounds(t *testing.T) {
	cases := []struct {
		name   string
		levels []int
		code   int
	}{
		{"empty", []int{}, http.StatusBadRequest},
		{"six entries", []int{90, 80, 70, 60, 50, 40}, http.StatusBadRequest},
		{"value zero", []int{30, 0}, http.StatusBadRequest},
		{"value ninety-one", []int{91}, http.StatusBadRequest},
		{"duplicated", []int{30, 30}, http.StatusBadRequest},
		{"single", []int{45}, http.StatusOK},
		{"five entries", []int{90, 60, 30, 14, 7}, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine, _ := newDashSettingsRouter(t, RoleOpsSupervisor)
			body := putThresholdsBody()
			body["thresholds"].(map[string]any)["expiryLevels"] = tc.levels
			w := doJSON(t, engine, http.MethodPut, "/api/v1/certs/settings", body)
			assert.Equal(t, tc.code, w.Code)
		})
	}
}

// TestSettings_PutInvalidRejectedEntirely Hard Rule：越界 PUT 整体拒绝，
// 不做部分写入——webhookUrls 等其余字段不落库。
func TestSettings_PutInvalidRejectedEntirely(t *testing.T) {
	engine, _ := newDashSettingsRouter(t, RoleOpsSupervisor)

	body := putThresholdsBody()
	body["thresholds"].(map[string]any)["verifyWindowHours"] = 25 // 越界
	w := doJSON(t, engine, http.MethodPut, "/api/v1/certs/settings", body)
	require.Equal(t, http.StatusBadRequest, w.Code)

	// 回读：仍为 DEFAULT（无部分写入）
	w = doJSON(t, engine, http.MethodGet, "/api/v1/certs/settings", nil)
	require.Equal(t, http.StatusOK, w.Code)
	vo := decodeSettings(t, w)
	assert.Empty(t, vo.WebhookURLs)
	assert.Equal(t, 24, vo.Thresholds.VerifyWindowHours)
}

// TestSettings_PutMissingThresholds 缺 thresholds 字段 → 400。
func TestSettings_PutMissingThresholds(t *testing.T) {
	engine, _ := newDashSettingsRouter(t, RoleOpsSupervisor)
	body := putThresholdsBody()
	delete(body, "thresholds")
	w := doJSON(t, engine, http.MethodPut, "/api/v1/certs/settings", body)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------
// exemptions（AC：增删 + GET 随附清单）
// ---------------------------------------------------------------------

// TestExemptions_AddRemove 添加→GET 随附→移除→GET 为空。
func TestExemptions_AddRemove(t *testing.T) {
	engine, _ := newDashSettingsRouter(t, RoleOpsSupervisor)

	w := doJSON(t, engine, http.MethodPost, "/api/v1/certs/settings/exemptions", map[string]string{
		"domain": "legacy.example.com", "reason": "internal endpoint",
	})
	require.Equal(t, http.StatusOK, w.Code)

	w = doJSON(t, engine, http.MethodGet, "/api/v1/certs/settings", nil)
	require.Equal(t, http.StatusOK, w.Code)
	vo := decodeSettings(t, w)
	require.Len(t, vo.Exemptions, 1)
	assert.Equal(t, "legacy.example.com", vo.Exemptions[0].Domain)
	assert.Equal(t, "internal endpoint", vo.Exemptions[0].Reason)

	w = doJSON(t, engine, http.MethodDelete, "/api/v1/certs/settings/exemptions/legacy.example.com", nil)
	require.Equal(t, http.StatusOK, w.Code)

	w = doJSON(t, engine, http.MethodGet, "/api/v1/certs/settings", nil)
	require.Equal(t, http.StatusOK, w.Code)
	vo = decodeSettings(t, w)
	assert.Empty(t, vo.Exemptions)
}

// TestExemptions_AddEmptyDomain 空 domain → 400。
func TestExemptions_AddEmptyDomain(t *testing.T) {
	engine, _ := newDashSettingsRouter(t, RoleOpsSupervisor)
	w := doJSON(t, engine, http.MethodPost, "/api/v1/certs/settings/exemptions", map[string]string{"domain": "  "})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------
// crds（AC：接 3.4 service，重复 409）
// ---------------------------------------------------------------------

// crdVO 登记响应解码目标。
type crdVO struct {
	ID            string `json:"id"`
	ClusterID     string `json:"clusterId"`
	APIGroup      string `json:"apiGroup"`
	Kind          string `json:"kind"`
	CertFieldPath string `json:"certFieldPath"`
	Enabled       bool   `json:"enabled"`
	Builtin       bool   `json:"builtin"`
}

// TestCrds_RegisterListDelete 登记→列表（enabled/builtin）→删除。
func TestCrds_RegisterListDelete(t *testing.T) {
	engine, _ := newDashSettingsRouter(t, RoleOpsSupervisor)

	reg := map[string]string{
		"clusterId":     "prod-cluster",
		"apiGroup":      "gateway.example.com",
		"kind":          "CustomGateway",
		"certFieldPath": "spec.tls.certificateId",
	}
	w := doJSON(t, engine, http.MethodPost, "/api/v1/certs/settings/crds", reg)
	require.Equal(t, http.StatusOK, w.Code)
	var env envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	var created crdVO
	require.NoError(t, json.Unmarshal(env.Data, &created))
	assert.NotEmpty(t, created.ID)
	assert.True(t, created.Enabled, "登记成功默认 enabled=true")
	assert.False(t, created.Builtin)

	w = doJSON(t, engine, http.MethodGet, "/api/v1/certs/settings/crds", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	var list []crdVO
	require.NoError(t, json.Unmarshal(env.Data, &list))
	require.Len(t, list, 1)
	assert.Equal(t, "CustomGateway", list[0].Kind)

	w = doJSON(t, engine, http.MethodDelete, "/api/v1/certs/settings/crds/"+created.ID, nil)
	require.Equal(t, http.StatusOK, w.Code)

	w = doJSON(t, engine, http.MethodGet, "/api/v1/certs/settings/crds", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	require.NoError(t, json.Unmarshal(env.Data, &list))
	assert.Empty(t, list)
}

// TestCrds_Duplicate409 重复登记（同 clusterId+apiGroup+kind）→ 409。
func TestCrds_Duplicate409(t *testing.T) {
	engine, _ := newDashSettingsRouter(t, RoleOpsSupervisor)
	reg := map[string]string{
		"clusterId": "prod-cluster", "apiGroup": "gateway.example.com",
		"kind": "CustomGateway", "certFieldPath": "spec.tls.certificateId",
	}
	w := doJSON(t, engine, http.MethodPost, "/api/v1/certs/settings/crds", reg)
	require.Equal(t, http.StatusOK, w.Code)

	w = doJSON(t, engine, http.MethodPost, "/api/v1/certs/settings/crds", reg)
	require.Equal(t, http.StatusConflict, w.Code)
	var env envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	assert.Equal(t, CodeCrdDuplicateRegistration, env.Error.Code)
}

// TestCrds_InvalidFieldPath400 非法 certFieldPath → 400（3.4 校验期拒绝）。
func TestCrds_InvalidFieldPath400(t *testing.T) {
	engine, _ := newDashSettingsRouter(t, RoleOpsSupervisor)
	w := doJSON(t, engine, http.MethodPost, "/api/v1/certs/settings/crds", map[string]string{
		"clusterId": "prod-cluster", "apiGroup": "gateway.example.com",
		"kind": "CustomGateway", "certFieldPath": "spec..tls",
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestCrds_DeleteBuiltin400 内置固定枚举项不可删除 → 400。
func TestCrds_DeleteBuiltin400(t *testing.T) {
	engine, d := newDashSettingsRouter(t, RoleOpsSupervisor)
	// 直接经 service 播种内置登记（handler 面仅做 HTTP 映射）
	err := service.NewCrdRegistrationService(d.crds).
		EnsureBuiltinRegistrations(context.Background(), "prod-cluster")
	require.NoError(t, err)
	regs, err := d.crds.List(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, regs)

	w := doJSON(t, engine, http.MethodDelete, "/api/v1/certs/settings/crds/"+regs[0].ID.Hex(), nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestCrds_DeleteNotFound 删除不存在的登记 → 404。
func TestCrds_DeleteNotFound(t *testing.T) {
	engine, _ := newDashSettingsRouter(t, RoleOpsSupervisor)
	w := doJSON(t, engine, http.MethodDelete, "/api/v1/certs/settings/crds/6590aabbccdd000000000000", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ---------------------------------------------------------------------
// test 告警（AC：经 4.3 通道，返回成功/失败原因）
// ---------------------------------------------------------------------

// TestSettings_TestAlert 渠道确认流：未配置接收人 → sent=false；
// 配置后成功 → sent=true 且 channelConfirmed 置位。
func TestSettings_TestAlert(t *testing.T) {
	engine, d := newDashSettingsRouter(t, RoleOpsSupervisor)

	// 未配置接收人：失败原因可见
	w := doJSON(t, engine, http.MethodPost, "/api/v1/certs/settings/test", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var env envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	var res struct {
		Sent   bool   `json:"sent"`
		Reason string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &res))
	assert.False(t, res.Sent)
	assert.NotEmpty(t, res.Reason)
	assert.Empty(t, d.publisher.Events(), "未发送")

	// 配置接收人后：发送成功 + channelConfirmed=true + 事件经通道发布
	err := d.alertCfg.Save(context.Background(), &domain.AlertConfig{
		WebhookURLs: []string{"https://hooks.example.com/cert"},
	})
	require.NoError(t, err)

	w = doJSON(t, engine, http.MethodPost, "/api/v1/certs/settings/test", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	require.NoError(t, json.Unmarshal(env.Data, &res))
	assert.True(t, res.Sent)

	events := d.publisher.Events()
	require.Len(t, events, 1)
	assert.Equal(t, service.AlertCategoryTest, events[0].Category)

	w = doJSON(t, engine, http.MethodGet, "/api/v1/certs/settings", nil)
	require.Equal(t, http.StatusOK, w.Code)
	vo := decodeSettings(t, w)
	assert.True(t, vo.ChannelConfirmed, "测试告警成功即确认渠道")
}

// ---------------------------------------------------------------------
// 权限矩阵（AC：settings/crds/exemptions/test 限运维主管/审计）
// ---------------------------------------------------------------------

// TestSettings_RoleMatrix 角色权限矩阵：主管/审计 200，工程师/只读/未设置 403。
func TestSettings_RoleMatrix(t *testing.T) {
	guarded := []struct {
		method, path string
		body         any
		allowedCode  int // 允许角色命中的状态码（默认 200；crds 删除未命中登记为 404）
	}{
		{http.MethodGet, "/api/v1/certs/settings", nil, http.StatusOK},
		{http.MethodPut, "/api/v1/certs/settings", putThresholdsBody(), http.StatusOK},
		{http.MethodPost, "/api/v1/certs/settings/exemptions", map[string]string{"domain": "a.example.com"}, http.StatusOK},
		{http.MethodDelete, "/api/v1/certs/settings/exemptions/a.example.com", nil, http.StatusOK},
		{http.MethodPost, "/api/v1/certs/settings/test", nil, http.StatusOK},
		{http.MethodPost, "/api/v1/certs/settings/crds", map[string]string{
			"clusterId": "c1", "apiGroup": "g.example.com", "kind": "K", "certFieldPath": "spec.cert"}, http.StatusOK},
		{http.MethodGet, "/api/v1/certs/settings/crds", nil, http.StatusOK},
		{http.MethodDelete, "/api/v1/certs/settings/crds/6590aabbccdd000000000000", nil, http.StatusNotFound},
	}
	roles := []struct {
		role    Role
		allowed bool
	}{
		{RoleOpsSupervisor, true},
		{RoleAuditor, true},
		{RoleOpsEngineer, false},
		{RoleViewer, false},
		{"", false}, // 未设置角色：deny-by-default
	}
	for _, ep := range guarded {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			for _, r := range roles {
				t.Run("role="+string(r.role), func(t *testing.T) {
					engine, _ := newDashSettingsRouter(t, r.role)
					w := doJSON(t, engine, ep.method, ep.path, ep.body)
					if !r.allowed {
						require.Equal(t, http.StatusForbidden, w.Code)
						var env envelope
						require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
						assert.Equal(t, CodeForbidden, env.Error.Code)
						return
					}
					require.Equal(t, ep.allowedCode, w.Code)
				})
			}
		})
	}
}

// TestRequireRoles_DenyByDefault 门卫单测：未设置/未知角色 403 且 Abort。
func TestRequireRoles_DenyByDefault(t *testing.T) {
	engine, _ := newDashSettingsRouter(t, "unknown-role")
	w := doJSON(t, engine, http.MethodGet, "/api/v1/certs/settings", nil)
	assert.Equal(t, http.StatusForbidden, w.Code)
}
