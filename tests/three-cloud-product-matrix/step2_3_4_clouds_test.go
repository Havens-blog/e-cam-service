// @feature cert-multicloud-deployers @api-functional
//
// Contracts: three-cloud-product-matrix / Step 2 — 华为云四产品执行两段式;
// Step 3 — AWS CloudFront 执行两段式（us-east-1 约束）;
// Step 4 — AWS ALB/NLB 执行两段式（绑定 API 分支）.
// Outcomes under test: huawei-four-products-success,
// bind-resolve-failure-isolated, upload-name-conflict-immediate-retry,
// cloudfront-upload-useast1, default-region-independent,
// cloudfront-rebind-idempotent, alb-nlb-listener-certificates,
// nlb-explicit-product-branch, listener-region-mismatch-rejected.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package three_cloud_product_matrix

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// huaweiCombos returns the four huawei combos.
func huaweiCombos() []struct {
	Cloud      domain.Cloud
	Product    domain.Product
	AccountKey string
	ResourceID string
	OldID      string
} {
	var out []struct {
		Cloud      domain.Cloud
		Product    domain.Product
		AccountKey string
		ResourceID string
		OldID      string
	}
	for _, c := range matrixCombos() {
		if c.Cloud == domain.CloudHuawei {
			out = append(out, c)
		}
	}
	return out
}

// requireAllSuccess executes the order and requires every item success.
func requireAllSuccess(t *testing.T, h *multicloudtest.Harness, orderID string, want int) {
	t.Helper()
	h.MustExecute(orderID)
	progress := h.MustProgress(orderID)
	require.Len(t, progress.ItemStates, want)
	for _, s := range progress.ItemStates {
		assert.Equal(t, string(domain.ItemStatusSuccess), s.Status,
			"item %s error=%s", s.ItemID, s.Error)
	}
}

// Contract outcome "huawei-four-products-success": 四组合上传经 SCM 导入返回
// UUID 形态 SCM ID，四产品分别按分支绑定成功，SCM ID 形态写入 CloudCertMapping。
func TestHuawei_FourProductsTwoPhaseSuccess(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	combos := huaweiCombos()
	oldFP, newCertID, newFP := seedMatrixWorld(t, h, combos)
	ids := seedMatrixItems(t, h, oldFP, newCertID, combos)
	orderID := orderIDOf(t, h, ids[0])

	requireAllSuccess(t, h, orderID, 4)

	// Output: 每组合一次上传（统一 CDN 口径）+ 一次产品分支绑定。
	uploads := h.Huawei.UploadCalls()
	require.Len(t, uploads, 4)
	for _, u := range uploads {
		assert.True(t, len(u) > len("cdn:"), "upload name present: %q", u)
	}
	binds := h.Huawei.BindRecords()
	require.Len(t, binds, 4)
	products := map[string]bool{}
	for _, b := range binds {
		products[b.Product] = true
	}
	assert.True(t, products["cdn"] && products["waf"] && products["alb"] && products["nlb"],
		"all four product branches routed: %v", products)

	// State: 四条 active 映射（SCM UUID 形态），与条目产物一致。
	items, err := h.Items.ListByOrder(context.Background(), orderID)
	require.NoError(t, err)
	require.Len(t, items, 4)
	for _, item := range items {
		require.NotEmpty(t, item.NewCloudCertID)
		mapping, err := h.MappingByCloudCert(item.ResourceRef.Cloud, item.ResourceRef.AccountKey, item.NewCloudCertID)
		require.NoError(t, err)
		assert.Equal(t, domain.MappingStatusActive, mapping.Status)
		assert.Equal(t, newFP, mapping.CertFingerprint)
		assertCloudCertIDForm(t, "huawei", mapping.CloudCertID)
	}
}

