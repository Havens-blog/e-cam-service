// @feature cert-volcano-import-sync @api-functional
//
// Package synctest provides the hermetic API-functional test harness for the
// cert-volcano-import-sync feature ( journeys under tests/<journey>/ ).
//
// The harness wires the production sync surface over in-memory repositories
// and stubbed cloud ports:
//
//	engine: gin.Recovery -> authGate ( EIAM auth stub ) -> CertRoleMiddleware
//	routes: /api/v1/certs/discovery/... ( DiscoveryHandler production
//	        registration incl. POST /discovery/sync )
//
// Production components under test ( unstubbed ):
//   - web.DiscoveryHandler + RequireRoles( RoleOpsEngineer ) guard
//   - service.CertSyncService增量判定 ( CAS guard, four-state judgment,
//     cloud/account isolation, static failure reasons )
//   - service.DiscoveryImportService synchronous face
//     ( ImportFromDiscoverySync: ledger idempotency, mapping upsert,
//     ALREADY_IN_LEDGER redirect )
//   - scheduler.CertJobs cert:cert-import guarded entry ( nil-degrade,
//     ErrSyncRunning yield )
//
// Stubbed boundaries ( per repo convention, real cloud accounts are not
// reachable from this host ):
//   - upstream EIAM authentication middleware ( 401 before role judgement )
//   - per-cloud cert-library listers ( CertLibraryLister: instance metadata )
//   - per-cloud import material adapters ( DiscoveryCertAdapter: chain PEM )
//   - per-cloud active account source ( ScanAccountSource )
//
// The material adapters count GetCertChain calls: the "skip costs zero
// material-channel Gets" invariant ( five-cloud ledger-skip discipline ) is
// asserted against that counter. Volcano skip semantics deliberately do NOT
// use the Get counter against the lister ( a real volcano lister performs
// per-instance Gets inside List — adapter-inherent cost, out of the skip
// contract scope ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package synctest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/scheduler"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/internal/cert/web"
	sharedomain "github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/ecodeclub/ginx/session"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Named timeout constants ( Golden Rule: no bare timeout literals ).
const (
	// HTTPClientTimeout bounds every request the harness issues.
	HTTPClientTimeout = 10 * time.Second
	// WaitDeadline bounds in-flight signal waits ( CAS race fixtures ).
	WaitDeadline = 5 * time.Second
	// WaitInterval is the wait between two signal polls.
	WaitInterval = 5 * time.Millisecond
)

// API route constants ( .forge/fact-table.json CERT_SYNC_ENDPOINT /
// SESSION_POLL_ENDPOINT ).
const (
	RouteDiscoverySync = "/api/v1/certs/discovery/sync"
	RouteImportSession = "/api/v1/certs/discovery/import/" // + sessionId
)

// cert_role claim values used across the guard journeys.
const (
	RoleOpsEngineerClaim = "ops_engineer"
	RoleViewerClaim      = "viewer"
	RoleAuditorClaim     = "auditor"
	RoleSupervisorClaim  = "ops_supervisor"
)

// CloudVolcano is the volcano discovery cloud identifier ( the domain.Cloud
// enum predates volcano; the service derives the value from the account
// provider constant — fact CERT_SYNC_CLOUD_ORDER / discoveryCloudVolcano ).
var CloudVolcano = domain.Cloud(sharedomain.CloudProviderVolcano)

