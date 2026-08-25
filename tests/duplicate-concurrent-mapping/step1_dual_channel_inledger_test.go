// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/duplicate-concurrent-mapping/contracts/step-1-dual-channel-inledger.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package duplicate_concurrent_mapping

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

// TestStep1_DualChannelInLedger_Success verifies the "success" outcome:
// both channels miss -> inLedger=false with the pending placeholder.
func TestStep1_DualChannelInLedger_Success(t *testing.T) {
	h, _, _, _ := multiAccountWorld(t)

	resp := h.Get(discoverytest.RoutePreview)
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	entry := discoverytest.FindPreviewItem(t, resp.PreviewItems(t), "aliyun", "acct-a", "cert-acc-a")
	assert.Equal(t, false, entry["inLedger"], "dual-channel miss keeps the entry selectable")
	assert.Equal(t, web.DiscoveryNotAfterPending, entry["notAfter"])
}

// TestStep1_DualChannelInLedger_FingerprintChannelHit verifies the
// "fingerprint-channel-hit" outcome.
func TestStep1_DualChannelInLedger_FingerprintChannelHit(t *testing.T) {
	h, _, b, _ := multiAccountWorld(t)
	ledgerNA := time.Date(2027, 10, 1, 0, 0, 0, 0, time.UTC)
	h.SeedCert(b.Fingerprint, ledgerNA) // manually imported before; no mapping row

	resp := h.Get(discoverytest.RoutePreview)
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	entry := discoverytest.FindPreviewItem(t, resp.PreviewItems(t), "aliyun", "acct-a", "cert-acc-a")
	assert.Equal(t, true, entry["inLedger"], "fingerprint channel alone marks inLedger")
	assert.Equal(t, ledgerNA.UTC().Format(time.RFC3339), entry["notAfter"], "ledger NotAfter is displayed")
}

// TestStep1_DualChannelInLedger_MappingChannelHit verifies the
// "mapping-channel-hit" outcome: a mapping row alone also marks inLedger
// even when the reference fingerprint matches no ledger certificate.
func TestStep1_DualChannelInLedger_MappingChannelHit(t *testing.T) {
	h := discoverytest.NewHarness(t, nil)
	snapID := h.SeedDoneSnapshotAt(time.Date(2026, 8, 24, 17, 30, 0, 0, time.UTC))
	unknownFP := discoverytest.FP("dup-mapping-channel")
	h.SeedRefs(discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-a", CloudCertID: "cert-acc-a", Fingerprint: unknownFP, ResourceID: "res-a", SnapshotID: snapID})
	h.SeedMapping(discoverytest.FP("dup-mapped-cert"), "aliyun", "acct-a", "cert-acc-a")

	resp := h.Get(discoverytest.RoutePreview)
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	entry := discoverytest.FindPreviewItem(t, resp.PreviewItems(t), "aliyun", "acct-a", "cert-acc-a")
	assert.Equal(t, true, entry["inLedger"], "mapping channel alone marks inLedger ( greyed out )")
}

// TestStep1_DualChannelInLedger_Unauthorized verifies the "unauthorized"
// outcome.
func TestStep1_DualChannelInLedger_Unauthorized(t *testing.T) {
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) { c.Claims = nil })
	resp := h.Get(discoverytest.RoutePreview)
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.False(t, resp.Env.Success)
	assert.Empty(t, resp.Env.Data)
}
