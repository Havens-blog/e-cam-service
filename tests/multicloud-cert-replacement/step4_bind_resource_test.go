// @feature cert-multicloud-deployers @api-functional
//
// Contract: multicloud-cert-replacement / Step 4 — 绑定段——绑定目标资源.
// Outcomes under test: success, aws-nlb-listener-branch, azure-appgw-kv-reference.
//
// Product-branch routing is exercised at the deployer layer: each bind call
// recorded by the stub carries "product:resource:cloudCertID", so the test
// can verify the explicit per-product API branch ( CloudFront distribution vs
// ELBv2 listener vs App Gateway KV reference ) chosen by the deployer.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multicloud_cert_replacement

import (
	"context"
	"strings"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedSingleItemOrder seeds an executing single-batch order with one pending
// cloud_api item for the given resource ref and returns the order and item IDs.
func seedSingleItemOrder(t *testing.T, h *multicloudtest.Harness, w replacementWorld,
	cloud, product, accountKey, resourceID, oldCloudCertID string) (string, string) {
	t.Helper()
	orderID := h.SeedOrder(domain.ChangeStatusExecuting, seededBatchInfo(1), nil, w.OldFP, w.NewCertID)
	ref := domain.ResourceRef{Channel: domain.ChannelCloudAPI, Cloud: cloud,
		Product: product, AccountKey: accountKey, ResourceID: resourceID}
	itemID := h.SeedItem(orderID, ref, domain.ItemStatusPending, 1, oldCloudCertID)
	return orderID, itemID
}

// executeOrder runs the execute endpoint for a seeded single-batch order and
// requires the item to settle in the given status, returning the progress.
func executeOrder(t *testing.T, h *multicloudtest.Harness, orderID string, wantStatus domain.ChangeItemStatus) multicloudtest.ProgressPayload {
	t.Helper()
	h.MustExecute(orderID)
	progress := h.MustProgress(orderID)
	require.Len(t, progress.ItemStates, 1)
	require.Equal(t, string(wantStatus), progress.ItemStates[0].Status,
		"item state: %s (error=%s)", progress.ItemStates[0].Status, progress.ItemStates[0].Error)
	return progress
}

// isAcmArn checks the us-east-1 ACM ARN shape ( fact MC_AWS_UPLOAD_REGION_PIN ).
func isAcmArn(id string) bool {
	return strings.HasPrefix(id, "arn:aws:acm:us-east-1:")
}

// isKvSecretID checks the versioned KV secret ID reference form
// ( fact MC_AZURE_UPLOAD_KV ).
func isKvSecretID(id string) bool {
	return strings.HasPrefix(id, "https://") &&
		strings.Contains(id, ".vault.azure.net/secrets/")
}

// Contract outcome "success": 绑定段将云证书库中的新证书绑定到引用的目标资源，
// 按各自绑定 API 完成；绑定结果显式成功，条目收敛 success。
func TestExecuteBindPhase_BindsTargetResource(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := replacementWorld{Domain: "www.example.com"}
	w.OldCertID, w.OldFP = h.SeedCompleteCert("old.example.com", w.Domain)
	w.NewCertID, w.NewFP = h.SeedCompleteCert("new.example.com", w.Domain)

	orderID, itemID := seedSingleItemOrder(t, h, w, "huawei", "cdn", "acct-hw-1",
		"www.example.com", "scm-old-cert-0001")

	progress := executeOrder(t, h, orderID, domain.ItemStatusSuccess)

	// Output: 绑定段调用携带目标资源与上传产物（显式成功才收敛 success）。
	records := h.Huawei.BindRecords()
	require.Len(t, records, 1, "one bind call")
	item, err := h.Items.GetByID(context.Background(), itemID)
	require.NoError(t, err)
	assert.Equal(t, "cdn", records[0].Product)
	assert.Equal(t, "www.example.com", records[0].ResourceID)
	assert.Equal(t, item.NewCloudCertID, records[0].CloudCertID, "bind consumed the uploaded cloud cert ID")

	// State: 条目收敛 success 且进度接口可见批次归属。
	assert.Equal(t, itemID, progress.ItemStates[0].ItemID)
	assert.Equal(t, 1, progress.ItemStates[0].BatchNo)
	// Deep (cross-entity): mapping row carries the same artifact.
	mapping, err := h.MappingByCloudCert("huawei", "acct-hw-1", item.NewCloudCertID)
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusActive, mapping.Status)
	assert.Equal(t, w.NewFP, mapping.CertFingerprint)
}

