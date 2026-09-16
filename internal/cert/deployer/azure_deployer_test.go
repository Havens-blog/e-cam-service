// azure_deployer_test.go Azure CloudDeployer 单元测试：fake KV 适配覆盖五方法 ×
// 两产品（上传统一 CDN 口径、限流退避有界、ListReferences 指纹三级解析、经
// CloudAPIChannel 端到端两段式与绑定失败补偿）（cert-multicloud-deployers 任务 3）。
package deployer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/azure"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// azureTestSecretID 构造 KV secret ID（测试用上传产物/引用形态）
func azureTestSecretID(name string) string {
	return "https://vault-main.vault.azure.net/secrets/" + name + "/v1"
}

// ---------------------------------------------------------------------
// 测试替身：mock Azure 完整证书适配（azureCertAPI 窄接口）
// ---------------------------------------------------------------------

// fakeAzureCertAPI mock azure.CertAdapter：记录调用序列与上传名，支持按调用次数
// 注入错误（限流/一般失败）与逐次返回 ID。
type fakeAzureCertAPI struct {
	mu          sync.Mutex
	uploadCalls []string // "product:name" 逐次
	uploadIDs   []string // 逐次返回 KV secret ID；耗尽沿用末值（缺省 vault-main secret ID）
	uploadErrFn func(call int) error
	lastAcct    *sharedomain.CloudAccount
	binds       []string // "product:resource:cert"
	bindErrFn   func(call int) error
	bindErr     error
	listCalls   []string
	listRefs    map[string][]azure.CloudCertRef
	listErr     error
	getCalls    []string
	getInfo     map[string]azure.CloudCertInfo
	getErr      error
	getErrs     map[string]error // 按证书 ID 注入错误（未命中回退 getErr/空 info）
	cleanups    []string
	cleanupErr  error
}

func (f *fakeAzureCertAPI) UploadCert(_ context.Context, acct *sharedomain.CloudAccount, product, name, _, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.uploadCalls) + 1
	f.uploadCalls = append(f.uploadCalls, product+":"+name)
	f.lastAcct = acct
	if f.uploadErrFn != nil {
		if err := f.uploadErrFn(n); err != nil {
			return "", err
		}
	}
	id := azureTestSecretID("uploaded-cert")
	if len(f.uploadIDs) >= n && f.uploadIDs[n-1] != "" {
		id = f.uploadIDs[n-1]
	} else if len(f.uploadIDs) > 0 {
		id = f.uploadIDs[len(f.uploadIDs)-1]
	}
	return id, nil
}

func (f *fakeAzureCertAPI) BindResource(_ context.Context, _ *sharedomain.CloudAccount, product, resourceID, cloudCertID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.binds) + 1
	f.binds = append(f.binds, product+":"+resourceID+":"+cloudCertID)
	if f.bindErrFn != nil {
		if err := f.bindErrFn(n); err != nil {
			return err
		}
	}
	return f.bindErr
}

func (f *fakeAzureCertAPI) ListReferences(_ context.Context, _ *sharedomain.CloudAccount, product string) ([]azure.CloudCertRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls = append(f.listCalls, product)
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listRefs[product], nil
}

func (f *fakeAzureCertAPI) GetCert(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) (azure.CloudCertInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls = append(f.getCalls, cloudCertID)
	if err, ok := f.getErrs[cloudCertID]; ok {
		return azure.CloudCertInfo{}, err
	}
	if f.getErr != nil {
		return azure.CloudCertInfo{}, f.getErr
	}
	if info, ok := f.getInfo[cloudCertID]; ok {
		return info, nil
	}
	return azure.CloudCertInfo{}, nil
}

func (f *fakeAzureCertAPI) CleanupOrphan(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleanups = append(f.cleanups, cloudCertID)
	return f.cleanupErr
}

func (f *fakeAzureCertAPI) uploadsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.uploadCalls...)
}

func (f *fakeAzureCertAPI) bindsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.binds...)
}

func (f *fakeAzureCertAPI) getSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.getCalls...)
}

