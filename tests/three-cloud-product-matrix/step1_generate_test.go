// @feature cert-multicloud-deployers @api-functional
//
// Contract: three-cloud-product-matrix / Step 1 — 生成覆盖 9 组合的变更清单.
// Outcomes under test: all-9-combos-executable, empty-combo-produces-no-items,
// unauthorized.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package three_cloud_product_matrix

import (
	"context"
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Contract outcome "all-9-combos-executable": 9 组合引用均为可执行项
// （AutoChangeable=true），无一组合按 ERR_DISCOVERY_ONLY 标记 skipped。
func TestGenerateChangeList_AllNineCombosExecutable(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	combos := matrixCombos()
	oldFP, newCertID, _ := seedMatrixWorld(t, h, combos)

	list := h.MustGenerate(oldFP, newCertID)

	// Output: 9 个条目全部可执行（恒可执行判定无云差异）。
	require.Len(t, list.Items, len(combos), "one item per nine combos")
	seen := map[string]bool{}
	for _, item := range list.Items {
		assert.True(t, item.AutoChangeable,
			"combo %s/%s must be executable (no discovery-only partition)",
			item.Target.Cloud, item.Target.Product)
		assert.Empty(t, item.Reason)
		assert.Equal(t, string(domain.ActionUploadAndBind), item.Action)
		seen[item.Target.Cloud+"/"+item.Target.Product] = true
	}
	assert.Len(t, seen, 9, "all nine cloud×product combos present")

	// State: 变更单 pending_confirm；9 个条目以 pending 入清单。
	order, err := h.Orders.GetByID(context.Background(), list.OrderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusPendingConfirm, order.Status)
	items, err := h.Items.ListByOrder(context.Background(), list.OrderID)
	require.NoError(t, err)
	require.Len(t, items, 9)
	for _, item := range items {
		assert.Equal(t, domain.ItemStatusPending, item.Status)
		assert.NotEmpty(t, item.OldCloudCertID, "rollback anchor persisted per combo")
	}
}

// Contract outcome "empty-combo-produces-no-items": 某组合零引用 → 该组合不
// 产出条目（静默无条目，非 skipped、非报错），其余组合不受影响。
func TestGenerateChangeList_EmptyComboProducesNoItems(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	combos := matrixCombos()
	// Drop one combo ( azure alb ) → 其余 8 组合引用齐备。
	filtered := combos[:8]
	oldFP, newCertID, _ := seedMatrixWorld(t, h, filtered)

	list := h.MustGenerate(oldFP, newCertID)

	// Output: 仅 8 个条目，缺组合静默缺席（不报错、不误标 skipped）。
	require.Len(t, list.Items, 8)
	for _, item := range list.Items {
		assert.NotEqual(t, "azure/alb", item.Target.Cloud+"/"+item.Target.Product)
		assert.True(t, item.AutoChangeable)
		assert.NotContains(t, item.Reason, "K8S_", "absent combos are not error-marked items")
	}

	// State: 变更单仅含其余组合条目。
	items, err := h.Items.ListByOrder(context.Background(), list.OrderID)
	require.NoError(t, err)
	assert.Len(t, items, 8)
}

// Contract outcome "unauthorized": POST /changes 无有效会话 → HTTP 401 全局
// 认证失败文案（非 cert 信封）。
func TestGenerateChangeList_UnauthenticatedRejected401(t *testing.T) {
	h := multicloudtest.NewHarness(t, func(cfg *multicloudtest.Config) {
		cfg.Claims = nil
	})
	combos := matrixCombos()
	oldFP, newCertID, _ := seedMatrixWorld(t, h, combos)

	resp := h.GenerateChangeList(oldFP, newCertID)

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	assert.Contains(t, resp.Body, "认证失败")
	assert.Nil(t, resp.Env.Error, "raw middleware body carries no cert envelope error")

	// State: 无状态变化，不创建变更单。
	_, total, err := h.Orders.ListPage(context.Background(), "", 0, 10)
	require.NoError(t, err)
	assert.Zero(t, total, "no change order created")
}
