// @feature cert-volcano-import-sync @api-functional
//
// Contract step-3-failure-summary-contract ( manual-sync-endpoint-guard ): the
// failure surface of the 200 summary — whitelist-only fields with static
// reasons, acceptance decoupled from execution outcome, and the all-success
// baseline. Outcomes: failure-summary-whitelist / import-failure-decoupled /
// zero-failure-completed-baseline.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package manual_sync_endpoint_guard

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestManualSyncEndpointGuard_Step3_FailureSummaryWhitelist verifies Outcome
// "failure-summary-whitelist": with an enumeration-layer failure ( import
// layer clean ) the summary carries failures entries with whitelist fields
// only and the static reason — no cloud error detail — and the round is
// partial_failed.
func TestManualSyncEndpointGuard_Step3_FailureSummaryWhitelist(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.msg3-whitelist.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))
	h.SeedCloudError(synctest.CloudVolcano, errAccountSourceDown)

	resp := h.PostSync()
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	run := synctest.MustDecodeSyncRun(t, resp)

	assert.Equal(t, "partial_failed", run.Status)
	assert.Equal(t, 0, run.ImportFailed, "the import layer is clean in this fixture")
	require.Len(t, run.Failures, 1)
	failure := run.Failures[0]
	assert.Equal(t, "volcano", failure.Cloud)
	assert.Equal(t, "", failure.AccountKey, "cloud-level failure carries no account key")
	assert.Equal(t, "", failure.CloudCertID, "no instance id at cloud level")
	assert.Equal(t, synctest.ReasonAccountLoadFailed, failure.Reason, "static reason text")
	assert.NotContains(t, failure.Reason, "down", "no cloud error detail in the reason")

	// Whitelist shape on the wire: omitted empty fields stay absent; only
	// cloud/reason are serialized for this cloud-level failure.
	data := resp.DataMap(t)
	rawFailures, ok := data["failures"].([]any)
	require.True(t, ok, "failures is an array")
	require.Len(t, rawFailures, 1)
	first, ok := rawFailures[0].(map[string]any)
	require.True(t, ok)
	keys := make([]string, 0, len(first))
	for k := range first {
		keys = append(keys, k)
	}
	assert.ElementsMatch(t, []string{"cloud", "reason"}, keys,
		"failure entry exposes whitelist fields only")
}

// TestManualSyncEndpointGuard_Step3_ImportFailureDecoupled verifies Outcome
// "import-failure-decoupled": acceptance ( 200 ) does not mean all imports
// succeeded — an import-layer failure is faithfully presented in the session
// entries and the partial_failed terminal state, without rewriting the HTTP
// layer to 5xx.
func TestManualSyncEndpointGuard_Step3_ImportFailureDecoupled(t *testing.T) {
	h := newJourneyHarness(t)
	fpOK := seedMaterial(t, h, domain.CloudAliyun, "cert-ok", "www.msg3-ok.com")
	fpBad := seedMaterial(t, h, domain.CloudAliyun, "cert-bad", "www.msg3-bad.com")
	h.Material(domain.CloudAliyun).AddError("cert-bad", errMaterialDown)
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount,
		synctest.Instance("cert-ok", fpOK), synctest.Instance("cert-bad", fpBad))

	resp := h.PostSync()
	require.Equal(t, http.StatusOK, resp.StatusCode, "the HTTP layer stays 200 — acceptance != all-success")
	run := synctest.MustDecodeSyncRun(t, resp)
	assert.Equal(t, "partial_failed", run.Status)
	assert.Equal(t, 2, run.Imported)
	assert.Equal(t, 1, run.ImportSucceeded)
	assert.Equal(t, 1, run.ImportFailed)
	assert.Empty(t, run.Failures, "import-layer failures live in the session, not the enum-layer list")

	sess := mustSession(t, h, run.SessionID)
	require.Equal(t, domain.DiscoveryImportPartialFailed, sess.Status)
	var failed *domain.DiscoveryImportItem
	for i := range sess.Items {
		if sess.Items[i].CloudCertID == "cert-bad" {
			failed = &sess.Items[i]
		}
	}
	require.NotNil(t, failed)
	assert.Equal(t, domain.DiscoveryItemFailed, failed.Result)
	assert.Equal(t, synctest.ReasonGetCertFailed, failed.ErrorReason, "static reason in the session entry")

	// Cross-entity: the healthy entry binds the ledger certificate it created.
	okItem := sess.Items[0]
	if okItem.CloudCertID == "cert-bad" {
		okItem = sess.Items[1]
	}
	cert, err := h.LedgerByFP(fpOK)
	require.NoError(t, err)
	assert.Equal(t, cert.ID.Hex(), okItem.MappedCertID, "the healthy entry binds its new ledger certificate")
}

// TestManualSyncEndpointGuard_Step3_ZeroFailureCompletedBaseline verifies
// Outcome "zero-failure-completed-baseline": with zero failures across all
// three layers the summary is completed with an empty failures array and
// importFailed 0.
func TestManualSyncEndpointGuard_Step3_ZeroFailureCompletedBaseline(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.msg3-baseline.com")
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", fp))

	run := h.MustSync()
	assert.Equal(t, "completed", run.Status)
	assert.Equal(t, 0, run.ImportFailed)
	assert.Empty(t, run.Failures)
	assert.NotNil(t, run.Failures, "failures is an empty array, not null ( envelope shape )")
	// On the wire: "failures" present as [].
	data := h.PostSync().DataMap(t) // an extra converged round for the raw shape
	failuresRaw, ok := data["failures"].([]any)
	require.True(t, ok, "failures serializes as an array")
	assert.Empty(t, failuresRaw)
}
