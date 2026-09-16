// @feature cert-multicloud-deployers @api-functional
//
// Contracts: rollback-restore-old-cert / Step 3 — BindResource 反绑回旧云证书;
// Step 4 — 验证窗口确认线上指纹恢复旧证书.
// Outcomes under test: rebind-success, repeated-rollback-idempotent,
// multi-resource-all-rebound, probe-consistent-with-old,
// cleanup-queue-race-protected, probe-lag-converges.
//
// 事实对齐：回滚闭环由绑定结果与条目状态判定（无独立回滚验证窗口）；拨测为
// 只读观测，不反向改变回滚终态。
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package rollback_restore_old_cert

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Contract outcome "rebind-success": 反绑回旧云证书（与正向替换同一 BindResource
// API 与产品分支语义）→ 资源恢复引用旧证书，条目 rolled_back；新证书映射转
// orphan 入清理队列。
func TestRebind_RestoresOldCloudCertReference(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedRollbackWorld(t, h, domain.CloudHuawei, domain.ProductCDN)
	orderID, successID := seedReplacedState(t, h, w, domain.CloudHuawei, domain.ProductCDN)

	h.MustRollback(orderID, successID)

	// Output: 绑定调用恢复旧证书（显式成功才收敛 rolled_back）。
	records := h.Huawei.BindRecords()
	require.Len(t, records, 1)
	assert.Equal(t, "cdn", records[0].Product, "same product branch as the forward replacement")
	assert.Equal(t, w.ResourceID, records[0].ResourceID)
	assert.Equal(t, w.OldCloudID, records[0].CloudCertID, "rebound to the old cloud cert ID")

	// State: 条目 rolled_back；变更单收敛 rolled_back；被替换新证书映射转 orphan。
	item, err := h.Items.GetByID(context.Background(), successID)
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusRolledBack, item.Status)
	order, err := h.Orders.GetByID(context.Background(), orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusRolledBack, order.Status)
	orphans := h.OrphanMappings()
	require.Len(t, orphans, 1)
	assert.Equal(t, w.NewCloudID, orphans[0].CloudCertID, "replaced new cert mapping queued for cleanup")

	// Deep: 旧证书获得回滚保护期（protectUntil 随终态固化）。
	oldCert, err := h.Certs.GetByFingerprint(context.Background(), w.OldFP)
	require.NoError(t, err)
	require.NotNil(t, oldCert.ProtectUntil)
}

// Contract outcome "repeated-rollback-idempotent": 重复回滚（所选条目均已
// rolled_back）→ 无可回滚范围 400，不产生重复绑定副作用，终态唯一。
func TestRebind_RepeatedRollbackIsIdempotent(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedRollbackWorld(t, h, domain.CloudHuawei, domain.ProductCDN)
	orderID := seedTerminalRolledBackState(t, h, w, domain.CloudHuawei, domain.ProductCDN)

	items, err := h.Items.ListByOrder(context.Background(), orderID)
	require.NoError(t, err)
	resp := h.Rollback(orderID, items[0].ID.Hex())

	// Output: 400 无可回滚范围，无重复绑定副作用。
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, resp.Body)
	assert.Empty(t, h.Huawei.BindRecords(), "no duplicate rebind side effect")

	// State: 终态唯一 —— 资源仍引用旧证书，条目保持 rolled_back。
	item, err := h.Items.GetByID(context.Background(), items[0].ID.Hex())
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusRolledBack, item.Status)
	order, err := h.Orders.GetByID(context.Background(), orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusPartialCompleted, order.Status)
}

