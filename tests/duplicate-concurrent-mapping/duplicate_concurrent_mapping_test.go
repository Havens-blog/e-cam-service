// @feature cert-cloud-discovery-import @api-functional
//
// Shared fixtures for the duplicate-concurrent-mapping journey tests.
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package duplicate_concurrent_mapping

import (
	"context"
	"errors"
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

// Static reasons ( fact IMPORT_ITEM_ERROR_REASONS ).
const (
	reasonInLedger   = "ALREADY_IN_LEDGER: 已在台账，已补建映射"
	reasonLedgerFail = "INTERNAL_ERROR: 台账写入失败"
	reasonNoAccount  = "ACCOUNT_NOT_FOUND: 云账号不存在或未启用"
)

// errLedgerWriteUnavailable simulates a non-duplicate storage failure.
var errLedgerWriteUnavailable = errors.New("ledger storage unavailable")

// multiAccountWorld builds the journey setup: a done snapshot whose
// references point two aliyun accounts at the same certificate, with the
// aliyun adapter serving one shared bundle under both cloud cert ids.
func multiAccountWorld(t *testing.T) (*discoverytest.Harness, *discoverytest.StubCertAdapter, *certtest.CertBundle, string) {
	t.Helper()
	aliyun := discoverytest.NewStubCertAdapter(domain.CloudAliyun)
	b := certtest.NewBundle(t, "www.dup-example.com", []string{"www.dup-example.com"}, nil)
	aliyun.AddMaterial("cert-acc-a", service.DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)})
	aliyun.AddMaterial("cert-acc-b", service.DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)})
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) {
		c.Adapters = []service.DiscoveryCertAdapter{aliyun}
		c.Accounts = map[domain.Cloud][]*sharedomain.CloudAccount{
			domain.CloudAliyun: {
				discoverytest.ActiveAccount(domain.CloudAliyun, "acct-a"),
				discoverytest.ActiveAccount(domain.CloudAliyun, "acct-b"),
			},
		}
	})
	snapID := h.SeedDoneSnapshotAt(time.Date(2026, 8, 24, 17, 0, 0, 0, time.UTC))
	h.SeedRefs(
		discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-a", CloudCertID: "cert-acc-a", Fingerprint: b.Fingerprint, ResourceID: "res-a", SnapshotID: snapID},
		discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-b", CloudCertID: "cert-acc-b", Fingerprint: b.Fingerprint, ResourceID: "res-b", SnapshotID: snapID},
	)
	return h, aliyun, b, snapID
}

// importAndAwait submits items and returns the terminal payload.
func importAndAwait(t *testing.T, h *discoverytest.Harness, items ...map[string]string) map[string]any {
	t.Helper()
	resp := h.Post(discoverytest.RouteImport, discoverytest.ImportBody(items...))
	require.Equal(t, http.StatusAccepted, resp.StatusCode, resp.Body)
	return h.PollImportTerminal(resp.DataMap(t)["sessionId"].(string))
}

// failFirstCertCreates wraps the certificate repository failing the first
// n Create calls with a non-duplicate storage error ( ledger-write-failure
// injection; later calls delegate to the in-memory fake ).
type failFirstCertCreates struct {
	*certtest.FakeCertificateRepo
	n int
}

// Create fails the first n calls, then delegates.
func (f *failFirstCertCreates) Create(ctx context.Context, cert *domain.Certificate) error {
	if f.n > 0 {
		f.n--
		return errLedgerWriteUnavailable
	}
	return f.FakeCertificateRepo.Create(ctx, cert)
}