// CertSyncClouds mirrors the service's fixed traversal order
// ( fact CERT_SYNC_CLOUD_ORDER: aliyun,tencent,huawei,aws,azure,volcano ).
var CertSyncClouds = []domain.Cloud{
	domain.CloudAliyun, domain.CloudTencent, domain.CloudHuawei,
	domain.CloudAWS, domain.CloudAzure, CloudVolcano,
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
// no data, e.g. auth-gate rejections or the 409 conflict path ).
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
// Typed response payloads ( white-listed projections of discovery_handler.go )
// ---------------------------------------------------------------------

// SyncFailurePayload mirrors CertSyncFailureVO ( whitelist fields: reason
// always present, cloud/accountKey/cloudCertId omitempty ).
type SyncFailurePayload struct {
	Cloud       string `json:"cloud,omitempty"`
	AccountKey  string `json:"accountKey,omitempty"`
	CloudCertID string `json:"cloudCertId,omitempty"`
	Reason      string `json:"reason"`
}

// SyncRunPayload mirrors CertSyncRunVO ( fact CERT_SYNC_RUN_VO_FIELDS ).
type SyncRunPayload struct {
	SessionID       string               `json:"sessionId,omitempty"`
	Status          string               `json:"status"`
	StartedAt       string               `json:"startedAt"`
	FinishedAt      string               `json:"finishedAt"`
	CloudsScanned   int                  `json:"cloudsScanned"`
	AccountsScanned int                  `json:"accountsScanned"`
	Listed          int                  `json:"listed"`
	Skipped         int                  `json:"skipped"`
	Backfilled      int                  `json:"backfilled"`
	Drifted         int                  `json:"drifted"`
	Imported        int                  `json:"imported"`
	ImportSucceeded int                  `json:"importSucceeded"`
	ImportFailed    int                  `json:"importFailed"`
	Failures        []SyncFailurePayload `json:"failures"`
}

// SessionItemPayload mirrors DiscoveryImportItemVO.
type SessionItemPayload struct {
	Cloud        string `json:"cloud"`
	AccountKey   string `json:"accountKey"`
	CloudCertID  string `json:"cloudCertId"`
	Result       string `json:"result"`
	MappedCertID string `json:"mappedCertId,omitempty"`
	ErrorReason  string `json:"errorReason,omitempty"`
}

// SessionPayload mirrors DiscoveryImportSessionVO ( progress poll face ).
type SessionPayload struct {
	SessionID string               `json:"sessionId"`
	Status    string               `json:"status"`
	Items     []SessionItemPayload `json:"items"`
	Progress  struct {
		Total     int `json:"total"`
		Succeeded int `json:"succeeded"`
		Failed    int `json:"failed"`
	} `json:"progress"`
	CreatedAt  string  `json:"createdAt"`
	FinishedAt *string `json:"finishedAt,omitempty"`
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
	// Accounts is the per-cloud active account source fixture.
	Accounts map[domain.Cloud][]*sharedomain.CloudAccount
	// AccountErrs injects per-cloud ActiveByCloud failures ( cloud-level
	// ACCOUNT_LOAD_FAILED isolation ).
	AccountErrs map[domain.Cloud]error
	// Listers registers cert-library lister stubs for these clouds. Clouds
	// without a registered lister are silently skipped by the sync service
	// ( capability gap, fact CERT_SYNC_LISTER_GAP_SKIP ).
	Listers []domain.Cloud
	// WireSync mounts the sync dependency on the discovery handler. Default
	// true; false exercises the not-wired 500 defense branch.
	WireSync bool
	// WrapMappings wraps the mapping repository handed to the services
	// ( Upsert fault injection ) while the harness keeps the base fake for
	// seeding and read-back assertions.
	WrapMappings func(base *certtest.FakeCloudCertMappingRepo) domain.CloudCertMappingRepository
}

// DefaultConfig returns the default configuration: an ops-engineer session
// with the sync dependency wired and no listers/accounts registered.
func DefaultConfig() Config {
	return Config{Claims: map[string]string{
		"cert_role": RoleOpsEngineerClaim,
		"username":  "ops-engineer",
	}, WireSync: true}
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
	Mappings *certtest.FakeCloudCertMappingRepo
	Sessions *CountingSessionRepo

	accounts  *StubAccountSource
	listers   map[domain.Cloud]*StubLibraryLister
	materials map[domain.Cloud]*StubMaterialAdapter

	// Sync is the production CertSyncService under test ( shared by the
	// scheduler face, the manual HTTP face, and the CAS guard ).
	Sync service.CertSyncService
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
		Mappings:  certtest.NewFakeCloudCertMappingRepo(),
		Sessions:  &CountingSessionRepo{FakeDiscoveryImportSessionRepo: certtest.NewFakeDiscoveryImportSessionRepo()},
		accounts:  &StubAccountSource{ByCloud: cfg.Accounts, ErrByCloud: cfg.AccountErrs},
		listers:   map[domain.Cloud]*StubLibraryLister{},
		materials: map[domain.Cloud]*StubMaterialAdapter{},
	}
	for _, cloud := range CertSyncClouds {
		h.materials[cloud] = NewStubMaterialAdapter(cloud)
	}
	for _, cloud := range cfg.Listers {
		h.listers[cloud] = NewStubLibraryLister(cloud)
	}

	certRepo := domain.CertificateRepository(h.Certs)
	mappingRepo := domain.CloudCertMappingRepository(h.Mappings)
	if cfg.WrapMappings != nil {
		mappingRepo = cfg.WrapMappings(h.Mappings)
	}

	adapters := make([]service.DiscoveryCertAdapter, 0, len(h.materials))
	for _, cloud := range CertSyncClouds {
		adapters = append(adapters, h.materials[cloud])
	}
	importSvc := service.NewDiscoveryImportService(h.Sessions, certRepo, mappingRepo, h.Refs, adapters, h.accounts, nil)
	previewSvc := service.NewDiscoveryPreviewService(h.Snaps, h.Refs, certRepo, mappingRepo)

	listerPorts := make([]service.CertLibraryLister, 0, len(h.listers))
	for _, cloud := range CertSyncClouds {
		if l, ok := h.listers[cloud]; ok {
			listerPorts = append(listerPorts, l)
		}
	}
	h.Sync = service.NewCertSyncService(listerPorts, h.accounts, importSvc, certRepo, mappingRepo)

	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(authGate(cfg.Claims))
	group := engine.Group("/api/v1/certs", web.CertRoleMiddleware())
	if cfg.WireSync {
		web.NewDiscoveryHandler(previewSvc, importSvc, h.Sync).RegisterRoutes(group)
	} else {
		web.NewDiscoveryHandler(previewSvc, importSvc).RegisterRoutes(group)
	}

	h.server = httptest.NewServer(engine)
	t.Cleanup(h.server.Close)
	h.client = &http.Client{Timeout: HTTPClientTimeout}
	return h
}

