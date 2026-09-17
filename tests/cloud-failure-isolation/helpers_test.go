// @feature cert-volcano-import-sync @api-functional
//
// Journey fixtures for cloud-failure-isolation contract tests: cloud x
// account failure matrix over the harness stubs.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package cloud_failure_isolation

import (
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/require"
)

// Journey account vocabulary ( aliyun hosts two accounts for the matrix ).
const (
	AliyunAccount  = "acct-a"
	AliyunAccount2 = "acct-a2"
	VolcanoAccount = "acct-v"
)

// newJourneyHarness builds the journey world: listers for aliyun + volcano,
// three active accounts.
func newJourneyHarness(t *testing.T) *synctest.Harness {
	t.Helper()
	h := synctest.NewHarness(t, func(c *synctest.Config) {
		c.Listers = []domain.Cloud{domain.CloudAliyun, synctest.CloudVolcano}
	})
	h.SeedAccount(domain.CloudAliyun, AliyunAccount)
	h.SeedAccount(domain.CloudAliyun, AliyunAccount2)
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

// failureIndex locates a failure entry by (cloud, accountKey) in the summary.
func failureIndex(run synctest.SyncRunPayload, cloud, accountKey string) int {
	for i, f := range run.Failures {
		if f.Cloud == cloud && f.AccountKey == accountKey {
			return i
		}
	}
	return -1
}

// errAccountSourceDown is the injected account-source failure ( cloud-level
// ACCOUNT_LOAD_FAILED fixtures ).
var errAccountSourceDown = errors.New("injected: account source unreachable")
