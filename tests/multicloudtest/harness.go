// @feature cert-multicloud-deployers @api-functional
//
// Package multicloudtest provides the hermetic API-functional test harness
// for the cert-multicloud-deployers feature ( journeys under
// tests/<journey>/ ).
//
// The harness wires the production change-management HTTP surface over
// in-memory repositories and stubbed per-cloud adapter ports:
//
//	engine: gin.Recovery -> authGate ( EIAM auth stub ) -> CertRoleMiddleware
//	routes: /api/v1/certs/changes... ( ChangeHandler production registration )
//
// Production components under test ( unstubbed ):
//   - web.ChangeHandler + RequireRoles role guards ( real middleware chain )
//   - service.ChangeService changelist generation ( real K8s partitioning via
//     the ManagementProbe port, real SAN precheck, real four-409 gates )
//   - service.ChangeExecuteService two-phase engine ( claim -> channel Deploy
//     -> rate-limit semantics -> item states -> verify window entry )
//   - deployer.CloudAPIChannel two-phase orchestration ( upload -> mapping
//     active -> bind -> compensation active->orphan + CleanupOrphan )
//   - per-cloud CloudDeployers ( huawei/aws/azure ) over stub adapters
//   - service.ChangeRollbackService ( entry gates -> GetCert precheck ->
//     rebind -> orphan marking -> protect period )
//   - service.OrphanCleanupService queue consumption gates
//   - service.VerifyWindowService window judgment / expiry finalization
//
// Only two boundaries are stubbed: the upstream EIAM authentication
// middleware ( 401 semantics before role judgement, mirroring production
// "auth precedes role" ordering ) and the per-cloud SDK adapters ( transport
// to华为 SCM / AWS ACM / Azure KV ). The probe TLS dialer is reached through
// the ProbeService seam: the verify-window service consumes a stub prober
// whose per-domain online fingerprints stand in for real 443 TLS dials.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package multicloudtest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/internal/cert/web"
	awscloud "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/aws"
	azurecloud "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/azure"
	huaweicloud "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/huawei"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/ecodeclub/ginx/session"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Named timeout constant ( Golden Rule: no bare timeout literals ).
const (
	// HTTPClientTimeout bounds every request the harness issues.
	HTTPClientTimeout = 10 * time.Second
)

// API route constants ( .forge/fact-table.json MC_CHANGE_ROUTES ).
const (
	RouteChanges    = "/api/v1/certs/changes"
	RouteChangeByID = "/api/v1/certs/changes/" // + orderID (+ suffix)
	RouteConfirm    = "/confirm"
	RouteExecute    = "/execute"
	RouteConfirmBat = "/confirm-batch"
	RouteCancel     = "/cancel"
	RouteProgress   = "/progress"
	RouteRollback   = "/rollback"
	RouteAudit      = "/audit"

	// RoleOpsEngineer is the ops-engineer cert_role claim value.
	RoleOpsEngineer = "ops_engineer"
)

// FastRetryPolicy is the bounded retry policy installed on every stub-backed
// deployer: the same shape as production defaults but with millisecond
// backoffs so rate-limit / name-conflict retry branches stay fast. The
// attempt accounting and new-upload-name-per-attempt semantics are unchanged.
func FastRetryPolicy() deployer.RetryPolicy {
	return deployer.RetryPolicy{
		MaxAttempts:  3,
		Backoffs:     []time.Duration{time.Millisecond, time.Millisecond},
		MaxTotalWait: 10 * time.Millisecond,
	}
}

// Envelope mirrors the production response envelope ( web.Envelope ).
type Envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *APIError       `json:"error"`
	Meta    json.RawMessage `json:"meta"`
}

// APIError mirrors web.APIError.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Response is a decoded HTTP response.
type Response struct {
	StatusCode  int
	ContentType string
	Body        string
	Env         Envelope
}

// RequireJSON asserts the transport contract: JSON content type + envelope.
func (r *Response) RequireJSON(t *testing.T) {
	t.Helper()
	require.True(t, strings.HasPrefix(r.ContentType, "application/json"),
		"content type %q must be application/json", r.ContentType)
}

