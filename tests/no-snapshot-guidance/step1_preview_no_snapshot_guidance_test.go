// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/no-snapshot-guidance/contracts/step-1-preview-no-snapshot-guidance.md
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

// TestStep1_PreviewNoSnapshotGuidance_Success verifies the "success"
// outcome: on a fresh system the preview returns the structured NO_SNAPSHOT
// guidance while the status endpoint reports the 200 empty state.
func TestStep1_PreviewNoSnapshotGuidance_Success(t *testing.T) {
	h := discoverytest.NewHarness(t, nil) // zero snapshots ( fixture state )

	preview := h.Get(discoverytest.RoutePreview)
	preview.RequireJSON(t)
	assert.Equal(t, http.StatusConflict, preview.StatusCode, preview.Body)
	assert.False(t, preview.Env.Success)
	require.NotNil(t, preview.Env.Error)
	assert.Equal(t, "NO_SNAPSHOT", preview.Env.Error.Code, "structured guidance code, never a 500")
	assert.Equal(t, "no completed scan snapshot available; run a scan first", preview.Env.Error.Message,
		"fixed safe guidance message")

	status := h.Get(discoverytest.RouteSnapshotStatus)
	status.RequireJSON(t)
	require.Equal(t, http.StatusOK, status.StatusCode, status.Body)
	assert.True(t, status.Env.Success)
	data := status.DataMap(t)
	assert.Equal(t, false, data["hasSnapshot"], "first-scan guidance: 200 empty state, distinct from preview 409")
	partials, ok := data["partialFailures"].([]any)
	require.True(t, ok, "partialFailures is an array, not null")
	assert.Empty(t, partials)
}

// TestStep1_PreviewNoSnapshotGuidance_FailedHistoryStillNoDone verifies the
// "failed-history-still-no-done" outcome: a failed snapshot history does
// not satisfy the preview; its partial failures stay visible on the status
// endpoint.
func TestStep1_PreviewNoSnapshotGuidance_FailedHistoryStillNoDone(t *testing.T) {
	h := discoverytest.NewHarness(t, nil)
	h.SeedFailedSnapshot(
		time.Date(2026, 8, 24, 7, 0, 0, 0, time.UTC),
		domain.FailReasonScanDiscoveryFailed,
		[]domain.ScanChannelFailure{{Cloud: "aliyun", Product: "cdn", Account: "acct-a", Reason: "list refs failed"}},
	)

	preview := h.Get(discoverytest.RoutePreview)
	assert.Equal(t, http.StatusConflict, preview.StatusCode, preview.Body)
	require.NotNil(t, preview.Env.Error)
	assert.Equal(t, "NO_SNAPSHOT", preview.Env.Error.Code, "no done snapshot exists, only failed history")

	status := h.Get(discoverytest.RouteSnapshotStatus)
	require.Equal(t, http.StatusOK, status.StatusCode, status.Body)
	data := status.DataMap(t)
	assert.Equal(t, true, data["hasSnapshot"])
	assert.Equal(t, "failed", data["status"])
	assert.Equal(t, domain.FailReasonScanDiscoveryFailed, data["failReason"])
	partials := data["partialFailures"].([]any)
	require.Len(t, partials, 1)
	p := partials[0].(map[string]any)
	assert.Equal(t, "aliyun", p["cloud"])
	assert.Equal(t, "acct-a", p["account"])
	assert.Equal(t, "list refs failed", p["reason"], "failed-scan details stay queryable for guidance")
}

// TestStep1_PreviewNoSnapshotGuidance_Unauthorized verifies the
// "unauthorized" outcome: anonymous callers never reach the guidance logic.
func TestStep1_PreviewNoSnapshotGuidance_Unauthorized(t *testing.T) {
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) { c.Claims = nil })
	resp := h.Get(discoverytest.RoutePreview)
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.False(t, resp.Env.Success)
	assert.Empty(t, resp.Env.Data)
}