func (f *fakeAzureCertAPI) cleanupsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cleanups...)
}

// newTestAzureDeployer 装配被测部署器：fake 适配 + 确定性时间/随机后缀
// （名称唯一性可精确断言）+ 即时睡眠记录器。
func newTestAzureDeployer(fake *fakeAzureCertAPI, mappings domain.CloudCertMappingRepository) (*AzureDeployer, *sleepRecorder) {
	d := NewAzureDeployer(fake, mappings)
	rec := &sleepRecorder{}
	d.sleep = rec.sleep
	d.now = func() time.Time { return time.Unix(1765432100, 0) }
	counter := 0
	d.randHex = func(int) string { counter++; return fmt.Sprintf("%04x", counter) }
	return d, rec
}

func testAzureCreds() Credential {
	return Credential{
		Kind: CredentialKindCloudAK, Cloud: "azure", AccountKey: "acc-main",
		AccessKey: "client-id", Secret: []byte("test-client-secret"), KeyVersion: 1,
	}
}

func testAzureTarget() DeployTarget {
	return DeployTarget{
		Channel: "cloud_api", Cloud: "azure", Product: "cdn",
		AccountKey: "acc-main", ResourceID: "fd-1/fe-kv",
	}
}

// ---------------------------------------------------------------------
// 编译期断言：完整 CertAdapter 满足窄接口（部署器生产装配形态）
// ---------------------------------------------------------------------

var _ azureCertAPI = (*azure.CertAdapter)(nil)

// ---------------------------------------------------------------------
// AC-2：两产品路由（绑定 KV 引用归一化收敛在适配层，部署器直传）
// ---------------------------------------------------------------------

// 两产品按 DeployTarget.product 路由至适配方法（AC-2）；Azure 引用统一 KV
// secret ID 形态，归一化在适配层单点完成（Hard Rule），部署器幂等透传。
func TestAzureDeployerBindRoutesTwoProducts(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAzureCertAPI{}
	d, _ := newTestAzureDeployer(fake, nil)

	targets := map[string]string{
		"cdn": "fd-1/fe-kv",  // Front Door/{终结点}
		"alb": "agw-1/lsn-1", // ApplicationGateway/{监听器}
	}
	for _, product := range []string{"cdn", "alb"} {
		assert.NoError(t, d.BindResource(ctx, testAzureCreds(), product, targets[product], azureTestSecretID("cert-1")), product)
	}

	binds := fake.bindsSnapshot()
	require.Len(t, binds, 2, "两产品各一次绑定")
	assert.Equal(t, "cdn:fd-1/fe-kv:"+azureTestSecretID("cert-1"), binds[0], "Front Door 自定义域名 HTTPS 配置绑定")
	assert.Equal(t, "alb:agw-1/lsn-1:"+azureTestSecretID("cert-1"), binds[1], "App Gateway 监听器 SSL 证书引用绑定")
}

// 未支持产品（waf/nlb 等）：适配哨兵错误透传（Azure WAF policy 与 Load Balancer
// 无 TLS 终结证书面，不入支持集）。
func TestAzureDeployerBindUnsupportedProduct(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAzureCertAPI{bindErr: azure.ErrCertProductNotSupported}
	d, _ := newTestAzureDeployer(fake, nil)

	err := d.BindResource(ctx, testAzureCreds(), "waf", "fd-1/fe-kv", azureTestSecretID("cert-1"))
	assert.ErrorIs(t, err, azure.ErrCertProductNotSupported)
}

// 凭证归属云不符/凭证非法：显式拒绝且不触达适配层。
func TestAzureDeployerRejectsForeignCredential(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAzureCertAPI{}
	d, _ := newTestAzureDeployer(fake, nil)

	foreign := testAzureCreds()
	foreign.Cloud = "aliyun"
	_, err := d.UploadCert(ctx, foreign, testCertPEM, []byte(testKeyPEM))
	assert.ErrorContains(t, err, "not azure")

	invalid := testAzureCreds()
	invalid.AccessKey = ""
	_, err = d.UploadCert(ctx, invalid, testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, ErrInvalidCredential)

	assert.Empty(t, fake.uploadsSnapshot(), "校验失败不产生云侧调用")
}

