// @feature cert-multicloud-deployers @api-functional
//
// Contract: three-cloud-product-matrix / Step 5 — Azure 两产品执行两段式
//（KV 引用绑定）.
// Outcomes under test: azure-two-products-success, appgw-inline-form-rejected,
// kv-missing-explicit-failure.
//
// NOTE: inline-data rejection and vault-target resolution live inside the real
// azure adapter ( cloudx unit tests ); at this surface they are encoded as
// injected adapter errors propagating through the deployer/channel/item
// contract.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package three_cloud_product_matrix

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// azureCombos returns the two azure combos ( cdn + alb ).
func azureCombos() []struct {
	Cloud      domain.Cloud
	Product    domain.Product
	AccountKey string
	ResourceID string
	OldID      string
} {
	return matrixCombos()[7:9]
}

// Contract outcome "azure-two-products-success": 证书经 KV 证书导入上传产生
// 版本化 KV secret ID 引用；Front Door / App Gateway 按 KV 引用语义绑定成功，
// 落 KV 引用形态 active 映射。
func TestAzure_TwoProductsTwoPhaseSuccess(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	combos := azureCombos()
	oldFP, newCertID, newFP := seedMatrixWorld(t, h, combos)
	ids := seedMatrixItems(t, h, oldFP, newCertID, combos)
	orderID := orderIDOf(t, h, ids[0])

	requireAllSuccess(t, h, orderID, 2)

	// Output: 每组合一次 KV 上传 + 一次 KV 引用语义绑定（Front Door 补丁 /
	// App Gateway SSL 证书子资源写）。
	uploads := h.Azure.UploadCalls()
	require.Len(t, uploads, 2)
	binds := h.Azure.BindRecords()
	require.Len(t, binds, 2)
	products := map[string]bool{}
	for _, b := range binds {
		products[b.Product] = true
	}
	assert.True(t, products["cdn"] && products["alb"], "front door + app gateway branches: %v", products)

	// State: 两条 KV secret ID 形态 active 映射，指纹指向新证书。
	items, err := h.Items.ListByOrder(context.Background(), orderID)
	require.NoError(t, err)
	require.Len(t, items, 2)
	for _, item := range items {
		require.NotEmpty(t, item.NewCloudCertID)
		assertCloudCertIDForm(t, "azure", item.NewCloudCertID)
		mapping, err := h.MappingByCloudCert("azure", item.ResourceRef.AccountKey, item.NewCloudCertID)
		require.NoError(t, err)
		assert.Equal(t, domain.MappingStatusActive, mapping.Status)
		assert.Equal(t, newFP, mapping.CertFingerprint)
	}
}

// Contract outcome "appgw-inline-form-rejected": KV-backed 场景绑定引用指向
// 本次上传的证书；inline data 形态被显式拒绝（不猜测转换）→ 条目失败无脏写入。
func TestAzure_AppGwInlineFormRejected(t *testing.T) {
	// KV-backed 场景：App Gateway SSL 证书资源写入本次上传的 KV secret 引用。
	h := multicloudtest.NewHarness(t, nil)
	combos := matrixCombos()[8:9] // azure alb only
	oldFP, newCertID, _ := seedMatrixWorld(t, h, combos)
	ids := seedMatrixItems(t, h, oldFP, newCertID, combos)
	orderID := orderIDOf(t, h, ids[0])

	requireAllSuccess(t, h, orderID, 1)

	items, err := h.Items.ListByOrder(context.Background(), orderID)
	require.NoError(t, err)
	binds := h.Azure.BindRecords()
	require.Len(t, binds, 1)
	assert.Equal(t, "appgw-1/listener-1", binds[0].ResourceID)
	assert.Equal(t, items[0].NewCloudCertID, binds[0].CloudCertID,
		"KV-backed bind references the uploaded certificate")

	// inline 场景：SSL 证书资源为 inline data 形态 → 适配器显式拒绝（要求
	// KV-backed 资源，不猜测转换），条目失败且无 active 脏写入。
	h2 := multicloudtest.NewHarness(t, nil)
	oldFP2, newCertID2, _ := seedMatrixWorld(t, h2, combos)
	ids2 := seedMatrixItems(t, h2, oldFP2, newCertID2, combos)
	orderID2 := orderIDOf(t, h2, ids2[0])
	inlineErr := errors.New("azure appgw: ssl certificate resource is inline data, key-vault-backed certificate required")
	h2.Azure.SetBindErr(1, inlineErr)

	h2.MustExecute(orderID2)

	progress := h2.MustProgress(orderID2)
	require.Len(t, progress.ItemStates, 1)
	assert.Equal(t, string(domain.ItemStatusFailed), progress.ItemStates[0].Status)
	assert.Contains(t, progress.ItemStates[0].Error, "EXEC_FAILED")
	// 无脏写入：新证书无 active 映射（绑定段失败仅随补偿转 orphan）。
	mappings := h2.OrphanMappings()
	require.Len(t, mappings, 1, "compensation moved the crash-anchor mapping to orphan")
	require.NotEmpty(t, h2.Azure.CleanupCalls(), "compensation delete ran for the rejected bind")
}

