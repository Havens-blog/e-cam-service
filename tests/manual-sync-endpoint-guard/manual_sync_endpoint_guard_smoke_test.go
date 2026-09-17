// @feature cert-volcano-import-sync @api-functional
//
// Journey smoke: manual-sync-endpoint-guard end-to-end plus error paths.
// Sequence: OpsEngineer trigger -> one-shot 200 summary -> sessionId
// continues into the existing progress endpoint -> repeat trigger after new
// cloud-side instances -> error paths: non-engineer 403, unauthenticated 401,
// unwired dependency 500.
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

func TestManualSyncEndpointGuard_FullJourneySmoke(t *testing.T) {
	h := newJourneyHarness(t)
	fpA := seedMaterial(t, h, domain.CloudAliyun, "cert-a1", "www.smoke5-a1.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", fpA))

	// ---- Step 1: OpsEngineer 手动触发 -> 一次性 200 摘要 ----
	first := h.MustSync()
	assert.Equal(t, "completed", first.Status)
	assert.Equal(t, 1, first.ImportSucceeded)
	assert.Equal(t, "manual", mustSession(t, h, first.SessionID).Operator)

	// ---- Step 2: 凭 sessionId 轮询进度至终态 ----
	sess := h.MustImportSession(first.SessionID)
	assert.Equal(t, "completed", sess.Status)
	assert.NotNil(t, sess.FinishedAt)

	// ---- Error paths: 角色边界 ----
	viewer := newHarnessAs(t, map[string]string{"cert_role": "viewer", "username": "v"}, true)
	assert.Equal(t, http.StatusForbidden, viewer.PostSync().StatusCode)
	anon := newHarnessAs(t, nil, true)
	assert.Equal(t, http.StatusUnauthorized, anon.PostSync().StatusCode)

	// ---- Step 4: 空闲期重复触发（含新增实例） -> 幂等收敛 ----
	fpA2 := seedMaterial(t, h, domain.CloudAliyun, "cert-a2", "www.smoke5-a2.com")
	fpV := seedMaterial(t, h, synctest.CloudVolcano, "cert-v1", "www.smoke5-v1.com")
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount,
		synctest.Instance("cert-a1", fpA), synctest.Instance("cert-a2", fpA2))
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", fpV))
	second := h.MustSync()
	assert.Equal(t, "completed", second.Status)
	assert.Equal(t, 2, second.Imported, "the new instances import")
	assert.Equal(t, 1, second.Skipped, "the imported instance skips")
	assert.Len(t, h.Ledger(), 3, "no duplicate rows across rounds")
	assert.Len(t, h.ActiveMappings(), 3, "no duplicate mappings across rounds")
	// Cross-entity: every mapping binds its ledger certificate.
	for _, m := range h.ActiveMappings() {
		_, err := h.LedgerByFP(m.CertFingerprint)
		assert.NoError(t, err, "mapping fingerprint %s resolves in the ledger", m.CertFingerprint)
	}

	// ---- Error path: 未装配 sync 依赖 -> 结构化 500 ----
	unwired := newHarnessAs(t, map[string]string{
		"cert_role": synctest.RoleOpsEngineerClaim, "username": "ops-engineer",
	}, false)
	unwiredResp := unwired.PostSync()
	assert.Equal(t, http.StatusInternalServerError, unwiredResp.StatusCode)
	require.NotNil(t, unwiredResp.Env.Error)
	assert.Equal(t, "INTERNAL_ERROR", unwiredResp.Env.Error.Code)
	synctest.RequireNoKeyMaterial(t, unwiredResp.Body)
}
