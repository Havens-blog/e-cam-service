package web

import (
	"bytes"
	"context"
	"encoding/json"

	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ---------------------------------------------------------------------
// 变更管理端点测试（任务 5.11）：
// AC-1 列表状态 Tab 筛选 + POST 生成清单（四阻断 409 + ChangeList 结构）
// AC-2 confirm（batchConf 越界 400）/execute/confirm-batch（409）/cancel（409）
// AC-3 progress 逐项状态轮询（itemStates + error + 批次）
// AC-4 rollback 仅成功项 / 无效目标 409 ROLLBACK_TARGET_INVALID（转人工提示）
// AC-5 audit 按单查询（7 类 action 全覆盖，与 ChangeReport 可比对）
// AC-6 终态 GET /:id 返回 ChangeReport 全字段；响应无私钥/密文/凭证字段
// ---------------------------------------------------------------------

// 测试指纹常量（64 位 hex）。
const (
	chgOldFP = "aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa"
	chgNewFP = "bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb"
)

// fakeChangeAuditSource 审计流水读取端口假实现（单订单场景：注入即返回）。
type fakeChangeAuditSource struct{ logs []service.ChangeAuditLog }

func (f *fakeChangeAuditSource) ListByOrder(_ context.Context, _ string) ([]service.ChangeAuditLog, error) {
	return append([]service.ChangeAuditLog(nil), f.logs...), nil
}

// fakeChangeUnmetSource 未达标清单读取端口假实现。
type fakeChangeUnmetSource struct{ domains []string }

func (f *fakeChangeUnmetSource) ListUnmetDomains(_ context.Context, _ string) ([]string, error) {
	return f.domains, nil
}

// fakeChangeOrphanCleanupSource 孤儿清理结果读取端口假实现。
type fakeChangeOrphanCleanupSource struct{ results []service.OrphanCleanupResult }

func (f *fakeChangeOrphanCleanupSource) ListOrphanCleanup(_ context.Context, _ string) ([]service.OrphanCleanupResult, error) {
	return f.results, nil
}

// fakeChangeCreds 凭证来源端口假实现（返回占位 kubeconfig 凭证，不触达外部）。
type fakeChangeCreds struct{}

func (fakeChangeCreds) CloudCredential(_ context.Context, cloud, _ string) (deployer.Credential, error) {
	return deployer.Credential{Kind: "cloud_ak", Cloud: cloud, AccountKey: "test", AccessKey: "ak", Secret: []byte("sk")}, nil
}

func (fakeChangeCreds) K8sCredential(_ context.Context, _ string) (deployer.Credential, error) {
	return deployer.Credential{Kind: "kubeconfig", Secret: []byte("kubeconfig")}, nil
}

// changeWebDeps 变更管理端点测试依赖聚合。
type changeWebDeps struct {
	orders   *certtest.FakeChangeOrderRepo
	items    *certtest.FakeChangeItemRepo
	certs    *certtest.FakeCertificateRepo
	alertCfg *certtest.FakeAlertConfigRepo
	snaps    *certtest.FakeScanSnapshotRepo
	refs     *certtest.FakeCertReferenceRepo
	probes   *certtest.FakeProbeResultRepo

	audit  *fakeChangeAuditSource
	unmet  *fakeChangeUnmetSource
	orphan *fakeChangeOrphanCleanupSource
	k8s    *deployer.SimulatedChannel

	auditWriter *fakeChangeAuditWriter // 7.2 订单生命周期审计写入（handler 注入）
}

// fakeChangeAuditWriter 审计写入端口假实现（记录追加事件）。
type fakeChangeAuditWriter struct{ events []service.ChangeAuditEvent }

func (f *fakeChangeAuditWriter) WriteChangeAudit(_ context.Context, e service.ChangeAuditEvent) error {
	f.events = append(f.events, e)
	return nil
}

// newChangeHandlerFixture 构造独立 fakes 上的变更 handler（既有测试路由
// 挂载复用：不与被测夹具共享状态，同 newDashboardSettingsHandlers 口径）。
func newChangeHandlerFixture(t *testing.T) *ChangeHandler {
	t.Helper()
	orders := certtest.NewFakeChangeOrderRepo()
	items := certtest.NewFakeChangeItemRepo()
	certs := certtest.NewFakeCertificateRepo()
	alertCfg := certtest.NewFakeAlertConfigRepo()
	snaps := certtest.NewFakeScanSnapshotRepo()
	refs := certtest.NewFakeCertReferenceRepo()
	changeSvc := service.NewChangeService(orders, items, certs, alertCfg, snaps, refs, nil)
	execSvc := service.NewChangeExecuteService(orders, items, certs, alertCfg, snaps, refs, nil, nil, nil, nil, nil, nil, nil)
	rbSvc := service.NewChangeRollbackService(orders, items, certs, alertCfg, nil, nil, nil, nil, nil, nil)
	querySvc := service.NewChangeQueryService(orders, items, snaps, certtest.NewFakeProbeResultRepo(), alertCfg, nil, nil, nil)
	return NewChangeHandler(querySvc, changeSvc, execSvc, rbSvc, nil)
}

// newChangeRouter 构造挂载全部 /api/v1/certs 路由的测试引擎（变更面以真实
// service + certtest fakes 装配；role 非空时注入角色中间件）。
func newChangeRouter(t *testing.T, role Role) (*gin.Engine, *changeWebDeps) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	d := &changeWebDeps{
		orders:      certtest.NewFakeChangeOrderRepo(),
		items:       certtest.NewFakeChangeItemRepo(),
		certs:       certtest.NewFakeCertificateRepo(),
		alertCfg:    certtest.NewFakeAlertConfigRepo(),
		snaps:       certtest.NewFakeScanSnapshotRepo(),
		refs:        certtest.NewFakeCertReferenceRepo(),
		probes:      certtest.NewFakeProbeResultRepo(),
		audit:       &fakeChangeAuditSource{},
		unmet:       &fakeChangeUnmetSource{},
		orphan:      &fakeChangeOrphanCleanupSource{},
		k8s:         deployer.NewSimulatedChannel(deployer.ChannelTypeK8sAPI),
		auditWriter: &fakeChangeAuditWriter{},
	}
	changeSvc := service.NewChangeService(d.orders, d.items, d.certs, d.alertCfg, d.snaps, d.refs, nil)
	execSvc := service.NewChangeExecuteService(d.orders, d.items, d.certs, d.alertCfg,
		d.snaps, d.refs, []deployer.ExecutionChannel{d.k8s}, fakeChangeCreds{}, nil, nil, nil, nil, nil)
	rbSvc := service.NewChangeRollbackService(d.orders, d.items, d.certs, d.alertCfg,
		nil, []deployer.ExecutionChannel{d.k8s}, nil, fakeChangeCreds{}, nil, nil)
	querySvc := service.NewChangeQueryService(d.orders, d.items, d.snaps, d.probes,
		d.alertCfg, d.unmet, d.orphan, d.audit)
	changeH := NewChangeHandler(querySvc, changeSvc, execSvc, rbSvc, d.auditWriter)

	engine := gin.New()
	if role != "" {
		engine.Use(withRole(role))
	}
	importSvc := service.NewImportService(d.certs, certtest.NewFakeBatchSessionRepo(), certtest.NewTestCrypto(t))
	ledgerSvc := service.NewLedgerService(d.certs, d.refs, d.snaps)
	queryRefSvc := service.NewReferenceQueryService(d.certs, d.refs, d.snaps, &fakeScanTrigger{})
	dashH, settingsH := newDashboardSettingsHandlers(d.certs, d.refs, d.snaps)
	RegisterRoutes(engine, NewCertHandler(importSvc), NewReferenceHandler(queryRefSvc),
		NewDiscoveryHandler(service.NewDiscoveryPreviewService(d.snaps, d.refs, d.certs, certtest.NewFakeCloudCertMappingRepo()), newDiscoveryImportSvcForRouter()),
		NewLedgerHandler(ledgerSvc), dashH, settingsH, changeH)
	return engine, d
}

