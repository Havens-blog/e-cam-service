// @feature cert-volcano-import-sync @api-functional
//
// Contract step-2-enumerate-units ( cloud-failure-isolation ): every cloud x
// active account forms an independent processing unit. Outcomes: success /
// empty-account-skip / invalid-account-isolated.
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

// TestCloudFailureIsolation_Step2_EnumerateUnitsSuccess verifies Outcome
// "success": three units across two clouds enumerate independently and the
// summary counts each unit.
func TestCloudFailureIsolation_Step2_EnumerateUnitsSuccess(t *testing.T) {
	h := newJourneyHarness(t)
	fp1 := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.cfi2-a1.com")
	fp2 := seedMaterial(t, h, domain.CloudAliyun, "cert-a2", "www.cfi2-a2.com")
	fp3 := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.cfi2-v1.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp1))
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount2, synctest.Instance("cert-a2", fp2))
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", fp3))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Equal(t, 2, run.CloudsScanned, "two clouds with lister ports")
	require.Equal(t, 3, run.AccountsScanned, "three independent (cloud, account) units")
	require.Equal(t, 3, run.Listed)
	require.Empty(t, run.Failures, "units share no failure state")
}

// TestCloudFailureIsolation_Step2_EmptyAccountSkip verifies Outcome
// "empty-account-skip": an account with zero listed instances is a skip —
// no failure, Listed unchanged for it, no import entries produced.
func TestCloudFailureIsolation_Step2_EmptyAccountSkip(t *testing.T) {
	h := newJourneyHarness(t)
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount) // empty library
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a2", "www.cfi2-empty.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount2, synctest.Instance("cert-a2", fp))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Equal(t, 3, run.AccountsScanned, "the empty units were still enumerated ( aliyun-acct-a + volcano-acct-v + aliyun-acct-a2 )")
	require.Equal(t, 1, run.Listed, "the empty units contribute nothing to Listed")
	require.Empty(t, run.Failures)
	require.Len(t, h.Ledger(), 1, "only the healthy unit imported")
	require.Equal(t, fp, h.Ledger()[0].Fingerprint)
}

// TestCloudFailureIsolation_Step2_InvalidAccountIsolated verifies Outcome
// "invalid-account-isolated": an unreachable/invalid account is isolated at
// cloud level ( ACCOUNT_LOAD_FAILED static reason ) — other clouds continue.
func TestCloudFailureIsolation_Step2_InvalidAccountIsolated(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.cfi2-invalid.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))
	h.SeedCloudError(synctest.CloudVolcano, errAccountSourceDown)

	run := h.MustSync()
	require.Equal(t, "partial_failed", run.Status, "the cloud-level failure flips the terminal state")
	require.Equal(t, 1, run.CloudsScanned, "the failed cloud is not counted as scanned")
	require.Equal(t, 2, run.AccountsScanned, "the failed cloud's accounts are not enumerated ( aliyun's two accounts were )")

	require.Len(t, run.Failures, 1, "exactly the failed cloud recorded")
	failure := run.Failures[0]
	require.Equal(t, "volcano", failure.Cloud)
	require.Equal(t, "", failure.AccountKey, "cloud-level failure carries no account key")
	require.Equal(t, synctest.ReasonAccountLoadFailed, failure.Reason, "static reason only")
	require.NotContains(t, failure.Reason, "credentials expired", "no cloud error detail in the response")

	// Other cloud completed normally ( isolation ).
	require.Len(t, h.Ledger(), 1, "the healthy cloud imported its instance")
	require.Equal(t, fp, h.Ledger()[0].Fingerprint)
}
