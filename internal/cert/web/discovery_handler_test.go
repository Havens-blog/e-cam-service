package web

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// discoveryDeps 发现导入端点测试依赖（内存假实现句柄；查询面与会话面共享
// 同一 fake 世界——预览/导入/落库断言经同一假实现读回）。
type discoveryDeps struct {
	certs    *certtest.FakeCertificateRepo
	refs     *certtest.FakeCertReferenceRepo
	snaps    *certtest.FakeScanSnapshotRepo
	mappings *certtest.FakeCloudCertMappingRepo
	imports  *discoveryImportTestDeps
}

// newDiscoveryRouter 构造挂载全部 /api/v1/certs 路由的测试引擎
// （覆盖 /discovery 静态段与 ledger /:id 参数段共存；发现面以运维工程师发起）。
func newDiscoveryRouter(t *testing.T) (*gin.Engine, *discoveryDeps) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	d := &discoveryDeps{
		certs:    certtest.NewFakeCertificateRepo(),
		refs:     certtest.NewFakeCertReferenceRepo(),
		snaps:    certtest.NewFakeScanSnapshotRepo(),
		mappings: certtest.NewFakeCloudCertMappingRepo(),
		imports:  newDiscoveryImportTestDeps(),
	}
	importSvc := service.NewImportService(d.certs, certtest.NewFakeBatchSessionRepo(), certtest.NewTestCrypto(t))
	ledgerSvc := service.NewLedgerService(d.certs, d.refs, d.snaps)
	querySvc := service.NewReferenceQueryService(d.certs, d.refs, d.snaps, &fakeScanTrigger{})
	discSvc := service.NewDiscoveryPreviewService(d.snaps, d.refs, d.certs, d.mappings)
	discImportSvc := d.imports.svc(d.certs, d.mappings, d.refs)
	dashH, settingsH := newDashboardSettingsHandlers(d.certs, d.refs, d.snaps)
	engine := gin.New()
	engine.Use(withRole(RoleOpsEngineer))
	RegisterRoutes(engine, NewCertHandler(importSvc), NewReferenceHandler(querySvc),
		NewDiscoveryHandler(discSvc, discImportSvc), NewLedgerHandler(ledgerSvc),
		dashH, settingsH, newChangeHandlerFixture(t))
	return engine, d
}

// seedDoneSnapshotAt 指定 startedAt 的 done 快照。
func (d *discoveryDeps) seedDoneSnapshotAt(t *testing.T, startedAt time.Time) string {
	t.Helper()
	id, err := d.snaps.Create(context.Background(), &domain.ScanSnapshot{StartedAt: startedAt})
	require.NoError(t, err)
	require.NoError(t, d.snaps.MarkFinished(context.Background(), id, domain.ScanStatusDone, ""))
	return id
}

// seedRef 单条云引用播种。
func (d *discoveryDeps) seedRef(t *testing.T, snapID string, cloud domain.Cloud, accountKey, cloudCertID, fp, resourceID string) {
	t.Helper()
	_, err := d.refs.CreateMulti(context.Background(), []domain.CertReference{{
		CertFingerprint:       fp,
		Cloud:                 cloud,
		Product:               domain.ProductCDN,
		ResourceID:            resourceID,
		ReferencedCloudCertID: cloudCertID,
		AccountKey:            accountKey,
		SnapshotID:            snapID,
	}})
	require.NoError(t, err)
}

// ---------------------------------------------------------------------
// GET /api/v1/certs/discovery/preview
// ---------------------------------------------------------------------

