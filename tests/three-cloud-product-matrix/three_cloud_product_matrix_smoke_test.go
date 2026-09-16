// @feature cert-multicloud-deployers @api-functional
//
// Journey smoke: three-cloud-product-matrix happy path end-to-end plus one
// error path. Nine cloud×product combos through one executing order ( generate
// shape -> execute -> per-cloud upload/bind -> active mappings -> completed
// report ), with single-combo failure isolation as the error-path check.
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

// Journey invariants verified across the loop:
//   - 9 组合共享同一五方法端口语义与两段式编排，无云/产品差异豁免；
//   - 云证书 ID 形态按云固定（华为 SCM UUID / AWS ACM ARN / Azure KV 引用）；
//   - 单组合失败隔离：任一组合失败不影响其余组合的执行与终态。
func TestThreeCloudProductMatrix_FullMatrixSmoke(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	combos := matrixCombos()
	oldFP, newCertID, newFP := seedMatrixWorld(t, h, combos)
	ids := seedMatrixItems(t, h, oldFP, newCertID, combos)
	orderID := orderIDOf(t, h, ids[0])

	// ---- 执行：9 组合单批全部收敛 success ----
	h.MustExecute(orderID)
	progress := h.MustProgress(orderID)
	require.Len(t, progress.ItemStates, 9)
	for _, s := range progress.ItemStates {
		require.Equal(t, string(domain.ItemStatusSuccess), s.Status,
			"item %s error=%s", s.ItemID, s.Error)
	}

	// Output: 每组合恰好一次上传 + 一次绑定（9 上传 / 9 绑定，按云分账）。
	require.Len(t, h.Huawei.UploadCalls(), 4)
	require.Len(t, h.Aws.UploadCalls(), 3)
	require.Len(t, h.Azure.UploadCalls(), 2)
	require.Len(t, h.Huawei.BindRecords(), 4)
	require.Len(t, h.Aws.BindRecords(), 3)
	require.Len(t, h.Azure.BindRecords(), 2)

	// State: 订单收敛进验证窗口；9 条 per-cloud 形态的 active 映射。
	detail := h.MustDetail(orderID)
	assert.Equal(t, string(domain.ChangeStatusVerifying), detail.Status)
	items, err := h.Items.ListByOrder(context.Background(), orderID)
	require.NoError(t, err)
	require.Len(t, items, 9)
	for _, item := range items {
		require.NotEmpty(t, item.NewCloudCertID)
		mapping, err := h.MappingByCloudCert(item.ResourceRef.Cloud, item.ResourceRef.AccountKey, item.NewCloudCertID)
		require.NoError(t, err)
		assert.Equal(t, domain.MappingStatusActive, mapping.Status)
		assert.Equal(t, newFP, mapping.CertFingerprint)
		assertCloudCertIDForm(t, item.ResourceRef.Cloud, mapping.CloudCertID)
	}

	// ---- Error path: 单组合绑定失败隔离（其余 8 组合不受影响） ----
	h2 := multicloudtest.NewHarness(t, nil)
	oldFP2, newCertID2, _ := seedMatrixWorld(t, h2, combos)
	ids2 := seedMatrixItems(t, h2, oldFP2, newCertID2, combos)
	orderID2 := orderIDOf(t, h2, ids2[0])
	// 华为第二个绑定调用失败（4 个华为条目按项 ID 稳定排序中的第 2 个）。
	h2.Huawei.SetBindErr(2, errors.New("huawei waf: host not found, update rejected"))

	h2.MustExecute(orderID2)

	progress2 := h2.MustProgress(orderID2)
	require.Len(t, progress2.ItemStates, 9)
	failures, successes := 0, 0
	for _, s := range progress2.ItemStates {
		switch domain.ChangeItemStatus(s.Status) {
		case domain.ItemStatusFailed:
			failures++
			assert.Contains(t, s.Error, "EXEC_FAILED")
		case domain.ItemStatusSuccess:
			successes++
		}
	}
	assert.Equal(t, 1, failures, "exactly one combo isolated into failure")
	assert.Equal(t, 8, successes, "the other eight combos were unaffected")
	require.Len(t, h2.OrphanMappings(), 1, "failed combo mapping compensated to orphan")
	require.NotEmpty(t, h2.Huawei.CleanupCalls(), "compensation delete ran for the failed combo")
}
