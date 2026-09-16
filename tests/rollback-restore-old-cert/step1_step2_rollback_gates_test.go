// @feature cert-multicloud-deployers @api-functional
//
// Contracts: rollback-restore-old-cert / Step 1 — 对已完成替换的条目发起回滚;
// Step 2 — GetCert 校验旧云证书仍有效.
// Outcomes under test: rollback-accepted, entry-state-invalid,
// scope-empty-rejected, unauthorized, precheck-pass, rollback-target-invalid,
// cross-cloud-form-mismatch-rejected.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package rollback_restore_old_cert

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	huaweicloud "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/huawei"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Contract outcome "rollback-accepted": 200 受理 —— 系统定位映射中的旧云证书
// ID 并按云解析，准备反绑；回滚编排启动（逐条云回读/绑定）。
func TestRollbackRequest_AcceptedForCompletedReplacement(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedRollbackWorld(t, h, domain.CloudHuawei, domain.ProductCDN)
	orderID, successID := seedReplacedState(t, h, w, domain.CloudHuawei, domain.ProductCDN)

	ack := h.MustRollback(orderID, successID)
	assert.Equal(t, orderID, ack.OrderID)

	// State: 回滚编排启动 —— 条目收敛 rolled_back，旧证书获得保护期。
	item, err := h.Items.GetByID(context.Background(), successID)
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusRolledBack, item.Status)
	order, err := h.Orders.GetByID(context.Background(), orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusRolledBack, order.Status)
	assert.Empty(t, order.ActiveMutex, "terminal convergence cleared the mutex token")

	// 反绑按旧云证书 ID 执行（与正向替换同一绑定 API）。
	binds := h.Huawei.BindRecords()
	require.Len(t, binds, 1)
	assert.Equal(t, w.OldCloudID, binds[0].CloudCertID, "rebind targets the old cloud cert ID")
	// 按云解析旧云证书 ID：GetCert 只读校验命中该 ID。
	assert.Contains(t, h.Huawei.GetCalls(), w.OldCloudID)
}

// Contract outcome "entry-state-invalid": 变更单状态不满足回滚入口校验
// （非收敛态）→ 409 CHANGE_STATE_CONFLICT，无回滚副作用。
func TestRollbackRequest_EntryStateInvalidRejected(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedRollbackWorld(t, h, domain.CloudHuawei, domain.ProductCDN)

	// completed 为非收敛态（白名单拒绝）。
	orderID := h.SeedOrder(domain.ChangeStatusCompleted, nil, nil, w.OldFP, w.NewCertID)
	successID := h.SeedItem(orderID, cloudRef(w, domain.CloudHuawei, domain.ProductCDN),
		domain.ItemStatusSuccess, 1, w.OldCloudID, w.NewCloudID)

	resp := h.Rollback(orderID, successID)

	require.Equal(t, http.StatusConflict, resp.StatusCode, resp.Body)
	require.NotNil(t, resp.Env.Error)
	assert.Equal(t, "CHANGE_STATE_CONFLICT", resp.Env.Error.Code)

	// State: 变更单与条目状态均不变，无云调用。
	order, err := h.Orders.GetByID(context.Background(), orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusCompleted, order.Status)
	item, err := h.Items.GetByID(context.Background(), successID)
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusSuccess, item.Status)
	assert.Empty(t, h.Huawei.BindRecords(), "no rebind attempted")
}

// Contract outcome "scope-empty-rejected": 空条目列表 / 全为已回滚条目 →
// 400 INVALID_REQUEST，不产生回滚副作用。
func TestRollbackRequest_ScopeEmptyOrNoSuccessItemsRejected(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedRollbackWorld(t, h, domain.CloudHuawei, domain.ProductCDN)
	orderID := seedTerminalRolledBackState(t, h, w, domain.CloudHuawei, domain.ProductCDN)

	// 空列表：handler 层 400。
	emptyResp := h.Post(multicloudtest.RouteChangeByID+orderID+multicloudtest.RouteRollback,
		map[string]any{"itemIds": []string{}})
	require.Equal(t, http.StatusBadRequest, emptyResp.StatusCode, emptyResp.Body)
	require.NotNil(t, emptyResp.Env.Error)
	assert.Equal(t, "INVALID_REQUEST", emptyResp.Env.Error.Code)
	assert.Contains(t, emptyResp.Env.Error.Message, "itemIds is required")

	// 全为已回滚条目：范围准入仅 success → 400 无可回滚范围。
	items, err := h.Items.ListByOrder(context.Background(), orderID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	resp := h.Rollback(orderID, items[0].ID.Hex())
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, resp.Body)
	require.NotNil(t, resp.Env.Error)
	assert.Equal(t, "INVALID_REQUEST", resp.Env.Error.Code)

	// State: 无重复绑定副作用（幂等：结果与首次回滚一致）。
	assert.Empty(t, h.Huawei.BindRecords(), "no duplicate rebind side effect")
	item, err := h.Items.GetByID(context.Background(), items[0].ID.Hex())
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusRolledBack, item.Status)
}

