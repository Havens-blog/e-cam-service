// @feature cert-multicloud-deployers @api-functional
//
// Contract: bind-failure-compensation / Step 2 — 触发 CleanupOrphan 补偿.
// Outcomes under test: compensation-success, double-invocation-idempotent,
// mapping-missing-compensation-incomplete.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package bind_failure_compensation

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bindErr is the injected bind rejection ( adapter-static text semantics ).
var bindErr = errors.New("huawei cdn: resource state does not allow certificate update")

// Contract outcome "compensation-success": 绑定失败补偿删除已上传云证书
// （华为/AWS/Azure 按各自删除 API 路由），映射转 orphan，补偿幂等可重入。
func TestCompensation_CleanupOrphanDeletesUploadedCert(t *testing.T) {
	type scenario struct {
		cloud      domain.Cloud
		product    domain.Product
		accountKey string
		resourceID string
		oldID      string
	}
	scenarios := []scenario{
		{domain.CloudHuawei, domain.ProductCDN, "acct-hw-1", "www.example.com", "old-hw-1"},
		{domain.CloudAWS, domain.ProductALB, "acct-aws-1",
			"arn:aws:elasticloadbalancing:us-east-1:123456789012:listener/alb-1", "old-aws-arn"},
		{domain.CloudAzure, domain.ProductCDN, "acct-az-1", "frontdoor-1/endpoint-1", "old-az-secret"},
	}

	for _, sc := range scenarios {
		h := multicloudtest.NewHarness(t, nil)
		w := seedCompensationWorld(t, h, sc.cloud, sc.product, sc.accountKey, sc.resourceID)
		h.OldCertCloudID(string(sc.cloud), sc.oldID, w.OldFP)
		orderID, _ := seedExecutingWithPendingItem(t, h, w, string(sc.cloud), string(sc.product), sc.accountKey, sc.resourceID)
		bindErrFor(t, h, sc.cloud, bindErr)

		h.MustExecute(orderID)

		// Output: 补偿删除已上传产物 —— 该云 stub 记录一次 CleanupOrphan 调用，
		// 目标正是本次上传的云证书 ID（幂等可重入的删除路由）。
		// NOTE: 失败路径不持久化条目 NewCloudCertID（finishItem 失败分支传空），
		// 云证书 ID 经补偿链的映射行（active→orphan）承载。
		calls := cleanupCallsFor(t, h, sc.cloud)
		require.Len(t, calls, 1)

		// State: 映射 active→orphan（清理队列入口），云侧证书删除完成。
		orphans := h.OrphanMappings()
		require.Len(t, orphans, 1)
		assert.Equal(t, calls[0], orphans[0].CloudCertID)
		assert.Equal(t, w.NewFP, orphans[0].CertFingerprint)
	}
}

// Contract outcome "double-invocation-idempotent": 同一补偿触发两次（重试与
// 队列消费竞争；第二次时云侧证书已不存在）→ 二次调用不报错、不重复删除。
func TestCompensation_DoubleInvocationIdempotent(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := seedCompensationWorld(t, h, domain.CloudHuawei, domain.ProductCDN, "acct-hw-1", "www.example.com")
	orderID, _ := seedExecutingWithPendingItem(t, h, w, "huawei", "cdn", "acct-hw-1", "www.example.com")
	h.Huawei.SetBindErr(1, bindErr)

	h.MustExecute(orderID)

	orphans := h.OrphanMappings()
	require.Len(t, orphans, 1, "compensation marked the mapping orphan")
	newID := orphans[0].CloudCertID
	require.Len(t, h.Huawei.CleanupCalls(), 1, "first compensation delete recorded")

	// 第二次补偿触发（云侧证书已不存在 —— 各云幂等成功语义）。
	creds, err := h.Creds.CloudCredential(context.Background(), "huawei", "acct-hw-1")
	require.NoError(t, err)
	require.NoError(t, h.Channel.CleanupOrphanCert(context.Background(), creds, "huawei", newID),
		"second CleanupOrphan on a deleted cert is idempotent success")
	creds.Zeroize()

	// 无重复副作用：恰好两次删除调用（首次删除 + 幂等重放），结果一致。
	assert.Len(t, h.Huawei.CleanupCalls(), 2)
	// State: 映射保持 orphan（队列后续消费以删除承载，无重复删除）。
	for _, m := range h.OrphanMappings() {
		assert.Equal(t, domain.MappingStatusOrphan, m.Status)
	}
}

// Contract outcome "mapping-missing-compensation-incomplete": 补偿时按云证书
// ID 查找映射未命中（记录缺失）→ 仍尽力 CleanupOrphan；补偿结果标记未完成
// （orphan compensation incomplete），不产生脏映射记录。
func TestCompensation_MappingLookupMissKeepsOrphanCandidate(t *testing.T) {
	h := multicloudtest.NewHarness(t, func(cfg *multicloudtest.Config) {
		// 补偿反查路径注入"映射缺失"（记录被删除/缺失的注入式边界）。
		cfg.HideMappingLookups = []string{"*"}
	})
	w := seedCompensationWorld(t, h, domain.CloudHuawei, domain.ProductCDN, "acct-hw-1", "www.example.com")
	orderID, _ := seedExecutingWithPendingItem(t, h, w, "huawei", "cdn", "acct-hw-1", "www.example.com")
	h.Huawei.SetBindErr(1, bindErr)

	// NOTE: the upload itself upserts the mapping before the bind segment;
	// with every lookup hidden the compensation cannot resolve it, which is
	// the documented "mapping lookup miss at compensation time" boundary.
	h.MustExecute(orderID)

	items, err := h.Items.ListByOrder(context.Background(), orderID)
	require.NoError(t, err)
	require.Len(t, items, 1)

	// Output: 补偿尽力执行 —— 云侧删除调用仍然发生。
	calls := h.Huawei.CleanupCalls()
	require.NotEmpty(t, calls, "best-effort CleanupOrphan still ran")

	// 补偿结果标记未完成：条目失败因附 orphan compensation incomplete 语义。
	assert.Equal(t, domain.ItemStatusFailed, items[0].Status)
	assert.Contains(t, items[0].Error, "orphan compensation incomplete")

	// State: 不产生脏映射记录；孤儿候选标记保留（OrphanCandidate 语义）。
	assert.Empty(t, h.OrphanMappings(), "no dirty mapping row invented by the compensation")
}

// bindErrFor injects the bind rejection on the owning cloud's stub.
func bindErrFor(t *testing.T, h *multicloudtest.Harness, cloud domain.Cloud, err error) {
	t.Helper()
	switch cloud {
	case domain.CloudHuawei:
		h.Huawei.SetBindErr(1, err)
	case domain.CloudAWS:
		h.Aws.SetBindErr(1, err)
	case domain.CloudAzure:
		h.Azure.SetBindErr(1, err)
	default:
		t.Fatalf("unsupported cloud %q", cloud)
	}
}

// cleanupCallsFor returns the recorded cleanup calls of one cloud stub.
func cleanupCallsFor(t *testing.T, h *multicloudtest.Harness, cloud domain.Cloud) []string {
	t.Helper()
	switch cloud {
	case domain.CloudHuawei:
		return h.Huawei.CleanupCalls()
	case domain.CloudAWS:
		return h.Aws.CleanupCalls()
	case domain.CloudAzure:
		return h.Azure.CleanupCalls()
	default:
		t.Fatalf("unsupported cloud %q", cloud)
		return nil
	}
}
