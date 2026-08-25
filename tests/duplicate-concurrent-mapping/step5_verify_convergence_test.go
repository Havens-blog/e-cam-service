// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/duplicate-concurrent-mapping/contracts/step-5-verify-convergence.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package duplicate_concurrent_mapping

import (
	"net/http"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep5_VerifyConvergence_Success verifies the "success" outcome: the
// converged world holds one ledger record, per-account mappings and
// references associated with the certificate.
func TestStep5_VerifyConvergence_Success(t *testing.T) {
	h, _, b, _ := multiAccountWorld(t)

	final := importAndAwait(t, h,
		discoverytest.ImportItem("aliyun", "acct-a", "cert-acc-a"),
		discoverytest.ImportItem("aliyun", "acct-b", "cert-acc-b"),
	)
	assert.Equal(t, "completed", final["status"])

	ledger := h.Ledger()
	require.Len(t, ledger, 1)
	certID := ledger[0].ID.Hex()

	// The references associate with the certificate through the API view.
	view := h.Get("/api/v1/certs/" + certID + discoverytest.RouteCertRefsSuffix)
	require.Equal(t, http.StatusOK, view.StatusCode, view.Body)
	data := view.DataMap(t)
	assert.Equal(t, b.Fingerprint, data["fingerprint"])
	assert.Equal(t, float64(2), data["refCount"], "both account references link to the record")

	require.Len(t, h.MappingsByFP(b.Fingerprint), 2)
	assert.Len(t, h.RefsByFP(b.Fingerprint), 2)

	// Every converged reference carries the certificate's fingerprint, so
	// the association is immediate ( cross-entity reference -> certificate ).
	for _, r := range h.RefsByFP(b.Fingerprint) {
		assert.Equal(t, ledger[0].Fingerprint, r.CertFingerprint)
	}
}

// TestStep5_VerifyConvergence_ReplayIdempotent verifies the
// "replay-idempotent" outcome: resubmitting the same batch converges
// completed with backfill notes and zero new records.
func TestStep5_VerifyConvergence_ReplayIdempotent(t *testing.T) {
	h, _, b, _ := multiAccountWorld(t)

	first := importAndAwait(t, h,
		discoverytest.ImportItem("aliyun", "acct-a", "cert-acc-a"),
		discoverytest.ImportItem("aliyun", "acct-b", "cert-acc-b"),
	)
	require.Equal(t, "completed", first["status"])

	second := importAndAwait(t, h,
		discoverytest.ImportItem("aliyun", "acct-a", "cert-acc-a"),
		discoverytest.ImportItem("aliyun", "acct-b", "cert-acc-b"),
	)
	assert.Equal(t, "completed", second["status"], "replay stays completed")
	survivorID := h.Ledger()[0].ID.Hex()
	for _, raw := range second["items"].([]any) {
		it := raw.(map[string]any)
		assert.Equal(t, "success", it["result"])
		assert.Equal(t, reasonInLedger, it["errorReason"], "every replayed item takes the backfill path")
		assert.Equal(t, survivorID, it["mappedCertId"],
			"replay still maps onto the original ledger record ( cross-entity )")
	}

	assert.Len(t, h.Ledger(), 1, "uk_fingerprint keeps the ledger count at one")
	assert.Len(t, h.MappingsByFP(b.Fingerprint), 2, "uk_fp_cloud_account keeps the mapping count at two")
}

// TestStep5_VerifyConvergence_CrossCloudSameFingerprint verifies the
// "cross-cloud-same-fingerprint" outcome.
func TestStep5_VerifyConvergence_CrossCloudSameFingerprint(t *testing.T) {
	aliyun := discoverytest.NewStubCertAdapter(domain.CloudAliyun)
	tencent := discoverytest.NewStubCertAdapter(domain.CloudTencent)
	b := certtest.NewBundle(t, "www.dup-crosscloud.com", []string{"www.dup-crosscloud.com"}, nil)
	aliyun.AddMaterial("cert-ali", service.DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)})
	tencent.AddMaterial("ssl-tx", service.DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)})
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) {
		c.Adapters = []service.DiscoveryCertAdapter{aliyun, tencent}
		c.Accounts = map[domain.Cloud][]*sharedomain.CloudAccount{
			domain.CloudAliyun:  {discoverytest.ActiveAccount(domain.CloudAliyun, "acct-ali")},
			domain.CloudTencent: {discoverytest.ActiveAccount(domain.CloudTencent, "acct-tx")},
		}
	})
	snapID := h.SeedDoneSnapshotAt(time.Date(2026, 8, 24, 18, 0, 0, 0, time.UTC))
	h.SeedRefs(
		discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-ali", CloudCertID: "cert-ali", Fingerprint: b.Fingerprint, ResourceID: "res-ali", SnapshotID: snapID},
		discoverytest.RefSpec{Cloud: domain.CloudTencent, AccountKey: "acct-tx", CloudCertID: "ssl-tx", Fingerprint: b.Fingerprint, ResourceID: "res-tx", SnapshotID: snapID},
	)

	final := importAndAwait(t, h,
		discoverytest.ImportItem("aliyun", "acct-ali", "cert-ali"),
		discoverytest.ImportItem("tencent", "acct-tx", "ssl-tx"),
	)
	assert.Equal(t, "completed", final["status"])
	items := final["items"].([]any)
	assert.Equal(t, "success", items[0].(map[string]any)["result"])
	second := items[1].(map[string]any)
	assert.Equal(t, "success", second["result"])
	assert.Equal(t, reasonInLedger, second["errorReason"], "the second cloud hits the same fingerprint")

	assert.Len(t, h.Ledger(), 1, "cross-cloud identical content still yields one record")
	mappings := h.MappingsByFP(b.Fingerprint)
	require.Len(t, mappings, 2, "one mapping per cloud")
	assert.ElementsMatch(t, []string{"aliyun", "tencent"}, []string{mappings[0].Cloud, mappings[1].Cloud})
}
