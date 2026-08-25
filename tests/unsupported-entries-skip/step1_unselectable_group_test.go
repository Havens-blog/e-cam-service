// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/unsupported-entries-skip/contracts/step-1-unselectable-group.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package unsupported_entries_skip

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep1_UnselectableGroup_HuaweiGroupUnselectable verifies the
// "huawei-group-unselectable" outcome.
func TestStep1_UnselectableGroup_HuaweiGroupUnselectable(t *testing.T) {
	h, _, _, _ := mixedWorld(t)

	resp := h.Get(discoverytest.RoutePreview)
	resp.RequireJSON(t)
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	huawei := discoverytest.FindPreviewItem(t, resp.PreviewItems(t), "huawei", "acct-hw", "scm-1")
	assert.Equal(t, false, huawei["parseable"], "huawei group is unselectable")
	assert.Equal(t, "unsupported_cloud", huawei["parseReason"])
}

// TestStep1_UnselectableGroup_IAMHostedDegraded verifies the
// "iam-hosted-degraded" outcome.
func TestStep1_UnselectableGroup_IAMHostedDegraded(t *testing.T) {
	h, _, _, _ := mixedWorld(t)

	resp := h.Get(discoverytest.RoutePreview)
	resp.RequireJSON(t)
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	iam := discoverytest.FindPreviewItem(t, resp.PreviewItems(t), "aws", "acct-aws", "iam-123")
	assert.Equal(t, false, iam["parseable"], "AWS IAM-hosted (non-ARN) is degraded unselectable")
	assert.Equal(t, "iam_hosted", iam["parseReason"])
}

// TestStep1_UnselectableGroup_NormalEntriesSelectable verifies the
// "normal-entries-selectable" outcome ( contrast group ).
func TestStep1_UnselectableGroup_NormalEntriesSelectable(t *testing.T) {
	h, _, _, _ := mixedWorld(t)

	resp := h.Get(discoverytest.RoutePreview)
	resp.RequireJSON(t)
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	items := resp.PreviewItems(t)

	aliyun := discoverytest.FindPreviewItem(t, items, "aliyun", "acct-a", "cert-A")
	assert.Equal(t, true, aliyun["parseable"], "normal entry stays selectable")
	assert.Empty(t, aliyun["parseReason"])

	arn := discoverytest.FindPreviewItem(t, items, "aws", "acct-aws", "arn:aws:acm:us-east-1:1:certificate/ok")
	assert.Equal(t, true, arn["parseable"], "AWS ARN form stays selectable")
	assert.Empty(t, arn["parseReason"])
}

// TestStep1_UnselectableGroup_Unauthorized verifies the "unauthorized"
// outcome.
func TestStep1_UnselectableGroup_Unauthorized(t *testing.T) {
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) { c.Claims = nil })
	resp := h.Get(discoverytest.RoutePreview)
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.False(t, resp.Env.Success)
	assert.Empty(t, resp.Env.Data, "no preview group data leaks")
}
