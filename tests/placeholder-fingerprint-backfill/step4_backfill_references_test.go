// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/placeholder-fingerprint-backfill/contracts/step-4-backfill-references.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package placeholder_fingerprint_backfill

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

// TestStep4_BackfillReferences_Success verifies the "success" outcome:
// after the item succeeds, every placeholder reference of the triple is
// backfilled to the import-time real fingerprint.
func TestStep4_BackfillReferences_Success(t *testing.T) {
	h, tencent, snapID := tencentWorld(t, "acct-tx")
	b := certtest.NewBundle(t, "www.phb-backfill.com", []string{"www.phb-backfill.com"}, nil)
	tencent.AddMaterial("ssl-9", goodMaterial(b))
	h.SeedRefs(
		placeholderRefSpec("tencent", "acct-tx", "ssl-9", "res-1", snapID),
		placeholderRefSpec("tencent", "acct-tx", "ssl-9", "res-2", snapID),
	)

	final := importOne(t, h, "tencent", "acct-tx", "ssl-9")
	assert.Equal(t, "completed", final["status"])

	refs := h.RefsByFP(b.Fingerprint)
	require.Len(t, refs, 2, "both placeholder references now carry the real fingerprint")
	for _, r := range refs {
		assert.Equal(t, "ssl-9", r.ReferencedCloudCertID)
		assert.NotEqual(t, discoverytest.PlaceholderFP("tencent", "acct-tx", "ssl-9"), r.CertFingerprint)
	}
	assert.Empty(t, h.RefsByFP(discoverytest.PlaceholderFP("tencent", "acct-tx", "ssl-9")),
		"no placeholder reference remains for the triple")
}

// TestStep4_BackfillReferences_RealFingerprintNeverOverwritten verifies
// the "real-fingerprint-never-overwritten" outcome: the CAS backfill only
// replaces the exact placeholder value; real fingerprints survive.
func TestStep4_BackfillReferences_RealFingerprintNeverOverwritten(t *testing.T) {
	h, tencent, snapID := tencentWorld(t, "acct-tx")
	b := certtest.NewBundle(t, "www.phb-cas.com", []string{"www.phb-cas.com"}, nil)
	tencent.AddMaterial("ssl-9", goodMaterial(b))
	otherFP := discoverytest.FP("phb-other-cert")
	h.SeedCert(otherFP, time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC))

	h.SeedRefs(
		placeholderRefSpec("tencent", "acct-tx", "ssl-9", "res-placeholder", snapID),
		discoverytest.RefSpec{Cloud: domain.CloudTencent, AccountKey: "acct-tx", CloudCertID: "ssl-9", Fingerprint: otherFP, ResourceID: "res-real", SnapshotID: snapID},
	)

	final := importOne(t, h, "tencent", "acct-tx", "ssl-9")
	assert.Equal(t, "completed", final["status"])

	backfilled := h.RefsByFP(b.Fingerprint)
	require.Len(t, backfilled, 1, "only the placeholder reference was rewritten")
	assert.Equal(t, "res-placeholder", backfilled[0].ResourceID)

	kept := h.RefsByFP(otherFP)
	require.Len(t, kept, 1, "the real fingerprint reference survives untouched")
	assert.Equal(t, "res-real", kept[0].ResourceID)
}

// TestStep4_BackfillReferences_ACMRenewalCurrentCert verifies the
// "acm-renewal-current-cert" outcome: the backfill uses the import-time
// certificate content, not the scan-time one.
func TestStep4_BackfillReferences_ACMRenewalCurrentCert(t *testing.T) {
	aws := discoverytest.NewStubCertAdapter(domain.CloudAWS)
	renewed := certtest.NewBundle(t, "www.phb-renewed.com", []string{"www.phb-renewed.com"}, nil)
	aws.AddMaterial("arn:aws:acm:us-east-1:1:certificate/renewed", goodMaterial(renewed))
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) {
		c.Adapters = []service.DiscoveryCertAdapter{aws}
		c.Accounts = map[domain.Cloud][]*sharedomain.CloudAccount{
			domain.CloudAWS: {discoverytest.ActiveAccount(domain.CloudAWS, "acct-aws")},
		}
	})
	snapID := h.SeedDoneSnapshotAt(time.Date(2026, 8, 24, 16, 0, 0, 0, time.UTC))
	h.SeedRefs(placeholderRefSpec("aws", "acct-aws", "arn:aws:acm:us-east-1:1:certificate/renewed", "res-acm", snapID))

	final := importOne(t, h, "aws", "acct-aws", "arn:aws:acm:us-east-1:1:certificate/renewed")
	assert.Equal(t, "completed", final["status"])

	refs := h.RefsByFP(renewed.Fingerprint)
	require.Len(t, refs, 1, "the reference carries the renewed ( import-time ) fingerprint")
}

