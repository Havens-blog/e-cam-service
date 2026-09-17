// @feature cert-volcano-import-sync @api-functional
//
// Journey smoke: cloud-failure-isolation end-to-end plus one error path.
// Sequence: healthy two-cloud round -> a second round with one cloud's
// account source down ( isolation: static reason, other cloud completes,
// partial_failed ) -> rerun with the failure released converges idempotently
// -> the failure summary stays whitelist-only on the wire.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package cloud_failure_isolation

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloudFailureIsolation_FullJourneySmoke(t *testing.T) {
	h := newJourneyHarness(t)
	fpA := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.smoke4-a1.com")
	fpV := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.smoke4-v1.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fpA))
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", fpV))

	// ---- Step 1-2: healthy round, all units complete ----
	first := h.MustSync()
	assert.Equal(t, "completed", first.Status)
	assert.Equal(t, 2, first.CloudsScanned)
	assert.Equal(t, 2, first.ImportSucceeded)
	assert.Empty(t, first.Failures)

	// ---- Step 3-5: error path — one cloud's account source down, and a NEW
	// instance appears on the failing cloud during the outage ( the failed
	// face the rerun must backfill ) ----
	h.SeedCloudError(synctest.CloudVolcano, errAccountSourceDown)
	fpV2 := seedMaterial(t, h, synctest.CloudVolcano, "cert-v2", "www.smoke4-v2.com")
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount,
		synctest.Instance("cert-v1", fpV), synctest.Instance("cert-v2", fpV2))
	resp := h.PostSync()
	require.Equal(t, http.StatusOK, resp.StatusCode, "execution-layer failure still answers 200 ( not 5xx )")

	var failed synctest.SyncRunPayload
	require.NoError(t, json.Unmarshal(resp.Env.Data, &failed), "summary decode: %s", resp.Env.Data)
	assert.Equal(t, "partial_failed", failed.Status)
	require.Len(t, failed.Failures, 1)
	assert.Equal(t, "volcano", failed.Failures[0].Cloud)
	assert.Equal(t, synctest.ReasonAccountLoadFailed, failed.Failures[0].Reason, "static reason on the wire")
	assert.NotContains(t, resp.Body, "unreachable", "no cloud error detail in the HTTP body")
	assert.Len(t, h.Ledger(), 2, "the healthy cloud's import survived ( isolation ); the failed face did not import")
	_, err := h.LedgerByFP(fpV2)
	assert.Error(t, err, "the outage-window instance is not ledgered yet")

	// ---- Rerun: failure released -> idempotent convergence ----
	h.SeedCloudError(synctest.CloudVolcano, nil)
	repaired := h.MustSync()
	assert.Equal(t, "completed", repaired.Status, "the failed face converges on rerun")
	assert.Equal(t, 1, repaired.Imported, "the outage-window instance imports")
	assert.Equal(t, 2, repaired.Skipped, "the healthy faces ( aliyun + volcano cert-v1 ) are not re-processed")
	assert.Len(t, h.Ledger(), 3, "no duplicate ledger rows")
	assert.Len(t, h.ActiveMappings(), 3, "no duplicate mappings")
	synctest.RequireNoKeyMaterial(t, h.PostSync().Body)
}
