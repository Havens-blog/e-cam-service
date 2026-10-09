// @feature cert-volcano-deployer @api-functional
//
// Journey fixtures for the volcano-cert-replacement contract tests: the
// volcano two-phase / verify-window / rollback / orphan-closure journeys
// reuse the multicloudtest harness ( production change-management HTTP
// surface over in-memory repositories ) and register the real
// VolcanoDeployer over the StubVolcanoCertLibrary — the same shape as the
// cert-multicloud-deployers journeys, extended to the 6th cloud.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package volcano_cert_replacement

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	sharedomain "github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/require"
)

// verifyConfirmProbes is domain.DefaultThresholds().VerifyConfirmProbes ( 2 ):
// consecutive consistent probe rounds required by the window.
const verifyConfirmProbes = 2

// volcanoCloud is the volcano cloud identity ( shared/domain account provider
// constant; cert/domain.Cloud 枚举未含火山，转译同值 ).
var volcanoCloud = domain.Cloud(sharedomain.CloudProviderVolcano)

// volcanoWorld bundles the fixture entities of one volcano replacement
// scenario ( one old/new ledger cert pair shared by four product references,
// distinct account per product so every ( fingerprint, cloud, accountKey )
// mapping key is unique — mirroring the multicloud journey precedent ).
type volcanoWorld struct {
	OldCertID   string
	OldFP       string
	NewCertID   string
	NewFP       string
	SnapshotID  string
	Domain      string // target SAN shared by old/new certificates
	OldCloudIDs map[domain.Product]string
	Accounts    map[domain.Product]string
}

// newVolcanoHarness builds the multicloud harness and registers the real
// VolcanoDeployer ( 4 products ) over the stub cert library on the production
// CloudAPIChannel — the registration-visibility seam under test.
func newVolcanoHarness(t *testing.T, stub *StubVolcanoCertLibrary) *multicloudtest.Harness {
	t.Helper()
	h := multicloudtest.NewHarness(t, nil)
	require.NoError(t, h.Channel.RegisterDeployer(string(sharedomain.CloudProviderVolcano),
		deployer.NewVolcanoDeployer(h.Mappings,
			deployer.WithVolcanoCertLibrary(stub),
			deployer.WithVolcanoRetryPolicy(multicloudtest.FastRetryPolicy())),
		"cdn", "waf", "alb", "nlb"))
	return h
}

// seedVolcanoReplacement seeds the four-product fixture: one complete old
// certificate, one ready new certificate ( shared SAN www.example.com ), a
// fresh done snapshot carrying one volcano reference per product ( CDN/WAF
// resource = domain, ALB/NLB = {lbId}/{listenerId} composite — the scan
// adapter resourceId forms ) with {product}:{id}-normalized referenced cloud
// cert IDs.
func seedVolcanoReplacement(t *testing.T, h *multicloudtest.Harness) volcanoWorld {
	t.Helper()
	w := volcanoWorld{Domain: "www.example.com"}
	w.OldCertID, w.OldFP = h.SeedCompleteCert("old.example.com", w.Domain)
	w.NewCertID, w.NewFP = h.SeedCompleteCert("new.example.com", w.Domain)

	w.Accounts = map[domain.Product]string{
		domain.ProductCDN: "ak-volcano-cdn",
		domain.ProductWAF: "ak-volcano-waf",
		domain.ProductALB: "ak-volcano-alb",
		domain.ProductNLB: "ak-volcano-nlb",
	}
	// WAF old cert ID is numeric ( WAF service certificate library Id form ).
	w.OldCloudIDs = map[domain.Product]string{
		domain.ProductCDN: "cdn:cert-old-cdn",
		domain.ProductWAF: "waf:101",
		domain.ProductALB: "alb:cert-old-alb",
		domain.ProductNLB: "nlb:cert-old-nlb",
	}

	w.SnapshotID = h.SeedDoneSnapshotWithRefs([]multicloudtest.RefSpec{
		{Cloud: volcanoCloud, Product: domain.ProductCDN, AccountKey: w.Accounts[domain.ProductCDN],
			ResourceID: w.Domain, CloudCertID: w.OldCloudIDs[domain.ProductCDN], Fingerprint: w.OldFP},
		{Cloud: volcanoCloud, Product: domain.ProductWAF, AccountKey: w.Accounts[domain.ProductWAF],
			ResourceID: w.Domain, CloudCertID: w.OldCloudIDs[domain.ProductWAF], Fingerprint: w.OldFP},
		{Cloud: volcanoCloud, Product: domain.ProductALB, AccountKey: w.Accounts[domain.ProductALB],
			ResourceID: "vol-alb-1/lsn-alb-1", CloudCertID: w.OldCloudIDs[domain.ProductALB], Fingerprint: w.OldFP},
		{Cloud: volcanoCloud, Product: domain.ProductNLB, AccountKey: w.Accounts[domain.ProductNLB],
			ResourceID: "vol-nlb-1/lsn-nlb-1", CloudCertID: w.OldCloudIDs[domain.ProductNLB], Fingerprint: w.OldFP},
	})
	return w
}

