// @feature cert-multicloud-deployers @api-functional
//
// Contracts: bind-failure-compensation / Step 3 — 映射状态迁移 active→orphan
// 入清理队列; Step 4 — 清理队列执行删除云侧孤儿证书.
// Outcomes under test: orphan-transition-enqueued,
// compensation-delete-failure-keeps-orphan, cleanup-success,
// rerun-after-compensation-new-ids, already-deleted-success, sweep-isolation.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package bind_failure_compensation

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Contract outcome "orphan-transition-enqueued": 补偿链路将映射 active→orphan
// 入清理队列（状态枚举仅 active/orphan 两态，按上传时间先进先出消费），
// 状态机与既有云失败路径同构。
func TestCompensation_OrphanTransitionEnqueuesCleanup(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedCompensationWorld(t, h, domain.CloudHuawei, domain.ProductCDN, "acct-hw-1", "www.example.com")
	orderID, _ := seedExecutingWithPendingItem(t, h, w, "huawei", "cdn", "acct-hw-1", "www.example.com")
	h.Huawei.SetBindErr(1, bindErr)

	h.MustExecute(orderID)

	// Output: 映射状态迁移为 orphan（状态机两态枚举）。
	orphans := h.OrphanMappings()
	require.Len(t, orphans, 1)
	mapping, err := h.MappingByCloudCert("huawei", "acct-hw-1", orphans[0].CloudCertID)
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusOrphan, mapping.Status)

	// State: 队列可见性就绪 —— 按状态列表查询可取到（入清理队列）。
	assert.Equal(t, mapping.ID, orphans[0].ID, "the same row transitioned, not duplicated")

	// 单调可追溯：不存在同一云证书 ID 的 active/orphan 并存记录。
	rows, err := h.Mappings.ListByFingerprint(context.Background(), w.NewFP)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, domain.MappingStatusOrphan, rows[0].Status)
}

// Contract outcome "compensation-delete-failure-keeps-orphan": 云侧删除调用
// 失败 → 映射保留 orphan 留队列重试，不吞错；单条失败不中断清扫；重复失败
// 按去重键抑制重复告警；恢复后重试收敛。
func TestCompensation_DeleteFailureKeepsOrphanAndRetries(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedCompensationWorld(t, h, domain.CloudHuawei, domain.ProductCDN, "acct-hw-1", "www.example.com")
	orderID, _ := seedExecutingWithPendingItem(t, h, w, "huawei", "cdn", "acct-hw-1", "www.example.com")
	shrinkVerifyWindow(t, h)
	h.Huawei.SetBindErr(1, bindErr)

	h.MustExecute(orderID)

	// 状态：映射保留 orphan（补偿删除失败后不吞错、不误标已清理）。
	orphans := h.OrphanMappings()
	require.Len(t, orphans, 1)
	newID := orphans[0].CloudCertID
	mapping, err := h.MappingByCloudCert("huawei", "acct-hw-1", newID)
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusOrphan, mapping.Status)

	// 订单收敛到终态（窗口到期终局判定），释放清理占用门禁。
	expired, err := h.Verify.FinalizeExpiredWindows(context.Background())
	require.NoError(t, err)
	require.Len(t, expired, 1)

	// 失败后重试（清扫消费）：仍失败 → 失败结果 + 运维处置告警（仅新失败）。
	h.Huawei.SetCleanupErr(newID, errors.New("huawei scm: delete throttled, retry later"))
	consumed, err := h.Cleanup.SweepOrphans(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, consumed)
	kept, err := h.MappingByCloudCert("huawei", "acct-hw-1", newID)
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusOrphan, kept.Status, "kept in the queue for retry")
	results := h.Orphans.All()
	require.Len(t, results, 1)
	assert.False(t, results[0].Success)
	// 窗口终局判定还会发布变更关联恢复告警；运维处置告警恰好一条（仅新失败）。
	opsAlerts := 0
	for _, e := range h.Alerts.Events() {
		if e.Category == service.AlertCategoryOps {
			opsAlerts++
		}
	}
	assert.Equal(t, 1, opsAlerts, "ops alert for the new cleanup failure")

	// 重复消费（同键去重）：不再产生重复结果/重复告警。
	_, err = h.Cleanup.SweepOrphans(context.Background())
	require.NoError(t, err)
	assert.Len(t, h.Orphans.All(), 1, "deduped record not duplicated")
	opsAlerts = 0
	for _, e := range h.Alerts.Events() {
		if e.Category == service.AlertCategoryOps {
			opsAlerts++
		}
	}
	assert.Equal(t, 1, opsAlerts, "repeat failure suppressed by the dedup key")

	// 云侧恢复 → 重试收敛：映射删除（清理成功语义）。
	h.Huawei.SetCleanupErr(newID, nil)
	consumed, err = h.Cleanup.SweepOrphans(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, consumed, "retry converges once the cloud recovers")
	assert.Empty(t, h.OrphanMappings(), "queue drained after successful retry")
}

