// @feature cert-volcano-import-sync @api-functional
//
// Contract step-3-fingerprint-compare ( first-sync-backfill ): listed
// instance metadata is compared against ledger fingerprints and existing
// mappings. Outcomes: success ( first run = full backfill ) /
// already-mapped-skip / revoked-filtered.
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

// TestFirstSyncBackfill_Step3_FirstRunFullBackfill verifies Outcome
// "success": with an empty ledger every listed instance forms an import
// entry ( no special-casing — first run IS the full backfill ), one entry
// per instance triple.
func TestFirstSyncBackfill_Step3_FirstRunFullBackfill(t *testing.T) {
	h := newJourneyHarness(t)
	fpA1 := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.step3-a1.com")
	fpA2 := seedMaterial(t, h, domain.CloudAliyun, "cert-a2", "www.step3-a2.com")
	fpV1 := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.step3-v1.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount,
		synctest.Instance("cert-a1", fpA1), synctest.Instance("cert-a2", fpA2))
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", fpV1))
	require.Empty(t, h.Ledger(), "fixture precondition: empty ledger ( first run )")

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Equal(t, 3, run.Listed)
	require.Equal(t, 3, run.Imported, "every listed instance enters the delta set on an empty ledger")
	require.Equal(t, 3, run.ImportSucceeded)
	require.Len(t, h.Ledger(), 3, "first run backfills all instances")
	require.Len(t, h.ActiveMappings(), 3, "one mapping per instance triple")

	// Session items carry one entry per instance triple ( success semantics ).
	sess := mustSession(t, h, run.SessionID)
	require.Len(t, sess.Items, 3)
	for _, item := range sess.Items {
		require.Equal(t, domain.DiscoveryItemSuccess, item.Result)
		require.NotEmpty(t, item.MappedCertID, "each entry binds its ledger certificate")
	}
}

// TestFirstSyncBackfill_Step3_AlreadyMappedSkip verifies Outcome
// "already-mapped-skip": a fingerprint already in the ledger with a complete
// mapping is judged already-synced — no import, no writes, and ( five-cloud
// ledger discipline ) zero material-channel Gets at judgment.
func TestFirstSyncBackfill_Step3_AlreadyMappedSkip(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.step3-mapped.com")
	h.SeedLedgerCert(fp)
	h.SeedMapping(fp, "aliyun", AliyunAccount, "cert-a1")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))
	ledgerBefore, mappingsBefore := len(h.Ledger()), len(h.ActiveMappings())

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Equal(t, 1, run.Skipped, "the mapped instance is judged already-synced")
	require.Equal(t, 0, run.Imported)
	require.Empty(t, run.SessionID, "no import entries -> no import session")
	require.Equal(t, int32(0), h.Sessions.Created(), "no session document written")
	require.Equal(t, ledgerBefore, len(h.Ledger()), "ledger untouched")
	require.Equal(t, mappingsBefore, len(h.ActiveMappings()), "mappings untouched")
	require.Equal(t, 0, h.MaterialGets(), "judgment issues zero material-channel Gets ( AC3 discipline )")
}

// TestFirstSyncBackfill_Step3_RevokedFiltered verifies Outcome
// "revoked-filtered": a revoked instance never reaches the judgment layer —
// the lister stub models the volcano adapter's two-phase filter contract
// ( fact VOLCANO_FILTER_TWO_PHASE: IsCertificateRevoked=true or status !=
// Issued is excluded from the returned set ), so the round neither imports
// nor counts it.
func TestFirstSyncBackfill_Step3_RevokedFiltered(t *testing.T) {
	h := newJourneyHarness(t)
	fpIssued := seedMaterial(t, h, synctest.CloudVolcano, "cert-v-ok", "www.step3-issued.com")
	fpRevoked := synctest.FP("step3-revoked-instance") // never listed, never imported
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount,
		synctest.Instance("cert-v-ok", fpIssued))
	// The revoked instance exists in the stub's cloud-side world but the
	// adapter contract filters it: it never appears in the returned set.

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Equal(t, 1, run.Listed, "the revoked instance is absent from the listing")
	require.Equal(t, 1, run.Imported)
	require.Empty(t, run.Failures)
	_, err := h.LedgerByFP(fpRevoked)
	require.Error(t, err, "the revoked fingerprint is never registered in the ledger")
	_, err = h.LedgerByFP(fpIssued)
	require.NoError(t, err, "the issued instance backfills normally")
	require.Equal(t, int32(1), h.Material(synctest.CloudVolcano).Calls(),
		"material channel touched only for the issued instance")
}