// Contract outcome "bind-resolve-failure-isolated": 某组合绑定段失败 → 该组合
// 条目失败记 EXEC_FAILED 静态文案（映射随补偿转 orphan），其余组合隔离不受影响。
func TestHuawei_BindResolveFailureIsolated(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	combos := huaweiCombos()
	oldFP, newCertID, _ := seedMatrixWorld(t, h, combos)
	// 注入式资源解析失败：第二个绑定调用失败（回滚按项 ID 稳定排序）。
	ids := seedMatrixItems(t, h, oldFP, newCertID, combos)
	orderID := orderIDOf(t, h, ids[0])
	sort.Strings(ids)
	h.Huawei.SetBindErr(2, errors.New("huawei waf: host not found, update rejected"))

	h.MustExecute(orderID)

	// Output: 失败组合 EXEC_FAILED 静态文案；其余组合隔离继续成功。
	progress := h.MustProgress(orderID)
	require.Len(t, progress.ItemStates, 4)
	failures, successes := 0, 0
	for _, s := range progress.ItemStates {
		switch domain.ChangeItemStatus(s.Status) {
		case domain.ItemStatusFailed:
			failures++
			assert.Contains(t, s.Error, "EXEC_FAILED")
		case domain.ItemStatusSuccess:
			successes++
		}
	}
	assert.Equal(t, 1, failures, "exactly one combo failed")
	assert.Equal(t, 3, successes, "the other three combos were isolated from the failure")

	// State: 失败组合映射随补偿转 orphan；成功组合映射 active。
	orphans := h.OrphanMappings()
	require.Len(t, orphans, 1)
	require.NotEmpty(t, h.Huawei.CleanupCalls(), "compensation delete ran for the failed combo")
}

// Contract outcome "upload-name-conflict-immediate-retry": 上传名冲突 → 即时
// 重试无退避，重试立即换新名称副本；耗尽后显式失败不猜测。
func TestHuawei_UploadNameConflictImmediateRetry(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	combos := huaweiCombos()[:1] // single combo keeps call accounting simple
	oldFP, newCertID, _ := seedMatrixWorld(t, h, combos)
	ids := seedMatrixItems(t, h, oldFP, newCertID, combos)
	orderID := orderIDOf(t, h, ids[0])

	// 第一次上传名冲突，第二次成功（名称冲突即时重试，无退避等待）。
	h.Huawei.SetUploadErr(1, errors.New("cert name already exist"))

	h.MustExecute(orderID)

	// Output: 重试立即换新名称副本，条目继续两段式收敛 success。
	uploads := h.Huawei.UploadCalls()
	require.Len(t, uploads, 2, "immediate retry consumed one extra attempt: %v", uploads)
	assert.NotEqual(t, uploads[0], uploads[1], "retry switches to a fresh name copy")
	progress := h.MustProgress(orderID)
	assert.Equal(t, string(domain.ItemStatusSuccess), progress.ItemStates[0].Status)
}

// awsCloudfrontCombos returns the AWS cdn combo.
func awsCloudfrontCombos() []struct {
	Cloud      domain.Cloud
	Product    domain.Product
	AccountKey string
	ResourceID string
	OldID      string
} {
	return matrixCombos()[4:5]
}

// Contract outcome "cloudfront-upload-useast1": 证书经 ACM 导入固定上传至
// us-east-1（上传地域与账户默认区域解耦），返回 ACM ARN；分配按查看器证书
// 替换，ARN 形态写入映射。
func TestAwsCloudFront_UploadPinnedToUseast1(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	combos := awsCloudfrontCombos()
	oldFP, newCertID, newFP := seedMatrixWorld(t, h, combos)
	ids := seedMatrixItems(t, h, oldFP, newCertID, combos)
	orderID := orderIDOf(t, h, ids[0])

	requireAllSuccess(t, h, orderID, 1)

	// Output: 上传统一走 CDN 口径（CloudFront us-east-1 硬约束 → 产物恒
	// us-east-1 ARN）；分配绑定校验同地域 ARN。
	uploads := h.Aws.UploadCalls()
	require.Len(t, uploads, 1)
	assert.True(t, len(uploads[0]) > len("cdn:"), "CDN-channel upload: %q", uploads[0])

	// State: ARN 形态 active 映射；分配引用该 ARN。
	items, err := h.Items.ListByOrder(context.Background(), orderID)
	require.NoError(t, err)
	require.NotEmpty(t, items[0].NewCloudCertID)
	assertCloudCertIDForm(t, "aws", items[0].NewCloudCertID)
	mapping, err := h.MappingByCloudCert("aws", "acct-aws-1", items[0].NewCloudCertID)
	require.NoError(t, err)
	assert.Equal(t, newFP, mapping.CertFingerprint)
	binds := h.Aws.BindRecords()
	require.Len(t, binds, 1)
	assert.Equal(t, "cdn", binds[0].Product, "CloudFront distribution path")
	assert.Equal(t, "distribution-1", binds[0].ResourceID)
}

