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
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// discoveryDeps 发现导入查询端点测试依赖（内存假实现句柄）。
type discoveryDeps struct {
	certs    *certtest.FakeCertificateRepo
	refs     *certtest.FakeCertReferenceRepo
	snaps    *certtest.FakeScanSnapshotRepo
	mappings *certtest.FakeCloudCertMappingRepo
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
	}
	importSvc := service.NewImportService(d.certs, certtest.NewFakeBatchSessionRepo(), certtest.NewTestCrypto(t))
	ledgerSvc := service.NewLedgerService(d.certs, d.refs, d.snaps)
	querySvc := service.NewReferenceQueryService(d.certs, d.refs, d.snaps, &fakeScanTrigger{})
	discSvc := service.NewDiscoveryPreviewService(d.snaps, d.refs, d.certs, d.mappings)
	dashH, settingsH := newDashboardSettingsHandlers(d.certs, d.refs, d.snaps)
	engine := gin.New()
	engine.Use(withRole(RoleOpsEngineer))
	RegisterRoutes(engine, NewCertHandler(importSvc), NewReferenceHandler(querySvc),
		NewDiscoveryHandler(discSvc), NewLedgerHandler(ledgerSvc),
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
			NewDiscoveryHandler(discSvc), NewLedgerHandler(service.NewLedgerService(certs, refs, snaps)),
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
