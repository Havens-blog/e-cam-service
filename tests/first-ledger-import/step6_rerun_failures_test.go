// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/first-ledger-import/contracts/step-6-rerun-failures.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package first_ledger_import

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep6_RerunFailures_Success verifies the "success" outcome: after a
// partial failure the operator reruns only the failed item; the rerun is
// idempotent and the ledger/mapping state converges.
func TestStep6_RerunFailures_Success(t *testing.T) {
	h, aliyun := aliyunWorld(t)
	bOk := certtest.NewBundle(t, "www.step6-ok.com", []string{"www.step6-ok.com"}, nil)
	aliyun.AddMaterial("cert-ok", materialFor(bOk))
	// cert-flaky has no material in round one ( fails ), material arrives
	// before the rerun.

	first := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-ok"),
		discoverytest.ImportItem("aliyun", "acct-a", "cert-flaky"),
	))
	require.Equal(t, http.StatusAccepted, first.StatusCode, first.Body)
	firstFinal := h.PollImportTerminal(first.DataMap(t)["sessionId"].(string))
	assert.Equal(t, "partial_failed", firstFinal["status"])

	// Cloud-side problem resolved: material now exists for the failed item.
	bFlaky := certtest.NewBundle(t, "www.step6-flaky.com", []string{"www.step6-flaky.com"}, nil)
	aliyun.AddMaterial("cert-flaky", materialFor(bFlaky))

	rerun := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-flaky"),
	))
	require.Equal(t, http.StatusAccepted, rerun.StatusCode, rerun.Body)
	rerunItems := rerun.DataMap(t)["items"].([]any)
	require.Len(t, rerunItems, 1, "rerun processes only the remaining item")
	assert.Equal(t, "cert-flaky", rerunItems[0].(map[string]any)["cloudCertId"])

	rerunFinal := h.PollImportTerminal(rerun.DataMap(t)["sessionId"].(string))
	assert.Equal(t, "completed", rerunFinal["status"])
	rerunItem := rerunFinal["items"].([]any)[0].(map[string]any)
	assert.Equal(t, "success", rerunItem["result"])

	ledger := h.Ledger()
	require.Len(t, ledger, 2, "converged ledger: one record per distinct fingerprint")
	fps := map[string]bool{ledger[0].Fingerprint: true, ledger[1].Fingerprint: true}
	assert.True(t, fps[bOk.Fingerprint] && fps[bFlaky.Fingerprint], "no duplicate ledger records from the rerun")
	assert.Equal(t, 1, len(h.MappingsByFP(bOk.Fingerprint)), "first-round success not re-imported")
	assert.Equal(t, 1, len(h.MappingsByFP(bFlaky.Fingerprint)))
	for _, c := range ledger {
		if c.Fingerprint == bFlaky.Fingerprint {
			assert.Equal(t, rerunItem["mappedCertId"], c.ID.Hex(), "rerun mappedCertId points at its ledger record")
		}
	}
}

// TestStep6_RerunFailures_FailedItemStillFailing verifies the
// "failed-item-still-failing" outcome: an unresolved rerun fails again with
// the same static reason and writes nothing new.
func TestStep6_RerunFailures_FailedItemStillFailing(t *testing.T) {
	h, aliyun := aliyunWorld(t)
	bOk := certtest.NewBundle(t, "www.step6b-ok.com", []string{"www.step6b-ok.com"}, nil)
	aliyun.AddMaterial("cert-ok", materialFor(bOk))

	first := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-ok"),
		discoverytest.ImportItem("aliyun", "acct-a", "cert-gone"),
	))
	require.Equal(t, http.StatusAccepted, first.StatusCode, first.Body)
	firstFinal := h.PollImportTerminal(first.DataMap(t)["sessionId"].(string))
	assert.Equal(t, "partial_failed", firstFinal["status"])
	ledgerAfterFirst := len(h.Ledger())

	rerun := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-gone"),
	))
	require.Equal(t, http.StatusAccepted, rerun.StatusCode, rerun.Body)
	rerunFinal := h.PollImportTerminal(rerun.DataMap(t)["sessionId"].(string))

	assert.Equal(t, "partial_failed", rerunFinal["status"], "rerun does not magically succeed")
	item := rerunFinal["items"].([]any)[0].(map[string]any)
	assert.Equal(t, "failed", item["result"])
	assert.Equal(t, "CERT_GET_FAILED: 云侧已不存在", item["errorReason"], "same static reason again")

	assert.Len(t, h.Ledger(), ledgerAfterFirst, "no duplicate ledger records from the failing rerun")
	assert.Equal(t, int32(2), h.SessionsCreated(), "two sessions total ( first + rerun )")
}

// TestStep6_RerunFailures_Unauthorized verifies the "unauthorized" outcome:
// an unauthenticated rerun creates no session.
func TestStep6_RerunFailures_Unauthorized(t *testing.T) {
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) { c.Claims = nil })
	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-A"),
	))
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.Equal(t, int32(0), h.SessionsCreated(), "no rerun session is created")
}
