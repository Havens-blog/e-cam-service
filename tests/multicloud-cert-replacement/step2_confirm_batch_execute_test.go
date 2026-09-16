// @feature cert-multicloud-deployers @api-functional
//
// Contract: multicloud-cert-replacement / Step 2 — 确认变更清单并进入分批执行.
// Outcomes under test: success, batch-gate-blocking, unauthorized.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multicloud_cert_replacement

import (
	"context"
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Contract outcome "success": confirm 受理（批次分配落定）+ execute 受理（当前批
// 条目进入两段式编排）→ 批内条目 success，订单进入批间验证窗口。
func TestConfirmAndExecute_BatchedTwoPhaseRuns(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedThreeCloudReplacement(t, h)

	list := generateFor(t, h, w)

	// Confirm: 批次分配固化（3 项 → 每批 1 项，共 3 批；首批 ≤ floor(3/2)）。
	ack := h.MustConfirm(list.OrderID, batchedThreeConf())
	assert.Equal(t, list.OrderID, ack.OrderID)

	detail := h.MustDetail(list.OrderID)
	require.NotNil(t, detail.BatchInfo, "batch assignment persisted on confirm")
	assert.Equal(t, 3, detail.BatchInfo.TotalBatches)
	assert.Equal(t, 1, detail.BatchInfo.CurrentBatch)
	assert.Equal(t, 1, detail.BatchInfo.BatchSize, "effective batch size = min(1, floor(3/2))")
	assert.False(t, detail.BatchInfo.Paused)

	// Execute: 当前批（batch 1, huawei 项）两段式执行成功。
	h.MustExecute(list.OrderID)

	progress := h.MustProgress(list.OrderID)
	require.Len(t, progress.ItemStates, 3)
	byItem := map[string]multicloudtest.ProgressItemPayload{}
	for _, s := range progress.ItemStates {
		byItem[s.ItemID] = s
	}
	// Batch 1 = exactly one item ( effective batch size = floor(3/2) bound );
	// it settled success; the other two stay pending in later batches.
	var batch1State multicloudtest.ProgressItemPayload
	batch1Count := 0
	for _, s := range progress.ItemStates {
		if s.BatchNo == 1 {
			batch1Count++
			batch1State = s
		} else {
			assert.Equal(t, string(domain.ItemStatusPending), byItem[s.ItemID].Status,
				"later-batch item not executed yet")
		}
	}
	assert.Equal(t, 1, batch1Count, "first batch holds exactly one item (<= floor(total/2))")
	assert.Equal(t, string(domain.ItemStatusSuccess), batch1State.Status,
		"batch-1 item succeeded via the two-phase orchestration (error=%s)", batch1State.Error)
	assert.Empty(t, batch1State.Error)

	// State: 订单进入批间验证窗口（非终批 → verifying + paused 等待人工续批）。
	order, err := h.Orders.GetByID(context.Background(), list.OrderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusVerifying, order.Status)
	require.NotNil(t, order.BatchInfo)
	assert.True(t, order.BatchInfo.Paused, "manual batch continuation required between batches")
	require.NotNil(t, order.VerifyExpected, "verify expected sealed on window entry")

	// Deep (cross-entity): the executed item's new cloud cert ID is persisted
	// on both the item and its active mapping row.
	item, err := h.Items.GetByID(context.Background(), batch1State.ItemID)
	require.NoError(t, err)
	require.NotEmpty(t, item.NewCloudCertID, "upload produced a cloud cert ID")
	mapping, err := h.MappingByCloudCert(item.ResourceRef.Cloud, item.ResourceRef.AccountKey, item.NewCloudCertID)
	require.NoError(t, err, "mapping row written for the uploaded cert")
	assert.Equal(t, domain.MappingStatusActive, mapping.Status)
	assert.Equal(t, w.NewFP, mapping.CertFingerprint)
	assert.Equal(t, item.ResourceRef.AccountKey, mapping.AccountKey)
}

// Contract outcome "batch-gate-blocking": 上一批未全部 success → 409
// BATCH_NOT_CONFIRMABLE；已完成批次结果保留，不整单回退、不重复执行。
func TestConfirmBatch_GateBlocksWhenPreviousBatchIncomplete(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedThreeCloudReplacement(t, h)

	// Seeded multi-batch order in the inter-batch paused state with a failed
	// item in the previous batch (限流重试/硬失败均阻断续批).
	oldOrderID := h.SeedOrder(domain.ChangeStatusExecuting,
		&domain.BatchInfo{TotalBatches: 2, CurrentBatch: 1, BatchSize: 1, Paused: true},
		nil, w.OldFP, w.NewCertID)
	cloudRef := domain.ResourceRef{Channel: domain.ChannelCloudAPI, Cloud: "huawei",
		Product: "cdn", AccountKey: "acct-hw-1", ResourceID: "www.example.com"}
	successID := h.SeedItem(oldOrderID, cloudRef, domain.ItemStatusSuccess, 1, w.OldCloudIDs["huawei"])
	awsRef := domain.ResourceRef{Channel: domain.ChannelCloudAPI, Cloud: "aws",
		Product: "alb", AccountKey: "acct-aws-1",
		ResourceID: "arn:aws:elasticloadbalancing:us-east-1:123456789012:listener/alb-1"}
	failedID := h.SeedItem(oldOrderID, awsRef, domain.ItemStatusFailed, 1, w.OldCloudIDs["aws"])

	resp := h.ConfirmBatch(oldOrderID)

	// Output: 409 门禁拦截，不自动前滚。
	require.Equal(t, http.StatusConflict, resp.StatusCode, resp.Body)
	require.NotNil(t, resp.Env.Error, "cert envelope error present")
	assert.Equal(t, "BATCH_NOT_CONFIRMABLE", resp.Env.Error.Code)

	// State: 失败条目保持 failed；订单保持 executing（分批暂停语义）；
	// 已完成批次结果保留（success 条目不动、批次指针不前移）。
	order, err := h.Orders.GetByID(context.Background(), oldOrderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusExecuting, order.Status)
	require.NotNil(t, order.BatchInfo)
	assert.Equal(t, 1, order.BatchInfo.CurrentBatch, "no batch advancement")

	failed, err := h.Items.GetByID(context.Background(), failedID)
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusFailed, failed.Status)
	succeeded, err := h.Items.GetByID(context.Background(), successID)
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusSuccess, succeeded.Status, "completed batch results kept")
}

