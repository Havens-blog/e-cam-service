// @feature cert-multicloud-deployers @api-functional
//
// Contract: multicloud-cert-replacement / Step 5 — 云证书 ID 写入 CloudCertMapping.
// Outcomes under test: success, abnormal-id-explicit-rejection, upsert-idempotent-overwrite.
//
// The "abnormal ID" explicit rejections that live inside the real cloud
// adapters ( CloudFront non-ACM-ARN refusal, listener/region mismatch text )
// are adapter-internal ( fact MC_AWS_UPLOAD_RETURN / MC_AWS_LISTENER_CHECK,
// covered by cloudx unit tests ). At this surface the reachable explicit
// rejection is the empty-ID invalid-target error on the cleanup/inspect
// paths ( fact MC_MAPPING_WRITE_BEFORE_BIND: cloud_api_channel.go:283-285 ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multicloud_cert_replacement

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Contract outcome "success": 映射记录落库且状态 active；ID 形态按云区分，
// 三云 ID 空间互斥不混淆；映射先于绑定段写入（唯一键 Upsert，uploadedAt 默认当前）。
func TestExecuteMappingWrittenActivePerCloudForm(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedThreeCloudReplacement(t, h)

	list := generateFor(t, h, w)
	h.MustConfirm(list.OrderID, batchedThreeConf())
	h.MustExecute(list.OrderID) // batch 1 ( huawei item )

	// Deep (cross-entity) per item: uploaded ID -> item.NewCloudCertID ->
	// active mapping row, with the per-cloud ID form ( batch 1 = the first
	// item in (cloud, product, resourceId) order ).
	items, err := h.Items.ListByOrder(context.Background(), list.OrderID)
	require.NoError(t, err)
	var executed *domain.ChangeItem
	pendingCount := 0
	for i := range items {
		switch items[i].Status {
		case domain.ItemStatusSuccess:
			executed = &items[i]
		case domain.ItemStatusPending:
			pendingCount++
		}
	}
	require.NotNil(t, executed, "batch-1 item executed")
	assert.Equal(t, 2, pendingCount, "batches 2-3 not executed yet")

	mapping, err := h.MappingByCloudCert(executed.ResourceRef.Cloud, executed.ResourceRef.AccountKey, executed.NewCloudCertID)
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusActive, mapping.Status)
	assert.Equal(t, w.NewFP, mapping.CertFingerprint)
	assert.False(t, mapping.UploadedAt.IsZero(), "uploadedAt defaulted on upsert")
	assert.WithinDuration(t, time.Now(), mapping.UploadedAt, time.Minute)
	// ID 空间互斥：映射 ID 形态与执行云一致。
	assertCloudCertIDForm(t, executed.ResourceRef.Cloud, mapping.CloudCertID)

	// State: 只有已执行条目写了映射（其余批次未到）。
	rows, err := h.Mappings.ListByFingerprint(context.Background(), w.NewFP)
	require.NoError(t, err)
	assert.Len(t, rows, 1, "only the executed item has written a mapping so far")
}

// assertCloudCertIDForm verifies the per-cloud ID form is mutually exclusive:
// huawei = SCM UUID ( no ARN / no KV URL ), aws = us-east-1 ACM ARN,
// azure = versioned KV secret ID reference.
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

// Contract outcome "abnormal-id-explicit-rejection": 异常形态 ID 显式失败不
// 猜测——空 ID 在清理/回读路径按无效目标错误拒绝，不产生成功终态。
func TestExecuteMappingAbnormalIdExplicitRejection(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := replacementWorld{Domain: "www.example.com"}
	w.OldCertID, w.OldFP = h.SeedCompleteCert("old.example.com", w.Domain)
	w.NewCertID, w.NewFP = h.SeedCompleteCert("new.example.com", w.Domain)

	// Seeded orphan mapping carrying an abnormal ( empty ) cloud cert ID.
	orderID := h.SeedOrder(domain.ChangeStatusPartialCompleted, nil, nil, w.OldFP, w.NewCertID)
	h.SeedItem(orderID, domain.ResourceRef{Channel: domain.ChannelCloudAPI, Cloud: "huawei",
		Product: "cdn", AccountKey: "acct-hw-1", ResourceID: "www.example.com"},
		domain.ItemStatusSuccess, 1, w.OldCloudIDs["huawei"])
	stale := h.SeedMapping(w.NewFP, "huawei", "acct-hw-1", "")
	require.NoError(t, h.Mappings.UpdateStatus(context.Background(), stale.ID.Hex(), domain.MappingStatusOrphan))

	consumed, err := h.Cleanup.SweepOrphans(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, consumed, "a cleanup action was produced (explicit failure, not silent skip)")

	// Output: 显式失败（invalid-target 语义）→ 失败结果 + 运维处置告警。
	results := h.Orphans.All()
	require.Len(t, results, 1)
	assert.False(t, results[0].Success, "abnormal ID fails explicitly, no success terminal")
	assert.Equal(t, service.OrphanActionCleanup, results[0].Action)
	require.Len(t, h.Alerts.Events(), 1, "cleanup failure raises an ops alert")

	// State: 不产生以异常 ID 为绑定的成功终态；记录保留待收敛。
	kept, err := h.MappingByCloudCert("huawei", "acct-hw-1", "")
	require.NoError(t, err)
	assert.Equal(t, stale.ID, kept.ID, "dirty record stays for retry instead of silent success")
}

// Contract outcome "upsert-idempotent-overwrite": 同键再次 Upsert 幂等覆盖
// （不新增行），记录以最新执行为准且状态 active，行数不变。
func TestExecuteMappingUpsertIdempotentOverwrite(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := replacementWorld{Domain: "www.example.com"}
	w.OldCertID, w.OldFP = h.SeedCompleteCert("old.example.com", w.Domain)
	w.NewCertID, w.NewFP = h.SeedCompleteCert("new.example.com", w.Domain)

	// 既有同键 active 映射（崩溃恢复后的重跑场景遗留）。
	staleID := "scm-stale-execution-copy"
	stale := h.SeedMapping(w.NewFP, "huawei", "acct-hw-1", staleID)

	orderID, _ := seedSingleItemOrder(t, h, w, "huawei", "cdn", "acct-hw-1",
		"www.example.com", w.OldCloudIDs["huawei"])
	h.MustExecute(orderID)
	progress := h.MustProgress(orderID)
	require.Equal(t, string(domain.ItemStatusSuccess), progress.ItemStates[0].Status,
		"error=%s", progress.ItemStates[0].Error)

	// State: 行数不变，cloudCertId 与 uploadedAt 更新为最新执行产物。
	rows, err := h.Mappings.ListByFingerprint(context.Background(), w.NewFP)
	require.NoError(t, err)
	require.Len(t, rows, 1, "unique-key upsert does not add a row")
	fresh := rows[0]
	assert.Equal(t, stale.ID, fresh.ID, "same key, same row")
	assert.NotEqual(t, staleID, fresh.CloudCertID, "record reflects the latest execution")
	assert.Equal(t, domain.MappingStatusActive, fresh.Status)
	assert.False(t, fresh.UploadedAt.IsZero(), "uploadedAt refreshed on overwrite")
}
