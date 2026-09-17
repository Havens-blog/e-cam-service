// @feature cert-volcano-import-sync @api-functional
//
// Contract step-2-enumerate-clouds ( first-sync-backfill ): the round
// enumerates every cert-reachable cloud x active account in the fixed order.
// Outcomes: success / empty-account-skip / lister-gap-cloud-skip.
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

// TestFirstSyncBackfill_Step2_EnumerateCloudsAccounts verifies Outcome
// "success": every cloud x active account forms an independent unit and the
// run summary counts clouds ( lister-ready + account read OK ) and accounts
// separately.
func TestFirstSyncBackfill_Step2_EnumerateCloudsAccounts(t *testing.T) {
	h := newJourneyHarness(t)
	// Three units across two clouds: aliyun hosts two accounts.
	h.SeedAccount(domain.CloudAliyun, "acct-a2")
	fp1 := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.step2-a1.com")
	fp2 := seedMaterial(t, h, domain.CloudAliyun, "cert-a2", "www.step2-a2.com")
	fp3 := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.step2-v1.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp1))
	seedInstances(t, h, domain.CloudAliyun, "acct-a2", synctest.Instance("cert-a2", fp2))
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", fp3))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Equal(t, 2, run.CloudsScanned, "only lister-ready clouds with successful account reads count")
	require.Equal(t, 3, run.AccountsScanned, "every enumerated (cloud, account) pair counts")
	require.Equal(t, 3, run.Listed, "each unit lists its own instance metadata")
	require.Empty(t, run.Failures)
}

// TestFirstSyncBackfill_Step2_EmptyAccountSkip verifies Outcome
// "empty-account-skip": an account whose cert library lists zero instances
// is a skip, not a failure — no errorReason, other units continue.
func TestFirstSyncBackfill_Step2_EmptyAccountSkip(t *testing.T) {
	h := newJourneyHarness(t)
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount) // empty listing
	fp := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.step2-empty.com")
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", fp))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status, "empty enumeration is not a failure")
	require.Equal(t, 2, run.AccountsScanned, "the empty account was still enumerated")
	require.Equal(t, 1, run.Listed, "the empty account contributes no listed instances")
	require.Empty(t, run.Failures, "no errorReason recorded for an empty listing")
	// Only the healthy unit produced entries: the empty aliyun unit produced none.
	ledger := h.Ledger()
	require.Len(t, ledger, 1, "no import entries produced by the empty unit")
	require.Equal(t, fp, ledger[0].Fingerprint)
}

// TestFirstSyncBackfill_Step2_ListerGapCloudSkip verifies Outcome
// "lister-gap-cloud-skip": a cert-reachable cloud without a registered
// cert-library lister is silently skipped ( capability gap, not a failure );
// other clouds continue.
func TestFirstSyncBackfill_Step2_ListerGapCloudSkip(t *testing.T) {
	h := synctest.NewHarness(t, func(c *synctest.Config) {
		c.Listers = []domain.Cloud{domain.CloudAliyun} // tencent has no lister port
	})
	h.SeedAccount(domain.CloudAliyun, AliyunAccount)
	h.SeedAccount(domain.CloudTencent, "acct-tx") // enumerable account, no lister
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.step2-gap.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.Equal(t, 1, run.CloudsScanned, "the gap cloud is not counted as scanned")
	require.Equal(t, 1, run.AccountsScanned, "the gap cloud's account is not enumerated")
	require.Empty(t, run.Failures, "capability gap records no failure")
	require.Len(t, h.Ledger(), 1, "the lister-ready cloud still completes its round")
}