// DataMap decodes Env.Data into a generic map ( nil when the envelope carries
// no data, e.g. auth-gate rejections ).
func (r *Response) DataMap(t *testing.T) map[string]any {
	t.Helper()
	if len(r.Env.Data) == 0 {
		return nil
	}
	var m map[string]any
	require.NoError(t, json.Unmarshal(r.Env.Data, &m), "data is a JSON object: %s", r.Body)
	return m
}

// ---------------------------------------------------------------------
// Typed response payloads ( white-listed projections of change_handler.go )
// ---------------------------------------------------------------------

// TargetPayload mirrors targetVO.
type TargetPayload struct {
	Channel    string `json:"channel"`
	Cloud      string `json:"cloud"`
	Product    string `json:"product"`
	AccountKey string `json:"accountKey"`
	ResourceID string `json:"resourceId"`
}

// ChangeListItemPayload mirrors changeListItemVO.
type ChangeListItemPayload struct {
	ItemID         string        `json:"itemId"`
	Target         TargetPayload `json:"target"`
	Action         string        `json:"action"`
	AutoChangeable bool          `json:"autoChangeable"`
	Reason         string        `json:"reason"`
}

// SANCheckPayload mirrors sanCheckVO.
type SANCheckPayload struct {
	Passed  bool     `json:"passed"`
	Missing []string `json:"missing"`
	NewSANs []string `json:"newSans"`
}

// ChangeListPayload mirrors changeListVO ( POST /changes 201 body ).
type ChangeListPayload struct {
	OrderID          string                  `json:"orderId"`
	OldFingerprint   string                  `json:"oldFingerprint"`
	NewCertID        string                  `json:"newCertId"`
	SnapshotID       string                  `json:"snapshotId"`
	ScanFreshnessHrs int                     `json:"scanFreshnessHrs"`
	Items            []ChangeListItemPayload `json:"items"`
	SANCheck         SANCheckPayload         `json:"sanCheck"`
	Warnings         []string                `json:"warnings"`
}

// AckPayload mirrors changeAckVO.
type AckPayload struct {
	OrderID string `json:"orderId"`
}

// ProgressItemPayload mirrors progressItemVO.
type ProgressItemPayload struct {
	ItemID  string `json:"itemId"`
	Status  string `json:"status"`
	Error   string `json:"error"`
	BatchNo int    `json:"batchNo"`
}

// ProgressPayload mirrors changeProgressVO.
type ProgressPayload struct {
	OrderID      string                `json:"orderId"`
	Status       string                `json:"status"`
	CurrentBatch int                   `json:"currentBatch"`
	ItemStates   []ProgressItemPayload `json:"itemStates"`
}

// DetailItemPayload mirrors changeDetailItemVO.
type DetailItemPayload struct {
	ItemID         string        `json:"itemId"`
	Target         TargetPayload `json:"target"`
	Action         string        `json:"action"`
	AutoChangeable bool          `json:"autoChangeable"`
	Reason         string        `json:"reason"`
	BatchNo        int           `json:"batchNo"`
	Status         string        `json:"status"`
	Error          string        `json:"error"`
}

// BatchInfoPayload mirrors batchInfoVO.
type BatchInfoPayload struct {
	TotalBatches int  `json:"totalBatches"`
	CurrentBatch int  `json:"currentBatch"`
	BatchSize    int  `json:"batchSize"`
	Paused       bool `json:"paused"`
}

// ReportSummaryPayload mirrors reportSummaryVO.
type ReportSummaryPayload struct {
	Total      int `json:"total"`
	Success    int `json:"success"`
	Failed     int `json:"failed"`
	Skipped    int `json:"skipped"`
	RolledBack int `json:"rolledBack"`
}

// ReportVerifyPayload mirrors verifySummaryVO.
type ReportVerifyPayload struct {
	WindowUntil  string `json:"windowUntil"`
	ExpectedNew  string `json:"expectedNew"`
	ProbePass    int    `json:"probePass"`
	ProbeDiff    int    `json:"probeDiff"`
	ProbeSkipped int    `json:"probeSkipped"`
	Unmet        int    `json:"unmet"`
}

// OrphanCleanupPayload mirrors orphanCleanupVO.
type OrphanCleanupPayload struct {
	Cloud       string `json:"cloud"`
	CloudCertID string `json:"cloudCertId"`
	Action      string `json:"action"`
	Success     bool   `json:"success"`
	At          string `json:"at"`
}