// seedLiveResources registers the in-cloud resources the bind layer locates
// at deploy/rollback time: the WAF protected domain ( with current
// AccessMode ), the ALB HTTPS listener and the NLB listener, each referencing
// the OLD product-library cert.
func seedLiveResources(t *testing.T, stub *StubVolcanoCertLibrary, w volcanoWorld) {
	t.Helper()
	stub.SetWAFDomain(w.Domain, 101, 1)
	stub.SetALBListener("lsn-alb-1", "vol-alb-1", "https", "cert-old-alb")
	stub.SetNLBListener("lsn-nlb-1", "vol-nlb-1", "cert-old-nlb")
}

// seedRollbackTargets registers valid rollback targets on the stub for every
// product and seeds the pre-replacement active mappings ( the old cloud cert
// per product, fingerprint = old ledger fingerprint ):
//   - cdn: native SHA-256 fingerprint channel ( cloud-side authoritative );
//   - waf/alb/nlb: no fingerprint channel — GetCert resolves the fingerprint
//     via the mapping reverse-lookup fallback ( the existing resolution
//     chain 口径 ), which is exactly what these seeded mappings exercise.
func seedRollbackTargets(t *testing.T, h *multicloudtest.Harness, stub *StubVolcanoCertLibrary, w volcanoWorld) {
	t.Helper()
	notAfterUnix := time.Now().Add(365 * 24 * time.Hour).Unix()
	notAfterStr := time.Now().Add(365 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	stub.SetCDNCert("cert-old-cdn", w.OldFP, notAfterUnix)
	stub.SetWAFServiceCert(101, notAfterStr)
	stub.SetALBCert("cert-old-alb", notAfterStr)
	stub.SetALBCert("cert-old-nlb", notAfterStr)
	for _, product := range []domain.Product{domain.ProductCDN, domain.ProductWAF, domain.ProductALB, domain.ProductNLB} {
		h.SeedMapping(w.OldFP, string(volcanoCloud), w.Accounts[product], w.OldCloudIDs[product])
	}
}

// batchedVolcanoConf is the confirm configuration for the 4-item volcano
// order: Enabled=true, BatchSize=2 ( effective size = min(2, floor(4/2)) = 2,
// two batches of two ), MaxBatchRatio=0.5 ( the hard upper bound ).
func batchedVolcanoConf() *deployer.BatchConf {
	return &deployer.BatchConf{Enabled: true, BatchSize: 2, MaxBatchRatio: 0.5}
}

// probeRounds runs the production verify-window prober for the given number
// of consecutive rounds ( default 2 = VerifyConfirmProbes ).
func probeRounds(t *testing.T, h *multicloudtest.Harness, rounds int) {
	t.Helper()
	for range rounds {
		_, err := h.Verify.ProbeVerifyingWindows(context.Background())
		require.NoError(t, err)
	}
}
