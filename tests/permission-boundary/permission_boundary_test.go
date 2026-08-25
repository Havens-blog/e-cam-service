// @feature cert-cloud-discovery-import @api-functional
//
// Shared fixtures for the permission-boundary journey tests.
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package permission_boundary

import (
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// roleClaims builds an EIAM-style claims map for a session.
func roleClaims(role, username string) map[string]string {
	return map[string]string{"cert_role": role, "username": username}
}

// engineerClaims returns the whitelisted OpsEngineer session claims.
func engineerClaims() map[string]string { return roleClaims("ops_engineer", "ops-engineer") }

// importedCertID seeds an importable world ( done snapshot + reference +
// active account + material adapter ) and returns it together with the
// ledger certificate id the scan-trigger endpoint requires.
func importedCertID(t *testing.T, h *discoverytest.Harness, fp string) string {
	t.Helper()
	snapID := h.SeedDoneSnapshotAt(time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC))
	h.SeedRefs(discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-a", CloudCertID: "cert-A", Fingerprint: fp, ResourceID: "res-1", SnapshotID: snapID})
	cert := h.SeedCert(fp, time.Date(2027, 9, 1, 0, 0, 0, 0, time.UTC))
	return cert.ID.Hex()
}

// boundaryWorld builds a world carrying importable data ( snapshot,
// reference, account, material ) with the given session claims.
func boundaryWorld(t *testing.T, claims map[string]string) (*discoverytest.Harness, *discoverytest.StubCertAdapter, string) {
	t.Helper()
	aliyun := discoverytest.NewStubCertAdapter(domain.CloudAliyun)
	b := certtest.NewBundle(t, "www.boundary-example.com", []string{"www.boundary-example.com"}, nil)
	aliyun.AddMaterial("cert-A", service.DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)})
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) {
		c.Claims = claims
		c.Adapters = []service.DiscoveryCertAdapter{aliyun}
		c.Accounts = map[domain.Cloud][]*sharedomain.CloudAccount{
			domain.CloudAliyun: {discoverytest.ActiveAccount(domain.CloudAliyun, "acct-a")},
		}
	})
	certID := importedCertID(t, h, b.Fingerprint)
	return h, aliyun, certID
}

// unknownSessionID returns a random non-existent session id.
func unknownSessionID() string { return primitive.NewObjectID().Hex() }
