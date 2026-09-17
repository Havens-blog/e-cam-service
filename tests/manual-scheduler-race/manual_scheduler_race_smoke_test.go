// @feature cert-volcano-import-sync @api-functional
//
// Journey smoke: manual-scheduler-race lifecycle end-to-end plus one error
// path. Sequence: scheduler job round backfills the baseline -> manual round
// converges read-only -> the manual round absorbs a cross-cloud duplicate ->
// error path: a second manual trigger during an in-flight round answers an
// immediate 409 -> the in-flight round's summary sessionId continues into the
// existing progress endpoint.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package manual_scheduler_race

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManualSchedulerRace_FullJourneySmoke(t *testing.T) {
	h := newJourneyHarness(t)
	fpA := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.smoke3-a1.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fpA))

	// ---- Step 1: 定时轮正常启动 ( scheduler job face ) ----
	require.NoError(t, synctest.CertImportJobRun(h.Sync)(context.Background()))
	schedulerSess := mustSession(t, h, h.Sessions.LastID())
	assert.Equal(t, "scheduler", schedulerSess.Operator)
	assert.Len(t, h.Ledger(), 1, "the baseline round backfilled the aliyun instance")

	// ---- Step 2: 空闲期手动触发 -> 一次性 200 摘要 ----
	manual := h.MustSync()
	assert.Equal(t, "completed", manual.Status)
	assert.Equal(t, 1, manual.Skipped, "the converged instance skips on the manual round")
	assert.Empty(t, manual.SessionID, "zero delta -> no session handle")

	// ---- Steps 3-4: 重叠证书的幂等消化 ( duplicate absorption ) ----
	sharedFP := seedSameFingerprintOnBothClouds(t, h, "www.smoke3-same.com")
	require.NotEqual(t, fpA, sharedFP)
	absorbed := h.MustSync()
	assert.Equal(t, "completed", absorbed.Status)
	assert.Equal(t, 2, absorbed.ImportSucceeded, "both entries settle as success")
	assert.Equal(t, 0, absorbed.ImportFailed)
	assert.Len(t, h.Ledger(), 2, "the shared fingerprint ledgered exactly once ( plus the baseline cert )")
	assert.Len(t, h.ActiveMappings(), 3, "one mapping per cloud account triple")

	// ---- Error path: running 中手动触发 -> 即时 409 ----
	fpC := seedMaterial(t, h, domain.CloudAliyun, "cert-a3", "www.smoke3-c.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount,
		synctest.Instance("cert-a1", fpA), synctest.Instance("cert-a3", fpC))
	release, done := holdRoundInFlight(t, h)
	conflict := h.PostSync()
	assert.Equal(t, http.StatusConflict, conflict.StatusCode)
	require.NotNil(t, conflict.Env.Error)
	assert.Equal(t, "CERT_SYNC_IN_PROGRESS", conflict.Env.Error.Code)
	assert.NotContains(t, conflict.Body, "sessionId", "the conflict offers no polling handle")
	release()
	final := synctest.AwaitSyncResponse(t, done)
	require.Equal(t, http.StatusOK, final.StatusCode, "the in-flight round converges")

	// ---- Step 5: 凭 sessionId 轮询至终态 ----
	var finalRun synctest.SyncRunPayload
	require.NoError(t, json.Unmarshal(final.Env.Data, &finalRun), "the converged round returns its summary")
	require.NotEmpty(t, finalRun.SessionID, "the round imported a new instance -> polling handle present")
	sess := h.MustImportSession(finalRun.SessionID)
	assert.Equal(t, "completed", sess.Status, "the poll face reaches the terminal state")
	assert.Equal(t, 1, sess.Progress.Succeeded, "the in-flight round's result is not lost")
	synctest.RequireNoKeyMaterial(t, h.PostSync().Body)
}
