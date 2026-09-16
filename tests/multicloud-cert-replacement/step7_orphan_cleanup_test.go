// @feature cert-multicloud-deployers @api-functional
//
// Contract: multicloud-cert-replacement / Step 7 — 旧证书孤儿清理.
// Outcomes under test: success, owning-order-occupancy-skip, protection-period-skipkeep.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multicloud_cert_replacement

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Contract outcome "success": 终态单收敛已将成功条目旧证书映射 active→orphan
// 入清理队列；队列消费删除云侧孤儿证书并删除映射记录（cleaned 以删除承载），
// 结果 Action=cleanup 且 Success=true。
func TestOrphanCleanup_ConsumesOrphanAfterTerminalState(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := replacementWorld{Domain: "www.example.com"}
	w.OldCertID, w.OldFP = h.SeedCompleteCert("old.example.com", w.Domain)
	w.NewCertID, w.NewFP = h.SeedCompleteCert("new.example.com", w.Domain)

	// Terminal order with a success item whose old cloud cert was replaced.
	w.OldCloudIDs = map[string]string{"huawei": "scm-old-cert-0001"}
	orderID := h.SeedOrder(domain.ChangeStatusCompleted, nil, nil, w.OldFP, w.NewCertID)
	h.SeedItem(orderID, domain.ResourceRef{Channel: domain.ChannelCloudAPI, Cloud: "huawei",
		Product: "cdn", AccountKey: "acct-hw-1", ResourceID: "www.example.com"},
		domain.ItemStatusSuccess, 1, w.OldCloudIDs["huawei"])

	// Old cert mapping still active ( enqueue happens at consumption time ),
	// no protection period on the old certificate.
	h.SeedMapping(w.OldFP, "huawei", "acct-hw-1", w.OldCloudIDs["huawei"])

	// Output: 事件触发消费（验证窗口终态后即时消费该单清理队列）。
	consumed, err := h.Cleanup.ConsumeOrderQueue(context.Background(), orderID)
	require.NoError(t, err)
	assert.Equal(t, 1, consumed)

	// Deep (cross-entity): 旧证书映射 active→orphan→删除（队列与映射收敛一致）。
	_, err = h.MappingByCloudCert("huawei", "acct-hw-1", w.OldCloudIDs["huawei"])
	assert.Error(t, err, "consumed orphan mapping is deleted (cleaned carried by deletion)")
	assert.Empty(t, h.OrphanMappings(), "no orphan rows left in the queue")

	// 云侧删除调用发生（华为 SCM 删除 API 路由）。
	assert.Contains(t, h.Huawei.CleanupCalls(), w.OldCloudIDs["huawei"])

	// 结果记录 Action=cleanup 且 Success=true，报告可见。
	results, err := h.Orphans.ListOrphanCleanup(context.Background(), orderID)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, service.OrphanActionCleanup, results[0].Action)
	assert.True(t, results[0].Success)
	assert.Equal(t, w.OldCloudIDs["huawei"], results[0].CloudCertID)

	detail := h.MustDetail(orderID)
	require.NotNil(t, detail.Report)
	require.Len(t, detail.Report.OrphanCleanup, 1)
	assert.Equal(t, service.OrphanActionCleanup, detail.Report.OrphanCleanup[0].Action)
	assert.True(t, detail.Report.OrphanCleanup[0].Success)
}

// Contract outcome "owning-order-occupancy-skip": 待清理旧证书仍被在途变更单
// 占用（active 互斥令牌或新证书）→ 孤儿判定跳过不删除，队列状态收敛不误删。
func TestOrphanCleanup_SkipsWhenOwnedByInFlightOrder(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := replacementWorld{Domain: "www.example.com"}
	w.OldCertID, w.OldFP = h.SeedCompleteCert("old.example.com", w.Domain)
	w.NewCertID, w.NewFP = h.SeedCompleteCert("new.example.com", w.Domain)

	// The old cert is the mutex token of an in-flight executing order.
	w.OldCloudIDs = map[string]string{"huawei": "scm-old-cert-0001"}
	h.SeedOrder(domain.ChangeStatusExecuting, seededBatchInfo(1), nil, w.OldFP, w.NewCertID)
	orphan := h.SeedMapping(w.OldFP, "huawei", "acct-hw-1", w.OldCloudIDs["huawei"])
	// Mark it orphan directly ( already enqueued by a previous convergence ).
	require.NoError(t, h.Mappings.UpdateStatus(context.Background(), orphan.ID.Hex(), domain.MappingStatusOrphan))

	consumed, err := h.Cleanup.SweepOrphans(context.Background())
	require.NoError(t, err)
	assert.Zero(t, consumed, "occupied orphan is skipped before any cloud call")

	// State: 映射保持 orphan 留队列；无云侧删除、无清理结果。
	kept, err := h.MappingByCloudCert("huawei", "acct-hw-1", w.OldCloudIDs["huawei"])
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusOrphan, kept.Status)
	assert.Empty(t, h.Huawei.CleanupCalls(), "no cloud deletion attempted")
	assert.Empty(t, h.Orphans.All(), "no cleanup record for a silent occupancy skip")
}

// Contract outcome "protection-period-skipkeep": 保护期未到期（如回滚后 7 天
// 保护窗内）→ skip_keep 且记成功、不删除、不报错。
func TestOrphanCleanup_ProtectPeriodSkipKeep(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := replacementWorld{Domain: "www.example.com"}
	w.OldCertID, w.OldFP = h.SeedCompleteCert("old.example.com", w.Domain)
	w.NewCertID, w.NewFP = h.SeedCompleteCert("new.example.com", w.Domain)
	w.OldCloudIDs = map[string]string{"huawei": "scm-old-cert-0001"}

	// Terminal rolled-back order: its replaced new-cert mapping sits in the
	// cleanup queue while the certificate carries an active protection period.
	orderID := h.SeedOrder(domain.ChangeStatusRolledBack, nil, nil, w.OldFP, w.NewCertID)
	h.SeedItem(orderID, domain.ResourceRef{Channel: domain.ChannelCloudAPI, Cloud: "huawei",
		Product: "cdn", AccountKey: "acct-hw-1", ResourceID: "www.example.com"},
		domain.ItemStatusSuccess, 1, w.OldCloudIDs["huawei"])

	orphan := h.SeedMapping(w.NewFP, "huawei", "acct-hw-1", "scm-new-cert-copy")
	require.NoError(t, h.Mappings.UpdateStatus(context.Background(), orphan.ID.Hex(), domain.MappingStatusOrphan))
	// 保护期未到期（回滚完成的保护窗）。
	h.SeedProtectUntil(w.NewFP, time.Now().Add(7*24*time.Hour))

	consumed, err := h.Cleanup.SweepOrphans(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, consumed, "skip_keep produces a recorded result")

	// Output: 按保护期跳过保留（skip_keep 且记成功、不删除、不报错）。
	results, err := h.Orphans.ListOrphanCleanup(context.Background(), orderID)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, service.OrphanActionSkipKeep, results[0].Action)
	assert.True(t, results[0].Success, "skip_keep is a success outcome, not an error")
	assert.Equal(t, "scm-new-cert-copy", results[0].CloudCertID)

	// State: 证书保留；映射保持 orphan 留队列（保护期结束后的清扫再消费）。
	kept, err := h.MappingByCloudCert("huawei", "acct-hw-1", "scm-new-cert-copy")
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusOrphan, kept.Status)
	assert.Empty(t, h.Huawei.CleanupCalls(), "protected cert is never deleted")
}
