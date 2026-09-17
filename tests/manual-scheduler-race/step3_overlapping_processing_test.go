// @feature cert-volcano-import-sync @api-functional
//
// Contract step-3-overlapping-processing ( manual-scheduler-race ): rounds
// whose delta sets overlap the same fingerprint converge through idempotent
// semantics. Outcomes: success ( both rounds enter the import path ) /
// cross-cloud-same-fingerprint / import-path-not-blocked-by-cas.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package manual_scheduler_race

import (
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/require"
)

// TestManualSchedulerRace_Step3_OverlappingRoundsConverge verifies Outcome
// "success": two CAS-serialized rounds whose delta sets overlap the same
// fingerprint both enter the import path ( the first fails at the material
// channel leaving the fingerprint unledgered, the second succeeds after the
// material becomes available ) — the write race is absorbed idempotently and
// the convergence shape is unique.
func TestManualSchedulerRace_Step3_OverlappingRoundsConverge(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.race3-overlap.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))
	h.Material(domain.CloudAliyun).AddError("cert-a1", errors.New("cloud unreachable"))

	first := h.MustSync()
	require.Equal(t, "partial_failed", first.Status, "round one enters the import path and fails at the material channel")
	require.Equal(t, 1, first.Imported)
	require.Equal(t, 1, first.ImportFailed)
	require.Empty(t, h.Ledger(), "the overlapping fingerprint is still unledgered")

	h.Material(domain.CloudAliyun).ClearError("cert-a1")
	second := h.MustSync()
	require.Equal(t, "completed", second.Status, "round two enters the import path and converges")
	require.Equal(t, 1, second.Imported, "the second round re-imports the still-unledgered fingerprint")
	require.Equal(t, 1, second.ImportSucceeded)

	require.Len(t, h.Ledger(), 1, "the convergence shape is unique: exactly one ledger row")
	require.Len(t, h.ActiveMappings(), 1, "exactly one mapping")
	require.Equal(t, fp, h.ActiveMappings()[0].CertFingerprint)
}

// TestManualSchedulerRace_Step3_CrossCloudSameFingerprint verifies Outcome
// "cross-cloud-same-fingerprint": two clouds carrying the same content within
// the same processing window converge idempotently — one ledger record, one
// mapping per cloud account, no failed entries.
func TestManualSchedulerRace_Step3_CrossCloudSameFingerprint(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedSameFingerprintOnBothClouds(t, h, "www.race3-same.com")

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Equal(t, 2, run.Imported, "both triples enter the import path")
	require.Equal(t, 2, run.ImportSucceeded, "the duplicate write is absorbed as success")
	require.Equal(t, 0, run.ImportFailed)

	require.Len(t, h.Ledger(), 1, "exactly one ledger record for the shared fingerprint")
	mappings := h.ActiveMappings()
	require.Len(t, mappings, 2, "one mapping per cloud account")
	for _, m := range mappings {
		require.Equal(t, fp, m.CertFingerprint, "both mappings bind the single ledger record")
	}
}

// TestManualSchedulerRace_Step3_ImportPathNotBlockedByCAS verifies Outcome
// "import-path-not-blocked-by-cas": the CAS guards rounds, not entries — a
// later round judges and imports freely ( a new instance added between rounds
// imports normally ), with no deadlock and no entry-level blocking.
func TestManualSchedulerRace_Step3_ImportPathNotBlockedByCAS(t *testing.T) {
	h := newJourneyHarness(t)
	fpA := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.race3-notblocked-a.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fpA))

	first := h.MustSync()
	require.Equal(t, 1, first.ImportSucceeded, "the first round holds and releases the CAS normally")

	fpB := seedMaterial(t, h, domain.CloudAliyun, "cert-a2", "www.race3-notblocked-b.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount,
		synctest.Instance("cert-a1", fpA), synctest.Instance("cert-a2", fpB))

	second := h.MustSync()
	require.Equal(t, "completed", second.Status, "the later round runs free of entry-level blocking")
	require.Equal(t, 1, second.Imported, "the new instance is imported normally")
	require.Equal(t, 1, second.Skipped, "the already-ledgered instance skips")
	require.Len(t, h.Ledger(), 2, "no deadlock residue: both fingerprints ledgered")
}