// ---- 播种 helpers ----

// seedCert 写入台账证书，返回 ID（hex）。
func (d *changeWebDeps) seedCert(t *testing.T, fp string, sans []string, hosting domain.HostingStatus) string {
	t.Helper()
	c := &domain.Certificate{Fingerprint: fp, Sans: sans, HostingStatus: hosting}
	require.NoError(t, d.certs.Create(context.Background(), c))
	return c.ID.Hex()
}

// seedDoneSnapshot 写入成功快照（startedAt=now-age），返回快照 ID。
func (d *changeWebDeps) seedDoneSnapshot(t *testing.T, age time.Duration) string {
	t.Helper()
	snap := &domain.ScanSnapshot{StartedAt: time.Now().Add(-age), Status: domain.ScanStatusDone}
	id, err := d.snaps.Create(context.Background(), snap)
	require.NoError(t, err)
	return id
}

// seedCloudRef 写入快照内云引用。
func (d *changeWebDeps) seedCloudRef(t *testing.T, snapshotID, fp, resourceID string) {
	t.Helper()
	_, err := d.refs.CreateMulti(context.Background(), []domain.CertReference{{
		CertFingerprint: fp, Cloud: domain.CloudAliyun, Product: domain.ProductCDN,
		AccountKey: "acct-1", ResourceID: resourceID, ReferencedCloudCertID: "old-cloud-cert",
		SnapshotID: snapshotID,
	}})
	require.NoError(t, err)
}

// seedOrder 写入订单（status/activeMutex 由调用方控制），返回 ID（hex）。
func (d *changeWebDeps) seedOrder(t *testing.T, status domain.ChangeStatus, batch *domain.BatchInfo, expected *domain.VerifyExpected) string {
	t.Helper()
	o := &domain.ChangeOrder{
		OldCertFingerprint: chgOldFP,
		NewCertID:          "new-cert-id",
		Status:             status,
		SnapshotID:         "snap-seeded",
		BatchInfo:          batch,
		VerifyExpected:     expected,
	}
	if domain.IsActiveChangeStatus(status) {
		o.ActiveMutex = chgOldFP
	}
	id, err := d.orders.Create(context.Background(), o)
	require.NoError(t, err)
	return id
}

