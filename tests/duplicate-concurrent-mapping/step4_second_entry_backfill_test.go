// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/duplicate-concurrent-mapping/contracts/step-4-second-entry-backfill.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package duplicate_concurrent_mapping

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep4_SecondEntryBackfill_Success verifies the "success" outcome:
// the second same-fingerprint item hits the duplicate sentinel, switches to
// mapping backfill and records success.
func TestStep4_SecondEntryBackfill_Success(t *testing.T) {
	h, _, b, _ := multiAccountWorld(t)

	final := importAndAwait(t, h,
		discoverytest.ImportItem("aliyun", "acct-a", "cert-acc-a"),
		discoverytest.ImportItem("aliyun", "acct-b", "cert-acc-b"),
	)
	assert.Equal(t, "completed", final["status"], "no item degrades on the duplicate path")

	items := final["items"].([]any)
	first := items[0].(map[string]any)
	second := items[1].(map[string]any)
	assert.Equal(t, "success", first["result"])
	assert.Empty(t, first["errorReason"], "fresh registration carries no note")
	assert.Equal(t, "success", second["result"], "duplicate entry still succeeds")
	assert.Equal(t, reasonInLedger, second["errorReason"], "backfill note on the duplicate entry")
	assert.Equal(t, first["mappedCertId"], second["mappedCertId"], "both accounts map onto the same ledger record")

	assert.Len(t, h.Ledger(), 1, "single ledger record despite two accounts")
	mappings := h.MappingsByFP(b.Fingerprint)
	require.Len(t, mappings, 2, "one mapping per account ( uk_fp_cloud_account )")
	assert.ElementsMatch(t, []string{"acct-a", "acct-b"}, []string{mappings[0].AccountKey, mappings[1].AccountKey})
	for _, m := range mappings {
		assert.Equal(t, b.Fingerprint, m.CertFingerprint,
			"every account mapping points at the single ledger record's fingerprint ( cross-entity )")
	}
}

// TestStep4_SecondEntryBackfill_ConcurrentRaceDuplicate verifies the
// "concurrent-race-duplicate" outcome: two sessions racing on the same
// fingerprint converge to one ledger record with both mappings, no failed
// items.
func TestStep4_SecondEntryBackfill_ConcurrentRaceDuplicate(t *testing.T) {
	h, _, b, _ := multiAccountWorld(t)

	r1 := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-acc-a"),
	))
	r2 := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-b", "cert-acc-b"),
	))
	require.Equal(t, http.StatusAccepted, r1.StatusCode, r1.Body)
	require.Equal(t, http.StatusAccepted, r2.StatusCode, r2.Body)

	f1 := h.PollImportTerminal(r1.DataMap(t)["sessionId"].(string))
	f2 := h.PollImportTerminal(r2.DataMap(t)["sessionId"].(string))
	assert.Equal(t, "completed", f1["status"])
	assert.Equal(t, "completed", f2["status"], "the losing racer still succeeds via backfill")

	m1 := f1["items"].([]any)[0].(map[string]any)["mappedCertId"]
	m2 := f2["items"].([]any)[0].(map[string]any)["mappedCertId"]
	assert.Equal(t, m1, m2, "both sessions point at the single surviving record")

	assert.Len(t, h.Ledger(), 1, "exactly one ledger record survives the race")
	require.Len(t, h.MappingsByFP(b.Fingerprint), 2, "both account mappings exist")
}

// TestStep4_SecondEntryBackfill_AccountMissing verifies the
// "account-missing" outcome: an unknown accountKey fails statically and
// never blocks the rest.
func TestStep4_SecondEntryBackfill_AccountMissing(t *testing.T) {
	h, _, b, _ := multiAccountWorld(t)

	final := importAndAwait(t, h,
		discoverytest.ImportItem("aliyun", "acct-ghost", "cert-acc-a"),
		discoverytest.ImportItem("aliyun", "acct-a", "cert-acc-a"),
	)
	assert.Equal(t, "partial_failed", final["status"])
	items := final["items"].([]any)
	ghost := items[0].(map[string]any)
	assert.Equal(t, "failed", ghost["result"])
	assert.Equal(t, reasonNoAccount, ghost["errorReason"])
	ok := items[1].(map[string]any)
	assert.Equal(t, "success", ok["result"], "the rest of the session keeps processing")

	require.Len(t, h.Ledger(), 1)
	assert.Equal(t, b.Fingerprint, h.Ledger()[0].Fingerprint)
	assert.Len(t, h.MappingsByFP(b.Fingerprint), 1, "no mapping row for the ghost account")
}
