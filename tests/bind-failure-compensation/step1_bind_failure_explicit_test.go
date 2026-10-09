// @feature cert-multicloud-deployers @api-functional
//
// Contract: bind-failure-compensation / Step 1 — 绑定段执行失败并显式报错.
// Outcomes under test: bind-failure-explicit, bind-rate-limited,
// interrupted-before-bind, unauthorized.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package bind_failure_compensation

import (
	"context"
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cloudx-sdk"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Contract outcome "bind-failure-explicit": 绑定被云侧拒绝 → 条目 failed
// (EXEC_FAILED + 静态文案)，不进入验证窗口、不标记成功；补偿链触发
// （映射 active→orphan + CleanupOrphan，详见 Step 2/3 断言）。
func TestBindFailure_ExplicitFailureMarksItemFailed(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedCompensationWorld(t, h, domain.CloudHuawei, domain.ProductCDN, "acct-hw-1", "www.example.com")
	orderID, _ := seedExecutingWithPendingItem(t, h, w, "huawei", "cdn", "acct-hw-1", "www.example.com")

	// 注入式绑定拒绝（非限流，适配层静态文案 + 产品上下文）。
	h.Huawei.SetBindErr(1, assert.AnError)

	h.MustExecute(orderID)
	progress := h.MustProgress(orderID)
	require.Len(t, progress.ItemStates, 1)

	// Output: 条目 failed，失败因 EXEC_FAILED + 静态文案（无凭证/私钥片段）。
	assert.Equal(t, string(domain.ItemStatusFailed), progress.ItemStates[0].Status)
	assert.Contains(t, progress.ItemStates[0].Error, "EXEC_FAILED")
	assert.NotContains(t, progress.ItemStates[0].Error, "test-sk", "credential material never leaks")
	assert.NotContains(t, progress.ItemStates[0].Error, "PRIVATE KEY")

	// State: 该条目不标记成功；上传产物经补偿转 orphan 入清理队列。
	orphans := h.OrphanMappings()
	require.Len(t, orphans, 1, "compensation moved the mapping into the cleanup queue")
	assert.Equal(t, w.NewFP, orphans[0].CertFingerprint)
	// 补偿尽力删除已上传云证书。
	require.NotEmpty(t, h.Huawei.CleanupCalls(), "CleanupOrphan compensation ran")

	// 订单进入批间验证窗口（单批失败项不标记成功，窗口随后判定）。
	order, err := h.Orders.GetByID(context.Background(), orderID)
	require.NoError(t, err)
	assert.NotEqual(t, domain.ChangeStatusCompleted, order.Status)
}

// Contract outcome "bind-rate-limited": 绑定段限流 → 有界退避自动重试（不无限）；
// 重试成功收敛 success。NOTE: engine-level rate_limited marking ( 30s/2m real
// backoff ) is out of hermetic scope; the deployer-level bounded retry seam
// ( same RetryPolicy shape, millisecond backoffs ) is exercised here.
func TestBindFailure_RateLimitedRetriesBounded(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedCompensationWorld(t, h, domain.CloudHuawei, domain.ProductCDN, "acct-hw-1", "www.example.com")
	orderID, _ := seedExecutingWithPendingItem(t, h, w, "huawei", "cdn", "acct-hw-1", "www.example.com")

	// 第一次绑定限流，第二次成功（部署器有界重试预算内）。
	h.Huawei.SetBindErr(1, cloudx.ErrCloudRateLimited)

	h.MustExecute(orderID)

	// Output: 限流被有界重试吸收，条目收敛 success（无失败残留）。
	binds := h.Huawei.BindCalls()
	require.Len(t, binds, 2, "bounded retry consumed exactly one extra bind attempt")
	progress := h.MustProgress(orderID)
	assert.Equal(t, string(domain.ItemStatusSuccess), progress.ItemStates[0].Status,
		"error=%s", progress.ItemStates[0].Error)

	// State: 重试成功后映射保持 active（无补偿残留）。
	orphans := h.OrphanMappings()
	assert.Empty(t, orphans, "no orphan mapping on a rate-limited-then-successful bind")
}

// Contract outcome "interrupted-before-bind": 进程在 BindResource 前中断 →
// 心跳超时恢复 failed(EXEC_TIMEOUT) + 告警；悬空上传产物可经补偿链路收敛。
func TestBindFailure_InterruptedBeforeBindRecoveredByTimeout(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedCompensationWorld(t, h, domain.CloudHuawei, domain.ProductCDN, "acct-hw-1", "www.example.com")
	orderID := h.SeedOrder(domain.ChangeStatusExecuting,
		&domain.BatchInfo{TotalBatches: 1, CurrentBatch: 1, BatchSize: 1, Paused: false},
		nil, w.OldFP, w.NewCertID)
	ref := domain.ResourceRef{Channel: domain.ChannelCloudAPI, Cloud: "huawei",
		Product: "cdn", AccountKey: "acct-hw-1", ResourceID: "www.example.com"}
	itemID := seedTimedOutItem(t, h, orderID, ref, w.OldCloudIDs["huawei"])

	// Crash anchor: mapping already active from the pre-crash upload.
	firstUploadID := "scm-dangling-copy"
	h.SeedMapping(w.NewFP, "huawei", "acct-hw-1", firstUploadID)

	shrinkVerifyWindow(t, h)

	// 恢复链路（生产入口 cert:executing-timeout 调度任务）。
	recovered, err := h.Exec.RecoverTimedOutItems(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, recovered)

	// Output: 条目 failed(EXEC_TIMEOUT)，恢复告警已发布。
	progress := h.MustProgress(orderID)
	require.Len(t, progress.ItemStates, 1)
	assert.Equal(t, string(domain.ItemStatusFailed), progress.ItemStates[0].Status)
	assert.Contains(t, progress.ItemStates[0].Error, "EXEC_TIMEOUT")
	require.Len(t, h.Notifier.Events(), 1)
	assert.Equal(t, itemID, h.Notifier.Events()[0].ItemID)

	// State: 悬空映射保留（重跑按唯一键覆盖，不直接复用悬空 ID 绑定）。
	anchor, err := h.MappingByCloudCert("huawei", "acct-hw-1", firstUploadID)
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusActive, anchor.Status)

	// 窗口到期终局判定收敛订单（终态后队列可消费该悬空产物）。
	expired, err := h.Verify.FinalizeExpiredWindows(context.Background())
	require.NoError(t, err)
	require.Len(t, expired, 1)
}

