// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/placeholder-fingerprint-backfill/contracts/step-1-placeholder-marked.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package placeholder_fingerprint_backfill

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep1_PlaceholderMarked_Success verifies the "success" outcome: a
// placeholder-fingerprint entry is selectable and flagged deferred_parse.
func TestStep1_PlaceholderMarked_Success(t *testing.T) {
	h, _, snapID := tencentWorld(t, "acct-tx")
	h.SeedRefs(placeholderRefSpec("tencent", "acct-tx", "ssl-9", "res-1", snapID))
	h.SeedRefs(placeholderRefSpec("tencent", "acct-tx", "ssl-10", "res-2", snapID))

	resp := h.Get(discoverytest.RoutePreview)
	resp.RequireJSON(t)
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	entry := discoverytest.FindPreviewItem(t, resp.PreviewItems(t), "tencent", "acct-tx", "ssl-9")
	assert.Equal(t, true, entry["parseable"], "placeholder entries stay selectable ( unlike huawei/IAM-hosted )")
	assert.Equal(t, "deferred_parse", entry["parseReason"], "marked as parse-at-import")
	assert.Equal(t, false, entry["inLedger"])
}

// TestStep1_PlaceholderMarked_RealFingerprintEntryUnmarked verifies the
// "real-fingerprint-entry-unmarked" outcome: real fingerprints carry no
// deferred marker ( the marker never spreads ).
func TestStep1_PlaceholderMarked_RealFingerprintEntryUnmarked(t *testing.T) {
	h, _, snapID := tencentWorld(t, "acct-tx")
	h.SeedRefs(discoverytest.RefSpec{
		Cloud: domain.CloudTencent, AccountKey: "acct-tx", CloudCertID: "ssl-real",
		Fingerprint: discoverytest.FP("phb-real"), ResourceID: "res-r", SnapshotID: snapID,
	})
	h.SeedRefs(placeholderRefSpec("tencent", "acct-tx", "ssl-9", "res-1", snapID))

	resp := h.Get(discoverytest.RoutePreview)
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	items := resp.PreviewItems(t)

	real := discoverytest.FindPreviewItem(t, items, "tencent", "acct-tx", "ssl-real")
	assert.Equal(t, true, real["parseable"])
	assert.Empty(t, real["parseReason"], "real fingerprint carries no deferred marker")

	ph := discoverytest.FindPreviewItem(t, items, "tencent", "acct-tx", "ssl-9")
	assert.Equal(t, "deferred_parse", ph["parseReason"], "the marker only hits the exact formula value")
}

// TestStep1_PlaceholderMarked_Unauthorized verifies the "unauthorized"
// outcome.
func TestStep1_PlaceholderMarked_Unauthorized(t *testing.T) {
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) { c.Claims = nil })
	resp := h.Get(discoverytest.RoutePreview)
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.False(t, resp.Env.Success)
	assert.Empty(t, resp.Env.Data)
}
