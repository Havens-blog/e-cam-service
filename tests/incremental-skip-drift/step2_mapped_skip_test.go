// @feature cert-volcano-import-sync @api-functional
//
// Contract step-2-mapped-skip ( incremental-skip-drift ): mapped instances
// are skipped — no import action, no ledger/mapping writes, and skips never
// degrade into failures. Outcomes: success / volcano-skip-semantics /
// skip-not-failure.
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

// TestIncrementalSkipDrift_Step2_MappedSkipSuccess verifies Outcome
// "success": a fingerprint in the ledger with a complete same-fingerprint
// mapping skips — Skipped counts up, nothing is written.
func TestIncrementalSkipDrift_Step2_MappedSkipSuccess(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMappedInstance(t, h, domain.CloudAliyun, AliyunAccount, "cert-a1")
	ledgerBefore, mappingsBefore := len(h.Ledger()), len(h.ActiveMappings())

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Equal(t, 1, run.Skipped, "the mapped instance is skipped")
	require.Equal(t, 0, run.Imported)
	require.Equal(t, 0, run.Backfilled)
	require.Equal(t, ledgerBefore, len(h.Ledger()), "no ledger write")
	require.Equal(t, mappingsBefore, len(h.ActiveMappings()), "no mapping write")
	require.Equal(t, fp, mustMappingFP(t, h, "aliyun", AliyunAccount, "cert-a1"))
}

// TestIncrementalSkipDrift_Step2_VolcanoSkipSemantics verifies Outcome
// "volcano-skip-semantics": for a volcano instance already in the ledger,
// skip means no import and no ledger/mapping write. The List-internal
// per-instance Get is an adapter-inherent cost ( the volcano SDK listing
// carries no fingerprint field ) and deliberately NOT part of the skip
// contract — the asserted channel is the import material channel, which must
// stay silent.
func TestIncrementalSkipDrift_Step2_VolcanoSkipSemantics(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMappedInstance(t, h, synctest.CloudVolcano, VolcanoAccount, "cert-v1")
	ledgerBefore, mappingsBefore := len(h.Ledger()), len(h.ActiveMappings())

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Equal(t, 1, run.Skipped)
	require.Equal(t, 0, run.Imported, "skip produces no import action")
	require.Equal(t, ledgerBefore, len(h.Ledger()), "no ledger write")
	require.Equal(t, mappingsBefore, len(h.ActiveMappings()), "no mapping write")
	require.Equal(t, 0, h.MaterialGets(), "the import material channel stays silent ( List-internal Get cost is out of the skip contract scope )")
	require.Equal(t, fp, mustMappingFP(t, h, "volcano", VolcanoAccount, "cert-v1"))
}

// TestIncrementalSkipDrift_Step2_SkipNotFailure verifies Outcome
// "skip-not-failure": any number of skips with no other failure surface
// keeps ImportFailed at zero, the failure list empty, and the round
// terminal state completed.
func TestIncrementalSkipDrift_Step2_SkipNotFailure(t *testing.T) {
	h := newJourneyHarness(t)
	seedMappedInstance(t, h, domain.CloudAliyun, AliyunAccount, "cert-a1")
	seedMappedInstance(t, h, synctest.CloudVolcano, VolcanoAccount, "cert-v1")

	run := h.MustSync()
	require.Equal(t, "completed", run.Status, "an all-skip round is completed, never partial_failed")
	require.Equal(t, 2, run.Skipped)
	require.Equal(t, 0, run.ImportFailed)
	require.Empty(t, run.Failures)
	require.Equal(t, int32(0), h.Sessions.Created(), "no session -> no failed entries anywhere")
}
