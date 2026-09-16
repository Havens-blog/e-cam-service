// @feature cert-multicloud-deployers @api-functional
//
// Journey fixtures for three-cloud-product-matrix contract tests.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package three_cloud_product_matrix

import (
	"strings"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
)

// matrixCombos enumerates the nine registered cloud×product combinations.
func matrixCombos() []struct {
	Cloud      domain.Cloud
	Product    domain.Product
	AccountKey string
	ResourceID string
	OldID      string
} {
	return []struct {
		Cloud      domain.Cloud
		Product    domain.Product
		AccountKey string
		ResourceID string
		OldID      string
	}{
		{domain.CloudHuawei, domain.ProductCDN, "acct-hw-1", "www.example.com", "old-hw-cdn"},
		{domain.CloudHuawei, domain.ProductWAF, "acct-hw-2", "waf.example.com", "old-hw-waf"},
		{domain.CloudHuawei, domain.ProductALB, "acct-hw-3", "hw-elb-1/listener-1", "old-hw-alb"},
		{domain.CloudHuawei, domain.ProductNLB, "acct-hw-4", "hw-elb-2/listener-2", "old-hw-nlb"},
		{domain.CloudAWS, domain.ProductCDN, "acct-aws-1", "distribution-1", "arn:aws:acm:us-east-1:123456789012:certificate/old-cdn"},
		{domain.CloudAWS, domain.ProductALB, "acct-aws-2", "arn:aws:elasticloadbalancing:us-east-1:123456789012:listener/alb-1", "arn:aws:acm:us-east-1:123456789012:certificate/old-alb"},
		{domain.CloudAWS, domain.ProductNLB, "acct-aws-3", "arn:aws:elasticloadbalancing:us-east-1:123456789012:listener/nlb-1", "arn:aws:acm:us-east-1:123456789012:certificate/old-nlb"},
		{domain.CloudAzure, domain.ProductCDN, "acct-az-1", "frontdoor-1/endpoint-1", "https://vault-test.vault.azure.net/secrets/old-fd/1"},
		{domain.CloudAzure, domain.ProductALB, "acct-az-2", "appgw-1/listener-1", "https://vault-test.vault.azure.net/secrets/old-appgw/1"},
	}
}

// seedMatrixWorld seeds the shared world: complete old/new certificates, one
// fresh done snapshot with references for the given combos, and valid in-cloud
// old certs on every stub.
func seedMatrixWorld(t *testing.T, h *multicloudtest.Harness, combos []struct {
	Cloud      domain.Cloud
	Product    domain.Product
	AccountKey string
	ResourceID string
	OldID      string
}) (string, string, string) {
	t.Helper()
	_, oldFP := h.SeedCompleteCert("old.example.com", "www.example.com")
	newCertID, newFP := h.SeedCompleteCert("new.example.com", "www.example.com")

	refs := make([]multicloudtest.RefSpec, 0, len(combos))
	for _, c := range combos {
		h.OldCertCloudID(string(c.Cloud), c.OldID, oldFP)
		refs = append(refs, multicloudtest.RefSpec{
			Cloud: c.Cloud, Product: c.Product, AccountKey: c.AccountKey,
			ResourceID: c.ResourceID, CloudCertID: c.OldID, Fingerprint: oldFP,
		})
	}
	h.SeedDoneSnapshotWithRefs(refs)
	return oldFP, newCertID, newFP
}

// seedMatrixItems seeds an executing single-batch order with one pending item
// per combo ( distinct account keys keep the mapping keys unique ) and returns
// the order ID.
func seedMatrixItems(t *testing.T, h *multicloudtest.Harness, oldFP, newCertID string, combos []struct {
	Cloud      domain.Cloud
	Product    domain.Product
	AccountKey string
	ResourceID string
	OldID      string
}) []string {
	t.Helper()
	orderID := h.SeedOrder(domain.ChangeStatusExecuting,
		&domain.BatchInfo{TotalBatches: 1, CurrentBatch: 1, BatchSize: len(combos), Paused: false},
		nil, oldFP, newCertID)
	ids := make([]string, 0, len(combos))
	for _, c := range combos {
		ref := domain.ResourceRef{Channel: domain.ChannelCloudAPI, Cloud: string(c.Cloud),
			Product: string(c.Product), AccountKey: c.AccountKey, ResourceID: c.ResourceID}
		ids = append(ids, h.SeedItem(orderID, ref, domain.ItemStatusPending, 1, c.OldID))
	}
	return ids
}

// assertCloudCertIDForm verifies the per-cloud ID form is mutually exclusive:
// huawei = SCM UUID, aws = us-east-1 ACM ARN, azure = versioned KV secret ID.
func assertCloudCertIDForm(t *testing.T, cloud, cloudCertID string) {
	t.Helper()
	switch cloud {
	case "huawei":
		assert.NotContains(t, cloudCertID, "arn:aws:acm", "huawei ID is not an ACM ARN")
		assert.NotContains(t, cloudCertID, "vault.azure.net", "huawei ID is not a KV reference")
		assert.Contains(t, cloudCertID, "-", "SCM ID carries UUID-form segments")
	case "aws":
		assert.True(t, strings.HasPrefix(cloudCertID, "arn:aws:acm:us-east-1:"),
			"aws ID is a us-east-1 ACM ARN, got %q", cloudCertID)
	case "azure":
		assert.True(t, strings.HasPrefix(cloudCertID, "https://") &&
			strings.Contains(cloudCertID, ".vault.azure.net/secrets/"),
			"azure ID is a KV secret ID reference, got %q", cloudCertID)
	}
}
