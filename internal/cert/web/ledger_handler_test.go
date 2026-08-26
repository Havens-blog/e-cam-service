package web

import (
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

// ledgerDeps 台账端点测试依赖（内存假实现句柄，供夹具播种）。
type ledgerDeps struct {
	certs *certtest.FakeCertificateRepo
	refs  *certtest.FakeCertReferenceRepo
	snaps *certtest.FakeScanSnapshotRepo
}

// newLedgerRouter 构造挂载全部 /api/v1/certs 路由的测试引擎
// （gin 路由树同时覆盖 /stats 静态段与 /:id 参数段共存）。
func newLedgerRouter(t *testing.T) (*gin.Engine, *ledgerDeps) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	certs := certtest.NewFakeCertificateRepo()
	refs := certtest.NewFakeCertReferenceRepo()
	snaps := certtest.NewFakeScanSnapshotRepo()
	importSvc := service.NewImportService(certs, certtest.NewFakeBatchSessionRepo(), certtest.NewTestCrypto(t))
	ledgerSvc := service.NewLedgerService(certs, refs, snaps)
	querySvc := service.NewReferenceQueryService(certs, refs, snaps, &fakeScanTrigger{})
	dashH, settingsH := newDashboardSettingsHandlers(certs, refs, snaps)
	engine := gin.New()
	// 7.2 角色门卫全量接线后，台账面测试以运维工程师身份发起
	//（工程师对列表/统计/详情/删除全量放行）。
	engine.Use(withRole(RoleOpsEngineer))
	RegisterRoutes(engine, NewCertHandler(importSvc), NewReferenceHandler(querySvc),
		NewDiscoveryHandler(service.NewDiscoveryPreviewService(snaps, refs, certs, certtest.NewFakeCloudCertMappingRepo()), newDiscoveryImportSvcForRouter()),
		NewLedgerHandler(ledgerSvc), dashH, settingsH, newChangeHandlerFixture(t))
	return engine, &ledgerDeps{certs: certs, refs: refs, snaps: snaps}
}

// lfp 互异 64 位 hex 指纹（种子前置，前缀片段可作 search 夹具）。
func lfp(i int) string { return fmt.Sprintf("aa%04x%058x", i, i) }

// seedCert 落一张台账证书（含密文私钥载荷，验证序列化白名单）。
func (d *ledgerDeps) seedCert(t *testing.T, fingerprint string, mutate func(*domain.Certificate)) string {
	t.Helper()
	c := &domain.Certificate{
		Fingerprint:   fingerprint,
		CommonName:    fmt.Sprintf("cn-%s.example.com", fingerprint[:6]),
		Sans:          []string{fmt.Sprintf("san-%s.example.com", fingerprint[:6])},
		Issuer:        "certtest Intermediate CA",
		SerialNumber:  "serial-" + fingerprint[:6],
		NotBefore:     time.Now().Add(-24 * time.Hour),
		NotAfter:      time.Now().Add(365 * 24 * time.Hour),
		KeyAlgorithm:  domain.KeyAlgorithmECDSA,
		HostingStatus: domain.HostingStatusComplete,
		// 密文载荷：若序列化白名单失效，正文将出现该值（断言不泄露）
		EncryptedPrivateKey: &domain.EncryptedSecret{
			Ciphertext: "bGVhazAuY2lwaGVydGV4dC5wcm9iZQ==", KeyVersion: 1, Algo: domain.AlgoAES256GCM,
		},
		CertPEM: "-----BEGIN CERTIFICATE-----\nZm9v\n-----END CERTIFICATE-----",
	}
	if mutate != nil {
		mutate(c)
	}
	require.NoError(t, d.certs.Create(context.Background(), c))
	return c.ID.Hex()
}

func (d *ledgerDeps) seedDoneSnapshot(t *testing.T, coverage []domain.CoverageMeta) string {
	t.Helper()
	snap := &domain.ScanSnapshot{StartedAt: time.Now().Add(-time.Hour), CoverageMeta: coverage}
	id, err := d.snaps.Create(context.Background(), snap)
	require.NoError(t, err)
	require.NoError(t, d.snaps.MarkFinished(context.Background(), id, domain.ScanStatusDone, ""))
	return id
}

func (d *ledgerDeps) seedRef(t *testing.T, fingerprint, snapID string, cloud domain.Cloud, product domain.Product) {
	t.Helper()
	_, err := d.refs.CreateMulti(context.Background(), []domain.CertReference{{
		CertFingerprint: fingerprint, Cloud: cloud, Product: product,
		ResourceID: "res-1", SnapshotID: snapID,
	}})
	require.NoError(t, err)
}