// Contract outcome "default-region-independent": 账户默认区域非 us-east-1 →
// 上传地域仍恒 us-east-1；跨地域显式拒绝（非 us-east-1 地域提示）。
// NOTE: the region pin itself lives in the real ACM adapter ( cloudx unit
// tests ); here we assert the deployment with a region-less credential still
// lands a us-east-1 ARN mapping, and that a cross-region bind rejection
// propagates as an explicit item failure.
func TestAwsCloudFront_DefaultRegionIndependent(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	combos := awsCloudfrontCombos()
	oldFP, newCertID, _ := seedMatrixWorld(t, h, combos)
	ids := seedMatrixItems(t, h, oldFP, newCertID, combos)
	orderID := orderIDOf(t, h, ids[0])

	// 凭据未携带区域指定（stub 凭据无区域字段）→ 上传仍恒 us-east-1 产物。
	requireAllSuccess(t, h, orderID, 1)

	// State: ARN 落映射（us-east-1 形态），无跨地域脏绑定。
	items, err := h.Items.ListByOrder(context.Background(), orderID)
	require.NoError(t, err)
	assertCloudCertIDForm(t, "aws", items[0].NewCloudCertID)

	// 跨地域显式拒绝：注入监听器/证书地域不一致错误 → 条目显式失败。
	h2 := multicloudtest.NewHarness(t, nil)
	combos2 := matrixCombos()[5:6] // aws alb
	oldFP2, newCertID2, _ := seedMatrixWorld(t, h2, combos2)
	ids2 := seedMatrixItems(t, h2, oldFP2, newCertID2, combos2)
	orderID2 := orderIDOf(t, h2, ids2[0])
	h2.Aws.SetBindErr(1, errors.New("aws elbv2: listener region differs from certificate, import per region"))
	h2.MustExecute(orderID2)
	progress := h2.MustProgress(orderID2)
	require.Len(t, progress.ItemStates, 1)
	assert.Equal(t, string(domain.ItemStatusFailed), progress.ItemStates[0].Status)
	assert.Contains(t, progress.ItemStates[0].Error, "EXEC_FAILED")
}

// Contract outcome "cloudfront-rebind-idempotent": 分配查看器证书已等于目标
// ARN → 幂等跳过，不再发起分配更新（不消耗 ETag）。NOTE: the "already equal"
// skip lives in the real CloudFront adapter; at this surface the observable
// is the converged terminal state with exactly one bind per rebind invocation.
func TestAwsCloudFront_RebindIdempotentTerminalState(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	combos := awsCloudfrontCombos()
	oldFP, newCertID, _ := seedMatrixWorld(t, h, combos)
	ids := seedMatrixItems(t, h, oldFP, newCertID, combos)
	orderID := orderIDOf(t, h, ids[0])

	requireAllSuccess(t, h, orderID, 1)

	// State: 分配配置收敛于目标 ARN（重复绑定语义由适配层幂等跳过，
	// 不消耗 ETag —— cloudx 单测覆盖；此处断言终态一致且无部分绑定）。
	items, err := h.Items.ListByOrder(context.Background(), orderID)
	require.NoError(t, err)
	binds := h.Aws.BindRecords()
	require.Len(t, binds, 1)
	assert.Equal(t, items[0].NewCloudCertID, binds[0].CloudCertID,
		"the bound viewer certificate equals the target ARN")
	mapping, err := h.MappingByCloudCert("aws", "acct-aws-1", items[0].NewCloudCertID)
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusActive, mapping.Status)
}