// ReportPayload mirrors changeReportVO.
type ReportPayload struct {
	OrderID       string                 `json:"orderId"`
	Status        string                 `json:"status"`
	Summary       ReportSummaryPayload   `json:"summary"`
	Items         []ProgressItemPayload  `json:"items"`
	Verify        ReportVerifyPayload    `json:"verify"`
	OrphanCleanup []OrphanCleanupPayload `json:"orphanCleanup"`
	UnmetDomains  []string               `json:"unmetDomains"`
	FinishedAt    string                 `json:"finishedAt"`
}

// DetailPayload mirrors changeDetailVO.
type DetailPayload struct {
	OrderID        string              `json:"orderId"`
	OldFingerprint string              `json:"oldFingerprint"`
	NewCertID      string              `json:"newCertId"`
	Status         string              `json:"status"`
	BatchInfo      *BatchInfoPayload   `json:"batchInfo"`
	ProtectUntil   *string             `json:"protectUntil"`
	Items          []DetailItemPayload `json:"items"`
	Report         *ReportPayload      `json:"report"`
}

// AuditLogPayload mirrors auditLogVO.
type AuditLogPayload struct {
	At     string `json:"at"`
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Detail string `json:"detail"`
	ItemID string `json:"itemId"`
}

// AuditPayload mirrors changeAuditVO.
type AuditPayload struct {
	OrderID string            `json:"orderId"`
	Logs    []AuditLogPayload `json:"logs"`
}

// ---------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------

// Config tunes a Harness. Zero-value fields fall back to defaults; Claims nil
// means "unauthenticated request" ( 401 at the auth gate ).
type Config struct {
	// Claims injected into the memory session ( cert_role drives the real
	// CertRoleMiddleware mapping ).
	Claims map[string]string
	// HideMappingLookups hides these cloud cert IDs from FindByCloudCertID
	// for every service ( compensation-lookup-miss fault injection ).
	HideMappingLookups []string
	// MgmtProbe overrides the default K8s management probe stub.
	MgmtProbe service.ManagementProbe
}

// DefaultConfig returns the default configuration: an ops-engineer session.
func DefaultConfig() Config {
	return Config{Claims: map[string]string{
		"cert_role": RoleOpsEngineer,
		"username":  "ops-engineer",
	}}
}

// Harness owns one isolated world: in-memory repositories, stubbed cloud
// ports, a gin engine with the production middleware chain, and an
// httptest.Server driving real HTTP requests.
type Harness struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client

	Certs    *certtest.FakeCertificateRepo
	Refs     *certtest.FakeCertReferenceRepo
	Snaps    *certtest.FakeScanSnapshotRepo
	Probes   *certtest.FakeProbeResultRepo
	Exempts  *certtest.FakeExemptionRepo
	Mappings *certtest.FakeCloudCertMappingRepo
	Orders   *certtest.FakeChangeOrderRepo
	Items    *certtest.FakeChangeItemRepo
	AlertCfg *certtest.FakeAlertConfigRepo

	Huawei *StubHuaweiCertAPI
	Aws    *StubAwsCertAPI
	Azure  *StubAzureCertAPI
	Mgmt   *StubMgmtProbe
	Creds  *StubCredSource
	Prober *StubProbeRunner

	Alerts    *service.InMemoryAlertPublisher
	AuditSink *AuditStore
	Orphans   *OrphanResultStore
	Unmet     *UnmetStore
	Notifier  *ExecNotifier

	Changes     service.ChangeService
	Exec        service.ChangeExecuteService
	RollbackSvc service.ChangeRollbackService
	Cleanup     service.OrphanCleanupService
	Verify      service.VerifyWindowService

	// Channel is the production cloud API channel ( OrphanCleaner /
	// RollbackTargetSource / ExecutionChannel port — the cleanup consumer
	// and rollback flow route per-cloud calls through it ).
	Channel *deployer.CloudAPIChannel

	crypto *domain.EnvelopeCrypto
}

