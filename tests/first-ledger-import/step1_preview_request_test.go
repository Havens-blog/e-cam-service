// @feature cert-cloud-discovery-import @api-functional
//
// Contract: docs/features/cert-cloud-discovery-import/testing/first-ledger-import/contracts/step-1-preview-request.md
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with extra scrutiny.

package first_ledger_import

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/internal/cert/web"
	"github.com/Havens-blog/e-cam-service/tests/discoverytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStep1_PreviewRequest_Success verifies the "success" outcome: a done
// snapshot with multi-cloud parseable references aggregates into the unique
// certificate list ( triple dedup, crd/empty-cloud exclusion ) with no cloud
// API call and sub-second response.
func TestStep1_PreviewRequest_Success(t *testing.T) {
	aliyun := discoverytest.NewStubCertAdapter(domain.CloudAliyun) // cloud-call accounting only
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) {
		*c = discoverytest.DefaultConfig()
		c.Adapters = []service.DiscoveryCertAdapter{aliyun}
	})

	started := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	snapID := h.SeedDoneSnapshotAt(started)
	fpA, fpT := discoverytest.FP("step1-a"), discoverytest.FP("step1-t")
	h.SeedRefs(
		// Two resources on the same triple -> one deduped entry, refCount=2.
		discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-main", CloudCertID: "cert-A", Fingerprint: fpA, ResourceID: "cdn-res-1", SnapshotID: snapID},
		discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-main", CloudCertID: "cert-A", Fingerprint: fpA, ResourceID: "cdn-res-2", SnapshotID: snapID},
		discoverytest.RefSpec{Cloud: domain.CloudTencent, AccountKey: "acct-tx", CloudCertID: "ssl-8", Fingerprint: fpT, ResourceID: "waf-res-8", SnapshotID: snapID},
		// Excluded from the preview: crd product and empty cloud.
		discoverytest.RefSpec{Cloud: domain.CloudAliyun, Product: domain.ProductCRD, AccountKey: "acct-main", CloudCertID: "crd-cert", Fingerprint: discoverytest.FP("step1-crd"), ResourceID: "crd-1", SnapshotID: snapID},
		discoverytest.RefSpec{Cloud: "", AccountKey: "acct-main", CloudCertID: "cert-empty", Fingerprint: discoverytest.FP("step1-empty"), ResourceID: "res-empty", SnapshotID: snapID},
	)

	begin := time.Now()
	resp := h.Get(discoverytest.RoutePreview)
	elapsed := time.Since(begin)
	resp.RequireJSON(t)
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	require.Less(t, elapsed, discoverytest.PreviewResponseBudget, "preview must stay under 1s (pure DB aggregation)")
	assert.True(t, resp.Env.Success)

	data := resp.DataMap(t)
	assert.Equal(t, snapID, data["snapshotId"], "preview is based on the latest done snapshot")
	assert.Equal(t, started.UTC().Format(time.RFC3339), data["snapshotStartedAt"])
	assert.Equal(t, float64(2), data["count"], "crd and empty-cloud references are excluded")

	items := resp.PreviewItems(t)
	require.Len(t, items, 2)
	assert.Equal(t, "aliyun", items[0].(map[string]any)["cloud"], "entries are sorted cloud -> accountKey -> cloudCertId")
	assert.Equal(t, "tencent", items[1].(map[string]any)["cloud"])

	first := discoverytest.FindPreviewItem(t, items, "aliyun", "acct-main", "cert-A")
	assert.Equal(t, float64(2), first["refCount"], "two resources on one triple dedup into one entry with refCount=2")
	assert.Equal(t, false, first["inLedger"], "ledger is empty so no entry is in ledger")
	assert.Equal(t, true, first["parseable"])
	assert.Equal(t, web.DiscoveryNotAfterPending, first["notAfter"], "unregistered entry carries the pending placeholder")

	discoverytest.RequireNoKeyMaterial(t, resp.Body)
	assert.Equal(t, int32(0), aliyun.Calls(), "preview is pure DB aggregation: no cloud API call")
}

// TestStep1_PreviewRequest_NoSnapshot verifies the "no-snapshot" outcome:
// without any done snapshot the preview returns the structured NO_SNAPSHOT
// conflict, never a 500.
func TestStep1_PreviewRequest_NoSnapshot(t *testing.T) {
	h := discoverytest.NewHarness(t, nil) // fresh world: no done snapshot
	h.SeedRunningSnapshotAt(time.Date(2026, 8, 24, 8, 0, 0, 0, time.UTC))

	resp := h.Get(discoverytest.RoutePreview)
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusConflict, resp.StatusCode, resp.Body)
	assert.False(t, resp.Env.Success)
	require.NotNil(t, resp.Env.Error)
	assert.Equal(t, "NO_SNAPSHOT", resp.Env.Error.Code)
	assert.NotEmpty(t, resp.Env.Error.Message)
	assert.Nil(t, resp.Env.Data, "no business data on structured error")
	discoverytest.RequireNoKeyMaterial(t, resp.Body)
}

// TestStep1_PreviewRequest_Unauthorized verifies the "unauthorized" outcome:
// an unauthenticated request gets 401 semantics and never reaches business
// logic.
func TestStep1_PreviewRequest_Unauthorized(t *testing.T) {
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) { c.Claims = nil })
	resp := h.Get(discoverytest.RoutePreview)
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.False(t, resp.Env.Success)
	assert.Empty(t, resp.Env.Data, "no business data leaks on auth failure")
	discoverytest.RequireNoKeyMaterial(t, resp.Body)
}

// mappingLookupFailure injects a CloudCertMapping read failure ( preview
// dual-channel lookup ) over the base fake.
type mappingLookupFailure struct {
	*certtest.FakeCloudCertMappingRepo
}

func (f *mappingLookupFailure) FindByCloudCertID(context.Context, string, string, string) (domain.CloudCertMapping, error) {
	return domain.CloudCertMapping{}, errors.New("mapping storage unavailable")
}

// TestStep1_PreviewRequest_InternalRepoError verifies the
// "internal-repo-error" outcome: a mapping repository failure surfaces as
// 500 INTERNAL_ERROR with a fixed safe message.
func TestStep1_PreviewRequest_InternalRepoError(t *testing.T) {
	h := discoverytest.NewHarness(t, func(c *discoverytest.Config) {
		c.WrapMappings = func(base *certtest.FakeCloudCertMappingRepo) domain.CloudCertMappingRepository {
			return &mappingLookupFailure{FakeCloudCertMappingRepo: base}
		}
	})
	snapID := h.SeedDoneSnapshotAt(time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC))
	h.SeedRefs(discoverytest.RefSpec{Cloud: domain.CloudAliyun, AccountKey: "acct-main", CloudCertID: "cert-A", Fingerprint: discoverytest.FP("step1-err"), ResourceID: "res-1", SnapshotID: snapID})

	resp := h.Get(discoverytest.RoutePreview)
	resp.RequireJSON(t)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode, resp.Body)
	assert.False(t, resp.Env.Success)
	require.NotNil(t, resp.Env.Error)
	assert.Equal(t, "INTERNAL_ERROR", resp.Env.Error.Code)
	assert.Equal(t, "internal server error", resp.Env.Error.Message, "fixed safe message, no repo details")
	assert.NotContains(t, resp.Body, "mapping storage unavailable")
}
