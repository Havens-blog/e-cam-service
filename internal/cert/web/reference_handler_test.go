package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeScanTrigger StartScan 端口假实现（可编程结果/错误 + 调用计数）。
type fakeScanTrigger struct {
	mu    sync.Mutex
	calls int
	res   service.ScanResult
	err   error
}

func (f *fakeScanTrigger) StartScan(_ context.Context) (service.ScanResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.res, f.err
}

func (f *fakeScanTrigger) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// newReferenceRouter 构造挂载全部 /api/v1/certs 路由的测试引擎
// （复用 ledgerDeps 播种夹具；覆盖 /reverse 静态段与 /:id 参数段共存）。
func newReferenceRouter(t *testing.T) (*gin.Engine, *ledgerDeps, *fakeScanTrigger) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	certs := certtest.NewFakeCertificateRepo()
	refs := certtest.NewFakeCertReferenceRepo()
	snaps := certtest.NewFakeScanSnapshotRepo()
	scan := &fakeScanTrigger{}
	importSvc := service.NewImportService(certs, certtest.NewFakeBatchSessionRepo(), certtest.NewTestCrypto(t))
	ledgerSvc := service.NewLedgerService(certs, refs, snaps)
	querySvc := service.NewReferenceQueryService(certs, refs, snaps, scan)
	dashH, settingsH := newDashboardSettingsHandlers(certs, refs, snaps)
	engine := gin.New()
	// 7.2 角色门卫全量接线后，引用面测试以运维工程师身份发起。
	engine.Use(withRole(RoleOpsEngineer))
	RegisterRoutes(engine, NewCertHandler(importSvc), NewReferenceHandler(querySvc),
		NewDiscoveryHandler(service.NewDiscoveryPreviewService(snaps, refs, certs, certtest.NewFakeCloudCertMappingRepo()), newDiscoveryImportSvcForRouter()),
		NewLedgerHandler(ledgerSvc), dashH, settingsH, newChangeHandlerFixture(t))
	return engine, &ledgerDeps{certs: certs, refs: refs, snaps: snaps}, scan
}

// seedDoneSnapshotAt 指定 startedAt 的成功快照（多快照时序控制）。
func (d *ledgerDeps) seedDoneSnapshotAt(t *testing.T, coverage []domain.CoverageMeta, startedAt time.Time) string {
	t.Helper()
	snap := &domain.ScanSnapshot{StartedAt: startedAt, CoverageMeta: coverage}
	id, err := d.snaps.Create(context.Background(), snap)
	require.NoError(t, err)
	require.NoError(t, d.snaps.MarkFinished(context.Background(), id, domain.ScanStatusDone, ""))
	return id
}

// seedRunningSnapshot 运行中快照（防重 409 附进行中快照信息夹具）。
func (d *ledgerDeps) seedRunningSnapshot(t *testing.T) string {
	t.Helper()
	id, err := d.snaps.Create(context.Background(), &domain.ScanSnapshot{})
	require.NoError(t, err)
	return id
}

// seedFullRef 全字段引用播种（cloud/product/cluster/namespace/kind/account）。
func (d *ledgerDeps) seedFullRef(t *testing.T, fingerprint, snapID string, mutate func(*domain.CertReference)) {
	t.Helper()
	r := domain.CertReference{
		CertFingerprint: fingerprint,
		Cloud:           domain.CloudAliyun,
		Product:         domain.ProductCDN,
		ResourceID:      fmt.Sprintf("res-%s", fingerprint[:6]),
		AccountKey:      "acct-main",
		SnapshotID:      snapID,
	}
	if mutate != nil {
		mutate(&r)
	}
	_, err := d.refs.CreateMulti(context.Background(), []domain.CertReference{r})
	require.NoError(t, err)
}

func doPost(t *testing.T, engine *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
	return w
}

// ---------------------------------------------------------------------
// AC1：GET /api/v1/certs/:id/references 分组+覆盖率+盲区声明
// ---------------------------------------------------------------------

