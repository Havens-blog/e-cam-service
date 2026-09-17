// @feature cert-volcano-import-sync @api-functional
//
// Journey smoke: first-sync-backfill happy path end-to-end plus one error
// path. Steps 1-6 in sequence: scheduler job trigger -> two-cloud
// enumeration -> empty-ledger full backfill -> ledger + mapping
// establishment -> converged re-run ( read-only ) -> new instance with
// broken material ( error path: static failure reason, partial_failed,
// import-layer failures stay in the session ) -> repaired material re-run
// converges. State flows between steps via the ledger, mappings, and session
// documents.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package first_sync_backfill

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirstSyncBackfill_FullJourneySmoke(t *testing.T) {
	h := newJourneyHarness(t)
	fpA := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.smoke-a1.com")
	fpV := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.smoke-v1.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fpA))
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", fpV))

	// ---- Step 1: 天级调度到达触发同步轮 ( scheduler job entry ) ----
	require.NoError(t, synctest.CertImportJobRun(h.Sync)(context.Background()))
	sess := mustSession(t, h, h.Sessions.LastID())
	assert.Equal(t, "scheduler", sess.Operator, "operator attributes the scheduler source")

	// ---- Steps 2-5: 枚举 -> 全量回填 -> 建立映射 ----
	assert.Len(t, h.Ledger(), 2, "both clouds backfilled on the empty ledger")
	assert.Len(t, h.ActiveMappings(), 2, "one mapping per instance triple")
	_, err := h.LedgerByFP(fpA)
	assert.NoError(t, err)
	_, err = h.LedgerByFP(fpV)
	assert.NoError(t, err)

	// ---- Step 6b: 第二轮空转收敛 ( read-only idempotent convergence ) ----
	second := h.MustSync()
	assert.Equal(t, 2, second.Skipped, "both instances re-judge as skipped")
	assert.Equal(t, 0, second.Imported)
	assert.Empty(t, second.SessionID, "zero delta -> no import session")
	assert.Len(t, h.Ledger(), 2, "ledger unchanged")
	assert.Len(t, h.ActiveMappings(), 2, "mappings unchanged")

	// ---- Error path: 新实例材料通道失败 -> 静态失败因 partial_failed ----
	fpBad := seedMaterial(t, h, domain.CloudAliyun, "cert-bad", "www.smoke-bad.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount,
		synctest.Instance("cert-a1", fpA), synctest.Instance("cert-bad", fpBad))
	h.Material(domain.CloudAliyun).AddError("cert-bad", errors.New("cloud unreachable"))

	broken := h.MustSync()
	assert.Equal(t, "partial_failed", broken.Status, "the failed entry flips the terminal state")
	assert.Equal(t, 1, broken.Imported, "only the unlisted instance enters the import path")
	assert.Equal(t, 0, broken.ImportSucceeded)
	assert.Equal(t, 1, broken.ImportFailed)
	assert.Empty(t, broken.Failures, "import-layer failures live in the session entries, not the enum-layer failure list")
	assert.Len(t, h.Ledger(), 2, "the failed instance leaves no ledger record")

	brokenSess := mustSession(t, h, broken.SessionID)
	require.Len(t, brokenSess.Items, 1)
	assert.Equal(t, domain.DiscoveryItemFailed, brokenSess.Items[0].Result)
	assert.Equal(t, synctest.ReasonGetCertFailed, brokenSess.Items[0].ErrorReason, "static reason text only")

	// ---- Step 4b/5b: 修复后重跑幂等收敛 ( failed face backfills ) ----
	h.Material(domain.CloudAliyun).ClearError("cert-bad")
	repaired := h.MustSync()
	assert.Equal(t, "completed", repaired.Status, "the failed face converges on rerun")
	assert.Equal(t, 1, repaired.ImportSucceeded, "the previously failed instance imports")
	assert.Equal(t, 2, repaired.Skipped, "already-ledgered instances stay skipped")
	assert.Len(t, h.Ledger(), 3, "no duplicate rows across rounds")
	assert.Len(t, h.ActiveMappings(), 3, "no duplicate mappings across rounds")

	// Journey invariant: no key material ever leaves the service boundary.
	final := h.PostSync()
	synctest.RequireNoKeyMaterial(t, final.Body)
}
