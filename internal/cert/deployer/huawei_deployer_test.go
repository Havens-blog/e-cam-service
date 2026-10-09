package deployer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cloudx-sdk"
	"github.com/Havens-blog/e-cloudx-sdk/huawei"
	sharedomain "github.com/Havens-blog/e-cloudx-sdk/domain"
)

// ---------------------------------------------------------------------
// 测试替身：mock 华为云完整证书适配（huaweiCertAPI 窄接口）
// ---------------------------------------------------------------------

// fakeHuaweiCertAPI mock 华为云 CertAdapter：记录调用序列与上传名，支持按调用
// 次数注入错误（限流/命名冲突/一般失败）与逐次返回 ID。
type fakeHuaweiCertAPI struct {
	mu          sync.Mutex
	uploadCalls []string // "product:name" 逐次
	uploadIDs   []string // 逐次返回 ID；耗尽沿用末值（缺省 scm-9001）
	uploadErrFn func(call int) error
	lastAcct    *sharedomain.CloudAccount
	binds       []string // "product:resource:cert"
	bindErrFn   func(call int) error
	bindErr     error
	listCalls   []string
	listRefs    map[string][]huawei.CloudCertRef
	listErr     error
	getCalls    []string
	getInfo     map[string]huawei.CloudCertInfo
	getErr      error
	cleanups    []string
	cleanupErr  error
}

func (f *fakeHuaweiCertAPI) UploadCert(_ context.Context, acct *sharedomain.CloudAccount, product, name, _, _ string) (string, error) {
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
	id := "scm-9001"
	if len(f.uploadIDs) >= n && f.uploadIDs[n-1] != "" {
		id = f.uploadIDs[n-1]
	} else if len(f.uploadIDs) > 0 {
		id = f.uploadIDs[len(f.uploadIDs)-1]
	}
	return id, nil
}

func (f *fakeHuaweiCertAPI) BindResource(_ context.Context, _ *sharedomain.CloudAccount, product, resourceID, cloudCertID string) error {
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

func (f *fakeHuaweiCertAPI) ListReferences(_ context.Context, _ *sharedomain.CloudAccount, product string) ([]huawei.CloudCertRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls = append(f.listCalls, product)
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listRefs[product], nil
}

func (f *fakeHuaweiCertAPI) GetCert(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) (huawei.CloudCertInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls = append(f.getCalls, cloudCertID)
	if f.getErr != nil {
		return huawei.CloudCertInfo{}, f.getErr
	}
	if info, ok := f.getInfo[cloudCertID]; ok {
		return info, nil
	}
	return huawei.CloudCertInfo{}, nil
}

func (f *fakeHuaweiCertAPI) CleanupOrphan(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleanups = append(f.cleanups, cloudCertID)
	return f.cleanupErr
}

func (f *fakeHuaweiCertAPI) uploadsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.uploadCalls...)
}

func (f *fakeHuaweiCertAPI) bindsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.binds...)
}

func (f *fakeHuaweiCertAPI) getSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.getCalls...)
}

func (f *fakeHuaweiCertAPI) cleanupsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cleanups...)
}

// newTestHuaweiDeployer 装配被测部署器：fake 适配 + 确定性时间/随机后缀
// （名称唯一性可精确断言）+ 即时睡眠记录器。
func newTestHuaweiDeployer(fake *fakeHuaweiCertAPI, mappings domain.CloudCertMappingRepository) (*HuaweiDeployer, *sleepRecorder) {
	d := NewHuaweiDeployer(fake, mappings)
	rec := &sleepRecorder{}
	d.sleep = rec.sleep
	d.now = func() time.Time { return time.Unix(1765432100, 0) }
	counter := 0
	d.randHex = func(int) string { counter++; return fmt.Sprintf("%04x", counter) }
	return d, rec
}

func testHuaweiCreds() Credential {
	return Credential{
		Kind: CredentialKindCloudAK, Cloud: "huawei", AccountKey: "acc-main",
		AccessKey: "HW-test-ak", Secret: []byte("test-sk-plaintext"), KeyVersion: 1,
	}
}

func testHuaweiTarget() DeployTarget {
	return DeployTarget{
		Channel: "cloud_api", Cloud: "huawei", Product: "cdn",
		AccountKey: "acc-main", ResourceID: "www.example.com",
	}
}