func TestReferencesAPIHasRefs(t *testing.T) {
	engine, d, _ := newReferenceRouter(t)
	id := d.seedCert(t, lfp(1), nil)
	snapID := d.seedDoneSnapshot(t, []domain.CoverageMeta{
		{Cloud: "aliyun", Product: "cdn", Covered: 2, Total: 5},
		{Cloud: "tencent", Product: "waf", Covered: 1, Total: -1},
		{Cloud: "", Product: "crd", Covered: 1, Total: -1},
	})
	d.seedFullRef(t, lfp(1), snapID, func(r *domain.CertReference) {
		r.ResourceID, r.ReferencedCloudCertID = "cdn-res-1", "aliyun-cert-1"
	})
	d.seedFullRef(t, lfp(1), snapID, func(r *domain.CertReference) {
		r.ResourceID, r.ReferencedCloudCertID = "cdn-res-2", "aliyun-cert-1"
	})
	d.seedFullRef(t, lfp(1), snapID, func(r *domain.CertReference) {
		r.Cloud, r.Product = domain.CloudTencent, domain.ProductWAF
		r.ResourceID, r.ReferencedCloudCertID = "waf-res-1", "tencent-cert-9"
	})
	d.seedFullRef(t, lfp(1), snapID, func(r *domain.CertReference) {
		r.Cloud, r.Product = "", domain.ProductCRD
		r.ClusterID, r.Namespace, r.Kind = "cluster-a", "istio-system", "Gateway"
		r.ResourceID, r.ReferencedCloudCertID = "gw-1", "cloud-cert-x"
	})

	w := doGet(t, engine, "/api/v1/certs/"+id+"/references")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	data := decodeData(t, w)

	assert.Equal(t, "has_refs", data["referenceStatus"])
	assert.Equal(t, float64(4), data["refCount"])
	assert.Equal(t, id, data["certId"])
	assert.Equal(t, lfp(1), data["fingerprint"])
	assert.NotEmpty(t, data["lastScanAt"], "附最近扫描时间")
	assert.NotEmpty(t, data["snapshotId"])

	// 分组：云/产品/集群三键分组，字典序稳定
	groups, ok := data["groups"].([]any)
	require.True(t, ok, "groups 为数组")
	require.Len(t, groups, 3)
	g0 := groups[0].(map[string]any)
	assert.Equal(t, "aliyun", g0["cloud"])
	assert.Equal(t, "cdn", g0["product"])
	g0Refs := g0["references"].([]any)
	require.Len(t, g0Refs, 2)
	item := g0Refs[0].(map[string]any)
	for _, field := range []string{"resourceId", "referencedCloudCertId", "accountKey"} {
		assert.Contains(t, item, field, "云引用项缺字段 %s", field)
	}
	// K8s 分组：clusterId/namespace/kind
	g2 := groups[2].(map[string]any)
	assert.Equal(t, "crd", g2["product"])
	assert.Equal(t, "cluster-a", g2["clusterId"])
	k8sItem := g2["references"].([]any)[0].(map[string]any)
	assert.Equal(t, "istio-system", k8sItem["namespace"])
	assert.Equal(t, "Gateway", k8sItem["kind"])

	// 覆盖率：total=-1 → 分母不可用标记
	coverage := data["coverage"].([]any)
	require.Len(t, coverage, 3)
	c0 := coverage[0].(map[string]any)
	assert.Equal(t, float64(5), c0["total"])
	assert.Equal(t, true, c0["denominatorAvailable"])
	c1 := coverage[1].(map[string]any)
	assert.Equal(t, float64(-1), c1["total"])
	assert.Equal(t, false, c1["denominatorAvailable"])
	note, _ := c1["denominatorNote"].(string)
	assert.Contains(t, note, "分母不可用", "total=-1 输出分母不可用标记")

	// Hard Rule：覆盖边界声明不可省略
	boundary, _ := data["coverageBoundary"].(string)
	assert.Contains(t, boundary, "本视图不含 VM Nginx 配置级引用")

	assertNoKeyMaterial(t, w)
}

// ---------------------------------------------------------------------
// AC2：三分支 referenceStatus（no_refs_scanned ≠ blind_spot，附扫描时间）
// ---------------------------------------------------------------------

