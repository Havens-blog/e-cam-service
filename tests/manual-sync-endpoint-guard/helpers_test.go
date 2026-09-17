// @feature cert-volcano-import-sync @api-functional
//
// Journey fixtures for manual-sync-endpoint-guard contract tests: claims
// variants for the role matrix and gated in-flight rounds.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package manual_sync_endpoint_guard

import (
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/require"
)

// Journey account vocabulary.
const (
	AliyunAccount  = "acct-a"
	VolcanoAccount = "acct-v"
)

// newJourneyHarness builds the journey world: listers for aliyun + volcano,
// one active account each, sync dependency wired.
func newJourneyHarness(t *testing.T) *synctest.Harness {
	t.Helper()
	h := synctest.NewHarness(t, func(c *synctest.Config) {
		c.Listers = []domain.Cloud{domain.CloudAliyun, synctest.CloudVolcano}
	})
	h.SeedAccount(domain.CloudAliyun, AliyunAccount)
	h.SeedAccount(synctest.CloudVolcano, VolcanoAccount)
	return h
}

// newHarnessAs builds the journey world with specific claims ( role matrix
// fixtures ) and optionally no sync dependency.
func newHarnessAs(t *testing.T, claims map[string]string, wireSync bool) *synctest.Harness {
	t.Helper()
	h := synctest.NewHarness(t, func(c *synctest.Config) {
		c.Claims = claims
		c.Listers = []domain.Cloud{domain.CloudAliyun, synctest.CloudVolcano}
		c.WireSync = wireSync
	})
	h.SeedAccount(domain.CloudAliyun, AliyunAccount)
	h.SeedAccount(synctest.CloudVolcano, VolcanoAccount)
	return h
}

// seedMaterial registers a parseable chain as import material and returns
// the fingerprint.
func seedMaterial(t *testing.T, h *synctest.Harness, cloud domain.Cloud, cloudCertID, cn string) string {
	t.Helper()
	bundle := certtest.NewBundle(t, cn, []string{cn}, nil)
	h.Material(cloud).AddBundle(cloudCertID, bundle)
	return bundle.Fingerprint
}

// seedInstances replaces a cloud account's listed instance metadata.
func seedInstances(t *testing.T, h *synctest.Harness, cloud domain.Cloud, accountKey string, instances ...service.CertLibraryInstance) {
	t.Helper()
	h.Lister(cloud).SetInstances(accountKey, instances...)
}

// mustSession reads back one import session document.
func mustSession(t *testing.T, h *synctest.Harness, sessionID string) domain.DiscoveryImportSession {
	t.Helper()
	require.NotEmpty(t, sessionID, "session ID must be non-empty for read-back")
	sess, err := h.SessionByID(sessionID)
	require.NoError(t, err)
	return sess
}

// holdRoundInFlight starts a manual round blocked on a gated lister and
// returns the release func plus the async HTTP response channel.
func holdRoundInFlight(t *testing.T, h *synctest.Harness) (release func(), done <-chan *synctest.Response) {
	t.Helper()
	lister := h.Lister(domain.CloudAliyun)
	gate := make(chan struct{})
	lister.SetGate(gate)
	respCh := make(chan *synctest.Response, 1)
	go func() { respCh <- h.PostSync() }()
	synctest.WaitForSignal(t, lister.Entered(), "the manual round to reach the gated lister ( CAS held )")
	return func() { close(gate) }, respCh
}

// errMaterialDown is the injected material-channel failure.
var errMaterialDown = errors.New("injected: material channel unreachable")

// errAccountSourceDown is the injected account-source failure ( cloud-level
// ACCOUNT_LOAD_FAILED fixtures ).
var errAccountSourceDown = errors.New("injected: account source unreachable")