// authGate stubs the upstream EIAM authentication middleware: no session =>
// 401 semantics before role judgement; otherwise injects the memory session
// ( claims feed CertRoleMiddleware ) and the username ( operator attribution ).
func authGate(claims map[string]string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if claims == nil {
			c.PureJSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   gin.H{"code": "UNAUTHORIZED", "message": "authentication required"},
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
	require.NoError(h.t, json.Unmarshal(body, &out.Env), "response is JSON envelope: %s", out.Body)
	return out
}

// PostSync drives POST /api/v1/certs/discovery/sync ( no request body
// semantics ).
func (h *Harness) PostSync() *Response {
	h.t.Helper()
	return h.Post(RouteDiscoverySync, map[string]any{})
}

// MustSync requires the one-shot 200 terminal summary and decodes it.
func (h *Harness) MustSync() SyncRunPayload {
	h.t.Helper()
	resp := h.PostSync()
	require.Equal(h.t, http.StatusOK, resp.StatusCode, resp.Body)
	resp.RequireJSON(h.t)
	return MustDecodeSyncRun(h.t, resp)
}

// MustDecodeSyncRun decodes a 200 sync-summary response body into the typed
// payload ( for responses captured asynchronously in race fixtures ).
func MustDecodeSyncRun(t *testing.T, resp *Response) SyncRunPayload {
	t.Helper()
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	var out SyncRunPayload
	require.NoError(t, json.Unmarshal(resp.Env.Data, &out), "sync summary: %s", resp.Env.Data)
	return out
}

// GetImportSession drives GET /api/v1/certs/discovery/import/:sessionId.
func (h *Harness) GetImportSession(sessionID string) *Response {
	h.t.Helper()
	return h.Get(RouteImportSession + sessionID)
}

// MustImportSession requires 200 and decodes the session VO.
func (h *Harness) MustImportSession(sessionID string) SessionPayload {
	h.t.Helper()
	resp := h.GetImportSession(sessionID)
	require.Equal(h.t, http.StatusOK, resp.StatusCode, resp.Body)
	var out SessionPayload
	require.NoError(h.t, json.Unmarshal(resp.Env.Data, &out), "session: %s", resp.Env.Data)
	return out
}

// ---------------------------------------------------------------------
// Scheduler face driver
// ---------------------------------------------------------------------

// CertImportJobRun returns the guarded cert:cert-import job entry bound to
// the given sync service ( sync nil exercises the nil-degrade branch ). The
// scheduler job yields silently on ErrSyncRunning ( fact JOB_CERT_IMPORT_SPEC ).
func CertImportJobRun(sync service.CertSyncService) func(context.Context) error {
	jobs := &scheduler.CertJobs{Sync: sync}
	for _, spec := range jobs.JobSpecs(0) {
		if spec.Name == scheduler.JobCertImport {
			return spec.Run
		}
	}
	panic("synctest: cert:cert-import job spec not found")
}

// ---------------------------------------------------------------------
// Cloud port stubs
// ---------------------------------------------------------------------

// StubLibraryLister is a per-cloud CertLibraryLister stub: instance metadata
// per account ( falling back to the cloud-level default ), per-account and
// cloud-level list errors, call accounting, and an optional gate that holds
// ListInstances until released ( CAS race fixtures ).
type StubLibraryLister struct {
	cloud domain.Cloud

	mu               sync.Mutex
	byAccount        map[string][]service.CertLibraryInstance
	defaultInstances []service.CertLibraryInstance
	errByAccount     map[string]error
	listErr          error
	lists            int
	gate             chan struct{}
	entered          chan struct{}
}

// NewStubLibraryLister creates an empty lister stub for one cloud.
func NewStubLibraryLister(cloud domain.Cloud) *StubLibraryLister {
	return &StubLibraryLister{
		cloud:        cloud,
		byAccount:    map[string][]service.CertLibraryInstance{},
		errByAccount: map[string]error{},
		entered:      make(chan struct{}, 1),
	}
}

// Cloud reports the stub's cloud.
func (l *StubLibraryLister) Cloud() domain.Cloud { return l.cloud }

// SetInstances registers the instance list for one account.
func (l *StubLibraryLister) SetInstances(accountKey string, instances ...service.CertLibraryInstance) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.byAccount[accountKey] = append([]service.CertLibraryInstance(nil), instances...)
}

