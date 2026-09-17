// @feature cert-volcano-import-sync @api-functional
//
// Contract step-4-repeat-trigger ( manual-sync-endpoint-guard ): repeated
// manual triggers in idle state converge idempotently, and the unwired sync
// dependency degrades to a structured 500. Outcomes:
// success-repeat-with-new-instances / repeat-zero-delta-converged /
// service-not-wired-500.
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

// TestManualSyncEndpointGuard_Step4_RepeatWithNewInstances verifies Outcome
// "success-repeat-with-new-instances": after a converged round a repeat
// trigger with new cloud-side instances is accepted again — the new instances
// import, previously imported ones skip, and no duplicate rows appear.
func TestManualSyncEndpointGuard_Step4_RepeatWithNewInstances(t *testing.T) {
	h := newJourneyHarness(t)
	fpA := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.msg4-repeat-a.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fpA))

	first := h.MustSync()
	require.Equal(t, "completed", first.Status)
	require.Equal(t, 1, first.ImportSucceeded)

	// New ( unledgered ) instances appear on both clouds before the repeat.
	fpA2 := seedMaterial(t, h, domain.CloudAliyun, "cert-a2", "www.msg4-repeat-a2.com")
	fpV := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.msg4-repeat-v.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount,
		synctest.Instance("cert-a1", fpA), synctest.Instance("cert-a2", fpA2))
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", fpV))

	second := h.MustSync()
	require.Equal(t, "completed", second.Status, "the repeat trigger is accepted again after terminal release")
	require.Equal(t, 2, second.Imported, "the new instances import")
	require.Equal(t, 1, second.Skipped, "the previously imported instance is not re-processed")
	require.Len(t, h.Ledger(), 3, "no duplicate ledger rows across rounds")
	require.Len(t, h.ActiveMappings(), 3, "no duplicate mapping rows")
	require.Equal(t, int32(2), h.Sessions.Created(), "each importing round owns its session; CAS cycled cleanly")

	// Cross-entity: the repeat round's entries bind their new ledger certs.
	secondSess := mustSession(t, h, second.SessionID)
	bound := 0
	for _, item := range secondSess.Items {
		cert, err := h.LedgerByFP(mapFingerprint(t, h, item.Cloud, item.AccountKey, item.CloudCertID))
		require.NoError(t, err, "each entry's mapping resolves to a ledger certificate")
		if item.MappedCertID == cert.ID.Hex() {
			bound++
		}
	}
	require.Equal(t, 2, bound, "both new entries bind their own ledger certificates")
}

// mapFingerprint resolves the mapping fingerprint behind a session entry.
func mapFingerprint(t *testing.T, h *synctest.Harness, cloud, accountKey, cloudCertID string) string {
	t.Helper()
	m, err := h.LatestMappingByCloudCert(cloud, accountKey, cloudCertID)
	require.NoError(t, err)
	return m.CertFingerprint
}

// TestManualSyncEndpointGuard_Step4_RepeatZeroDeltaConverged verifies Outcome
// "repeat-zero-delta-converged": a repeat trigger with zero delta returns 200
// with imported 0, no sessionId, and zero ledger/mapping writes — an idempotent
// no-op, not an error.
func TestManualSyncEndpointGuard_Step4_RepeatZeroDeltaConverged(t *testing.T) {
	h := newJourneyHarness(t)
	fp := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.msg4-zero.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fp))

	first := h.MustSync()
	require.Equal(t, 1, first.ImportSucceeded)
	ledgerBefore, mappingsBefore := len(h.Ledger()), len(h.ActiveMappings())
	getsAfterFirst := h.MaterialGets()

	second := h.MustSync()
	require.Equal(t, "completed", second.Status, "zero delta is business-as-usual ( 200, not an error )")
	require.Equal(t, 0, second.Imported)
	require.Empty(t, second.SessionID, "no import session created")
	require.Equal(t, ledgerBefore, len(h.Ledger()), "zero ledger writes")
	require.Equal(t, mappingsBefore, len(h.ActiveMappings()), "zero mapping writes")
	require.Equal(t, getsAfterFirst, h.MaterialGets(),
		"zero material-channel Gets on the converged repeat ( delta vs the first round )")
}

// TestManualSyncEndpointGuard_Step4_ServiceNotWired500 verifies Outcome
// "service-not-wired-500": with the handler constructed without the sync
// dependency the trigger answers a structured 500 INTERNAL_ERROR with the
// fixed not-wired text — no panic, no stack leakage, no state change.
func TestManualSyncEndpointGuard_Step4_ServiceNotWired500(t *testing.T) {
	// Authenticated caller ( wireSync=false: optional variadic zero form ).
	h := newHarnessAs(t, map[string]string{
		"cert_role": synctest.RoleOpsEngineerClaim, "username": "ops-engineer",
	}, false)

	resp := h.PostSync()
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode, resp.Body)
	require.NotNil(t, resp.Env.Error)
	assert.Equal(t, "INTERNAL_ERROR", resp.Env.Error.Code)
	assert.Equal(t, "cert sync service not wired", resp.Env.Error.Message, "fixed defensive text")
	assert.NotContains(t, resp.Body, "goroutine", "no stack leakage")
	assert.NotContains(t, resp.Body, "panic", "the handler did not panic")
	assert.Equal(t, int32(0), h.Sessions.Created(), "no sync started")
	assert.Empty(t, h.Ledger(), "no state change")
}