// seedItem 写入变更项（预生成 ObjectID 保证可引用），返回项 ID（hex）。
func (d *changeWebDeps) seedItem(t *testing.T, orderID string, status domain.ChangeItemStatus, errMsg string) string {
	t.Helper()
	it := domain.ChangeItem{
		ID:          primitive.NewObjectID(),
		OrderID:     orderID,
		Action:      domain.ActionUploadAndBind,
		ResourceRef: domain.ResourceRef{Channel: domain.ChannelCloudAPI, Cloud: "aliyun", Product: "cdn", AccountKey: "acct-1", ResourceID: "res-" + primitive.NewObjectID().Hex()[:6]},
		Status:      status,
		Error:       errMsg,
	}
	_, err := d.items.CreateMulti(context.Background(), []domain.ChangeItem{it})
	require.NoError(t, err)
	return it.ID.Hex()
}

// doPostJSON 发起 JSON POST 请求。
func doPostJSON(t *testing.T, engine *gin.Engine, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw)))
	return w
}

// decodeInto 将信封 data 解码为目标结构。
func decodeInto(t *testing.T, w *httptest.ResponseRecorder, target any) {
	t.Helper()
	env := decode(t, w)
	require.NoError(t, json.Unmarshal(env.Data, target), "data: %s", env.Data)
}

// ---- AC-1：列表 + 生成清单 ----

func TestChangeList_StatusTabFilter(t *testing.T) {
	engine, d := newChangeRouter(t, RoleOpsEngineer)
	d.seedOrder(t, domain.ChangeStatusVerifying, nil, nil)
	d.seedOrder(t, domain.ChangeStatusCompleted, nil, nil)
	d.seedOrder(t, domain.ChangeStatusCompleted, nil, nil)

	// 全量
	w := doGet(t, engine, "/api/v1/certs/changes")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	env := decode(t, w)
	var all []map[string]any
	require.NoError(t, json.Unmarshal(env.Data, &all))
	assert.Len(t, all, 3)
	var meta struct {
		Total    int64 `json:"total"`
		Page     int   `json:"page"`
		PageSize int   `json:"pageSize"`
	}
	require.NoError(t, json.Unmarshal(env.Meta, &meta))
	assert.EqualValues(t, 3, meta.Total)
	assert.Equal(t, 1, meta.Page)
	assert.Equal(t, 20, meta.PageSize)

	// 状态 Tab 筛选
	w = doGet(t, engine, "/api/v1/certs/changes?status=completed")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	env = decode(t, w)
	var filtered []map[string]any
	require.NoError(t, json.Unmarshal(env.Data, &filtered))
	require.Len(t, filtered, 2)
	for _, o := range filtered {
		assert.Equal(t, "completed", o["status"])
		assert.NotEmpty(t, o["id"])
		assert.NotEmpty(t, o["oldFingerprint"])
	}

	// 非法状态 400
	w = doGet(t, engine, "/api/v1/certs/changes?status=running")
	require.Equal(t, http.StatusBadRequest, w.Code)
	env = decode(t, w)
	require.NotNil(t, env.Error)
	assert.Equal(t, CodeInvalidRequest, env.Error.Code)
}

func TestChangeList_Roles(t *testing.T) {
	// 7.2 AC"运维主管/审计=审计+配置+变更查看"：审计角色对列表/详情放行
	//（5.11 契约演进，api-handbook Auth 列含主管/审计阅单）。
	for _, tc := range []struct {
		role      Role
		status    int
		forbidden bool
	}{
		{RoleOpsEngineer, http.StatusOK, false},
		{RoleOpsSupervisor, http.StatusOK, false},
		{RoleAuditor, http.StatusOK, false},
		{RoleViewer, http.StatusForbidden, true},
	} {
		engine, _ := newChangeRouter(t, tc.role)
		w := doGet(t, engine, "/api/v1/certs/changes")
		assert.Equal(t, tc.status, w.Code, "role=%s: %s", tc.role, w.Body.String())
		if tc.forbidden {
			env := decode(t, w)
			require.NotNil(t, env.Error)
			assert.Equal(t, CodeForbidden, env.Error.Code)
		}
	}
}