// ---------------------------------------------------------------------
// AC-1：上传名唯一生成（ecam-{指纹前8}-{unix秒}-{随机}）+ 统一 CDN 口径
// ---------------------------------------------------------------------

// 名称规则与逐次唯一（C7：重试不复用可能已成功的名称——KV 删除按证书名删除
// 全体版本，名称复用会误删其他版本的证书）；第一段统一以 CDN 口径上传
// （KV 为账号级证书库，无 AWS CloudFront 式地域硬约束，口径选择纯为端口形状
// 与四云一致）。
func TestAzureUploadNameGeneration(t *testing.T) {
	bundle := certtest.NewBundle(t, "www.example.com", nil, nil)
	fake := &fakeAzureCertAPI{}
	d, _ := newTestAzureDeployer(fake, nil)

	ctx := context.Background()
	secretID, err := d.UploadCert(ctx, testAzureCreds(), string(bundle.CertPEM), bundle.KeyPEM)
	require.NoError(t, err)
	assert.Equal(t, azureTestSecretID("uploaded-cert"), secretID, "上传产物为 KV secret ID 形态")

	uploads := fake.uploadsSnapshot()
	require.Len(t, uploads, 1)
	product, name, ok := strings.Cut(uploads[0], ":")
	require.True(t, ok)
	assert.Equal(t, "cdn", product, "第一段统一以 CDN 口径上传（端口形状一致口径）")
	assert.Regexp(t, uploadNamePattern, name)
	assert.LessOrEqual(t, len(name), 63, "上传名沿用 63 字符上限（KV 上限 127 内取三云对齐口径）")
	assert.Contains(t, name, bundle.Fingerprint[:8], "指纹前 8 位来源=证书叶 DER SHA256")
	assert.Contains(t, name, "1765432100", "unix 秒时间戳分量")

	// 同材料重复生成：随机后缀保证唯一（C7）。
	names := []string{name}
	for i := 0; i < 5; i++ {
		_, err := d.UploadCert(ctx, testAzureCreds(), string(bundle.CertPEM), bundle.KeyPEM)
		require.NoError(t, err)
		names = append(names, strings.SplitN(fake.uploadsSnapshot()[len(names)], ":", 2)[1])
	}
	assert.Len(t, uniqueStrings(names), len(names), "逐次生成名互不相同")
}

// 超 63 字符防御性截断（随机后缀异常超长场景）。
func TestAzureUploadNameTruncation(t *testing.T) {
	d, _ := newTestAzureDeployer(&fakeAzureCertAPI{}, nil)
	d.randHex = func(int) string { return strings.Repeat("a", 80) }

	name := d.generateUploadName(testCertPEM)
	assert.Len(t, name, 63)
	assert.True(t, strings.HasPrefix(name, "ecam-"), "截断保留前缀")
}

// 材料缺失：显式拒绝。
func TestAzureDeployerUploadRejectsEmptyMaterial(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAzureCertAPI{}
	d, _ := newTestAzureDeployer(fake, nil)

	_, err := d.UploadCert(ctx, testAzureCreds(), "", []byte(testKeyPEM))
	assert.ErrorContains(t, err, "requires cert PEM and key PEM")
	_, err = d.UploadCert(ctx, testAzureCreds(), testCertPEM, nil)
	assert.ErrorContains(t, err, "requires cert PEM and key PEM")
	assert.Empty(t, fake.uploadsSnapshot())
}

// ---------------------------------------------------------------------
// AC-6：限流退避（固定序列，有界上限；Azure 请求计数限流语义经适配层
// 429 → 哨兵归一，本层只消费哨兵）
// ---------------------------------------------------------------------