func TestDiscoveryPreviewAPI(t *testing.T) {
	t.Run("aggregated entries with seven field classes", func(t *testing.T) {
		engine, d := newDiscoveryRouter(t)
		started := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
		snapID := d.seedDoneSnapshotAt(t, started)
		// 未登记条目（notAfter 占位）
		d.seedRef(t, snapID, domain.CloudAliyun, "acct-main", "cert-A", lfp(1), "cdn-res-1")
		// 台账指纹命中条目（inLedger + 台账 notAfter）
		ledgerNA := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
		require.NoError(t, d.certs.Create(context.Background(), &domain.Certificate{
			Fingerprint: lfp(2), CommonName: "cn-ledger", NotAfter: ledgerNA,
			HostingStatus: domain.HostingStatusFingerprintOnly,
		}))
		d.seedRef(t, snapID, domain.CloudTencent, "acct-tx", "ssl-8", lfp(2), "waf-res-8")

		w := doGet(t, engine, "/api/v1/certs/discovery/preview")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		data := decodeData(t, w)

		assert.Equal(t, snapID, data["snapshotId"])
		assert.Equal(t, started.UTC().Format(time.RFC3339), data["snapshotStartedAt"], "附快照时间（超 7 天重扫提示依据）")
		assert.Equal(t, float64(2), data["count"])
		items, ok := data["items"].([]any)
		require.True(t, ok, "items 为数组而非 null")
		require.Len(t, items, 2)

		// 七类字段白名单：cloud/accountKey/cloudCertId/refCount/inLedger/notAfter/parseable
		pending := items[0].(map[string]any) // aliyun 字典序在前
		for _, field := range []string{"cloud", "accountKey", "cloudCertId", "refCount", "inLedger", "notAfter", "parseable"} {
			assert.Contains(t, pending, field, "预览条目缺字段 %s", field)
		}
		assert.Equal(t, "aliyun", pending["cloud"])
		assert.Equal(t, "acct-main", pending["accountKey"])
		assert.Equal(t, "cert-A", pending["cloudCertId"])
		assert.Equal(t, float64(1), pending["refCount"])
		assert.Equal(t, false, pending["inLedger"])
		assert.Equal(t, DiscoveryNotAfterPending, pending["notAfter"], "未登记条目 notAfter 占位显示")
		assert.Equal(t, true, pending["parseable"])

		inLedger := items[1].(map[string]any)
		assert.Equal(t, "tencent", inLedger["cloud"])
		assert.Equal(t, true, inLedger["inLedger"], "台账指纹命中条目 inLedger=true")
		assert.Equal(t, ledgerNA.UTC().Format(time.RFC3339), inLedger["notAfter"], "inLedger 条目显示台账 NotAfter")

		assertNoKeyMaterial(t, w)
	})

	t.Run("no done snapshot returns structured NO_SNAPSHOT not 500", func(t *testing.T) {
		engine, _ := newDiscoveryRouter(t) // 无任何快照
		w := doGet(t, engine, "/api/v1/certs/discovery/preview")
		assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		env := decode(t, w)
		require.NotNil(t, env.Error)
		assert.Equal(t, "NO_SNAPSHOT", env.Error.Code, "SC-3：结构化错误码（非 500）")
		assert.False(t, env.Success)
		assert.NotEmpty(t, env.Error.Message)
	})

	t.Run("route not swallowed by :id", func(t *testing.T) {
		// Gin 静态段/参数段共存：/discovery/preview 不得被 /:id 吃掉
		engine, _ := newDiscoveryRouter(t)
		w := doGet(t, engine, "/api/v1/certs/discovery/preview")
		assert.Equal(t, http.StatusConflict, w.Code, "命中 discovery 路由（NO_SNAPSHOT）而非 :id 404")
	})
}

// ---------------------------------------------------------------------
// GET /api/v1/certs/discovery/snapshot-status
// ---------------------------------------------------------------------

