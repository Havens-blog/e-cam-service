// @feature cert-cloud-discovery-import @api-functional
//
// Journey smoke test: no-snapshot-guidance ( full guidance loop
// NO_SNAPSHOT -> trigger scan -> poll running -> done -> preview -> import ).
// Source journey:
// docs/features/cert-cloud-discovery-import/testing/no-snapshot-guidance/journey.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package no_snapshot_guidance

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

// TestNoSnapshotGuidance_Smoke walks the guidance journey end to end.
func TestNoSnapshotGuidance_Smoke(t *testing.T) {
	aliyun := discoverytest.NewStubCertAdapter(domain.CloudAliyun)
	b := certtest.NewBundle(t, "www.guidance-smoke.com", []string{"www.guidance-smoke.com"}, nil)
	aliyun.AddMaterial("cert-A", service.DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)})
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) {
		c.Adapters = []service.DiscoveryCertAdapter{aliyun}
		c.Accounts = map[domain.Cloud][]*sharedomain.CloudAccount{
			domain.CloudAliyun: {discoverytest.ActiveAccount(domain.CloudAliyun, "acct-a")},
		}
	})
	cert := h.SeedCert(discoverytest.FP("guidance-smoke-cert"), time.Date(2027, 12, 1, 0, 0, 0, 0, time.UTC))

	// Step 1: preview on a fresh system returns the NO_SNAPSHOT guidance.
	preview := h.Get(discoverytest.RoutePreview)
	require.Equal(t, http.StatusConflict, preview.StatusCode, preview.Body)
	assert.Equal(t, "NO_SNAPSHOT", preview.Env.Error.Code)
	emptyState := h.Get(discoverytest.RouteSnapshotStatus).DataMap(t)
	assert.Equal(t, false, emptyState["hasSnapshot"])
	assert.Empty(t, emptyState["partialFailures"], "empty state carries no partial failure rows")

	// Step 2: follow the guidance and trigger the scan.
	h.Scan.SetRefsToWrite([]discoverytest.RefSpec{
		{Cloud: domain.CloudAliyun, AccountKey: "acct-a", CloudCertID: "cert-A", Fingerprint: b.Fingerprint, ResourceID: "res-1"},
	})
	triggered := h.Post(scanRoute(cert.ID.Hex()), nil)
	require.Equal(t, http.StatusOK, triggered.StatusCode, triggered.Body)
	tData := triggered.DataMap(t)
	snapshotID, _ := tData["snapshotId"].(string)
	require.NotEmpty(t, snapshotID)
	assert.Equal(t, "running", tData["status"])
	assert.Equal(t, float64(1), tData["referencesWritten"], "the scan landed its reference")

	// Step 3: poll while running.
	poll := h.Get(discoverytest.RouteSnapshotStatus).DataMap(t)
	assert.Equal(t, "running", poll["status"])
	assert.Equal(t, snapshotID, poll["snapshotId"])

	// Step 4: scan converges to done; the preview loop closes.
	h.FinishScanDone(snapshotID)
	done := h.Get(discoverytest.RouteSnapshotStatus).DataMap(t)
	assert.Equal(t, "done", done["status"])

	list := h.Get(discoverytest.RoutePreview)
	require.Equal(t, http.StatusOK, list.StatusCode, list.Body)
	assert.Equal(t, float64(1), list.DataMap(t)["count"])
	assert.Equal(t, "cert-A", list.PreviewItems(t)[0].(map[string]any)["cloudCertId"])

	// The loop hands over into the first-ledger-import flow.
	imp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(
		discoverytest.ImportItem("aliyun", "acct-a", "cert-A"),
	))
	require.Equal(t, http.StatusAccepted, imp.StatusCode, imp.Body)
	final := h.PollImportTerminal(imp.DataMap(t)["sessionId"].(string))
	assert.Equal(t, "completed", final["status"])

	// Error path through the journey: anonymous caller is rejected at the
	// auth gate before any guidance logic.
	anonH := discoverytest.NewHarness(t, func(c *discoverytest.Config) { c.Claims = nil })
	anon := anonH.Get(discoverytest.RoutePreview)
	assert.Equal(t, http.StatusUnauthorized, anon.StatusCode, anon.Body)
}
