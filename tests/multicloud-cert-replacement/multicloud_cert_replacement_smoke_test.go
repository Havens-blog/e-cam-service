// @feature cert-multicloud-deployers @api-functional
//
// Journey smoke: multicloud-cert-replacement happy path end-to-end plus one
// error path. Steps 1-7 in sequence: generate ( three clouds ) -> confirm
// ( batched ) -> per-batch execute ( two-phase upload/bind ) -> verify window
// probes -> old-cert orphan cleanup, with the in-flight 409 gate as the
// error-path check. State flows between steps via the order ID and item IDs.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multicloud_cert_replacement

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Journey invariants verified across the whole loop:
//   - 三云替换语义同构：同一状态机、同一两段式编排；
//   - 两段式顺序不可逆：UploadCert 成功后才 BindResource（映射先行写入）；
//   - 云证书 ID 空间互斥（SCM ID / ACM ARN / KV 引用）；
//   - 全程未登录云控制台（仅 API 面）。
func TestMulticloudCertReplacement_FullLifecycleSmoke(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedThreeCloudReplacement(t, h)

	// ---- Step 1: 生成覆盖三云的变更清单 ----
	list := h.MustGenerate(w.OldFP, w.NewCertID)
	require.Len(t, list.Items, 3)
	for _, item := range list.Items {
		assert.True(t, item.AutoChangeable, "three-cloud refs all executable")
	}

	// ---- Error path: 在途互斥 —— 活跃单存在时重复生成被 409 阻断 ----
	dup := h.GenerateChangeList(w.OldFP, w.NewCertID)
	require.Equal(t, http.StatusConflict, dup.StatusCode, dup.Body)
	require.NotNil(t, dup.Env.Error)
	assert.Equal(t, "CHANGE_IN_FLIGHT", dup.Env.Error.Code)

	// ---- Step 2: 确认并分批执行（3 批，每批 1 项） ----
	h.MustConfirm(list.OrderID, batchedThreeConf())

	// ---- Steps 2-5: 逐批执行两段式 → 映射 active → 验证窗口 → 人工续批 ----
	for batch := 1; batch <= 3; batch++ {
		h.MustExecute(list.OrderID)
		progress := h.MustProgress(list.OrderID)
		for _, state := range progress.ItemStates {
			if state.BatchNo == batch {
				assert.Equal(t, string(domain.ItemStatusSuccess), state.Status,
					"batch %d item succeeded (error=%s)", batch, state.Error)
			}
		}
		if batch < 3 {
			// 非终批：进入验证窗口后人工续批（批级达标需连续探测一致）。
			h.Prober.SetOnlineFingerprint(w.Domain, w.NewFP)
			probeRounds(t, h, verifyConfirmProbes)
			h.MustConfirmBatch(list.OrderID)
		}
	}

	// Deep (cross-entity): 三云映射齐备且形态按云正确，与条目产物一致。
	items, err := h.Items.ListByOrder(context.Background(), list.OrderID)
	require.NoError(t, err)
	require.Len(t, items, 3)
	forms := map[string]bool{}
	for _, item := range items {
		require.NotEmpty(t, item.NewCloudCertID)
		mapping, err := h.MappingByCloudCert(item.ResourceRef.Cloud, item.ResourceRef.AccountKey, item.NewCloudCertID)
		require.NoError(t, err, "mapping for %s", item.ResourceRef.Cloud)
		assert.Equal(t, domain.MappingStatusActive, mapping.Status)
		assertCloudCertIDForm(t, item.ResourceRef.Cloud, mapping.CloudCertID)
		forms[item.ResourceRef.Cloud] = true
		// 两段式顺序：绑定调用使用的是本条上传产物。
		binds := bindsFor(t, h, item.ResourceRef.Cloud)
		require.NotEmpty(t, binds)
		assert.Contains(t, binds[len(binds)-1], item.NewCloudCertID)
	}
	assert.True(t, forms["huawei"] && forms["aws"] && forms["azure"], "all three cloud ID spaces exercised")

	// ---- Step 6: 验证窗口拨测确认线上指纹（终批） ----
	h.Prober.SetOnlineFingerprint(w.Domain, w.NewFP)
	probeRounds(t, h, verifyConfirmProbes)
	detail := h.MustDetail(list.OrderID)
	require.NotNil(t, detail.Report)
	assert.Equal(t, string(domain.ChangeStatusCompleted), detail.Report.Status, "window met completes the order")

	// ---- Step 7: 旧证书孤儿清理（终态后事件触发消费） ----
	// Seed the pre-replacement active mappings ( old cloud cert per cloud ).
	// 终态固化已给旧证书 7 天保护期（completed 保护期固化）；契约前置要求
	// "不在保护期"，这里将保护期拨到过去模拟保护期已过。
	for _, item := range items {
		h.SeedMapping(w.OldFP, item.ResourceRef.Cloud, item.ResourceRef.AccountKey, item.OldCloudCertID)
		h.ExpireProtectUntil(w.OldFP, time.Now().Add(-time.Hour))
	}
	consumed, err := h.Cleanup.ConsumeOrderQueue(context.Background(), list.OrderID)
	require.NoError(t, err)
	assert.Equal(t, 3, consumed, "one old-cert orphan per cloud consumed")
	for _, item := range items {
		_, err := h.MappingByCloudCert(item.ResourceRef.Cloud, item.ResourceRef.AccountKey, item.OldCloudCertID)
		assert.Error(t, err, "old cloud cert mapping deleted for %s", item.ResourceRef.Cloud)
	}

	// Deep (cross-entity): cleanup results recorded per cloud on the report.
	final := h.MustDetail(list.OrderID)
	require.NotNil(t, final.Report)
	require.Len(t, final.Report.OrphanCleanup, 3)
	for _, o := range final.Report.OrphanCleanup {
		assert.Equal(t, service.OrphanActionCleanup, o.Action)
		assert.True(t, o.Success)
	}
}

// bindsFor returns the recorded bind calls of one cloud stub.
func bindsFor(t *testing.T, h *multicloudtest.Harness, cloud string) []string {
	t.Helper()
	switch cloud {
	case "huawei":
		return h.Huawei.BindCalls()
	case "aws":
		return h.Aws.BindCalls()
	case "azure":
		return h.Azure.BindCalls()
	default:
		t.Fatalf("unsupported cloud %q", cloud)
		return nil
	}
}