func TestDiscoverySnapshotStatusAPI(t *testing.T) {
	t.Run("zero snapshots empty state", func(t *testing.T) {
		engine, _ := newDiscoveryRouter(t)
		w := doGet(t, engine, "/api/v1/certs/discovery/snapshot-status")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		data := decodeData(t, w)
		assert.Equal(t, false, data["hasSnapshot"], "零快照空态：200 + hasSnapshot=false（区别于 preview 的 NO_SNAPSHOT）")
		partials, ok := data["partialFailures"].([]any)
		require.True(t, ok, "partialFailures 为空数组而非 null")
		assert.Empty(t, partials)
	})

	t.Run("failed latest with partial failures", func(t *testing.T) {
		engine, d := newDiscoveryRouter(t)
		d.seedDoneSnapshotAt(t, time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)) // 旧 done 被新 failed 遮蔽
		failedID, err := d.snaps.Create(context.Background(), &domain.ScanSnapshot{
			StartedAt: time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC),
		})
		require.NoError(t, err)
		require.NoError(t, d.snaps.FinishScan(context.Background(), failedID,
			domain.ScanStatusFailed, domain.FailReasonScanDiscoveryFailed, nil,
			[]domain.ScanChannelFailure{{Cloud: "huawei", Product: "cdn", Account: "acct-hw", Reason: "list refs failed"}}))

		w := doGet(t, engine, "/api/v1/certs/discovery/snapshot-status")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		data := decodeData(t, w)
		assert.Equal(t, true, data["hasSnapshot"])
		assert.Equal(t, failedID, data["snapshotId"])
		assert.Equal(t, "failed", data["status"])
		assert.Equal(t, domain.FailReasonScanDiscoveryFailed, data["failReason"])
		assert.NotEmpty(t, data["startedAt"])
		partials := data["partialFailures"].([]any)
		require.Len(t, partials, 1)
		p := partials[0].(map[string]any)
		assert.Equal(t, "huawei", p["cloud"])
		assert.Equal(t, "acct-hw", p["account"])
		assert.Equal(t, "list refs failed", p["reason"])
		assertNoKeyMaterial(t, w)
	})

	t.Run("running latest", func(t *testing.T) {
		engine, d := newDiscoveryRouter(t)
		started := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
		_, err := d.snaps.Create(context.Background(), &domain.ScanSnapshot{StartedAt: started})
		require.NoError(t, err)

		w := doGet(t, engine, "/api/v1/certs/discovery/snapshot-status")
		require.Equal(t, http.StatusOK, w.Code)
		data := decodeData(t, w)
		assert.Equal(t, true, data["hasSnapshot"])
		assert.Equal(t, "running", data["status"], "running 可见（引导轮询 running→done/failed）")
		assert.Equal(t, started.UTC().Format(time.RFC3339), data["startedAt"])
	})

	t.Run("snapshot repo failure surfaces 500 envelope", func(t *testing.T) {
		// 快照仓储故障 → 500 INTERNAL_ERROR（区别于零快照 200 空态）
		gin.SetMode(gin.TestMode)
		certs := certtest.NewFakeCertificateRepo()
		refs := certtest.NewFakeCertReferenceRepo()
		snaps := &failingLatestSnapRepo{FakeScanSnapshotRepo: certtest.NewFakeScanSnapshotRepo()}
		discSvc := service.NewDiscoveryPreviewService(snaps, refs, certs, certtest.NewFakeCloudCertMappingRepo())
		engine := gin.New()
		engine.Use(withRole(RoleOpsEngineer))
		RegisterRoutes(engine, NewCertHandler(service.NewImportService(certs, certtest.NewFakeBatchSessionRepo(), certtest.NewTestCrypto(t))),
			NewReferenceHandler(service.NewReferenceQueryService(certs, refs, snaps, &fakeScanTrigger{})),
			NewDiscoveryHandler(discSvc, newDiscoveryImportSvcForRouter()), NewLedgerHandler(service.NewLedgerService(certs, refs, snaps)),
			nil, nil, newChangeHandlerFixture(t))

		w := doGet(t, engine, "/api/v1/certs/discovery/snapshot-status")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		env := decode(t, w)
		require.NotNil(t, env.Error)
		assert.Equal(t, "INTERNAL_ERROR", env.Error.Code)
	})
}

// failingLatestSnapRepo Latest 注入故障的包装（覆盖 snapshot-status 错误传播）。
type failingLatestSnapRepo struct {
	*certtest.FakeScanSnapshotRepo
}

func (f *failingLatestSnapRepo) Latest(context.Context) (domain.ScanSnapshot, error) {
	return domain.ScanSnapshot{}, errors.New("snapshot storage unavailable")
}

