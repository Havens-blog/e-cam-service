// @feature cert-volcano-import-sync @api-functional
//
// Contract step-4-idempotent-absorption ( manual-scheduler-race ): the
// latecomer's fingerprint unique-key conflict is absorbed as a mapping
// backfill with success semantics. Outcomes: success-duplicate-redirect /
// both-sessions-success-no-failed / no-second-ledger-row.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package manual_scheduler_race

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/require"
)

// TestManualSchedulerRace_Step4_DuplicateRedirectSuccess verifies Outcome
// "success-duplicate-redirect": the latecomer entry hits the fingerprint
// unique key, redirects to the existing certificate to backfill its mapping,
// and records success ( idempotent semantics, never a failure ).
func TestManualSchedulerRace_Step4_DuplicateRedirectSuccess(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedSameFingerprintOnBothClouds(t, h, "www.race4-redirect.com")

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Equal(t, 2, run.ImportSucceeded, "both entries settle as success")
	require.Equal(t, 0, run.ImportFailed)

	sess := mustSession(t, h, run.SessionID)
	require.Len(t, sess.Items, 2)
	var redirect *domain.DiscoveryImportItem
	for i := range sess.Items {
		item := &sess.Items[i]
		require.Equal(t, domain.DiscoveryItemSuccess, item.Result, "the duplicate hit is success semantics")
		require.NotEmpty(t, item.MappedCertID)
		if item.ErrorReason != "" {
			redirect = item
		}
	}
	require.NotNil(t, redirect, "the latecomer entry carries the idempotent replay note")
	require.Equal(t, synctest.ReasonAlreadyInLedger, redirect.ErrorReason,
		"the replay note is the static ALREADY_IN_LEDGER text")
	cert, err := h.LedgerByFP(fp)
	require.NoError(t, err)
	require.Equal(t, cert.ID.Hex(), redirect.MappedCertID, "the redirect binds the existing certificate")
}

// TestManualSchedulerRace_Step4_BothSessionsSuccessNoFailed verifies Outcome
// "both-sessions-success-no-failed": both rounds of an overlapping window
// finish with success entries only — the failure counters stay zero ( the
// idempotent semantics never degrade into failed entries ).
func TestManualSchedulerRace_Step4_BothSessionsSuccessNoFailed(t *testing.T) {
	h := newJourneyHarness(t)
	fpA := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.race4-both-a.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fpA))

	first := h.MustSync()
	require.Equal(t, 1, first.ImportSucceeded)
	require.Equal(t, 0, first.ImportFailed)

	fpV := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.race4-both-v.com")
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", fpV))

	second := h.MustSync()
	require.Equal(t, 1, second.ImportSucceeded)
	require.Equal(t, 0, second.ImportFailed)

	require.Equal(t, int32(2), h.Sessions.Created(), "both rounds own a session")
	for _, sessID := range []string{first.SessionID, second.SessionID} {
		sess := mustSession(t, h, sessID)
		require.NotEmpty(t, sess.Items)
		for _, item := range sess.Items {
			require.Equal(t, domain.DiscoveryItemSuccess, item.Result, "no entry degrades to failed")
		}
	}
}

// TestManualSchedulerRace_Step4_NoSecondLedgerRow verifies Outcome
// "no-second-ledger-row": when both rounds ( or both entries of one round )
// attempt to ledger the same fingerprint, the unique constraint guarantees
// exactly one row and the latecomer does not re-parse the chain into a second
// record.
func TestManualSchedulerRace_Step4_NoSecondLedgerRow(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedSameFingerprintOnBothClouds(t, h, "www.race4-onerow.com")

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)

	ledger := h.Ledger()
	require.Len(t, ledger, 1, "the ledger row count never grows with the race")
	require.Equal(t, fp, ledger[0].Fingerprint)
	require.Equal(t, int32(1), h.Sessions.Created(), "one round, one session for the shared fingerprint")
	require.Len(t, h.ActiveMappings(), 2, "each cloud account still gets its own mapping")
}
