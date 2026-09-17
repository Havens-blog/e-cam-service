// @feature cert-volcano-import-sync @api-functional
//
// Contract step-5-terminal-and-rerun ( cloud-failure-isolation ): the round
// settles into the binary terminal states with a whitelist failure summary,
// and reruns converge idempotently. Outcomes: partial-failed-terminal-summary
// / terminal-state-binary-semantics / rerun-converges-idempotent.
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

// TestCloudFailureIsolation_Step5_PartialFailedTerminalSummary verifies
// Outcome "partial-failed-terminal-summary": a round with a failure settles
// partial_failed and its failure summary carries only whitelist fields with
// the static reason — no cloud error detail.
func TestCloudFailureIsolation_Step5_PartialFailedTerminalSummary(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.cfi5-partial.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))
	h.SeedCloudError(synctest.CloudVolcano, errAccountSourceDown)

	run := h.MustSync()
	require.Equal(t, "partial_failed", run.Status, "any failure -> partial_failed")
	require.NotEmpty(t, run.FinishedAt, "terminal rounds carry finishedAt")
	require.Len(t, run.Failures, 1)
	failure := run.Failures[0]
	require.Equal(t, synctest.ReasonAccountLoadFailed, failure.Reason)
	// Whitelist fields only: cloud/accountKey/cloudCertId omitempty + reason.
	require.Equal(t, "volcano", failure.Cloud)
	require.Equal(t, "", failure.AccountKey)
	require.Equal(t, "", failure.CloudCertID)
	require.Len(t, failureIndexAll(run), 1, "no extra failure entries")
}

// TestCloudFailureIsolation_Step5_TerminalStateBinarySemantics verifies
// Outcome "terminal-state-binary-semantics": a fully-successful round and a
// round containing a failure settle into completed / partial_failed
// respectively — running never gets stuck and the terminal state is
// decidable via status + finishedAt.
func TestCloudFailureIsolation_Step5_TerminalStateBinarySemantics(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.cfi5-binary.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))

	clean := h.MustSync()
	require.Equal(t, "completed", clean.Status, "no failure -> completed")
	require.NotEmpty(t, clean.FinishedAt)
	cleanSess := mustSession(t, h, clean.SessionID)
	require.Equal(t, domain.DiscoveryImportCompleted, cleanSess.Status)
	require.NotNil(t, cleanSess.FinishedAt, "the session's terminal state is decidable via status/finishedAt")

	h.SeedCloudError(synctest.CloudVolcano, errAccountSourceDown)
	failing := h.MustSync()
	require.Equal(t, "partial_failed", failing.Status, "any failure -> partial_failed")
	require.NotEmpty(t, failing.FinishedAt, "the failing round also converges ( no hang )")
	require.Empty(t, failing.SessionID,
		"the failing round had no import entries -> no new session, yet it still settled into the binary terminal state")
	require.Equal(t, int32(1), h.Sessions.Created(), "no intermediate stuck state: session count stable, no dangling running session")
}

// TestCloudFailureIsolation_Step5_RerunConvergesIdempotent verifies Outcome
// "rerun-converges-idempotent": after a failed round, the rerun reprocesses
// the failed face, does not duplicate rows, does not accumulate failures
// across rounds, and converges on its own terminal state.
func TestCloudFailureIsolation_Step5_RerunConvergesIdempotent(t *testing.T) {
	h := newJourneyHarness(t)
	fpA := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.cfi5-rerun-a.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fpA))
	fpV := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.cfi5-rerun-v.com")
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", fpV))
	h.SeedCloudError(synctest.CloudVolcano, errAccountSourceDown)

	failed := h.MustSync()
	require.Equal(t, "partial_failed", failed.Status)
	require.Len(t, failed.Failures, 1)
	ledgerAfterFailed := len(h.Ledger())

	h.SeedCloudError(synctest.CloudVolcano, nil)
	rerun := h.MustSync()
	require.Equal(t, "completed", rerun.Status, "the rerun converges ( failure does not accumulate across rounds )")
	require.Empty(t, rerun.Failures, "no failure carried over")
	require.Equal(t, 1, rerun.Imported, "the failed face reprocessed")
	require.Len(t, h.Ledger(), ledgerAfterFailed+1, "exactly the failed face added a row")
	require.Len(t, h.ActiveMappings(), 2, "one mapping per instance, no duplicates")
}

// failureIndexAll returns indexes of all failure entries ( used to assert the
// failure list carries nothing beyond the expected units ).
func failureIndexAll(run synctest.SyncRunPayload) []int {
	out := make([]int, 0, len(run.Failures))
	for i := range run.Failures {
		out = append(out, i)
	}
	return out
}
