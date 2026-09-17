// @feature cert-volcano-import-sync @api-functional
//
// Journey fixtures for manual-scheduler-race contract tests.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package manual_scheduler_race

import (
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
// one active account each.
func newJourneyHarness(t *testing.T) *synctest.Harness {
	t.Helper()
	h := synctest.NewHarness(t, func(c *synctest.Config) {
		c.Listers = []domain.Cloud{domain.CloudAliyun, synctest.CloudVolcano}
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
// returns the release func plus the async HTTP response channel ( the CAS is
// provably held once the lister signal fires ).
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

// seedSameFingerprintOnBothClouds registers one shared chain as both clouds'
// material and lists the same fingerprint from both accounts ( cross-cloud
// duplicate absorption fixtures ).
func seedSameFingerprintOnBothClouds(t *testing.T, h *synctest.Harness, cn string) string {
	t.Helper()
	bundle := certtest.NewBundle(t, cn, []string{cn}, nil)
	h.Material(domain.CloudAliyun).AddBundle("cert-a1", bundle)
	h.Material(synctest.CloudVolcano).AddBundle("cert-v1", bundle)
	seedInstances(t, h, domain.CloudAliyun, AliyunAccount, synctest.Instance("cert-a1", bundle.Fingerprint))
	seedInstances(t, h, synctest.CloudVolcano, VolcanoAccount, synctest.Instance("cert-v1", bundle.Fingerprint))
	return bundle.Fingerprint
}
