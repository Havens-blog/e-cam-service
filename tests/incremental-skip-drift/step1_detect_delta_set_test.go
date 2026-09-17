// @feature cert-volcano-import-sync @api-functional
//
// Contract step-1-detect-delta-set ( incremental-skip-drift ): the judgment
// layer splits the listing into the four delta states against ledger
// fingerprints and existing mappings. Outcomes: success ( mixed delta ) /
// all-mapped-zero-action / empty-fingerprint-degraded.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package incremental_skip_drift

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/require"
)

// TestIncrementalSkipDrift_Step1_DeltaSetDetection verifies Outcome
// "success": only "fingerprint not in ledger" or "mapping missing/drifted"
// instances enter import/refresh actions, the rest skip, and the four-state
// counters reflect the delta composition.
func TestIncrementalSkipDrift_Step1_DeltaSetDetection(t *testing.T) {
	h := newJourneyHarness(t)
	// Skipped: mapped instance on aliyun.
	skippedFP := seedMappedInstance(t, h, domain.CloudAliyun, AliyunAccount, "cert-a1")
	// Backfilled: fingerprint in the ledger but no mapping for this triple.
	backfillFP := synctest.FP("step1-backfill")
	h.SeedLedgerCert(backfillFP)
	// Imported: unknown fingerprint with import material.
	importFP := seedMaterial(t, h, synctest.CloudVolcano, "cert-v-new", "www.step1-new.com")
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount,
		synctest.Instance("cert-v-bf", backfillFP), synctest.Instance("cert-v-new", importFP))
	ledgerBefore := len(h.Ledger())

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Equal(t, 1, run.Skipped, "the mapped instance lands in the skip set")
	require.Equal(t, 1, run.Backfilled, "the mapping-missing instance lands in the backfill set")
	require.Equal(t, 1, run.Imported, "the unknown fingerprint enters the import path")
	require.Equal(t, 0, run.Drifted)
	require.Equal(t, 0, run.ImportFailed)

	require.Len(t, h.Ledger(), ledgerBefore+1, "only the unknown fingerprint added a ledger row")
	mapping, err := h.LatestMappingByCloudCert("volcano", VolcanoAccount, "cert-v-bf")
	require.NoError(t, err)
	require.Equal(t, backfillFP, mapping.CertFingerprint, "backfill wrote the missing mapping")
	require.Equal(t, skippedFP, mustMappingFP(t, h, "aliyun", AliyunAccount, "cert-a1"))
}

// TestIncrementalSkipDrift_Step1_AllMappedZeroAction verifies Outcome
// "all-mapped-zero-action": with every instance mapped the round takes zero
// actions — no import, no ledger/mapping writes, no import session, and
// ( five-cloud ledger discipline ) zero material-channel Gets at judgment.
func TestIncrementalSkipDrift_Step1_AllMappedZeroAction(t *testing.T) {
	h := newJourneyHarness(t)
	seedMappedInstance(t, h, domain.CloudAliyun, AliyunAccount, "cert-a1")
	seedMappedInstance(t, h, synctest.CloudVolcano, VolcanoAccount, "cert-v1")
	ledgerBefore, mappingsBefore := len(h.Ledger()), len(h.ActiveMappings())

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Equal(t, 2, run.Skipped)
	require.Equal(t, 0, run.Imported)
	require.Empty(t, run.SessionID, "no import entries -> no import session handle")
	require.Equal(t, int32(0), h.Sessions.Created(), "no session document written")
	require.Equal(t, ledgerBefore, len(h.Ledger()), "ledger untouched")
	require.Equal(t, mappingsBefore, len(h.ActiveMappings()), "mappings untouched")
	require.Equal(t, 0, h.MaterialGets(), "zero material-channel Gets ( volcano List-internal Get cost is adapter-inherent and out of this contract's scope )")
}

// TestIncrementalSkipDrift_Step1_EmptyFingerprintDegraded verifies Outcome
// "empty-fingerprint-degraded": an instance whose listing cannot verify the
// fingerprint skips when mapped and converts to an import entry when
// unmapped ( the pipeline resolves the real fingerprint and backfills the
// mapping ) — never a misjudged failure.
func TestIncrementalSkipDrift_Step1_EmptyFingerprintDegraded(t *testing.T) {
	t.Run("mapped instance degrades to skip", func(t *testing.T) {
		h := newJourneyHarness(t)
		fp := synctest.FP("step1-degraded-mapped")
		h.SeedLedgerCert(fp)
		h.SeedMapping(fp, "volcano", VolcanoAccount, "cert-v1")
		seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount,
			synctest.Instance("cert-v1", "")) // listing cannot verify the fingerprint

		run := h.MustSync()
		require.Equal(t, "completed", run.Status)
		require.Equal(t, 1, run.Skipped, "mapped + unverifiable fingerprint converges as a skip")
		require.Equal(t, 0, run.Imported)
		require.Empty(t, run.Failures, "the degraded shape is not a failure")
	})

	t.Run("unmapped instance degrades to import", func(t *testing.T) {
		h := newJourneyHarness(t)
		realFP := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.step1-degraded.com")
		seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount,
			synctest.Instance("cert-v1", "")) // listing cannot verify the fingerprint

		run := h.MustSync()
		require.Equal(t, "completed", run.Status)
		require.Equal(t, 1, run.Imported, "unmapped + unverifiable fingerprint converts to an import entry")
		require.Equal(t, 1, run.ImportSucceeded)
		require.Empty(t, run.Failures)

		cert, err := h.LedgerByFP(realFP)
		require.NoError(t, err, "the pipeline registered the real parsed fingerprint")
		mapping, err := h.LatestMappingByCloudCert("volcano", VolcanoAccount, "cert-v1")
		require.NoError(t, err)
		require.Equal(t, realFP, mapping.CertFingerprint, "mapping binds the resolved fingerprint, not the empty listing value")
		require.Equal(t, cert.ID.Hex(), mustSession(t, h, run.SessionID).Items[0].MappedCertID)
	})
}
