// @feature watchalert-subproject-integration @api-functional
//
// Contract: docs/features/watchalert-subproject-integration/testing/dev-subsystem-startup/contracts/step-2-backend-startup-success.md
//
// Step 2: 后端编译完成并在开发端口拉起
// Tests the backend HTTP health endpoint as an API-observable proxy for
// "backend started and running on dev port". The health endpoint
// (GET /api/v1/cam/health) is registered before auth middleware in
// ioc/gin.go and returns 200 {code:0, msg:"ok"} when the server is up.
package dev_subsystem_startup

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestStep2_BackendStartupSuccess verifies the "success" Outcome:
//
//	Preconditions: backend compiled, dev port free, no stale processes.
//	Input: developer waits for backend compile to finish.
//	Output: backend responds on dev port (HTTP probe succeeds), logs queryable.
//	State: backend running, logs observable.
func TestStep2_BackendStartupSuccess(t *testing.T) {
	h := newHarness(t, HarnessConfig{})

	resp := h.Get("/api/v1/cam/health")
	defer resp.Body.Close()

	// Structural: health endpoint returns 200.
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Behavioral: response body contains code=0 confirming healthy state.
	var body map[string]any
	assert.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, float64(0), body["code"], "health endpoint must report code=0")

	// Behavioral: msg field confirms "ok" state (not just non-empty).
	assert.Equal(t, "ok", body["msg"], "health endpoint must report msg=ok")

	// Behavioral: Content-Type is JSON (state correctness of response format).
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
}

// TestStep2_BackendPortConflict verifies the "backend-port-conflict-or-stale-process" Outcome:
//
//	Preconditions: backend dev port occupied or stale backend process present.
//	Input: developer runs unified start command and checks status.
//	Output: backend dev port HTTP probe fails or returns non-backend response,
//	        status command shows port conflict or stale process.
//	State: backend not started; developer must clean up and retry.
//
// In the e-cam-service API surface this is simulated by shutting down the
// test server and verifying the HTTP probe fails (connection refused).
func TestStep2_BackendPortConflict(t *testing.T) {
	h := newHarness(t, HarnessConfig{})

	// Simulate port conflict: close the server before probing.
	h.server.Close()

	resp, err := h.GetE("/api/v1/cam/health")

	// Behavioral: HTTP probe must fail (connection refused = port not bound
	// or occupied by another process). This is the observable signal that
	// the backend is NOT running on the expected port.
	assert.Error(t, err, "probe must fail when backend port is unavailable")
	assert.Nil(t, resp, "no response should be received when port is occupied")
}