// SetDefaultInstances registers the fallback list for unconfigured accounts.
func (l *StubLibraryLister) SetDefaultInstances(instances []service.CertLibraryInstance) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.defaultInstances = instances
}

// SetError registers a per-account list failure ( with optional partial
// results — the adapter contract returns already-fetched instances alongside
// the error, fact CERT_SYNC_LIST_PARTIAL_TOLERANCE ).
func (l *StubLibraryLister) SetError(accountKey string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errByAccount[accountKey] = err
}

// SetCloudError registers a cloud-level list failure for any account.
func (l *StubLibraryLister) SetCloudError(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.listErr = err
}

// ClearError removes every list failure ( rerun-convergence fixtures ).
func (l *StubLibraryLister) ClearError() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errByAccount = map[string]error{}
	l.listErr = nil
}

// SetGate holds every subsequent ListInstances until the channel is closed
// or the caller context expires. Any stale Entered() token from a previous
// round is drained so the next WaitForSignal can only observe the NEW round
// reaching the lister.
func (l *StubLibraryLister) SetGate(gate chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gate = gate
	select {
	case <-l.entered:
	default:
	}
}

// DrainSignal discards any pending Entered() token ( stale signals from
// earlier rounds of the same harness would otherwise make WaitForSignal
// return before the gated round actually holds the CAS ).
func (l *StubLibraryLister) DrainSignal() {
	select {
	case <-l.entered:
	default:
	}
}

// Entered returns a signal channel that receives one token when ListInstances
// is first reached ( proof that an in-flight round holds the CAS ).
func (l *StubLibraryLister) Entered() <-chan struct{} { return l.entered }

// Lists returns the ListInstances call count ( read-only discipline probe ).
func (l *StubLibraryLister) Lists() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lists
}

