// @feature cert-cloud-discovery-import @api-functional
//
// Package discoverytest provides the hermetic API-functional test harness for
// the cert-cloud-discovery-import feature ( journeys under tests/<journey>/ ).
//
// The harness wires the production HTTP surface of the feature over in-memory
// repositories and stubbed cloud ports:
//
//	engine: gin.Recovery -> authGate (EIAM auth stub) -> CertRoleMiddleware
//	group:  /api/v1/certs  (reference + discovery handlers, production order)
//
// Only the upstream EIAM authentication middleware is stubbed: requests
// without a session are rejected with 401 semantics before any role or
// business logic runs ( mirroring production "auth precedes role" ordering );
// authenticated requests carry a memory session whose claims feed the real
// CertRoleMiddleware claims-to-role mapping and the real RequireRoles guards
// mounted by the handlers under test.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package discoverytest

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
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/internal/cert/web"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/ecodeclub/ginx/session"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Named timeout constants ( Golden Rule: no bare timeout literals ).
const (
	// HTTPClientTimeout bounds every request the harness issues.
	HTTPClientTimeout = 10 * time.Second
	// PollTerminalDeadline bounds import-session polling loops.
	PollTerminalDeadline = 5 * time.Second
	// PollInterval is the wait between two poll iterations.
	PollInterval = 10 * time.Millisecond
	// PreviewResponseBudget mirrors the contract invariant "preview < 1s".
	PreviewResponseBudget = time.Second
)

// API route constants ( from .forge/fact-table.json ROUTE facts ).
const (
	RoutePreview        = "/api/v1/certs/discovery/preview"
	RouteSnapshotStatus = "/api/v1/certs/discovery/snapshot-status"
	RouteImport         = "/api/v1/certs/discovery/import"
	RouteImportSession  = "/api/v1/certs/discovery/import/"
	RouteCertScanSuffix = "/scan"
	RouteCertRefsSuffix = "/references"
)

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

// Config tunes a Harness. Zero-value fields fall back to defaults; Claims nil
// means "unauthenticated request" ( 401 at the auth gate ).
type Config struct {
	Claims      map[string]string
	Accounts    map[domain.Cloud][]*sharedomain.CloudAccount
	AccountErrs map[domain.Cloud]error
	Adapters    []service.DiscoveryCertAdapter
	Scan        service.ScanTriggerPort
	// WrapCerts / WrapMappings wrap the base in-memory repositories handed to
	// the services ( fault injection ) while the harness keeps the base fakes
	// for seeding and read-back assertions.
	WrapCerts    func(base *certtest.FakeCertificateRepo) domain.CertificateRepository
	WrapMappings func(base *certtest.FakeCloudCertMappingRepo) domain.CloudCertMappingRepository
}

// DefaultConfig returns the default configuration: an ops-engineer session
// ( explicit cert_role claim ) and no cloud adapters / accounts.
func DefaultConfig() Config {
	return Config{Claims: map[string]string{
		"cert_role": "ops_engineer",
		"username":  "ops-engineer",
	}}
}