func TestReferencesAPIStatusBranches(t *testing.T) {
	t.Run("no_refs_scanned with last scan time", func(t *testing.T) {
		engine, d, _ := newReferenceRouter(t)
		id := d.seedCert(t, lfp(1), nil)
		d.seedDoneSnapshot(t, []domain.CoverageMeta{{Cloud: "aliyun", Product: "cdn", Covered: 0, Total: 5}})

		w := doGet(t, engine, "/api/v1/certs/"+id+"/references")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		data := decodeData(t, w)
		assert.Equal(t, "no_refs_scanned", data["referenceStatus"], "已扫描无匹配 ≠ 无引用")
		assert.Equal(t, float64(0), data["refCount"])
		assert.NotEmpty(t, data["lastScanAt"], "未发现引用附最近扫描时间")
		assert.Empty(t, data["groups"].([]any))
		boundary, _ := data["coverageBoundary"].(string)
		assert.Contains(t, boundary, "本视图不含 VM Nginx 配置级引用")
	})

	t.Run("blind_spot no snapshot", func(t *testing.T) {
		engine, d, _ := newReferenceRouter(t)
		id := d.seedCert(t, lfp(1), nil) // 无任何成功快照

		w := doGet(t, engine, "/api/v1/certs/"+id+"/references")
		require.Equal(t, http.StatusOK, w.Code)
		data := decodeData(t, w)
		assert.Equal(t, "blind_spot", data["referenceStatus"])
		assert.Nil(t, data["lastScanAt"], "无扫描 → lastScanAt=null，与 no_refs_scanned 区分")
		reason, _ := data["reason"].(string)
		assert.Contains(t, reason, "snapshot")
		boundary, _ := data["coverageBoundary"].(string)
		assert.Contains(t, boundary, "本视图不含 VM Nginx 配置级引用")
	})

	t.Run("blind_spot scope uncovered", func(t *testing.T) {
		engine, d, _ := newReferenceRouter(t)
		id := d.seedCert(t, lfp(1), nil)
		// 旧快照：该指纹有 huawei/cdn 历史引用；新快照范围仅 aliyun/cdn
		oldSnap := d.seedDoneSnapshotAt(t, nil, time.Now().Add(-2*time.Hour))
		d.seedFullRef(t, lfp(1), oldSnap, func(r *domain.CertReference) {
			r.Cloud, r.Product = domain.CloudHuawei, domain.ProductCDN
		})
		d.seedDoneSnapshotAt(t, []domain.CoverageMeta{{Cloud: "aliyun", Product: "cdn", Covered: 0, Total: 3}},
			time.Now().Add(-1*time.Hour))

		w := doGet(t, engine, "/api/v1/certs/"+id+"/references")
		require.Equal(t, http.StatusOK, w.Code)
		data := decodeData(t, w)
		assert.Equal(t, "blind_spot", data["referenceStatus"], "涉及云/产品未纳入扫描范围")
		reason, _ := data["reason"].(string)
		assert.Contains(t, reason, "huawei/cdn")
	})

	t.Run("not found and invalid id", func(t *testing.T) {
		engine, _, _ := newReferenceRouter(t)
		w := doGet(t, engine, "/api/v1/certs/000000000000000000000000/references")
		assert.Equal(t, http.StatusNotFound, w.Code)
		env := decode(t, w)
		require.NotNil(t, env.Error)
		assert.Equal(t, CodeNotFound, env.Error.Code)

		w2 := doGet(t, engine, "/api/v1/certs/not-hex/references")
		assert.Equal(t, http.StatusBadRequest, w2.Code)
		env2 := decode(t, w2)
		assert.Equal(t, CodeInvalidID, env2.Error.Code)
	})
}

// ---------------------------------------------------------------------
// AC3：GET /api/v1/certs/reverse?domain= 反向查询
// ---------------------------------------------------------------------