func TestGenerateChangeList_Success(t *testing.T) {
	engine, d := newChangeRouter(t, RoleOpsEngineer)
	d.seedCert(t, chgOldFP, []string{"a.example.com", "b.example.com"}, domain.HostingStatusComplete)
	newID := d.seedCert(t, chgNewFP, []string{"a.example.com", "b.example.com", "c.example.com"}, domain.HostingStatusComplete)
	snapID := d.seedDoneSnapshot(t, 2*time.Hour)
	d.seedCloudRef(t, snapID, chgOldFP, "res-1")
	d.seedCloudRef(t, snapID, chgOldFP, "res-2")

	w := doPostJSON(t, engine, "/api/v1/certs/changes", GenerateChangeRequest{
		OldFingerprint: chgOldFP, NewCertID: newID,
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var list changeListVO
	decodeInto(t, w, &list)
	assert.NotEmpty(t, list.OrderID)
	assert.Equal(t, chgOldFP, list.OldFingerprint)
	assert.Equal(t, newID, list.NewCertID)
	assert.Equal(t, snapID, list.SnapshotID)
	assert.True(t, list.SANCheck.Passed)
	require.Len(t, list.Items, 2)
	assert.Equal(t, "upload_and_bind", list.Items[0].Action)
	assert.True(t, list.Items[0].AutoChangeable)
	assert.Equal(t, "cloud_api", list.Items[0].Target.Channel)
	assert.Equal(t, "res-1", list.Items[0].Target.ResourceID)
	assert.NotEmpty(t, list.Warnings) // 覆盖边界等盲区声明
	assertNoKeyMaterial(t, w)

	// 7.2：成功生成后追加 create 审计事件（订单级、含旧/新指纹与项数）。
	require.Len(t, d.auditWriter.events, 1, "create 审计事件应恰好一条")
	ev := d.auditWriter.events[0]
	assert.Equal(t, list.OrderID, ev.OrderID)
	assert.Equal(t, service.AuditActionCreate, ev.Action)
	assert.Empty(t, ev.ItemID, "create 为订单级事件")
	assert.Contains(t, ev.Detail, chgOldFP)
	assert.Contains(t, ev.Detail, newID)
	assert.False(t, ev.At.IsZero())
}

func TestGenerateChangeList_BlockingErrors(t *testing.T) {
	seedValid := func(d *changeWebDeps) (string, string) {
		d.seedCert(t, chgOldFP, []string{"a.example.com"}, domain.HostingStatusComplete)
		newID := d.seedCert(t, chgNewFP, []string{"a.example.com"}, domain.HostingStatusComplete)
		snapID := d.seedDoneSnapshot(t, 2*time.Hour)
		d.seedCloudRef(t, snapID, chgOldFP, "res-1")
		return newID, snapID
	}
	cases := []struct {
		name string
		code string
		prep func(t *testing.T, engine *gin.Engine, d *changeWebDeps) GenerateChangeRequest
	}{
		{
			name: "SCAN_STALE：快照超新鲜度阈值",
			code: domain.CodeScanStale,
			prep: func(t *testing.T, _ *gin.Engine, d *changeWebDeps) GenerateChangeRequest {
				d.seedCert(t, chgOldFP, []string{"a.example.com"}, domain.HostingStatusComplete)
				newID := d.seedCert(t, chgNewFP, []string{"a.example.com"}, domain.HostingStatusComplete)
				snapID := d.seedDoneSnapshot(t, 48*time.Hour)
				d.seedCloudRef(t, snapID, chgOldFP, "res-1")
				return GenerateChangeRequest{OldFingerprint: chgOldFP, NewCertID: newID}
			},
		},
		{
			name: "CHANGE_IN_FLIGHT：同指纹在途单",
			code: domain.CodeChangeInFlight,
			prep: func(t *testing.T, _ *gin.Engine, d *changeWebDeps) GenerateChangeRequest {
				newID, _ := seedValid(d)
				d.seedOrder(t, domain.ChangeStatusPendingConfirm, nil, nil)
				return GenerateChangeRequest{OldFingerprint: chgOldFP, NewCertID: newID}
			},
		},
		{
			name: "NEW_CERT_FINGERPRINT_ONLY：新证书仅指纹登记",
			code: domain.CodeNewCertFingerprintOnly,
			prep: func(t *testing.T, _ *gin.Engine, d *changeWebDeps) GenerateChangeRequest {
				d.seedCert(t, chgOldFP, []string{"a.example.com"}, domain.HostingStatusComplete)
				newID := d.seedCert(t, chgNewFP, []string{"a.example.com"}, domain.HostingStatusFingerprintOnly)
				snapID := d.seedDoneSnapshot(t, 2*time.Hour)
				d.seedCloudRef(t, snapID, chgOldFP, "res-1")
				return GenerateChangeRequest{OldFingerprint: chgOldFP, NewCertID: newID}
			},
		},
		{
			name: "SAN_INSUFFICIENT：新证书 SAN 缺失目标域名",
			code: domain.CodeSanInsufficient,
			prep: func(t *testing.T, _ *gin.Engine, d *changeWebDeps) GenerateChangeRequest {
				d.seedCert(t, chgOldFP, []string{"a.example.com", "b.example.com"}, domain.HostingStatusComplete)
				newID := d.seedCert(t, chgNewFP, []string{"a.example.com"}, domain.HostingStatusComplete)
				snapID := d.seedDoneSnapshot(t, 2*time.Hour)
				d.seedCloudRef(t, snapID, chgOldFP, "res-1")
				return GenerateChangeRequest{OldFingerprint: chgOldFP, NewCertID: newID}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine, d := newChangeRouter(t, RoleOpsEngineer)
			req := tc.prep(t, engine, d)
			w := doPostJSON(t, engine, "/api/v1/certs/changes", req)
			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			env := decode(t, w)
			require.NotNil(t, env.Error)
			assert.Equal(t, tc.code, env.Error.Code)
			assert.False(t, env.Success)
			// 7.2：阻断路径不追加审计事件（仅成功受理的操作入流水）。
			assert.Empty(t, d.auditWriter.events)
		})
	}
}

func TestGenerateChangeList_BadRequest(t *testing.T) {
	engine, _ := newChangeRouter(t, RoleOpsEngineer)
	w := doPostJSON(t, engine, "/api/v1/certs/changes", GenerateChangeRequest{NewCertID: "x"})
	require.Equal(t, http.StatusBadRequest, w.Code)
	env := decode(t, w)
	assert.Equal(t, CodeInvalidRequest, env.Error.Code)

	w = doPostJSON(t, engine, "/api/v1/certs/changes", map[string]any{"oldFingerprint": 123})
	require.Equal(t, http.StatusBadRequest, w.Code)

	// viewer 不可生成
	engine, _ = newChangeRouter(t, RoleViewer)
	w = doPostJSON(t, engine, "/api/v1/certs/changes", GenerateChangeRequest{OldFingerprint: chgOldFP, NewCertID: "x"})
	require.Equal(t, http.StatusForbidden, w.Code)
}

// ---- AC-2：confirm / execute / confirm-batch / cancel ----

func TestConfirm_BatchConfValidation(t *testing.T) {
	engine, d := newChangeRouter(t, RoleOpsEngineer)
	// 经生成清单路径造一张合法 pending_confirm 单（快照/引用一致）
	d.seedCert(t, chgOldFP, []string{"a.example.com"}, domain.HostingStatusComplete)
	newID := d.seedCert(t, chgNewFP, []string{"a.example.com"}, domain.HostingStatusComplete)
	snapID := d.seedDoneSnapshot(t, 2*time.Hour)
	d.seedCloudRef(t, snapID, chgOldFP, "res-1")
	w := doPostJSON(t, engine, "/api/v1/certs/changes", GenerateChangeRequest{OldFingerprint: chgOldFP, NewCertID: newID})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var list changeListVO
	decodeInto(t, w, &list)

	path := "/api/v1/certs/changes/" + list.OrderID + "/confirm"
	// batchConf 越界：BatchSize<=0
	w = doPostJSON(t, engine, path, ConfirmRequest{BatchConf: &BatchConfVO{Enabled: true, BatchSize: 0, MaxBatchRatio: 0.3}})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	env := decode(t, w)
	assert.Equal(t, CodeInvalidRequest, env.Error.Code)

	// MaxBatchRatio>0.5 拒绝
	w = doPostJSON(t, engine, path, ConfirmRequest{BatchConf: &BatchConfVO{Enabled: true, BatchSize: 1, MaxBatchRatio: 0.7}})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	env = decode(t, w)
	assert.Equal(t, CodeInvalidRequest, env.Error.Code)

	// 合法 confirm（单引用清单：Enabled=false 单批全量合法）
	w = doPostJSON(t, engine, path, ConfirmRequest{})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var ack changeAckVO
	decodeInto(t, w, &ack)
	assert.Equal(t, list.OrderID, ack.OrderID)
}

func TestExecute_PausedOrderConflict(t *testing.T) {
	engine, d := newChangeRouter(t, RoleOpsEngineer)
	orderID := d.seedOrder(t, domain.ChangeStatusExecuting, &domain.BatchInfo{
		TotalBatches: 2, CurrentBatch: 1, BatchSize: 1, Paused: true,
	}, nil)

	w := doPostJSON(t, engine, "/api/v1/certs/changes/"+orderID+"/execute", nil)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	env := decode(t, w)
	require.NotNil(t, env.Error)
	assert.Equal(t, domain.CodeBatchNotConfirmable, env.Error.Code)
}

func TestConfirmBatch_NotConfirmable(t *testing.T) {
	// 未分批单：无 batchInfo → 409 BATCH_NOT_CONFIRMABLE
	t.Run("unbatched order", func(t *testing.T) {
		engine, d := newChangeRouter(t, RoleOpsEngineer)
		orderID := d.seedOrder(t, domain.ChangeStatusVerifying, nil, nil)

		w := doPostJSON(t, engine, "/api/v1/certs/changes/"+orderID+"/confirm-batch", nil)
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		env := decode(t, w)
		require.NotNil(t, env.Error)
		assert.Equal(t, domain.CodeBatchNotConfirmable, env.Error.Code)
	})

	// 分批单上一批存在 failed 项：门控 1 拒绝
	t.Run("previous batch has failed item", func(t *testing.T) {
		engine, d := newChangeRouter(t, RoleOpsEngineer)
		paused := &domain.BatchInfo{TotalBatches: 2, CurrentBatch: 1, BatchSize: 1, Paused: true}
		orderID := d.seedOrder(t, domain.ChangeStatusExecuting, paused, nil)
		d.seedItem(t, orderID, domain.ItemStatusFailed, "CLOUD_API_RATELIMITED: boom")

		w := doPostJSON(t, engine, "/api/v1/certs/changes/"+orderID+"/confirm-batch", nil)
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		env := decode(t, w)
		assert.Equal(t, domain.CodeBatchNotConfirmable, env.Error.Code)
	})
}

func TestCancel_NotCancellable(t *testing.T) {
	engine, d := newChangeRouter(t, RoleOpsEngineer)
	orderID := d.seedOrder(t, domain.ChangeStatusVerifying, nil, nil)

	w := doPostJSON(t, engine, "/api/v1/certs/changes/"+orderID+"/cancel", nil)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	env := decode(t, w)
	require.NotNil(t, env.Error)
	assert.Equal(t, domain.CodeChangeNotCancellable, env.Error.Code)
}

// ---- AC-3：progress 逐项轮询 ----

func TestProgress_ItemStates(t *testing.T) {
	engine, d := newChangeRouter(t, RoleOpsEngineer)
	orderID := d.seedOrder(t, domain.ChangeStatusExecuting, &domain.BatchInfo{
		TotalBatches: 2, CurrentBatch: 1, BatchSize: 2,
	}, nil)
	id1 := d.seedItem(t, orderID, domain.ItemStatusSuccess, "")
	id2 := d.seedItem(t, orderID, domain.ItemStatusRunning, "")
	id3 := d.seedItem(t, orderID, domain.ItemStatusRateLimited, "CLOUD_API_RATELIMITED: backoff")
	id4 := d.seedItem(t, orderID, domain.ItemStatusFailed, "K8S_UNREACHABLE: dial timeout")

	w := doGet(t, engine, "/api/v1/certs/changes/"+orderID+"/progress")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var p changeProgressVO
	decodeInto(t, w, &p)
	assert.Equal(t, orderID, p.OrderID)
	assert.Equal(t, "executing", p.Status)
	assert.Equal(t, 1, p.CurrentBatch)
	require.Len(t, p.ItemStates, 4)

	byID := map[string]progressItemVO{}
	for _, s := range p.ItemStates {
		byID[s.ItemID] = s
	}
	assert.Equal(t, "success", byID[id1].Status)
	assert.Equal(t, "running", byID[id2].Status)
	assert.Equal(t, "rate_limited", byID[id3].Status)
	assert.Contains(t, byID[id3].Error, "CLOUD_API_RATELIMITED")
	assert.Equal(t, "failed", byID[id4].Status)
	assert.Contains(t, byID[id4].Error, "K8S_UNREACHABLE")
	assertNoKeyMaterial(t, w)

	// 未命中 404
	w = doGet(t, engine, "/api/v1/certs/changes/"+primitive.NewObjectID().Hex()+"/progress")
	require.Equal(t, http.StatusNotFound, w.Code)
}

// ---- AC-4：rollback ----

func TestRollback_ScopeValidation(t *testing.T) {
	engine, d := newChangeRouter(t, RoleOpsEngineer)
	orderID := d.seedOrder(t, domain.ChangeStatusPartialCompleted, nil, nil)
	failedID := d.seedItem(t, orderID, domain.ItemStatusFailed, "K8S_UNREACHABLE: x")

	// 空 itemIds：请求侧 400
	w := doPostJSON(t, engine, "/api/v1/certs/changes/"+orderID+"/rollback", RollbackRequest{})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	env := decode(t, w)
	assert.Equal(t, CodeInvalidRequest, env.Error.Code)

	// 仅非成功项：仅成功项可回滚（范围解析 400）
	w = doPostJSON(t, engine, "/api/v1/certs/changes/"+orderID+"/rollback", RollbackRequest{ItemIDs: []string{failedID}})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	env = decode(t, w)
	assert.Equal(t, CodeInvalidRequest, env.Error.Code)
}

func TestRollback_InvalidTargetConflict(t *testing.T) {
	engine, d := newChangeRouter(t, RoleOpsEngineer)
	orderID := d.seedOrder(t, domain.ChangeStatusPartialCompleted, nil, nil)
	// 成功项但 oldCloudCertId 缺失：无可恢复目标 → ROLLBACK_TARGET_INVALID
	itemID := d.seedItem(t, orderID, domain.ItemStatusSuccess, "")

	w := doPostJSON(t, engine, "/api/v1/certs/changes/"+orderID+"/rollback", RollbackRequest{ItemIDs: []string{itemID}})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	env := decode(t, w)
	require.NotNil(t, env.Error)
	assert.Equal(t, domain.CodeRollbackTargetInvalid, env.Error.Code)
	// 附转人工提示语义
	assert.Contains(t, strings.ToLower(env.Error.Message), "manual intervention")
}

func TestRollback_SuccessPath(t *testing.T) {
	engine, d := newChangeRouter(t, RoleOpsEngineer)
	orderID := d.seedOrder(t, domain.ChangeStatusPartialCompleted, nil, nil)
	// k8s 项：前置三判定的云侧 GetCert 不适用（引用经 patch 恢复），配合
	// SimulatedChannel(k8s_api) 走通整链。
	it := domain.ChangeItem{
		ID:             primitive.NewObjectID(),
		OrderID:        orderID,
		Action:         domain.ActionPatchCRD,
		ResourceRef:    domain.ResourceRef{Channel: domain.ChannelK8sAPI, ClusterID: "c1", Namespace: "ns", Kind: "Certificate", ResourceID: "cert-crd"},
		OldCloudCertID: "old-cloud-cert",
		Status:         domain.ItemStatusSuccess,
	}
	_, err := d.items.CreateMulti(context.Background(), []domain.ChangeItem{it})
	require.NoError(t, err)

	w := doPostJSON(t, engine, "/api/v1/certs/changes/"+orderID+"/rollback", RollbackRequest{ItemIDs: []string{it.ID.Hex()}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// 回滚后经 progress 观察：项 rolled_back、订单收敛 rolled_back 终态
	w = doGet(t, engine, "/api/v1/certs/changes/"+orderID+"/progress")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var p changeProgressVO
	decodeInto(t, w, &p)
	require.Len(t, p.ItemStates, 1)
	assert.Equal(t, "rolled_back", p.ItemStates[0].Status)
	assert.Equal(t, "rolled_back", p.Status)
}

// ---- AC-6：终态详情/报告全字段 ----

func TestGetChange_TerminalReportFullFields(t *testing.T) {
	engine, d := newChangeRouter(t, RoleOpsEngineer)
	now := time.Now().UTC()
	expected := &domain.VerifyExpected{
		NewCertFingerprint: chgNewFP,
		Domains:            []string{"a.example.com", "b.example.com", "*.wild.example.com"},
		ExcludedDomains:    []string{"ex.example.com"},
		WindowUntil:        now.Add(-time.Hour),
	}
	orderID := d.seedOrder(t, domain.ChangeStatusPartialCompleted, nil, expected)

	// 变更项：success + failed + skipped(生成期不可执行) + rolled_back
	mk := func(status domain.ChangeItemStatus, errMsg string, executed, heartbeat *time.Time) domain.ChangeItem {
		return domain.ChangeItem{
			ID: primitive.NewObjectID(), OrderID: orderID, Action: domain.ActionUploadAndBind,
			ResourceRef: domain.ResourceRef{Channel: domain.ChannelCloudAPI, Cloud: "aliyun", Product: "cdn", AccountKey: "acct-1", ResourceID: "res-" + primitive.NewObjectID().Hex()[:6]},
			Status:      status, Error: errMsg, ExecutedAt: executed, HeartbeatAt: heartbeat,
		}
	}
	execAt := now.Add(-2 * time.Hour)
	finAt := now.Add(-30 * time.Minute)
	items := []domain.ChangeItem{
		mk(domain.ItemStatusSuccess, "", &execAt, &finAt),
		mk(domain.ItemStatusFailed, "K8S_UNREACHABLE: dial timeout", &execAt, &finAt),
		mk(domain.ItemStatusSkipped, "ERR_DISCOVERY_ONLY: huawei 首期无部署器", nil, nil),
		mk(domain.ItemStatusRolledBack, "", &execAt, &finAt),
	}
	_, err := d.items.CreateMulti(context.Background(), items)
	require.NoError(t, err)

	// 探测记录：a 达标（连续 2 次一致）、b 差异
	for _, p := range []domain.ProbeResult{
		{Domain: "a.example.com", ProbeAt: now.Add(-10 * time.Minute), OnlineFingerprint: chgNewFP, Status: domain.ProbeStatusConsistent},
		{Domain: "a.example.com", ProbeAt: now.Add(-20 * time.Minute), OnlineFingerprint: chgNewFP, Status: domain.ProbeStatusChangeLinkedDiff},
		{Domain: "b.example.com", ProbeAt: now.Add(-10 * time.Minute), OnlineFingerprint: chgOldFP, Status: domain.ProbeStatusChangeLinkedDiff},
	} {
		require.NoError(t, d.probes.Create(context.Background(), &p))
	}
	// 报告存档（5.10/5.9 写入侧对称读端口）
	d.unmet.domains = []string{"b.example.com"}
	d.orphan.results = []service.OrphanCleanupResult{{
		Cloud: "aliyun", CloudCertID: "old-cloud-cert", Action: service.OrphanActionCleanup, Success: true, At: now,
	}}

	w := doGet(t, engine, "/api/v1/certs/changes/"+orderID)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var detail changeDetailVO
	decodeInto(t, w, &detail)
	assert.Equal(t, orderID, detail.OrderID)
	assert.Equal(t, "partial_completed", detail.Status)
	assert.Len(t, detail.Items, 4)
	// 详情含 ChangeList 结构：target/action/autoChangeable/reason
	for _, it := range detail.Items {
		assert.NotEmpty(t, it.Target.Channel)
		assert.NotEmpty(t, it.Action)
	}
	// 生成期不可执行项：skipped + reason
	for _, it := range detail.Items {
		if it.Status == "skipped" {
			assert.False(t, it.AutoChangeable)
			assert.Contains(t, it.Reason, "ERR_DISCOVERY_ONLY")
		}
	}

	// 终态：ChangeReport 全字段
	require.NotNil(t, detail.Report)
	r := detail.Report
	assert.Equal(t, orderID, r.OrderID)
	assert.Equal(t, "partial_completed", r.Status)
	assert.Equal(t, 4, r.Summary.Total)
	assert.Equal(t, 1, r.Summary.Success)
	assert.Equal(t, 1, r.Summary.Failed)
	assert.Equal(t, 1, r.Summary.Skipped)
	assert.Equal(t, 1, r.Summary.RolledBack)
	require.Len(t, r.Items, 4)
	for _, it := range r.Items {
		assert.NotEmpty(t, it.ItemID)
		assert.NotEmpty(t, it.Status)
	}
	// latencyMs = HeartbeatAt - ExecutedAt（90 分钟）
	for _, it := range r.Items {
		if it.Status == "success" || it.Status == "failed" {
			assert.EqualValues(t, 90*time.Minute.Milliseconds(), it.LatencyMs)
		}
	}
	// Verify 全字段
	assert.Equal(t, chgNewFP, r.Verify.ExpectedNew)
	assert.EqualValues(t, 1, r.Verify.ProbePass)
	assert.EqualValues(t, 1, r.Verify.ProbeDiff)
	assert.EqualValues(t, 2, r.Verify.ProbeSkipped) // 豁免 1 + 无 override 通配符 1
	assert.EqualValues(t, 1, r.Verify.Unmet)
	assert.Equal(t, []string{"b.example.com"}, r.UnmetDomains)
	assert.NotEmpty(t, r.Verify.WindowUntil)
	assert.NotEmpty(t, r.FinishedAt)
	// OrphanCleanup 结果
	require.Len(t, r.OrphanCleanup, 1)
	assert.Equal(t, "old-cloud-cert", r.OrphanCleanup[0].CloudCertID)
	assert.True(t, r.OrphanCleanup[0].Success)

	// Hard Rule：响应无任何私钥/密文/凭证字段
	assertNoKeyMaterial(t, w)
	assert.NotContains(t, w.Body.String(), "keyVersion")
	assert.NotContains(t, w.Body.String(), "\"secret\"")
}

func TestGetChange_ActiveOrderNoReport(t *testing.T) {
	engine, d := newChangeRouter(t, RoleOpsEngineer)
	orderID := d.seedOrder(t, domain.ChangeStatusPendingConfirm, nil, nil)
	d.seedItem(t, orderID, domain.ItemStatusPending, "")

	w := doGet(t, engine, "/api/v1/certs/changes/"+orderID)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var detail changeDetailVO
	decodeInto(t, w, &detail)
	assert.Nil(t, detail.Report)
	assert.Len(t, detail.Items, 1)
}

// ---- AC-5：audit 按单查询 ----

func TestAudit_ByOrderAllActions(t *testing.T) {
	engine, d := newChangeRouter(t, RoleOpsEngineer)
	orderID := d.seedOrder(t, domain.ChangeStatusCompleted, nil, nil)
	itemID := d.seedItem(t, orderID, domain.ItemStatusSuccess, "")
	at := time.Now().UTC().Add(-time.Hour)
	d.audit.logs = []service.ChangeAuditLog{
		{At: at, Actor: "ops@example.com", Action: "create", Detail: "change order created"},
		{At: at.Add(time.Minute), Actor: "ops@example.com", Action: "confirm", Detail: "batch conf fixed"},
		{At: at.Add(2 * time.Minute), Actor: "ops@example.com", Action: "execute", Detail: "dispatched batch 1"},
		{At: at.Add(3 * time.Minute), Actor: "executor", Action: "item_result", Detail: "item success", ItemID: itemID},
		{At: at.Add(4 * time.Minute), Actor: "scheduler", Action: "verify", Detail: "window met"},
		{At: at.Add(5 * time.Minute), Actor: "ops@example.com", Action: "rollback", Detail: "rolled back", ItemID: itemID},
		{At: at.Add(6 * time.Minute), Actor: "orphan-cleanup", Action: "orphan_cleanup", Detail: "cleaned old cloud cert"},
	}

	w := doGet(t, engine, "/api/v1/certs/changes/"+orderID+"/audit")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var audit changeAuditVO
	decodeInto(t, w, &audit)
	assert.Equal(t, orderID, audit.OrderID)
	require.Len(t, audit.Logs, 7)
	actions := map[string]bool{}
	for _, l := range audit.Logs {
		assert.NotEmpty(t, l.At)
		assert.NotEmpty(t, l.Actor)
		assert.NotEmpty(t, l.Action)
		assert.NotEmpty(t, l.Detail)
		actions[l.Action] = true
	}
	for _, want := range []string{"create", "confirm", "execute", "item_result", "rollback", "verify", "orphan_cleanup"} {
		assert.True(t, actions[want], "audit action %q missing", want)
	}
	// 项级事件携带 itemId
	for _, l := range audit.Logs {
		if l.Action == "item_result" || l.Action == "rollback" {
			assert.Equal(t, itemID, l.ItemID)
		} else {
			assert.Empty(t, l.ItemID)
		}
	}
	assertNoKeyMaterial(t, w)

	// 未命中订单：404
	w = doGet(t, engine, "/api/v1/certs/changes/"+primitive.NewObjectID().Hex()+"/audit")
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestAudit_Roles(t *testing.T) {
	for _, role := range []Role{RoleOpsEngineer, RoleOpsSupervisor, RoleAuditor} {
		engine, d := newChangeRouter(t, role)
		orderID := d.seedOrder(t, domain.ChangeStatusCompleted, nil, nil)
		w := doGet(t, engine, "/api/v1/certs/changes/"+orderID+"/audit")
		assert.Equal(t, http.StatusOK, w.Code, "role=%s", role)
	}
	engine, _ := newChangeRouter(t, RoleViewer)
	w := doGet(t, engine, "/api/v1/certs/changes/"+primitive.NewObjectID().Hex()+"/audit")
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// 未接线审计存储：端点契约仍闭合（空流水）。
func TestAudit_NotWiredEmpty(t *testing.T) {
	engine, d := newChangeRouter(t, RoleOpsEngineer)
	orderID := d.seedOrder(t, domain.ChangeStatusCompleted, nil, nil)

	w := doGet(t, engine, "/api/v1/certs/changes/"+orderID+"/audit")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var audit changeAuditVO
	decodeInto(t, w, &audit)
	assert.Empty(t, audit.Logs)
}

// 非法订单 ID：400（不泄露存在性）。
func TestChangeEndpoints_InvalidID(t *testing.T) {
	engine, _ := newChangeRouter(t, RoleOpsEngineer)
	for _, path := range []string{
		"/api/v1/certs/changes/not-hex-id",
		"/api/v1/certs/changes/not-hex-id/progress",
		"/api/v1/certs/changes/not-hex-id/audit",
	} {
		w := doGet(t, engine, path)
		assert.Equal(t, http.StatusBadRequest, w.Code, path)
		env := decode(t, w)
		assert.Equal(t, CodeInvalidID, env.Error.Code)
	}
}