func doGet(t *testing.T, engine *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func doDelete(t *testing.T, engine *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, path, nil))
	return w
}

// decodeData 将信封 data 解码为 map。
func decodeData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	env := decode(t, w)
	var data map[string]any
	require.NoError(t, json.Unmarshal(env.Data, &data), "data: %s", env.Data)
	return data
}

// ---------------------------------------------------------------------
// AC1：GET /api/v1/certs 列表
// ---------------------------------------------------------------------

func TestListCertsAPI(t *testing.T) {
	engine, d := newLedgerRouter(t)
	now := time.Now()
	for i := 1; i <= 22; i++ {
		offset := time.Duration(i) * 24 * time.Hour
		d.seedCert(t, lfp(i), func(c *domain.Certificate) { c.NotAfter = now.Add(offset) })
	}

	// 默认分页：每页 20。data 载荷为 {items,total,page,pageSize}（前端 ListCertsResponse
	// 契约——unwrapCertEnvelope 成功路径只取 data，分页信息必须随载荷返回）。
	w := doGet(t, engine, "/api/v1/certs")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	env := decode(t, w)
	assert.True(t, env.Success)
	var page1 struct {
		Items    []map[string]any `json:"items"`
		Total    float64          `json:"total"`
		Page     float64          `json:"page"`
		PageSize float64          `json:"pageSize"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &page1))
	assert.Len(t, page1.Items, 20)
	assert.Equal(t, float64(22), page1.Total)
	assert.Equal(t, float64(1), page1.Page)
	assert.Equal(t, float64(20), page1.PageSize)

	// AC1 字段齐全（白名单）
	first := page1.Items[0]
	for _, field := range []string{"id", "fingerprint", "commonName", "sans", "issuer",
		"notAfter", "daysLeft", "hostingStatus", "protectUntil", "refCount"} {
		assert.Contains(t, first, field, "列表项缺字段 %s", field)
	}
	assert.NotContains(t, first, "encryptedPrivateKey")
	assert.NotContains(t, first, "certPem")
	assertNoKeyMaterial(t, w)

	// 第二页
	w2 := doGet(t, engine, "/api/v1/certs?page=2")
	require.Equal(t, http.StatusOK, w2.Code)
	env2 := decode(t, w2)
	var page2 struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(env2.Data, &page2))
	assert.Len(t, page2.Items, 2)
}

func TestListCertsAPIFilters(t *testing.T) {
	engine, d := newLedgerRouter(t)
	now := time.Now()
	d.seedCert(t, lfp(1), func(c *domain.Certificate) { c.NotAfter = now.Add(3 * 24 * time.Hour) })
	d.seedCert(t, lfp(2), func(c *domain.Certificate) {
		c.NotAfter = now.Add(40 * 24 * time.Hour)
		c.HostingStatus = domain.HostingStatusFingerprintOnly
		c.EncryptedPrivateKey = nil
	})
	d.seedCert(t, lfp(3), func(c *domain.Certificate) { c.NotAfter = now.Add(-time.Hour) })

	count := func(path string) int {
		w := doGet(t, engine, path)
		require.Equal(t, http.StatusOK, w.Code, path+": "+w.Body.String())
		env := decode(t, w)
		var page struct {
			Items []map[string]any `json:"items"`
		}
		require.NoError(t, json.Unmarshal(env.Data, &page))
		return len(page.Items)
	}

	assert.Equal(t, 1, count("/api/v1/certs?hostingStatus=fingerprint_only"))
	assert.Equal(t, 1, count("/api/v1/certs?daysLeft=expired"))
	assert.Equal(t, 1, count("/api/v1/certs?daysLeft=le7"))
	assert.Equal(t, 1, count("/api/v1/certs?daysLeft=gt30"))
	assert.Equal(t, 1, count("/api/v1/certs?search=san-aa0001"))
	assert.Equal(t, 1, count("/api/v1/certs?search="+lfp(3)[:10]))
	assert.Equal(t, 3, count("/api/v1/certs"))

	// 非法参数 → 400 INVALID_REQUEST
	for _, path := range []string{
		"/api/v1/certs?hostingStatus=bogus",
		"/api/v1/certs?daysLeft=next-week",
	} {
		w := doGet(t, engine, path)
		assert.Equal(t, http.StatusBadRequest, w.Code, path)
		env := decode(t, w)
		require.NotNil(t, env.Error)
		assert.Equal(t, CodeInvalidRequest, env.Error.Code)
	}
}

// ---------------------------------------------------------------------
// AC2：GET /api/v1/certs/:id 详情
// ---------------------------------------------------------------------

func TestGetCertAPI(t *testing.T) {
	engine, d := newLedgerRouter(t)
	id := d.seedCert(t, lfp(1), func(c *domain.Certificate) {
		c.ExpectedDomain = "san-aa0001.example.com"
	})
	snapID := d.seedDoneSnapshot(t, []domain.CoverageMeta{{Cloud: "aliyun", Product: "cdn", Covered: 1, Total: 1}})
	d.seedRef(t, lfp(1), snapID, domain.CloudAliyun, domain.ProductCDN)

	w := doGet(t, engine, "/api/v1/certs/"+id)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	data := decodeData(t, w)

	assert.Equal(t, lfp(1), data["fingerprint"])
	assert.Equal(t, "cn-aa0001.example.com", data["commonName"])
	assert.Equal(t, true, data["hasKey"], "完整托管 → hasKey=true")
	assert.Equal(t, "complete", data["hostingStatus"])
	assert.Equal(t, "ECDSA", data["keyAlgorithm"])
	assert.Equal(t, "serial-aa0001", data["serialNumber"])
	assert.Equal(t, "has_refs", data["referenceStatus"])
	assert.Equal(t, float64(1), data["refCount"])
	assert.Contains(t, data, "notBefore")
	assert.Contains(t, data, "notAfter")
	assert.Contains(t, data, "daysLeft")
	assert.Contains(t, data, "createdAt")
	assert.Contains(t, data, "expiryAlertLevel")

	// Hard Rule：密文与明文均不得出现在任何 JSON
	assertNoKeyMaterial(t, w)
	assert.NotContains(t, w.Body.String(), "bGVhazAuY2lwaGVydGV4dC5wcm9iZQ==", "密文值不得回显")
	assert.NotContains(t, w.Body.String(), "Zm9v", "certPem 内容不得回显")

	// 仅指纹登记 → hasKey=false
	id2 := d.seedCert(t, lfp(2), func(c *domain.Certificate) {
		c.HostingStatus = domain.HostingStatusFingerprintOnly
		c.EncryptedPrivateKey = nil
	})
	w2 := doGet(t, engine, "/api/v1/certs/"+id2)
	require.Equal(t, http.StatusOK, w2.Code)
	assert.Equal(t, false, decodeData(t, w2)["hasKey"])

	// 未命中 / 非法 ID
	w3 := doGet(t, engine, "/api/v1/certs/000000000000000000000000")
	assert.Equal(t, http.StatusNotFound, w3.Code)
	w4 := doGet(t, engine, "/api/v1/certs/not-hex")
	assert.Equal(t, http.StatusBadRequest, w4.Code)
}

// ---------------------------------------------------------------------
// AC3/AC6：DELETE /api/v1/certs/:id 三分支 + 保护期
// ---------------------------------------------------------------------

func TestDeleteCertAPIBranches(t *testing.T) {
	t.Run("has_refs blocked", func(t *testing.T) {
		engine, d := newLedgerRouter(t)
		id := d.seedCert(t, lfp(1), nil)
		snapID := d.seedDoneSnapshot(t, nil)
		d.seedRef(t, lfp(1), snapID, domain.CloudAliyun, domain.ProductCDN)

		w := doDelete(t, engine, "/api/v1/certs/"+id)
		assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		env := decode(t, w)
		require.NotNil(t, env.Error)
		assert.Equal(t, "CERT_HAS_REFS", env.Error.Code)

		var meta map[string]any
		require.NoError(t, json.Unmarshal(env.Meta, &meta))
		assert.Equal(t, "has_refs", meta["referenceStatus"])
		assert.Equal(t, float64(1), meta["refCount"])
		// 证书仍在
		w2 := doGet(t, engine, "/api/v1/certs/"+id)
		assert.Equal(t, http.StatusOK, w2.Code)
	})

	t.Run("blind_spot blocked with reason", func(t *testing.T) {
		engine, d := newLedgerRouter(t)
		id := d.seedCert(t, lfp(1), nil) // 无任何成功快照 → 盲区

		w := doDelete(t, engine, "/api/v1/certs/"+id)
		assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		env := decode(t, w)
		require.NotNil(t, env.Error)
		assert.Equal(t, "CERT_HAS_REFS", env.Error.Code)

		var meta map[string]any
		require.NoError(t, json.Unmarshal(env.Meta, &meta))
		assert.Equal(t, "blind_spot", meta["referenceStatus"])
		reason, _ := meta["reason"].(string)
		assert.Contains(t, reason, "snapshot", "blind_spot 附盲区原因")
	})

	t.Run("no_refs_scanned in protection period blocked", func(t *testing.T) {
		engine, d := newLedgerRouter(t)
		d.seedDoneSnapshot(t, nil)
		until := time.Now().Add(72 * time.Hour)
		id := d.seedCert(t, lfp(1), func(c *domain.Certificate) { c.ProtectUntil = &until })

		w := doDelete(t, engine, "/api/v1/certs/"+id)
		assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		env := decode(t, w)
		require.NotNil(t, env.Error)
		assert.Equal(t, "CERT_HAS_REFS", env.Error.Code)

		var meta map[string]any
		require.NoError(t, json.Unmarshal(env.Meta, &meta))
		assert.Equal(t, "no_refs_scanned", meta["referenceStatus"])
		assert.Contains(t, meta, "protectUntil", "附保护期截止时间")
	})

	t.Run("no_refs_scanned outside protection deleted", func(t *testing.T) {
		engine, d := newLedgerRouter(t)
		d.seedDoneSnapshot(t, nil)
		id := d.seedCert(t, lfp(1), nil)

		w := doDelete(t, engine, "/api/v1/certs/"+id)
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
		data := decodeData(t, w)
		assert.Equal(t, true, data["deleted"])

		w2 := doGet(t, engine, "/api/v1/certs/"+id)
		assert.Equal(t, http.StatusNotFound, w2.Code)
	})

	t.Run("not found and invalid id", func(t *testing.T) {
		engine, _ := newLedgerRouter(t)
		w := doDelete(t, engine, "/api/v1/certs/000000000000000000000000")
		assert.Equal(t, http.StatusNotFound, w.Code)
		w2 := doDelete(t, engine, "/api/v1/certs/bad-id")
		assert.Equal(t, http.StatusBadRequest, w2.Code)
	})
}

// ---------------------------------------------------------------------
// AC4/AC5/AC6：GET /api/v1/certs/stats 双口径覆盖率
// ---------------------------------------------------------------------

func TestStatsAPI(t *testing.T) {
	engine, d := newLedgerRouter(t)

	// 台账：A complete、B fingerprint_only、C complete
	d.seedCert(t, lfp(1), nil)
	d.seedCert(t, lfp(2), func(c *domain.Certificate) {
		c.HostingStatus = domain.HostingStatusFingerprintOnly
		c.EncryptedPrivateKey = nil
	})
	d.seedCert(t, lfp(3), nil)
	// 扫描发现：A（与台账重叠）、X、Y（未登记）
	snapID := d.seedDoneSnapshot(t, nil)
	d.seedRef(t, lfp(1), snapID, domain.CloudAliyun, domain.ProductCDN)
	d.seedRef(t, lfp(1), snapID, domain.CloudTencent, domain.ProductWAF)
	d.seedRef(t, lfp(11), snapID, domain.CloudAliyun, domain.ProductCDN)
	d.seedRef(t, lfp(12), snapID, domain.CloudAWS, domain.ProductCDN)

	w := doGet(t, engine, "/api/v1/certs/stats")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	data := decodeData(t, w)

	assert.Equal(t, float64(3), data["total"])
	assert.Equal(t, float64(2), data["complete"])
	assert.Equal(t, float64(1), data["fingerprintOnly"])
	assert.Equal(t, float64(2), data["missingRegistrations"])
	assert.Equal(t, float64(5), data["denominator"], "分母=扫描去重∪台账=|{A,X,Y,B,C}|")
	assert.InDelta(t, 0.6, data["registrationRate"], 1e-9)
	assert.InDelta(t, 0.4, data["replaceableRate"], 1e-9)
	assert.InDelta(t, 1.0/3.0, data["fingerprintOnlyRate"], 0.0001)

	sources, ok := data["denominatorSources"].(map[string]any)
	require.True(t, ok, "denominatorSources 为对象")
	assert.Equal(t, float64(3), sources["scannedUniqueFingerprints"], "{A,X,Y} 去重")
	assert.Equal(t, float64(2), sources["manualOnlyFingerprints"], "{B,C}")

	assertNoKeyMaterial(t, w)
}
