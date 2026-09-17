// @feature cert-volcano-import-sync @api-functional
//
// Contract step-5-old-mapping-reverse-lookup ( incremental-skip-drift ): old
// mapping rows are retained ( never deleted ) and the reverse lookup by
// cloud cert ID always resolves the latest uploadedAt row. Outcomes:
// success / multi-history-order / old-mapping-not-deleted.
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

// TestIncrementalSkipDrift_Step5_ReverseLookupLatest verifies Outcome
// "success": after a completed drift both mapping rows coexist, the reverse
// lookup resolves the new fingerprint, and the drift is observable in the
// summary.
func TestIncrementalSkipDrift_Step5_ReverseLookupLatest(t *testing.T) {
	h := newJourneyHarness(t)
	oldFP := synctest.FP("step5-old")
	seedDriftPrecondition(t, h, synctest.CloudVolcano, VolcanoAccount, "cert-v1", oldFP)
	newFP := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.step5-latest.com")
	h.Lister(synctest.CloudVolcano).SetInstances(VolcanoAccount,
		synctest.Instance("cert-v1", newFP))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Len(t, h.MappingsByFP(oldFP), 1, "the old mapping is retained")
	require.Len(t, h.MappingsByFP(newFP), 1, "the new mapping row exists")
	require.Equal(t, newFP, mustMappingFP(t, h, "volcano", VolcanoAccount, "cert-v1"),
		"the reverse lookup ( uploadedAt descending ) resolves the new fingerprint")
	require.NotEmpty(t, run.SessionID, "the drift went through the import pipeline")
}

// TestIncrementalSkipDrift_Step5_MultiHistoryOrder verifies Outcome
// "multi-history-order": several historical mappings for the same cloudCertID
// ( multiple re-issues ) all survive and the reverse lookup resolves the
// strictly-latest uploadedAt row.
func TestIncrementalSkipDrift_Step5_MultiHistoryOrder(t *testing.T) {
	h := newJourneyHarness(t)
	fp1 := synctest.FP("step5-history-1")
	fp2 := synctest.FP("step5-history-2")
	fp3 := synctest.FP("step5-history-3")
	now := time.Now()
	h.SeedLedgerCert(fp1)
	h.SeedLedgerCert(fp2)
	h.SeedLedgerCert(fp3)
	h.SeedMapping(fp1, "volcano", VolcanoAccount, "cert-v1", now.Add(-3*time.Hour))
	h.SeedMapping(fp2, "volcano", VolcanoAccount, "cert-v1", now.Add(-2*time.Hour))
	h.SeedMapping(fp3, "volcano", VolcanoAccount, "cert-v1", now.Add(-1*time.Hour))
	// The cloud currently serves the newest fingerprint: the round judges it
	// through the reverse lookup ( mapped + same fingerprint -> skip ).
	h.Lister(synctest.CloudVolcano).SetInstances(VolcanoAccount,
		synctest.Instance("cert-v1", fp3))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Equal(t, 1, run.Skipped, "the current fingerprint is judged mapped through the reverse lookup")
	require.Equal(t, 0, run.ImportFailed)

	latest, err := h.LatestMappingByCloudCert("volcano", VolcanoAccount, "cert-v1")
	require.NoError(t, err)
	require.Equal(t, fp3, latest.CertFingerprint, "the reverse lookup resolves the strictly-latest uploadedAt row")
	require.Len(t, h.MappingsByFP(fp1), 1, "history row 1 retained")
	require.Len(t, h.MappingsByFP(fp2), 1, "history row 2 retained")
	require.Len(t, h.MappingsByFP(fp3), 1, "history row 3 retained")
}

// TestIncrementalSkipDrift_Step5_OldMappingNotDeleted verifies Outcome
// "old-mapping-not-deleted": after a completed drift no delete action has
// happened — the old row survives untouched and never influences the
// judgment or the reverse lookup ( orphan recycling is out of scope for this
// feature ).
func TestIncrementalSkipDrift_Step5_OldMappingNotDeleted(t *testing.T) {
	h := newJourneyHarness(t)
	oldFP := synctest.FP("step5-keep-old")
	seedDriftPrecondition(t, h, synctest.CloudVolcano, VolcanoAccount, "cert-v1", oldFP)
	newFP := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.step5-keep.com")
	h.Lister(synctest.CloudVolcano).SetInstances(VolcanoAccount,
		synctest.Instance("cert-v1", newFP))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)

	oldRows := h.MappingsByFP(oldFP)
	require.Len(t, oldRows, 1, "the old mapping row still exists ( retention, not deletion )")
	require.Equal(t, domain.MappingStatusActive, oldRows[0].Status,
		"the retained row is untouched ( still active )")
	require.Equal(t, newFP, mustMappingFP(t, h, "volcano", VolcanoAccount, "cert-v1"),
		"the retained row does not shadow the latest fingerprint")
	require.Len(t, h.ActiveMappings(), 2, "both rows coexist in active state")
	_ = run
}