// 限流后退避恢复：按固定序列睡眠，逐次换名重试（C7），最终成功。
func TestAzureDeployerRateLimitBackoffRecovers(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAzureCertAPI{uploadErrFn: func(call int) error {
		if call <= 2 {
			return fmt.Errorf("azure keyvault api throttled: %w", cloudx.ErrCloudRateLimited)
		}
		return nil
	}}
	d, rec := newTestAzureDeployer(fake, nil)

	secretID, err := d.UploadCert(ctx, testAzureCreds(), testCertPEM, []byte(testKeyPEM))
	require.NoError(t, err)
	assert.Equal(t, azureTestSecretID("uploaded-cert"), secretID)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, rec.snapshot(),
		"固定退避序列 1s/2s（默认策略前两档）")

	uploads := fake.uploadsSnapshot()
	assert.Len(t, uploads, 3)
	assert.Len(t, uniqueStrings(uploads), 3, "重试逐次换名（不得复用可能已成功的名称）")
}

// 限流持续：次数上限耗尽即失败，绝不无限重试（Hard Rule）；哨兵语义保留。
func TestAzureDeployerRateLimitExhaustsByAttempts(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAzureCertAPI{uploadErrFn: func(int) error {
		return fmt.Errorf("azure keyvault api throttled: %w", cloudx.ErrCloudRateLimited)
	}}
	d, rec := newTestAzureDeployer(fake, nil)

	_, err := d.UploadCert(ctx, testAzureCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited, "耗尽后哨兵仍可判定（5.7 映射 rate_limited/failed）")
	assert.ErrorContains(t, err, "retries exhausted after 5 attempts")
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second},
		rec.snapshot(), "默认固定序列全量消费后停止")
	assert.Len(t, fake.uploadsSnapshot(), 5, "默认 MaxAttempts=5")
}

// 退避总时长上限：下一档退避将超总时长即停止（Hard Rule：上限次数+总时长双闸）。
func TestAzureDeployerRateLimitExhaustsByTotalWait(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAzureCertAPI{uploadErrFn: func(int) error {
		return fmt.Errorf("throttled: %w", cloudx.ErrCloudRateLimited)
	}}
	d, rec := newTestAzureDeployer(fake, nil)
	d.retry = RetryPolicy{
		MaxAttempts:  10,
		Backoffs:     []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second},
		MaxTotalWait: 3 * time.Second,
	}

	_, err := d.UploadCert(ctx, testAzureCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited)
	assert.ErrorContains(t, err, "total backoff cap")
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, rec.snapshot(),
		"累计 3s 后，下一档 4s 超总时长上限即止（尝试 3 次）")
	assert.Len(t, fake.uploadsSnapshot(), 3)
}

