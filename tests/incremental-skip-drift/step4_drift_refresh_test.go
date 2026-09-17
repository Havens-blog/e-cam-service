// @feature cert-volcano-import-sync @api-functional
//
// Contract step-4-drift-refresh ( incremental-skip-drift ): a re-issued
// instance ( same cloudCertID, new fingerprint ) refreshes the mapping while
// the old row is retained. Outcomes: success ( import-path drift ) /
// duplicate-fingerprint-redirect / resigned-then-revoked.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package incremental_skip_drift

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/require"
)

// TestIncrementalSkipDrift_Step4_DriftRefreshSuccess verifies Outcome
// "success": the new fingerprint enters the ledger through the existing
// import pipeline, a new mapping row is written ( refresh semantics ), and
// the old mapping row stays — the reverse lookup resolves the new
// fingerprint. ( When the new fingerprint is already ledgered the judgment
// refreshes the mapping in place and counts Drifted — that shape is asserted
// by step-6 drift-event-observable. )
func TestIncrementalSkipDrift_Step4_DriftRefreshSuccess(t *testing.T) {
	h := newJourneyHarness(t)
	oldFP := synctest.FP("step4-old-fingerprint")
	seedDriftPrecondition(t, h, synctest.CloudVolcano, VolcanoAccount, "cert-v1", oldFP)
	newFP := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.step4-resigned.com")
	h.Lister(synctest.CloudVolcano).SetInstances(VolcanoAccount,
		synctest.Instance("cert-v1", newFP))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status, "drift is refresh semantics, not failure")
	require.Equal(t, 1, run.Imported, "the unseen new fingerprint takes the import path")
	require.Equal(t, 1, run.ImportSucceeded)
	require.Equal(t, 0, run.ImportFailed)

	// Ledger: old and new fingerprints each exactly one record.
	_, err := h.LedgerByFP(oldFP)
	require.NoError(t, err, "old fingerprint retained in the ledger")
	_, err = h.LedgerByFP(newFP)
	require.NoError(t, err, "new fingerprint written to the ledger")

	// Mappings: a new row for the new fingerprint, the old row retained.
	require.Len(t, h.MappingsByFP(oldFP), 1, "old mapping retained ( drift leaves a trace )")
	require.Len(t, h.MappingsByFP(newFP), 1, "new mapping row written")
	require.Equal(t, newFP, mustMappingFP(t, h, "volcano", VolcanoAccount, "cert-v1"),
		"reverse lookup resolves the latest fingerprint")
}

// TestIncrementalSkipDrift_Step4_DuplicateFingerprintRedirect verifies
// Outcome "duplicate-fingerprint-redirect": the re-issued content matches a
// certificate already in the ledger — no second ledger record is produced,
// the mapping is established toward the existing certificate, and the entry
// stays success semantics ( the ledger-refresh judgment shape; the
// ErrDuplicateFingerprint in-round redirect itself is exercised by
// first-sync-backfill step-4 cross-cloud-same-fingerprint ).
func TestIncrementalSkipDrift_Step4_DuplicateFingerprintRedirect(t *testing.T) {
	h := newJourneyHarness(t)
	newFP := synctest.FP("step4-existing-cert")
	h.SeedLedgerCert(newFP) // another certificate already carries this fingerprint
	h.Lister(synctest.CloudVolcano).SetInstances(VolcanoAccount,
		synctest.Instance("cert-v2", newFP))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Equal(t, 1, run.Backfilled, "the judgment establishes the missing mapping")
	require.Equal(t, 0, run.Imported, "no import needed for a ledgered fingerprint")
	require.Equal(t, 0, run.ImportFailed)
	require.Empty(t, run.Failures)

	ledgers := h.Ledger()
	require.Len(t, ledgers, 1, "no second ledger record for the same fingerprint")
	require.Equal(t, newFP, ledgers[0].Fingerprint)
	mapping, err := h.LatestMappingByCloudCert("volcano", VolcanoAccount, "cert-v2")
	require.NoError(t, err)
	require.Equal(t, newFP, mapping.CertFingerprint, "the mapping points at the existing certificate")
}

// TestIncrementalSkipDrift_Step4_ResignedThenRevoked verifies Outcome
// "resigned-then-revoked": the re-issued instance is revoked — the adapter
// two-phase filter ( fact VOLCANO_FILTER_TWO_PHASE, modeled by the lister
// stub contract ) keeps it out of the listing, so nothing is imported, the
// old fingerprint mapping stays in its retained state.
func TestIncrementalSkipDrift_Step4_ResignedThenRevoked(t *testing.T) {
	h := newJourneyHarness(t)
	oldFP := synctest.FP("step4-revoked-old")
	seedDriftPrecondition(t, h, synctest.CloudVolcano, VolcanoAccount, "cert-v1", oldFP)
	revokedFP := synctest.FP("step4-revoked-new")                // exists cloud-side but filtered out
	h.Lister(synctest.CloudVolcano).SetInstances(VolcanoAccount) // the revoked instance never reaches the returned set

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Equal(t, 0, run.Listed, "the revoked instance is absent from the listing")
	require.Equal(t, 0, run.Imported)
	require.Empty(t, run.Failures)

	_, err := h.LedgerByFP(revokedFP)
	require.Error(t, err, "the revoked new fingerprint is never registered")
	require.Len(t, h.MappingsByFP(oldFP), 1, "the old mapping stays in its retained state")
	require.Equal(t, oldFP, mustMappingFP(t, h, "volcano", VolcanoAccount, "cert-v1"))
}