// Contract outcome "multi-resource-all-rebound": 多资源条目批量回滚 → 全部
// 反绑回旧证书；任一条目失败 → 该条目 rollback_failed + 立即告警，变更单
// 收敛 rollback_failed（不静默混合）。
func TestRebind_MultiResourceAllReboundOrExplicitFailure(t *testing.T) {
	t.Run("all rebind succeeds", func(t *testing.T) {
		h := multicloudtest.NewHarness(t, nil)
		w := seedRollbackWorld(t, h, domain.CloudHuawei, domain.ProductCDN)
		orderID := h.SeedOrder(domain.ChangeStatusPartialCompleted, nil, nil, w.OldFP, w.NewCertID)
		ref := cloudRef(w, domain.CloudHuawei, domain.ProductCDN)
		id1 := h.SeedItem(orderID, ref, domain.ItemStatusSuccess, 1, w.OldCloudID, w.NewCloudID)
		id2 := h.SeedItem(orderID, ref, domain.ItemStatusSuccess, 1, w.OldCloudID, w.NewCloudID)

		h.MustRollback(orderID, id1, id2)

		// Output: 所有已绑定资源条目均反绑回旧证书，无遗漏。
		records := h.Huawei.BindRecords()
		require.Len(t, records, 2)
		for _, r := range records {
			assert.Equal(t, w.OldCloudID, r.CloudCertID)
		}
		for _, id := range []string{id1, id2} {
			item, err := h.Items.GetByID(context.Background(), id)
			require.NoError(t, err)
			assert.Equal(t, domain.ItemStatusRolledBack, item.Status)
		}
		order, err := h.Orders.GetByID(context.Background(), orderID)
		require.NoError(t, err)
		assert.Equal(t, domain.ChangeStatusRolledBack, order.Status)
	})

	t.Run("one rebind failure fails loudly", func(t *testing.T) {
		h := multicloudtest.NewHarness(t, nil)
		w := seedRollbackWorld(t, h, domain.CloudHuawei, domain.ProductCDN)
		orderID := h.SeedOrder(domain.ChangeStatusPartialCompleted, nil, nil, w.OldFP, w.NewCertID)
		ref := cloudRef(w, domain.CloudHuawei, domain.ProductCDN)
		id1 := h.SeedItem(orderID, ref, domain.ItemStatusSuccess, 1, w.OldCloudID, w.NewCloudID)
		id2 := h.SeedItem(orderID, ref, domain.ItemStatusSuccess, 1, w.OldCloudID, w.NewCloudID)

		// 回滚按项 ID 稳定排序执行：注入第二次绑定调用失败。
		ids := []string{id1, id2}
		sort.Strings(ids)
		h.Huawei.SetBindErr(2, errors.New("huawei cdn: listener update rejected"))

		h.MustRollback(orderID, ids...) // 项级失败为异步子任务状态口径，不以同步错误中断

		// Output: 成功项 rolled_back；失败项 rollback_failed；订单收敛 rollback_failed。
		first, err := h.Items.GetByID(context.Background(), ids[0])
		require.NoError(t, err)
		assert.Equal(t, domain.ItemStatusRolledBack, first.Status)
		second, err := h.Items.GetByID(context.Background(), ids[1])
		require.NoError(t, err)
		assert.Equal(t, domain.ItemStatusRollbackFailed, second.Status)
		assert.NotEmpty(t, second.Error, "failure reason persisted")

		// 立即告警（四类之"回滚失败"，不得静默）。
		found := false
		for _, e := range h.Alerts.Events() {
			if e.Category == service.AlertCategoryRollbackFailed {
				found = true
			}
		}
		assert.True(t, found, "rollback-failure alert published immediately")
	})
}

// Contract outcome "probe-consistent-with-old": 拨测线上指纹与旧证书台账指纹
// 一致 → 分类 consistent；回滚终态不变（拨测为只读观测）。
func TestRollbackObservation_ProbeConsistentWithOldCert(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedRollbackWorld(t, h, domain.CloudHuawei, domain.ProductCDN)
	orderID, successID := seedReplacedState(t, h, w, domain.CloudHuawei, domain.ProductCDN)
	h.MustRollback(orderID, successID)

	// 拨测线上指纹回到旧证书（旧证书持有该域名 SAN → 分类 consistent）。
	h.Prober.SetOnlineFingerprint(w.Domain, w.OldFP)
	results, err := h.Prober.ProbeLedgerDomains(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, w.OldFP, results[0].OnlineFingerprint)
	assert.Equal(t, domain.ProbeStatusConsistent, results[0].Status)

	// State: 回滚终态不变（条目 rolled_back、订单终态）。
	item, err := h.Items.GetByID(context.Background(), successID)
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusRolledBack, item.Status)
}

