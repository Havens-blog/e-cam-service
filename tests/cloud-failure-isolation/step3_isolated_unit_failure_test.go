// @feature cert-volcano-import-sync @api-functional
//
// Contract step-3-isolated-unit-failure ( cloud-failure-isolation ): list
// failures and account-load failures are isolated per unit with static
// reasons. Outcomes: list-failed-isolated-static-reason /
// account-load-failed-cloud-level.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package cloud_failure_isolation

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/require"
)

// TestCloudFailureIsolation_Step3_ListFailedIsolatedStaticReason verifies
// Outcome "list-failed-isolated-static-reason": a unit whose listing fails
// ( rate-limit class included ) is isolated with the CERT_LIST_FAILED static
// reason, does no retry storm within the round, and its already-fetched
// partial results still enter judgment.
func TestCloudFailureIsolation_Step3_ListFailedIsolatedStaticReason(t *testing.T) {
	h := newJourneyHarness(t)
	// Volcano acct-v: listing fails but the adapter contract returns the
	// partially-fetched instance alongside the error.
	fpPartial := seedMaterial(t, h, synctest.CloudVolcano, "cert-v-partial", "www.cfi3-partial.com")
	h.Lister(synctest.CloudVolcano).SetError(VolcanoAccount, errAccountSourceDown)
	h.Lister(synctest.CloudVolcano).SetInstances(VolcanoAccount, synctest.Instance("cert-v-partial", fpPartial))
	// Healthy unit on the other cloud.
	fpOK := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.cfi3-ok.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fpOK))
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount2) // empty healthy unit

	run := h.MustSync()
	require.Equal(t, "partial_failed", run.Status)
	require.Equal(t, 1, h.Lister(synctest.CloudVolcano).Lists(),
		"the failing unit is listed exactly once ( no in-round retry storm )")

	idx := failureIndex(run, "volcano", VolcanoAccount)
	require.NotEqual(t, -1, idx, "the failing unit is located in the failure list")
	failure := run.Failures[idx]
	require.Equal(t, synctest.ReasonListFailed, failure.Reason, "static reason text only")
	require.NotContains(t, failure.Reason, "429", "no cloud response fragment in the reason")
	require.NotContains(t, failure.Reason, "unreachable", "no cloud error detail in the reason")

	// Partial results still enter judgment ( adapter partial tolerance ).
	require.Equal(t, 2, run.Imported, "the partial instance and the healthy unit's instance both imported")
	require.Equal(t, 2, run.ImportSucceeded)
	require.Equal(t, 0, run.ImportFailed)
	require.Equal(t, 3, run.AccountsScanned, "all three units enumerated")
	require.Equal(t, 2, run.CloudsScanned, "the failed cloud still counts as scanned ( account read succeeded )")
	require.Len(t, h.Ledger(), 2, "both units' instances registered")
}

// TestCloudFailureIsolation_Step3_AccountLoadFailedCloudLevel verifies
// Outcome "account-load-failed-cloud-level": a cloud whose account read fails
// records the ACCOUNT_LOAD_FAILED static reason with an empty account key
// ( cloud-level aggregation ) and is excluded from CloudsScanned.
func TestCloudFailureIsolation_Step3_AccountLoadFailedCloudLevel(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.cfi3-cloud.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))
	h.SeedCloudError(synctest.CloudVolcano, errAccountSourceDown)

	run := h.MustSync()
	require.Equal(t, "partial_failed", run.Status)

	idx := failureIndex(run, "volcano", "")
	require.NotEqual(t, -1, idx, "the cloud-level failure is recorded with an empty account key")
	failure := run.Failures[idx]
	require.Equal(t, synctest.ReasonAccountLoadFailed, failure.Reason)
	require.Equal(t, "", failure.CloudCertID, "cloud-level failure carries no instance id")

	require.Equal(t, 1, run.CloudsScanned, "the failed cloud is excluded from CloudsScanned")
	require.Equal(t, 2, run.AccountsScanned, "only the healthy cloud's two accounts enumerated")
	require.Len(t, h.Ledger(), 1, "the healthy cloud completes its import")
	require.Equal(t, fp, h.Ledger()[0].Fingerprint)
}
