// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/permission-boundary/contracts/step-2-write-endpoints-403.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package permission_boundary

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep2_WriteEndpoints403_Success verifies the "success" outcome: a
// non-OpsEngineer role gets 403 on the write endpoints with zero state
// side effects ( no session, no cloud call, no writes ).
func TestStep2_WriteEndpoints403_Success(t *testing.T) {
	h, aliyun, _ := boundaryWorld(t, roleClaims("viewer", "viewer-user"))
	ledgerBefore := len(h.Ledger())

	progress := h.Get(discoverytest.RouteImportSession + unknownSessionID())
	assert.Equal(t, http.StatusForbidden, progress.StatusCode, progress.Body)

	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-A"),
	))
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, resp.Body)
	assert.False(t, resp.Env.Success)
	require.NotNil(t, resp.Env.Error)
	assert.Equal(t, "FORBIDDEN", resp.Env.Error.Code)

	assert.Equal(t, int32(0), h.SessionsCreated(), "rejected import creates no session")
	assert.Len(t, h.Ledger(), ledgerBefore, "rejected import writes no ledger record")
	assert.Equal(t, int32(0), aliyun.Calls(), "rejected import triggers no cloud API call")
}

// TestStep2_WriteEndpoints403_AnyNonOpsRole403 verifies the
// "any-non-ops-role-403" outcome: the whitelist admits OpsEngineer only --
// every other authenticated role shape ( including the higher-privilege
// supervisor and unknown explicit values ) is denied.
func TestStep2_WriteEndpoints403_AnyNonOpsRole403(t *testing.T) {
	cases := []struct {
		name   string
		claims map[string]string
	}{
		{"viewer explicit role", roleClaims("viewer", "viewer-user")},
		{"auditor explicit role", roleClaims("auditor", "auditor-user")},
		{"ops_supervisor explicit role", roleClaims("ops_supervisor", "supervisor-user")},
		{"supervisor via cert:settings capability", map[string]string{"authorized_codes": "cert:settings", "username": "cap-user"}},
		{"unknown explicit role value denies", roleClaims("superadmin", "weird-user")},
		{"authenticated session without cert signals maps to viewer", map[string]string{"username": "plain-user"}},
		{"multi-role without engineer denies", roleClaims("viewer,auditor", "multi-user")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _, _ := boundaryWorld(t, tc.claims)
			resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
				discoverytest.ImportItem("aliyun", "acct-a", "cert-A"),
			))
			resp.RequireJSON(t)
			assert.Equal(t, http.StatusForbidden, resp.StatusCode, resp.Body)
			require.NotNil(t, resp.Env.Error)
			assert.Equal(t, "FORBIDDEN", resp.Env.Error.Code)
			assert.Equal(t, int32(0), h.SessionsCreated())
		})
	}
}
