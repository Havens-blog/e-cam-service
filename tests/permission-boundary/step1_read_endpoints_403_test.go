// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/permission-boundary/contracts/step-1-read-endpoints-403.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package permission_boundary

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep1_ReadEndpoints403_Success verifies the "success" outcome: a
// logged-in non-OpsEngineer role is denied 403 on both read endpoints even
// though importable data exists ( denial, not absence ).
func TestStep1_ReadEndpoints403_Success(t *testing.T) {
	h, _, _ := boundaryWorld(t, roleClaims("viewer", "viewer-user"))

	for _, route := range []string{discoverytest.RoutePreview, discoverytest.RouteSnapshotStatus} {
		resp := h.Get(route)
		resp.RequireJSON(t)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%s: %s", route, resp.Body)
		assert.False(t, resp.Env.Success)
		require.NotNil(t, resp.Env.Error, route)
		assert.Equal(t, "FORBIDDEN", resp.Env.Error.Code)
		assert.Equal(t, "insufficient role for this operation", resp.Env.Error.Message,
			"fixed safe message: no endpoint internals or stack traces leak")
		assert.NotContains(t, resp.Body, "goroutine")
		assert.NotContains(t, resp.Body, ".go:")
	}
}

// TestStep1_ReadEndpoints403_Unauthenticated401 verifies the
// "unauthenticated-401" outcome: authentication runs before role judgement
// and business logic on all four endpoints.
func TestStep1_ReadEndpoints403_Unauthenticated401(t *testing.T) {
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) { c.Claims = nil })
	routes := []string{
		discoverytest.RoutePreview,
		discoverytest.RouteSnapshotStatus,
		discoverytest.RouteImport,
		discoverytest.RouteImportSession + unknownSessionID(),
	}
	for _, route := range routes {
		var resp *discoverytest.Response
		if route == discoverytest.RouteImport {
			resp = h.Post(route, discoverytest.ImportBody(discoverytest.ImportItem("aliyun", "acct-a", "cert-A")))
		} else {
			resp = h.Get(route)
		}
		resp.RequireJSON(t)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "%s: %s", route, resp.Body)
		assert.False(t, resp.Env.Success)
		assert.Nil(t, resp.Env.Data, "no business data on auth failure")
	}
	assert.Equal(t, int32(0), h.SessionsCreated(), "auth failure creates no sessions")
}
