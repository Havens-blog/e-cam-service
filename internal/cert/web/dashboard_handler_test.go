package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dashSettingsDeps 看板/配置端点测试依赖（内存假实现句柄，供夹具播种与断言）。
type dashSettingsDeps struct {
	certs     *certtest.FakeCertificateRepo
	refs      *certtest.FakeCertReferenceRepo
	snaps     *certtest.FakeScanSnapshotRepo
	probes    *certtest.FakeProbeResultRepo
	exempts   *certtest.FakeExemptionRepo
	alertCfg  *certtest.FakeAlertConfigRepo
	crds      *certtest.FakeCrdRegistrationRepo
	publisher *service.InMemoryAlertPublisher
}

// withRole 返回以指定角色发起请求的快捷构造（角色经 SetRole 注入，
// 模拟 7.2 EIAM 鉴权链写入；role 空串=未设置）。
func withRole(role Role) func(*gin.Context) {
	return func(c *gin.Context) {
		if role != "" {
			SetRole(c, role)
		}
	}
}

// newDashSettingsRouter 构造挂载全部 /api/v1/certs 路由的测试引擎。
// role 非空时注入角色上下文中间件（未设置角色场景传 ""）。
func newDashSettingsRouter(t *testing.T, role Role) (*gin.Engine, *dashSettingsDeps) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	d := &dashSettingsDeps{
		certs:     certtest.NewFakeCertificateRepo(),
		refs:      certtest.NewFakeCertReferenceRepo(),
		snaps:     certtest.NewFakeScanSnapshotRepo(),
		probes:    certtest.NewFakeProbeResultRepo(),
		exempts:   certtest.NewFakeExemptionRepo(),
		alertCfg:  certtest.NewFakeAlertConfigRepo(),
		crds:      certtest.NewFakeCrdRegistrationRepo(),
		publisher: service.NewInMemoryAlertPublisher(),
	}
	importSvc := service.NewImportService(d.certs, certtest.NewFakeBatchSessionRepo(), certtest.NewTestCrypto(t))
	ledgerSvc := service.NewLedgerService(d.certs, d.refs, d.snaps)
	querySvc := service.NewReferenceQueryService(d.certs, d.refs, d.snaps, &fakeScanTrigger{})
	dashSvc := service.NewDashboardService(d.certs, d.refs, d.snaps, d.probes, d.exempts, d.alertCfg, ledgerSvc, nil)
	settingsSvc := service.NewSettingsService(d.alertCfg, d.exempts, d.publisher)
	crdSvc := service.NewCrdRegistrationService(d.crds)
	engine := gin.New()
	if role != "" {
		engine.Use(withRole(role))
	}
	RegisterRoutes(engine,
		NewCertHandler(importSvc), NewReferenceHandler(querySvc),
		NewDiscoveryHandler(service.NewDiscoveryPreviewService(d.snaps, d.refs, d.certs, certtest.NewFakeCloudCertMappingRepo()), newDiscoveryImportSvcForRouter()),
		NewLedgerHandler(ledgerSvc),
		NewDashboardHandler(dashSvc), NewSettingsHandler(settingsSvc, crdSvc), newChangeHandlerFixture(t))
	return engine, d
}

// newDashboardSettingsHandlers 构造 dashboard+settings handlers
// （既有测试路由挂载复用：独立 fake 依赖，不与被测夹具共享状态）。
func newDashboardSettingsHandlers(
	certs *certtest.FakeCertificateRepo,
	refs *certtest.FakeCertReferenceRepo,
	snaps *certtest.FakeScanSnapshotRepo,
) (*DashboardHandler, *SettingsHandler) {
	ledgerSvc := service.NewLedgerService(certs, refs, snaps)
	dashSvc := service.NewDashboardService(certs, refs, snaps,
		certtest.NewFakeProbeResultRepo(), certtest.NewFakeExemptionRepo(),
		certtest.NewFakeAlertConfigRepo(), ledgerSvc, nil)
	settingsSvc := service.NewSettingsService(
		certtest.NewFakeAlertConfigRepo(), certtest.NewFakeExemptionRepo(), nil)
	crdSvc := service.NewCrdRegistrationService(certtest.NewFakeCrdRegistrationRepo())
	return NewDashboardHandler(dashSvc), NewSettingsHandler(settingsSvc, crdSvc)
}