// ---------------------------------------------------------------------
// AC-2：四产品路由 + CloudDeployer 端口实现
// ---------------------------------------------------------------------

// 四产品按 DeployTarget.product 路由至适配方法（AC-2）；华为云 ELB 监听
// 引用即裸 SCM 证书 ID（SCM 全局服务，无 aliyun 式地域后缀形态），归一化
// 幂等透传。
func TestHuaweiDeployerBindRoutesFourProducts(t *testing.T) {
	ctx := context.Background()
	fake := &fakeHuaweiCertAPI{}
	d, _ := newTestHuaweiDeployer(fake, nil)

	for _, product := range []string{"cdn", "waf", "alb", "nlb"} {
		assert.NoError(t, d.BindResource(ctx, testHuaweiCreds(), product, "www.example.com", "scm-9001"))
	}

	binds := fake.bindsSnapshot()
	assert.Len(t, binds, 4, "四产品各一次绑定")
	assert.Equal(t, "cdn:www.example.com:scm-9001", binds[0], "CDN 域名级绑定，SCM ID 直传")
	assert.Equal(t, "waf:www.example.com:scm-9001", binds[1], "WAF 域名级绑定")
	assert.Equal(t, "alb:www.example.com:scm-9001", binds[2], "ALB（ELB L7）监听级绑定，裸 SCM ID 不追加地域后缀")
	assert.Equal(t, "nlb:www.example.com:scm-9001", binds[3], "NLB（ELB L4）监听级绑定")
}

// 监听证书 ID 形态归一化幂等：裸 SCM ID 与任意既有形态引用均原样透传
// （发现引用/回滚旧 ID 不被改写）。
func TestHuaweiDeployerBindNormalizesListenerCertID(t *testing.T) {
	assert.Equal(t, "scm-9001", normalizeHuaweiListenerCertID("alb", "scm-9001"))
	assert.Equal(t, "scm-9001", normalizeHuaweiListenerCertID("nlb", "scm-9001"))
	assert.Equal(t, "scm-9001", normalizeHuaweiListenerCertID("cdn", "scm-9001"))
	assert.Equal(t, "", normalizeHuaweiListenerCertID("alb", ""), "空 ID 保持空（显式失败归适配层）")
}

// 未支持产品（dcdn/clb 等）：适配哨兵错误透传。
func TestHuaweiDeployerBindUnsupportedProduct(t *testing.T) {
	ctx := context.Background()
	fake := &fakeHuaweiCertAPI{bindErr: huawei.ErrCertProductNotSupported}
	d, _ := newTestHuaweiDeployer(fake, nil)

	err := d.BindResource(ctx, testHuaweiCreds(), "clb", "lb-1", "scm-9001")
	assert.ErrorIs(t, err, huawei.ErrCertProductNotSupported)
}

// 凭证归属云不符/凭证非法：显式拒绝且不触达适配层。
func TestHuaweiDeployerRejectsForeignCredential(t *testing.T) {
	ctx := context.Background()
	fake := &fakeHuaweiCertAPI{}
	d, _ := newTestHuaweiDeployer(fake, nil)

	foreign := testHuaweiCreds()
	foreign.Cloud = "aliyun"
	_, err := d.UploadCert(ctx, foreign, testCertPEM, []byte(testKeyPEM))
	assert.ErrorContains(t, err, "not huawei")

	invalid := testHuaweiCreds()
	invalid.AccessKey = ""
	_, err = d.UploadCert(ctx, invalid, testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, ErrInvalidCredential)

	assert.Empty(t, fake.uploadsSnapshot(), "校验失败不产生云侧调用")
}

// ---------------------------------------------------------------------
// AC-1：上传名唯一生成（ecam-{指纹前8}-{unix秒}-{随机}，≤63 字符）
// ---------------------------------------------------------------------

