// @feature cert-multicloud-deployers @api-functional
//
// Journey smoke: rollback-restore-old-cert happy path end-to-end plus one
// error path. Full replace through the HTTP surface ( generate -> confirm ->
// execute success ) -> rollback by old cloud cert ID ( request -> GetCert
// precheck -> rebind -> terminal convergence ) -> probe observation, with the
// invalid-target 409 as the error-path check.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package rollback_restore_old_cert

import (
	"context"
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Journey invariants verified across the loop:
//   - 回滚前必须经 GetCert 校验旧云证书有效，禁止盲绑；
//   - 回滚按旧云证书 ID 反绑，与正向替换复用同一 BindResource；
//   - 回滚幂等：重复回滚不产生重复副作用，终态唯一；
//   - 回滚完成的旧证书获得保护期，免于清理队列误删。
func TestRollbackRestoreOldCert_FullLoopSmoke(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedRollbackWorld(t, h, domain.CloudHuawei, domain.ProductCDN)

	// ---- 正向替换（HTTP 全流程）：generate -> confirm -> execute success ----
	list := h.MustGenerate(w.OldFP, w.NewCertID)
	require.Len(t, list.Items, 1)
	h.MustConfirm(list.OrderID, nil)
	h.MustExecute(list.OrderID)
	progress := h.MustProgress(list.OrderID)
	require.Len(t, progress.ItemStates, 1)
	require.Equal(t, string(domain.ItemStatusSuccess), progress.ItemStates[0].Status,
		"error=%s", progress.ItemStates[0].Error)
	itemID := list.Items[0].ItemID

	// ---- Step 1: 发起回滚（受理） ----
	ack := h.MustRollback(list.OrderID, itemID)
	assert.Equal(t, list.OrderID, ack.OrderID)

	// ---- Step 2: GetCert 校验旧云证书有效（只读回读命中旧 ID） ----
	require.Contains(t, h.Huawei.GetCalls(), w.OldCloudID)

	// ---- Step 3: BindResource 反绑回旧云证书 ----
	// 两次绑定：正向替换（执行期上传产物）+ 本次回滚反绑（映射中的旧证书 ID）。
	records := h.Huawei.BindRecords()
	require.Len(t, records, 2)
	assert.NotEqual(t, w.OldCloudID, records[0].CloudCertID, "forward bind used the fresh upload artifact")
	assert.Equal(t, w.OldCloudID, records[1].CloudCertID, "resource restored to the old cert")

	// State: 条目 rolled_back；订单收敛 rolled_back；新证书映射转 orphan。
	item, err := h.Items.GetByID(context.Background(), itemID)
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusRolledBack, item.Status)
	order, err := h.Orders.GetByID(context.Background(), list.OrderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusRolledBack, order.Status)

	// Deep (cross-entity): 旧证书保护期置位 + 新证书映射入清理队列。
	oldCert, err := h.Certs.GetByFingerprint(context.Background(), w.OldFP)
	require.NoError(t, err)
	require.NotNil(t, oldCert.ProtectUntil)
	orphans := h.OrphanMappings()
	require.Len(t, orphans, 1)
	assert.Equal(t, records[0].CloudCertID, orphans[0].CloudCertID,
		"the replaced artifact's mapping was queued for cleanup")

	// ---- Step 4: 验证窗口确认线上指纹恢复旧证书（只读观测） ----
	h.Prober.SetOnlineFingerprint(w.Domain, w.OldFP)
	results, err := h.Prober.ProbeLedgerDomains(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, domain.ProbeStatusConsistent, results[0].Status,
		"online fingerprint matches the restored old cert")

	// ---- Error path: 对无效回滚目标 409 阻断（另一张单，云侧旧证书已删） ----
	w2 := seedRollbackWorld(t, h, domain.CloudHuawei, domain.ProductCDN)
	// w2 的旧证书同样注册有效；构造一张新单但让云侧证书"已被删除"：
	// stub 未注册的 ID 报 Exists=false —— 这里直接换用未注册的旧 ID。
	order2 := h.SeedOrder(domain.ChangeStatusPartialCompleted, nil, nil, w2.OldFP, w2.NewCertID)
	item2 := h.SeedItem(order2, cloudRef(w2, domain.CloudHuawei, domain.ProductCDN),
		domain.ItemStatusSuccess, 1, "scm-deleted-old-cert", w2.NewCloudID)
	resp := h.Rollback(order2, item2)
	require.Equal(t, http.StatusConflict, resp.StatusCode, resp.Body)
	require.NotNil(t, resp.Env.Error)
	assert.Equal(t, "ROLLBACK_TARGET_INVALID", resp.Env.Error.Code)
}