// Harness owns one isolated world: in-memory repositories, stubbed cloud
// ports, a gin engine with the production middleware chain, and an
// httptest.Server driving real HTTP requests.
type Harness struct {
	t  *testing.T
	s  *httptest.Server
	cl *http.Client

	Certs    *certtest.FakeCertificateRepo
	Refs     *certtest.FakeCertReferenceRepo
	Snaps    *certtest.FakeScanSnapshotRepo
	Mappings *certtest.FakeCloudCertMappingRepo

	// sessions is the counting wrapper around the base session fake.
	sessions *CountingSessionRepo
	// Scan is the stub scan trigger when the default stub is used.
	Scan *StubScanTrigger
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
		t:        t,
		Certs:    certtest.NewFakeCertificateRepo(),
		Refs:     certtest.NewFakeCertReferenceRepo(),
		Snaps:    certtest.NewFakeScanSnapshotRepo(),
		Mappings: certtest.NewFakeCloudCertMappingRepo(),
		sessions: &CountingSessionRepo{FakeDiscoveryImportSessionRepo: certtest.NewFakeDiscoveryImportSessionRepo()},
	}

	certRepo := domain.CertificateRepository(h.Certs)
	if cfg.WrapCerts != nil {
		certRepo = cfg.WrapCerts(h.Certs)
	}
	mappingRepo := domain.CloudCertMappingRepository(h.Mappings)
	if cfg.WrapMappings != nil {
		mappingRepo = cfg.WrapMappings(h.Mappings)
	}

	scan := cfg.Scan
	if scan == nil {
		h.Scan = &StubScanTrigger{}
		h.Scan.bind(h.Snaps, h.Refs)
		scan = h.Scan
	} else if stub, ok := scan.(*StubScanTrigger); ok {
		h.Scan = stub
		h.Scan.bind(h.Snaps, h.Refs)
	}

	accounts := &StubAccountSource{ByCloud: cfg.Accounts, ErrByCloud: cfg.AccountErrs}
	querySvc := service.NewReferenceQueryService(certRepo, h.Refs, h.Snaps, scan)
	previewSvc := service.NewDiscoveryPreviewService(h.Snaps, h.Refs, certRepo, mappingRepo)
	importSvc := service.NewDiscoveryImportService(h.sessions, certRepo, mappingRepo, h.Refs, cfg.Adapters, accounts)

	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(authGate(cfg.Claims))
	group := engine.Group("/api/v1/certs", web.CertRoleMiddleware())
	web.NewReferenceHandler(querySvc).RegisterRoutes(group)
	web.NewDiscoveryHandler(previewSvc, importSvc).RegisterRoutes(group)

	h.s = httptest.NewServer(engine)
	t.Cleanup(h.s.Close)
	h.cl = &http.Client{Timeout: HTTPClientTimeout}
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
func (h *Harness) URL(path string) string { return h.s.URL + path }

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
	resp, err := h.cl.Do(req)
	require.NoError(h.t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(h.t, err)
	out := &Response{StatusCode: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), Body: string(body)}
	require.NoError(h.t, json.Unmarshal(body, &out.Env), "response is JSON envelope: %s", out.Body)
	return out
}

// RequireJSON asserts the transport contract: JSON content type + envelope.
func (r *Response) RequireJSON(t *testing.T) {
	t.Helper()
	require.True(t, strings.HasPrefix(r.ContentType, "application/json"),
		"content type %q must be application/json", r.ContentType)
}

// DataMap decodes Env.Data into a generic map ( nil when the envelope
// carries no data, e.g. auth-gate rejections ).
func (r *Response) DataMap(t *testing.T) map[string]any {
	t.Helper()
	if len(r.Env.Data) == 0 {
		return nil
	}
	var m map[string]any
	require.NoError(t, json.Unmarshal(r.Env.Data, &m), "data is a JSON object: %s", r.Body)
	return m
}

// PreviewItems extracts the preview entries array from a preview response
// ( data is an object with a nested items array, never null ).
func (r *Response) PreviewItems(t *testing.T) []any {
	t.Helper()
	data := r.DataMap(t)
	require.NotNil(t, data, "preview data object missing: %s", r.Body)
	items, ok := data["items"].([]any)
	require.True(t, ok, "preview data.items is an array: %s", r.Body)
	return items
}

// DataSlice decodes Env.Data into a generic array.
func (r *Response) DataSlice(t *testing.T) []any {
	t.Helper()
	if len(r.Env.Data) == 0 {
		return nil
	}
	var s []any
	require.NoError(t, json.Unmarshal(r.Env.Data, &s), "data is a JSON array: %s", r.Body)
	return s
}

// PollImportTerminal polls the session progress endpoint until the session
// leaves the running state ( condition-driven wait bounded by a deadline ),
// returning the terminal payload.
func (h *Harness) PollImportTerminal(sessionID string) map[string]any {
	h.t.Helper()
	deadline := time.Now().Add(PollTerminalDeadline)
	for {
		resp := h.Get(RouteImportSession + sessionID)
		require.Equal(h.t, http.StatusOK, resp.StatusCode, resp.Body)
		data := resp.DataMap(h.t)
		if data["status"] != "running" {
			return data
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("discovery import session %s did not reach terminal state within %s: %v",
				sessionID, PollTerminalDeadline, data)
		}
		time.Sleep(PollInterval)
	}
}

// SessionsCreated returns how many import sessions were persisted.
func (h *Harness) SessionsCreated() int32 { return h.sessions.Created() }

// ---------------------------------------------------------------------
// Seeding helpers ( fixture-from-spec support )
// ---------------------------------------------------------------------

