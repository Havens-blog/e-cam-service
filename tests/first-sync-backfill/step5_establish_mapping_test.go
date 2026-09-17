// @feature cert-volcano-import-sync @api-functional
//
// Contract step-5-establish-mapping ( first-sync-backfill ): successfully
// imported instances get one ( fingerprint, cloud, accountKey ) mapping each
// via the idempotent Upsert. Outcomes: success / rerun-no-duplicate-mapping /
// mapping-binding-consistency.
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

// TestFirstSyncBackfill_Step5_EstablishMappingSuccess verifies Outcome
// "success": after a successful import exactly one mapping exists for the
// instance triple and the ledger row count is unchanged by mapping writes.
func TestFirstSyncBackfill_Step5_EstablishMappingSuccess(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.step5-mapping.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Len(t, h.Ledger(), 1, "one imported certificate")
	require.Len(t, h.ActiveMappings(), 1, "one mapping per instance triple")

	mapping := h.ActiveMappings()[0]
	require.Equal(t, fp, mapping.CertFingerprint)
	require.Equal(t, "aliyun", mapping.Cloud)
	require.Equal(t, AliyunAccount, mapping.AccountKey)
	require.Equal(t, "cert-a1", mapping.CloudCertID)
	require.NotEmpty(t, run.SessionID)
}

// TestFirstSyncBackfill_Step5_RerunNoDuplicateMapping verifies Outcome
// "rerun-no-duplicate-mapping": re-processing an instance whose mapping
// already exists is judged as an already-mapped skip — the Upsert unique key
// stays idempotent and no second mapping row appears.
func TestFirstSyncBackfill_Step5_RerunNoDuplicateMapping(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.step5-rerun.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))

	first := h.MustSync()
	require.Equal(t, 1, first.ImportSucceeded, "first round imports the instance")
	require.Equal(t, 1, len(h.ActiveMappings()))

	second := h.MustSync()
	require.Equal(t, "completed", second.Status)
	require.Equal(t, 1, second.Skipped, "the mapped instance re-judges as a skip")
	require.Equal(t, 0, second.Backfilled, "no duplicate backfill on rerun")
	require.Len(t, h.ActiveMappings(), 1, "mapping row count unchanged")
	require.Len(t, h.Ledger(), 1, "ledger row count unchanged")
}

// TestFirstSyncBackfill_Step5_MappingBindingConsistency verifies Outcome
// "mapping-binding-consistency": the mapping row's fingerprint matches the
// ledger certificate and its triple matches the source instance ( one-to-one
// binding between mapping and ledger ).
func TestFirstSyncBackfill_Step5_MappingBindingConsistency(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.step5-bind.com")
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", fp))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)

	mappings := h.ActiveMappings()
	require.Len(t, mappings, 1)
	mapping := mappings[0]
	cert, err := h.LedgerByFP(mapping.CertFingerprint)
	require.NoError(t, err, "the mapping fingerprint resolves to a ledger certificate")

	// Cross-entity binding: mapping.fingerprint == ledger cert fingerprint,
	// triple == the source instance metadata, session entry binds the cert.
	require.Equal(t, fp, cert.Fingerprint)
	require.Equal(t, "volcano", mapping.Cloud)
	require.Equal(t, VolcanoAccount, mapping.AccountKey)
	require.Equal(t, "cert-v1", mapping.CloudCertID)
	sess := mustSession(t, h, run.SessionID)
	require.Equal(t, cert.ID.Hex(), sess.Items[0].MappedCertID)
	require.Equal(t, domain.HostingStatusFingerprintOnly, cert.HostingStatus,
		"import path registers fingerprint-only ( no key material )")
}