// Contract outcome "kv-missing-explicit-failure": 无可用 Key Vault 且装配与
// 环境变量均未注入 → 上传段显式失败（需要 vault 目标），不猜测默认实例、
// 不静默降级；其余云组合隔离不受影响。
func TestAzure_KvMissingExplicitFailure(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	combos := azureCombos()
	oldFP, newCertID, _ := seedMatrixWorld(t, h, combos)
	ids := seedMatrixItems(t, h, oldFP, newCertID, combos)
	orderID := orderIDOf(t, h, ids[0])
	// 无 vault 目标：上传段显式失败（前置资源缺失，不猜测默认实例、无重试）。
	vaultErr := errors.New("azure kv: vault target required (option or env), no default instance guessed")
	h.Azure.SetUploadErr(1, vaultErr)
	h.Azure.SetUploadErr(2, vaultErr)

	h.MustExecute(orderID)

	// Output: Azure 条目显式失败（EXEC_FAILED 静态文案）。
	progress := h.MustProgress(orderID)
	require.Len(t, progress.ItemStates, 2)
	// State: 上传段失败先于映射锚点 → 无映射写入（无 active，无 orphan）。
	items, err := h.Items.ListByOrder(context.Background(), orderID)
	require.NoError(t, err)
	require.Len(t, items, 2)
	productOfItem := map[string]string{}
	for _, item := range items {
		productOfItem[item.ID.Hex()] = item.ResourceRef.Product
		mapping, err := h.MappingByCloudCert(item.ResourceRef.Cloud, item.ResourceRef.AccountKey, item.NewCloudCertID)
		require.Error(t, err, "no mapping written for a failed upload segment")
		assert.Equal(t, domain.CloudCertMapping{}, mapping)
	}
	for _, s := range progress.ItemStates {
		assert.Equal(t, string(domain.ItemStatusFailed), s.Status, "product=%s", productOfItem[s.ItemID])
		assert.Contains(t, s.Error, "EXEC_FAILED")
	}
	assert.Empty(t, h.OrphanMappings(), "upload-phase failure never created a mapping to compensate")
	uploads := h.Azure.UploadCalls()
	require.Len(t, uploads, 2, "one terminal upload attempt per combo, no retry loop")
	assert.Empty(t, h.Azure.BindRecords(), "no bind attempted without an uploaded certificate")

	// 隔离：其余云组合不受影响 —— 另一张单中 huawei 组合正常收敛 success。
	h2 := multicloudtest.NewHarness(t, nil)
	hwCombos := huaweiCombos()[:1]
	oldFP2, newCertID2, _ := seedMatrixWorld(t, h2, hwCombos)
	ids2 := seedMatrixItems(t, h2, oldFP2, newCertID2, hwCombos)
	orderID2 := orderIDOf(t, h2, ids2[0])
	requireAllSuccess(t, h2, orderID2, 1)
}
