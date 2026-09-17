// @feature cert-volcano-import-sync @api-functional
//
// Contract step-6-session-reconcile ( incremental-skip-drift ): mixed rounds
// reconcile into a failure-free terminal state; drift is observable and
// never degrades into a failed entry. Outcomes: success /
// drift-event-observable / no-failed-entries-after-drift.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package incremental_skip_drift

import (
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/require"
)

// TestIncrementalSkipDrift_Step6_SessionReconcileSuccess verifies Outcome
// "success": a round mixing skip / backfill / drift / import reconciles —
// the ledger carries both drift fingerprints, the newest mapping row wins
// the reverse lookup, the import entry settles success, and the round is
// failure-free.
func TestIncrementalSkipDrift_Step6_SessionReconcileSuccess(t *testing.T) {
	h := newJourneyHarness(t)
	// Skipped: mapped instance.
	seedMappedInstance(t, h, domain.CloudAliyun, AliyunAccount, "cert-a1")
	// Drifted: new fingerprint already ledgered ( ledger-refresh shape ).
	driftOld := synctest.FP("step6-drift-old")
	driftNew := synctest.FP("step6-drift-new")
	h.SeedLedgerCert(driftOld)
	h.SeedLedgerCert(driftNew)
	h.SeedMapping(driftOld, "volcano", VolcanoAccount, "cert-v-drift", time.Now().Add(-time.Hour))
	// Backfilled: ledger fingerprint without mapping.
	backfillFP := synctest.FP("step6-backfill")
	h.SeedLedgerCert(backfillFP)
	// Imported: unknown fingerprint with material.
	importFP := seedMaterial(t, h, synctest.CloudVolcano, "cert-v-new", "www.step6-new.com")
	h.Lister(synctest.CloudVolcano).SetInstances(VolcanoAccount,
		synctest.Instance("cert-v-drift", driftNew),
		synctest.Instance("cert-v-bf", backfillFP),
		synctest.Instance("cert-v-new", importFP))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status, "zero-failure mixed rounds settle completed")
	require.Equal(t, 1, run.Skipped)
	require.Equal(t, 1, run.Backfilled)
	require.Equal(t, 1, run.Drifted, "the ledger-refresh drift is counted")
	require.Equal(t, 1, run.Imported)
	require.Equal(t, 0, run.ImportFailed)
	require.Empty(t, run.Failures)

	// Ledger: skipped instance's cert + old/new drift pair + backfill + import.
	require.Len(t, h.Ledger(), 5, "one record per fingerprint ( mapped + drift pair + backfill + import )")
	require.Equal(t, driftNew, mustMappingFP(t, h, "volcano", VolcanoAccount, "cert-v-drift"))
	sess := mustSession(t, h, run.SessionID)
	require.Equal(t, domain.DiscoveryImportCompleted, sess.Status)
	require.Len(t, sess.Items, 1, "only the unknown fingerprint produced an import entry")
	require.Equal(t, domain.DiscoveryItemSuccess, sess.Items[0].Result)
}

// TestIncrementalSkipDrift_Step6_DriftEventObservable verifies Outcome
// "drift-event-observable": the drift semantics are observable — Drifted
// counts up, the drifted triple's reverse lookup resolves the new
// fingerprint, the round's import entry binds the new fingerprint certificate,
// and the session operator attributes the source.
func TestIncrementalSkipDrift_Step6_DriftEventObservable(t *testing.T) {
	h := newJourneyHarness(t)
	// Drift A: new fingerprint already ledgered -> Drifted counter.
	driftOld := synctest.FP("step6-observable-old")
	driftNew := synctest.FP("step6-observable-new")
	h.SeedLedgerCert(driftOld)
	h.SeedLedgerCert(driftNew)
	h.SeedMapping(driftOld, "volcano", VolcanoAccount, "cert-v1", time.Now().Add(-time.Hour))
	// Import entry: an unknown fingerprint imports and binds the new cert.
	importFP := seedMaterial(t, h, synctest.CloudVolcano, "cert-v-new", "www.step6-observable.com")
	h.Lister(synctest.CloudVolcano).SetInstances(VolcanoAccount,
		synctest.Instance("cert-v1", driftNew), synctest.Instance("cert-v-new", importFP))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Equal(t, 1, run.Drifted, "the drift event is counted in the summary")
	require.Equal(t, 1, run.Imported)
	require.Equal(t, 0, run.ImportFailed)
	require.Equal(t, driftNew, mustMappingFP(t, h, "volcano", VolcanoAccount, "cert-v1"),
		"the drift refresh points the reverse lookup at the new fingerprint")
	require.Len(t, h.MappingsByFP(driftOld), 1, "the old mapping stays observable in retention")

	sess := mustSession(t, h, run.SessionID)
	require.Equal(t, "manual", sess.Operator, "the manual face attributes the source ( the scheduler face is asserted by first-sync-backfill step-1 )")
	require.Len(t, sess.Items, 1)
	cert, err := h.LedgerByFP(importFP)
	require.NoError(t, err)
	require.Equal(t, cert.ID.Hex(), sess.Items[0].MappedCertID,
		"the import entry binds the newly fingerprinted certificate")
}

// TestIncrementalSkipDrift_Step6_NoFailedEntriesAfterDrift verifies Outcome
// "no-failed-entries-after-drift": rounds containing only drift / backfill /
// skip produce zero failures — the idempotent semantics never degrade into
// failed entries and the terminal state is completed.
func TestIncrementalSkipDrift_Step6_NoFailedEntriesAfterDrift(t *testing.T) {
	h := newJourneyHarness(t)
	driftOld := synctest.FP("step6-nofail-old")
	driftNew := synctest.FP("step6-nofail-new")
	h.SeedLedgerCert(driftOld)
	h.SeedLedgerCert(driftNew)
	h.SeedMapping(driftOld, "volcano", VolcanoAccount, "cert-v1", time.Now().Add(-time.Hour))
	backfillFP := synctest.FP("step6-nofail-bf")
	h.SeedLedgerCert(backfillFP)
	h.Lister(synctest.CloudVolcano).SetInstances(VolcanoAccount,
		synctest.Instance("cert-v1", driftNew), synctest.Instance("cert-v-bf", backfillFP))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status, "drift + backfill + skip is failure-free")
	require.Equal(t, 0, run.ImportFailed, "the failure counter stays zero")
	require.Empty(t, run.Failures, "the failure list stays empty")
	require.Equal(t, int32(0), h.Sessions.Created(), "no import session -> no failed entries anywhere")
}
