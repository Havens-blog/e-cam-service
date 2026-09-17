// @feature cert-volcano-import-sync @api-functional
//
// Contract step-2-manual-trigger ( manual-scheduler-race ): the manual face
// answers a one-shot 200 summary when idle, an immediate structured 409 while
// a round runs, and a 401 for unauthenticated callers. Outcomes: success /
// conflict-409 / unauthorized.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package manual_scheduler_race

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/require"
)

// TestManualSchedulerRace_Step2_ManualTriggerSuccess verifies Outcome
// "success": the manual trigger is accepted with the one-shot 200 terminal
// summary ( full field shape ) and the created session attributes
// operator=manual.
func TestManualSchedulerRace_Step2_ManualTriggerSuccess(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.race2-manual.com")
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", fp))

	run := h.MustSync()
	require.Equal(t, "completed", run.Status)
	require.NotEmpty(t, run.SessionID, "the round produced an import session")
	require.NotEmpty(t, run.StartedAt, "the summary carries the round timeline")
	require.NotEmpty(t, run.FinishedAt)
	started, err := time.Parse(time.RFC3339, run.StartedAt)
	require.NoError(t, err, "startedAt is RFC3339")
	finished, err := time.Parse(time.RFC3339, run.FinishedAt)
	require.NoError(t, err, "finishedAt is RFC3339")
	require.False(t, finished.Before(started), "the round converges within its own timeline")
	require.Equal(t, 1, run.Imported)
	require.Equal(t, 1, run.ImportSucceeded)
	require.Empty(t, run.Failures)

	sess := mustSession(t, h, run.SessionID)
	require.Equal(t, "manual", sess.Operator, "the round is attributed to the manual source")
}

// TestManualSchedulerRace_Step2_Conflict409 verifies Outcome "conflict-409":
// while a round is in flight the manual trigger answers an immediate
// structured 409 CERT_SYNC_IN_PROGRESS — no queueing, no new sessionId, no
// cloud error detail — and the in-flight session is unaffected.
func TestManualSchedulerRace_Step2_Conflict409(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.race2-conflict.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))

	release, done := holdRoundInFlight(t, h)

	resp := h.PostSync()
	require.Equal(t, http.StatusConflict, resp.StatusCode, "the conflict is immediate ( non-500, no queueing )")
	resp.RequireJSON(t)
	require.NotNil(t, resp.Env.Error)
	require.Equal(t, "CERT_SYNC_IN_PROGRESS", resp.Env.Error.Code)
	require.NotEmpty(t, resp.Env.Error.Message)
	require.Nil(t, resp.DataMap(t), "the conflict path carries no data payload")
	require.NotContains(t, resp.Body, "sessionId", "the conflict path offers no polling handle")
	require.NotContains(t, resp.Body, "cloud unreachable", "no cloud-side error detail in the conflict body")
	require.Equal(t, int32(0), h.Sessions.Created(), "no new session document for the rejected trigger")

	release()
	final := synctest.AwaitSyncResponse(t, done)
	require.Equal(t, http.StatusOK, final.StatusCode, "the in-flight round converges untouched")
	require.Equal(t, int32(1), h.Sessions.Created(), "only the in-flight round's session exists")
}

// TestManualSchedulerRace_Step2_Unauthorized verifies Outcome "unauthorized":
// an unauthenticated request is rejected 401 before any business logic — no
// session, no state change, no sensitive leakage.
func TestManualSchedulerRace_Step2_Unauthorized(t *testing.T) {
	h := synctest.NewHarness(t, func(c *synctest.Config) {
		c.Claims = nil // unauthenticated caller
		c.Listers = []domain.Cloud{domain.CloudAliyun}
	})
	h.SeedAccount(domain.CloudAliyun, AliyunAccount)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.race2-401.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))

	resp := h.PostSync()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "authentication precedes role judgement")
	resp.RequireJSON(t)
	require.False(t, resp.Env.Success)
	require.NotNil(t, resp.Env.Error)
	require.Equal(t, "UNAUTHORIZED", resp.Env.Error.Code)
	require.NotContains(t, strings.ToLower(resp.Body), "credential", "no sensitive information in the rejection")
	require.Equal(t, int32(0), h.Sessions.Created(), "no state change for unauthenticated requests")
	require.Empty(t, h.Ledger(), "no import ran")
}
