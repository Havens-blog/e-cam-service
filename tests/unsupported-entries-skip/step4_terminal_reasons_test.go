// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/unsupported-entries-skip/contracts/step-4-terminal-reasons.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package unsupported_entries_skip

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep4_TerminalReasons_Success verifies the "success" outcome: the
// mixed session converges with parseable items registered and skipped items
// counted on the failed side without breaking the session.
func TestStep4_TerminalReasons_Success(t *testing.T) {
	h, aliyun, _, _ := mixedWorld(t)
	b := certtest.NewBundle(t, "www.skip-term.com", []string{"www.skip-term.com"}, nil)
	aliyun.AddMaterial("cert-A", goodMaterial(b))

	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-A"),
		discoverytest.ImportItem("huawei", "acct-hw", "scm-1"),
	))
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	sessionID := resp.DataMap(t)["sessionId"].(string)
	final := h.PollImportTerminal(sessionID)
	assert.Equal(t, "partial_failed", final["status"], "skip counts on the failed side")
	assert.NotNil(t, final["finishedAt"])

	// The progress endpoint serves the same terminal read ( verification view ).
	again := h.Get(discoverytest.RouteImportSession + sessionID).DataMap(t)
	assert.Equal(t, final["status"], again["status"])
	progress := again["progress"].(map[string]any)
	assert.Equal(t, float64(2), progress["total"])
	assert.Equal(t, float64(1), progress["succeeded"])
	assert.Equal(t, float64(1), progress["failed"])
	assert.Equal(t, "success", again["items"].([]any)[0].(map[string]any)["result"],
		"the parseable item registered on the mixed terminal state")

	items := again["items"].([]any)
	skipped := items[1].(map[string]any)
	assert.Equal(t, "failed", skipped["result"])
	assert.Equal(t, reasonUnsupportedCloud, skipped["errorReason"])

	require.Len(t, h.Ledger(), 1, "only the parseable entry registered")
	assert.Equal(t, "fingerprint_only", string(h.Ledger()[0].HostingStatus))
}

// TestStep4_TerminalReasons_AllUnparseableTerminal verifies the
// "all-unparseable-terminal" outcome: a fully unparseable submission still
// converges with every item reasoned and zero business writes.
func TestStep4_TerminalReasons_AllUnparseableTerminal(t *testing.T) {
	h, _, _, snapID := mixedWorld(t)
	fpGone := discoverytest.FP("skip-all-gone")
	h.SeedRefs(discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-a", CloudCertID: "cert-gone", Fingerprint: fpGone, ResourceID: "res-gone", SnapshotID: snapID})

	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("huawei", "acct-hw", "scm-1"),
		discoverytest.ImportItem("aws", "acct-aws", "iam-123"),
		discoverytest.ImportItem("aliyun", "acct-a", "cert-gone"),
	))
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	final := h.PollImportTerminal(resp.DataMap(t)["sessionId"].(string))
	assert.Equal(t, "partial_failed", final["status"], "session converges with every item reasoned")

	progress := final["progress"].(map[string]any)
	assert.Equal(t, float64(3), progress["failed"])
	assert.Equal(t, float64(0), progress["succeeded"])

	items := final["items"].([]any)
	require.Len(t, items, 3)
	assert.Equal(t, reasonUnsupportedCloud, items[0].(map[string]any)["errorReason"])
	assert.Equal(t, reasonIAMHosted, items[1].(map[string]any)["errorReason"])
	assert.Equal(t, reasonCertGone, items[2].(map[string]any)["errorReason"])

	assert.Empty(t, h.Ledger(), "no ledger writes")
	assert.Empty(t, h.MappingsByFP(fpGone), "no mapping rows for any skipped item")
	require.Len(t, h.RefsByFP(fpGone), 1, "the seeded reference row survives untouched ( no backfill )")
	assert.Equal(t, fpGone, h.RefsByFP(fpGone)[0].CertFingerprint)
}

// TestStep4_TerminalReasons_NoLeakStaticReasons verifies the
// "no-leak-static-reasons" outcome: a cloud error carrying sensitive
// content is reduced to the fixed static reason.
func TestStep4_TerminalReasons_NoLeakStaticReasons(t *testing.T) {
	h, _, aws, _ := mixedWorld(t)
	const arnID = "arn:aws:acm:us-east-1:1:certificate/leaky"
	const secretFragment = "AKIA-SECRET-TOKEN-do-not-leak"
	aws.AddError(arnID, errors.New("acm GetCertificate failed: Authorization header Bearer "+secretFragment))

	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aws", "acct-aws", arnID),
	))
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	final := h.PollImportTerminal(resp.DataMap(t)["sessionId"].(string))

	item := final["items"].([]any)[0].(map[string]any)
	assert.Equal(t, "failed", item["result"])
	assert.Equal(t, reasonGetCertFailed, item["errorReason"], "fixed static reason")
	assert.NotContains(t, item["errorReason"], secretFragment, "cloud response fragments never surface")
	assert.NotContains(t, h.Get(discoverytest.RouteImportSession+resp.DataMap(t)["sessionId"].(string)).Body, secretFragment)
	assert.Empty(t, h.Ledger())
}

// TestStep4_TerminalReasons_Unauthorized verifies the "unauthorized"
// outcome.
func TestStep4_TerminalReasons_Unauthorized(t *testing.T) {
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) { c.Claims = nil })
	resp := h.Get(discoverytest.RouteImportSession + "000000000000000000000000")
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.False(t, resp.Env.Success)
	assert.Empty(t, resp.Env.Data)
}
