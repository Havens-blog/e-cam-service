// @feature cert-cloud-discovery-import @api-functional
//
// Journey smoke test: permission-boundary ( per endpoint 401 -> 403 ->
// allowed walkthrough ). Source journey:
// docs/features/cert-cloud-discovery-import/testing/permission-boundary/journey.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package permission_boundary

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPermissionBoundary_Smoke walks the journey end to end: every endpoint
// rejects anonymous ( 401 ) and non-OpsEngineer ( 403 ) callers, admits
// OpsEngineer on a real flow, and the denied paths leave zero sessions
// behind.
func TestPermissionBoundary_Smoke(t *testing.T) {
	anon, _, _ := boundaryWorld(t, nil)
	viewer, _, _ := boundaryWorld(t, roleClaims("viewer", "viewer-user"))
	engineer, _, _ := boundaryWorld(t, engineerClaims())

	// Error paths first: anonymous 401 and low-privilege 403 on every
	// endpoint ( the import POST body is valid so a bug would surface as an
	// unexpected 2xx, not as a validation error ).
	body := discoverytest.ImportBody(discoverytest.ImportItem("aliyun", "acct-a", "cert-A"))
	getRoutes := []string{discoverytest.RoutePreview, discoverytest.RouteSnapshotStatus, discoverytest.RouteImportSession + unknownSessionID()}
	for _, h := range []*discoverytest.Harness{anon, viewer} {
		ledgerBefore := len(h.Ledger())
		for _, route := range getRoutes {
			resp := h.Get(route)
			assert.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, resp.StatusCode, resp.Body)
			assert.False(t, resp.Env.Success)
			assert.Empty(t, resp.Env.Data, "no business data leaks on denial")
		}
		for _, resp := range []*discoverytest.Response{h.Post(discoverytest.RouteImport, body)} {
			assert.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, resp.StatusCode, resp.Body)
			assert.False(t, resp.Env.Success)
			assert.Empty(t, resp.Env.Data, "no business data leaks on denial")
		}
		assert.Len(t, h.Ledger(), ledgerBefore, "denied paths leave the ledger untouched")
	}
	require.Equal(t, int32(0), anon.SessionsCreated(), "denied paths never create sessions")
	require.Equal(t, int32(0), viewer.SessionsCreated())

	// Authorized walkthrough on the same surface.
	preview := engineer.Get(discoverytest.RoutePreview)
	require.Equal(t, http.StatusOK, preview.StatusCode, preview.Body)
	assert.Equal(t, float64(1), preview.DataMap(t)["count"])
	assert.Equal(t, "cert-A", preview.PreviewItems(t)[0].(map[string]any)["cloudCertId"],
		"real preview entry flows for the whitelisted role")

	status := engineer.Get(discoverytest.RouteSnapshotStatus)
	require.Equal(t, http.StatusOK, status.StatusCode, status.Body)
	assert.Equal(t, true, status.DataMap(t)["hasSnapshot"])

	imp := engineer.Post(discoverytest.RouteImport, body)
	require.Equal(t, http.StatusAccepted, imp.StatusCode, imp.Body)
	sessionID, _ := imp.DataMap(t)["sessionId"].(string)
	require.NotEmpty(t, sessionID)

	progress := engineer.Get(discoverytest.RouteImportSession + sessionID)
	require.Equal(t, http.StatusOK, progress.StatusCode, progress.Body)
	assert.Equal(t, sessionID, progress.DataMap(t)["sessionId"])
	assert.Equal(t, int32(1), engineer.SessionsCreated(), "exactly one authorized session exists")
}