// ---------------------------------------------------------------------
// 会话面端点（cert-cloud-discovery-import 任务 5）
// ---------------------------------------------------------------------

// webImportAccountSource 发现导入 web 层测试账号源（命名区别于 service 层
// discoveryAccountSourceStub，防撞名）。
type webImportAccountSource struct {
	accounts map[domain.Cloud][]*sharedomain.CloudAccount
}

func (s *webImportAccountSource) ActiveByCloud(_ context.Context, cloud domain.Cloud) ([]*sharedomain.CloudAccount, error) {
	return s.accounts[cloud], nil
}

// webImportCertAdapter 发现导入 web 层测试材料端口桩：certID→材料映射；
// 未配置的 certID 返回 Exists=false（条目按"云侧已不存在"口径记因）。
type webImportCertAdapter struct {
	cloud    domain.Cloud
	material map[string]service.DiscoveryCertMaterial
}

func (a *webImportCertAdapter) Cloud() domain.Cloud { return a.cloud }

func (a *webImportCertAdapter) GetCertChain(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) (service.DiscoveryCertMaterial, error) {
	if m, ok := a.material[cloudCertID]; ok {
		return m, nil
	}
	return service.DiscoveryCertMaterial{Exists: false}, nil
}

// discoveryImportTestDeps 会话面测试依赖（certs/mappings/refs 与查询面共享
// 同一 fake 世界，落库断言经同一假实现读回）。
type discoveryImportTestDeps struct {
	sessions *certtest.FakeDiscoveryImportSessionRepo
	accounts *webImportAccountSource
	aliyun   *webImportCertAdapter
}

// newDiscoveryImportTestDeps 会话面测试依赖：双阿里账号（多账号同证书场景）
// + 阿里材料桩（material 由用例按 certID 装载）。
func newDiscoveryImportTestDeps() *discoveryImportTestDeps {
	return &discoveryImportTestDeps{
		sessions: certtest.NewFakeDiscoveryImportSessionRepo(),
		accounts: &webImportAccountSource{accounts: map[domain.Cloud][]*sharedomain.CloudAccount{
			domain.CloudAliyun: {
				{Name: "acct-a", Provider: sharedomain.CloudProvider(domain.CloudAliyun), Status: sharedomain.CloudAccountStatusActive},
				{Name: "acct-b", Provider: sharedomain.CloudProvider(domain.CloudAliyun), Status: sharedomain.CloudAccountStatusActive},
			},
		}},
		aliyun: &webImportCertAdapter{cloud: domain.CloudAliyun, material: map[string]service.DiscoveryCertMaterial{}},
	}
}

// svc 基于指定 fake 世界构造发现导入服务（certs/mappings/refs 由调用方注入，
// 查询面/会话面共享同一假实现）。
func (d *discoveryImportTestDeps) svc(
	certs *certtest.FakeCertificateRepo,
	mappings *certtest.FakeCloudCertMappingRepo,
	refs *certtest.FakeCertReferenceRepo,
) service.DiscoveryImportService {
	return service.NewDiscoveryImportService(d.sessions, certs, mappings, refs,
		[]service.DiscoveryCertAdapter{d.aliyun}, d.accounts)
}

// newDiscoveryImportSvcForRouter 非发现用例测试路由（cert/change/dashboard/
// ledger/reference 及权限矩阵）的发现导入服务装配：自含 fake 世界，仅保证
// POST /discovery/import 在允许角色下可达业务层（不进入业务断言）。
func newDiscoveryImportSvcForRouter() service.DiscoveryImportService {
	d := newDiscoveryImportTestDeps()
	return d.svc(certtest.NewFakeCertificateRepo(), certtest.NewFakeCloudCertMappingRepo(), certtest.NewFakeCertReferenceRepo())
}

