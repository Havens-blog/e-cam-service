// @feature cert-volcano-import-sync @api-functional
//
// Journey fixtures for first-sync-backfill contract tests. Every helper
// builds the preconditions declared by the corresponding Contract's
// fixture_spec ( CloudAccount / CertLibraryInstance / Certificate /
// CloudCertMapping / DiscoveryImportSession entities, carried by the harness
// stubs and in-memory fakes ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package first_sync_backfill

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/require"
)

// Journey account vocabulary ( one active account per listed cloud ).
const (
	AliyunAccount  = "acct-a"
	VolcanoAccount = "acct-v"
)

// newJourneyHarness builds the default journey world: cert-library listers
// registered for aliyun + volcano ( the two clouds with fixtures in this
// journey ), the remaining four clouds stay capability-gap skipped.
func newJourneyHarness(t *testing.T) *synctest.Harness {
	t.Helper()
	h := synctest.NewHarness(t, func(c *synctest.Config) {
		c.Listers = []domain.Cloud{domain.CloudAliyun, synctest.CloudVolcano}
	})
	h.SeedAccount(domain.CloudAliyun, AliyunAccount)
	h.SeedAccount(synctest.CloudVolcano, VolcanoAccount)
	return h
}

// seedBundle generates a parseable certificate chain, registers it as a
// cloud's import material for one cloudCertID, and returns the bundle
// ( fingerprint available as bundle.Fingerprint; registering the same bundle
// on several clouds realizes "identical cloud-side content" fixtures ).
func seedBundle(t *testing.T, h *synctest.Harness, cloud domain.Cloud, cloudCertID, cn string) *certtest.CertBundle {
	t.Helper()
	bundle := certtest.NewBundle(t, cn, []string{cn}, nil)
	h.Material(cloud).AddBundle(cloudCertID, bundle)
	return bundle
}

// seedMaterial registers a parseable certificate chain as a cloud's import
// material for one cloudCertID and returns its ledger fingerprint.
func seedMaterial(t *testing.T, h *synctest.Harness, cloud domain.Cloud, cloudCertID, cn string) string {
	t.Helper()
	return seedBundle(t, h, cloud, cloudCertID, cn).Fingerprint
}

// seedInstances replaces a cloud account's listed instance metadata.
func seedInstances(t *testing.T, h *synctest.Harness, cloud domain.Cloud, accountKey string, instances ...service.CertLibraryInstance) {
	t.Helper()
	h.Lister(cloud).SetInstances(accountKey, instances...)
}

// mustSession reads back one import session document, failing when the ID is
// empty or unknown ( shared deep-assertion helper ).
func mustSession(t *testing.T, h *synctest.Harness, sessionID string) domain.DiscoveryImportSession {
	t.Helper()
	require.NotEmpty(t, sessionID, "session ID must be non-empty for read-back")
	sess, err := h.SessionByID(sessionID)
	require.NoError(t, err)
	return sess
}