// 退避睡眠被取消：返回 ctx 错误，不再继续尝试。
func TestAzureDeployerBackoffCanceled(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAzureCertAPI{uploadErrFn: func(int) error {
		return fmt.Errorf("throttled: %w", cloudx.ErrCloudRateLimited)
	}}
	d, rec := newTestAzureDeployer(fake, nil)
	rec.err = context.DeadlineExceeded

	_, err := d.UploadCert(ctx, testAzureCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Len(t, fake.uploadsSnapshot(), 1, "首试失败进入退避，退避中断即止（重试未发生）")
}

// 非限流错误：立即返回，不退避不重试。
func TestAzureDeployerNonRetryableNoRetry(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAzureCertAPI{uploadErrFn: func(int) error {
		return errors.New("certificate and key do not match")
	}}
	d, rec := newTestAzureDeployer(fake, nil)

	_, err := d.UploadCert(ctx, testAzureCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorContains(t, err, "certificate and key do not match")
	assert.Empty(t, rec.snapshot(), "一般失败不退避")
	assert.Len(t, fake.uploadsSnapshot(), 1)
}

// 退避策略归一化：零值/非法回退缺省保守值。
func TestAzureRetryPolicyOptionNormalization(t *testing.T) {
	def := DefaultRetryPolicy()
	assert.Equal(t, def, RetryPolicy{}.normalized())

	custom := RetryPolicy{
		MaxAttempts:  2,
		Backoffs:     []time.Duration{time.Second},
		MaxTotalWait: time.Second,
	}
	d := NewAzureDeployer(&fakeAzureCertAPI{}, nil, WithAzureRetryPolicy(custom))
	assert.Equal(t, custom, d.retry)
	assert.Equal(t, def, NewAzureDeployer(&fakeAzureCertAPI{}, nil, WithAzureRetryPolicy(RetryPolicy{})).retry,
		"零值配置回退缺省保守值")
}

// ---------------------------------------------------------------------
// AC-5：ListReferences（复用发现适配引用形态 + 指纹三级解析）
// ---------------------------------------------------------------------

// 指纹解析三级口径：映射反查 → GetCert（SHA256 对齐）→ 确定性占位指纹；
// 同云证书多引用只查一次 GetCert。
func TestAzureDeployerListReferencesFingerprints(t *testing.T) {
	ctx := context.Background()
	fpMapped := strings.Repeat("a", 64)
	fpFromCloud := strings.Repeat("b", 64)
	certA := azureTestSecretID("cert-a")
	certB := azureTestSecretID("cert-b")
	certC := azureTestSecretID("cert-c")
	certD := azureTestSecretID("cert-d")

	mappings := certtest.NewFakeCloudCertMappingRepo()
	require.NoError(t, mappings.Upsert(ctx, &domain.CloudCertMapping{
		CertFingerprint: fpMapped, Cloud: "azure", AccountKey: "acc-main",
		CloudCertID: certA, Status: domain.MappingStatusActive,
	}))

	fake := &fakeAzureCertAPI{
		listRefs: map[string][]azure.CloudCertRef{
			"cdn": {
				{Cloud: "azure", Product: "cdn", ResourceID: "fd-1/fe-a", ReferencedCloudCertID: certA, AccountKey: "acc-main"},
				{Cloud: "azure", Product: "cdn", ResourceID: "fd-1/fe-b", ReferencedCloudCertID: certB, AccountKey: "acc-main"},
				{Cloud: "azure", Product: "cdn", ResourceID: "fd-2/fe-c", ReferencedCloudCertID: certC, AccountKey: "acc-main"},
				{Cloud: "azure", Product: "cdn", ResourceID: "fd-2/fe-d", ReferencedCloudCertID: certD, AccountKey: "acc-main"},
			},
		},
		getInfo: map[string]azure.CloudCertInfo{
			certB: {Exists: true, Fingerprint: fpFromCloud, NotAfter: time.Now().Add(90 * 24 * time.Hour)},
		},
		// certC：GetCert 失败（非证书 secret 等无法复核场景）
		getErrs: map[string]error{
			certC: errors.New("azure keyvault api error: not a certificate secret"),
		},
	}
	d, _ := newTestAzureDeployer(fake, mappings)

	refs, err := d.ListReferences(ctx, testAzureCreds(), "cdn")
	require.NoError(t, err)
	require.Len(t, refs, 4)

	byResource := make(map[string]domain.CertReference, len(refs))
	for _, r := range refs {
		byResource[r.ResourceID] = r
	}
	assert.Equal(t, fpMapped, byResource["fd-1/fe-a"].CertFingerprint, "映射反查命中")
	assert.Equal(t, fpFromCloud, byResource["fd-1/fe-b"].CertFingerprint, "GetCert SHA256 对齐口径")
	assert.Equal(t, fpPlaceholder("azure", "acc-main", certC), byResource["fd-2/fe-c"].CertFingerprint,
		"GetCert 失败 → 确定性占位指纹（不中断发现）")
	assert.Equal(t, fpPlaceholder("azure", "acc-main", certD), byResource["fd-2/fe-d"].CertFingerprint,
		"GetCert 未命中 → 确定性占位指纹")

	assert.Equal(t, []string{certB, certC, certD},
		fake.getSnapshot(), "映射命中不查 GetCert；同证书多引用去重仅查一次")

	for _, r := range refs {
		assert.Equal(t, domain.CloudAzure, r.Cloud)
		assert.Equal(t, domain.ProductCDN, r.Product)
		assert.Equal(t, "acc-main", r.AccountKey)
	}
}

// mappings 缺省（nil）：跳过映射反查，直接 GetCert fallback → 占位指纹。
func TestAzureDeployerListReferencesWithoutMappings(t *testing.T) {
	ctx := context.Background()
	certID := azureTestSecretID("agw-cert")
	fake := &fakeAzureCertAPI{
		listRefs: map[string][]azure.CloudCertRef{
			"alb": {{Cloud: "azure", Product: "alb", ResourceID: "agw-1/lsn-kv", ReferencedCloudCertID: certID, AccountKey: "acc-main"}},
		},
	}
	d, _ := newTestAzureDeployer(fake, nil)

	refs, err := d.ListReferences(ctx, testAzureCreds(), "alb")
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, fpPlaceholder("azure", "acc-main", certID), refs[0].CertFingerprint)
	assert.Equal(t, []string{certID}, fake.getSnapshot(), "无映射仍尝试 GetCert")
}

// ListReferences 限流：退避后整体重试（整页重取）。
func TestAzureDeployerListReferencesRateLimited(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAzureCertAPI{
		listErr:  fmt.Errorf("list throttled: %w", cloudx.ErrCloudRateLimited),
		listRefs: map[string][]azure.CloudCertRef{},
	}
	d, _ := newTestAzureDeployer(fake, nil)

	_, err := d.ListReferences(ctx, testAzureCreds(), "cdn")
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited)
	assert.Len(t, fake.listCalls, 5)
}

