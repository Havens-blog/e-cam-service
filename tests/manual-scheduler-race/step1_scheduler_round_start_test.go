// @feature cert-volcano-import-sync @api-functional
//
// Contract step-1-scheduler-round-start ( manual-scheduler-race ): the
// scheduler round starts normally, yields silently while a round is in
// flight, and the CAS releases after any terminal state. Outcomes: success /
// already-running-cas-skip / cas-release-after-terminal.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package manual_scheduler_race

import (
	"context"
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/require"
)

// TestManualSchedulerRace_Step1_SchedulerRoundStarts verifies Outcome
// "success": the guarded cert:cert-import entry acquires the CAS, starts the
// round, and the created session attributes operator=scheduler.
func TestManualSchedulerRace_Step1_SchedulerRoundStarts(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.race1-start.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))

	require.NoError(t, synctest.CertImportJobRun(h.Sync)(context.Background()),
		"the scheduler job entry starts a clean round")

	require.Equal(t, int32(1), h.Sessions.Created())
	sess := mustSession(t, h, h.Sessions.LastID())
	require.Equal(t, "scheduler", sess.Operator, "the round is attributed to the scheduler source")
	require.Equal(t, domain.DiscoveryImportCompleted, sess.Status)
	require.Len(t, h.ActiveMappings(), 1, "the round's import established its mapping")
	require.Equal(t, 1, h.Lister(domain.CloudAliyun).Lists(), "cloud-side access stayed read-only List calls")
}

// TestManualSchedulerRace_Step1_CASAlreadyRunningSkips verifies Outcome
// "already-running-cas-skip": with a round in flight the service returns
// ErrSyncRunning and the scheduler job yields silently ( no restart, no
// error, no second session ); the in-flight round is unaffected.
func TestManualSchedulerRace_Step1_CASAlreadyRunningSkips(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.race1-cas.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))

	release, done := holdRoundInFlight(t, h)

	_, err := h.Sync.SyncCertificates(context.Background())
	require.ErrorIs(t, err, service.ErrSyncRunning, "second scheduler trigger is rejected by the CAS")

	jobRun := synctest.CertImportJobRun(h.Sync)
	require.NoError(t, jobRun(context.Background()), "the scheduler job yields silently while CAS is held")
	require.Equal(t, int32(0), h.Sessions.Created(), "no second session while the round is in flight")

	// Release the gate and WAIT for the in-flight round to converge before
	// asserting its post-state ( the round runs on another goroutine ).
	release()
	final := synctest.AwaitSyncResponse(t, done)
	require.Equal(t, http.StatusOK, final.StatusCode, "the in-flight round converges untouched")
	require.Equal(t, int32(1), h.Sessions.Created(), "the in-flight round keeps its own single session")
}

// TestManualSchedulerRace_Step1_CASReleasesAfterTerminal verifies Outcome
// "cas-release-after-terminal": after a round converged to its terminal state
// a new round starts normally — no deadlock, no residual CAS placeholder.
func TestManualSchedulerRace_Step1_CASReleasesAfterTerminal(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.race1-release.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))

	first := h.MustSync()
	require.Equal(t, "completed", first.Status, "the first round reached its terminal state")

	second := h.MustSync()
	require.Equal(t, "completed", second.Status, "the next round starts normally after terminal release")
	require.Equal(t, 1, second.Skipped, "the converged instance re-judges as a skip ( CAS not stuck )")
	require.Equal(t, int32(1), h.Sessions.Created(),
		"the second round is zero-delta ( no import session ); the point is it RUNS at all — no residual CAS placeholder")
}
