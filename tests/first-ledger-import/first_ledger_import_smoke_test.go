// @feature cert-cloud-discovery-import @api-functional
//
// Journey smoke test: first-ledger-import ( happy path through all six
// steps + one error path ). Source journey:
// docs/features/cert-cloud-discovery-import/testing/first-ledger-import/journey.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package first_ledger_import

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

// TestFirstLedgerImport_Smoke walks the golden path end to end: preview on an
// empty ledger -> confirm import -> async per-item processing -> poll to
// terminal convergence -> refreshed preview shows the registered state.
func TestFirstLedgerImport_Smoke(t *testing.T) {
	aliyun := discoverytest.NewStubCertAdapter(domain.CloudAliyun)
	tencent := discoverytest.NewStubCertAdapter(domain.CloudTencent)
	bA := certtest.NewBundle(t, "www.smoke-aliyun.com", []string{"www.smoke-aliyun.com"}, nil)
	bT := certtest.NewBundle(t, "www.smoke-tencent.com", []string{"www.smoke-tencent.com"}, nil)
	aliyun.AddMaterial("cert-A", service.DiscoveryCertMaterial{Exists: true, CertChainPEM: string(bA.CertPEM)})
	tencent.AddMaterial("ssl-T", service.DiscoveryCertMaterial{Exists: true, CertChainPEM: string(bT.CertPEM)})

	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) {
		c.Adapters = []service.DiscoveryCertAdapter{aliyun, tencent}
		c.Accounts = map[domain.Cloud][]*sharedomain.CloudAccount{
			domain.CloudAliyun:  {discoverytest.ActiveAccount(domain.CloudAliyun, "acct-a")},
			domain.CloudTencent: {discoverytest.ActiveAccount(domain.CloudTencent, "acct-t")},
		}
	})

	// Step 1-2: preview over the latest done snapshot on an empty ledger.
	started := time.Date(2026, 8, 23, 8, 0, 0, 0, time.UTC)
	snapID := h.SeedDoneSnapshotAt(started)
	h.SeedRefs(
		discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-a", CloudCertID: "cert-A", Fingerprint: bA.Fingerprint, ResourceID: "res-a1", SnapshotID: snapID},
		discoverytest.RefSpec{Cloud: domain.CloudTencent, AccountKey: "acct-t", CloudCertID: "ssl-T", Fingerprint: bT.Fingerprint, ResourceID: "res-t1", SnapshotID: snapID},
	)
	preview := h.Get(discoverytest.RoutePreview)
	require.Equal(t, http.StatusOK, preview.StatusCode, preview.Body)
	pData := preview.DataMap(t)
	assert.Equal(t, snapID, pData["snapshotId"])
	pItems := pData["items"].([]any)
	require.Len(t, pItems, 2)
	assert.Equal(t, false, discoverytest.FindPreviewItem(t, pItems, "aliyun", "acct-a", "cert-A")["inLedger"])
	assert.Equal(t, false, discoverytest.FindPreviewItem(t, pItems, "tencent", "acct-t", "ssl-T")["inLedger"])

	// Step 3: confirm the import of both unregistered entries.
	created := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-A"),
		discoverytest.ImportItem("tencent", "acct-t", "ssl-T"),
	))
	require.Equal(t, http.StatusAccepted, created.StatusCode, created.Body)
	sessionID, _ := created.DataMap(t)["sessionId"].(string)
	require.NotEmpty(t, sessionID)

	// Step 4-5: async processing converges; polling reaches the terminal state.
	final := h.PollImportTerminal(sessionID)
	assert.Equal(t, "completed", final["status"])
	progress := final["progress"].(map[string]any)
	assert.Equal(t, float64(2), progress["succeeded"])
	assert.Equal(t, float64(0), progress["failed"])

	// Step 6: converged ledger/mapping state, visible on a refreshed preview.
	ledger := h.Ledger()
	require.Len(t, ledger, 2)
	for _, c := range ledger {
		assert.Equal(t, domain.HostingStatusFingerprintOnly, c.HostingStatus)
		assert.NotContains(t, c.CertPEM, "PRIVATE KEY")
	}
	assert.Len(t, h.MappingsByFP(bA.Fingerprint), 1)
	assert.Len(t, h.MappingsByFP(bT.Fingerprint), 1)

	refreshed := h.Get(discoverytest.RoutePreview)
	require.Equal(t, http.StatusOK, refreshed.StatusCode)
	rItems := refreshed.PreviewItems(t)
	assert.Equal(t, true, discoverytest.FindPreviewItem(t, rItems, "aliyun", "acct-a", "cert-A")["inLedger"],
		"registered entry now shows inLedger")
	assert.Equal(t, leafNotAfter(t, bA.CertPEM).UTC().Format(time.RFC3339),
		discoverytest.FindPreviewItem(t, rItems, "aliyun", "acct-a", "cert-A")["notAfter"],
		"inLedger entry shows the ledger NotAfter")

	// Error path through the journey: malformed confirm payload.
	bad := h.Post(discoverytest.RouteImport, discoverytest.ImportBody())
	assert.Equal(t, http.StatusBadRequest, bad.StatusCode, bad.Body)
	require.NotNil(t, bad.Env.Error)
	assert.Equal(t, "INVALID_REQUEST", bad.Env.Error.Code)
}
