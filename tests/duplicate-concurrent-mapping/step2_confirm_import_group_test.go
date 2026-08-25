// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/duplicate-concurrent-mapping/contracts/step-2-confirm-import-group.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package duplicate_concurrent_mapping

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep2_ConfirmImportGroup_Success verifies the "success" outcome:
// the two-account same-certificate group is accepted as one session.
func TestStep2_ConfirmImportGroup_Success(t *testing.T) {
	h, _, _, _ := multiAccountWorld(t)

	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-acc-a"),
		discoverytest.ImportItem("aliyun", "acct-b", "cert-acc-b"),
	))
	resp.RequireJSON(t)
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	data := resp.DataMap(t)
	sessionID, _ := data["sessionId"].(string)
	require.NotEmpty(t, sessionID)
	assert.Equal(t, "running", data["status"])
	items := data["items"].([]any)
	require.Len(t, items, 2)
	progress := data["progress"].(map[string]any)
	assert.Equal(t, float64(2), progress["total"])
}

// TestStep2_ConfirmImportGroup_ValidationErrorMissingTriple verifies the
// "validation-error-missing-triple" outcome.
func TestStep2_ConfirmImportGroup_ValidationErrorMissingTriple(t *testing.T) {
	h := discoverytest.NewHarness(t, nil)
	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem(" ", "acct-a", "cert-acc-a"),
	))
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, resp.Body)
	require.NotNil(t, resp.Env.Error)
	assert.Equal(t, "INVALID_REQUEST", resp.Env.Error.Code)
	assert.Equal(t, int32(0), h.SessionsCreated())
}

// TestStep2_ConfirmImportGroup_Unauthorized verifies the "unauthorized"
// outcome.
func TestStep2_ConfirmImportGroup_Unauthorized(t *testing.T) {
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) { c.Claims = nil })
	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-acc-a"),
	))
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.Equal(t, int32(0), h.SessionsCreated())
}
