// @feature cert-volcano-import-sync @api-functional
//
// Contract step-3-backfill-mapping ( incremental-skip-drift ): fingerprints
// already in the ledger without a mapping for the triple get their mapping
// backfilled via the idempotent Upsert. Outcomes: success /
// unique-key-replay / upsert-failed-static-reason.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package incremental_skip_drift

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/require"
)

// TestIncrementalSkipDrift_Step3_BackfillMappingSuccess verifies Outcome
// "success": a ledger-known fingerprint with a missing mapping backfills the
// mapping only — the ledger is not duplicated and the entry is success
// semantics.
func TestIncrementalSkipDrift_Step3_BackfillMappingSuccess(t *testing.T) {
	h := newJourneyHarness(t)
	fp := synctest.FP("step3-backfill-target")
	h.SeedLedgerCert(fp)
	h.Lister(synctest.CloudVolcano).SetInstances(VolcanoAccount,
		synctest.Instance("cert-v1", fp))
	ledgerBefore := len(h.Ledger())

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Equal(t, 1, run.Backfilled, "the missing mapping is backfilled")
	require.Equal(t, 0, run.Imported, "the fingerprint is already in the ledger — no import")
	require.Equal(t, 0, run.ImportFailed)
	require.Len(t, h.Ledger(), ledgerBefore, "the ledger is not duplicated")

	mapping, err := h.LatestMappingByCloudCert("volcano", VolcanoAccount, "cert-v1")
	require.NoError(t, err)
	require.Equal(t, fp, mapping.CertFingerprint)
	require.Equal(t, domain.MappingStatusActive, mapping.Status)
}

// TestIncrementalSkipDrift_Step3_UniqueKeyReplay verifies Outcome
// "unique-key-replay": replaying the same instance after its mapping was
// backfilled produces no second row — the instance re-judges into the
// already-mapped skip set and Backfilled does not accumulate.
func TestIncrementalSkipDrift_Step3_UniqueKeyReplay(t *testing.T) {
	h := newJourneyHarness(t)
	fp := synctest.FP("step3-replay")
	h.SeedLedgerCert(fp)
	h.Lister(synctest.CloudVolcano).SetInstances(VolcanoAccount,
		synctest.Instance("cert-v1", fp))

	first := h.MustSync()
	require.Equal(t, 1, first.Backfilled, "first round backfills the mapping")
	require.Equal(t, 1, len(h.ActiveMappings()))

	second := h.MustSync()
	require.Equal(t, "completed", second.Status)
	require.Equal(t, 1, second.Skipped, "the replayed instance is now an already-mapped skip")
	require.Equal(t, 0, second.Backfilled, "Backfilled does not accumulate on replay")
	require.Len(t, h.ActiveMappings(), 1, "the unique key yields exactly one row")
}

// TestIncrementalSkipDrift_Step3_UpsertFailedStaticReason verifies Outcome
// "upsert-failed-static-reason": a mapping-write failure records the static
// "mapping backfill failed" reason ( repository/cloud detail stays in the
// service log ), other instances continue, and the round settles
// partial_failed with no mapping written.
func TestIncrementalSkipDrift_Step3_UpsertFailedStaticReason(t *testing.T) {
	h := synctest.NewHarness(t, func(c *synctest.Config) {
		c.Listers = []domain.Cloud{synctest.CloudVolcano}
		c.WrapMappings = func(base *certtest.FakeCloudCertMappingRepo) domain.CloudCertMappingRepository {
			fault := &failUpsertMappings{CloudCertMappingRepository: base}
			fault.fail.Store(true)
			return fault
		}
	})
	h.SeedAccount(synctest.CloudVolcano, VolcanoAccount)

	fpTarget := synctest.FP("step3-upsert-fail")
	h.SeedLedgerCert(fpTarget)
	// A second, already-mapped instance proves the failure stays isolated.
	fpMapped := synctest.FP("step3-upsert-mapped")
	h.SeedLedgerCert(fpMapped)
	h.SeedMapping(fpMapped, "volcano", VolcanoAccount, "cert-v-ok")
	h.Lister(synctest.CloudVolcano).SetInstances(VolcanoAccount,
		synctest.Instance("cert-v-fail", fpTarget), synctest.Instance("cert-v-ok", fpMapped))
	mappingsBefore := len(h.ActiveMappings())

	run := h.MustSync()
	require.Equal(t, "partial_failed", run.Status, "the write failure flips the terminal state")
	require.Equal(t, 1, run.Skipped, "the healthy instance continues unaffected")
	require.Len(t, run.Failures, 1)
	failure := run.Failures[0]
	require.Equal(t, synctest.ReasonMappingFailed, failure.Reason, "static reason text only")
	require.Equal(t, "volcano", failure.Cloud)
	require.Equal(t, VolcanoAccount, failure.AccountKey)
	require.Equal(t, "cert-v-fail", failure.CloudCertID, "the failure locates the failing triple")
	require.NotContains(t, failure.Reason, "injected", "no repository error detail in the response")
	require.Equal(t, mappingsBefore, len(h.ActiveMappings()), "no mapping written for the failed entry")
}
