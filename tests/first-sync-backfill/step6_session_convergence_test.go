// @feature cert-volcano-import-sync @api-functional
//
// Contract step-6-session-convergence ( first-sync-backfill ): the round
// settles into a terminal state with per-entry results and idempotent
// re-run convergence. Outcomes: success / second-run-idempotent-empty /
// timeout-partial-convergence.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package first_sync_backfill

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/require"
)

// TestFirstSyncBackfill_Step6_SessionConvergenceSuccess verifies Outcome
// "success": all import entries processed, the session settles completed
// with per-entry results, and the summary counters agree with the ledger and
// mapping state ( probe convergence is exercised by the reference-scan
// journey of the discovery-import feature; the sync round's contribution is
// the converged ledger + mapping state asserted here ).
func TestFirstSyncBackfill_Step6_SessionConvergenceSuccess(t *testing.T) {
	h := newJourneyHarness(t)
	fpA := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.step6-a1.com")
	fpV := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.step6-v1.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fpA))
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", fpV))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status, "zero-failure rounds settle completed")
	require.NotEmpty(t, run.FinishedAt, "terminal rounds carry finishedAt")
	require.Equal(t, 2, run.ImportSucceeded)
	require.Equal(t, 0, run.ImportFailed)
	require.Empty(t, run.Failures)

	sess := mustSession(t, h, run.SessionID)
	require.Equal(t, domain.DiscoveryImportCompleted, sess.Status)
	require.NotNil(t, sess.FinishedAt, "session carries its terminal timestamp")
	require.Len(t, sess.Items, 2)
	require.Len(t, h.Ledger(), 2, "ledger rows match the judgment counts")
	require.Len(t, h.ActiveMappings(), 2, "mapping rows match the judgment counts")
}

// TestFirstSyncBackfill_Step6_SecondRunIdempotentEmpty verifies Outcome
// "second-run-idempotent-empty": with no cloud-side change the next round is
// a read-only convergence — every instance skips, zero ledger/mapping
// writes, no new import session.
func TestFirstSyncBackfill_Step6_SecondRunIdempotentEmpty(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.step6-idem.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))

	first := h.MustSync()
	require.Equal(t, 1, first.ImportSucceeded)
	ledgerAfterFirst := len(h.Ledger())
	mappingsAfterFirst := len(h.ActiveMappings())
	getsAfterFirst := h.MaterialGets()

	second := h.MustSync()
	require.Equal(t, "completed", second.Status, "a converged round is business-as-usual, not an error")
	require.Equal(t, 1, second.Skipped, "the unchanged instance re-judges as skipped")
	require.Equal(t, 0, second.Imported, "zero import entries")
	require.Empty(t, second.SessionID, "no import entries -> no new session handle")
	require.Empty(t, second.Failures)
	require.Equal(t, int32(1), h.Sessions.Created(), "no second session document")
	require.Equal(t, ledgerAfterFirst, len(h.Ledger()), "zero ledger writes ( read-only discipline )")
	require.Equal(t, mappingsAfterFirst, len(h.ActiveMappings()), "zero mapping writes")
	require.Equal(t, getsAfterFirst, h.MaterialGets(), "zero material-channel Gets on the converged round")
}

// TestFirstSyncBackfill_Step6_TimeoutPartialConvergence verifies Outcome
// "timeout-partial-convergence" at the contract boundary:
//
// ASSERTION_DEPTH_EXEMPT ( partial ): the overall sync budget
// ( discoveryImportTimeout, 10-minute scale ) is owned by the service's
// internal context ( cert_sync_service.go run() detaches from the caller
// context ) and exposes no external knob, so it cannot be exhausted at the
// API/journey layer without a 10-minute wall clock. The timeout semantics —
// remaining clouds/entries marked with the SESSION_TIMEOUT static reason,
// already-imported items not rolled back, the round converging instead of
// hanging — are covered at the unit layer by TestCertSync_OverallTimeout
// ( injected 50ms budget ). The adjacent journey-layer contract ( every round
// settles into a binary terminal state with finishedAt, no intermediate
// stuck state ) is asserted by the terminal-state tests of this journey and
// of cloud-failure-isolation.
func TestFirstSyncBackfill_Step6_TimeoutPartialConvergence(t *testing.T) {
	t.Skip("overall sync budget is not exhaustible at the API layer ( service-internal 10min context, no external knob ); timeout semantics covered by unit-layer TestCertSync_OverallTimeout with an injected budget")
}