// Contract outcome "cleanup-success": 映射 orphan 且不被占用、不在保护期 →
// 清理删除云侧孤儿证书并删除映射（队列与映射收敛一致）。
func TestCleanupQueue_ConsumesOrphanDeletesMapping(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedCompensationWorld(t, h, domain.CloudHuawei, domain.ProductCDN, "acct-hw-1", "www.example.com")

	// 终态单 + orphan 映射（无占用、无保护期）。
	orderID := h.SeedOrder(domain.ChangeStatusPartialCompleted, nil, nil, w.OldFP, w.NewCertID)
	h.SeedItem(orderID, domain.ResourceRef{Channel: domain.ChannelCloudAPI, Cloud: "huawei",
		Product: "cdn", AccountKey: "acct-hw-1", ResourceID: "www.example.com"},
		domain.ItemStatusSuccess, 1, w.OldCloudIDs["huawei"])
	orphan := h.SeedMapping(w.NewFP, "huawei", "acct-hw-1", "scm-dangling-new-copy")
	require.NoError(t, h.Mappings.UpdateStatus(context.Background(), orphan.ID.Hex(), domain.MappingStatusOrphan))

	consumed, err := h.Cleanup.SweepOrphans(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, consumed)

	// Output: 云侧孤儿证书被删除，映射记录删除（收敛一致），不留半成品。
	_, err = h.MappingByCloudCert("huawei", "acct-hw-1", "scm-dangling-new-copy")
	assert.Error(t, err, "consumed orphan mapping deleted")
	assert.Contains(t, h.Huawei.CleanupCalls(), "scm-dangling-new-copy")
	results, err := h.Orphans.ListOrphanCleanup(context.Background(), orderID)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, service.OrphanActionCleanup, results[0].Action)
	assert.True(t, results[0].Success)
}

// Contract outcome "rerun-after-compensation-new-ids": 补偿完成后重跑 →
// 重新上传/绑定产生新的云证书 ID 与 active 映射，不复用已清理的 orphan 记录。
func TestCleanupQueue_RerunProducesNewIdsAndActiveMapping(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedCompensationWorld(t, h, domain.CloudHuawei, domain.ProductCDN, "acct-hw-1", "www.example.com")

	// 首次失败执行 → 补偿 → 清理收敛（映射删除）。
	orderID, _ := seedExecutingWithPendingItem(t, h, w, "huawei", "cdn", "acct-hw-1", "www.example.com")
	shrinkVerifyWindow(t, h)
	h.Huawei.SetBindErr(1, bindErr)
	h.MustExecute(orderID)
	expired, err := h.Verify.FinalizeExpiredWindows(context.Background())
	require.NoError(t, err)
	require.Len(t, expired, 1, "failed order finalized to a terminal state")
	consumed, err := h.Cleanup.SweepOrphans(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, consumed)
	firstRunID := h.Huawei.UploadCalls()[0]

	// 重跑：新变更单重新执行两段式。
	newOrderID, _ := seedExecutingWithPendingItem(t, h, w, "huawei", "cdn", "acct-hw-1", "www.example.com")
	h.MustExecute(newOrderID)

	// Output: 新的云证书 ID 与 active 映射 —— 不复用已清理的 orphan 记录。
	progress := h.MustProgress(newOrderID)
	require.Len(t, progress.ItemStates, 1)
	assert.Equal(t, string(domain.ItemStatusSuccess), progress.ItemStates[0].Status,
		"error=%s", progress.ItemStates[0].Error)
	uploads := h.Huawei.UploadCalls()
	require.Len(t, uploads, 2)
	assert.NotEqual(t, firstRunID, uploads[1], "rerun uploads a fresh copy under a new name")

	// Deep (cross-entity): 新映射 active，行数仍为 1（同键覆盖，无冲突）。
	items, err := h.Items.ListByOrder(context.Background(), newOrderID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	mapping, err := h.MappingByCloudCert("huawei", "acct-hw-1", items[0].NewCloudCertID)
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusActive, mapping.Status)
	rows, err := h.Mappings.ListByFingerprint(context.Background(), w.NewFP)
	require.NoError(t, err)
	assert.Len(t, rows, 1, "new and old mappings do not conflict (same unique key)")
}