// ListInstances implements service.CertLibraryLister.
func (l *StubLibraryLister) ListInstances(ctx context.Context, creds *sharedomain.CloudAccount) ([]service.CertLibraryInstance, error) {
	l.mu.Lock()
	l.lists++
	gate := l.gate
	l.mu.Unlock()
	select {
	case l.entered <- struct{}{}:
	default:
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	instances := l.byAccount[creds.Name]
	if instances == nil {
		instances = l.defaultInstances
	}
	err := l.errByAccount[creds.Name]
	if err == nil {
		err = l.listErr
	}
	return append([]service.CertLibraryInstance(nil), instances...), err
}

// StubMaterialAdapter is a per-cloud DiscoveryCertAdapter stub with call
// accounting: cloudCertID -> material / error. Unconfigured IDs report
// Exists=false ( cert deleted cloud-side ).
type StubMaterialAdapter struct {
	cloud    domain.Cloud
	mu       sync.Mutex
	material map[string]service.DiscoveryCertMaterial
	errs     map[string]error
	calls    atomic.Int32
	called   map[string]int
}

// NewStubMaterialAdapter creates an empty material stub for one cloud.
func NewStubMaterialAdapter(cloud domain.Cloud) *StubMaterialAdapter {
	return &StubMaterialAdapter{
		cloud:    cloud,
		material: map[string]service.DiscoveryCertMaterial{},
		errs:     map[string]error{},
		called:   map[string]int{},
	}
}

// Cloud reports the stub's cloud.
func (a *StubMaterialAdapter) Cloud() domain.Cloud { return a.cloud }

// AddMaterial registers in-cloud material for a cloudCertID.
func (a *StubMaterialAdapter) AddMaterial(cloudCertID string, m service.DiscoveryCertMaterial) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.material[cloudCertID] = m
}

// AddBundle registers a parsed test certificate bundle as material.
func (a *StubMaterialAdapter) AddBundle(cloudCertID string, bundle *certtest.CertBundle) {
	a.AddMaterial(cloudCertID, service.DiscoveryCertMaterial{
		Exists:       true,
		CertChainPEM: string(bundle.CertPEM),
	})
}

// AddError registers a GetCertChain failure for a cloudCertID.
func (a *StubMaterialAdapter) AddError(cloudCertID string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.errs[cloudCertID] = err
}

// ClearError removes a previously registered GetCertChain failure
// ( rerun-convergence fixtures ).
func (a *StubMaterialAdapter) ClearError(cloudCertID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.errs, cloudCertID)
}

// GetCertChain implements service.DiscoveryCertAdapter.
func (a *StubMaterialAdapter) GetCertChain(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) (service.DiscoveryCertMaterial, error) {
	a.calls.Add(1)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.called[cloudCertID]++
	if err, ok := a.errs[cloudCertID]; ok {
		return service.DiscoveryCertMaterial{}, err
	}
	if m, ok := a.material[cloudCertID]; ok {
		return m, nil
	}
	return service.DiscoveryCertMaterial{Exists: false}, nil
}

// Calls returns the total GetCertChain call count for this cloud.
func (a *StubMaterialAdapter) Calls() int32 { return a.calls.Load() }

// Material exposes the stub for a cloud ( panics on unknown clouds — a test
// addressing an out-of-vocabulary cloud is a fixture bug ).
func (h *Harness) Material(cloud domain.Cloud) *StubMaterialAdapter {
	h.t.Helper()
	a, ok := h.materials[cloud]
	require.True(h.t, ok, "no material adapter registered for cloud %q", cloud)
	return a
}

// MaterialGets returns the total material-channel GetCertChain calls across
// all clouds ( the "judgment/skip costs zero material Gets" invariant probe ).
func (h *Harness) MaterialGets() int {
	total := int32(0)
	for _, a := range h.materials {
		total += a.Calls()
	}
	return int(total)
}

// Lister exposes the stub lister for a cloud ( panics when the cloud was not
// registered via Config.Listers — capability-gap clouds stay unregistered ).
func (h *Harness) Lister(cloud domain.Cloud) *StubLibraryLister {
	h.t.Helper()
	l, ok := h.listers[cloud]
	require.True(h.t, ok, "no lister registered for cloud %q ( add it via Config.Listers )", cloud)
	return l
}

// StubAccountSource stubs service.ScanAccountSource ( active accounts by
// cloud, optional per-cloud read failures ).
type StubAccountSource struct {
	ByCloud    map[domain.Cloud][]*sharedomain.CloudAccount
	ErrByCloud map[domain.Cloud]error
}

// ActiveByCloud implements service.ScanAccountSource. A nil error entry in
// ErrByCloud means "cleared" — the account list is served normally.
func (s *StubAccountSource) ActiveByCloud(_ context.Context, cloud domain.Cloud) ([]*sharedomain.CloudAccount, error) {
	if err, ok := s.ErrByCloud[cloud]; ok && err != nil {
		return nil, err
	}
	return s.ByCloud[cloud], nil
}

// ActiveAccount builds one active cloud account.
func ActiveAccount(cloud domain.Cloud, name string) *sharedomain.CloudAccount {
	return &sharedomain.CloudAccount{
		Name:     name,
		Provider: sharedomain.CloudProvider(cloud),
		Status:   sharedomain.CloudAccountStatusActive,
	}
}