// SeedDoneSnapshotAt creates a done snapshot with the given startedAt.
func (h *Harness) SeedDoneSnapshotAt(startedAt time.Time) string {
	h.t.Helper()
	id, err := h.Snaps.Create(context.Background(), &domain.ScanSnapshot{StartedAt: startedAt})
	require.NoError(h.t, err)
	require.NoError(h.t, h.Snaps.MarkFinished(context.Background(), id, domain.ScanStatusDone, ""))
	return id
}

// SeedRunningSnapshotAt creates a running snapshot with the given startedAt.
func (h *Harness) SeedRunningSnapshotAt(startedAt time.Time) string {
	h.t.Helper()
	id, err := h.Snaps.Create(context.Background(), &domain.ScanSnapshot{StartedAt: startedAt})
	require.NoError(h.t, err)
	return id
}

// FinishScanDone advances a running snapshot to done ( scan completion ).
func (h *Harness) FinishScanDone(id string) {
	h.t.Helper()
	require.NoError(h.t, h.Snaps.MarkFinished(context.Background(), id, domain.ScanStatusDone, ""))
}

// SeedFailedSnapshot creates a failed snapshot carrying partial failures.
func (h *Harness) SeedFailedSnapshot(startedAt time.Time, failReason string, partials []domain.ScanChannelFailure) string {
	h.t.Helper()
	id, err := h.Snaps.Create(context.Background(), &domain.ScanSnapshot{StartedAt: startedAt})
	require.NoError(h.t, err)
	require.NoError(h.t, h.Snaps.FinishScan(context.Background(), id, domain.ScanStatusFailed, failReason, nil, partials))
	return id
}

// RefSpec describes one cloud reference to seed into a snapshot.
type RefSpec struct {
	Cloud       domain.Cloud
	Product     domain.Product
	AccountKey  string
	CloudCertID string
	Fingerprint string
	ResourceID  string
	SnapshotID  string
}

// SeedRefs writes cloud references ( CertReference.belongs_to ScanSnapshot ).
func (h *Harness) SeedRefs(specs ...RefSpec) {
	h.t.Helper()
	refs := make([]domain.CertReference, 0, len(specs))
	for _, s := range specs {
		product := s.Product
		if product == "" {
			product = domain.ProductCDN
		}
		refs = append(refs, domain.CertReference{
			CertFingerprint:       s.Fingerprint,
			Cloud:                 s.Cloud,
			Product:               product,
			ResourceID:            s.ResourceID,
			ReferencedCloudCertID: s.CloudCertID,
			AccountKey:            s.AccountKey,
			SnapshotID:            s.SnapshotID,
		})
	}
	_, err := h.Refs.CreateMulti(context.Background(), refs)
	require.NoError(h.t, err)
}

// SeedCert writes a fingerprint-only ledger certificate and returns it.
func (h *Harness) SeedCert(fingerprint string, notAfter time.Time) domain.Certificate {
	h.t.Helper()
	cert := &domain.Certificate{
		Fingerprint:   fingerprint,
		CommonName:    "seed-" + fingerprint[:8],
		NotAfter:      notAfter,
		HostingStatus: domain.HostingStatusFingerprintOnly,
	}
	require.NoError(h.t, h.Certs.Create(context.Background(), cert))
	return *cert
}

// SeedMapping writes a cloud certificate mapping row.
func (h *Harness) SeedMapping(fingerprint, cloud, accountKey, cloudCertID string) {
	h.t.Helper()
	require.NoError(h.t, h.Mappings.Upsert(context.Background(), &domain.CloudCertMapping{
		CertFingerprint: fingerprint,
		Cloud:           cloud,
		AccountKey:      accountKey,
		CloudCertID:     cloudCertID,
	}))
}

// Ledger returns all ledger certificates ( read-back assertions ).
func (h *Harness) Ledger() []domain.Certificate {
	h.t.Helper()
	out, err := h.Certs.List(context.Background())
	require.NoError(h.t, err)
	return out
}

// MappingsByFP returns mappings for a fingerprint.
func (h *Harness) MappingsByFP(fingerprint string) []domain.CloudCertMapping {
	h.t.Helper()
	out, err := h.Mappings.ListByFingerprint(context.Background(), fingerprint)
	require.NoError(h.t, err)
	return out
}