// Contract outcome "already-deleted-success": 清理消费时云侧证书已被人工删除
// → 判定"已不存在"为清理成功语义，不报错不重试死循环，队列收敛。
func TestCleanupQueue_AlreadyDeletedIsIdempotentSuccess(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedCompensationWorld(t, h, domain.CloudHuawei, domain.ProductCDN, "acct-hw-1", "www.example.com")

	orderID := h.SeedOrder(domain.ChangeStatusPartialCompleted, nil, nil, w.OldFP, w.NewCertID)
	h.SeedItem(orderID, domain.ResourceRef{Channel: domain.ChannelCloudAPI, Cloud: "huawei",
		Product: "cdn", AccountKey: "acct-hw-1", ResourceID: "www.example.com"},
		domain.ItemStatusSuccess, 1, w.OldCloudIDs["huawei"])
	orphan := h.SeedMapping(w.NewFP, "huawei", "acct-hw-1", "scm-human-deleted")
	require.NoError(t, h.Mappings.UpdateStatus(context.Background(), orphan.ID.Hex(), domain.MappingStatusOrphan))
	// 云侧证书不存在（人工删除）—— stub 未注册该 ID，CleanupOrphan 幂等成功。

	consumed, err := h.Cleanup.SweepOrphans(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, consumed)

	// Output: 不报错（NoError 已断言）；清理结果 Success=true，队列收敛。
	results, err := h.Orphans.ListOrphanCleanup(context.Background(), orderID)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, service.OrphanActionCleanup, results[0].Action)
	assert.True(t, results[0].Success)
	_, err = h.MappingByCloudCert("huawei", "acct-hw-1", "scm-human-deleted")
	assert.Error(t, err, "mapping deleted, queue converged")

	// 再次清扫：无 orphan 记录可消费（不重试死循环）。
	consumed, err = h.Cleanup.SweepOrphans(context.Background())
	require.NoError(t, err)
	assert.Zero(t, consumed)
}

// Contract outcome "sweep-isolation": 清扫批次中某条删除失败 → 其余照常删除
// 收敛，失败记录保留 orphan 待重试并聚合上报；清扫不因单条失败中断。
func TestCleanupQueue_SweepIsolatesSingleFailure(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedCompensationWorld(t, h, domain.CloudHuawei, domain.ProductCDN, "acct-hw-1", "www.example.com")

	// 终态归属单（报告承载；非活跃 → 不占用）。
	h.SeedOrder(domain.ChangeStatusPartialCompleted, nil, nil, w.OldFP, w.NewCertID)
	// 两条 orphan 记录：其一云删除注入失败，另一正常。
	orphanA := h.SeedMapping(w.NewFP, "huawei", "acct-hw-1", "scm-dangling-copy-1")
	require.NoError(t, h.Mappings.UpdateStatus(context.Background(), orphanA.ID.Hex(), domain.MappingStatusOrphan))
	orphanB := h.SeedMapping(w.NewFP, "aws", "acct-aws-1", "arn:aws:acm:us-east-1:123456789012:certificate/dangling-2")
	require.NoError(t, h.Mappings.UpdateStatus(context.Background(), orphanB.ID.Hex(), domain.MappingStatusOrphan))
	h.Aws.SetCleanupErr("arn:aws:acm:us-east-1:123456789012:certificate/dangling-2",
		errors.New("aws acm: throttled"))

	consumed, err := h.Cleanup.SweepOrphans(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, consumed, "both records handled (one cleanup, one failure record)")

	// Output: 失败项保留 orphan 待重试，其余照常删除收敛。
	_, err = h.MappingByCloudCert("huawei", "acct-hw-1", "scm-dangling-copy-1")
	assert.Error(t, err, "healthy record deleted")
	kept, err := h.MappingByCloudCert("aws", "acct-aws-1",
		"arn:aws:acm:us-east-1:123456789012:certificate/dangling-2")
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusOrphan, kept.Status, "failing record kept for retry")

	// State: 失败结果 Success=false + 告警（不吞错）。
	results := h.Orphans.All()
	require.Len(t, results, 2)
	var failed bool
	for _, r := range results {
		if !r.Success {
			failed = true
			assert.Equal(t, service.OrphanActionCleanup, r.Action)
		}
	}
	assert.True(t, failed, "one failed cleanup result recorded")
	opsAlerts := 0
	for _, e := range h.Alerts.Events() {
		if e.Category == service.AlertCategoryOps {
			opsAlerts++
		}
	}
	require.Equal(t, 1, opsAlerts, "ops alert published for the failing record")
}