// Contract outcome "aws-nlb-listener-branch": NLB 按产品分支走 ELBv2 监听证书
// API（与 ALB 共用该 API 形态但产品路由显式），不经 CloudFront 分配路径，
// 绑定前幂等预检、默认证书位次不变。
func TestExecuteBindPhase_AwsNlbListenerCertificatesBranch(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := replacementWorld{Domain: "www.example.com"}
	w.OldCertID, w.OldFP = h.SeedCompleteCert("old.example.com", w.Domain)
	w.NewCertID, w.NewFP = h.SeedCompleteCert("new.example.com", w.Domain)

	listenerARN := "arn:aws:elasticloadbalancing:us-east-1:123456789012:listener/nlb-1"
	orderID, itemID := seedSingleItemOrder(t, h, w, "aws", "nlb", "acct-aws-1",
		listenerARN, "arn:aws:acm:us-east-1:123456789012:certificate/old-cert")

	h.MustExecute(orderID)
	progress := h.MustProgress(orderID)
	require.Len(t, progress.ItemStates, 1)
	require.Equal(t, string(domain.ItemStatusSuccess), progress.ItemStates[0].Status,
		"error=%s", progress.ItemStates[0].Error)

	// Output: 绑定走监听证书分支（product=nlb），资源为监听器 ARN，证书为 ACM ARN。
	records := h.Aws.BindRecords()
	require.Len(t, records, 1)
	assert.Equal(t, "nlb", records[0].Product, "explicit NLB product branch (not the CloudFront cdn path)")
	assert.Equal(t, listenerARN, records[0].ResourceID, "binds the listener resource")
	assert.True(t, isAcmArn(records[0].CloudCertID),
		"certificate bound is the ACM ARN upload artifact, got %q", records[0].CloudCertID)

	// 上传统一走 ACM（CDN 口径上传：CloudFront us-east-1 硬约束使产物恒为 ARN）。
	uploads := h.Aws.UploadCalls()
	require.Len(t, uploads, 1)
	assert.NotEmpty(t, uploads[0], "upload name generated per attempt")

	// State: NLB 监听器证书集更新（ACM ARN 映射 active）。
	item, err := h.Items.GetByID(context.Background(), itemID)
	require.NoError(t, err)
	mapping, err := h.MappingByCloudCert("aws", "acct-aws-1", item.NewCloudCertID)
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusActive, mapping.Status)
}

// Contract outcome "azure-appgw-kv-reference": App Gateway 按 KV 证书引用形态
// 绑定（keyVaultSecretId 指向本次上传的 KV secret，引用而非直传 ID）。
func TestExecuteBindPhase_AzureAppGatewayKvReference(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	w := replacementWorld{Domain: "www.example.com"}
	w.OldCertID, w.OldFP = h.SeedCompleteCert("old.example.com", w.Domain)
	w.NewCertID, w.NewFP = h.SeedCompleteCert("new.example.com", w.Domain)

	gatewayRef := "appgw-1/listener-1"
	orderID, itemID := seedSingleItemOrder(t, h, w, "azure", "alb", "acct-az-1",
		gatewayRef, "https://vault-test.vault.azure.net/secrets/old-cert/1")

	h.MustExecute(orderID)
	progress := h.MustProgress(orderID)
	require.Len(t, progress.ItemStates, 1)
	require.Equal(t, string(domain.ItemStatusSuccess), progress.ItemStates[0].Status,
		"error=%s", progress.ItemStates[0].Error)

	// Output: 绑定以 KV secret 引用语义完成（引用指向本次上传的证书）。
	records := h.Azure.BindRecords()
	require.Len(t, records, 1)
	assert.Equal(t, "alb", records[0].Product, "App Gateway product branch")
	assert.Equal(t, gatewayRef, records[0].ResourceID, "binds the gateway/listener resource")
	assert.True(t, isKvSecretID(records[0].CloudCertID),
		"cloudCertID is the KV secret ID reference form, got %q", records[0].CloudCertID)

	// Deep (cross-entity): the KV reference on the bind call is exactly the
	// uploaded artifact recorded on the item and its active mapping.
	item, err := h.Items.GetByID(context.Background(), itemID)
	require.NoError(t, err)
	assert.Equal(t, item.NewCloudCertID, records[0].CloudCertID)
	mapping, err := h.MappingByCloudCert("azure", "acct-az-1", item.NewCloudCertID)
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusActive, mapping.Status)
}
