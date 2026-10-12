// @feature watchalert-subproject-integration @api-functional
//
// Package dev_subsystem_startup provides the hermetic API-functional test
// harness for the dev-subsystem-startup journey (e-cam-service surface).
//
// The harness spins an in-process gin engine with the real auth middleware
// (EcmdbAuthMiddlewareWithConfig) backed by a stub session provider, plus the
// real RequireTenant gate. A health endpoint and a protected ping endpoint are
// registered to exercise the full middleware chain without requiring external
// services.
//
// Outcomes that are NOT e-cam-service API-observable (Step 1 unified start
// command, Step 3 frontend startup, Step 6 quality gate) are not generated
// here — they are dev-tooling/CLI behaviours covered by other surfaces.
// Step 5 outcomes validation-error and unavailable-503 are watch-alert/eiam
// behaviours (1401/1403/1503 envelope codes do not exist in e-cam-service
// production code) and are tested against the eiam surface, not here.
package dev_subsystem_startup

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/ecodeclub/ginx/gctx"
	"github.com/ecodeclub/ginx/session"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/require"
)

// HTTPClientTimeout is the upper bound for every HTTP request in tests.
const HTTPClientTimeout = 5 * time.Second

// stubProvider is a controllable session.Provider for hermetic auth tests.
// Set err to non-nil to simulate an expired/missing session (→ 401).
// Set claims to non-nil to simulate a valid authenticated session.
type stubProvider struct {
	claims session.Claims
	err    error
}

func (p *stubProvider) NewSession(_ *gctx.Context, _ int64, _ map[string]string, _ map[string]any) (session.Session, error) {
	return nil, errors.New("not implemented in stub")
}

func (p *stubProvider) Get(_ *gctx.Context) (session.Session, error) {
	if p.err != nil {
		return nil, p.err
	}
	return session.NewMemorySession(p.claims), nil
}

func (p *stubProvider) Destroy(_ *gctx.Context) error { return nil }

func (p *stubProvider) UpdateClaims(_ *gctx.Context, _ session.Claims) error { return nil }

func (p *stubProvider) RenewAccessToken(_ *gctx.Context) error { return nil }

// HarnessConfig controls which auth state the test server simulates.
type HarnessConfig struct {
	// Claims: nil → no session (401); non-nil → valid session with given claims.
	Claims session.Claims
	// HasSessionError: true → session.Get returns error (expired/invalid session).
	HasSessionError bool
}

// Harness wraps a test gin engine + httptest server.
type Harness struct {
	t        *testing.T
	engine   *gin.Engine
	server   *httptest.Server
	provider *stubProvider
}

// newHarness builds a test server with the real auth middleware chain.
// The health endpoint is registered BEFORE auth (whitelisted), the protected
// ping endpoint is registered AFTER auth + RequireTenant.
func newHarness(t *testing.T, cfg HarnessConfig) *Harness {
	t.Helper()
	gin.SetMode(gin.TestMode)

	sp := &stubProvider{
		claims: cfg.Claims,
	}
	if cfg.HasSessionError {
		sp.err = errors.New("session expired or invalid")
	}

	engine := gin.New()
	engine.Use(gin.Recovery())

	// Health endpoint — registered before auth, mimics ioc/gin.go lines 58-61.
	engine.GET("/api/v1/cam/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok"})
	})

	// Auth middleware — real implementation from internal/shared/middleware.
	engine.Use(middleware.EcmdbAuthMiddlewareWithConfig(sp, middleware.AuthConfig{}, elog.DefaultLogger))

	// Protected endpoint group with RequireTenant — mimics ioc/gin.go tenantScoped().
	protected := engine.Group("/api/v1/cam")
	protected.Use(middleware.RequireTenant(elog.DefaultLogger))
	protected.GET("/ping", func(c *gin.Context) {
		uid := middleware.GetUid(c)
		tenantID := middleware.GetTenantID(c)
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "pong",
			"data": gin.H{"uid": uid, "tenant_id": tenantID},
		})
	})

	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	return &Harness{
		t:        t,
		engine:   engine,
		server:   server,
		provider: sp,
	}
}

// URL returns the full URL for a given path on the test server.
func (h *Harness) URL(path string) string {
	return h.server.URL + path
}

// Get issues a GET request with an Accept header and returns the response.
func (h *Harness) Get(path string) *http.Response {
	h.t.Helper()
	resp, err := h.GetE(path)
	require.NoError(h.t, err)
	return resp
}

// GetE issues a GET request and returns both response and error (for tests
// that expect the request to fail, e.g. port-conflict simulation).
func (h *Harness) GetE(path string) (*http.Response, error) {
	h.t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.URL(path), nil)
	require.NoError(h.t, err)
	req.Header.Set("Accept", "application/json")
	cl := &http.Client{Timeout: HTTPClientTimeout}
	return cl.Do(req)
}

// Post issues a POST request with the given body and returns the response.
func (h *Harness) Post(path, contentType string, body io.Reader) *http.Response {
	h.t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.URL(path), body)
	require.NoError(h.t, err)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	cl := &http.Client{Timeout: HTTPClientTimeout}
	resp, err := cl.Do(req)
	require.NoError(h.t, err)
	return resp
}
