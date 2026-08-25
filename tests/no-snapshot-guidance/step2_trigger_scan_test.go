// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/no-snapshot-guidance/contracts/step-2-trigger-scan.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package no_snapshot_guidance

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// engineerClaims returns the whitelisted OpsEngineer session claims.
func engineerClaims() map[string]string {
	return map[string]string{"cert_role": "ops_engineer", "username": "ops-engineer"}
}

// scanWorld builds a world with an active aliyun account and a ledger
// certificate ( the scan trigger endpoint locates the certificate first ).
// claims follows the harness Config semantics: nil means unauthenticated.
func scanWorld(t *testing.T, claims map[string]string) (*discoverytest.Harness, string) {
	t.Helper()
	aliyun := discoverytest.NewStubCertAdapter(domain.CloudAliyun)
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) {
		c.Claims = claims
		c.Adapters = []service.DiscoveryCertAdapter{aliyun}
		c.Accounts = map[domain.Cloud][]*sharedomain.CloudAccount{
			domain.CloudAliyun: {discoverytest.ActiveAccount(domain.CloudAliyun, "acct-a")},
		}
	})
	cert := h.SeedCert(discoverytest.FP("guidance-cert"), time.Date(2027, 12, 1, 0, 0, 0, 0, time.UTC))
	return h, cert.ID.Hex()
}

// scanRoute builds the immediate-scan endpoint for a certificate.
func scanRoute(certID string) string {
	return "/api/v1/certs/" + certID + discoverytest.RouteCertScanSuffix
}

// TestStep2_TriggerScan_Success verifies the "success" outcome: triggering
// the guided scan starts a scan and produces a running snapshot the
// guidance loop can poll.
func TestStep2_TriggerScan_Success(t *testing.T) {
	h, certID := scanWorld(t, engineerClaims())

	resp := h.Post(scanRoute(certID), nil)
	resp.RequireJSON(t)
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	assert.True(t, resp.Env.Success)
	data := resp.DataMap(t)
	snapshotID, _ := data["snapshotId"].(string)
	require.NotEmpty(t, snapshotID, "scan trigger returns the snapshot handle")
	assert.Equal(t, "running", data["status"], "a running snapshot is produced for polling")
	assert.Equal(t, float64(0), data["channelsFailed"], "no channel failures on the guided scan")

	// The guidance loop sees the running snapshot immediately.
	status := h.Get(discoverytest.RouteSnapshotStatus)
	require.Equal(t, http.StatusOK, status.StatusCode, status.Body)
	sData := status.DataMap(t)
	assert.Equal(t, true, sData["hasSnapshot"])
	assert.Equal(t, "running", sData["status"])
	assert.Equal(t, snapshotID, sData["snapshotId"], "status endpoint reports the triggered snapshot ( cross-entity )")
}

// TestStep2_TriggerScan_ScanAlreadyRunning verifies the
// "scan-already-running" outcome: a second trigger while a scan is in
// flight returns 409 SCAN_IN_PROGRESS with the in-flight snapshot info and
// starts no new scan.
func TestStep2_TriggerScan_ScanAlreadyRunning(t *testing.T) {
	h, certID := scanWorld(t, engineerClaims())
	runningID := h.SeedRunningSnapshotAt(time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC))
	h.Scan.SetMode(discoverytest.ScanModeInProgress)

	resp := h.Post(scanRoute(certID), nil)
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusConflict, resp.StatusCode, resp.Body)
	assert.False(t, resp.Env.Success)
	require.NotNil(t, resp.Env.Error)
	assert.Equal(t, "SCAN_IN_PROGRESS", resp.Env.Error.Code)

	var meta map[string]any
	require.NoError(t, json.Unmarshal(resp.Env.Meta, &meta), "409 carries in-flight snapshot meta")
	assert.Equal(t, runningID, meta["snapshotId"], "meta points at the existing running snapshot")
	assert.NotEmpty(t, meta["startedAt"])

	// No second running snapshot was created: the status endpoint still
	// reports the original in-flight snapshot.
	status := h.Get(discoverytest.RouteSnapshotStatus).DataMap(t)
	assert.Equal(t, runningID, status["snapshotId"], "the original running snapshot is still the latest")
	assert.Equal(t, "running", status["status"])
}

// TestStep2_TriggerScan_Unauthorized verifies the "unauthorized" outcome:
// anonymous trigger starts nothing.
func TestStep2_TriggerScan_Unauthorized(t *testing.T) {
	_, certID := scanWorld(t, engineerClaims())
	unauth := discoverytest.NewHarness(t, func(c *discoverytest.Config) { c.Claims = nil })
	// Anonymous requests hit a fresh world; reuse the route shape only.
	resp := unauth.Post(scanRoute(certID), nil)
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.False(t, resp.Env.Success)
	assert.Nil(t, resp.Env.Data)
	assert.Equal(t, int32(0), unauth.Scan.Calls(), "no scan orchestration starts unauthenticated")
}
