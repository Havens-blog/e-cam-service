// @feature cert-cloud-discovery-import @api-functional
//
// Journey smoke test: unsupported-entries-skip ( preview unselectable group
// -> confirm mixed list -> per-item skip -> terminal reasons + one error
// path ). Source journey:
// docs/features/cert-cloud-discovery-import/testing/unsupported-entries-skip/journey.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package unsupported_entries_skip

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnsupportedEntriesSkip_Smoke walks the journey end to end.
func TestUnsupportedEntriesSkip_Smoke(t *testing.T) {
	h, aliyun, _, _ := mixedWorld(t)
	b := certtest.NewBundle(t, "www.skip-smoke.com", []string{"www.skip-smoke.com"}, nil)
	aliyun.AddMaterial("cert-A", goodMaterial(b))

	// Step 1: the unselectable group is visible in the preview.
	preview := h.Get(discoverytest.RoutePreview)
	require.Equal(t, http.StatusOK, preview.StatusCode, preview.Body)
	items := preview.PreviewItems(t)
	assert.Equal(t, false, discoverytest.FindPreviewItem(t, items, "huawei", "acct-hw", "scm-1")["parseable"])
	assert.Equal(t, false, discoverytest.FindPreviewItem(t, items, "aws", "acct-aws", "iam-123")["parseable"])

	// Step 2-4: forced mixed submission converges with static reasons.
	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-A"),
		discoverytest.ImportItem("huawei", "acct-hw", "scm-1"),
	))
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	final := h.PollImportTerminal(resp.DataMap(t)["sessionId"].(string))
	assert.Equal(t, "partial_failed", final["status"])
	progress := final["progress"].(map[string]any)
	assert.Equal(t, float64(1), progress["succeeded"])
	assert.Equal(t, float64(1), progress["failed"])

	items = final["items"].([]any)
	assert.Equal(t, "success", items[0].(map[string]any)["result"])
	assert.Equal(t, reasonUnsupportedCloud, items[1].(map[string]any)["errorReason"])
	require.Len(t, h.Ledger(), 1, "exactly the parseable entry registered")

	// The registered state is visible on a refreshed preview.
	refreshed := h.Get(discoverytest.RoutePreview).PreviewItems(t)
	assert.Equal(t, true, discoverytest.FindPreviewItem(t, refreshed, "aliyun", "acct-a", "cert-A")["inLedger"])

	// Error path through the journey: malformed item triple.
	bad := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aws", "acct-aws", " "),
	))
	assert.Equal(t, http.StatusBadRequest, bad.StatusCode, bad.Body)
	require.NotNil(t, bad.Env.Error)
	assert.Equal(t, "INVALID_REQUEST", bad.Env.Error.Code)
}
