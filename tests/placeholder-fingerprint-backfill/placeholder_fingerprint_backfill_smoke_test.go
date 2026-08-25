// @feature cert-cloud-discovery-import @api-functional
//
// Journey smoke test: placeholder-fingerprint-backfill ( preview marker ->
// import resolves real fingerprint -> backfill -> reference list ).
// Source journey:
// docs/features/cert-cloud-discovery-import/testing/placeholder-fingerprint-backfill/journey.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package placeholder_fingerprint_backfill

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPlaceholderFingerprintBackfill_Smoke walks the journey end to end.
func TestPlaceholderFingerprintBackfill_Smoke(t *testing.T) {
	h, tencent, snapID := tencentWorld(t, "acct-tx")
	b := certtest.NewBundle(t, "www.phb-smoke.com", []string{"www.phb-smoke.com"}, nil)
	tencent.AddMaterial("ssl-9", goodMaterial(b))
	h.SeedRefs(placeholderRefSpec("tencent", "acct-tx", "ssl-9", "res-1", snapID))

	// Step 1: the placeholder entry is marked parse-at-import and selectable.
	preview := h.Get(discoverytest.RoutePreview)
	require.Equal(t, http.StatusOK, preview.StatusCode, preview.Body)
	entry := discoverytest.FindPreviewItem(t, preview.PreviewItems(t), "tencent", "acct-tx", "ssl-9")
	assert.Equal(t, true, entry["parseable"])
	assert.Equal(t, "deferred_parse", entry["parseReason"])

	// Steps 2-4: import resolves the real fingerprint and backfills.
	final := importOne(t, h, "tencent", "acct-tx", "ssl-9")
	assert.Equal(t, "completed", final["status"])
	item := final["items"].([]any)[0].(map[string]any)
	assert.Equal(t, "success", item["result"])
	certID, _ := item["mappedCertId"].(string)
	require.NotEmpty(t, certID)
	require.Len(t, h.RefsByFP(b.Fingerprint), 1, "reference backfilled to the real fingerprint")
	assert.Len(t, h.Ledger(), 1)

	// Step 5: the certificate reference list shows the linked reference.
	refs := h.Get("/api/v1/certs/" + certID + discoverytest.RouteCertRefsSuffix)
	require.Equal(t, http.StatusOK, refs.StatusCode, refs.Body)
	assert.Equal(t, float64(1), refs.DataMap(t)["refCount"])

	// Error path through the journey: empty import list.
	bad := h.Post(discoverytest.RouteImport, discoverytest.ImportBody())
	assert.Equal(t, http.StatusBadRequest, bad.StatusCode, bad.Body)
}
