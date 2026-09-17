// @feature cert-volcano-import-sync @api-functional
//
// Contract step-1-trigger-round ( cloud-failure-isolation ): the round is
// accepted with CAS held, and the CAS releases normally even after a
// partial_failed terminal state. Outcomes: success /
// guard-released-after-partial-failed.
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

// TestCloudFailureIsolation_Step1_TriggerRoundSuccess verifies Outcome
// "success": triggering a round is accepted ( CAS acquired ), the import
// session is created, and cloud-side access stays read-only.
func TestCloudFailureIsolation_Step1_TriggerRoundSuccess(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.cfi1-start.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status, "clean matrix converges completed")
	require.NotEmpty(t, run.SessionID, "the round created its import session")
	require.Equal(t, "manual", mustSession(t, h, run.SessionID).Operator)
	require.Equal(t, 2, run.CloudsScanned, "aliyun + volcano both lister-ready with successful account reads")
	require.Equal(t, 3, run.AccountsScanned, "three active accounts across the two clouds")
	require.Equal(t, 1, run.Listed, "only aliyun lists an instance; volcano's library is empty")
	require.Equal(t, 2, h.Lister(domain.CloudAliyun).Lists(),
		"cloud-side calls were List-only ( once per aliyun account )")
}

// TestCloudFailureIsolation_Step1_GuardReleasedAfterPartialFailed verifies
// Outcome "guard-released-after-partial-failed": after a round settles
// partial_failed the CAS is released — the next trigger starts normally
// ( failure terminal state leaves no residual placeholder ).
func TestCloudFailureIsolation_Step1_GuardReleasedAfterPartialFailed(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.cfi1-release.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))
	h.SeedCloudError(synctest.CloudVolcano, errAccountSourceDown)

	failed := h.MustSync()
	require.Equal(t, "partial_failed", failed.Status, "the account-read failure flips the terminal state")

	// Immediately re-trigger: the guard must be free ( failure released ).
	h.SeedCloudError(synctest.CloudVolcano, nil)
	next := h.MustSync()
	require.Equal(t, "completed", next.Status, "a new round starts after the failed terminal state")
	require.Equal(t, 1, next.Skipped, "the already-imported instance re-judges as skip")
	require.Empty(t, next.Failures, "the transient failure was cleared before rerun")
}
