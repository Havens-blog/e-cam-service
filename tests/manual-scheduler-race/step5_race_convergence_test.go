// @feature cert-volcano-import-sync @api-functional
//
// Contract step-5-race-convergence ( manual-scheduler-race ): the colliding
// rounds converge to a unique shape, the summary sessionId continues into the
// existing progress endpoint, and operator attribution distinguishes the
// sources. Outcomes: success / poll-progress-via-sessionid /
// operator-attribution-both-rounds.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package manual_scheduler_race

import (
	"context"
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/require"
)

// TestManualSchedulerRace_Step5_RaceConvergenceSuccess verifies Outcome
// "success": after the colliding rounds settle, the shared fingerprint has
// exactly one ledger record, both sessions are terminal without failed
// entries, and the mapping is unique and correctly bound.
func TestManualSchedulerRace_Step5_RaceConvergenceSuccess(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedSameFingerprintOnBothClouds(t, h, "www.race5-converge.com")

	absorbed := h.MustSync() // duplicate absorption round ( session one )
	require.Equal(t, "completed", absorbed.Status)
	converged := h.MustSync() // follow-up round ( session two, all skip )
	require.Equal(t, "completed", converged.Status)

	require.Len(t, h.Ledger(), 1, "the shared fingerprint keeps exactly one ledger record")
	require.Equal(t, fp, h.Ledger()[0].Fingerprint)
	// Mapping uniqueness is per uk_fp_cloud_account ( fingerprint+cloud+accountKey ):
	// the cross-cloud duplicate legitimately produces one row per cloud account,
	// each pointing at the single ledger record.
	mappings := h.ActiveMappings()
	require.Len(t, mappings, 2, "one unique mapping per cloud account triple")
	for _, m := range mappings {
		require.Equal(t, fp, m.CertFingerprint, "each mapping points at the ledgered certificate")
	}
	clouds := []string{mappings[0].Cloud, mappings[1].Cloud}
	require.ElementsMatch(t, []string{"aliyun", "volcano"}, clouds, "no duplicate mapping per triple")

	for _, sessID := range []string{absorbed.SessionID, converged.SessionID} {
		if sessID == "" {
			continue // the converged round created no import session
		}
		sess := mustSession(t, h, sessID)
		require.NotEqual(t, domain.DiscoveryImportRunning, sess.Status, "the session reached a terminal state")
		for _, item := range sess.Items {
			require.NotEqual(t, domain.DiscoveryItemFailed, item.Result, "no failed entries anywhere")
		}
	}
}

// TestManualSchedulerRace_Step5_PollProgressViaSessionID verifies Outcome
// "poll-progress-via-sessionid": a non-empty summary sessionId continues into
// the existing progress endpoint through to the terminal state — no result is
// lost.
func TestManualSchedulerRace_Step5_PollProgressViaSessionID(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.race5-poll.com")
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", fp))

	run := h.MustSync()
	require.NotEmpty(t, run.SessionID, "the summary carries a polling handle for this round")

	resp := h.GetImportSession(run.SessionID)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	sess := h.MustImportSession(run.SessionID)
	require.Equal(t, run.SessionID, sess.SessionID)
	require.Equal(t, "completed", sess.Status, "the session is terminal through the poll face")
	require.NotNil(t, sess.FinishedAt, "the terminal state is decidable via status/finishedAt")
	require.Equal(t, 1, sess.Progress.Total)
	require.Equal(t, 1, sess.Progress.Succeeded)
	require.Len(t, sess.Items, 1, "the per-entry results survive into the poll face")
}

// TestManualSchedulerRace_Step5_OperatorAttribution verifies Outcome
// "operator-attribution-both-rounds": the scheduler round's session carries
// operator=scheduler and the manual round's operator=manual — the sources are
// distinguishable and never conflated.
func TestManualSchedulerRace_Step5_OperatorAttribution(t *testing.T) {
	h := newJourneyHarness(t)
	fpA := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.race5-attr-a.com")
	fpV := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.race5-attr-v.com")
	// Scheduler round operates on the aliyun instance.
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fpA))

	// Scheduler round ( job entry ).
	require.NoError(t, synctest.CertImportJobRun(h.Sync)(context.Background()))
	schedulerSess := mustSession(t, h, h.Sessions.LastID())
	require.Equal(t, "scheduler", schedulerSess.Operator)

	// Manual round ( HTTP face ) on the volcano instance.
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", fpV))
	manual := h.MustSync()
	manualSess := mustSession(t, h, manual.SessionID)
	require.Equal(t, "manual", manualSess.Operator)

	require.NotEqual(t, schedulerSess.ID, manualSess.ID, "the two rounds own distinct sessions")
	require.Equal(t, "volcano", manualSess.Items[0].Cloud, "the manual round imported the volcano instance")
}
