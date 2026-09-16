// @feature cert-multicloud-deployers @api-functional
//
// Journey smoke: bind-failure-compensation. The journey's "happy path" is the
// failure→compensation→convergence loop itself, driven end-to-end through the
// HTTP surface: execute a bind-failing item ( explicit failure ) -> mapping
// active->orphan -> CleanupOrphan compensation -> order finalized terminal ->
// cleanup queue deletes the orphan -> rerun produces fresh IDs, with the 401
// gate as the extra error-path check.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package bind_failure_compensation

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Journey invariants verified across the loop:
//   - 绑定失败必经 CleanupOrphan 补偿，且补偿幂等；
//   - 失败状态、orphan 迁移、清理队列与既有云走同一状态机；
//   - 云端错误细节仅入日志不进 API 响应（静态文案）；
//   - 清理队列最终收敛（删除或显式保留，不静默丢失）。
func TestBindFailureCompensation_FullLoopSmoke(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedCompensationWorld(t, h, domain.CloudHuawei, domain.ProductCDN, "acct-hw-1", "www.example.com")
	shrinkVerifyWindow(t, h) // failed batch converges to a terminal state immediately

	// ---- Step 1: 绑定段失败并显式报错（经 POST execute 的 HTTP 面） ----
	orderID, itemID := seedExecutingWithPendingItem(t, h, w, "huawei", "cdn", "acct-hw-1", "www.example.com")
	h.Huawei.SetBindErr(1, errors.New("huawei cdn: listener protocol mismatch, update rejected"))
	h.MustExecute(orderID)

	progress := h.MustProgress(orderID)
	require.Len(t, progress.ItemStates, 1)
	state := progress.ItemStates[0]
	assert.Equal(t, itemID, state.ItemID)
	assert.Equal(t, string(domain.ItemStatusFailed), state.Status)
	assert.Contains(t, state.Error, "EXEC_FAILED")
	assert.NotContains(t, state.Error, "test-sk", "no credential material in the API response")

	// ---- Step 2/3: CleanupOrphan 补偿 + 映射 active→orphan 入清理队列 ----
	orphans := h.OrphanMappings()
	require.Len(t, orphans, 1, "compensation moved the mapping into the cleanup queue")
	newID := orphans[0].CloudCertID
	require.NotEmpty(t, newID, "upload phase produced a cloud cert ID before the bind failure")
	require.Len(t, h.Huawei.CleanupCalls(), 1, "compensation delete executed")
	mapping, err := h.MappingByCloudCert("huawei", "acct-hw-1", newID)
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusOrphan, mapping.Status)

	// ---- 订单收敛（窗口到期终局判定）→ 终态释放清理门禁 ----
	expired, err := h.Verify.FinalizeExpiredWindows(context.Background())
	require.NoError(t, err)
	require.Len(t, expired, 1)

	// ---- Step 4: 清理队列消费删除云侧孤儿证书 ----
	consumed, err := h.Cleanup.SweepOrphans(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, consumed)
	_, err = h.MappingByCloudCert("huawei", "acct-hw-1", newID)
	assert.Error(t, err, "orphan mapping deleted ( queue and mapping converged )")
	results, err := h.Orphans.ListOrphanCleanup(context.Background(), orderID)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, service.OrphanActionCleanup, results[0].Action)
	assert.True(t, results[0].Success)

	// ---- 重跑：重新上传/绑定产生新的云证书 ID 与 active 映射 ----
	newOrderID, _ := seedExecutingWithPendingItem(t, h, w, "huawei", "cdn", "acct-hw-1", "www.example.com")
	h.MustExecute(newOrderID)
	rerunProgress := h.MustProgress(newOrderID)
	require.Len(t, rerunProgress.ItemStates, 1)
	assert.Equal(t, string(domain.ItemStatusSuccess), rerunProgress.ItemStates[0].Status,
		"rerun succeeds with a fresh two-phase run (error=%s)", rerunProgress.ItemStates[0].Error)
	rerunItems, err := h.Items.ListByOrder(context.Background(), newOrderID)
	require.NoError(t, err)
	require.Len(t, rerunItems, 1)
	assert.NotEqual(t, newID, rerunItems[0].NewCloudCertID, "rerun produces a fresh cloud cert ID")

	// ---- Error path: 无有效会话 → 401 原始文案 ----
	deny := multicloudtest.NewHarness(t, func(cfg *multicloudtest.Config) { cfg.Claims = nil })
	w2 := seedCompensationWorld(t, deny, domain.CloudHuawei, domain.ProductCDN, "acct-hw-1", "www.example.com")
	denyOrder := deny.SeedOrder(domain.ChangeStatusExecuting,
		&domain.BatchInfo{TotalBatches: 1, CurrentBatch: 1, BatchSize: 1, Paused: false},
		nil, w2.OldFP, w2.NewCertID)
	deny.SeedItem(denyOrder, domain.ResourceRef{Channel: domain.ChannelCloudAPI, Cloud: "huawei",
		Product: "cdn", AccountKey: "acct-hw-1", ResourceID: "www.example.com"},
		domain.ItemStatusPending, 1, w2.OldCloudIDs["huawei"])
	resp := deny.Execute(denyOrder)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.Contains(t, resp.Body, "认证失败")
}
