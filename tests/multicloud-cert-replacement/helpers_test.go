// @feature cert-multicloud-deployers @api-functional
//
// Journey fixtures for multicloud-cert-replacement contract tests. Every
// helper builds the preconditions declared by the corresponding Contract's
// fixture_spec ( Certificate / ScanSnapshot / CertReference / CloudAccount /
// ChangeOrder / ChangeItem entities ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multicloud_cert_replacement

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// verifyConfirmProbes is domain.DefaultThresholds().VerifyConfirmProbes ( 2 ):
// the number of consecutive consistent probe rounds required by the window.
const verifyConfirmProbes = 2

// replacementWorld bundles the fixture entities of one replacement scenario.
type replacementWorld struct {
	OldCertID   string
	OldFP       string
	NewCertID   string
	NewFP       string
	SnapshotID  string
	Domain      string // target SAN shared by old/new certificates
	OldCloudIDs map[string]string
}

// seedThreeCloudReplacement seeds the happy-path fixture: one complete old
// certificate ( hostingStatus=complete, private key in ledger ), one ready
// new certificate, a fresh done snapshot with one three-cloud reference per
// cloud ( distinct account per cloud so each (fingerprint, cloud, accountKey)
// mapping key is unique ), and valid rollback targets on every cloud stub.
func seedThreeCloudReplacement(t *testing.T, h *multicloudtest.Harness) replacementWorld {
	t.Helper()
	w := replacementWorld{Domain: "www.example.com"}
	w.OldCertID, w.OldFP = h.SeedCompleteCert("old.example.com", w.Domain)
	w.NewCertID, w.NewFP = h.SeedCompleteCert("new.example.com", w.Domain)

	w.OldCloudIDs = map[string]string{
		"huawei": "scm-old-cert-0001",
		"aws":    "arn:aws:acm:us-east-1:123456789012:certificate/old-cert",
		"azure":  "https://vault-test.vault.azure.net/secrets/old-cert/1",
	}
	for cloud, oldID := range w.OldCloudIDs {
		h.OldCertCloudID(cloud, oldID, w.OldFP)
	}

	w.SnapshotID = h.SeedDoneSnapshotWithRefs([]multicloudtest.RefSpec{
		{Cloud: domain.CloudHuawei, Product: domain.ProductCDN, AccountKey: "acct-hw-1",
			ResourceID: "www.example.com", CloudCertID: w.OldCloudIDs["huawei"], Fingerprint: w.OldFP},
		{Cloud: domain.CloudAWS, Product: domain.ProductALB, AccountKey: "acct-aws-1",
			ResourceID:  "arn:aws:elasticloadbalancing:us-east-1:123456789012:listener/alb-1",
			CloudCertID: w.OldCloudIDs["aws"], Fingerprint: w.OldFP},
		{Cloud: domain.CloudAzure, Product: domain.ProductCDN, AccountKey: "acct-az-1",
			ResourceID: "frontdoor-1/frontend-endpoint-1", CloudCertID: w.OldCloudIDs["azure"], Fingerprint: w.OldFP},
	})
	return w
}

// generateFor drives POST /changes against the seeded world and requires the
// generation to succeed, returning the changelist payload.
func generateFor(t *testing.T, h *multicloudtest.Harness, w replacementWorld) multicloudtest.ChangeListPayload {
	t.Helper()
	return h.MustGenerate(w.OldFP, w.NewCertID)
}

// batchedThreeConf is the confirm configuration for a 3-item order:
// Enabled=true, BatchSize=1 ( effective size = min(1, floor(3/2)) = 1 ),
// MaxBatchRatio=0.5 ( the hard upper bound ).
func batchedThreeConf() *deployer.BatchConf {
	return &deployer.BatchConf{Enabled: true, BatchSize: 1, MaxBatchRatio: 0.5}
}

// seededBatchInfo builds a single-batch BatchInfo ( totalBatches=1 ).
func seededBatchInfo(size int) *domain.BatchInfo {
	return &domain.BatchInfo{TotalBatches: 1, CurrentBatch: 1, BatchSize: size, Paused: false}
}

// probeRounds runs the production verify-window prober for the configured
// number of consecutive rounds ( default 2 = VerifyConfirmProbes ).
func probeRounds(t *testing.T, h *multicloudtest.Harness, rounds int) {
	t.Helper()
	for i := 0; i < rounds; i++ {
		_, err := h.Verify.ProbeVerifyingWindows(context.Background())
		require.NoError(t, err)
	}
}

// seedTimedOutItem writes a running change item whose heartbeat stopped
// longer than the default 30-minute threshold ago ( crash-recovery fixture ).
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
