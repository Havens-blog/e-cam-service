// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/first-ledger-import/contracts/step-4-session-item-processing.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package first_ledger_import

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep4_SessionItemProcessing_Success verifies the "success" outcome:
// one parseable item goes GetCert -> sanitize -> parse -> fingerprint-only
// registration -> idempotent mapping, ending in success with mappedCertId.
func TestStep4_SessionItemProcessing_Success(t *testing.T) {
	h, aliyun := aliyunWorld(t)
	b := certtest.NewBundle(t, "www.step4-example.com", []string{"www.step4-example.com"}, nil)
	aliyun.AddMaterial("cert-A", materialFor(b))

	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-A"),
	))
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	sessionID, _ := resp.DataMap(t)["sessionId"].(string)

	final := h.PollImportTerminal(sessionID)
	assert.Equal(t, "completed", final["status"])
	items := final["items"].([]any)
	require.Len(t, items, 1)
	item := items[0].(map[string]any)
	assert.Equal(t, "success", item["result"])
	mappedCertID, _ := item["mappedCertId"].(string)
	require.NotEmpty(t, mappedCertID, "success item carries mappedCertId")

	ledger := h.Ledger()
	require.Len(t, ledger, 1, "one fingerprint-only ledger record")
	assert.Equal(t, b.Fingerprint, ledger[0].Fingerprint)
	assert.Equal(t, domain.HostingStatusFingerprintOnly, ledger[0].HostingStatus)
	assert.Nil(t, ledger[0].EncryptedPrivateKey, "no private key material is ever persisted")
	assert.NotContains(t, ledger[0].CertPEM, "PRIVATE KEY", "ledger PEM holds CERTIFICATE blocks only")
	assert.GreaterOrEqual(t, strings.Count(ledger[0].CertPEM, "-----BEGIN CERTIFICATE-----"), 1)
	assert.Equal(t, mappedCertID, ledger[0].ID.Hex(),
		"mappedCertId points at the ledger record ( cross-entity link )")

	mappings := h.MappingsByFP(b.Fingerprint)
	require.Len(t, mappings, 1, "one cloud cert mapping for the triple")
	assert.Equal(t, "acct-a", mappings[0].AccountKey)
	assert.Equal(t, "cert-A", mappings[0].CloudCertID)
	assert.Equal(t, "aliyun", mappings[0].Cloud, "mapping row records the cloud of the triple")

	progress := final["progress"].(map[string]any)
	assert.Equal(t, float64(1), progress["succeeded"])
	assert.Equal(t, float64(0), progress["failed"])
}

// TestStep4_SessionItemProcessing_ItemFailureContinues verifies the
// "item-failure-continues" outcome: a doomed first item records a static
// reason and the session still processes the following item.
func TestStep4_SessionItemProcessing_ItemFailureContinues(t *testing.T) {
	h, aliyun := aliyunWorld(t)
	bOk := certtest.NewBundle(t, "www.step4-ok.com", []string{"www.step4-ok.com"}, nil)
	aliyun.AddMaterial("cert-bad", garbagePEMMaterial())
	aliyun.AddMaterial("cert-ok", materialFor(bOk))

	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-bad"),
		discoverytest.ImportItem("aliyun", "acct-a", "cert-ok"),
	))
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	sessionID, _ := resp.DataMap(t)["sessionId"].(string)

	final := h.PollImportTerminal(sessionID)
	assert.Equal(t, "partial_failed", final["status"], "one failure converges the session to partial_failed")
	items := final["items"].([]any)
	require.Len(t, items, 2)

	failed := items[0].(map[string]any)
	assert.Equal(t, "failed", failed["result"])
	reason, _ := failed["errorReason"].(string)
	require.NotEmpty(t, reason, "failed item records an error reason")
	assert.Regexp(t, `^(CERT_[A-Z_]+|INTERNAL_ERROR): `, reason, "reason is code + static text")
	assert.NotContains(t, reason, "garbage", "reason never carries cloud/raw response fragments")

	succeeded := items[1].(map[string]any)
	assert.Equal(t, "success", succeeded["result"], "session continues past the failed item")
	assert.NotEmpty(t, succeeded["mappedCertId"])

	progress := final["progress"].(map[string]any)
	assert.Equal(t, float64(1), progress["succeeded"])
	assert.Equal(t, float64(1), progress["failed"])

	ledger := h.Ledger()
	require.Len(t, ledger, 1, "only the succeeding item wrote the ledger")
	assert.Equal(t, bOk.Fingerprint, ledger[0].Fingerprint)
}

// TestStep4_SessionItemProcessing_CertDeletedAfterPreview verifies the
// "cert-deleted-after-preview" outcome: drift between preview and import is
// caught by the real-time GetCert existence check.
func TestStep4_SessionItemProcessing_CertDeletedAfterPreview(t *testing.T) {
	h, _ := aliyunWorld(t)
	fp := discoverytest.FP("step4-gone")
	snapID := h.SeedDoneSnapshotAt(time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC))
	h.SeedRefs(discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-a", CloudCertID: "cert-A", Fingerprint: fp, ResourceID: "res-1", SnapshotID: snapID})

	// At preview time the entry is selectable ( preview is snapshot-based and
	// never calls the cloud ); no material is registered, i.e. the certificate
	// has been deleted cloud-side between preview and import.
	preview := h.Get(discoverytest.RoutePreview)
	require.Equal(t, http.StatusOK, preview.StatusCode)
	entry := discoverytest.FindPreviewItem(t, preview.PreviewItems(t), "aliyun", "acct-a", "cert-A")
	assert.Equal(t, true, entry["parseable"], "entry is selectable at preview time")

	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-A"),
	))
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	sessionID, _ := resp.DataMap(t)["sessionId"].(string)

	final := h.PollImportTerminal(sessionID)
	assert.Equal(t, "partial_failed", final["status"])
	item := final["items"].([]any)[0].(map[string]any)
	assert.Equal(t, "failed", item["result"])
	assert.Equal(t, "CERT_GET_FAILED: 云侧已不存在", item["errorReason"])

	assert.Empty(t, h.Ledger(), "deleted cert writes nothing to the ledger")
	assert.Empty(t, h.MappingsByFP(fp))
}
