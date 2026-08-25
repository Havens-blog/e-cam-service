// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/no-snapshot-guidance/contracts/step-3-poll-snapshot-status.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package no_snapshot_guidance

import (
	"net/http"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep3_PollSnapshotStatus_Success verifies the "success" outcome: the
// status endpoint answers each poll with the latest snapshot fields and
// keeps answering while the scan runs ( no long-running request shape ).
func TestStep3_PollSnapshotStatus_Success(t *testing.T) {
	h := discoverytest.NewHarness(t, nil)
	started := time.Date(2026, 8, 24, 10, 30, 0, 0, time.UTC)
	runningID := h.SeedRunningSnapshotAt(started)

	for i := 0; i < 3; i++ {
		resp := h.Get(discoverytest.RouteSnapshotStatus)
		resp.RequireJSON(t)
		require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
		assert.True(t, resp.Env.Success)
		data := resp.DataMap(t)
		assert.Equal(t, true, data["hasSnapshot"])
		assert.Equal(t, runningID, data["snapshotId"])
		assert.Equal(t, "running", data["status"], "polling observes the running snapshot")
		assert.Equal(t, started.UTC().Format(time.RFC3339), data["startedAt"])
		partials, ok := data["partialFailures"].([]any)
		require.True(t, ok, "partialFailures is an array")
		assert.Empty(t, partials)
	}
}

// TestStep3_PollSnapshotStatus_FailedTerminalDetail verifies the
// "failed-terminal-detail" outcome: a failed terminal snapshot surfaces
// failReason plus the per-channel partial failure rows.
func TestStep3_PollSnapshotStatus_FailedTerminalDetail(t *testing.T) {
	h := discoverytest.NewHarness(t, nil)
	h.SeedFailedSnapshot(
		time.Date(2026, 8, 24, 11, 0, 0, 0, time.UTC),
		domain.FailReasonScanDiscoveryFailed,
		[]domain.ScanChannelFailure{
			{Cloud: "aliyun", Product: "cdn", Account: "acct-a", Reason: "list refs failed"},
			{Cloud: "tencent", Product: "waf", Account: "acct-t", Reason: "tls handshake failed"},
		},
	)

	resp := h.Get(discoverytest.RouteSnapshotStatus)
	resp.RequireJSON(t)
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	data := resp.DataMap(t)
	assert.Equal(t, true, data["hasSnapshot"])
	assert.Equal(t, "failed", data["status"])
	assert.Equal(t, domain.FailReasonScanDiscoveryFailed, data["failReason"])
	partials := data["partialFailures"].([]any)
	require.Len(t, partials, 2)
	first := partials[0].(map[string]any)
	assert.Equal(t, "aliyun", first["cloud"])
	assert.Equal(t, "cdn", first["product"])
	assert.Equal(t, "acct-a", first["account"])
	assert.Equal(t, "list refs failed", first["reason"])
	second := partials[1].(map[string]any)
	assert.Equal(t, "tencent", second["cloud"])
	discoverytest.RequireNoKeyMaterial(t, resp.Body)
}

// TestStep3_PollSnapshotStatus_Unauthorized verifies the "unauthorized"
// outcome: anonymous polling returns no snapshot state.
func TestStep3_PollSnapshotStatus_Unauthorized(t *testing.T) {
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) { c.Claims = nil })
	resp := h.Get(discoverytest.RouteSnapshotStatus)
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.False(t, resp.Env.Success)
	assert.Empty(t, resp.Env.Data)
}
