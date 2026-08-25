// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/first-ledger-import/contracts/step-3-confirm-import.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package first_ledger_import

import (
	"net/http"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep3_ConfirmImport_Success verifies the "success" outcome: the 202
// initial snapshot shape ( sessionId, running, pending items, progress )
// with the session persisted before async execution starts.
func TestStep3_ConfirmImport_Success(t *testing.T) {
	h, _ := aliyunWorld(t)
	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-A"),
		discoverytest.ImportItem("aliyun", "acct-a", "cert-B"),
	))
	resp.RequireJSON(t)
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	assert.True(t, resp.Env.Success)

	data := resp.DataMap(t)
	sessionID, _ := data["sessionId"].(string)
	require.NotEmpty(t, sessionID, "202 carries the session handle")
	assert.Equal(t, "running", data["status"])
	assert.NotEmpty(t, data["createdAt"])
	items := data["items"].([]any)
	require.Len(t, items, 2)
	for _, raw := range items {
		it := raw.(map[string]any)
		assert.Equal(t, "pending", it["result"], "202 initial snapshot items are pending")
		assert.NotContains(t, it, "mappedCertId")
	}
	progress := data["progress"].(map[string]any)
	assert.Equal(t, float64(2), progress["total"])
	assert.Equal(t, float64(0), progress["succeeded"])
	assert.Equal(t, float64(0), progress["failed"])
	discoverytest.RequireNoKeyMaterial(t, resp.Body)
	assert.Equal(t, int32(1), h.SessionsCreated(), "session persisted on accept")

	// Persist-before-async: the session is readable via the progress endpoint
	// immediately after the 202 ( the browser may close at any time ).
	early := h.Get(discoverytest.RouteImportSession + sessionID)
	require.Equal(t, http.StatusOK, early.StatusCode, early.Body)
	assert.Equal(t, sessionID, early.DataMap(t)["sessionId"])
}

// TestStep3_ConfirmImport_InLedgerLockedExcluded verifies the
// "in-ledger-locked-excluded" outcome: the frontend submits only
// unregistered entries; the accepted session excludes the locked triple.
func TestStep3_ConfirmImport_InLedgerLockedExcluded(t *testing.T) {
	h := discoverytest.NewHarness(t, nil)
	snapID := h.SeedDoneSnapshotAt(time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC))
	fpLocked, fpFree := discoverytest.FP("step3-locked"), discoverytest.FP("step3-free")
	h.SeedCert(fpLocked, time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC))
	h.SeedRefs(
		discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-a", CloudCertID: "cert-locked", Fingerprint: fpLocked, ResourceID: "res-1", SnapshotID: snapID},
		discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-a", CloudCertID: "cert-free", Fingerprint: fpFree, ResourceID: "res-2", SnapshotID: snapID},
	)

	preview := h.Get(discoverytest.RoutePreview)
	require.Equal(t, http.StatusOK, preview.StatusCode)
	pItems := preview.PreviewItems(t)
	assert.Equal(t, true, discoverytest.FindPreviewItem(t, pItems, "aliyun", "acct-a", "cert-locked")["inLedger"],
		"ledger-hit entry is greyed out ( inLedger )")

	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-free"),
	))
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	items := resp.DataMap(t)["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, "cert-free", items[0].(map[string]any)["cloudCertId"],
		"the locked triple never enters the import session")
}

// TestStep3_ConfirmImport_ValidationErrorEmptyItems verifies the
// "validation-error-empty-items" outcome: 400 INVALID_REQUEST with no
// session created.
func TestStep3_ConfirmImport_ValidationErrorEmptyItems(t *testing.T) {
	h := discoverytest.NewHarness(t, nil)
	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody())
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, resp.Body)
	assert.False(t, resp.Env.Success)
	require.NotNil(t, resp.Env.Error)
	assert.Equal(t, "INVALID_REQUEST", resp.Env.Error.Code)
	assert.Equal(t, "items is required", resp.Env.Error.Message)
	assert.Equal(t, int32(0), h.SessionsCreated(), "no import session is created on validation error")
}

// TestStep3_ConfirmImport_ValidationErrorMissingTriple verifies the
// "validation-error-missing-triple" outcome: an item missing a triple field
// is rejected with 400 and no session.
func TestStep3_ConfirmImport_ValidationErrorMissingTriple(t *testing.T) {
	h := discoverytest.NewHarness(t, nil)
	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", " ", "cert-A"),
	))
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, resp.Body)
	require.NotNil(t, resp.Env.Error)
	assert.Equal(t, "INVALID_REQUEST", resp.Env.Error.Code)
	assert.Equal(t, "items entry requires cloud, accountKey and cloudCertId", resp.Env.Error.Message)
	assert.Equal(t, int32(0), h.SessionsCreated())
}

// TestStep3_ConfirmImport_Unauthorized verifies the "unauthorized" outcome:
// no credentials -> 401, no session.
func TestStep3_ConfirmImport_Unauthorized(t *testing.T) {
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) { c.Claims = nil })
	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-A"),
	))
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.False(t, resp.Env.Success)
	assert.Equal(t, int32(0), h.SessionsCreated(), "unauthenticated import creates nothing")
}