// pollDiscoveryImportTerminal 经进度端点轮询直至终态（completed/partial_failed）。
func pollDiscoveryImportTerminal(t *testing.T, engine *gin.Engine, sessionID string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		w := doGet(t, engine, "/api/v1/certs/discovery/import/"+sessionID)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		data := decodeData(t, w)
		if data["status"] != string(domain.DiscoveryImportRunning) {
			return data
		}
		if time.Now().After(deadline) {
			t.Fatalf("discovery import session %s did not reach terminal state within deadline: %v", sessionID, data)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// discoveryImportItems 勾选条目请求体。
func discoveryImportItems(items []map[string]string) map[string]any {
	return map[string]any{"items": items}
}

func TestDiscoveryImportAPI_MultiAccountSameCertEndToEnd(t *testing.T) {
	engine, d := newDiscoveryRouter(t)
	// 同一证书材料挂两个云证书 ID（多账号各引用一份）
	b := certtest.NewBundle(t, "www.shared-example.com", []string{"www.shared-example.com"}, nil)
	d.imports.aliyun.material["cert-acc-a"] = service.DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)}
	d.imports.aliyun.material["cert-acc-b"] = service.DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)}

	// 202 初始快照：sessionId + running + 全 pending + progress.total
	w := doPostJSON(t, engine, "/api/v1/certs/discovery/import", discoveryImportItems([]map[string]string{
		{"cloud": "aliyun", "accountKey": "acct-a", "cloudCertId": "cert-acc-a"},
		{"cloud": "aliyun", "accountKey": "acct-b", "cloudCertId": "cert-acc-b"},
	}))
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	snap := decodeData(t, w)
	sessionID, _ := snap["sessionId"].(string)
	require.NotEmpty(t, sessionID, "202 语义返回 sessionId（会话句柄）")
	assert.Equal(t, "running", snap["status"])
	assert.NotEmpty(t, snap["createdAt"])
	sItems, ok := snap["items"].([]any)
	require.True(t, ok, "items 为数组而非 null")
	require.Len(t, sItems, 2)
	for _, raw := range sItems {
		it := raw.(map[string]any)
		assert.Equal(t, "pending", it["result"], "202 初始快照条目为 pending")
		assert.NotContains(t, it, "mappedCertId", "pending 条目不带 mappedCertId")
	}
	sProg := snap["progress"].(map[string]any)
	assert.Equal(t, float64(2), sProg["total"])
	assert.Equal(t, float64(0), sProg["succeeded"])
	assert.Equal(t, float64(0), sProg["failed"])
	assertNoKeyMaterial(t, w)

	// 进度轮询至终态：逐条 result/mappedCertId/errorReason + progress 计数
	final := pollDiscoveryImportTerminal(t, engine, sessionID)
	assert.Equal(t, "completed", final["status"])
	assert.NotNil(t, final["finishedAt"], "终态时点可见")
	fProg := final["progress"].(map[string]any)
	assert.Equal(t, float64(2), fProg["total"])
	assert.Equal(t, float64(2), fProg["succeeded"])
	assert.Equal(t, float64(0), fProg["failed"])
	fItems, ok := final["items"].([]any)
	require.True(t, ok)
	require.Len(t, fItems, 2)
	mappedIDs := make([]string, 0, 2)
	for _, raw := range fItems {
		it := raw.(map[string]any)
		assert.Equal(t, "success", it["result"])
		id, _ := it["mappedCertId"].(string)
		require.NotEmpty(t, id, "success 条目带 mappedCertId（台账证书 ID）")
		mappedIDs = append(mappedIDs, id)
	}
	assert.Equal(t, mappedIDs[0], mappedIDs[1], "同证书多账号映射到同一台账记录")

	// SC-6 端到端落库断言：1 台账记录 + 按账号各 1 映射
	ledger, err := d.certs.List(context.Background())
	require.NoError(t, err)
	require.Len(t, ledger, 1, "多账号同证书仅 1 条台账记录（uk_fingerprint）")
	assert.Equal(t, b.Fingerprint, ledger[0].Fingerprint)
	assert.Equal(t, domain.HostingStatusFingerprintOnly, ledger[0].HostingStatus)
	mappings, err := d.mappings.ListByFingerprint(context.Background(), b.Fingerprint)
	require.NoError(t, err)
	require.Len(t, mappings, 2, "CloudCertMapping 按账号各 1 条")
	assert.ElementsMatch(t, []string{"acct-a", "acct-b"}, []string{mappings[0].AccountKey, mappings[1].AccountKey})
	assert.ElementsMatch(t, []string{"cert-acc-a", "cert-acc-b"}, []string{mappings[0].CloudCertID, mappings[1].CloudCertID})
}

