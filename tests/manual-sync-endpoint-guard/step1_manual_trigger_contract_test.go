// @feature cert-volcano-import-sync @api-functional
//
// Contract step-1-manual-trigger-contract ( manual-sync-endpoint-guard ): the
// endpoint contract of POST /discovery/sync — OpsEngineer 200 one-shot
// summary, running conflict 409, and the 403/401 role/authentication
// boundaries. Outcomes: success-200-summary / conflict-409-running /
// forbidden-non-opsengineer / unauthorized / forbidden-no-role-signal.
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

// TestManualSyncEndpointGuard_Step1_Success200Summary verifies Outcome
// "success-200-summary": an OpsEngineer trigger is accepted with the one-shot
// 200 terminal summary carrying the full field shape, and the created session
// attributes operator=manual ( synchronous face: HTTP return = terminal ).
func TestManualSyncEndpointGuard_Step1_Success200Summary(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.msg1-200.com")
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", fp))

	resp := h.PostSync()
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	resp.RequireJSON(t)

	data := resp.DataMap(t)
	require.NotNil(t, data)
	// Full field shape ( fact CERT_SYNC_RUN_VO_FIELDS ).
	for _, key := range []string{
		"sessionId", "status", "startedAt", "finishedAt", "cloudsScanned",
		"accountsScanned", "listed", "skipped", "backfilled", "drifted",
		"imported", "importSucceeded", "importFailed", "failures",
	} {
		assert.Contains(t, data, key, "summary must expose field %q", key)
	}
	assert.Equal(t, "completed", data["status"], "HTTP return = terminal state ( no polling needed )")
	assert.NotEmpty(t, data["sessionId"], "the round produced an import session")
	assert.Equal(t, float64(1), data["imported"])
	assert.Equal(t, float64(1), data["importSucceeded"])
	assert.Equal(t, float64(0), data["importFailed"])
	assert.Len(t, data["failures"], 0)

	sess := mustSession(t, h, h.Sessions.LastID())
	assert.Equal(t, "manual", sess.Operator, "the round is attributed to the manual source")
	synctest.RequireNoKeyMaterial(t, resp.Body)

	// Cross-entity binding behind the summary: the imported instance's mapping
	// points at the ledger certificate the session entry binds.
	cert, err := h.LedgerByFP(fp)
	require.NoError(t, err, "the imported fingerprint is ledgered")
	mapping, err := h.LatestMappingByCloudCert("volcano", VolcanoAccount, "cert-v1")
	require.NoError(t, err)
	assert.Equal(t, cert.Fingerprint, mapping.CertFingerprint, "mapping binds the ledger certificate")
	assert.Equal(t, cert.ID.Hex(), sess.Items[0].MappedCertID, "session entry binds the same certificate")
}

// TestManualSyncEndpointGuard_Step1_Conflict409Running verifies Outcome
// "conflict-409-running": with a round already running the trigger answers an
// immediate structured 409 CERT_SYNC_IN_PROGRESS ( not 500, no queueing );
// the in-flight session is unaffected.
func TestManualSyncEndpointGuard_Step1_Conflict409Running(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.msg1-409.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))

	release, done := holdRoundInFlight(t, h)

	resp := h.PostSync()
	require.Equal(t, http.StatusConflict, resp.StatusCode, "immediate conflict, not 500 and not queued")
	require.NotNil(t, resp.Env.Error)
	assert.Equal(t, "CERT_SYNC_IN_PROGRESS", resp.Env.Error.Code)
	assert.NotEmpty(t, resp.Env.Error.Message)
	assert.Nil(t, resp.DataMap(t), "no data payload on the conflict path")
	require.Equal(t, int32(0), h.Sessions.Created(), "no new session document")

	release()
	final := synctest.AwaitSyncResponse(t, done)
	require.Equal(t, http.StatusOK, final.StatusCode, "the in-flight round converges untouched")
	require.Equal(t, int32(1), h.Sessions.Created(), "exactly the in-flight round's session exists")

	// The in-flight round's own session converged with its entry bound.
	inFlightRun := synctest.MustDecodeSyncRun(t, final)
	inFlightSess := mustSession(t, h, inFlightRun.SessionID)
	assert.Equal(t, domain.DiscoveryItemSuccess, inFlightSess.Items[0].Result,
		"the in-flight session was not disturbed by the rejected second trigger")
	cert, err := h.LedgerByFP(fp)
	require.NoError(t, err)
	assert.Equal(t, cert.ID.Hex(), inFlightSess.Items[0].MappedCertID,
		"the in-flight entry binds its ledger certificate")
}

