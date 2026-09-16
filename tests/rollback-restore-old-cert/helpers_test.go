// @feature cert-multicloud-deployers @api-functional
//
// Journey fixtures for rollback-restore-old-cert contract tests.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package rollback_restore_old_cert

import (
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
)

// rollbackWorld bundles the fixture entities of one rollback scenario.
type rollbackWorld struct {
	OldFP      string
	NewFP      string
	NewCertID  string
	NewCloudID string
	Domain     string
	OldCloudID string
	AccountKey string
	ResourceID string
	OldCertID  string
}

// seedRollbackWorld seeds the replacement-done state: complete old/new
// certificates over one shared SAN and the old cloud cert still valid on the
// cloud side ( rollback target ).
func seedRollbackWorld(t *testing.T, h *multicloudtest.Harness, cloud domain.Cloud, product domain.Product) rollbackWorld {
	t.Helper()
	w := rollbackWorld{
		Domain:     "www.example.com",
		AccountKey: "acct-hw-1",
		ResourceID: "www.example.com",
		OldCloudID: "old-cloud-cert-1",
		NewCloudID: "new-cloud-cert-1",
	}
	if cloud == domain.CloudAWS {
		w.AccountKey = "acct-aws-1"
		w.ResourceID = "arn:aws:elasticloadbalancing:us-east-1:123456789012:listener/alb-1"
		w.OldCloudID = "arn:aws:acm:us-east-1:123456789012:certificate/old-cert"
		w.NewCloudID = "arn:aws:acm:us-east-1:123456789012:certificate/new-cert"
	}
	if cloud == domain.CloudAzure {
		w.AccountKey = "acct-az-1"
		w.ResourceID = "appgw-1/listener-1"
		w.OldCloudID = "https://vault-test.vault.azure.net/secrets/old-cert/1"
		w.NewCloudID = "https://vault-test.vault.azure.net/secrets/new-cert/1"
	}

	w.OldCertID, w.OldFP = h.SeedCompleteCert("old.example.com", w.Domain)
	w.NewCertID, w.NewFP = h.SeedCompleteCert("new.example.com", w.Domain)
	h.OldCertCloudID(string(cloud), w.OldCloudID, w.OldFP)
	// Fresh done snapshot carrying the old-fingerprint reference ( the HTTP
	// generate flow binds the changelist to the latest done snapshot ).
	h.SeedDoneSnapshotWithRefs([]multicloudtest.RefSpec{
		{Cloud: cloud, Product: product, AccountKey: w.AccountKey,
			ResourceID: w.ResourceID, CloudCertID: w.OldCloudID, Fingerprint: w.OldFP},
	})
	return w
}

// seedReplacedState seeds an executing order ( one failed + one success item )
// plus the new cert's active mapping — the post-replacement state a rollback
// request arrives at. Returns ( orderID, successItemID ).
func seedReplacedState(t *testing.T, h *multicloudtest.Harness, w rollbackWorld, cloud domain.Cloud, product domain.Product) (string, string) {
	t.Helper()
	orderID := h.SeedOrder(domain.ChangeStatusExecuting,
		&domain.BatchInfo{TotalBatches: 1, CurrentBatch: 1, BatchSize: 1, Paused: false},
		nil, w.OldFP, w.NewCertID)
	ref := cloudRef(w, cloud, product)
	h.SeedItem(orderID, ref, domain.ItemStatusFailed, 1, w.OldCloudID)
	successID := h.SeedItem(orderID, ref, domain.ItemStatusSuccess, 1, w.OldCloudID, w.NewCloudID)
	h.SeedMapping(w.NewFP, string(cloud), w.AccountKey, w.NewCloudID)
	return orderID, successID
}

// cloudRef builds the cloud_api resource ref of the scenario.
func cloudRef(w rollbackWorld, cloud domain.Cloud, product domain.Product) domain.ResourceRef {
	return domain.ResourceRef{Channel: domain.ChannelCloudAPI, Cloud: string(cloud),
		Product: string(product), AccountKey: w.AccountKey, ResourceID: w.ResourceID}
}

// seedTerminalRolledBackState seeds a partial_completed order whose single
// item is already rolled_back ( repeated-rollback / idempotency fixture ).
func seedTerminalRolledBackState(t *testing.T, h *multicloudtest.Harness, w rollbackWorld, cloud domain.Cloud, product domain.Product) string {
	t.Helper()
	orderID := h.SeedOrder(domain.ChangeStatusPartialCompleted, nil, nil, w.OldFP, w.NewCertID)
	h.SeedItem(orderID, cloudRef(w, cloud, product), domain.ItemStatusRolledBack, 1, w.OldCloudID, w.NewCloudID)
	return orderID
}
