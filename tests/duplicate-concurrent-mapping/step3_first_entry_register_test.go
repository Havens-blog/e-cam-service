// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/duplicate-concurrent-mapping/contracts/step-3-first-entry-register.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package duplicate_concurrent_mapping

import (
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

// TestStep3_FirstEntryRegister_Success verifies the "success" outcome: the
// first item registers the fingerprint and builds its mapping.
func TestStep3_FirstEntryRegister_Success(t *testing.T) {
	h, _, b, _ := multiAccountWorld(t)

	final := importAndAwait(t, h, discoverytest.ImportItem("aliyun", "acct-a", "cert-acc-a"))
	assert.Equal(t, "completed", final["status"])
	item := final["items"].([]any)[0].(map[string]any)
	assert.Equal(t, "success", item["result"])
	mappedCertID, _ := item["mappedCertId"].(string)
	require.NotEmpty(t, mappedCertID)

	ledger := h.Ledger()
	require.Len(t, ledger, 1, "one unique ledger record ( uk_fingerprint )")
	assert.Equal(t, b.Fingerprint, ledger[0].Fingerprint)
	assert.Equal(t, domain.HostingStatusFingerprintOnly, ledger[0].HostingStatus)
	assert.Equal(t, mappedCertID, ledger[0].ID.Hex())

	mappings := h.MappingsByFP(b.Fingerprint)
	require.Len(t, mappings, 1, "one mapping for this cloud+account")
	assert.Equal(t, "acct-a", mappings[0].AccountKey)
	assert.Equal(t, ledger[0].Fingerprint, mappings[0].CertFingerprint,
		"mapping row references the ledger certificate fingerprint ( cross-entity )")
}

// TestStep3_FirstEntryRegister_DuplicateFingerprintContinues verifies the
// "duplicate-fingerprint-continues" outcome: a pre-existing same-fingerprint
// certificate turns the item into mapping-backfill success.
func TestStep3_FirstEntryRegister_DuplicateFingerprintContinues(t *testing.T) {
	h, _, b, _ := multiAccountWorld(t)
	existing := h.SeedCert(b.Fingerprint, time.Date(2027, 11, 1, 0, 0, 0, 0, time.UTC))

	final := importAndAwait(t, h, discoverytest.ImportItem("aliyun", "acct-a", "cert-acc-a"))
	assert.Equal(t, "completed", final["status"], "duplicate fingerprint never degrades the session")
	item := final["items"].([]any)[0].(map[string]any)
	assert.Equal(t, "success", item["result"])
	assert.Equal(t, reasonInLedger, item["errorReason"], "idempotent replay note on the success item")
	assert.Equal(t, existing.ID.Hex(), item["mappedCertId"], "mapping points at the existing record")

	assert.Len(t, h.Ledger(), 1, "no second ledger record for the same fingerprint")
	require.Len(t, h.MappingsByFP(b.Fingerprint), 1, "the mapping row is still built")
}

// TestStep3_FirstEntryRegister_LedgerWriteFailure verifies the
// "ledger-write-failure" outcome: a non-duplicate storage failure records
// the static INTERNAL_ERROR reason, writes nothing, and the session keeps
// processing the next item.
func TestStep3_FirstEntryRegister_LedgerWriteFailure(t *testing.T) {
	aliyun := discoverytest.NewStubCertAdapter(domain.CloudAliyun)
	bFail := certtest.NewBundle(t, "www.dup-writefail.com", []string{"www.dup-writefail.com"}, nil)
	bOK := certtest.NewBundle(t, "www.dup-writeok.com", []string{"www.dup-writeok.com"}, nil)
	aliyun.AddMaterial("cert-fail", service.DiscoveryCertMaterial{Exists: true, CertChainPEM: string(bFail.CertPEM)})
	aliyun.AddMaterial("cert-ok", service.DiscoveryCertMaterial{Exists: true, CertChainPEM: string(bOK.CertPEM)})
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) {
		c.Adapters = []service.DiscoveryCertAdapter{aliyun}
		c.Accounts = map[domain.Cloud][]*sharedomain.CloudAccount{
			domain.CloudAliyun: {discoverytest.ActiveAccount(domain.CloudAliyun, "acct-a")},
		}
		c.WrapCerts = func(base *certtest.FakeCertificateRepo) domain.CertificateRepository {
			return &failFirstCertCreates{FakeCertificateRepo: base, n: 1}
		}
	})

	final := importAndAwait(t, h,
		discoverytest.ImportItem("aliyun", "acct-a", "cert-fail"),
		discoverytest.ImportItem("aliyun", "acct-a", "cert-ok"),
	)
	assert.Equal(t, "partial_failed", final["status"])
	items := final["items"].([]any)
	failed := items[0].(map[string]any)
	assert.Equal(t, "failed", failed["result"])
	assert.Equal(t, reasonLedgerFail, failed["errorReason"], "static INTERNAL_ERROR reason")
	ok := items[1].(map[string]any)
	assert.Equal(t, "success", ok["result"], "the session continues after the storage failure")

	assert.Empty(t, h.MappingsByFP(bFail.Fingerprint), "no mapping for the failed item")
	require.Len(t, h.Ledger(), 1)
	assert.Equal(t, bOK.Fingerprint, h.Ledger()[0].Fingerprint)
}
