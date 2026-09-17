// @feature cert-volcano-import-sync @api-functional
//
// Contract step-4-import-unlisted ( first-sync-backfill ): unlisted
// fingerprints enter the existing discovery-import idempotent pipeline
// ( synchronous face ). Outcomes: success / cross-cloud-same-fingerprint /
// chain-fetch-failed.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package first_sync_backfill

import (
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/require"
)

// TestFirstSyncBackfill_Step4_ImportUnlistedSuccess verifies Outcome
// "success": an unlisted fingerprint is imported through the existing
// pipeline ( chain fetch + parse ), the ledger gains the record, and the
// session entry binds the new certificate.
func TestFirstSyncBackfill_Step4_ImportUnlistedSuccess(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.step4-import.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Equal(t, 1, run.Imported)
	require.Equal(t, 1, run.ImportSucceeded)
	require.Equal(t, 0, run.ImportFailed)

	sess := mustSession(t, h, run.SessionID)
	require.Len(t, sess.Items, 1)
	item := sess.Items[0]
	require.Equal(t, domain.DiscoveryItemSuccess, item.Result)
	require.Equal(t, "aliyun", item.Cloud)
	require.Equal(t, AliyunAccount, item.AccountKey)
	require.Equal(t, "cert-a1", item.CloudCertID)

	cert, err := h.LedgerByFP(fp)
	require.NoError(t, err, "the unlisted fingerprint is now registered")
	require.Equal(t, cert.ID.Hex(), item.MappedCertID, "session entry binds the newly imported certificate")
}

// TestFirstSyncBackfill_Step4_CrossCloudSameFingerprint verifies Outcome
// "cross-cloud-same-fingerprint": two clouds carrying identical content
// ( same fingerprint ) in one round converge to exactly one ledger record —
// the first import wins, the second hits ErrDuplicateFingerprint and is
// absorbed as a mapping backfill ( success semantics, never a failure ) —
// with one mapping per cloud account.
func TestFirstSyncBackfill_Step4_CrossCloudSameFingerprint(t *testing.T) {
	h := newJourneyHarness(t)
	// The same chain PEM is served by both clouds -> same parsed fingerprint.
	bundle := seedBundle(t, h, domain.CloudAliyun, "cert-a1", "www.step4-same.com")
	h.Material(synctest.CloudVolcano).AddBundle("cert-v1", bundle)
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", bundle.Fingerprint))
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", bundle.Fingerprint))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status, "duplicate absorption is success semantics, not failure")
	require.Equal(t, 2, run.Imported, "both triples entered the import path")
	require.Equal(t, 2, run.ImportSucceeded, "both entries converge as success")
	require.Equal(t, 0, run.ImportFailed)
	require.Empty(t, run.Failures)

	ledgers := h.Ledger()
	require.Len(t, ledgers, 1, "exactly one ledger record for the shared fingerprint")
	require.Equal(t, bundle.Fingerprint, ledgers[0].Fingerprint)
	mappings := h.ActiveMappings()
	require.Len(t, mappings, 2, "one mapping per cloud account")
	for _, m := range mappings {
		require.Equal(t, bundle.Fingerprint, m.CertFingerprint, "both mappings bind the single ledger certificate")
	}
}

// TestFirstSyncBackfill_Step4_ChainFetchFailed verifies Outcome
// "chain-fetch-failed": one instance's material fetch fails cloud-side — the
// failed entry records a static errorReason ( no cloud error detail ), the
// remaining instances converge, and the round settles partial_failed.
func TestFirstSyncBackfill_Step4_ChainFetchFailed(t *testing.T) {
	h := newJourneyHarness(t)
	injected := "volcano sdk timeout: request 429 from 10.0.0.9"
	fpOK := seedMaterial(t, h, domain.CloudAliyun, "cert-ok", "www.step4-ok.com")
	fpBad := seedMaterial(t, h, domain.CloudAliyun, "cert-bad", "www.step4-bad.com")
	h.Material(domain.CloudAliyun).AddError("cert-bad", errors.New(injected))
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount,
		synctest.Instance("cert-ok", fpOK), synctest.Instance("cert-bad", fpBad))

	run := h.MustSync()
	require.Equal(t, "partial_failed", run.Status, "the failed entry flips the round terminal state")
	require.Equal(t, 2, run.Imported)
	require.Equal(t, 1, run.ImportSucceeded, "the healthy instance converges")
	require.Equal(t, 1, run.ImportFailed)

	sess := mustSession(t, h, run.SessionID)
	var failed, ok *domain.DiscoveryImportItem
	for i := range sess.Items {
		switch sess.Items[i].CloudCertID {
		case "cert-bad":
			failed = &sess.Items[i]
		case "cert-ok":
			ok = &sess.Items[i]
		}
	}
	require.NotNil(t, failed)
	require.NotNil(t, ok)
	require.Equal(t, domain.DiscoveryItemFailed, failed.Result)
	require.Equal(t, synctest.ReasonGetCertFailed, failed.ErrorReason, "static reason text only")
	require.NotContains(t, failed.ErrorReason, "429", "no cloud error detail in the reason")
	require.NotContains(t, failed.ErrorReason, injected, "no cloud response fragment in the reason")
	require.Equal(t, domain.DiscoveryItemSuccess, ok.Result)
	cert, err := h.LedgerByFP(fpOK)
	require.NoError(t, err, "the healthy instance still registers")
	require.Equal(t, cert.ID.Hex(), ok.MappedCertID)
	_, err = h.LedgerByFP(fpBad)
	require.Error(t, err, "the failed instance leaves no ledger record ( rerunnable )")
}