// RefsByFP returns cloud references carrying a fingerprint.
func (h *Harness) RefsByFP(fingerprint string) []domain.CertReference {
	h.t.Helper()
	out, err := h.Refs.ListByFingerprint(context.Background(), fingerprint)
	require.NoError(h.t, err)
	return out
}

// FP derives a deterministic distinct test fingerprint.
func FP(seed string) string {
	sum := sha256.Sum256([]byte("discovery-test:" + seed))
	return hex.EncodeToString(sum[:])
}

// PlaceholderFP re-derives the documented deterministic placeholder
// fingerprint ( fact PLACEHOLDER_FINGERPRINT_FORMULA:
// sha256hex("certscan-unresolved:"+cloud+"|"+accountKey+"|"+cloudCertID) ).
func PlaceholderFP(cloud, accountKey, cloudCertID string) string {
	sum := sha256.Sum256([]byte("certscan-unresolved:" + cloud + "|" + accountKey + "|" + cloudCertID))
	return hex.EncodeToString(sum[:])
}

// ImportItem builds one request item triple.
func ImportItem(cloud, accountKey, cloudCertID string) map[string]string {
	return map[string]string{"cloud": cloud, "accountKey": accountKey, "cloudCertId": cloudCertID}
}

// ImportBody builds a POST /discovery/import request body.
func ImportBody(items ...map[string]string) map[string]any {
	return map[string]any{"items": items}
}

// FindPreviewItem locates a preview entry by triple.
func FindPreviewItem(t *testing.T, items []any, cloud, accountKey, cloudCertID string) map[string]any {
	t.Helper()
	for _, raw := range items {
		it, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if it["cloud"] == cloud && it["accountKey"] == accountKey && it["cloudCertId"] == cloudCertID {
			return it
		}
	}
	t.Fatalf("preview entry %s/%s/%s not found in %v", cloud, accountKey, cloudCertID, items)
	return nil
}

// RequireNoKeyMaterial asserts a response body leaks no private key material.
func RequireNoKeyMaterial(t *testing.T, body string) {
	t.Helper()
	require.NotContains(t, body, "PRIVATE KEY", "response must not carry key material")
}

// ---------------------------------------------------------------------
// Cloud port stubs
// ---------------------------------------------------------------------

// StubCertAdapter is a per-cloud DiscoveryCertAdapter stub: cloudCertID ->
// material / error, with call accounting. Unconfigured IDs report
// Exists=false ( cert deleted cloud-side ).
type StubCertAdapter struct {
	cloud    domain.Cloud
	mu       sync.Mutex
	material map[string]service.DiscoveryCertMaterial
	errs     map[string]error
	calls    atomic.Int32
	called   map[string]int
}

// NewStubCertAdapter creates an empty stub for one cloud.
func NewStubCertAdapter(cloud domain.Cloud) *StubCertAdapter {
	return &StubCertAdapter{
		cloud:    cloud,
		material: map[string]service.DiscoveryCertMaterial{},
		errs:     map[string]error{},
		called:   map[string]int{},
	}
}

// Cloud reports the stub's cloud.
func (a *StubCertAdapter) Cloud() domain.Cloud { return a.cloud }

// AddMaterial registers in-cloud material for a cloudCertID.
func (a *StubCertAdapter) AddMaterial(cloudCertID string, m service.DiscoveryCertMaterial) *StubCertAdapter {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.material[cloudCertID] = m
	return a
}

// AddError registers a GetCert failure for a cloudCertID.
func (a *StubCertAdapter) AddError(cloudCertID string, err error) *StubCertAdapter {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.errs[cloudCertID] = err
	return a
}