func TestReverseQueryAPI(t *testing.T) {
	engine, d, _ := newReferenceRouter(t)
	// 同一域名两张证书（多证书并存）
	d.seedCert(t, lfp(1), func(c *domain.Certificate) { c.Sans = []string{"dual.example.com"} })
	d.seedCert(t, lfp(2), func(c *domain.Certificate) { c.Sans = []string{"dual.example.com"} })
	// 通配符证书
	d.seedCert(t, lfp(3), func(c *domain.Certificate) { c.Sans = []string{"*.wild.example.com"} })
	snapID := d.seedDoneSnapshot(t, nil)
	d.seedFullRef(t, lfp(1), snapID, func(r *domain.CertReference) {
		r.Cloud, r.Product, r.ResourceID = domain.CloudAliyun, domain.ProductCDN, "cdn-res-a"
	})
	d.seedFullRef(t, lfp(2), snapID, func(r *domain.CertReference) {
		r.Cloud, r.Product, r.ResourceID = domain.CloudTencent, domain.ProductWAF, "waf-res-b"
	})
	d.seedFullRef(t, lfp(3), snapID, func(r *domain.CertReference) {
		r.Cloud, r.Product, r.ResourceID = domain.CloudHuawei, domain.ProductCDN, "hw-res-w"
	})
	// 未登记指纹（扫描发现、台账未登记）
	d.seedFullRef(t, lfp(90), snapID, func(r *domain.CertReference) {
		r.Cloud, r.Product, r.ResourceID = domain.CloudAWS, domain.ProductCDN, "aws-res-x"
	})

	t.Run("same domain two certs separated by fingerprint", func(t *testing.T) {
		w := doGet(t, engine, "/api/v1/certs/reverse?domain=dual.example.com")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		data := decodeData(t, w)
		items, ok := data["items"].([]any)
		require.True(t, ok)
		require.Len(t, items, 2, "两张证书并存")

		byFP := map[string]map[string]any{}
		for _, raw := range items {
			it := raw.(map[string]any)
			byFP[it["fingerprint"].(string)] = it
		}
		// Hard Rule：按指纹严格区分，各自引用互不混淆
		fp1 := byFP[lfp(1)]
		require.NotNil(t, fp1)
		assert.Equal(t, true, fp1["registered"])
		assert.Equal(t, float64(1), fp1["referenceCount"])
		ref1 := fp1["references"].([]any)[0].(map[string]any)
		assert.Equal(t, "cdn-res-a", ref1["resourceId"])
		assert.Equal(t, "aliyun", ref1["cloud"])

		fp2 := byFP[lfp(2)]
		require.NotNil(t, fp2)
		ref2 := fp2["references"].([]any)[0].(map[string]any)
		assert.Equal(t, "waf-res-b", ref2["resourceId"])
		assert.Equal(t, "tencent", ref2["cloud"])
	})

	t.Run("wildcard SAN covers single-label subdomain", func(t *testing.T) {
		w := doGet(t, engine, "/api/v1/certs/reverse?domain=a.wild.example.com")
		require.Equal(t, http.StatusOK, w.Code)
		data := decodeData(t, w)
		items := data["items"].([]any)
		require.Len(t, items, 1)
		assert.Equal(t, lfp(3), items[0].(map[string]any)["fingerprint"])

		// 深层子域名不命中单标签通配符
		w2 := doGet(t, engine, "/api/v1/certs/reverse?domain=b.a.wild.example.com")
		require.Equal(t, http.StatusOK, w2.Code)
		assert.Empty(t, decodeData(t, w2)["items"].([]any))
	})

	t.Run("resource name lookup with unregistered fingerprint", func(t *testing.T) {
		w := doGet(t, engine, "/api/v1/certs/reverse?domain=aws-res-x")
		require.Equal(t, http.StatusOK, w.Code)
		data := decodeData(t, w)
		items := data["items"].([]any)
		require.Len(t, items, 1)
		it := items[0].(map[string]any)
		assert.Equal(t, lfp(90), it["fingerprint"])
		assert.Equal(t, false, it["registered"], "扫描发现未登记指纹 → registered=false")
		assert.Empty(t, it["certId"])
	})

	t.Run("no match returns empty list not error", func(t *testing.T) {
		w := doGet(t, engine, "/api/v1/certs/reverse?domain=missing.example.com")
		require.Equal(t, http.StatusOK, w.Code, "无匹配 → 空列表（区别于错误）")
		env := decode(t, w)
		assert.True(t, env.Success)
		data := decodeData(t, w)
		items, ok := data["items"].([]any)
		require.True(t, ok, "items 为空数组而非 null")
		assert.Empty(t, items)
	})

	t.Run("missing domain param 400", func(t *testing.T) {
		w := doGet(t, engine, "/api/v1/certs/reverse")
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decode(t, w)
		require.NotNil(t, env.Error)
		assert.Equal(t, CodeInvalidRequest, env.Error.Code)
		assert.False(t, env.Success, "错误信封 success=false")
		assert.Equal(t, "INVALID_REQUEST", env.Error.Code)
		assert.NotEmpty(t, env.Error.Message)
	})

	t.Run("reverse route not swallowed by :id", func(t *testing.T) {
		// Gin 静态段/参数段共存：/reverse 不得被 /:id 吃掉
		w := doGet(t, engine, "/api/v1/certs/reverse?domain=dual.example.com")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.NotContains(t, strings.ToLower(w.Body.String()), "not found")
	})
}

