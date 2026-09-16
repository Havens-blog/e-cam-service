// @feature cert-multicloud-deployers @api-functional
//
// Contract: multicloud-cert-replacement / Step 1 — 为三云证书引用生成变更清单.
// Outcomes under test: success, k8s-managed-nonexecutable, unauthorized.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multicloud_cert_replacement

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Contract outcome "success": 201 变更清单 VO，三云引用条目 AutoChangeable=true，
// 与既有云引用同栏呈现；无 ERR_DISCOVERY_ONLY 分区；订单 pending_confirm 且
// ActiveMutex=旧证书指纹；三云条目以 pending 状态入清单。
func TestGenerateChangeList_ThreeCloudReferencesExecutable(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedThreeCloudReplacement(t, h)

	list := generateFor(t, h, w)

	// Output: 三云条目全部可执行（云通道判定恒可执行），均为 upload_and_bind。
	require.Len(t, list.Items, 3, "one item per three-cloud reference")
	seen := map[string]bool{}
	for _, item := range list.Items {
		assert.True(t, item.AutoChangeable,
			"cloud_api item %s must be auto-changeable (no discovery-only partition)", item.ItemID)
		assert.Empty(t, item.Reason, "executable items carry no reason")
		assert.Equal(t, string(domain.ActionUploadAndBind), item.Action)
		assert.Equal(t, string(domain.ChannelCloudAPI), item.Target.Channel)
		seen[item.Target.Cloud] = true
	}
	assert.True(t, seen["huawei"] && seen["aws"] && seen["azure"],
		"all three clouds present in the list: %v", seen)
	assert.NotContains(t, strings.Join(list.Warnings, "\n"), "ERR_DISCOVERY_ONLY",
		"discovery-only partition is removed with the three deployers")
	assert.True(t, list.SANCheck.Passed, "new cert SANs cover the old cert SANs")

	// State: 订单 pending_confirm 且 ActiveMutex=旧证书指纹；条目 pending 入库。
	order, err := h.Orders.GetByID(context.Background(), list.OrderID)
	require.NoError(t, err)
	assert.Equal(t, domain.ChangeStatusPendingConfirm, order.Status)
	assert.Equal(t, w.OldFP, order.ActiveMutex, "active mutex token holds the old fingerprint")
	assert.Equal(t, w.SnapshotID, order.SnapshotID, "order binds the latest done snapshot")

	items, err := h.Items.ListByOrder(context.Background(), list.OrderID)
	require.NoError(t, err)
	require.Len(t, items, 3)
	for _, item := range items {
		assert.Equal(t, domain.ItemStatusPending, item.Status, "items enter the list as pending")
		// Deep (cross-entity): every persisted item keeps the old cloud cert
		// ID as the rollback anchor from its snapshot reference.
		assert.NotEmpty(t, item.OldCloudCertID, "rollback anchor persisted from the reference")
		assert.Contains(t, w.OldCloudIDs, item.ResourceRef.Cloud)
		assert.Equal(t, w.OldCloudIDs[item.ResourceRef.Cloud], item.OldCloudCertID)
	}
}

// Contract outcome "k8s-managed-nonexecutable": K8s 托管引用条目 skipped 且
// Error 记 K8S_MANAGEMENT_* 原因前缀，不入执行分母；三云引用仍可执行。
func TestGenerateChangeList_K8sManagedReferenceSkipped(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedThreeCloudReplacement(t, h)

	// Fresh snapshot carrying both the huawei cloud ref and a K8s-managed
	// CRD reference ( probe stub answers "managed signal" for K8s targets ).
	w.SnapshotID = h.SeedDoneSnapshotWithRefs([]multicloudtest.RefSpec{
		{Cloud: domain.CloudHuawei, Product: domain.ProductCDN, AccountKey: "acct-hw-1",
			ResourceID: "www.example.com", CloudCertID: w.OldCloudIDs["huawei"], Fingerprint: w.OldFP},
		{Product: domain.ProductCRD, ClusterID: "cluster-1", Namespace: "ingress-nginx",
			Kind: "Ingress", ResourceID: "web-ingress", Fingerprint: w.OldFP},
	}, time.Now().Add(time.Second))

	list := generateFor(t, h, w)

	require.Len(t, list.Items, 2, "cloud item + k8s item")
	var cloudItem, k8sItem *multicloudtest.ChangeListItemPayload
	for i := range list.Items {
		if list.Items[i].Target.Channel == string(domain.ChannelK8sAPI) ||
			list.Items[i].Target.Product == string(domain.ProductCRD) {
			k8sItem = &list.Items[i]
		} else {
			cloudItem = &list.Items[i]
		}
	}
	require.NotNil(t, cloudItem, "cloud_api item present")
	require.NotNil(t, k8sItem, "k8s item present")

	// K8s item: skipped + K8S_MANAGEMENT_* reason, not auto-changeable.
	assert.False(t, k8sItem.AutoChangeable)
	assert.True(t, strings.HasPrefix(k8sItem.Reason, "K8S_MANAGEMENT_"),
		"reason carries a K8S_MANAGEMENT_* prefix, got %q", k8sItem.Reason)

	// Cloud item: unaffected, executable as usual.
	assert.True(t, cloudItem.AutoChangeable)
	assert.Empty(t, cloudItem.Reason)

	// State: skipped 条目与原因一并持久化；三云条目 pending。
	items, err := h.Items.ListByOrder(context.Background(), list.OrderID)
	require.NoError(t, err)
	require.Len(t, items, 2)
	for _, item := range items {
		if item.Action == domain.ActionPatchCRD {
			assert.Equal(t, domain.ItemStatusSkipped, item.Status,
				"unexecutable k8s item persisted as skipped (kept out of the execution denominator)")
			assert.Contains(t, item.Error, "K8S_MANAGEMENT_SIGNAL")
		} else {
			assert.Equal(t, domain.ItemStatusPending, item.Status)
		}
	}
}

// Contract outcome "unauthorized": HTTP 401 全局认证中间件文案（非 cert 信封），
// 不创建变更单。
func TestGenerateChangeList_UnauthenticatedRejected401(t *testing.T) {
	h := multicloudtest.NewHarness(t, func(cfg *multicloudtest.Config) {
		cfg.Claims = nil // no valid session
	})
	w := seedThreeCloudReplacement(t, h)

	resp := h.GenerateChangeList(w.OldFP, w.NewCertID)

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode, resp.Body)
	// Raw global-auth body, not the cert module envelope.
	assert.Contains(t, resp.Body, "认证失败", "global auth failure copy, got %s", resp.Body)
	assert.NotContains(t, resp.Body, "FORBIDDEN", "not a cert-envelope 403")
	assert.Nil(t, resp.Env.Error, "raw middleware body carries no cert envelope error")

	// State: 无状态变化，不创建变更单。
	_, total, err := h.Orders.ListPage(context.Background(), "", 0, 10)
	require.NoError(t, err)
	assert.Zero(t, total, "no change order created")
}
