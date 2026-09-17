// @feature cert-volcano-import-sync @api-functional
//
// Contract step-4-remaining-clouds-complete ( cloud-failure-isolation ): the
// healthy units complete normally while a unit fails, and a rerun backfills
// the failed face. Outcomes: success / rerun-backfills-failed-cloud.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package cloud_failure_isolation

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/require"
)

// TestCloudFailureIsolation_Step4_RemainingCloudsComplete verifies Outcome
// "success": with one failing unit the remaining cloud x accounts converge
// normally — healthy-unit counters accumulate truthfully and the failure does
// not propagate across units.
func TestCloudFailureIsolation_Step4_RemainingCloudsComplete(t *testing.T) {
	h := newJourneyHarness(t)
	// Healthy: aliyun with two units ( one instance + one mapped instance ).
	fpImport := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.cfi4-import.com")
	fpMapped := synctest.FP("cfi4-mapped")
	h.SeedLedgerCert(fpMapped)
	h.SeedMapping(fpMapped, "aliyun", AliyunAccount2, "cert-a2")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fpImport))
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount2, synctest.Instance("cert-a2", fpMapped))
	// Failing: volcano account read fails.
	h.SeedCloudError(synctest.CloudVolcano, errAccountSourceDown)

	run := h.MustSync()
	require.Equal(t, "partial_failed", run.Status, "the failing unit is reflected in the terminal state")
	require.Len(t, run.Failures, 1, "the failure stayed within its unit ( no propagation )")
	require.Equal(t, "volcano", run.Failures[0].Cloud)

	// Healthy units converged with truthful counters.
	require.Equal(t, 1, run.Imported, "the unknown fingerprint imported")
	require.Equal(t, 1, run.Skipped, "the mapped instance skipped")
	require.Equal(t, 1, run.ImportSucceeded)
	require.Equal(t, 0, run.ImportFailed, "no import-layer failure")
	require.Len(t, h.Ledger(), 2, "the healthy cloud's ledger state complete")
}

// TestCloudFailureIsolation_Step4_RerunBackfillsFailedCloud verifies Outcome
// "rerun-backfills-failed-cloud": after a round where a cloud failed, a rerun
// backfills that cloud's still-present instances while the previous round's
// success face is not re-processed.
func TestCloudFailureIsolation_Step4_RerunBackfillsFailedCloud(t *testing.T) {
	h := newJourneyHarness(t)
	fpA := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.cfi4-rerun-a.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fpA))
	fpV := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.cfi4-rerun-v.com")
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", fpV))
	h.SeedCloudError(synctest.CloudVolcano, errAccountSourceDown)

	first := h.MustSync()
	require.Equal(t, "partial_failed", first.Status)
	require.Len(t, h.Ledger(), 1, "the failed cloud's instance was not reachable in round one")
	_, err := h.LedgerByFP(fpV)
	require.Error(t, err, "volcano instance not imported in the failed round")

	// Failure released ( cloud reachable again ), rerun.
	h.SeedCloudError(synctest.CloudVolcano, nil)
	second := h.MustSync()
	require.Equal(t, "completed", second.Status, "the rerun converges")
	require.Equal(t, 1, second.Imported, "the previously failed face imports")
	require.Equal(t, 1, second.Skipped, "the previous success face is not re-processed")
	require.Len(t, h.Ledger(), 2, "no duplicate rows")
	_, err = h.LedgerByFP(fpV)
	require.NoError(t, err, "the failed face's instance is now ledgered")
}