// GetCertChain implements service.DiscoveryCertAdapter.
func (a *StubCertAdapter) GetCertChain(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) (service.DiscoveryCertMaterial, error) {
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

// Calls returns the total GetCertChain call count.
func (a *StubCertAdapter) Calls() int32 { return a.calls.Load() }

// StubAccountSource stubs service.ScanAccountSource ( active accounts by
// cloud, optional per-cloud read failures ).
type StubAccountSource struct {
	ByCloud    map[domain.Cloud][]*sharedomain.CloudAccount
	ErrByCloud map[domain.Cloud]error
}

// ActiveByCloud implements service.ScanAccountSource.
func (s *StubAccountSource) ActiveByCloud(_ context.Context, cloud domain.Cloud) ([]*sharedomain.CloudAccount, error) {
	if err, ok := s.ErrByCloud[cloud]; ok {
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

// StubScanMode selects StubScanTrigger behaviour.
type StubScanMode string

// Stub scan trigger modes.
const (
	// ScanModeAsync creates a running snapshot and returns it ( guidance
	// journey: frontend switches to polling ).
	ScanModeAsync StubScanMode = "async"
	// ScanModeInProgress reports a scan already running ( 409 path ).
	ScanModeInProgress StubScanMode = "in-progress"
)

// StubScanTrigger stubs service.ScanTriggerPort. In async mode it creates a
// running snapshot ( and optionally writes references into it ), mirroring
// the asynchronous orchestration the guidance journey polls on.
type StubScanTrigger struct {
	mu          sync.Mutex
	mode        StubScanMode
	refsToWrite []RefSpec
	calls       atomic.Int32
	lastSnap    string

	snaps domain.ScanSnapshotRepository
	refs  domain.CertReferenceRepository
}

// SetMode switches the stub mode.
func (s *StubScanTrigger) SetMode(m StubScanMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = m
}

// SetRefsToWrite configures the references the "scan" writes into its
// snapshot ( rescan semantics: unresolved triples are rebuilt from the
// deterministic placeholder formula by the caller ).
func (s *StubScanTrigger) SetRefsToWrite(refs []RefSpec) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refsToWrite = refs
}

// Calls returns the StartScan call count.
func (s *StubScanTrigger) Calls() int32 { return s.calls.Load() }

// LastSnapshotID returns the snapshot created by the last async StartScan.
func (s *StubScanTrigger) LastSnapshotID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSnap
}

// bind wires the shared in-memory repositories the stub acts on.
func (s *StubScanTrigger) bind(snaps domain.ScanSnapshotRepository, refs domain.CertReferenceRepository) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snaps = snaps
	s.refs = refs
}

// StartScan implements service.ScanTriggerPort.
func (s *StubScanTrigger) StartScan(ctx context.Context) (service.ScanResult, error) {
	s.calls.Add(1)
	s.mu.Lock()
	mode := s.mode
	refs := s.refsToWrite
	snaps := s.snaps
	refRepo := s.refs
	s.mu.Unlock()
	if mode == ScanModeInProgress || snaps == nil {
		return service.ScanResult{}, domain.ErrScanInProgress
	}
	id, err := snaps.Create(ctx, &domain.ScanSnapshot{StartedAt: time.Now()})
	if err != nil {
		return service.ScanResult{}, err
	}
	s.mu.Lock()
	s.lastSnap = id
	s.mu.Unlock()
	written := 0
	if len(refs) > 0 {
		specs := make([]RefSpec, len(refs))
		copy(specs, refs)
		for i := range specs {
			specs[i].SnapshotID = id
		}
		rows := make([]domain.CertReference, 0, len(specs))
		for _, sp := range specs {
			product := sp.Product
			if product == "" {
				product = domain.ProductCDN
			}
			rows = append(rows, domain.CertReference{
				CertFingerprint:       sp.Fingerprint,
				Cloud:                 sp.Cloud,
				Product:               product,
				ResourceID:            sp.ResourceID,
				ReferencedCloudCertID: sp.CloudCertID,
				AccountKey:            sp.AccountKey,
				SnapshotID:            id,
			})
		}
		n, err := refRepo.CreateMulti(ctx, rows)
		if err != nil {
			return service.ScanResult{}, err
		}
		written = n
	}
	return service.ScanResult{
		SnapshotID:        id,
		Status:            domain.ScanStatusRunning,
		ReferencesWritten: written,
	}, nil
}

// CountingSessionRepo counts created import sessions ( zero-side-effect
// assertions for rejected requests ).
type CountingSessionRepo struct {
	*certtest.FakeDiscoveryImportSessionRepo
	created atomic.Int32
}

// Create counts then delegates.
func (c *CountingSessionRepo) Create(ctx context.Context, s *domain.DiscoveryImportSession) (string, error) {
	c.created.Add(1)
	return c.FakeDiscoveryImportSessionRepo.Create(ctx, s)
}

// Created returns the session creation count.
func (c *CountingSessionRepo) Created() int32 { return c.created.Load() }