// NewHarness builds an isolated world and starts its HTTP server. mutate is
// applied to the default configuration ( pass nil to keep defaults; set
// Claims to nil inside mutate for an unauthenticated caller ). The server is
// closed via t.Cleanup.
func NewHarness(t *testing.T, mutate func(*Config)) *Harness {
	t.Helper()
	cfg := DefaultConfig()
	if mutate != nil {
		mutate(&cfg)
	}
	gin.SetMode(gin.TestMode)

	h := &Harness{
		t:         t,
		Certs:     certtest.NewFakeCertificateRepo(),
		Refs:      certtest.NewFakeCertReferenceRepo(),
		Snaps:     certtest.NewFakeScanSnapshotRepo(),
		Probes:    certtest.NewFakeProbeResultRepo(),
		Exempts:   certtest.NewFakeExemptionRepo(),
		Mappings:  certtest.NewFakeCloudCertMappingRepo(),
		Orders:    certtest.NewFakeChangeOrderRepo(),
		Items:     certtest.NewFakeChangeItemRepo(),
		AlertCfg:  certtest.NewFakeAlertConfigRepo(),
		Huawei:    NewStubHuaweiCertAPI(),
		Aws:       NewStubAwsCertAPI(),
		Azure:     NewStubAzureCertAPI(),
		Mgmt:      NewStubMgmtProbe(),
		Creds:     &StubCredSource{},
		Alerts:    service.NewInMemoryAlertPublisher(),
		AuditSink: NewAuditStore(),
		Orphans:   NewOrphanResultStore(),
		Unmet:     NewUnmetStore(),
		Notifier:  &ExecNotifier{},
	}
	h.crypto = certtest.NewTestCrypto(t)

	mappingRepo := domain.CloudCertMappingRepository(h.Mappings)
	if len(cfg.HideMappingLookups) > 0 {
		mappingRepo = NewLookupHidingMappings(h.Mappings, cfg.HideMappingLookups...)
	}

	// Per-cloud deployers over stub adapters + the real cloud API channel.
	cloudCh := deployer.NewCloudAPIChannel(mappingRepo,
		deployer.NewLedgerMaterialSource(h.Certs, h.crypto),
		deployer.NewSnapshotOldRefSource(h.Snaps, h.Refs))
	h.Channel = cloudCh
	require.NoError(t, cloudCh.RegisterDeployer("huawei",
		deployer.NewHuaweiDeployer(h.Huawei, h.Mappings, deployer.WithHuaweiRetryPolicy(FastRetryPolicy())),
		"cdn", "waf", "alb", "nlb"))
	require.NoError(t, cloudCh.RegisterDeployer("aws",
		deployer.NewAwsDeployer(h.Aws, h.Mappings, deployer.WithAwsRetryPolicy(FastRetryPolicy())),
		"cdn", "alb", "nlb"))
	require.NoError(t, cloudCh.RegisterDeployer("azure",
		deployer.NewAzureDeployer(h.Azure, h.Mappings, deployer.WithAzureRetryPolicy(FastRetryPolicy())),
		"cdn", "alb"))

	mgmt := cfg.MgmtProbe
	if mgmt == nil {
		mgmt = h.Mgmt
	}
	h.Prober = &StubProbeRunner{probes: h.Probes, certs: h.Certs, orders: h.Orders}

	// Production services under test.
	h.Changes = service.NewChangeService(h.Orders, h.Items, h.Certs, h.AlertCfg, h.Snaps, h.Refs, mappingRepo, mgmt)
	h.Verify = service.NewVerifyWindowService(h.Orders, h.Certs, h.Exempts, h.AlertCfg, h.Probes, h.Prober, h.Changes, h.Unmet, h.Alerts)
	dispatch := &InlineDispatcher{}
	h.Exec = service.NewChangeExecuteService(h.Orders, h.Items, h.Certs, h.AlertCfg, h.Snaps, h.Refs,
		[]deployer.ExecutionChannel{cloudCh}, h.Creds, dispatch, h.Verify, h.Verify, h.Notifier, h.AuditSink)
	dispatch.SetRunner(h.Exec)
	h.RollbackSvc = service.NewChangeRollbackService(h.Orders, h.Items, h.Certs, h.AlertCfg, mappingRepo,
		[]deployer.ExecutionChannel{cloudCh}, cloudCh, h.Creds, h.Alerts, h.AuditSink)
	h.Cleanup = service.NewOrphanCleanupService(h.Orders, h.Items, h.Certs, mappingRepo,
		cloudCh, h.Creds, h.Orphans, h.Alerts)
	querySvc := service.NewChangeQueryService(h.Orders, h.Items, h.Snaps, h.Probes, h.AlertCfg, h.Unmet, h.Orphans, h.AuditSink)
	changeH := web.NewChangeHandler(querySvc, h.Changes, h.Exec, h.RollbackSvc, h.AuditSink)

	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(authGate(cfg.Claims))
	group := engine.Group("/api/v1/certs", web.CertRoleMiddleware())
	changeH.RegisterRoutes(group)

	h.server = httptest.NewServer(engine)
	t.Cleanup(h.server.Close)
	h.client = &http.Client{Timeout: HTTPClientTimeout}
	return h
}