// ---------------------------------------------------------------------
// AC4/AC5：POST /api/v1/certs/:id/scan 立即扫描 + 防重 409
// ---------------------------------------------------------------------

func TestTriggerScanAPI(t *testing.T) {
	t.Run("trigger ok", func(t *testing.T) {
		engine, d, scan := newReferenceRouter(t)
		id := d.seedCert(t, lfp(1), nil)
		scan.res = service.ScanResult{
			SnapshotID: "snap-1", Status: domain.ScanStatusDone, ReferencesWritten: 3,
			ChannelsAttempted: 2, ChannelsFailed: 0,
			CoverageMeta: []domain.CoverageMeta{{Cloud: "aliyun", Product: "cdn", Covered: 3, Total: 5}},
		}

		w := doPost(t, engine, "/api/v1/certs/"+id+"/scan")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		data := decodeData(t, w)
		assert.Equal(t, "snap-1", data["snapshotId"])
		assert.Equal(t, "done", data["status"])
		assert.Equal(t, float64(3), data["referencesWritten"])
		assert.Equal(t, float64(2), data["channelsAttempted"])
		cov := data["coverage"].([]any)[0].(map[string]any)
		assert.Equal(t, true, cov["denominatorAvailable"])
		assert.Equal(t, 1, scan.callCount(), "触发一次扫描")
		assertNoKeyMaterial(t, w)
	})

	t.Run("in progress 409 with snapshot info", func(t *testing.T) {
		engine, d, scan := newReferenceRouter(t)
		id := d.seedCert(t, lfp(1), nil)
		runningID := d.seedRunningSnapshot(t)
		scan.err = fmt.Errorf("%w", domain.ErrScanInProgress)

		w := doPost(t, engine, "/api/v1/certs/"+id+"/scan")
		assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		env := decode(t, w)
		require.NotNil(t, env.Error)
		assert.Equal(t, "SCAN_IN_PROGRESS", env.Error.Code)
		assert.False(t, env.Success, "错误信封 success=false")
		assert.NotEmpty(t, env.Error.Message)

		var meta map[string]any
		require.NoError(t, json.Unmarshal(env.Meta, &meta))
		assert.Equal(t, runningID, meta["snapshotId"], "附进行中快照信息")
		startedAt, _ := meta["startedAt"].(string)
		assert.NotEmpty(t, startedAt)
	})

	t.Run("cert not found scan not triggered", func(t *testing.T) {
		engine, _, scan := newReferenceRouter(t)
		w := doPost(t, engine, "/api/v1/certs/000000000000000000000000/scan")
		assert.Equal(t, http.StatusNotFound, w.Code)
		env := decode(t, w)
		assert.Equal(t, CodeNotFound, env.Error.Code)
		assert.Equal(t, 0, scan.callCount(), "证书不存在不触发扫描")
	})

	t.Run("invalid id 400", func(t *testing.T) {
		engine, _, _ := newReferenceRouter(t)
		w := doPost(t, engine, "/api/v1/certs/bad-id/scan")
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decode(t, w)
		assert.Equal(t, CodeInvalidID, env.Error.Code)
	})

	t.Run("scan service error 500 envelope", func(t *testing.T) {
		engine, d, scan := newReferenceRouter(t)
		id := d.seedCert(t, lfp(1), nil)
		scan.err = fmt.Errorf("boom")

		w := doPost(t, engine, "/api/v1/certs/"+id+"/scan")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		env := decode(t, w)
		require.NotNil(t, env.Error)
		assert.Equal(t, "INTERNAL_ERROR", env.Error.Code)
		assert.False(t, env.Success)
	})
}