// 名称规则与逐次唯一（C7：重试不复用可能已成功的名称）；真实证书束时
// 指纹前缀 = 台账指纹前 8 位。
func TestHuaweiUploadNameGeneration(t *testing.T) {
	bundle := certtest.NewBundle(t, "www.example.com", nil, nil)
	fake := &fakeHuaweiCertAPI{}
	d, _ := newTestHuaweiDeployer(fake, nil)

	ctx := context.Background()
	id, err := d.UploadCert(ctx, testHuaweiCreds(), string(bundle.CertPEM), bundle.KeyPEM)
	assert.NoError(t, err)
	assert.Equal(t, "scm-9001", id)

	uploads := fake.uploadsSnapshot()
	assert.Len(t, uploads, 1)
	product, name, ok := strings.Cut(uploads[0], ":")
	assert.True(t, ok)
	assert.Equal(t, "cdn", product, "第一段统一以 CDN 口径上传（SCM ID 为统一云证书 ID）")
	assert.Regexp(t, uploadNamePattern, name)
	assert.LessOrEqual(t, len(name), 63, "SCM 证书名 ≤63 字符")
	assert.Contains(t, name, bundle.Fingerprint[:8], "指纹前 8 位来源=证书叶 DER SHA256")
	assert.Contains(t, name, "1765432100", "unix 秒时间戳分量")

	// 同材料重复生成：随机后缀保证唯一（C7）。
	names := []string{name}
	for i := 0; i < 5; i++ {
		_, err := d.UploadCert(ctx, testHuaweiCreds(), string(bundle.CertPEM), bundle.KeyPEM)
		assert.NoError(t, err)
		names = append(names, strings.SplitN(fake.uploadsSnapshot()[len(names)], ":", 2)[1])
	}
	assert.Len(t, uniqueStrings(names), len(names), "逐次生成名互不相同")
}

// 超 63 字符防御性截断（随机后缀异常超长场景）。
func TestHuaweiUploadNameTruncation(t *testing.T) {
	d, _ := newTestHuaweiDeployer(&fakeHuaweiCertAPI{}, nil)
	d.randHex = func(int) string { return strings.Repeat("a", 80) }

	name := d.generateUploadName(testCertPEM)
	assert.Len(t, name, 63)
	assert.True(t, strings.HasPrefix(name, "ecam-"), "截断保留前缀")
}

// 材料缺失：显式拒绝。
func TestHuaweiDeployerUploadRejectsEmptyMaterial(t *testing.T) {
	ctx := context.Background()
	fake := &fakeHuaweiCertAPI{}
	d, _ := newTestHuaweiDeployer(fake, nil)

	_, err := d.UploadCert(ctx, testHuaweiCreds(), "", []byte(testKeyPEM))
	assert.ErrorContains(t, err, "requires cert PEM and key PEM")
	_, err = d.UploadCert(ctx, testHuaweiCreds(), testCertPEM, nil)
	assert.ErrorContains(t, err, "requires cert PEM and key PEM")
	assert.Empty(t, fake.uploadsSnapshot())
}

// ---------------------------------------------------------------------
// AC-6：限流退避（固定序列，有界上限）+ 命名冲突换名
// ---------------------------------------------------------------------

// 限流后退避恢复：按固定序列睡眠，逐次换名重试（C7），最终成功。
func TestHuaweiDeployerRateLimitBackoffRecovers(t *testing.T) {
	ctx := context.Background()
	fake := &fakeHuaweiCertAPI{uploadErrFn: func(call int) error {
		if call <= 2 {
			return fmt.Errorf("huawei scm api throttled: %w", cloudx.ErrCloudRateLimited)
		}
		return nil
	}}
	d, rec := newTestHuaweiDeployer(fake, nil)

	id, err := d.UploadCert(ctx, testHuaweiCreds(), testCertPEM, []byte(testKeyPEM))
	assert.NoError(t, err)
	assert.Equal(t, "scm-9001", id)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, rec.snapshot(),
		"固定退避序列 1s/2s（默认策略前两档）")

	uploads := fake.uploadsSnapshot()
	assert.Len(t, uploads, 3)
	assert.Len(t, uniqueStrings(uploads), 3, "重试逐次换名（不得复用可能已成功的名称）")
}

// 限流持续：次数上限耗尽即失败，绝不无限重试（Hard Rule）；哨兵语义保留。
func TestHuaweiDeployerRateLimitExhaustsByAttempts(t *testing.T) {
	ctx := context.Background()
	fake := &fakeHuaweiCertAPI{uploadErrFn: func(int) error {
		return fmt.Errorf("huawei scm api throttled: %w", cloudx.ErrCloudRateLimited)
	}}
	d, rec := newTestHuaweiDeployer(fake, nil)

	_, err := d.UploadCert(ctx, testHuaweiCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited, "耗尽后哨兵仍可判定（5.7 映射 rate_limited/failed）")
	assert.ErrorContains(t, err, "retries exhausted after 5 attempts")
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second},
		rec.snapshot(), "默认固定序列全量消费后停止")
	assert.Len(t, fake.uploadsSnapshot(), 5, "默认 MaxAttempts=5")
}

