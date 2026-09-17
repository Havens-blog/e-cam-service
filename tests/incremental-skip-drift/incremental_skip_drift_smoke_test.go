// @feature cert-volcano-import-sync @api-functional
//
// Journey smoke: incremental-skip-drift lifecycle end-to-end plus one error
// path. Sequence: baseline import -> mapped instance skips at zero material
// cost -> the volcano instance is re-issued ( same cloudCertID, new
// fingerprint ) and the refresh + retention + reverse-lookup semantics
// verify -> converged re-run stays read-only -> error path: a mapping-write
// failure isolates as a static failure reason with partial_failed -> a
// healthy re-run converges.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package incremental_skip_drift

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIncrementalSkipDrift_FullJourneySmoke(t *testing.T) {
	h, mappingFault := newJourneyHarnessWithMappingFault(t)

	// ---- Steps 1-2: baseline round, then zero-cost skip on rerun ----
	fpA := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.smoke2-a1.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fpA))
	baseline := h.MustSync()
	assert.Equal(t, 1, baseline.ImportSucceeded, "the baseline round imports the unknown fingerprint")

	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fpA))
	skipped := h.MustSync()
	assert.Equal(t, 1, skipped.Skipped, "the mapped instance re-judges as a skip")
	assert.Equal(t, 0, skipped.Imported)

	// ---- Steps 4-5: re-issue the volcano instance ( same cloudCertID ) ----
	oldFP := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.smoke2-v1-old.com")
	seedDriftPrecondition(t, h, synctest.CloudVolcano, VolcanoAccount, "cert-v1", oldFP)
	newBundle := certtest.NewBundle(t, "www.smoke2-v1-new.com", []string{"www.smoke2-v1-new.com"}, nil)
	h.Material(synctest.CloudVolcano).AddBundle("cert-v1", newBundle)
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", newBundle.Fingerprint))

	drift := h.MustSync()
	assert.Equal(t, "completed", drift.Status, "drift is refresh semantics, not failure")
	assert.Equal(t, 1, drift.Imported, "the unseen new fingerprint takes the import path")
	assert.Equal(t, 0, drift.ImportFailed)
	assert.Len(t, h.MappingsByFP(oldFP), 1, "the old mapping row is retained")
	assert.Len(t, h.MappingsByFP(newBundle.Fingerprint), 1, "the new mapping row is written")
	assert.Equal(t, newBundle.Fingerprint, mustMappingFP(t, h, "volcano", VolcanoAccount, "cert-v1"),
		"the reverse lookup resolves the latest fingerprint")

	// ---- Step 6: converged re-run stays read-only ----
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fpA))
	rerun := h.MustSync()
	assert.Equal(t, 2, rerun.Skipped, "both mapped instances skip")
	assert.Equal(t, 0, rerun.Imported)
	assert.Empty(t, rerun.Failures)

	// ---- Error path: mapping-write failure -> static reason, isolated ----
	mappingFault.fail.Store(true)
	brokenTarget := synctest.FP("smoke2-broken-target")
	h.SeedLedgerCert(brokenTarget)
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount,
		synctest.Instance("cert-v1", newBundle.Fingerprint), synctest.Instance("cert-v-broken", brokenTarget))

	broken := h.MustSync()
	assert.Equal(t, "partial_failed", broken.Status)
	require.Len(t, broken.Failures, 1)
	assert.Equal(t, synctest.ReasonMappingFailed, broken.Failures[0].Reason, "static reason text only")
	assert.Equal(t, "cert-v-broken", broken.Failures[0].CloudCertID, "the failure locates the failing triple")
	assert.Equal(t, 2, broken.Skipped, "the mapped instances ( aliyun + volcano ) are unaffected by the failing unit")

	// ---- Rerun converges: fault released, the failed face retried ----
	mappingFault.fail.Store(false)
	repaired := h.MustSync()
	assert.Equal(t, "completed", repaired.Status, "the failed face converges on rerun")
	assert.Equal(t, 1, repaired.Backfilled, "the previously failed mapping backfills")
	assert.Equal(t, 2, repaired.Skipped, "healthy faces stay skipped")
}