// doJSON 发起 JSON 请求并返回响应记录器。
func doJSON(t *testing.T, engine *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		req = httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

// dashboardVO 看板响应解码目标（字段级断言用）。
type dashboardVO struct {
	Summary struct {
		CountsByLevel        []int   `json:"countsByLevel"`
		DiffAlertCount       int     `json:"diffAlertCount"`
		ExemptCount          int     `json:"exemptCount"`
		WildcardSkippedCount int     `json:"wildcardSkippedCount"`
		RegistrationRate     float64 `json:"registrationRate"`
		ReplaceableRate      float64 `json:"replaceableRate"`
		FingerprintOnlyRate  float64 `json:"fingerprintOnlyRate"`
	} `json:"summary"`
	Items []struct {
		Domain            string   `json:"domain"`
		DaysLeft          int      `json:"daysLeft"`
		Level             string   `json:"level"`
		HostingType       string   `json:"hostingType"`
		ProbeStatus       string   `json:"probeStatus"`
		ReferencedClouds  []string `json:"referencedClouds"`
		CertID            string   `json:"certId"`
		Fingerprint       string   `json:"fingerprint"`
		LastProbeAt       *string  `json:"lastProbeAt"`
		OnlineFingerprint string   `json:"onlineFingerprint"`
	} `json:"items"`
	LastInspectionAt *string `json:"lastInspectionAt"`
}

// seedDashboardCert 落一张台账证书（notAfter 偏移控制分桶；返回指纹）。
func (d *dashSettingsDeps) seedDashboardCert(t *testing.T, fp string, sans []string, notAfterOffset time.Duration, hosting domain.HostingStatus) {
	t.Helper()
	err := d.certs.Create(context.Background(), &domain.Certificate{
		Fingerprint:   fp,
		CommonName:    sans[0],
		Sans:          sans,
		NotBefore:     time.Now().Add(-24 * time.Hour),
		NotAfter:      time.Now().Add(notAfterOffset),
		HostingStatus: hosting,
	})
	require.NoError(t, err)
}

// dfp 互异 64 位 hex 指纹（看板/配置测试种子）。
func dfp(i int) string { return fmt.Sprintf("bb%04x%058x", i, i) }

// ---------------------------------------------------------------------
// dashboard 口径（AC：countsByLevel / wildcardSkippedCount / rate 字段）
// ---------------------------------------------------------------------

// TestDashboard_SummaryCountsByLevel 5 个互斥分桶计数（证书粒度，UI 卡序）。
func TestDashboard_SummaryCountsByLevel(t *testing.T) {
	engine, d := newDashSettingsRouter(t, RoleViewer)

	d.seedDashboardCert(t, dfp(1), []string{"gt.a.example.com"}, 45*24*time.Hour, domain.HostingStatusComplete)           // >30
	d.seedDashboardCert(t, dfp(2), []string{"le30.a.example.com"}, 20*24*time.Hour, domain.HostingStatusComplete)         // (14,30]
	d.seedDashboardCert(t, dfp(3), []string{"le14.a.example.com"}, 10*24*time.Hour, domain.HostingStatusComplete)         // (7,14]
	d.seedDashboardCert(t, dfp(4), []string{"le7.a.example.com"}, 3*24*time.Hour, domain.HostingStatusFingerprintOnly)    // (0,7]
	d.seedDashboardCert(t, dfp(5), []string{"expired.a.example.com"}, -24*time.Hour, domain.HostingStatusFingerprintOnly) // 已过期

	w := doJSON(t, engine, http.MethodGet, "/api/v1/certs/dashboard", nil)
	require.Equal(t, http.StatusOK, w.Code)

	var env envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	require.True(t, env.Success)
	var vo dashboardVO
	require.NoError(t, json.Unmarshal(env.Data, &vo))

	// [gt30, le30, le14, le7, expired] = [1,1,1,1,1]
	require.Len(t, vo.Summary.CountsByLevel, 5)
	assert.Equal(t, []int{1, 1, 1, 1, 1}, vo.Summary.CountsByLevel)
}

// TestDashboard_RatesAndCounts 三个 rate 口径同 stats + diff/exempt/wildcard 计数。
func TestDashboard_RatesAndCounts(t *testing.T) {
	engine, d := newDashSettingsRouter(t, RoleViewer)

	// 台账 3 张：2 complete + 1 fingerprint_only
	d.seedDashboardCert(t, dfp(1), []string{"ok1.a.example.com"}, 45*24*time.Hour, domain.HostingStatusComplete)
	d.seedDashboardCert(t, dfp(2), []string{"ok2.a.example.com"}, 45*24*time.Hour, domain.HostingStatusComplete)
	d.seedDashboardCert(t, dfp(3), []string{"fponly.a.example.com"}, 45*24*time.Hour, domain.HostingStatusFingerprintOnly)
	// 扫描发现未登记指纹 1 个 → 分母 4（=3 台账 ∪ 1 扫描缺口）
	snapID := seedDoneSnapshotRefs(t, d, []domain.CertReference{
		{CertFingerprint: dfp(9), Cloud: domain.CloudAliyun, Product: "cdn", ResourceID: "res-9", SnapshotID: "snap-1"},
	})

	_ = snapID
	// 探测结果：ok1=diff（差异告警 1）、ok2=consistent、fponly=unreachable（不计差异）
	seedProbe(t, d.probes, "ok1.a.example.com", domain.ProbeStatusDiff)
	seedProbe(t, d.probes, "ok2.a.example.com", domain.ProbeStatusConsistent)
	seedProbe(t, d.probes, "fponly.a.example.com", domain.ProbeStatusUnreachable)
	// 通配符 SAN ×2：1 个有 override（拨测替代）、1 个无（skip 计数 1）
	d.seedDashboardCert(t, dfp(4), []string{"*.wild.example.com", "*.over.example.com"}, 45*24*time.Hour, domain.HostingStatusComplete)
	require.NoError(t, d.alertCfg.Save(context.Background(), &domain.AlertConfig{
		WildcardProbeOverrides: map[string]string{"*.over.example.com": "probe.over.example.com"},
	}))
	// 豁免 1 条
	err := d.exempts.Upsert(context.Background(), &domain.Exemption{Domain: "exempt.a.example.com"})
	require.NoError(t, err)

	w := doJSON(t, engine, http.MethodGet, "/api/v1/certs/dashboard", nil)
	require.Equal(t, http.StatusOK, w.Code)

	var env envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	var vo dashboardVO
	require.NoError(t, json.Unmarshal(env.Data, &vo))

	assert.Equal(t, 1, vo.Summary.DiffAlertCount, "仅常规 diff 计差异告警")
	assert.Equal(t, 1, vo.Summary.ExemptCount)
	assert.Equal(t, 1, vo.Summary.WildcardSkippedCount, "无 override 的通配符 SAN 计 skip")
	// 口径同 stats：台账 4（3 complete+1 fponly，含通配符证）∪ 扫描缺口 1 → 分母 5；
	// registrationRate=4/5、replaceableRate=3/5、fingerprintOnlyRate=1/4
	assert.InDelta(t, 0.8, vo.Summary.RegistrationRate, 1e-9)
	assert.InDelta(t, 0.6, vo.Summary.ReplaceableRate, 1e-9)
	assert.InDelta(t, 0.25, vo.Summary.FingerprintOnlyRate, 1e-9)
}

// seedDoneSnapshotRefs 写一张成功快照 + 一组引用（referencedClouds 数据源）。
func seedDoneSnapshotRefs(t *testing.T, d *dashSettingsDeps, refs []domain.CertReference) string {
	t.Helper()
	ctx := context.Background()
	id, err := d.snaps.Create(ctx, &domain.ScanSnapshot{Status: domain.ScanStatusDone})
	require.NoError(t, err)
	require.NoError(t, d.snaps.MarkFinished(ctx, id, domain.ScanStatusDone, ""))
	for i := range refs {
		refs[i].SnapshotID = id
	}
	_, err = d.refs.CreateMulti(ctx, refs)
	require.NoError(t, err)
	return id
}

// seedProbe 写一条探测结果。
func seedProbe(t *testing.T, probes *certtest.FakeProbeResultRepo, domainName string, status domain.ProbeStatus) {
	t.Helper()
	err := probes.Create(context.Background(), &domain.ProbeResult{Domain: domainName, Status: status})
	require.NoError(t, err)
}

// TestDashboard_ItemsFields items 字段口径：domain/daysLeft/level/hostingType/
// probeStatus/referencedClouds（K8s 引用记 k8s；未探测域 probeStatus 空串）。
func TestDashboard_ItemsFields(t *testing.T) {
	engine, d := newDashSettingsRouter(t, RoleViewer)

	d.seedDashboardCert(t, dfp(1), []string{"web.a.example.com"}, 12*24*time.Hour, domain.HostingStatusComplete)
	seedDoneSnapshotRefs(t, d, []domain.CertReference{
		{CertFingerprint: dfp(1), Cloud: domain.CloudTencent, Product: "cdn", ResourceID: "r1"},
		{CertFingerprint: dfp(1), Cloud: domain.CloudTencent, Product: "waf", ResourceID: "r2"},
		{CertFingerprint: dfp(1), Cloud: "", Product: "crd", ClusterID: "c1", ResourceID: "gw"}, // K8s 引用
		{CertFingerprint: dfp(1), Cloud: domain.CloudAliyun, Product: "cdn", ResourceID: "r3"},
	})
	seedProbe(t, d.probes, "web.a.example.com", domain.ProbeStatusExempt)

	w := doJSON(t, engine, http.MethodGet, "/api/v1/certs/dashboard", nil)
	require.Equal(t, http.StatusOK, w.Code)

	var env envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	var vo dashboardVO
	require.NoError(t, json.Unmarshal(env.Data, &vo))
	require.Len(t, vo.Items, 1)

	it := vo.Items[0]
	assert.Equal(t, "web.a.example.com", it.Domain)
	assert.Equal(t, 12, it.DaysLeft)
	assert.Equal(t, "le14", it.Level)
	assert.Equal(t, "complete", it.HostingType)
	assert.Equal(t, "exempt", it.ProbeStatus)
	// 云去重 + 字典序：aliyun/tencent + k8s（K8s 引用）
	assert.Equal(t, []string{"aliyun", "k8s", "tencent"}, it.ReferencedClouds)
}

// seedProbeDetail 写一条带拨测时点与线上指纹的探测结果（抽屉字段断言用）。
func seedProbeDetail(t *testing.T, probes *certtest.FakeProbeResultRepo, domainName string, status domain.ProbeStatus, onlineFP string, probeAt time.Time) {
	t.Helper()
	err := probes.Create(context.Background(), &domain.ProbeResult{
		Domain: domainName, Status: status, OnlineFingerprint: onlineFP, ProbeAt: probeAt,
	})
	require.NoError(t, err)
}

// TestDashboard_ItemProbeDetail 抽屉字段（任务 6.4 AC4）：
// certId/fingerprint=归属证书；lastProbeAt/onlineFingerprint=最近探测记录；
// 未探测域 lastProbeAt=null、onlineFingerprint 空串、certId 仍可用。
func TestDashboard_ItemProbeDetail(t *testing.T) {
	engine, d := newDashSettingsRouter(t, RoleViewer)
	d.seedDashboardCert(t, dfp(7), []string{"probe.a.example.com"}, 12*24*time.Hour, domain.HostingStatusComplete)
	probeAt := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second)
	seedProbeDetail(t, d.probes, "probe.a.example.com", domain.ProbeStatusDiff, "cc0123456789abcdef", probeAt)

	w := doJSON(t, engine, http.MethodGet, "/api/v1/certs/dashboard", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var env envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	var vo dashboardVO
	require.NoError(t, json.Unmarshal(env.Data, &vo))
	require.Len(t, vo.Items, 1)
	it := vo.Items[0]
	assert.Equal(t, "probe.a.example.com", it.Domain)
	assert.Equal(t, dfp(7), it.Fingerprint, "台账指纹=归属证书指纹")
	require.NotNil(t, it.LastProbeAt, "已探测域 lastProbeAt 非 null")
	assert.Equal(t, probeAt.UTC().Format(time.RFC3339), *it.LastProbeAt)
	assert.Equal(t, "cc0123456789abcdef", it.OnlineFingerprint)
	// certId 指向归属证书（fake 仓库分配的 ObjectID hex）
	assert.NotEmpty(t, it.CertID)
	cert, err := d.certs.GetByFingerprint(context.Background(), dfp(7))
	require.NoError(t, err)
	assert.Equal(t, cert.ID.Hex(), it.CertID)
}

// TestDashboard_ItemsProbeStatusUnprobed 未探测域 probeStatus=空串、
// lastInspectionAt 未接线=null。
func TestDashboard_ItemsProbeStatusUnprobed(t *testing.T) {
	engine, d := newDashSettingsRouter(t, RoleViewer)
	d.seedDashboardCert(t, dfp(1), []string{"fresh.a.example.com"}, 45*24*time.Hour, domain.HostingStatusComplete)

	w := doJSON(t, engine, http.MethodGet, "/api/v1/certs/dashboard", nil)
	require.Equal(t, http.StatusOK, w.Code)

	var env envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	var vo dashboardVO
	require.NoError(t, json.Unmarshal(env.Data, &vo))
	require.Len(t, vo.Items, 1)
	assert.Empty(t, vo.Items[0].ProbeStatus)
	assert.Nil(t, vo.Items[0].LastProbeAt, "未探测域 lastProbeAt=null")
	assert.Empty(t, vo.Items[0].OnlineFingerprint)
	assert.NotEmpty(t, vo.Items[0].CertID, "未探测域抽屉链接仍需 certId")
	assert.Nil(t, vo.LastInspectionAt, "4.4 未接线时输出 null")
}

// TestDashboard_AllRoles 全角色可访问（含只读查看者）。
func TestDashboard_AllRoles(t *testing.T) {
	for _, role := range []Role{RoleOpsEngineer, RoleOpsSupervisor, RoleAuditor, RoleViewer} {
		t.Run(string(role), func(t *testing.T) {
			engine, _ := newDashSettingsRouter(t, role)
			w := doJSON(t, engine, http.MethodGet, "/api/v1/certs/dashboard", nil)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

// TestDashboard_NoRole 无角色（未设置）亦可访问看板（全角色面，认证由全局链承接）。
func TestDashboard_NoRole(t *testing.T) {
	engine, _ := newDashSettingsRouter(t, "")
	w := doJSON(t, engine, http.MethodGet, "/api/v1/certs/dashboard", nil)
	assert.Equal(t, http.StatusOK, w.Code)
}
