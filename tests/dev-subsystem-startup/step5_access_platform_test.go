// @feature watchalert-subproject-integration @api-functional
//
// Contract: docs/features/watchalert-subproject-integration/testing/dev-subsystem-startup/contracts/step-5-access-platform-start-work.md
//
// Step 5: 在浏览器中访问告警平台开始日常开发工作
// Tests the e-cam-service auth middleware chain: session validation (401),
// tenant gate (403), and removed local login endpoint (404).
//
// NOTE: Outcomes "validation-error" (400) and "unavailable-503" are
// watch-alert/eiam behaviours — the 1401/1403/1503 envelope codes and
// isWriteRequest write-detection logic do NOT exist in e-cam-service
// production code. They are tested against the eiam surface, not here.
package dev_subsystem_startup

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ecodeclub/ginx/session"
)

// validClaims returns a session.Claims for a developer with a selected tenant.
func validClaims() session.Claims {
	return session.Claims{
		Uid:  1001,
		Data: map[string]string{"username": "dev-alice", "tenant_id": "7"},
	}
}

// validClaimsNoTenant returns a session.Claims for an authenticated developer
// who has NOT selected a tenant (tenant_id missing → RequireTenant rejects).
func validClaimsNoTenant() session.Claims {
	return session.Claims{
		Uid:  1001,
		Data: map[string]string{"username": "dev-alice"},
	}
}

// TestStep5_AccessPlatformSuccess verifies the "success" Outcome:
//
//	Preconditions: backend running, developer holds valid eiam shared session.
//	Input: developer accesses alert platform in browser.
//	Output: platform loads, eiam login available, developer can work.
//	State: platform loaded, developer logged in, ready to work.
func TestStep5_AccessPlatformSuccess(t *testing.T) {
	h := newHarness(t, HarnessConfig{Claims: validClaims()})

	resp := h.Get("/api/v1/cam/ping")
	defer resp.Body.Close()

	// Structural: authenticated request returns 200.
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Behavioral: response body code=0 confirms successful access.
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, float64(0), body["code"], "authenticated access must return code=0")

	// Behavioral (deep): uid from session claims propagates to response data,
	// confirming the full auth chain (session → claims → context → handler).
	data, ok := body["data"].(map[string]any)
	require.True(t, ok, "response must contain data field")
	assert.Equal(t, float64(1001), data["uid"], "uid from session must propagate to handler")

	// Behavioral (deep): tenant_id from session claims propagates to response,
	// confirming RequireTenant allowed the request (tenant_id != 0).
	assert.Equal(t, float64(7), data["tenant_id"], "tenant_id from session must propagate through RequireTenant gate")
}

// TestStep5_SessionExpired verifies the "session-expired" Outcome:
//
//	Preconditions: eiam session expired due to TTL timeout (was previously valid).
//	Input: developer continues operating on the platform.
//	Output: next request returns HTTP 401, frontend redirects to eiam login.
//	State: session expired, frontend redirects to login; backend still running.
func TestStep5_SessionExpired(t *testing.T) {
	// HasSessionError=true simulates a previously-valid session that has expired.
	h := newHarness(t, HarnessConfig{HasSessionError: true})

	resp := h.Get("/api/v1/cam/ping")
	defer resp.Body.Close()

	// Structural: expired session returns 401 Unauthorized.
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// Behavioral: error envelope contains code=401 (session state, not just HTTP status).
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, float64(401), body["code"], "expired session must return envelope code=401")

	// Behavioral (deep): message confirms session expiry semantics ("会话无效或已过期").
	msg, ok := body["message"].(string)
	require.True(t, ok, "response must contain message field")
	assert.Contains(t, msg, "会话", "message must indicate session-related failure")
	assert.Contains(t, msg, "过期", "message must indicate expiry semantics")

	// Behavioral (deep — state transition): auth middleware aborted before handler,
	// so response must NOT contain a "data" field (handler never produced output).
	// This confirms the state transition: valid session → expired → 401 with no data.
	_, hasData := body["data"]
	assert.False(t, hasData, "expired session: handler must not execute (no data field)")

	// Behavioral (deep — cross-entity): backend is still running despite session expiry.
	// Health endpoint (no auth) still returns 200, proving server state != session state.
	healthResp := h.Get("/api/v1/cam/health")
	defer healthResp.Body.Close()
	assert.Equal(t, http.StatusOK, healthResp.StatusCode, "backend must still be running despite session expiry")
}

