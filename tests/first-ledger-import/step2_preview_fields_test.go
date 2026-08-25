// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/first-ledger-import/contracts/step-2-preview-fields.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package first_ledger_import

import (
	"net/http"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/web"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// previewFieldsFixture seeds a done snapshot mixing unregistered entries with
// one ledger-fingerprint-hit entry ( >= 3 references per the fixture spec ).
func previewFieldsFixture(t *testing.T) (*discoverytest.Harness, string) {
	t.Helper()
	h := discoverytest.NewHarness(t, nil)
	started := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	snapID := h.SeedDoneSnapshotAt(started)
	ledgerNA := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	h.SeedCert(discoverytest.FP("step2-ledger"), ledgerNA)
	h.SeedRefs(
		discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-main", CloudCertID: "cert-U", Fingerprint: discoverytest.FP("step2-unreg"), ResourceID: "res-u", SnapshotID: snapID},
		discoverytest.RefSpec{Cloud: domain.CloudTencent, AccountKey: "acct-tx", CloudCertID: "ssl-8", Fingerprint: discoverytest.FP("step2-ledger"), ResourceID: "res-l", SnapshotID: snapID},
		discoverytest.RefSpec{Cloud: domain.CloudAzure, AccountKey: "acct-az", CloudCertID: "arn:aws:iam::1:server-certificate/az-9", Fingerprint: discoverytest.FP("step2-az"), ResourceID: "res-az", SnapshotID: snapID},
	)
	return h, snapID
}

// TestStep2_PreviewFields_Success verifies the "success" outcome: every
// entry exposes the seven field classes; unregistered entries show the
// pending placeholder while ledger-hit entries show the ledger NotAfter;
// the response carries snapshotStartedAt.
func TestStep2_PreviewFields_Success(t *testing.T) {
	h, snapID := previewFieldsFixture(t)
	ledgerNA := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)

	resp := h.Get(discoverytest.RoutePreview)
	resp.RequireJSON(t)
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	data := resp.DataMap(t)
	require.Equal(t, snapID, data["snapshotId"])
	assert.NotEmpty(t, data["snapshotStartedAt"], "snapshot point-in-time field")
	assert.Equal(t, float64(3), data["count"], "count invariant: count equals the deduped item count")

	items := resp.PreviewItems(t)
	require.Len(t, items, 3)
	for _, raw := range items {
		entry := raw.(map[string]any)
		for _, field := range []string{"cloud", "accountKey", "cloudCertId", "refCount", "inLedger", "notAfter", "parseable"} {
			assert.Contains(t, entry, field, "seven field classes must be present")
		}
	}

	unregistered := discoverytest.FindPreviewItem(t, items, "aliyun", "acct-main", "cert-U")
	assert.Equal(t, false, unregistered["inLedger"])
	assert.Equal(t, web.DiscoveryNotAfterPending, unregistered["notAfter"], "unregistered notAfter placeholder")
	assert.Equal(t, true, unregistered["parseable"])

	inLedger := discoverytest.FindPreviewItem(t, items, "tencent", "acct-tx", "ssl-8")
	assert.Equal(t, true, inLedger["inLedger"], "ledger fingerprint hit marks inLedger")
	assert.Equal(t, ledgerNA.UTC().Format(time.RFC3339), inLedger["notAfter"], "inLedger entry shows ledger NotAfter")
	discoverytest.RequireNoKeyMaterial(t, resp.Body)
}

// TestStep2_PreviewFields_StaleSnapshotNotice verifies the
// "stale-snapshot-notice" outcome: a done snapshot older than 7 days is
// still used and its original snapshotStartedAt is reported verbatim.
func TestStep2_PreviewFields_StaleSnapshotNotice(t *testing.T) {
	h := discoverytest.NewHarness(t, nil)
	stale := time.Now().UTC().Add(-8 * 24 * time.Hour).Truncate(time.Second)
	snapID := h.SeedDoneSnapshotAt(stale)
	h.SeedRefs(discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-main", CloudCertID: "cert-S", Fingerprint: discoverytest.FP("step2-stale"), ResourceID: "res-s", SnapshotID: snapID})

	resp := h.Get(discoverytest.RoutePreview)
	resp.RequireJSON(t)
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	data := resp.DataMap(t)
	assert.Equal(t, snapID, data["snapshotId"], "stale done snapshot is still the data source")
	assert.Equal(t, stale.UTC().Format(time.RFC3339), data["snapshotStartedAt"],
		"the original stale timestamp is reported so the frontend can flag rescan")
	assert.Equal(t, float64(1), data["count"])
}

// TestStep2_PreviewFields_UnparseableGroup verifies the
// "unparseable-group" outcome: huawei and AWS IAM-hosted ( non-ARN )
// entries are marked unparseable while normal entries stay selectable.
func TestStep2_PreviewFields_UnparseableGroup(t *testing.T) {
	h := discoverytest.NewHarness(t, nil)
	snapID := h.SeedDoneSnapshotAt(time.Date(2026, 8, 21, 11, 0, 0, 0, time.UTC))
	h.SeedRefs(
		discoverytest.RefSpec{Cloud: domain.CloudHuawei, AccountKey: "acct-hw", CloudCertID: "scm-1", Fingerprint: discoverytest.FP("step2-hw"), ResourceID: "res-hw", SnapshotID: snapID},
		discoverytest.RefSpec{Cloud: domain.CloudAWS, AccountKey: "acct-aws", CloudCertID: "iam-hosted-id", Fingerprint: discoverytest.FP("step2-iam"), ResourceID: "res-iam", SnapshotID: snapID},
		discoverytest.RefSpec{Cloud: domain.CloudAWS, AccountKey: "acct-aws", CloudCertID: "arn:aws:acm:us-east-1:1:certificate/abc", Fingerprint: discoverytest.FP("step2-arn"), ResourceID: "res-arn", SnapshotID: snapID},
		discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-main", CloudCertID: "cert-A", Fingerprint: discoverytest.FP("step2-normal"), ResourceID: "res-n", SnapshotID: snapID},
	)

	resp := h.Get(discoverytest.RoutePreview)
	resp.RequireJSON(t)
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	items := resp.PreviewItems(t)
	require.Len(t, items, 4)

	huawei := discoverytest.FindPreviewItem(t, items, "huawei", "acct-hw", "scm-1")
	assert.Equal(t, false, huawei["parseable"], "huawei group is unselectable")
	assert.Equal(t, "unsupported_cloud", huawei["parseReason"])

	iam := discoverytest.FindPreviewItem(t, items, "aws", "acct-aws", "iam-hosted-id")
	assert.Equal(t, false, iam["parseable"], "AWS IAM-hosted (non-ARN) is degraded unselectable")
	assert.Equal(t, "iam_hosted", iam["parseReason"])

	arn := discoverytest.FindPreviewItem(t, items, "aws", "acct-aws", "arn:aws:acm:us-east-1:1:certificate/abc")
	assert.Equal(t, true, arn["parseable"], "AWS ARN-form stays selectable")

	normal := discoverytest.FindPreviewItem(t, items, "aliyun", "acct-main", "cert-A")
	assert.Equal(t, true, normal["parseable"])
	assert.Empty(t, normal["parseReason"], "normal entry carries no parse reason")
}