// 退避总时长上限：下一档退避将超总时长即停止（Hard Rule：上限次数+总时长双闸）。
func TestHuaweiDeployerRateLimitExhaustsByTotalWait(t *testing.T) {
	ctx := context.Background()
	fake := &fakeHuaweiCertAPI{uploadErrFn: func(int) error {
		return fmt.Errorf("throttled: %w", cloudx.ErrCloudRateLimited)
	}}
	d, rec := newTestHuaweiDeployer(fake, nil)
	d.retry = RetryPolicy{
		MaxAttempts:  10,
		Backoffs:     []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second},
		MaxTotalWait: 3 * time.Second,
	}

	_, err := d.UploadCert(ctx, testHuaweiCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited)
	assert.ErrorContains(t, err, "total backoff cap")
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, rec.snapshot(),
		"累计 3s 后，下一档 4s 超总时长上限即止（尝试 3 次）")
	assert.Len(t, fake.uploadsSnapshot(), 3)
}

// 退避睡眠被取消：返回 ctx 错误，不再继续尝试。
func TestHuaweiDeployerBackoffCanceled(t *testing.T) {
	ctx := context.Background()
	fake := &fakeHuaweiCertAPI{uploadErrFn: func(int) error {
		return fmt.Errorf("throttled: %w", cloudx.ErrCloudRateLimited)
	}}
	d, rec := newTestHuaweiDeployer(fake, nil)
	rec.err = context.DeadlineExceeded

	_, err := d.UploadCert(ctx, testHuaweiCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Len(t, fake.uploadsSnapshot(), 1, "首试失败进入退避，退避中断即止（重试未发生）")
}

// 非限流错误：立即返回，不退避不重试。
func TestHuaweiDeployerNonRetryableNoRetry(t *testing.T) {
	ctx := context.Background()
	fake := &fakeHuaweiCertAPI{uploadErrFn: func(int) error {
		return errors.New("cert and key mismatch")
	}}
	d, rec := newTestHuaweiDeployer(fake, nil)

	_, err := d.UploadCert(ctx, testHuaweiCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorContains(t, err, "cert and key mismatch")
	assert.Empty(t, rec.snapshot(), "一般失败不退避")
	assert.Len(t, fake.uploadsSnapshot(), 1)
}

// 上传名冲突：立即换名重试（不睡眠），同一重试预算内恢复（与 5.4 B2 同口径；
// SCM name 非唯一键、DuplicateCheck=false 下该分支常态休眠，保守启发式兜底）。
func TestHuaweiDeployerUploadNameConflictRetries(t *testing.T) {
	ctx := context.Background()
	fake := &fakeHuaweiCertAPI{uploadErrFn: func(call int) error {
		if call == 1 {
			return errors.New("certificate name already exists")
		}
		return nil
	}}
	d, rec := newTestHuaweiDeployer(fake, nil)

	_, err := d.UploadCert(ctx, testHuaweiCreds(), testCertPEM, []byte(testKeyPEM))
	assert.NoError(t, err, "命名冲突可重试：重新生成名后成功")
	assert.Empty(t, rec.snapshot(), "冲突重试不占退避睡眠")

	uploads := fake.uploadsSnapshot()
	assert.Len(t, uploads, 2)
	assert.NotEqual(t, uploads[0], uploads[1], "重试使用新名称")
}

// 冲突误判防御：与名称无关的错误不走冲突重试。
func TestHuaweiDeployerNameConflictHeuristicNegative(t *testing.T) {
	assert.False(t, isCertNameConflictErr(nil))
	assert.False(t, isCertNameConflictErr(errors.New("listener not found")))
	assert.False(t, isCertNameConflictErr(fmt.Errorf("api error: %w", cloudx.ErrCloudRateLimited)))
	assert.True(t, isCertNameConflictErr(errors.New("证书名称重复")))
}

// 退避策略归一化：零值/非法回退缺省保守值。
func TestHuaweiRetryPolicyOptionNormalization(t *testing.T) {
	def := DefaultRetryPolicy()
	assert.Equal(t, def, RetryPolicy{}.normalized())

	custom := RetryPolicy{
		MaxAttempts:  2,
		Backoffs:     []time.Duration{time.Second},
		MaxTotalWait: time.Second,
	}
	d := NewHuaweiDeployer(&fakeHuaweiCertAPI{}, nil, WithHuaweiRetryPolicy(custom))
	assert.Equal(t, custom, d.retry)
	assert.Equal(t, def, NewHuaweiDeployer(&fakeHuaweiCertAPI{}, nil, WithHuaweiRetryPolicy(RetryPolicy{})).retry,
		"零值配置回退缺省保守值")
}

// ---------------------------------------------------------------------
// AC-5：ListReferences（复用发现适配引用形态 + 指纹三级解析）
// ---------------------------------------------------------------------

// 指纹解析三级口径：映射反查 → GetCert（SHA256 对齐）→ 确定性占位指纹；
// 同云证书多引用只查一次 GetCert。华为云特有：SHA-1 形态指纹（40hex，SCM
// 原生口径）永不通过 64 hex 对齐校验 → 占位指纹。
func TestHuaweiDeployerListReferencesFingerprints(t *testing.T) {
	ctx := context.Background()
	fpMapped := strings.Repeat("a", 64)
	fpFromCloud := strings.Repeat("b", 64)
	fpSHA1 := strings.Repeat("c", 40) // SCM 原生 SHA-1 口径

	mappings := certtest.NewFakeCloudCertMappingRepo()
	assert.NoError(t, mappings.Upsert(ctx, &domain.CloudCertMapping{
		CertFingerprint: fpMapped, Cloud: "huawei", AccountKey: "acc-main",
		CloudCertID: "scm-mapped", Status: domain.MappingStatusActive,
	}))

	fake := &fakeHuaweiCertAPI{
		listRefs: map[string][]huawei.CloudCertRef{
			"waf": {
				{Cloud: "huawei", Product: "waf", ResourceID: "www.example.com", ReferencedCloudCertID: "scm-mapped", AccountKey: "acc-main"},
				{Cloud: "huawei", Product: "waf", ResourceID: "api.example.com", ReferencedCloudCertID: "scm-aligned", AccountKey: "acc-main"},
				{Cloud: "huawei", Product: "waf", ResourceID: "m.example.com", ReferencedCloudCertID: "scm-sha1", AccountKey: "acc-main"},
				{Cloud: "huawei", Product: "waf", ResourceID: "s.example.com", ReferencedCloudCertID: "scm-ghost", AccountKey: "acc-main"},
			},
		},
		getInfo: map[string]huawei.CloudCertInfo{
			"scm-aligned": {Exists: true, Fingerprint: fpFromCloud, NotAfter: time.Now().Add(90 * 24 * time.Hour)},
			"scm-sha1":    {Exists: true, Fingerprint: fpSHA1},
		},
	}
	d, _ := newTestHuaweiDeployer(fake, mappings)

	refs, err := d.ListReferences(ctx, testHuaweiCreds(), "waf")
	assert.NoError(t, err)
	assert.Len(t, refs, 4)

	byResource := make(map[string]domain.CertReference, len(refs))
	for _, r := range refs {
		byResource[r.ResourceID] = r
	}
	assert.Equal(t, fpMapped, byResource["www.example.com"].CertFingerprint, "映射反查命中")
	assert.Equal(t, fpFromCloud, byResource["api.example.com"].CertFingerprint, "GetCert SHA256 对齐口径")
	assert.NotEqual(t, fpSHA1, byResource["m.example.com"].CertFingerprint, "SHA-1 指纹不通过对齐校验")
	assert.Equal(t, fpPlaceholder("huawei", "acc-main", "scm-sha1"), byResource["m.example.com"].CertFingerprint,
		"非对齐口径 → 确定性占位指纹")
	assert.Equal(t, fpPlaceholder("huawei", "acc-main", "scm-ghost"), byResource["s.example.com"].CertFingerprint,
		"GetCert 未命中 → 确定性占位指纹")

	assert.Equal(t, []string{"scm-aligned", "scm-sha1", "scm-ghost"}, fake.getSnapshot(),
		"映射命中不查 GetCert；同证书多引用去重仅查一次")

	for _, r := range refs {
		assert.Equal(t, domain.CloudHuawei, r.Cloud)
		assert.Equal(t, domain.ProductWAF, r.Product)
		assert.Equal(t, "acc-main", r.AccountKey)
	}
}

// mappings 缺省（nil）：跳过映射反查，直接 GetCert fallback → 占位指纹。
func TestHuaweiDeployerListReferencesWithoutMappings(t *testing.T) {
	ctx := context.Background()
	fake := &fakeHuaweiCertAPI{
		listRefs: map[string][]huawei.CloudCertRef{
			"cdn": {{Cloud: "huawei", Product: "cdn", ResourceID: "www.example.com", ReferencedCloudCertID: "ecam-ab12-1765432100-0a1b", AccountKey: "acc-main"}},
		},
	}
	d, _ := newTestHuaweiDeployer(fake, nil)

	refs, err := d.ListReferences(ctx, testHuaweiCreds(), "cdn")
	assert.NoError(t, err)
	assert.Equal(t, fpPlaceholder("huawei", "acc-main", "ecam-ab12-1765432100-0a1b"), refs[0].CertFingerprint)
	assert.Equal(t, []string{"ecam-ab12-1765432100-0a1b"}, fake.getSnapshot(), "无映射仍尝试 GetCert")
}

// fpPlaceholder 确定性占位指纹期望值（与 3.5 service.resolveUncached 同公式）。
func fpPlaceholder(cloud, accountKey, certID string) string {
	sum := sha256.Sum256([]byte("certscan-unresolved:" + cloud + "|" + accountKey + "|" + certID))
	return hex.EncodeToString(sum[:])
}

// ListReferences 限流：退避后整体重试（整页重取）。
func TestHuaweiDeployerListReferencesRateLimited(t *testing.T) {
	ctx := context.Background()
	fake := &fakeHuaweiCertAPI{
		listErr:  fmt.Errorf("list throttled: %w", cloudx.ErrCloudRateLimited),
		listRefs: map[string][]huawei.CloudCertRef{},
	}
	d, _ := newTestHuaweiDeployer(fake, nil)

	_, err := d.ListReferences(ctx, testHuaweiCreds(), "cdn")
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited)
	assert.Len(t, fake.listCalls, 5)
}

// ---------------------------------------------------------------------
// AC-3 / AC-4：GetCert / CleanupOrphan
// ---------------------------------------------------------------------

// GetCert 字段转换（Exists/NotAfter/Fingerprint）+ 云侧已删除=Exists=false 非错误。
func TestHuaweiDeployerGetCert(t *testing.T) {
	ctx := context.Background()
	notAfter := time.Now().Add(30 * 24 * time.Hour)
	fp64 := strings.Repeat("d", 64)
	fake := &fakeHuaweiCertAPI{
		getInfo: map[string]huawei.CloudCertInfo{
			"scm-9001": {Exists: true, Fingerprint: fp64, NotAfter: notAfter},
		},
	}
	d, _ := newTestHuaweiDeployer(fake, nil)

	info, err := d.GetCert(ctx, testHuaweiCreds(), "scm-9001")
	assert.NoError(t, err)
	assert.True(t, info.Exists, "回滚目标有效性校验：在库存在性")
	assert.Equal(t, fp64, info.Fingerprint, "SHA-256 对齐口径指纹（回滚指纹等值比对可用）")
	assert.WithinDuration(t, notAfter, info.NotAfter, time.Second)

	info, err = d.GetCert(ctx, testHuaweiCreds(), "ghost-cert")
	assert.NoError(t, err)
	assert.False(t, info.Exists, "云侧已删除=Exists=false 非错误")
}

// CleanupOrphan 透传 + 限流退避恢复。
func TestHuaweiDeployerCleanupOrphanRateLimited(t *testing.T) {
	ctx := context.Background()
	fake := &fakeHuaweiCertAPI{cleanupErr: fmt.Errorf("scm delete throttled: %w", cloudx.ErrCloudRateLimited)}
	d, rec := newTestHuaweiDeployer(fake, nil)

	err := d.CleanupOrphan(ctx, testHuaweiCreds(), "scm-9001")
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited, "补偿清理同样有界重试，耗尽透传哨兵")
	assert.Len(t, fake.cleanupsSnapshot(), 5)
	assert.Len(t, rec.snapshot(), 4)
}