// Contract outcome "cleanup-queue-race-protected": 回滚发起时旧证书恰在清理
// 队列（orphan）→ 回滚完成后旧证书获得保护期，清理消费按保护期跳过保留、
// 不删除；不出现回滚成功但旧证书已删的矛盾终态。
func TestRollbackObservation_CleanupQueueRaceProtected(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedRollbackWorld(t, h, domain.CloudHuawei, domain.ProductCDN)
	orderID, successID := seedReplacedState(t, h, w, domain.CloudHuawei, domain.ProductCDN)

	// 旧证书恰在清理队列中（映射已标记 orphan）但尚未被删除。
	orphan := h.SeedMapping(w.OldFP, "huawei", w.AccountKey, w.OldCloudID)
	require.NoError(t, h.Mappings.UpdateStatus(context.Background(), orphan.ID.Hex(), domain.MappingStatusOrphan))

	h.MustRollback(orderID, successID)

	// Output: 回滚完成后旧证书获得保护期 → 清理消费 skip_keep 不删除；
	// 被替换的新证书（无保护）照常清理 —— 一轮消费两条记录（1 skip_keep + 1 cleanup）。
	consumed, err := h.Cleanup.SweepOrphans(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, consumed)
	kept, err := h.MappingByCloudCert("huawei", w.AccountKey, w.OldCloudID)
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusOrphan, kept.Status, "old cert mapping kept in the queue")
	assert.NotContains(t, h.Huawei.CleanupCalls(), w.OldCloudID, "the rollback target is never deleted")
	skips := 0
	for _, r := range h.Orphans.All() {
		if r.Action == service.OrphanActionSkipKeep {
			skips++
		}
	}
	assert.Equal(t, 1, skips, "protection-period skip_keep recorded")

	// State: 旧证书台账 protectUntil 置位（幂等只延长）。
	oldCert, err := h.Certs.GetByFingerprint(context.Background(), w.OldFP)
	require.NoError(t, err)
	require.NotNil(t, oldCert.ProtectUntil)

	// 被替换的新证书映射已转 orphan 待后续清理（保护期外再清）。
	_, err = h.MappingByCloudCert("huawei", w.AccountKey, w.NewCloudID)
	assert.Error(t, err, "replaced new cert mapping consumed after rollback")
}

// Contract outcome "probe-lag-converges": 反绑完成但边缘指纹同步滞后 → 滞后
// 期间差异仅记 diff 分类与常规告警，后续轮次收敛；不改变回滚终态。
func TestRollbackObservation_ProbeLagConvergesWithoutStateChange(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedRollbackWorld(t, h, domain.CloudHuawei, domain.ProductCDN)
	orderID, successID := seedReplacedState(t, h, w, domain.CloudHuawei, domain.ProductCDN)
	h.MustRollback(orderID, successID)

	// 滞后期间：线上指纹未回到旧证书（边缘残留的第三方证书指纹）→ diff
	//（不误判回滚失败；台账新旧两证均持有该域名，须用非台账指纹构成 diff）。
	lagFP := multicloudtest.FP("edge-lag-fingerprint")
	h.Prober.SetOnlineFingerprint(w.Domain, lagFP)
	results, err := h.Prober.ProbeLedgerDomains(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, domain.ProbeStatusDiff, results[0].Status)

	// 收敛：线上指纹回到旧证书 → consistent。
	h.Prober.SetOnlineFingerprint(w.Domain, w.OldFP)
	results, err = h.Prober.ProbeLedgerDomains(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, domain.ProbeStatusConsistent, results[0].Status)

	// State: 回滚终态不变 —— 不因单次拨测不一致误判回滚失败。
	item, err := h.Items.GetByID(context.Background(), successID)
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusRolledBack, item.Status)
	order, err := h.Orders.GetByID(context.Background(), orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusRolledBack, order.Status)
}
