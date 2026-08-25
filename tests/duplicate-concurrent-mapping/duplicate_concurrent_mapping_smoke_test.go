// @feature cert-cloud-discovery-import @api-functional
//
// Journey smoke test: duplicate-concurrent-mapping ( dual-channel preview
// -> multi-account import -> convergence -> replay idempotency ).
// Source journey:
// docs/features/cert-cloud-discovery-import/testing/duplicate-concurrent-mapping/journey.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package duplicate_concurrent_mapping

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDuplicateConcurrentMapping_Smoke walks the journey end to end.
func TestDuplicateConcurrentMapping_Smoke(t *testing.T) {
	h, _, b, _ := multiAccountWorld(t)

	// Step 1: dual-channel miss keeps both entries selectable.
	preview := h.Get(discoverytest.RoutePreview)
	require.Equal(t, http.StatusOK, preview.StatusCode, preview.Body)
	items := preview.PreviewItems(t)
	assert.Equal(t, false, discoverytest.FindPreviewItem(t, items, "aliyun", "acct-a", "cert-acc-a")["inLedger"])
	assert.Equal(t, false, discoverytest.FindPreviewItem(t, items, "aliyun", "acct-b", "cert-acc-b")["inLedger"])

	// Steps 2-4: the group imports; the duplicate entry backfills a mapping.
	final := importAndAwait(t, h,
		discoverytest.ImportItem("aliyun", "acct-a", "cert-acc-a"),
		discoverytest.ImportItem("aliyun", "acct-b", "cert-acc-b"),
	)
	assert.Equal(t, "completed", final["status"])

	// Step 5: convergence plus replay idempotency.
	assert.Len(t, h.Ledger(), 1)
	assert.Len(t, h.MappingsByFP(b.Fingerprint), 2)

	replay := importAndAwait(t, h,
		discoverytest.ImportItem("aliyun", "acct-a", "cert-acc-a"),
		discoverytest.ImportItem("aliyun", "acct-b", "cert-acc-b"),
	)
	assert.Equal(t, "completed", replay["status"])
	assert.Len(t, h.Ledger(), 1, "replay adds nothing")
	assert.Len(t, h.MappingsByFP(b.Fingerprint), 2)

	// Error path through the journey: missing triple field.
	bad := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "", "cert-acc-a"),
	))
	assert.Equal(t, http.StatusBadRequest, bad.StatusCode, bad.Body)
}