// ---------------------------------------------------------------------
// AC-2 / AC-4：经 5.3 CloudAPIChannel 端到端（HuaweiDeployer 注入实例）
// ---------------------------------------------------------------------

// 两段式成功端到端：DeployResult 三字段 + 映射 active。
func TestHuaweiDeployerChannelDeployTwoStageSuccess(t *testing.T) {
	fake := &fakeHuaweiCertAPI{uploadIDs: []string{"scm-9002"}}
	mat := &fakeMaterial{certPEM: testCertPEM, keyPEM: []byte(testKeyPEM), keyVersion: 1}
	old := &fakeOldRefs{
		found: true,
		ref: domain.CertReference{
			Cloud: domain.CloudHuawei, Product: domain.ProductCDN,
			ResourceID: "www.example.com", ReferencedCloudCertID: "ecam-old-name", AccountKey: "acc-main",
		},
	}
	dep, _ := newTestHuaweiDeployer(fake, nil)
	mappings := certtest.NewFakeCloudCertMappingRepo()
	ch := NewCloudAPIChannel(mappings, mat, old)
	assert.NoError(t, ch.RegisterDeployer("huawei", dep, "cdn", "waf", "alb", "nlb"))

	res, err := ch.Deploy(context.Background(), testHuaweiCreds(), testHuaweiTarget(), testFingerprint)
	assert.NoError(t, err)

	assert.Equal(t, "scm-9002", res.NewCloudCertID)
	assert.Equal(t, "ecam-old-name", res.OldCloudCertID, "执行前从引用快照读取（回滚依据）")
	assert.True(t, res.OrphanCandidate, "旧云证书被替换 → 孤儿候选")

	uploads := fake.uploadsSnapshot()
	binds := fake.bindsSnapshot()
	assert.Len(t, uploads, 1)
	assert.Len(t, binds, 1)
	assert.Equal(t, "cdn:www.example.com:scm-9002", binds[0], "绑定用第一段产物（SCM ID）")
	assert.Regexp(t, uploadNamePattern, strings.SplitN(uploads[0], ":", 2)[1], "上传名唯一规则")
	assert.Empty(t, fake.cleanupsSnapshot(), "成功路径不做补偿清理")

	got, err := mappings.FindByCloudCertID(t.Context(), "huawei", "acc-main", "scm-9002")
	assert.NoError(t, err)
	assert.Equal(t, domain.MappingStatusActive, got.Status)

	// 凭证转换：AccountKey→Name、AK/SK 明文仅内存透传。
	fake.mu.Lock()
	acct := fake.lastAcct
	fake.mu.Unlock()
	assert.Equal(t, "acc-main", acct.Name)
	assert.Equal(t, sharedomain.CloudProviderHuawei, acct.Provider)
	assert.Equal(t, "HW-test-ak", acct.AccessKeyID)
}

