// @feature cert-volcano-import-sync @api-functional
//
// Journey fixtures for incremental-skip-drift contract tests: mapped-skip,
// backfill, and re-sign drift worlds over the harness stubs.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package incremental_skip_drift

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/tests/synctest"
	"github.com/stretchr/testify/require"
)

// Journey account vocabulary.
const (
	VolcanoAccount = "acct-v"
	AliyunAccount  = "acct-a"
)

// newJourneyHarness builds the journey world: listers for volcano + aliyun,
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

// seedMappedInstance seeds the "already synced" precondition: ledger
// certificate + complete mapping + listed instance with the same fingerprint.
func seedMappedInstance(t *testing.T, h *synctest.Harness, cloud domain.Cloud, accountKey, cloudCertID string) string {
	t.Helper()
	fp := synctest.FP("mapped:" + cloudCertID)
	h.SeedLedgerCert(fp)
	h.SeedMapping(fp, string(cloud), accountKey, cloudCertID)
	h.Lister(cloud).SetInstances(accountKey, synctest.Instance(cloudCertID, fp))
	return fp
}

// seedDriftPrecondition seeds the "re-signed instance" precondition: the old
// fingerprint is in the ledger with an hour-old mapping for the cloudCertID.
func seedDriftPrecondition(t *testing.T, h *synctest.Harness, cloud domain.Cloud, accountKey, cloudCertID, oldFP string) {
	t.Helper()
	h.SeedLedgerCert(oldFP)
	h.SeedMapping(oldFP, string(cloud), accountKey, cloudCertID, time.Now().Add(-time.Hour))
}

// failUpsertMappings injects Upsert failures into the mapping repository
// while delegating every other method to the base in-memory fake ( read-back
// assertions stay on the harness base fake ). The fault is toggleable so
// rerun-convergence fixtures can release it mid-test.
type failUpsertMappings struct {
	domain.CloudCertMappingRepository
	fail atomic.Bool
}

// Upsert fails while the fault is armed, otherwise delegates.
func (f *failUpsertMappings) Upsert(ctx context.Context, m *domain.CloudCertMapping) error {
	if f.fail.Load() {
		return errUpsertInjected
	}
	return f.CloudCertMappingRepository.Upsert(ctx, m)
}

// newJourneyHarnessWithMappingFault builds the journey world with a
// toggleable mapping-write fault installed; returns the toggle handle.
func newJourneyHarnessWithMappingFault(t *testing.T) (*synctest.Harness, *failUpsertMappings) {
	t.Helper()
	fault := &failUpsertMappings{}
	h := synctest.NewHarness(t, func(c *synctest.Config) {
		c.Listers = []domain.Cloud{domain.CloudAliyun, synctest.CloudVolcano}
		c.WrapMappings = func(base *certtest.FakeCloudCertMappingRepo) domain.CloudCertMappingRepository {
			fault.CloudCertMappingRepository = base
			return fault
		}
	})
	h.SeedAccount(domain.CloudAliyun, AliyunAccount)
	h.SeedAccount(synctest.CloudVolcano, VolcanoAccount)
	return h, fault
}

// mustSession reads back one import session document.
func mustSession(t *testing.T, h *synctest.Harness, sessionID string) domain.DiscoveryImportSession {
	t.Helper()
	require.NotEmpty(t, sessionID, "session ID must be non-empty for read-back")
	sess, err := h.SessionByID(sessionID)
	require.NoError(t, err)
	return sess
}

// mustMappingFP resolves the newest mapping fingerprint for a triple.
func mustMappingFP(t *testing.T, h *synctest.Harness, cloud, accountKey, cloudCertID string) string {
	t.Helper()
	m, err := h.LatestMappingByCloudCert(cloud, accountKey, cloudCertID)
	require.NoError(t, err)
	return m.CertFingerprint
}

var errUpsertInjected = errors.New("injected mapping write failure")