// CountingSessionRepo counts created import sessions ( zero-side-effect
// assertions for rejected requests and converged re-runs ) and remembers the
// last created session ID ( the scheduler job face discards the SyncRun, so
// operator attribution reads back through this handle ).
type CountingSessionRepo struct {
	*certtest.FakeDiscoveryImportSessionRepo
	created atomic.Int32

	mu     sync.Mutex
	lastID string
}

// Create counts then delegates.
func (c *CountingSessionRepo) Create(ctx context.Context, s *domain.DiscoveryImportSession) (string, error) {
	c.created.Add(1)
	id, err := c.FakeDiscoveryImportSessionRepo.Create(ctx, s)
	if err == nil {
		c.mu.Lock()
		c.lastID = id
		c.mu.Unlock()
	}
	return id, err
}

// Created returns the session creation count.
func (c *CountingSessionRepo) Created() int32 { return c.created.Load() }

// LastID returns the most recently created session ID ( empty when none ).
func (c *CountingSessionRepo) LastID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastID
}

// ---------------------------------------------------------------------
// Seeding helpers ( fixture-from-spec support )
// ---------------------------------------------------------------------

// SeedLedgerCert writes a fingerprint-only ledger certificate.
func (h *Harness) SeedLedgerCert(fingerprint string) domain.Certificate {
	h.t.Helper()
	cert := &domain.Certificate{
		Fingerprint:   fingerprint,
		CommonName:    "seed-" + fingerprint[:8],
		HostingStatus: domain.HostingStatusFingerprintOnly,
	}
	require.NoError(h.t, h.Certs.Create(context.Background(), cert))
	return *cert
}

// SeedMapping writes an active cloud certificate mapping row; an optional
// final argument pins uploadedAt ( drift reverse-lookup ordering fixtures ).
func (h *Harness) SeedMapping(fingerprint, cloud, accountKey, cloudCertID string, uploadedAt ...time.Time) {
	h.t.Helper()
	m := &domain.CloudCertMapping{
		CertFingerprint: fingerprint,
		Cloud:           cloud,
		AccountKey:      accountKey,
		CloudCertID:     cloudCertID,
	}
	if len(uploadedAt) > 0 {
		m.UploadedAt = uploadedAt[0]
	}
	require.NoError(h.t, h.Mappings.Upsert(context.Background(), m))
}

// SeedAccount registers one active account on a cloud's account source.
func (h *Harness) SeedAccount(cloud domain.Cloud, name string) {
	h.t.Helper()
	if h.accounts.ByCloud == nil {
		h.accounts.ByCloud = map[domain.Cloud][]*sharedomain.CloudAccount{}
	}
	h.accounts.ByCloud[cloud] = append(h.accounts.ByCloud[cloud], ActiveAccount(cloud, name))
}

// SeedCloudError injects a cloud-level account-read failure.
func (h *Harness) SeedCloudError(cloud domain.Cloud, err error) {
	if h.accounts.ErrByCloud == nil {
		h.accounts.ErrByCloud = map[domain.Cloud]error{}
	}
	h.accounts.ErrByCloud[cloud] = err
}

// Instance builds one cert-library instance ( judgment input metadata ).
func Instance(cloudCertID, fingerprint string) service.CertLibraryInstance {
	return service.CertLibraryInstance{CloudCertID: cloudCertID, Fingerprint: fingerprint}
}

// ---------------------------------------------------------------------
// Read-back helpers ( deep assertions )
// ---------------------------------------------------------------------

// Ledger returns all ledger certificates.
func (h *Harness) Ledger() []domain.Certificate {
	h.t.Helper()
	out, err := h.Certs.List(context.Background())
	require.NoError(h.t, err)
	return out
}

// LedgerByFP fetches one ledger certificate by fingerprint.
func (h *Harness) LedgerByFP(fingerprint string) (domain.Certificate, error) {
	h.t.Helper()
	return h.Certs.GetByFingerprint(context.Background(), fingerprint)
}

// ActiveMappings returns all active mapping rows ( total-count probe; every
// mapping this feature writes is active ).
func (h *Harness) ActiveMappings() []domain.CloudCertMapping {
	h.t.Helper()
	out, err := h.Mappings.ListByStatus(context.Background(), domain.MappingStatusActive)
	require.NoError(h.t, err)
	return out
}

