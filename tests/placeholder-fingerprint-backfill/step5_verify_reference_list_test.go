// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/placeholder-fingerprint-backfill/contracts/step-5-verify-reference-list.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package placeholder_fingerprint_backfill

import (
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep5_VerifyReferenceList_Success verifies the "success" outcome:
// the newly registered certificate's reference list shows both the
// backfilled and the real-fingerprint references.
func TestStep5_VerifyReferenceList_Success(t *testing.T) {
	h, tencent, snapID := tencentWorld(t, "acct-tx")
	b := certtest.NewBundle(t, "www.phb-refs.com", []string{"www.phb-refs.com"}, nil)
	tencent.AddMaterial("ssl-9", goodMaterial(b))
	h.SeedRefs(
		placeholderRefSpec("tencent", "acct-tx", "ssl-9", "res-backfilled", snapID),
		discoverytest.RefSpec{Cloud: domain.CloudTencent, AccountKey: "acct-tx", CloudCertID: "ssl-9", Fingerprint: b.Fingerprint, ResourceID: "res-real", SnapshotID: snapID},
	)

	final := importOne(t, h, "tencent", "acct-tx", "ssl-9")
	assert.Equal(t, "completed", final["status"])
	item := final["items"].([]any)[0].(map[string]any)
	certID, _ := item["mappedCertId"].(string)
	require.NotEmpty(t, certID)

	resp := h.Get("/api/v1/certs/" + certID + discoverytest.RouteCertRefsSuffix)
	resp.RequireJSON(t)
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	data := resp.DataMap(t)
	assert.Equal(t, certID, data["certId"])
	assert.Equal(t, b.Fingerprint, data["fingerprint"])
	assert.Equal(t, float64(2), data["refCount"], "backfilled and real references both link to the certificate")
	groups := data["groups"].([]any)
	require.Len(t, groups, 1, "references group under their cloud/product")
}

// TestStep5_VerifyReferenceList_PartialFailureRescanRefresh verifies the
// "partial-failure-rescan-refresh" outcome: after a partial round fails to
// backfill, a rescan rebuilds the placeholder reference and the rerun
// converges the backfill.
func TestStep5_VerifyReferenceList_PartialFailureRescanRefresh(t *testing.T) {
	h, tencent, snapID := tencentWorld(t, "acct-tx")
	b1 := certtest.NewBundle(t, "www.phb-r1.com", []string{"www.phb-r1.com"}, nil)
	b2 := certtest.NewBundle(t, "www.phb-r2.com", []string{"www.phb-r2.com"}, nil)
	tencent.AddMaterial("ssl-1", goodMaterial(b1))
	tencent.AddMaterial("ssl-2", garbagePEM())
	h.SeedRefs(
		placeholderRefSpec("tencent", "acct-tx", "ssl-1", "res-1", snapID),
		placeholderRefSpec("tencent", "acct-tx", "ssl-2", "res-2", snapID),
	)

	first := h.PollImportTerminal(postItems(t, h,
		discoverytest.ImportItem("tencent", "acct-tx", "ssl-1"),
		discoverytest.ImportItem("tencent", "acct-tx", "ssl-2"),
	))
	assert.Equal(t, "partial_failed", first["status"], "first round: ssl-2 unparseable")
	require.Len(t, h.RefsByFP(b1.Fingerprint), 1, "ssl-1 backfilled")

	// Rescan: ssl-1 resolves through the ledger mapping ( real fingerprint ),
	// ssl-2 is still unresolvable and is rebuilt from the placeholder formula.
	h.Scan.SetRefsToWrite([]discoverytest.RefSpec{
		{Cloud: domain.CloudTencent, AccountKey: "acct-tx", CloudCertID: "ssl-1", Fingerprint: b1.Fingerprint, ResourceID: "res-1-new"},
		placeholderRefSpec("tencent", "acct-tx", "ssl-2", "res-2-new", ""),
	})
	anchor := h.Ledger()[0]
	triggered := h.Post("/api/v1/certs/"+anchor.ID.Hex()+discoverytest.RouteCertScanSuffix, nil)
	require.Equal(t, http.StatusOK, triggered.StatusCode, triggered.Body)
	h.FinishScanDone(triggered.DataMap(t)["snapshotId"].(string))

	// The cloud-side problem is fixed; the rerun converges the remainder.
	tencent.AddMaterial("ssl-2", goodMaterial(b2))
	rerun := importOne(t, h, "tencent", "acct-tx", "ssl-2")
	assert.Equal(t, "completed", rerun["status"])

	require.Len(t, h.RefsByFP(b1.Fingerprint), 2, "already-backfilled part untouched and rebuilt as real")
	refs2 := h.RefsByFP(b2.Fingerprint)
	require.Len(t, refs2, 2, "placeholder references across snapshots all converged by the triple-scoped backfill")
	ph2 := discoverytest.PlaceholderFP("tencent", "acct-tx", "ssl-2")
	assert.Empty(t, h.RefsByFP(ph2), "no placeholder remains for the rerun triple")
}

// TestStep5_VerifyReferenceList_Unauthorized verifies the "unauthorized"
// outcome.
func TestStep5_VerifyReferenceList_Unauthorized(t *testing.T) {
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) { c.Claims = nil })
	resp := h.Get("/api/v1/certs/000000000000000000000000" + discoverytest.RouteCertRefsSuffix)
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.False(t, resp.Env.Success)
	assert.Empty(t, resp.Env.Data)
}