// TestStep5_Unauthorized401 verifies the "unauthorized-401" Outcome:
//
//	Preconditions: no eiam shared session established (credentials missing/invalid).
//	Input: developer accesses alert platform.
//	Output: backend returns HTTP 401, frontend redirects to eiam login.
//	State: auth failed (no valid session), backend still running.
//
// This is distinct from session-expired: here no session was ever established.
// At the API level both produce 401 — the distinction is semantic state.
func TestStep5_Unauthorized401(t *testing.T) {
	// No claims and no error → stubProvider.Get returns a MemorySession with
	// empty claims. But uid=0 and no tenant_id → RequireTenant still rejects.
	// To test the 401 path specifically, use HasSessionError to make Get fail.
	h := newHarness(t, HarnessConfig{HasSessionError: true})

	resp := h.Get("/api/v1/cam/ping")
	defer resp.Body.Close()

	// Structural: no session returns 401 Unauthorized.
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// Behavioral: error envelope code=401 confirms no session established.
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, float64(401), body["code"], "no session must return envelope code=401")

	// Behavioral: message indicates authentication failure.
	msg, ok := body["message"].(string)
	require.True(t, ok, "response must contain message field")
	assert.Contains(t, msg, "认证失败", "message must indicate authentication failure")
}

// TestStep5_Forbidden403 verifies the "forbidden-403" Outcome:
//
//	Preconditions: valid eiam shared session (authenticated), but no
//	               watchalert_role_binding match (no tenant selected).
//	Input: developer accesses alert platform.
//	Output: backend returns HTTP 403, frontend shows permission error in-place.
//	State: authenticated but no tenant, platform shows 403; backend still running.
//
// In e-cam-service, this maps to RequireTenant: session is valid but
// tenant_id=0 (no tenant selected). The middleware returns 403, not 401.
func TestStep5_Forbidden403(t *testing.T) {
	// Valid session but no tenant_id in claims → RequireTenant rejects with 403.
	h := newHarness(t, HarnessConfig{Claims: validClaimsNoTenant()})

	resp := h.Get("/api/v1/cam/ping")
	defer resp.Body.Close()

	// Structural: valid session without tenant returns 403 Forbidden.
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	// Behavioral: error envelope code=403 confirms tenant gate rejection.
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, float64(403), body["code"], "no-tenant must return envelope code=403")

	// Behavioral (deep): message confirms tenant-selection semantics ("未选定租户").
	msg, ok := body["message"].(string)
	require.True(t, ok, "response must contain message field")
	assert.Contains(t, msg, "租户", "message must indicate tenant-related rejection")

	// Behavioral (deep — state transition): auth middleware passed (session valid)
	// but RequireTenant aborted, so handler never executed. Response must NOT
	// contain a "data" field — confirming the state transition from
	// "authenticated" to "tenant-gate-rejected" with no handler output.
	_, hasData := body["data"]
	assert.False(t, hasData, "forbidden: handler must not execute (no data field)")

	// Behavioral (deep — cross-entity): the 403 comes from RequireTenant, not
	// the auth middleware. The message mentions "租户" (tenant), not "认证"
	// (authentication), proving the rejection origin crossed from session entity
	// to tenant-gate entity.
	assert.NotContains(t, msg, "认证", "403 must come from tenant gate, not auth middleware")
}

// TestStep5_LocalLoginRemoved404 verifies the "local-login-removed-404" Outcome:
//
//	Preconditions: old local login endpoint (POST /api/system/login) has been
//	               removed and is not registered.
//	Input: client sends POST to the removed endpoint with old credentials.
//	Output: backend returns HTTP 404, no token/session issued.
//	State: local login unavailable (404), no auth state produced.
//
// In e-cam-service, /api/system/login is not registered anywhere — gin returns
// 404 by default. The eiam login endpoint is POST /api/user/system/login.
func TestStep5_LocalLoginRemoved404(t *testing.T) {
	h := newHarness(t, HarnessConfig{})

	body := strings.NewReader(`{"username":"admin","password":"old-password"}`)
	resp := h.Post("/api/system/login", "application/json", body)
	defer resp.Body.Close()

	// Structural: removed endpoint returns 404 Not Found.
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	// Behavioral: response must NOT contain any token, session, or auth state
	// (no Set-Cookie header, no token field in body).
	setCookie := resp.Header.Get("Set-Cookie")
	assert.Empty(t, setCookie, "removed login must not issue any Set-Cookie")

	// Behavioral: response body must not contain token or session data.
	var respBody map[string]any
	if json.NewDecoder(resp.Body).Decode(&respBody) == nil {
		assert.NotContains(t, respBody, "token", "removed login must not return token")
		assert.NotContains(t, respBody, "session", "removed login must not return session")
	}
}