// MappingsByFP returns mappings carrying a fingerprint.
func (h *Harness) MappingsByFP(fingerprint string) []domain.CloudCertMapping {
	h.t.Helper()
	out, err := h.Mappings.ListByFingerprint(context.Background(), fingerprint)
	require.NoError(h.t, err)
	return out
}

// LatestMappingByCloudCert resolves the newest mapping row for a cloud cert
// ID ( FindByCloudCertID uploadedAt-descending semantics ).
func (h *Harness) LatestMappingByCloudCert(cloud, accountKey, cloudCertID string) (domain.CloudCertMapping, error) {
	h.t.Helper()
	return h.Mappings.FindByCloudCertID(context.Background(), cloud, accountKey, cloudCertID)
}

// SessionByID reads back one import session document ( operator attribution
// and item-level assertions ).
func (h *Harness) SessionByID(sessionID string) (domain.DiscoveryImportSession, error) {
	h.t.Helper()
	return h.Sessions.GetByID(context.Background(), sessionID)
}

// ---------------------------------------------------------------------
// Shared fixture vocabulary
// ---------------------------------------------------------------------

// FP derives a deterministic distinct test fingerprint.
func FP(seed string) string {
	sum := sha256.Sum256([]byte("sync-test:" + seed))
	return hex.EncodeToString(sum[:])
}

// Static failure-reason texts ( fact CERT_SYNC_STATIC_REASONS /
// IMPORT_ITEM_ERROR_REASONS — asserted verbatim by the journey tests ).
const (
	ReasonAccountLoadFailed = "ACCOUNT_LOAD_FAILED: 云账号读取失败"
	ReasonListFailed        = "CERT_LIST_FAILED: 云证书库列举失败"
	ReasonJudgeFailed       = "INTERNAL_ERROR: 增量判定失败"
	ReasonMappingFailed     = "INTERNAL_ERROR: 映射补建失败"
	ReasonSyncTimeout       = "SESSION_TIMEOUT: 同步整体超时，剩余条目可重跑"
	ReasonSubmitFailed      = "INTERNAL_ERROR: 导入会话创建失败"

	ReasonGetCertFailed    = "CERT_GET_FAILED: 云证书读取失败"
	ReasonCertGone         = "CERT_GET_FAILED: 云侧已不存在"
	ReasonAlreadyInLedger  = "ALREADY_IN_LEDGER: 已在台账，已补建映射"
	ReasonUnsupportedCloud = "CERT_IMPORT_UNSUPPORTED: 该云证书暂不支持自动解析"
)

// RequireNoKeyMaterial asserts a response body leaks no private key material.
func RequireNoKeyMaterial(t *testing.T, body string) {
	t.Helper()
	require.NotContains(t, body, "PRIVATE KEY", "response must not carry key material")
}

// WaitForSignal blocks until the signal channel delivers or the bounded wait
// deadline elapses ( Golden Rule: no unbounded waits, no bare sleeps ).
func WaitForSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	deadline := time.Now().Add(WaitDeadline)
	for {
		select {
		case <-ch:
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", WaitDeadline, what)
		}
		time.Sleep(WaitInterval)
	}
}

// AwaitSyncResponse collects an asynchronous POST /sync result ( CAS race
// fixtures drive the manual face from a goroutine ).
func AwaitSyncResponse(t *testing.T, done <-chan *Response) *Response {
	t.Helper()
	select {
	case resp := <-done:
		return resp
	case <-time.After(WaitDeadline):
		t.Fatalf("in-flight sync round did not converge within %s", WaitDeadline)
		return nil
	}
}

// SyncRoundResult carries one service-face round outcome ( CAS race fixtures
// drive SyncCertificates/SyncCertificatesManual from a goroutine ).
type SyncRoundResult struct {
	Run service.SyncRun
	Err error
}

// AwaitSyncRun collects an asynchronous service-face round result.
func AwaitSyncRun(t *testing.T, done <-chan SyncRoundResult) SyncRoundResult {
	t.Helper()
	select {
	case res := <-done:
		return res
	case <-time.After(WaitDeadline):
		t.Fatalf("in-flight sync round did not converge within %s", WaitDeadline)
		return SyncRoundResult{}
	}
}
