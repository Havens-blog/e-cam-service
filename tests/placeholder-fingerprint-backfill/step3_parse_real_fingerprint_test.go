// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/placeholder-fingerprint-backfill/contracts/step-3-parse-real-fingerprint.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package placeholder_fingerprint_backfill

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// postItems submits an import and returns the session id ( 202 required ).
func postItems(t *testing.T, h *discoverytest.Harness, items ...map[string]string) string {
	t.Helper()
	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(items...))
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	return resp.DataMap(t)["sessionId"].(string)
}

// garbagePEM returns in-cloud material whose PEM armor wraps non-DER bytes
// ( parse failure path ).
func garbagePEM() service.DiscoveryCertMaterial {
	return service.DiscoveryCertMaterial{
		Exists:       true,
		CertChainPEM: "-----BEGIN CERTIFICATE-----\nZ2FyYmFnZS1ub3QtYS1kZXI=\n-----END CERTIFICATE-----\n",
	}
}

// noPEM returns in-cloud material without any certificate payload.
func noPEM() service.DiscoveryCertMaterial {
	return service.DiscoveryCertMaterial{Exists: true, CertChainPEM: ""}
}

// TestStep3_ParseRealFingerprint_Success verifies the "success" outcome:
// the import resolves the real fingerprint, registers the fingerprint-only
// record and builds the mapping.
func TestStep3_ParseRealFingerprint_Success(t *testing.T) {
	h, tencent, snapID := tencentWorld(t, "acct-tx")
	b := certtest.NewBundle(t, "www.phb-parse.com", []string{"www.phb-parse.com"}, nil)
	tencent.AddMaterial("ssl-9", goodMaterial(b))
	h.SeedRefs(placeholderRefSpec("tencent", "acct-tx", "ssl-9", "res-1", snapID))

	final := importOne(t, h, "tencent", "acct-tx", "ssl-9")
	assert.Equal(t, "completed", final["status"])

	item := final["items"].([]any)[0].(map[string]any)
	assert.Equal(t, "success", item["result"])
	mappedCertID, _ := item["mappedCertId"].(string)
	require.NotEmpty(t, mappedCertID)

	ledger := h.Ledger()
	require.Len(t, ledger, 1, "the real fingerprint lands as a new record")
	assert.Equal(t, b.Fingerprint, ledger[0].Fingerprint, "placeholder replaced by the parsed real fingerprint")
	assert.Equal(t, domain.HostingStatusFingerprintOnly, ledger[0].HostingStatus)
	assert.Equal(t, mappedCertID, ledger[0].ID.Hex(), "mappedCertId links to the ledger record")

	mappings := h.MappingsByFP(b.Fingerprint)
	require.Len(t, mappings, 1)
	assert.Equal(t, "ssl-9", mappings[0].CloudCertID)
}

// TestStep3_ParseRealFingerprint_ParseFailureNoBackfill verifies the
// "parse-failure-no-backfill" outcome: an unparseable PEM fails the item,
// never triggers a backfill, and the session continues.
func TestStep3_ParseRealFingerprint_ParseFailureNoBackfill(t *testing.T) {
	h, tencent, snapID := tencentWorld(t, "acct-tx")
	bOK := certtest.NewBundle(t, "www.phb-parse-ok.com", []string{"www.phb-parse-ok.com"}, nil)
	tencent.AddMaterial("ssl-9", garbagePEM())
	tencent.AddMaterial("ssl-10", goodMaterial(bOK))
	h.SeedRefs(placeholderRefSpec("tencent", "acct-tx", "ssl-9", "res-1", snapID))
	h.SeedRefs(placeholderRefSpec("tencent", "acct-tx", "ssl-10", "res-2", snapID))

	final := h.PollImportTerminal(postItems(t, h,
		discoverytest.ImportItem("tencent", "acct-tx", "ssl-9"),
		discoverytest.ImportItem("tencent", "acct-tx", "ssl-10"),
	))
	assert.Equal(t, "partial_failed", final["status"])

	items := final["items"].([]any)
	bad := items[0].(map[string]any)
	assert.Equal(t, "failed", bad["result"])
	reason, _ := bad["errorReason"].(string)
	assert.Regexp(t, `^(CERT_[A-Z_]+|INTERNAL_ERROR): `, reason, "static code + text")
	ok := items[1].(map[string]any)
	assert.Equal(t, "success", ok["result"], "later items keep processing")

	ph := discoverytest.PlaceholderFP("tencent", "acct-tx", "ssl-9")
	refs := h.RefsByFP(ph)
	require.Len(t, refs, 1, "the failed placeholder item never triggers a backfill")
	assert.Equal(t, "ssl-9", refs[0].ReferencedCloudCertID)

	// The succeeding item's own backfill did run ( contrast within the
	// same session ): its placeholder reference carries the real fingerprint.
	okRefs := h.RefsByFP(bOK.Fingerprint)
	require.Len(t, okRefs, 1)
	assert.Equal(t, "ssl-10", okRefs[0].ReferencedCloudCertID)
	assert.Empty(t, h.RefsByFP(discoverytest.PlaceholderFP("tencent", "acct-tx", "ssl-10")))
}

// TestStep3_ParseRealFingerprint_NoPEMMaterial verifies the
// "no-pem-material" outcome: in-cloud but material-less entries fail with
// the dedicated static reason.
func TestStep3_ParseRealFingerprint_NoPEMMaterial(t *testing.T) {
	h, tencent, snapID := tencentWorld(t, "acct-tx")
	tencent.AddMaterial("ssl-9", noPEM())
	h.SeedRefs(placeholderRefSpec("tencent", "acct-tx", "ssl-9", "res-1", snapID))

	final := importOne(t, h, "tencent", "acct-tx", "ssl-9")
	assert.Equal(t, "partial_failed", final["status"])
	item := final["items"].([]any)[0].(map[string]any)
	assert.Equal(t, "failed", item["result"])
	assert.Equal(t, reasonNoPEM, item["errorReason"])

	assert.Empty(t, h.Ledger(), "no writes on the no-material path")
	ph := discoverytest.PlaceholderFP("tencent", "acct-tx", "ssl-9")
	require.Len(t, h.RefsByFP(ph), 1, "no backfill happened")
}