// TestStep4_BackfillReferences_WrongBackfillRescanRecoverable verifies the
// "wrong-backfill-rescan-recoverable" outcome: the placeholder formula is
// deterministic, so a rescan rebuilds the placeholder reference and a
// re-import converges again.
func TestStep4_BackfillReferences_WrongBackfillRescanRecoverable(t *testing.T) {
	h, tencent, snapID := tencentWorld(t, "acct-tx")
	b := certtest.NewBundle(t, "www.phb-recover.com", []string{"www.phb-recover.com"}, nil)
	tencent.AddMaterial("ssl-9", goodMaterial(b))

	// A wrongly-backfilled reference ( hypothetical bad write ) holding a
	// value that matches no real certificate.
	wrongFP := discoverytest.FP("phb-wrong-backfill")
	h.SeedRefs(discoverytest.RefSpec{Cloud: domain.CloudTencent, AccountKey: "acct-tx", CloudCertID: "ssl-9", Fingerprint: wrongFP, ResourceID: "res-wrong", SnapshotID: snapID})

	// A rescan rebuilds the reference from the deterministic placeholder
	// formula ( the documented recovery for unresolved triples ).
	h.Scan.SetRefsToWrite([]discoverytest.RefSpec{placeholderRefSpec("tencent", "acct-tx", "ssl-9", "res-wrong", "")})
	cert := h.SeedCert(discoverytest.FP("phb-recover-anchor"), time.Date(2027, 5, 1, 0, 0, 0, 0, time.UTC))
	triggered := h.Post("/api/v1/certs/"+cert.ID.Hex()+discoverytest.RouteCertScanSuffix, nil)
	require.Equal(t, http.StatusOK, triggered.StatusCode, triggered.Body)
	h.FinishScanDone(triggered.DataMap(t)["snapshotId"].(string))

	ph := discoverytest.PlaceholderFP("tencent", "acct-tx", "ssl-9")
	require.Len(t, h.RefsByFP(ph), 1, "rescan rebuilt the placeholder reference")

	// The re-import backfills it to the real fingerprint: recovered.
	final := importOne(t, h, "tencent", "acct-tx", "ssl-9")
	assert.Equal(t, "completed", final["status"])
	require.Len(t, h.RefsByFP(b.Fingerprint), 1, "converged back to the real fingerprint")
	assert.Empty(t, h.RefsByFP(ph))
}

// TestStep4_BackfillReferences_MultiAccountScoped verifies the
// "multi-account-scoped" outcome: backfill is scoped per
// ( cloud, accountKey, cloudCertId ) triple and never crosses accounts.
func TestStep4_BackfillReferences_MultiAccountScoped(t *testing.T) {
	h, tencent, snapID := tencentWorld(t, "acct-a", "acct-b", "acct-c")
	b := certtest.NewBundle(t, "www.phb-multi.com", []string{"www.phb-multi.com"}, nil)
	tencent.AddMaterial("shared-ssl", goodMaterial(b))
	h.SeedRefs(
		placeholderRefSpec("tencent", "acct-a", "shared-ssl", "res-a", snapID),
		placeholderRefSpec("tencent", "acct-b", "shared-ssl", "res-b", snapID),
		placeholderRefSpec("tencent", "acct-c", "shared-ssl", "res-c", snapID),
	)

	final := h.PollImportTerminal(postItems(t, h,
		discoverytest.ImportItem("tencent", "acct-a", "shared-ssl"),
		discoverytest.ImportItem("tencent", "acct-b", "shared-ssl"),
	))
	assert.Equal(t, "completed", final["status"])

	refs := h.RefsByFP(b.Fingerprint)
	require.Len(t, refs, 2, "only the imported triples were backfilled")
	resources := []string{refs[0].ResourceID, refs[1].ResourceID}
	assert.ElementsMatch(t, []string{"res-a", "res-b"}, resources)

	untouched := h.RefsByFP(discoverytest.PlaceholderFP("tencent", "acct-c", "shared-ssl"))
	require.Len(t, untouched, 1, "the never-imported account keeps its placeholder reference")
	assert.Equal(t, "res-c", untouched[0].ResourceID)
}
