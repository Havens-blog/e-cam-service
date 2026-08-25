// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/unsupported-entries-skip/contracts/step-3-item-skip-reasons.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package unsupported_entries_skip

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep3_ItemSkipReasons_UnsupportedCloudSkipped verifies the
// "unsupported-cloud-skipped" outcome: a forced huawei item is skipped with
// the static reason and the following parseable item still imports.
func TestStep3_ItemSkipReasons_UnsupportedCloudSkipped(t *testing.T) {
	h, aliyun, _, _ := mixedWorld(t)
	b := certtest.NewBundle(t, "www.skip-hw-contrast.com", []string{"www.skip-hw-contrast.com"}, nil)
	aliyun.AddMaterial("cert-A", goodMaterial(b))

	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("huawei", "acct-hw", "scm-1"),
		discoverytest.ImportItem("aliyun", "acct-a", "cert-A"),
	))
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	final := h.PollImportTerminal(resp.DataMap(t)["sessionId"].(string))
	assert.Equal(t, "partial_failed", final["status"])

	items := final["items"].([]any)
	require.Len(t, items, 2)
	huawei := items[0].(map[string]any)
	assert.Equal(t, "failed", huawei["result"])
	assert.Equal(t, reasonUnsupportedCloud, huawei["errorReason"], "static skip reason")
	assert.NotContains(t, huawei, "mappedCertId", "skipped item never maps to a ledger record")
	normal := items[1].(map[string]any)
	assert.Equal(t, "success", normal["result"], "the skip never blocks the parseable item")
	assert.NotEmpty(t, normal["mappedCertId"])

	require.Len(t, h.Ledger(), 1, "only the parseable item wrote the ledger")
	assert.Equal(t, b.Fingerprint, h.Ledger()[0].Fingerprint)
}

// TestStep3_ItemSkipReasons_IAMHostedSkipped verifies the
// "iam-hosted-skipped" outcome.
func TestStep3_ItemSkipReasons_IAMHostedSkipped(t *testing.T) {
	h, _, aws, _ := mixedWorld(t)
	b := certtest.NewBundle(t, "www.skip-arn.com", []string{"www.skip-arn.com"}, nil)
	aws.AddMaterial("arn:aws:acm:us-east-1:1:certificate/ok", goodMaterial(b))

	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aws", "acct-aws", "iam-123"),
		discoverytest.ImportItem("aws", "acct-aws", "arn:aws:acm:us-east-1:1:certificate/ok"),
	))
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	final := h.PollImportTerminal(resp.DataMap(t)["sessionId"].(string))
	assert.Equal(t, "partial_failed", final["status"])

	items := final["items"].([]any)
	iam := items[0].(map[string]any)
	assert.Equal(t, "failed", iam["result"])
	assert.Equal(t, reasonIAMHosted, iam["errorReason"], "IAM-hosted shares the unsupported semantics")
	assert.NotContains(t, iam, "mappedCertId", "skipped item never maps to a ledger record")
	arn := items[1].(map[string]any)
	assert.Equal(t, "success", arn["result"], "ARN-form item on the same cloud still imports")

	assert.Len(t, h.Ledger(), 1)
	assert.Equal(t, b.Fingerprint, h.Ledger()[0].Fingerprint)
}

// TestStep3_ItemSkipReasons_CertGoneSkipped verifies the
// "cert-gone-skipped" outcome: deleted-after-preview drift is caught by the
// real-time GetCert check.
func TestStep3_ItemSkipReasons_CertGoneSkipped(t *testing.T) {
	h, aliyun, _, snapID := mixedWorld(t)
	b := certtest.NewBundle(t, "www.skip-gone-contrast.com", []string{"www.skip-gone-contrast.com"}, nil)
	aliyun.AddMaterial("cert-A", goodMaterial(b))
	// cert-gone was selectable at preview time; no material is registered,
	// i.e. it was deleted cloud-side between preview and import.
	h.SeedRefs(discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-a", CloudCertID: "cert-gone", Fingerprint: discoverytest.FP("skip-gone"), ResourceID: "res-gone", SnapshotID: snapID})

	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-gone"),
		discoverytest.ImportItem("aliyun", "acct-a", "cert-A"),
	))
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	final := h.PollImportTerminal(resp.DataMap(t)["sessionId"].(string))
	assert.Equal(t, "partial_failed", final["status"])

	items := final["items"].([]any)
	gone := items[0].(map[string]any)
	assert.Equal(t, "failed", gone["result"])
	assert.Equal(t, reasonCertGone, gone["errorReason"])
	ok := items[1].(map[string]any)
	assert.Equal(t, "success", ok["result"], "the deleted cert never blocks the rest")
}
