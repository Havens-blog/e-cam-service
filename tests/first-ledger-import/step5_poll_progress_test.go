// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/first-ledger-import/contracts/step-5-poll-progress.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package first_ledger_import

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TestStep5_PollProgress_Success verifies the "success" outcome: the
// progress endpoint returns the terminal session isomorphically ( status,
// finishedAt, consistent progress counts, visible static failure reasons ).
func TestStep5_PollProgress_Success(t *testing.T) {
	h, aliyun := aliyunWorld(t)
	b := certtest.NewBundle(t, "www.step5-ok.com", []string{"www.step5-ok.com"}, nil)
	aliyun.AddMaterial("cert-ok", materialFor(b))
	// cert-gone stays unconfigured -> Exists=false failure item.

	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-ok"),
		discoverytest.ImportItem("aliyun", "acct-a", "cert-gone"),
	))
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	sessionID, _ := resp.DataMap(t)["sessionId"].(string)

	final := h.PollImportTerminal(sessionID)
	assert.Equal(t, "partial_failed", final["status"])
	assert.NotNil(t, final["finishedAt"], "terminal sessions expose finishedAt")
	assert.Equal(t, sessionID, final["sessionId"])

	progress := final["progress"].(map[string]any)
	assert.Equal(t, float64(2), progress["total"])
	assert.Equal(t, float64(1), progress["succeeded"])
	assert.Equal(t, float64(1), progress["failed"])

	items := final["items"].([]any)
	require.Len(t, items, 2)
	ok := items[0].(map[string]any)
	assert.Equal(t, "success", ok["result"])
	assert.NotEmpty(t, ok["mappedCertId"])
	gone := items[1].(map[string]any)
	assert.Equal(t, "failed", gone["result"])
	assert.Equal(t, "CERT_GET_FAILED: 云侧已不存在", gone["errorReason"], "static per-item reason is visible")
	discoverytest.RequireNoKeyMaterial(t, h.Get(discoverytest.RouteImportSession+sessionID).Body)
}

// TestStep5_PollProgress_BrowserReopenResume verifies the
// "browser-reopen-resume" outcome: the session is persisted before async
// execution, so progress can be resumed after a browser close/reopen with
// no result loss.
func TestStep5_PollProgress_BrowserReopenResume(t *testing.T) {
	h, aliyun := aliyunWorld(t)
	b1 := certtest.NewBundle(t, "www.step5-r1.com", []string{"www.step5-r1.com"}, nil)
	b2 := certtest.NewBundle(t, "www.step5-r2.com", []string{"www.step5-r2.com"}, nil)
	aliyun.AddMaterial("cert-1", materialFor(b1))
	aliyun.AddMaterial("cert-2", materialFor(b2))

	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-1"),
		discoverytest.ImportItem("aliyun", "acct-a", "cert-2"),
	))
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	sessionID, _ := resp.DataMap(t)["sessionId"].(string)

	// "Browser closed" mid-flight: the session itself keeps running server
	// side; on reopen the persisted session is immediately readable.
	onReopen := h.Get(discoverytest.RouteImportSession + sessionID)
	require.Equal(t, http.StatusOK, onReopen.StatusCode, onReopen.Body)
	reopened := onReopen.DataMap(t)
	assert.Equal(t, sessionID, reopened["sessionId"])

	final := h.PollImportTerminal(sessionID)
	assert.Equal(t, "completed", final["status"])

	// A later reopen returns the identical persisted results ( nothing lost ).
	after := h.Get(discoverytest.RouteImportSession + sessionID).DataMap(t)
	assert.Equal(t, final["status"], after["status"])
	assert.Equal(t, final["progress"], after["progress"])
	finalItems, reopenItems := final["items"].([]any), after["items"].([]any)
	require.Len(t, reopenItems, len(finalItems))
	for i := range finalItems {
		assert.Equal(t, finalItems[i].(map[string]any)["result"], reopenItems[i].(map[string]any)["result"])
	}
}

// TestStep5_PollProgress_SessionTimeoutRetryable covers the
// "session-timeout-retryable" outcome.
func TestStep5_PollProgress_SessionTimeoutRetryable(t *testing.T) {
	// SKIP_REASON: the session-wide budget is the unexported 10-minute
	// batchProcessTimeout constant ( fact IMPORT_TIMEOUT_SEMANTICS,
	// discovery_import_service.go:24-31 ) and cannot elapse inside an
	// API-functional test. The semantics ( remaining items get the
	// SESSION_TIMEOUT retryable reason, session still converges to a terminal
	// state, processed results are preserved ) are covered by the service
	// layer unit tests; the retryable static reason is documented in the
	// fact table.
	t.Skip("10-minute session budget cannot elapse in API-functional scope")
}

// TestStep5_PollProgress_SessionNotFound verifies the "session-not-found"
// outcome: an unknown sessionId yields the structured 404 envelope.
func TestStep5_PollProgress_SessionNotFound(t *testing.T) {
	h := discoverytest.NewHarness(t, nil)
	resp := h.Get(discoverytest.RouteImportSession + primitive.NewObjectID().Hex())
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, resp.Body)
	assert.False(t, resp.Env.Success)
	require.NotNil(t, resp.Env.Error)
	assert.Equal(t, "CERT_NOT_FOUND", resp.Env.Error.Code)
	assert.NotEmpty(t, resp.Env.Error.Message)
}

// TestStep5_PollProgress_Unauthorized verifies the "unauthorized" outcome:
// no credentials -> 401, no session data.
func TestStep5_PollProgress_Unauthorized(t *testing.T) {
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) { c.Claims = nil })
	resp := h.Get(discoverytest.RouteImportSession + primitive.NewObjectID().Hex())
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.False(t, resp.Env.Success)
	assert.Empty(t, resp.Env.Data)
}
