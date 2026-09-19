// @feature cert-volcano-deployer @api-functional
//
// volcano-product-aware-upload journey — minimal supplement over the
// existing tests/volcano-cert-replacement journeys ( gen-test-scripts
// dedup constraint ): the contract surfaces NEW vs task-5 functional tests.
//   - step-4 fallback-transparent: a deployer instance NOT implementing
//     ProductAwareUploader goes through the production channel type-assertion
//     dispatch to the generic UploadCert path ( csv unified library ),
//     behavior unchanged — the documented csv bind gap surfaces as the
//     ErrVolcanoCSVCertNotBindable sentinel, not as a dispatch error;
//   - step-3 id-space-mutex (+ step-1/2/3 success forms at port level):
//     one bundle uploaded via UploadCertForProduct to all four product
//     libraries yields four {product}:{id}-normalized IDs, pairwise
//     distinct, alb/nlb sharing the ALB library without collision;
//   - rollback-verify-window step-2 mapping-fallback-no-record /
//     invalid-target-rejected: GetCert on a fingerprintless library with no
//     mapping record keeps the fingerprint EMPTY ( fail-safe placeholder —
//     the upper three-judgment blocks, never fabricates validity ), and an
//     absent cloud cert answers Exists=false.
//
// Covered-by-reference ( no duplicate scripts generated ):
//   - two-phase / bind-failure-compensation / dispatch-visibility journeys:
//     TestVolcanoCertReplacement_FullLifecycleSmoke,
//     TestVolcanoCertReplacement_BindFailureCompensation,
//     TestVolcanoChannelDispatch_ProductVisibility (
//     tests/volcano-cert-replacement/volcano_cert_replacement_test.go );
//   - bind sentinels ( ErrVolcanoCSVCertNotBindable / product mismatch /
//     empty resource id / non-normalized cleanup id ),
//     ErrVolcanoProductNotSupported upload sentinel, bounded rate-limit
//     retry ( shared uploadCertIntoLibrary closure ⇒ fresh name per
//     attempt ), fingerprint resolution chain:
//     internal/cert/deployer/volcano_deployer_test.go unit layer;
//   - engine-level batch gate / retry-new-name / probe-mismatch hold:
//     tests/multicloud-cert-replacement ( cloud-agnostic semantics ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package volcano_cert_replacement

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/tests/multicloudtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// csvFallbackDeployer 是「未升级 ProductAwareUploader 端口」的部署器形态：
// 内嵌 CloudDeployer 接口仅持有五方法——与 *VolcanoDeployer 直接内嵌不同，
// 接口内嵌不会把 UploadCertForProduct 提升到本类型，通道类型断言因此回落
// 通用 UploadCert 路径（step-4 fallback-transparent 的被测形态）。
type csvFallbackDeployer struct {
	deployer.CloudDeployer
}

// 编译期断言：csvFallbackDeployer 恰为五方法 CloudDeployer（端口未升级形态）。
var _ deployer.CloudDeployer = csvFallbackDeployer{}

