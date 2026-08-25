// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/no-snapshot-guidance/contracts/step-4-done-enter-preview.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package no_snapshot_guidance

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep4_DoneEnterPreview_Success verifies the "success" outcome: once
// the polled snapshot is done with references landed, the preview closes
// the guidance loop.
func TestStep4_DoneEnterPreview_Success(t *testing.T) {
	h := discoverytest.NewHarness(t, nil)
	started := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	snapID := h.SeedDoneSnapshotAt(started)
	h.SeedRefs(discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-a", CloudCertID: "cert-A", Fingerprint: discoverytest.FP("guidance-done"), ResourceID: "res-1", SnapshotID: snapID})

	resp := h.Get(discoverytest.RoutePreview)
	resp.RequireJSON(t)
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	assert.True(t, resp.Env.Success)
	data := resp.DataMap(t)
	assert.Equal(t, snapID, data["snapshotId"])
	assert.Equal(t, started.UTC().Format(time.RFC3339), data["snapshotStartedAt"], "point-in-time field is carried")
	assert.Equal(t, float64(1), data["count"], "preview generates the unique list from the done snapshot")
	items := resp.PreviewItems(t)
	require.Len(t, items, 1)
	assert.Equal(t, "cert-A", items[0].(map[string]any)["cloudCertId"])
}

// TestStep4_DoneEnterPreview_PartialSuccessStillUsable verifies the
// "partial-success-still-usable" outcome: a done snapshot with partial
// failures still feeds the preview from the successfully landed references.
func TestStep4_DoneEnterPreview_PartialSuccessStillUsable(t *testing.T) {
	h := discoverytest.NewHarness(t, nil)
	started := time.Date(2026, 8, 24, 12, 30, 0, 0, time.UTC)
	id, err := h.Snaps.Create(context.Background(), &domain.ScanSnapshot{StartedAt: started})
	require.NoError(t, err)
	require.NoError(t, h.Snaps.FinishScan(context.Background(), id, domain.ScanStatusDone, "",
		nil, []domain.ScanChannelFailure{{Cloud: "huawei", Product: "cdn", Account: "acct-hw", Reason: "unsupported channel"}}))
	h.SeedRefs(discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-a", CloudCertID: "cert-A", Fingerprint: discoverytest.FP("guidance-partial"), ResourceID: "res-1", SnapshotID: id})

	preview := h.Get(discoverytest.RoutePreview)
	require.Equal(t, http.StatusOK, preview.StatusCode, preview.Body)
	data := preview.DataMap(t)
	assert.Equal(t, id, data["snapshotId"])
	assert.Equal(t, float64(1), data["count"], "landed references still feed the preview")
	assert.Equal(t, "cert-A", preview.PreviewItems(t)[0].(map[string]any)["cloudCertId"])
	assert.Len(t, preview.PreviewItems(t), 1)

	status := h.Get(discoverytest.RouteSnapshotStatus).DataMap(t)
	assert.Equal(t, "done", status["status"])
	partials := status["partialFailures"].([]any)
	require.Len(t, partials, 1, "partial failures remain queryable for rescan judgement")
	assert.Equal(t, "huawei", partials[0].(map[string]any)["cloud"])
}

// TestStep4_DoneEnterPreview_Unauthorized verifies the "unauthorized"
// outcome: anonymous preview request returns no list.
func TestStep4_DoneEnterPreview_Unauthorized(t *testing.T) {
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) { c.Claims = nil })
	resp := h.Get(discoverytest.RoutePreview)
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.False(t, resp.Env.Success)
	assert.Empty(t, resp.Env.Data)
}
