// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/permission-boundary/contracts/step-3-opsengineer-contrast.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package permission_boundary

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep3_OpsEngineerContrast_Success verifies the "success" outcome:
// switching to the OpsEngineer role reaches all four endpoints normally,
// proving the 403s above come from role judgement, not endpoint absence.
func TestStep3_OpsEngineerContrast_Success(t *testing.T) {
	h, _, _ := boundaryWorld(t, engineerClaims())

	preview := h.Get(discoverytest.RoutePreview)
	require.Equal(t, http.StatusOK, preview.StatusCode, preview.Body)
	assert.True(t, preview.Env.Success)
	assert.Equal(t, float64(1), preview.DataMap(t)["count"], "real preview data flows for the whitelisted role")

	status := h.Get(discoverytest.RouteSnapshotStatus)
	require.Equal(t, http.StatusOK, status.StatusCode, status.Body)
	assert.Equal(t, true, status.DataMap(t)["hasSnapshot"])

	imp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-A"),
	))
	require.Equal(t, http.StatusAccepted, imp.StatusCode, imp.Body)
	sessionID, _ := imp.DataMap(t)["sessionId"].(string)
	require.NotEmpty(t, sessionID)

	final := h.PollImportTerminal(sessionID)
	assert.Equal(t, "completed", final["status"], "authorized path runs the full business flow")
}

// TestStep3_OpsEngineerContrast_RejectedZeroStateResidue verifies the
// "rejected-zero-state-residue" outcome: after a 403-rejected import the
// world holds no trace of the rejected request; only the authorized
// follow-up creates a session.
func TestStep3_OpsEngineerContrast_RejectedZeroStateResidue(t *testing.T) {
	viewerHarness, aliyun, _ := boundaryWorld(t, roleClaims("viewer", "viewer-user"))

	rejected := viewerHarness.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-A"),
	))
	assert.Equal(t, http.StatusForbidden, rejected.StatusCode, rejected.Body)
	require.Equal(t, int32(0), viewerHarness.SessionsCreated(), "no residue session from the rejected request")
	require.Len(t, viewerHarness.Ledger(), 1, "ledger unchanged: only the pre-seeded fixture cert exists")
	require.Equal(t, int32(0), aliyun.Calls(), "no residue cloud calls")

	// The same world under the authorized role proceeds normally.
	engineerHarness, _, _ := boundaryWorld(t, engineerClaims())
	accepted := engineerHarness.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-A"),
	))
	require.Equal(t, http.StatusAccepted, accepted.StatusCode, accepted.Body)
	assert.Equal(t, int32(1), engineerHarness.SessionsCreated(), "authorized contrast creates exactly one session")
}
