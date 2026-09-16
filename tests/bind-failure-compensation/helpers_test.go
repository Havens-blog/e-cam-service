// @feature cert-multicloud-deployers @api-functional
//
// Journey fixtures for bind-failure-compensation contract tests.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package bind_failure_compensation

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// verifyConfirmProbes mirrors domain.DefaultThresholds().VerifyConfirmProbes.
const verifyConfirmProbes = 2

// compensationWorld bundles the fixture entities of one bind-failure scenario.
type compensationWorld struct {
	OldFP       string
	NewFP       string
	NewCertID   string
	Domain      string
	OldCloudIDs map[string]string
}

// seedCompensationWorld seeds: complete old/new certificates, a fresh done
// snapshot with one huawei cdn reference, and a valid rollback target.
func seedCompensationWorld(t *testing.T, h *multicloudtest.Harness, cloud domain.Cloud, product domain.Product, accountKey, resourceID string) compensationWorld {
	t.Helper()
	w := compensationWorld{Domain: "www.example.com"}
	_, w.OldFP = h.SeedCompleteCert("old.example.com", w.Domain)
	w.NewCertID, w.NewFP = h.SeedCompleteCert("new.example.com", w.Domain)
	w.OldCloudIDs = map[string]string{string(cloud): "old-cloud-cert-1"}
	h.OldCertCloudID(string(cloud), w.OldCloudIDs[string(cloud)], w.OldFP)
	h.SeedDoneSnapshotWithRefs([]multicloudtest.RefSpec{
		{Cloud: cloud, Product: product, AccountKey: accountKey,
			ResourceID: resourceID, CloudCertID: w.OldCloudIDs[string(cloud)], Fingerprint: w.OldFP},
	})
	return w
}

// seedExecutingWithPendingItem seeds an executing single-batch order holding
// one pending cloud_api item ( the pre-execute state of the failure flow ).
func seedExecutingWithPendingItem(t *testing.T, h *multicloudtest.Harness, w compensationWorld,
	cloud, product, accountKey, resourceID string) (string, string) {
	t.Helper()
	orderID := h.SeedOrder(domain.ChangeStatusExecuting,
		&domain.BatchInfo{TotalBatches: 1, CurrentBatch: 1, BatchSize: 1, Paused: false},
		nil, w.OldFP, w.NewCertID)
	ref := domain.ResourceRef{Channel: domain.ChannelCloudAPI, Cloud: cloud,
		Product: product, AccountKey: accountKey, ResourceID: resourceID}
	itemID := h.SeedItem(orderID, ref, domain.ItemStatusPending, 1, w.OldCloudIDs[cloud])
	return orderID, itemID
}

// shrinkVerifyWindow sets VerifyWindowHours=0 so a verifying order becomes
// immediately eligible for the window-expiry finalizer ( the production path
// from a failed batch to a terminal state that frees the cleanup gates ).
func shrinkVerifyWindow(t *testing.T, h *multicloudtest.Harness) {
	t.Helper()
	cfg, err := h.AlertCfg.Get(context.Background())
	require.NoError(t, err)
	cfg.Thresholds.VerifyWindowHours = 0
	require.NoError(t, h.AlertCfg.Save(context.Background(), &cfg))
}

// seedTimedOutItem writes a running change item whose heartbeat stopped
// longer than the default 30-minute threshold ago.
func seedTimedOutItem(t *testing.T, h *multicloudtest.Harness, orderID string, ref domain.ResourceRef, oldCloudCertID string) string {
	t.Helper()
	heartbeat := time.Now().Add(-31 * time.Minute)
	item := domain.ChangeItem{
		ID:             primitive.NewObjectID(),
		OrderID:        orderID,
		BatchNo:        1,
		Action:         domain.ActionUploadAndBind,
		ResourceRef:    ref,
		OldCloudCertID: oldCloudCertID,
		Status:         domain.ItemStatusRunning,
		HeartbeatAt:    &heartbeat,
	}
	_, err := h.Items.CreateMulti(context.Background(), []domain.ChangeItem{item})
	require.NoError(t, err)
	return item.ID.Hex()
}