// ---------------------------------------------------------------------
// AC-3 / AC-4：GetCert / CleanupOrphan
// ---------------------------------------------------------------------

// GetCert 字段转换（Exists/NotAfter/Fingerprint）+ 云侧已删除=Exists=false 非错误
// （回滚目标有效性校验依据：5.8 三判定消费）。
func TestAzureDeployerGetCert(t *testing.T) {
	ctx := context.Background()
	notAfter := time.Now().Add(30 * 24 * time.Hour)
	fp64 := strings.Repeat("d", 64)
	certID := azureTestSecretID("cert-1")
	fake := &fakeAzureCertAPI{
		getInfo: map[string]azure.CloudCertInfo{
			certID: {Exists: true, Fingerprint: fp64, NotAfter: notAfter},
		},
	}
	d, _ := newTestAzureDeployer(fake, nil)

	info, err := d.GetCert(ctx, testAzureCreds(), certID)
	require.NoError(t, err)
	assert.True(t, info.Exists, "回滚目标有效性校验：KV 在库存在性")
	assert.Equal(t, fp64, info.Fingerprint, "SHA256 对齐口径指纹（回滚指纹等值比对可用）")
	assert.WithinDuration(t, notAfter, info.NotAfter, time.Second)

	info, err = d.GetCert(ctx, testAzureCreds(), azureTestSecretID("gone-cert"))
	require.NoError(t, err)
	assert.False(t, info.Exists, "云侧已删除（含 KV 软删除态）=Exists=false 非错误")
}

// CleanupOrphan 透传 + 限流退避恢复。
func TestAzureDeployerCleanupOrphanRateLimited(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAzureCertAPI{cleanupErr: fmt.Errorf("kv delete throttled: %w", cloudx.ErrCloudRateLimited)}
	d, rec := newTestAzureDeployer(fake, nil)

	err := d.CleanupOrphan(ctx, testAzureCreds(), azureTestSecretID("orphan-cert"))
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited, "补偿清理同样有界重试，耗尽透传哨兵")
	assert.Len(t, fake.cleanupsSnapshot(), 5)
	assert.Len(t, rec.snapshot(), 4)
}

// ---------------------------------------------------------------------
// AC-2 / AC-4：经 5.3 CloudAPIChannel 端到端（AzureDeployer 注入实例）
// ---------------------------------------------------------------------