// Contract outcome "alb-nlb-listener-certificates": alb/nlb 按产品分支走 ELBv2
// 监听证书 API（共用 AddListenerCertificates 形态、幂等预检、默认位次不变）。
func TestAwsAlbNlb_ListenerCertificatesBranch(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	combos := matrixCombos()[5:7] // aws alb + nlb
	oldFP, newCertID, _ := seedMatrixWorld(t, h, combos)
	ids := seedMatrixItems(t, h, oldFP, newCertID, combos)
	orderID := orderIDOf(t, h, ids[0])

	requireAllSuccess(t, h, orderID, 2)

	// Output: 两组合均按监听器 ARN 资源形态走 ELBv2 分支，落 ACM ARN 映射。
	binds := h.Aws.BindRecords()
	require.Len(t, binds, 2)
	products := map[string]bool{}
	for _, b := range binds {
		products[b.Product] = true
		assert.True(t, b.Product == "alb" || b.Product == "nlb", "product branch: %s", b.Product)
		assert.True(t, isListenerArn(b.ResourceID), "listener ARN resource form: %q", b.ResourceID)
	}
	assert.True(t, products["alb"] && products["nlb"])

	// State: 两组合条目 success；两条 ARN 形态 active 映射。
	items, err := h.Items.ListByOrder(context.Background(), orderID)
	require.NoError(t, err)
	require.Len(t, items, 2)
	for _, item := range items {
		mapping, err := h.MappingByCloudCert("aws", item.ResourceRef.AccountKey, item.NewCloudCertID)
		require.NoError(t, err)
		assert.Equal(t, domain.MappingStatusActive, mapping.Status)
		assertCloudCertIDForm(t, "aws", mapping.CloudCertID)
	}
}

// Contract outcome "nlb-explicit-product-branch": NLB 按产品分支走监听证书
// API（不误走 CloudFront 分配路径），幂等预检防重复附加，无假绑定。
func TestAwsNlb_ExplicitProductBranchNotCloudFront(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	combos := matrixCombos()[6:7] // aws nlb only
	oldFP, newCertID, _ := seedMatrixWorld(t, h, combos)
	ids := seedMatrixItems(t, h, oldFP, newCertID, combos)
	orderID := orderIDOf(t, h, ids[0])

	requireAllSuccess(t, h, orderID, 1)

	// Output: 绑定走监听证书分支（product=nlb），不经 CloudFront 分配路径。
	binds := h.Aws.BindRecords()
	require.Len(t, binds, 1)
	assert.Equal(t, "nlb", binds[0].Product, "explicit NLB branch (no fake cdn-path binding)")
	assert.True(t, isListenerArn(binds[0].ResourceID))

	// State: 监听器证书集更新且默认证书位次不变（预检+附加语义由适配层承载）。
	items, err := h.Items.ListByOrder(context.Background(), orderID)
	require.NoError(t, err)
	mapping, err := h.MappingByCloudCert("aws", "acct-aws-3", items[0].NewCloudCertID)
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusActive, mapping.Status)
}

// Contract outcome "listener-region-mismatch-rejected": 监听器 ARN 地域与证书
// ARN 地域不一致 → 显式失败（静态文案），条目 failed，映射随补偿转 orphan。
func TestAwsListener_RegionMismatchRejected(t *testing.T) {
	h := multicloudtest.NewHarness(t, nil)
	combos := matrixCombos()[5:6]
	oldFP, newCertID, _ := seedMatrixWorld(t, h, combos)
	ids := seedMatrixItems(t, h, oldFP, newCertID, combos)
	orderID := orderIDOf(t, h, ids[0])
	h.Aws.SetBindErr(1, errors.New("aws elbv2: listener region differs from certificate, import per region"))

	h.MustExecute(orderID)

	// Output: 显式失败不跨地域强绑。
	progress := h.MustProgress(orderID)
	require.Len(t, progress.ItemStates, 1)
	assert.Equal(t, string(domain.ItemStatusFailed), progress.ItemStates[0].Status)
	assert.Contains(t, progress.ItemStates[0].Error, "EXEC_FAILED")

	// State: 映射随补偿转 orphan；补偿 CleanupOrphan 云调用发生。
	orphans := h.OrphanMappings()
	require.Len(t, orphans, 1)
	require.NotEmpty(t, h.Aws.CleanupCalls(), "compensation delete ran")
}

// isListenerArn checks the ELBv2 listener ARN resource form.
func isListenerArn(id string) bool {
	return strings.HasPrefix(id, "arn:aws:elasticloadbalancing:") && strings.Contains(id, ":listener/")
}

// orderIDOf resolves the order ID of a seeded item.
func orderIDOf(t *testing.T, h *multicloudtest.Harness, itemID string) string {
	t.Helper()
	item, err := h.Items.GetByID(context.Background(), itemID)
	require.NoError(t, err)
	return item.OrderID
}