// Contract outcome "unauthorized": execute 无有效会话 → HTTP 401 全局认证
// 失败文案（非 cert 信封）；不派发执行。
func TestBindFailure_UnauthenticatedRejected401(t *testing.T) {
	h := multicloudtest.NewHarness(t, func(cfg *multicloudtest.Config) {
		cfg.Claims = nil
	})
	w := seedCompensationWorld(t, h, domain.CloudHuawei, domain.ProductCDN, "acct-hw-1", "www.example.com")
	orderID := h.SeedOrder(domain.ChangeStatusExecuting,
		&domain.BatchInfo{TotalBatches: 1, CurrentBatch: 1, BatchSize: 1, Paused: false},
		nil, w.OldFP, w.NewCertID)
	h.SeedItem(orderID, domain.ResourceRef{Channel: domain.ChannelCloudAPI, Cloud: "huawei",
		Product: "cdn", AccountKey: "acct-hw-1", ResourceID: "www.example.com"},
		domain.ItemStatusPending, 1, w.OldCloudIDs["huawei"])

	resp := h.Execute(orderID)

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.Contains(t, resp.Body, "认证失败")
	assert.Nil(t, resp.Env.Error, "raw middleware body, not the cert envelope")

	// State: 不派发执行 —— 条目未领取，无云调用。
	items, err := h.Items.ListByOrder(context.Background(), orderID)
	require.NoError(t, err)
	for _, it := range items {
		assert.Equal(t, domain.ItemStatusPending, it.Status, "item not claimed")
	}
	assert.Empty(t, h.Huawei.UploadCalls(), "no cloud upload dispatched")
}