// 两段式成功端到端：DeployResult 三字段 + 映射 active（KV secret ID 写入映射）。
func TestAzureDeployerChannelDeployTwoStageSuccess(t *testing.T) {
	newCertID := azureTestSecretID("new-cert")
	oldCertID := azureTestSecretID("old-cert")
	fake := &fakeAzureCertAPI{uploadIDs: []string{newCertID}}
	mat := &fakeMaterial{certPEM: testCertPEM, keyPEM: []byte(testKeyPEM), keyVersion: 1}
	old := &fakeOldRefs{
		found: true,
		ref: domain.CertReference{
			Cloud: domain.CloudAzure, Product: domain.ProductCDN,
			ResourceID: "fd-1/fe-kv", ReferencedCloudCertID: oldCertID, AccountKey: "acc-main",
		},
	}
	dep, _ := newTestAzureDeployer(fake, nil)
	mappings := certtest.NewFakeCloudCertMappingRepo()
	ch := NewCloudAPIChannel(mappings, mat, old)
	require.NoError(t, ch.RegisterDeployer("azure", dep, "cdn", "alb"))

	res, err := ch.Deploy(context.Background(), testAzureCreds(), testAzureTarget(), testFingerprint)
	require.NoError(t, err)

	assert.Equal(t, newCertID, res.NewCloudCertID, "第一段产物=KV secret ID（映射表直接承载）")
	assert.Equal(t, oldCertID, res.OldCloudCertID, "执行前从引用快照读取（回滚依据）")
	assert.True(t, res.OrphanCandidate, "旧云证书被替换 → 孤儿候选")

	uploads := fake.uploadsSnapshot()
	binds := fake.bindsSnapshot()
	require.Len(t, uploads, 1)
	require.Len(t, binds, 1)
	assert.Equal(t, "cdn:fd-1/fe-kv:"+newCertID, binds[0], "绑定用第一段产物（KV secret ID）")
	assert.Regexp(t, uploadNamePattern, strings.SplitN(uploads[0], ":", 2)[1], "上传名唯一规则")
	assert.Empty(t, fake.cleanupsSnapshot(), "成功路径不做补偿清理")

	got, err := mappings.FindByCloudCertID(t.Context(), "azure", "acc-main", newCertID)
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusActive, got.Status)

	// 凭证转换：AccountKey→Name、AK/SK 明文仅内存透传。
	fake.mu.Lock()
	acct := fake.lastAcct
	fake.mu.Unlock()
	assert.Equal(t, "acc-main", acct.Name)
	assert.Equal(t, sharedomain.CloudProviderAzure, acct.Provider)
	assert.Equal(t, "client-id", acct.AccessKeyID)
}

// 第二段绑定失败端到端：CleanupOrphan 补偿清理 + 映射 active→orphan + OrphanCandidate=true。
func TestAzureDeployerChannelBindFailureCompensates(t *testing.T) {
	newCertID := azureTestSecretID("new-cert")
	fake := &fakeAzureCertAPI{
		uploadIDs: []string{newCertID},
		bindErr:   errors.New("frontend endpoint not found"),
	}
	mat := &fakeMaterial{certPEM: testCertPEM, keyPEM: []byte(testKeyPEM), keyVersion: 1}
	dep, _ := newTestAzureDeployer(fake, nil)
	mappings := certtest.NewFakeCloudCertMappingRepo()
	ch := NewCloudAPIChannel(mappings, mat, &fakeOldRefs{})
	require.NoError(t, ch.RegisterDeployer("azure", dep, "cdn", "alb"))

	res, err := ch.Deploy(context.Background(), testAzureCreds(), testAzureTarget(), testFingerprint)
	require.Error(t, err)
	assert.ErrorContains(t, err, "frontend endpoint not found")

	assert.True(t, res.OrphanCandidate)
	assert.Equal(t, newCertID, res.NewCloudCertID)

	// 补偿清理：未绑定云侧证书经 CleanupOrphan 删除（KV 404 幂等重放安全）。
	assert.Equal(t, []string{newCertID}, fake.cleanupsSnapshot())

	got, err := mappings.FindByCloudCertID(t.Context(), "azure", "acc-main", newCertID)
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusOrphan, got.Status)
}
