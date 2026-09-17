// @feature cert-volcano-import-sync @api-functional
//
// Contract step-2-poll-via-sessionid ( manual-sync-endpoint-guard ): the
// summary sessionId continues into the existing progress endpoint; the
// conflict path carries no polling handle; a zero-delta round has no session
// at all. Outcomes: success / conflict-no-poll-handle /
// empty-sessionid-no-import-session.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package manual_sync_endpoint_guard

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestManualSyncEndpointGuard_Step2_PollViaSessionID verifies Outcome
// "success": a non-empty summary sessionId can be used on the existing
// progress endpoint through to the terminal state — results are not lost.
func TestManualSyncEndpointGuard_Step2_PollViaSessionID(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.msg2-poll.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))

	run := h.MustSync()
	require.NotEmpty(t, run.SessionID, "the round produced an import session")

	sess := h.MustImportSession(run.SessionID)
	assert.Equal(t, run.SessionID, sess.SessionID)
	assert.Equal(t, "completed", sess.Status, "the session is terminal via the poll face")
	assert.NotNil(t, sess.FinishedAt, "terminal decidable via status/finishedAt")
	assert.Equal(t, 1, sess.Progress.Total)
	assert.Equal(t, 1, sess.Progress.Succeeded)
	assert.Len(t, sess.Items, 1, "per-entry results survive into the poll face")

	// Cross-entity: the polled entry binds the ledger certificate.
	cert, err := h.LedgerByFP(fp)
	require.NoError(t, err)
	assert.Equal(t, cert.ID.Hex(), sess.Items[0].MappedCertID)
}

// TestManualSyncEndpointGuard_Step2_ConflictNoPollHandle verifies Outcome
// "conflict-no-poll-handle": the 409 path produces no new sessionId and does
// not guide polling — while the in-flight session's own sessionId remains
// queryable through the same endpoint.
func TestManualSyncEndpointGuard_Step2_ConflictNoPollHandle(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.msg2-conflict.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))

	// Obtain the in-flight round's sessionId via its response after release —
	// but first capture the conflict response while it runs.
	release, done := holdRoundInFlight(t, h)
	conflict := h.PostSync()
	require.Equal(t, http.StatusConflict, conflict.StatusCode)
	assert.NotContains(t, conflict.Body, "sessionId", "the conflict response carries no polling handle")
	assert.Nil(t, conflict.DataMap(t), "no data payload at all")
	assert.Equal(t, int32(0), h.Sessions.Created(), "no new session document for the rejected trigger")

	release()
	final := synctest.AwaitSyncResponse(t, done)
	require.Equal(t, http.StatusOK, final.StatusCode)
	// The in-flight round's OWN sessionId continues to be pollable.
	run := synctest.MustDecodeSyncRun(t, final)
	require.NotEmpty(t, run.SessionID)
	sess := h.MustImportSession(run.SessionID)
	assert.Equal(t, "completed", sess.Status, "the in-flight session's progress stays queryable to terminal")
	assert.Equal(t, string(domain.DiscoveryItemSuccess), sess.Items[0].Result,
		"the in-flight session's entry converged, unperturbed by the rejected trigger")
}

// TestManualSyncEndpointGuard_Step2_EmptySessionIDNoImportSession verifies
// Outcome "empty-sessionid-no-import-session": a zero-delta round returns the
// full 200 summary without a sessionId ( no import session was created ) and
// no session document exists.
func TestManualSyncEndpointGuard_Step2_EmptySessionIDNoImportSession(t *testing.T) {
	h := newJourneyHarness(t)
	fp := synctest.FP("msg2-zero-delta")
	h.SeedLedgerCert(fp)
	h.SeedMapping(fp, "aliyun", AliyunAccount, "cert-a1")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))

	resp := h.PostSync()
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	data := resp.DataMap(t)
	require.NotNil(t, data, "the summary is still a complete 200 payload")
	assert.Equal(t, "completed", data["status"])
	_, hasSession := data["sessionId"]
	assert.False(t, hasSession, "sessionId is omitted ( no import session this round )")
	assert.NotContains(t, resp.Body, "sessionId", "no polling handle on the wire")
	assert.Equal(t, int32(0), h.Sessions.Created(), "no DiscoveryImportSession document created")
	assert.Equal(t, float64(1), data["skipped"], "counters are still complete")
}