// Contract outcome "unauthorized": confirm/execute 无有效会话 → HTTP 401 全局
// 认证失败文案；变更单状态不变，批次不分配。
func TestConfirmAndExecute_UnauthenticatedRejected401(t *testing.T) {
	h := multicloudtest.NewHarness(t, func(cfg *multicloudtest.Config) {
		cfg.Claims = nil
	})
	w := seedThreeCloudReplacement(t, h)
	orderID := h.SeedOrder(domain.ChangeStatusPendingConfirm, seededBatchInfo(1), nil, w.OldFP, w.NewCertID)
	itemID := h.SeedItem(orderID, domain.ResourceRef{Channel: domain.ChannelCloudAPI, Cloud: "huawei",
		Product: "cdn", AccountKey: "acct-hw-1", ResourceID: "www.example.com"},
		domain.ItemStatusPending, 1, w.OldCloudIDs["huawei"])

	confirmResp := h.Confirm(orderID, nil)
	require.Equal(t, http.StatusUnauthorized, confirmResp.StatusCode, confirmResp.Body)
	assert.Contains(t, confirmResp.Body, "认证失败")
	assert.Nil(t, confirmResp.Env.Error, "raw middleware body, not the cert envelope")

	executeResp := h.Execute(orderID)
	require.Equal(t, http.StatusUnauthorized, executeResp.StatusCode, executeResp.Body)
	assert.Contains(t, executeResp.Body, "认证失败")

	// State: 变更单状态不变，批次不分配，条目未执行。
	order, err := h.Orders.GetByID(context.Background(), orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusPendingConfirm, order.Status)
	assert.Equal(t, 1, order.BatchInfo.CurrentBatch, "batch pointer unchanged")
	item, err := h.Items.GetByID(context.Background(), itemID)
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusPending, item.Status)
	assert.Empty(t, h.Huawei.UploadCalls(), "no cloud upload dispatched")
}