// TestManualSyncEndpointGuard_Step1_ForbiddenNonOpsEngineer verifies Outcome
// "forbidden-non-opsengineer": authenticated non-OpsEngineer identities are
// always 403 — named roles, unknown explicit cert_role values ( deny, no
// viewer downgrade ), and capability-code-only accounts alike.
func TestManualSyncEndpointGuard_Step1_ForbiddenNonOpsEngineer(t *testing.T) {
	cases := []struct {
		name   string
		claims map[string]string
	}{
		{"viewer", map[string]string{"cert_role": "viewer", "username": "v"}},
		{"auditor", map[string]string{"cert_role": "auditor", "username": "a"}},
		{"ops_supervisor", map[string]string{"cert_role": "ops_supervisor", "username": "s"}},
		{"unknown explicit cert_role", map[string]string{"cert_role": "cert:settings", "username": "u"}},
		{"capability code only ( cert:settings )", map[string]string{"authorized_codes": "cert:settings", "username": "c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name+" rejected 403", func(t *testing.T) {
			h := newHarnessAs(t, tc.claims, true)
			resp := h.PostSync()
			require.Equal(t, http.StatusForbidden, resp.StatusCode, resp.Body)
			require.NotNil(t, resp.Env.Error)
			assert.Equal(t, "FORBIDDEN", resp.Env.Error.Code)
			assert.False(t, resp.Env.Success)
			assert.Nil(t, resp.DataMap(t), "no business data on the 403 path")
			assert.Equal(t, int32(0), h.Sessions.Created(), "no sync started")
			assert.Empty(t, h.Ledger(), "no business side effect")
		})
	}
}

// TestManualSyncEndpointGuard_Step1_Unauthorized verifies Outcome
// "unauthorized": an unauthenticated request gets 401 before any role
// judgement, with no sensitive leakage and no state change.
func TestManualSyncEndpointGuard_Step1_Unauthorized(t *testing.T) {
	h := newHarnessAs(t, nil, true)

	resp := h.PostSync()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "authentication precedes role judgement")
	resp.RequireJSON(t)
	assert.False(t, resp.Env.Success)
	require.NotNil(t, resp.Env.Error)
	assert.Equal(t, "UNAUTHORIZED", resp.Env.Error.Code)
	assert.Equal(t, int32(0), h.Sessions.Created(), "no state change")
	assert.Empty(t, h.Ledger())
	synctest.RequireNoKeyMaterial(t, resp.Body)
}

// TestManualSyncEndpointGuard_Step1_ForbiddenNoRoleSignal verifies Outcome
// "forbidden-no-role-signal": an authenticated session declaring no cert_role
// signal at all resolves to the read-only viewer face and is denied by the
// OpsEngineer whitelist ( no valid role = deny ).
func TestManualSyncEndpointGuard_Step1_ForbiddenNoRoleSignal(t *testing.T) {
	h := newHarnessAs(t, map[string]string{"username": "plain-user"}, true)

	resp := h.PostSync()
	require.Equal(t, http.StatusForbidden, resp.StatusCode, resp.Body)
	require.NotNil(t, resp.Env.Error)
	assert.Equal(t, "FORBIDDEN", resp.Env.Error.Code)
	assert.Equal(t, int32(0), h.Sessions.Created(), "no sync started")
	assert.Empty(t, h.Ledger(), "no business side effect")
}
