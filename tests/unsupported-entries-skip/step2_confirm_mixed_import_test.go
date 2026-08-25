// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/unsupported-entries-skip/contracts/step-2-confirm-mixed-import.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package unsupported_entries_skip

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep2_ConfirmMixedImport_Success verifies the "success" outcome: a
// mixed list ( parseable + forced unparseable entries ) is accepted as a
// session covering every submitted item.
func TestStep2_ConfirmMixedImport_Success(t *testing.T) {
	h, _, _, _ := mixedWorld(t)

	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-A"),
		discoverytest.ImportItem("huawei", "acct-hw", "scm-1"),
		discoverytest.ImportItem("aws", "acct-aws", "iam-123"),
	))
	resp.RequireJSON(t)
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	assert.True(t, resp.Env.Success)

	data := resp.DataMap(t)
	sessionID, _ := data["sessionId"].(string)
	require.NotEmpty(t, sessionID)
	assert.Equal(t, "running", data["status"])
	items := data["items"].([]any)
	require.Len(t, items, 3, "every submitted item enters the processing queue")
	progress := data["progress"].(map[string]any)
	assert.Equal(t, float64(3), progress["total"])
}

// TestStep2_ConfirmMixedImport_ValidationErrorMissingTriple verifies the
// "validation-error-missing-triple" outcome.
func TestStep2_ConfirmMixedImport_ValidationErrorMissingTriple(t *testing.T) {
	h := discoverytest.NewHarness(t, nil)
	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("huawei", "acct-hw", " "),
	))
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, resp.Body)
	require.NotNil(t, resp.Env.Error)
	assert.Equal(t, "INVALID_REQUEST", resp.Env.Error.Code)
	assert.Equal(t, int32(0), h.SessionsCreated())
}

// TestStep2_ConfirmMixedImport_Unauthorized verifies the "unauthorized"
// outcome.
func TestStep2_ConfirmMixedImport_Unauthorized(t *testing.T) {
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) { c.Claims = nil })
	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("huawei", "acct-hw", "scm-1"),
	))
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.Equal(t, int32(0), h.SessionsCreated())
}