func TestDiscoveryImportAPI_PartialFailedTerminal(t *testing.T) {
	engine, d := newDiscoveryRouter(t)
	b := certtest.NewBundle(t, "www.partial-example.com", []string{"www.partial-example.com"}, nil)
	d.imports.aliyun.material["cert-ok"] = service.DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)}
	// cert-gone 未配置材料 → Exists=false（预览后云侧删除漂移口径）

	w := doPostJSON(t, engine, "/api/v1/certs/discovery/import", discoveryImportItems([]map[string]string{
		{"cloud": "aliyun", "accountKey": "acct-a", "cloudCertId": "cert-ok"},
		{"cloud": "aliyun", "accountKey": "acct-a", "cloudCertId": "cert-gone"},
	}))
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	sessionID, _ := decodeData(t, w)["sessionId"].(string)

	final := pollDiscoveryImportTerminal(t, engine, sessionID)
	assert.Equal(t, "partial_failed", final["status"], "终态 partial_failed 可判")
	fItems := final["items"].([]any)
	require.Len(t, fItems, 2)
	succeeded := fItems[0].(map[string]any)
	assert.Equal(t, "success", succeeded["result"])
	failed := fItems[1].(map[string]any)
	assert.Equal(t, "failed", failed["result"])
	assert.Equal(t, "CERT_GET_FAILED: 云侧已不存在", failed["errorReason"], "失败条目记因（静态文案）")
	assert.NotContains(t, fItems[0], "errorReason", "首次导入成功条目无说明文案")
	fProg := final["progress"].(map[string]any)
	assert.Equal(t, float64(1), fProg["succeeded"])
	assert.Equal(t, float64(1), fProg["failed"])
}

func TestDiscoveryImportAPI_RequestValidation(t *testing.T) {
	engine, _ := newDiscoveryRouter(t)

	cases := []struct {
		name string
		body any
	}{
		{"empty items array", discoveryImportItems(nil)},
		{"missing items field", map[string]any{}},
		{"items not an array", map[string]any{"items": "not-an-array"}},
		{"item missing cloudCertId", discoveryImportItems([]map[string]string{
			{"cloud": "aliyun", "accountKey": "acct-a"}})},
		{"item blank accountKey", discoveryImportItems([]map[string]string{
			{"cloud": "aliyun", "accountKey": " ", "cloudCertId": "cert-A"}})},
	}
	for _, tc := range cases {
		t.Run(tc.name+" returns structured 400", func(t *testing.T) {
			w := doPostJSON(t, engine, "/api/v1/certs/discovery/import", tc.body)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			env := decode(t, w)
			require.NotNil(t, env.Error)
			assert.Equal(t, CodeInvalidRequest, env.Error.Code)
			assert.False(t, env.Success)
		})
	}
}

func TestDiscoveryImportAPI_ProgressLookup(t *testing.T) {
	engine, _ := newDiscoveryRouter(t)

	t.Run("unknown session returns 404 envelope", func(t *testing.T) {
		w := doGet(t, engine, "/api/v1/certs/discovery/import/"+primitive.NewObjectID().Hex())
		assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
		env := decode(t, w)
		require.NotNil(t, env.Error)
		assert.Equal(t, CodeNotFound, env.Error.Code)
	})

	t.Run("invalid session id returns 400 envelope", func(t *testing.T) {
		// 同时覆盖静态段共存：/discovery/import/... 不被 ledger /:id 吃掉
		w := doGet(t, engine, "/api/v1/certs/discovery/import/not-a-hex-id")
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		env := decode(t, w)
		require.NotNil(t, env.Error)
		assert.Equal(t, CodeInvalidID, env.Error.Code)
	})
}
