// @feature cert-multicloud-deployers @api-functional
//
// Contract: three-cloud-product-matrix / Step 6 — 全组合映射与清单终态核对.
// Outcomes under test: all-combos-mapping-consistent, cross-cloud-mixing-rejected.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package three_cloud_product_matrix

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Contract outcome "all-combos-mapping-consistent": 9 组合执行完成后核对 ——
// 映射齐备且形态按云正确，按 (certFingerprint, cloud, accountKey) 唯一键各
// 一行、状态 active，无跨云混淆；清单终态与执行结果一致，审计留痕可核对。
func TestAllCombos_MappingConsistentAfterExecution(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	combos := matrixCombos()
	oldFP, newCertID, newFP := seedMatrixWorld(t, h, combos)
	ids := seedMatrixItems(t, h, oldFP, newCertID, combos)
	orderID := orderIDOf(t, h, ids[0])

	requireAllSuccess(t, h, orderID, 9)

	// 清单终态：9/9 success 后订单进入验证窗口（完成态收敛的中间终态）。
	detail := h.MustDetail(orderID)
	assert.Equal(t, string(domain.ChangeStatusVerifying), detail.Status)

	// State: 9 条 active 映射，(fingerprint, cloud, accountKey) 唯一键各一行。
	items, err := h.Items.ListByOrder(context.Background(), orderID)
	require.NoError(t, err)
	require.Len(t, items, 9)
	seenKeys := map[string]bool{}
	for _, item := range items {
		require.NotEmpty(t, item.NewCloudCertID)
		mapping, err := h.MappingByCloudCert(item.ResourceRef.Cloud, item.ResourceRef.AccountKey, item.NewCloudCertID)
		require.NoError(t, err, "cloud=%s account=%s", item.ResourceRef.Cloud, item.ResourceRef.AccountKey)
		assert.Equal(t, domain.MappingStatusActive, mapping.Status)
		assert.Equal(t, newFP, mapping.CertFingerprint, "mapping anchored to the new certificate fingerprint")
		assertCloudCertIDForm(t, item.ResourceRef.Cloud, mapping.CloudCertID)

		key := newFP + "|" + item.ResourceRef.Cloud + "|" + item.ResourceRef.AccountKey
		assert.False(t, seenKeys[key], "unique key (fingerprint, cloud, accountKey) hit twice: %s", key)
		seenKeys[key] = true
	}
	assert.Len(t, seenKeys, 9, "nine distinct mapping keys, no cross-cloud confusion")
	assert.Empty(t, h.OrphanMappings(), "no compensation leftovers in a clean matrix run")

	// 审计留痕可核对：execute 订单事件 + 每组合 item_result（success）。
	audit := h.Audit(orderID)
	require.Equal(t, http.StatusOK, audit.StatusCode, audit.Body)
	var payload multicloudtest.AuditPayload
	require.NoError(t, json.Unmarshal(audit.Env.Data, &payload))
	executeEvents, itemResults := 0, 0
	for _, l := range payload.Logs {
		switch l.Action {
		case "execute":
			executeEvents++
		case "item_result":
			itemResults++
			assert.Contains(t, l.Detail, "status=success", "item %s", l.ItemID)
		}
	}
	assert.Equal(t, 1, executeEvents, "order-level execute audit event")
	assert.Equal(t, 9, itemResults, "one item_result audit event per combo")
}

// Contract outcome "cross-cloud-mixing-rejected": 跨云混用被识别并拒绝 ——
// 凭据云域不匹配显式报错（fail closed，无云调用副作用）；映射查询按
// (cloud, accountKey, cloudCertId) 三元组定位，他云 ID 不命中，既有映射不变。
func TestCrossCloudMixingRejected(t *testing.T) {
	// 场景 A：AWS 条目拿到他云凭据 → 部署器凭据云域校验拒绝。
	h := multicloudtest.NewHarness(t, nil)
	combos := matrixCombos()[4:5] // aws cdn
	oldFP, newCertID, _ := seedMatrixWorld(t, h, combos)
	ids := seedMatrixItems(t, h, oldFP, newCertID, combos)
	orderID := orderIDOf(t, h, ids[0])
	h.Creds.MismatchCloud = "aws"

	h.MustExecute(orderID)

	progress := h.MustProgress(orderID)
	require.Len(t, progress.ItemStates, 1)
	assert.Equal(t, string(domain.ItemStatusFailed), progress.ItemStates[0].Status)
	assert.Contains(t, progress.ItemStates[0].Error, "EXEC_FAILED")
	// 无脏写入：凭据校验在上传段前置 → 无绑定、无映射、无 orphan。
	assert.Empty(t, h.Aws.BindRecords(), "no bind with mismatched credentials")
	assert.Empty(t, h.OrphanMappings(), "credential rejection precedes the mapping anchor")

	// 场景 B：以他云 ID 查询映射 → 三元组不命中；既有映射不变。
	h2 := multicloudtest.NewHarness(t, nil)
	combos2 := huaweiCombos()[:1]
	oldFP2, newCertID2, newFP2 := seedMatrixWorld(t, h2, combos2)
	ids2 := seedMatrixItems(t, h2, oldFP2, newCertID2, combos2)
	orderID2 := orderIDOf(t, h2, ids2[0])
	requireAllSuccess(t, h2, orderID2, 1)

	items, err := h2.Items.ListByOrder(context.Background(), orderID2)
	require.NoError(t, err)
	goodID := items[0].NewCloudCertID

	// 他云 ID（ACM ARN / KV secret ID）在 huawei 三元组下不命中。
	for _, foreignID := range []string{
		"arn:aws:acm:us-east-1:123456789012:certificate/foreign",
		"https://vault-test.vault.azure.net/secrets/foreign/1",
	} {
		mapping, err := h2.MappingByCloudCert("huawei", "acct-hw-1", foreignID)
		require.Error(t, err, "foreign ID %q must not resolve in the huawei namespace", foreignID)
		assert.Equal(t, domain.CloudCertMapping{}, mapping, "no partial row leaks on a miss")
	}
	// 既有映射不变：正确三元组仍唯一命中 active。
	mapping, err := h2.MappingByCloudCert("huawei", "acct-hw-1", goodID)
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusActive, mapping.Status)
	assert.Equal(t, newFP2, mapping.CertFingerprint)
	assert.Empty(t, h2.OrphanMappings(), "lookup misses cause no state mutation")
}