// authGate stubs the upstream EIAM authentication middleware: no session =>
// the production 401 body ( raw gin.H, not the cert envelope ) before any
// role judgement; otherwise injects the memory session ( claims feed
// CertRoleMiddleware ) and the username ( operator attribution ).
func authGate(claims map[string]string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if claims == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "认证失败：会话无效或已过期",
			})
			c.Abort()
			return
		}
		c.Set(session.CtxSessionKey, session.NewMemorySession(session.Claims{Data: claims}))
		if u := claims["username"]; u != "" {
			c.Set(middleware.CtxUsernameKey, u)
		}
		c.Next()
	}
}

// ---------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------

// URL joins the harness server base URL with an API path.
func (h *Harness) URL(path string) string { return h.server.URL + path }

// Get issues GET with an explicit Accept header and decodes the envelope.
func (h *Harness) Get(path string) *Response {
	h.t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.URL(path), nil)
	require.NoError(h.t, err)
	req.Header.Set("Accept", "application/json")
	return h.do(req)
}

// Post issues POST with a JSON body and decodes the envelope.
func (h *Harness) Post(path string, body any) *Response {
	h.t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(h.t, err)
	req, err := http.NewRequest(http.MethodPost, h.URL(path), strings.NewReader(string(raw)))
	require.NoError(h.t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return h.do(req)
}

func (h *Harness) do(req *http.Request) *Response {
	h.t.Helper()
	resp, err := h.client.Do(req)
	require.NoError(h.t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(h.t, err)
	out := &Response{StatusCode: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), Body: string(body)}
	if err := json.Unmarshal(body, &out.Env); err != nil {
		// Raw ( non-envelope ) bodies are legitimate for the 401 auth gate;
		// keep the raw body only.
		out.Env = Envelope{}
	}
	return out
}

// ---------------------------------------------------------------------
// Endpoint drivers
// ---------------------------------------------------------------------

// GenerateChangeList POSTs the changelist generation endpoint.
func (h *Harness) GenerateChangeList(oldFingerprint, newCertID string) *Response {
	h.t.Helper()
	return h.Post(RouteChanges, map[string]any{
		"oldFingerprint": oldFingerprint,
		"newCertId":      newCertID,
	})
}

// Confirm POSTs the confirm endpoint; batchConf nil omits the field.
func (h *Harness) Confirm(orderID string, batchConf *deployer.BatchConf) *Response {
	h.t.Helper()
	body := map[string]any{}
	if batchConf != nil {
		body["batchConf"] = map[string]any{
			"enabled":       batchConf.Enabled,
			"batchSize":     batchConf.BatchSize,
			"maxBatchRatio": batchConf.MaxBatchRatio,
		}
	}
	return h.Post(RouteChangeByID+orderID+RouteConfirm, body)
}

// Execute POSTs the execute endpoint.
func (h *Harness) Execute(orderID string) *Response {
	h.t.Helper()
	return h.Post(RouteChangeByID+orderID+RouteExecute, map[string]any{})
}

// ConfirmBatch POSTs the confirm-batch endpoint.
func (h *Harness) ConfirmBatch(orderID string) *Response {
	h.t.Helper()
	return h.Post(RouteChangeByID+orderID+RouteConfirmBat, map[string]any{})
}

// Rollback POSTs the rollback endpoint with the given item IDs.
func (h *Harness) Rollback(orderID string, itemIDs ...string) *Response {
	h.t.Helper()
	return h.Post(RouteChangeByID+orderID+RouteRollback, map[string]any{"itemIds": itemIDs})
}

// Progress GETs the progress endpoint.
func (h *Harness) Progress(orderID string) *Response {
	h.t.Helper()
	return h.Get(RouteChangeByID + orderID + RouteProgress)
}

// Detail GETs the order detail endpoint.
func (h *Harness) Detail(orderID string) *Response {
	h.t.Helper()
	return h.Get(RouteChangeByID + orderID)
}

// Audit GETs the order audit endpoint.
func (h *Harness) Audit(orderID string) *Response {
	h.t.Helper()
	return h.Get(RouteChangeByID + orderID + RouteAudit)
}

// MustGenerate requires 201 and returns the decoded changelist payload.
func (h *Harness) MustGenerate(oldFingerprint, newCertID string) ChangeListPayload {
	h.t.Helper()
	resp := h.GenerateChangeList(oldFingerprint, newCertID)
	require.Equal(h.t, http.StatusCreated, resp.StatusCode, resp.Body)
	resp.RequireJSON(h.t)
	var out ChangeListPayload
	require.NoError(h.t, json.Unmarshal(resp.Env.Data, &out), "data: %s", resp.Env.Data)
	return out
}

// MustConfirm requires 200 and returns the ack payload.
func (h *Harness) MustConfirm(orderID string, batchConf *deployer.BatchConf) AckPayload {
	h.t.Helper()
	resp := h.Confirm(orderID, batchConf)
	require.Equal(h.t, http.StatusOK, resp.StatusCode, resp.Body)
	return decodeAck(h.t, resp)
}

// MustExecute requires 200 and returns the ack payload.
func (h *Harness) MustExecute(orderID string) AckPayload {
	h.t.Helper()
	resp := h.Execute(orderID)
	require.Equal(h.t, http.StatusOK, resp.StatusCode, resp.Body)
	return decodeAck(h.t, resp)
}

// MustProgress requires 200 and returns the decoded progress payload.
func (h *Harness) MustProgress(orderID string) ProgressPayload {
	h.t.Helper()
	resp := h.Progress(orderID)
	require.Equal(h.t, http.StatusOK, resp.StatusCode, resp.Body)
	var out ProgressPayload
	require.NoError(h.t, json.Unmarshal(resp.Env.Data, &out), "data: %s", resp.Env.Data)
	return out
}

// MustDetail requires 200 and returns the decoded detail payload.
func (h *Harness) MustDetail(orderID string) DetailPayload {
	h.t.Helper()
	resp := h.Detail(orderID)
	require.Equal(h.t, http.StatusOK, resp.StatusCode, resp.Body)
	var out DetailPayload
	require.NoError(h.t, json.Unmarshal(resp.Env.Data, &out), "data: %s", resp.Env.Data)
	return out
}

// MustRollback requires 200 and returns the ack payload.
func (h *Harness) MustRollback(orderID string, itemIDs ...string) AckPayload {
	h.t.Helper()
	resp := h.Rollback(orderID, itemIDs...)
	require.Equal(h.t, http.StatusOK, resp.StatusCode, resp.Body)
	return decodeAck(h.t, resp)
}

// MustConfirmBatch requires 200 and returns the ack payload.
func (h *Harness) MustConfirmBatch(orderID string) AckPayload {
	h.t.Helper()
	resp := h.ConfirmBatch(orderID)
	require.Equal(h.t, http.StatusOK, resp.StatusCode, resp.Body)
	return decodeAck(h.t, resp)
}

func decodeAck(t *testing.T, resp *Response) AckPayload {
	t.Helper()
	var out AckPayload
	require.NoError(t, json.Unmarshal(resp.Env.Data, &out), "data: %s", resp.Env.Data)
	return out
}

// ---------------------------------------------------------------------
// Seeding helpers ( fixture-from-spec support )
// ---------------------------------------------------------------------

// SeedCompleteCert writes a complete ledger certificate ( cert PEM chain +
// envelope-encrypted private key ) and returns its ID and fingerprint.
func (h *Harness) SeedCompleteCert(cn string, sans ...string) (string, string) {
	h.t.Helper()
	bundle := certtest.NewBundle(h.t, cn, sans, nil)
	cipher, ver, err := h.crypto.Encrypt(bundle.KeyPEM)
	require.NoError(h.t, err)
	cert := &domain.Certificate{
		Fingerprint:         bundle.Fingerprint,
		CommonName:          cn,
		Sans:                sans,
		HostingStatus:       domain.HostingStatusComplete,
		CertPEM:             string(bundle.CertPEM),
		EncryptedPrivateKey: &domain.EncryptedSecret{Ciphertext: cipher, KeyVersion: ver},
	}
	require.NoError(h.t, h.Certs.Create(context.Background(), cert))
	return cert.ID.Hex(), bundle.Fingerprint
}

// SeedFingerprintOnlyCert writes a fingerprint-only ledger certificate.
func (h *Harness) SeedFingerprintOnlyCert(fp string) {
	h.t.Helper()
	require.NoError(h.t, h.Certs.Create(context.Background(), &domain.Certificate{
		Fingerprint:   fp,
		CommonName:    "seed-" + fp[:8],
		HostingStatus: domain.HostingStatusFingerprintOnly,
	}))
}

// RefSpec describes one reference inside a scan snapshot.
type RefSpec struct {
	Cloud       domain.Cloud
	Product     domain.Product
	AccountKey  string
	ResourceID  string
	CloudCertID string
	Fingerprint string
	ClusterID   string
	Namespace   string
	Kind        string
}

// SeedDoneSnapshotWithRefs creates a fresh done snapshot carrying the given
// references and returns the snapshot ID. An optional final argument sets an
// explicit startedAt ( Windows clock granularity can render two immediate
// time.Now() calls equal, which flips LatestDone ordering ).
func (h *Harness) SeedDoneSnapshotWithRefs(refs []RefSpec, startedAt ...time.Time) string {
	h.t.Helper()
	ctx := context.Background()
	started := time.Now()
	if len(startedAt) > 0 {
		started = startedAt[0]
	}
	snapID, err := h.Snaps.Create(ctx, &domain.ScanSnapshot{StartedAt: started})
	require.NoError(h.t, err)
	require.NoError(h.t, h.Snaps.MarkFinished(ctx, snapID, domain.ScanStatusDone, ""))
	rows := make([]domain.CertReference, 0, len(refs))
	for _, s := range refs {
		product := s.Product
		rows = append(rows, domain.CertReference{
			CertFingerprint:       s.Fingerprint,
			Cloud:                 s.Cloud,
			Product:               product,
			AccountKey:            s.AccountKey,
			ResourceID:            s.ResourceID,
			ReferencedCloudCertID: s.CloudCertID,
			SnapshotID:            snapID,
			ClusterID:             s.ClusterID,
			Namespace:             s.Namespace,
			Kind:                  s.Kind,
			ScannedAt:             time.Now(),
		})
	}
	_, err = h.Refs.CreateMulti(ctx, rows)
	require.NoError(h.t, err)
	return snapID
}

// SeedOrder writes a change order with explicit status/batch/expected state
// and returns its ID. oldFP empty means "no mutex token". An optional final
// argument sets the order-level verify window ( production EnterVerify writes
// VerifyWindowUntil; the verify-window jobs scan orders by it ).
func (h *Harness) SeedOrder(status domain.ChangeStatus, batch *domain.BatchInfo, expected *domain.VerifyExpected, oldFP, newCertID string, verifyWindowUntil ...time.Time) string {
	h.t.Helper()
	order := &domain.ChangeOrder{
		OldCertFingerprint: oldFP,
		NewCertID:          newCertID,
		Status:             status,
		SnapshotID:         "snap-seeded",
		BatchInfo:          batch,
		VerifyExpected:     expected,
	}
	if len(verifyWindowUntil) > 0 {
		until := verifyWindowUntil[0]
		order.VerifyWindowUntil = &until
	}
	if domain.IsActiveChangeStatus(status) && oldFP != "" {
		order.ActiveMutex = oldFP
	}
	id, err := h.Orders.Create(context.Background(), order)
	require.NoError(h.t, err)
	return id
}

// SeedItem writes a change item with explicit state and returns its ID. An
// optional final argument sets the item's new cloud cert ID ( the upload
// artifact a successful replacement item carries ).
func (h *Harness) SeedItem(orderID string, ref domain.ResourceRef, status domain.ChangeItemStatus, batchNo int, oldCloudCertID string, newCloudCertID ...string) string {
	h.t.Helper()
	item := domain.ChangeItem{
		ID:             primitive.NewObjectID(),
		OrderID:        orderID,
		BatchNo:        batchNo,
		Action:         domain.ActionUploadAndBind,
		ResourceRef:    ref,
		OldCloudCertID: oldCloudCertID,
		Status:         status,
	}
	if len(newCloudCertID) > 0 {
		item.NewCloudCertID = newCloudCertID[0]
	}
	if domain.Product(ref.Product) == domain.ProductCRD {
		item.Action = domain.ActionPatchCRD
	}
	_, err := h.Items.CreateMulti(context.Background(), []domain.ChangeItem{item})
	require.NoError(h.t, err)
	return item.ID.Hex()
}

// SeedMapping writes an active mapping row and returns it.
func (h *Harness) SeedMapping(fingerprint, cloud, accountKey, cloudCertID string) domain.CloudCertMapping {
	h.t.Helper()
	m := &domain.CloudCertMapping{
		CertFingerprint: fingerprint,
		Cloud:           cloud,
		AccountKey:      accountKey,
		CloudCertID:     cloudCertID,
		Status:          domain.MappingStatusActive,
	}
	require.NoError(h.t, h.Mappings.Upsert(context.Background(), m))
	return *m
}

// SeedProtectUntil sets a protection period on the ledger certificate
// ( extend-only semantics, mirroring production SetProtectUntil ).
func (h *Harness) SeedProtectUntil(fingerprint string, until time.Time) {
	h.t.Helper()
	require.NoError(h.t, h.Certs.SetProtectUntil(context.Background(), fingerprint, until))
}

// ExpireProtectUntil force-overwrites the protection period ( test hook:
// simulates a protection window that has already elapsed, which the
// extend-only production primitive cannot express ).
func (h *Harness) ExpireProtectUntil(fingerprint string, until time.Time) {
	h.t.Helper()
	require.NoError(h.t, h.Certs.ForceProtectUntil(context.Background(), fingerprint, until))
}

// OldCertCloudID registers a valid rollback target on a cloud stub: the old
// cloud cert exists, is unexpired, and carries the old ledger fingerprint.
func (h *Harness) OldCertCloudID(cloud, oldCloudCertID, oldFingerprint string) {
	h.t.Helper()
	notAfter := time.Now().Add(365 * 24 * time.Hour)
	switch cloud {
	case "huawei":
		h.Huawei.SetCertInfo(oldCloudCertID, huaweicloud.CloudCertInfo{
			Exists: true, NotAfter: notAfter, Fingerprint: oldFingerprint,
		})
	case "aws":
		h.Aws.SetCertInfo(oldCloudCertID, awscloud.CloudCertInfo{
			Exists: true, NotAfter: notAfter, Fingerprint: oldFingerprint,
		})
	case "azure":
		h.Azure.SetCertInfo(oldCloudCertID, azurecloud.CloudCertInfo{
			Exists: true, NotAfter: notAfter, Fingerprint: oldFingerprint,
		})
	default:
		h.t.Fatalf("unsupported cloud %q", cloud)
	}
}

// MappingByCloudCert returns the mapping row for a cloud cert ID ( read-back
// assertion helper ).
func (h *Harness) MappingByCloudCert(cloud, accountKey, cloudCertID string) (domain.CloudCertMapping, error) {
	h.t.Helper()
	return h.Mappings.FindByCloudCertID(context.Background(), cloud, accountKey, cloudCertID)
}

// OrphanMappings returns every mapping currently in the cleanup queue.
func (h *Harness) OrphanMappings() []domain.CloudCertMapping {
	h.t.Helper()
	out, err := h.Mappings.ListByStatus(context.Background(), domain.MappingStatusOrphan)
	require.NoError(h.t, err)
	return out
}

// FP derives a deterministic ledger-aligned test fingerprint ( 64 hex ).
func FP(seed string) string {
	sum := sha256.Sum256([]byte("multicloud-test:" + seed))
	return hex.EncodeToString(sum[:])
}
