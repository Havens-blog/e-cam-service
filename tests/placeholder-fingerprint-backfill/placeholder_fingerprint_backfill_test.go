// @feature cert-cloud-discovery-import @api-functional
//
// Shared fixtures for the placeholder-fingerprint-backfill journey tests.
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
	"github.com/stretchr/testify/require"
)

// Static per-item reasons ( fact IMPORT_ITEM_ERROR_REASONS ).
const (
	reasonNoPEM    = "CERT_GET_FAILED: 云侧未返回可导入的证书材料"
	reasonInLedger = "ALREADY_IN_LEDGER: 已在台账，已补建映射"
)

// tencentWorld builds a tencent-backed world ( the SHA-1 placeholder
// fallback cloud in the acceptance samples ) with one active account.
func tencentWorld(t *testing.T, accounts ...string) (*discoverytest.Harness, *discoverytest.StubCertAdapter, string) {
	t.Helper()
	tencent := discoverytest.NewStubCertAdapter(domain.CloudTencent)
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) {
		c.Adapters = []service.DiscoveryCertAdapter{tencent}
		accs := make([]*sharedomain.CloudAccount, 0, len(accounts))
		for _, name := range accounts {
			accs = append(accs, discoverytest.ActiveAccount(domain.CloudTencent, name))
		}
		c.Accounts = map[domain.Cloud][]*sharedomain.CloudAccount{domain.CloudTencent: accs}
	})
	snapID := h.SeedDoneSnapshotAt(time.Date(2026, 8, 24, 15, 0, 0, 0, time.UTC))
	return h, tencent, snapID
}

// goodMaterial registers an existing in-cloud certificate bundle.
func goodMaterial(b *certtest.CertBundle) service.DiscoveryCertMaterial {
	return service.DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)}
}

// placeholderRefSpec builds a placeholder-fingerprint reference spec.
func placeholderRefSpec(cloud, accountKey, cloudCertID, resourceID, snapID string) discoverytest.RefSpec {
	return discoverytest.RefSpec{
		Cloud:       domain.Cloud(cloud),
		AccountKey:  accountKey,
		CloudCertID: cloudCertID,
		Fingerprint: discoverytest.PlaceholderFP(cloud, accountKey, cloudCertID),
		ResourceID:  resourceID,
		SnapshotID:  snapID,
	}
}

// importOne submits a single-item import and returns the terminal payload.
func importOne(t *testing.T, h *discoverytest.Harness, cloud, accountKey, cloudCertID string) map[string]any {
	t.Helper()
	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem(cloud, accountKey, cloudCertID),
	))
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	return h.PollImportTerminal(resp.DataMap(t)["sessionId"].(string))
}
