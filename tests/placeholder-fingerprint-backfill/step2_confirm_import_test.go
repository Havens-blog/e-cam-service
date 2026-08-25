// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/placeholder-fingerprint-backfill/contracts/step-2-confirm-import.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package placeholder_fingerprint_backfill

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep2_ConfirmImport_Success verifies the "success" outcome: the
// placeholder entry enters the processing queue like any other item.
func TestStep2_ConfirmImport_Success(t *testing.T) {
	h, _, snapID := tencentWorld(t, "acct-tx")
	h.SeedRefs(placeholderRefSpec("tencent", "acct-tx", "ssl-9", "res-1", snapID))

	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("tencent", "acct-tx", "ssl-9"),
	))
	resp.RequireJSON(t)
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	data := resp.DataMap(t)
	sessionID, _ := data["sessionId"].(string)
	require.NotEmpty(t, sessionID)
	assert.Equal(t, "running", data["status"])
	items := data["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, "ssl-9", items[0].(map[string]any)["cloudCertId"])
}

// TestStep2_ConfirmImport_ValidationErrorEmptyItems verifies the
// "validation-error-empty-items" outcome.
func TestStep2_ConfirmImport_ValidationErrorEmptyItems(t *testing.T) {
	h := discoverytest.NewHarness(t, nil)
	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody())
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, resp.Body)
	require.NotNil(t, resp.Env.Error)
	assert.Equal(t, "INVALID_REQUEST", resp.Env.Error.Code)
	assert.Equal(t, "items is required", resp.Env.Error.Message)
	assert.Equal(t, int32(0), h.SessionsCreated())
}

// TestStep2_ConfirmImport_Unauthorized verifies the "unauthorized" outcome.
func TestStep2_ConfirmImport_Unauthorized(t *testing.T) {
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) { c.Claims = nil })
	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("tencent", "acct-tx", "ssl-9"),
	))
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.Equal(t, int32(0), h.SessionsCreated())
}
