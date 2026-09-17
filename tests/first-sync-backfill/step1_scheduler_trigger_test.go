// @feature cert-volcano-import-sync @api-functional
//
// Contract step-1-scheduler-trigger ( first-sync-backfill ): the daily
// scheduler reaches 01:00 and drives one sync round through the
// CertificateSyncer narrow port. Outcomes: success / already-running-cas-skip
// / nil-service-degrade.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package first_sync_backfill

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/require"
)

// TestFirstSyncBackfill_Step1_SchedulerTriggerSuccess verifies Outcome
// "success": the guarded cert:cert-import job entry starts a round ( CAS
// acquired ), the created import session carries operator=scheduler, and the
// read-only cloud discipline holds ( only List + material Gets ).
func TestFirstSyncBackfill_Step1_SchedulerTriggerSuccess(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.step1-scheduler.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))

	jobRun := synctest.CertImportJobRun(h.Sync)
	require.NoError(t, jobRun(context.Background()), "scheduler job entry returns nil on a clean round")

	// The round ran and backfilled: exactly one import session, operator=scheduler.
	require.Equal(t, int32(1), h.Sessions.Created(), "one import session for one round")
	sess, err := h.SessionByID(h.Sessions.LastID())
	require.NoError(t, err)
	require.Equal(t, "scheduler", sess.Operator, "session operator attributes the scheduler source")
	require.Equal(t, string(domain.DiscoveryImportCompleted), string(sess.Status))

	// Ledger + mapping established ( state dimension ).
	_, err = h.LedgerByFP(fp)
	require.NoError(t, err, "instance fingerprint registered in the ledger")
	require.Len(t, h.ActiveMappings(), 1)
	require.Equal(t, fp, h.ActiveMappings()[0].CertFingerprint, "mapping binds the imported fingerprint")

	// Read-only discipline: lister called, material channel used for import,
	// and the stubs expose no write path at all ( constructive guarantee ).
	require.Equal(t, 1, h.Lister(domain.CloudAliyun).Lists())
	require.Equal(t, int32(1), h.Material(domain.CloudAliyun).Calls())
}

// TestFirstSyncBackfill_Step1_CASAlreadyRunningSkips verifies Outcome
// "already-running-cas-skip": with a round in flight ( CAS held ), the
// service returns ErrSyncRunning and the scheduler job yields silently
// without creating a second round; the in-flight round is unaffected.
func TestFirstSyncBackfill_Step1_CASAlreadyRunningSkips(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.step1-cas.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))

	lister := h.Lister(domain.CloudAliyun)
	gate := make(chan struct{})
	lister.SetGate(gate)

	type roundResult = synctest.SyncRoundResult
	inFlight := make(chan roundResult, 1)
	go func() {
		run, err := h.Sync.SyncCertificates(context.Background())
		inFlight <- roundResult{Run: run, Err: err}
	}()
	synctest.WaitForSignal(t, lister.Entered(), "in-flight round to reach the lister ( CAS held )")

	// Service contract: second trigger is rejected with ErrSyncRunning.
	_, err := h.Sync.SyncCertificates(context.Background())
	require.ErrorIs(t, err, service.ErrSyncRunning)

	// Scheduler job yields silently ( no error, no restart ).
	jobRun := synctest.CertImportJobRun(h.Sync)
	require.NoError(t, jobRun(context.Background()), "scheduler yields silently while a round is in flight")

	require.Equal(t, int32(0), h.Sessions.Created(), "no second session while CAS is held")

	// Release the gate: the in-flight round converges untouched.
	close(gate)
	res := synctest.AwaitSyncRun(t, inFlight)
	require.NoError(t, res.Err)
	require.Equal(t, int32(1), h.Sessions.Created(), "in-flight round keeps its own single session")
	require.Equal(t, "scheduler", mustSession(t, h, res.Run.SessionID).Operator)
}

// TestFirstSyncBackfill_Step1_NilSyncServiceDegrades verifies Outcome
// "nil-service-degrade": with the Sync narrow port unwired the scheduler job
// degrades gracefully ( warn + skip, no panic, normal return ).
func TestFirstSyncBackfill_Step1_NilSyncServiceDegrades(t *testing.T) {
	jobRun := synctest.CertImportJobRun(nil)
	require.NotPanics(t, func() {
		require.NoError(t, jobRun(context.Background()), "unwired sync port degrades to a silent skip")
	})
}