// Journey: volcano-product-aware-upload / step-4 ( fallback-transparent )
// — 部署器实例未实现可选端口时，生产 CloudAPIChannel.Deploy 的类型断言分发
// 不报错、静默回落通用 UploadCert（csv 统一库口径，行为与端口升级前完全
// 一致）；随后第二段绑定命中已显式化的 csv 缺口哨兵
// ErrVolcanoCSVCertNotBindable（结构性缺口，非分发层错误），补偿链路照常
// 收口（映射 active→orphan + 云侧清理）。调用方对端口分发无感知。
func TestVolcanoProductAwareUpload_FallbackWithoutPort(t *testing.T) {
	stub := NewStubVolcanoCertLibrary()
	h := multicloudtest.NewHarness(t, nil)
	real := deployer.NewVolcanoDeployer(h.Mappings,
		deployer.WithVolcanoCertLibrary(stub),
		deployer.WithVolcanoRetryPolicy(multicloudtest.FastRetryPolicy()))

	// 运行期确认：五方法包装形态不再满足可选端口（回落路径的触发前提）。
	_, isProductAware := interface{}(csvFallbackDeployer{real}).(deployer.ProductAwareUploader)
	require.False(t, isProductAware, "wrapper must NOT satisfy ProductAwareUploader")
	require.NoError(t, h.Channel.RegisterDeployer("volcano", csvFallbackDeployer{real},
		"cdn", "waf", "alb", "nlb"))

	w := seedVolcanoReplacement(t, h)
	h.SeedDoneSnapshotWithRefs([]multicloudtest.RefSpec{
		{Cloud: volcanoCloud, Product: domain.ProductCDN, AccountKey: w.Accounts[domain.ProductCDN],
			ResourceID: w.Domain, CloudCertID: w.OldCloudIDs[domain.ProductCDN], Fingerprint: w.OldFP},
	}, time.Now().Add(time.Minute))

	list := h.MustGenerate(w.OldFP, w.NewCertID)
	require.Len(t, list.Items, 1)
	h.MustConfirm(list.OrderID, nil)
	h.MustExecute(list.OrderID)

	// 回落路径生效：第一段上传落在 csv 统一库（非产品库定向），通道分发
	// 本身无报错。
	csvN, cdnN, wafN, albN := stub.UploadCounts()
	assert.Equal(t, 1, csvN, "fallback dispatches the generic csv-library upload")
	assert.Equal(t, 0, cdnN+wafN+albN, "no product-directed upload on the fallback path")
	require.NotEmpty(t, stub.UploadInputs())
	assert.True(t, strings.HasPrefix(stub.UploadInputs()[0], "csv:"),
		"fallback artifact is csv-prefixed: %s", stub.UploadInputs()[0])

	items, err := h.Items.ListByOrder(context.Background(), list.OrderID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	item := items[0]
	assert.Equal(t, domain.ItemStatusFailed, item.Status, "csv bind gap fails the item (error=%s)", item.Error)
	assert.Contains(t, item.Error, "csv unified-library cert cannot bind",
		"failure is the documented csv sentinel, not a dispatch error")

	// 补偿链路与端口升级前行为一致：第一段产物映射 active→orphan + 云侧清理。
	orphans := h.OrphanMappings()
	require.Len(t, orphans, 1)
	newCloudCertID := orphans[0].CloudCertID
	assert.True(t, strings.HasPrefix(newCloudCertID, "csv:"), "fallback product is csv: %s", newCloudCertID)
	assert.Equal(t, w.NewFP, orphans[0].CertFingerprint)
	assert.True(t, stub.DeleteRecordedFor(strings.TrimPrefix(newCloudCertID, "csv:")),
		"compensation cleaned the fallback csv artifact")
}

// Journey: volcano-product-aware-upload / steps 1-3 ( id-space-mutex + success
// forms ) — 同一证书束经 ProductAwareUploader 端口对四产品库逐一上传：产物
// {product}:{id} 前缀=目标产品、四产品 ID 两两互异（alb/nlb 共用 ALB 监听
// 证书库但不冲突）、csv 统一库零产物；逐 ID GetCert 回读在库（跨产品不误引，
// ID 归一断言口径对齐三云 ID 空间互斥测试先例）。
func TestVolcanoProductAwareUpload_PortDirectedIDSpaceMutex(t *testing.T) {
	stub := NewStubVolcanoCertLibrary()
	h := multicloudtest.NewHarness(t, nil)
	d := deployer.NewVolcanoDeployer(h.Mappings,
		deployer.WithVolcanoCertLibrary(stub),
		deployer.WithVolcanoRetryPolicy(multicloudtest.FastRetryPolicy()))

	bundle := certtest.NewBundle(t, "mutex.example.com", nil, nil)
	certPEM := string(bundle.CertPEM)

	creds, err := h.Creds.CloudCredential(context.Background(), "volcano", "acc-volcano")
	require.NoError(t, err)

	products := []domain.Product{domain.ProductCDN, domain.ProductWAF, domain.ProductALB, domain.ProductNLB}
	uploaded := map[string]string{} // normalized ID -> product
	rawIDs := map[string]bool{}     // cross-product raw-ID distinctness
	for _, product := range products {
		id, uerr := d.UploadCertForProduct(context.Background(), creds, string(product),
			certPEM, append([]byte(nil), bundle.KeyPEM...))
		require.NoError(t, uerr, "directed upload for %s", product)

		prefix, raw, ok := strings.Cut(id, ":")
		require.True(t, ok, "normalized {product}:{id}: %s", id)
		assert.Equal(t, string(product), prefix, "%s 上传产物前缀=目标产品", product)
		assert.False(t, rawIDs[raw], "同束证书跨产品 raw ID 互异: %s", raw)
		rawIDs[raw] = true
		uploaded[id] = string(product)
	}
	require.Len(t, rawIDs, 4, "four products, four distinct raw IDs ( alb/nlb share one library without collision )")

	// Stub 上传仅计数不落库：按确定性产物 ID 把在库状态补齐，使 GetCert 回读
	// 成为真实的「路由不误引」断言（若跨产品误路由，对应库查不到 → Exists=false）。
	notAfter := time.Now().Add(365 * 24 * time.Hour)
	notAfterUnix := notAfter.Unix()
	notAfterStr := notAfter.Format("2006-01-02 15:04:05")
	stub.SetCDNCert("cert-cdn-1", bundle.Fingerprint, notAfterUnix, "mutex.example.com")
	stub.SetWAFServiceCert(31, notAfterStr)
	stub.SetALBCert("cert-alb-1", notAfterStr)
	stub.SetALBCert("cert-alb-2", notAfterStr)
	for id := range uploaded {
		info, gerr := d.GetCert(context.Background(), creds, id)
		require.NoError(t, gerr, "GetCert roundtrip for %s", id)
		assert.True(t, info.Exists, "%s visible in its own product library ( no cross-product misreference )", id)
	}
	csvN, cdnN, wafN, albN := stub.UploadCounts()
	assert.Equal(t, 0, csvN, "产品库定向路径不落 csv 统一库")
	assert.Equal(t, 1, cdnN, "cdn 定向上传一次")
	assert.Equal(t, 1, wafN, "waf 定向上传一次")
	assert.Equal(t, 2, albN, "alb/nlb 共用 ALB 监听证书库上传 API 各一次")
}

// Journey: volcano-rollback-verify-window / step-2 ( mapping-fallback-no-record,
// invalid-target-rejected ) — GetCert 对无指纹通道库（waf/alb/nlb）的映射
// 回退边界：回退无果时指纹留空（fail-safe 占位——上层三判定阻断转人工，
// 不伪造有效性判定）；映射有记录时回退命中；云侧不在库 → Exists=false
// （三判定拦截，回滚不误绑）。
func TestVolcanoGetCertFingerprintFallback_NoRecordFailSafe(t *testing.T) {
	stub := NewStubVolcanoCertLibrary()
	h := multicloudtest.NewHarness(t, nil)
	d := deployer.NewVolcanoDeployer(h.Mappings,
		deployer.WithVolcanoCertLibrary(stub),
		deployer.WithVolcanoRetryPolicy(multicloudtest.FastRetryPolicy()))

	creds, err := h.Creds.CloudCredential(context.Background(), "volcano", "acc-volcano")
	require.NoError(t, err)
	notAfter := time.Now().Add(365 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	stub.SetWAFServiceCert(101, notAfter)

	// mapping-fallback-no-record：库在但映射无记录 → Exists=true、指纹留空。
	info, err := d.GetCert(context.Background(), creds, "waf:101")
	require.NoError(t, err)
	assert.True(t, info.Exists, "waf library cert present")
	assert.Empty(t, info.Fingerprint,
		"无指纹通道且映射无记录：指纹留空（fail-safe 占位，三判定阻断），不伪造")

	// 映射反查有记录：回退命中（既有解析链口径，与 dispatch journey 互证）。
	h.SeedMapping(multicloudtest.FP("getcert-waf-old"), "volcano", "acc-volcano", "waf:101")
	info, err = d.GetCert(context.Background(), creds, "waf:101")
	require.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Equal(t, multicloudtest.FP("getcert-waf-old"), info.Fingerprint,
		"映射反查回退填充指纹")

	// invalid-target-rejected：云侧不在库 → Exists=false（回滚三判定拦截）。
	info, err = d.GetCert(context.Background(), creds, "waf:404")
	require.NoError(t, err)
	assert.False(t, info.Exists, "absent cloud cert answers Exists=false ( rollback precheck rejects )")
}