// Contract outcome "unauthorized": rollback 无有效会话 → HTTP 401 全局认证
// 失败文案；回滚编排不启动。
func TestRollbackRequest_UnauthenticatedRejected401(t *testing.T) {
	h := multicloudtest.NewHarness(t, func(cfg *multicloudtest.Config) {
		cfg.Claims = nil
	})
	w := seedRollbackWorld(t, h, domain.CloudHuawei, domain.ProductCDN)
	orderID, successID := seedReplacedState(t, h, w, domain.CloudHuawei, domain.ProductCDN)

	resp := h.Rollback(orderID, successID)

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.Contains(t, resp.Body, "认证失败")
	assert.Nil(t, resp.Env.Error, "raw middleware body, not the cert envelope")

	// State: 无状态变化，回滚编排不启动。
	item, err := h.Items.GetByID(context.Background(), successID)
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusSuccess, item.Status)
	assert.Empty(t, h.Huawei.GetCalls(), "no GetCert precheck ran")
}

// Contract outcome "precheck-pass": 旧证书存在、有效期晚于当前、指纹与变更单
// 旧证书指纹一致 → 预检通过后才继续回滚（只读校验，无状态迁移）。
func TestRollbackPrecheck_PassesForValidOldCert(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedRollbackWorld(t, h, domain.CloudHuawei, domain.ProductCDN)
	orderID, successID := seedReplacedState(t, h, w, domain.CloudHuawei, domain.ProductCDN)

	h.MustRollback(orderID, successID)

	// Output: GetCert 只读回读命中旧云证书 ID。
	require.Contains(t, h.Huawei.GetCalls(), w.OldCloudID)

	// State: 预检通过后回滚继续 —— 条目 rolled_back（回滚闭环由绑定结果判定）。
	item, err := h.Items.GetByID(context.Background(), successID)
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusRolledBack, item.Status)
}

// Contract outcome "rollback-target-invalid": 云侧旧证书已删除（Exists=false）
// → 409 ROLLBACK_TARGET_INVALID 显式失败不猜测；无状态变更；审计记录回滚目标无效。
func TestRollbackPrecheck_TargetInvalidBlocksWholeRollback(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedRollbackWorld(t, h, domain.CloudHuawei, domain.ProductCDN)
	// 云侧旧证书已被人工删除：覆写 stub 在库状态 → GetCert Exists=false。
	h.Huawei.SetCertInfo(w.OldCloudID, huaweicloud.CloudCertInfo{})
	orderID, successID := seedReplacedState(t, h, w, domain.CloudHuawei, domain.ProductCDN)

	resp := h.Rollback(orderID, successID)

	// Output: 409 显式失败，不盲绑到无效证书。
	require.Equal(t, http.StatusConflict, resp.StatusCode, resp.Body)
	require.NotNil(t, resp.Env.Error)
	assert.Equal(t, "ROLLBACK_TARGET_INVALID", resp.Env.Error.Code)
	assert.Empty(t, h.Huawei.BindRecords(), "no blind rebind to an invalid target")

	// State: 全部条目与变更单状态不变（无半回滚状态）。
	item, err := h.Items.GetByID(context.Background(), successID)
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusSuccess, item.Status)
	order, err := h.Orders.GetByID(context.Background(), orderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusExecuting, order.Status)

	// Audit: 回滚目标无效事件已记录（rollback_target_invalid）。
	audit := h.Audit(orderID)
	require.Equal(t, http.StatusOK, audit.StatusCode)
	var payload multicloudtest.AuditPayload
	require.NoError(t, json.Unmarshal(audit.Env.Data, &payload))
	found := false
	for _, l := range payload.Logs {
		if l.Action == "rollback" && strings.Contains(l.Detail, "rollback_target_invalid") {
			found = true
		}
	}
	assert.True(t, found, "rollback_target_invalid audit event recorded")
}

// Contract outcome "cross-cloud-form-mismatch-rejected": 旧 ID 形态/凭据云域
// 与目标云不匹配（如 ACM ARN 交由华为部署器解析）→ 显式拒绝，不误判有效。
func TestRollbackPrecheck_CrossCloudCredentialMismatchRejected(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedRollbackWorld(t, h, domain.CloudHuawei, domain.ProductCDN)
	orderID, successID := seedReplacedState(t, h, w, domain.CloudHuawei, domain.ProductCDN)

	// 注入凭据云域错配：华为条目拿到 AWS 凭据 → 部署器 cloudAccountFor 拒绝。
	h.Creds.MismatchCloud = "huawei"

	resp := h.Rollback(orderID, successID)

	// Output: 显式失败（fail closed），不误判有效、不跨云混用。
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode, resp.Body)
	assert.Empty(t, h.Huawei.BindRecords(), "no cross-cloud bind")
	assert.Empty(t, h.Huawei.GetCalls(), "deployer rejected the credential before any cloud call")

	// State: 无状态迁移。
	item, err := h.Items.GetByID(context.Background(), successID)
	require.NoError(t, err)
	assert.Equal(t, domain.ItemStatusSuccess, item.Status)
}

// jsonUnmarshalAudit removed: the audit payload decodes inline above.