// 第二段绑定失败端到端：CleanupOrphan 补偿清理 + 映射 active→orphan + OrphanCandidate=true。
func TestHuaweiDeployerChannelBindFailureCompensates(t *testing.T) {
	fake := &fakeHuaweiCertAPI{uploadIDs: []string{"scm-9003"}, bindErr: errors.New("listener not found")}
	mat := &fakeMaterial{certPEM: testCertPEM, keyPEM: []byte(testKeyPEM), keyVersion: 1}
	dep, _ := newTestHuaweiDeployer(fake, nil)
	mappings := certtest.NewFakeCloudCertMappingRepo()
	ch := NewCloudAPIChannel(mappings, mat, &fakeOldRefs{})
	assert.NoError(t, ch.RegisterDeployer("huawei", dep, "cdn", "waf", "alb", "nlb"))

	res, err := ch.Deploy(context.Background(), testHuaweiCreds(), testHuaweiTarget(), testFingerprint)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "listener not found")

	assert.True(t, res.OrphanCandidate)
	assert.Equal(t, "scm-9003", res.NewCloudCertID)

	// 补偿清理：未绑定云侧证书经 CleanupOrphan 删除（幂等重放安全）。
	assert.Equal(t, []string{"scm-9003"}, fake.cleanupsSnapshot())

	got, err := mappings.FindByCloudCertID(t.Context(), "huawei", "acc-main", "scm-9003")
	assert.NoError(t, err)
	assert.Equal(t, domain.MappingStatusOrphan, got.Status)
}
