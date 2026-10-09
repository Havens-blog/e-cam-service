// aws_deployer_test.go AWS CloudDeployer 单元测试：fake ACM 适配覆盖五方法 ×
// 三产品（含上传 CDN 口径、限流退避有界、ListReferences 指纹三级解析、经
// CloudAPIChannel 端到端两段式与绑定失败补偿）（cert-multicloud-deployers 任务 2）。
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
	"github.com/Havens-blog/e-cloudx-sdk"
	"github.com/Havens-blog/e-cloudx-sdk/aws"
	sharedomain "github.com/Havens-blog/e-cloudx-sdk/domain"
)

// awsTestARN 构造指定地域的 ACM 证书 ARN（测试用上传产物形态）
func awsTestARN(region string) string {
	return "arn:aws:acm:" + region + ":123456789012:certificate/c1"
}

// ---------------------------------------------------------------------
// 测试替身：mock AWS 完整证书适配（awsCertAPI 窄接口）
// ---------------------------------------------------------------------

// fakeAwsCertAPI mock aws.CertAdapter：记录调用序列与上传名，支持按调用次数
// 注入错误（限流/一般失败）与逐次返回 ID。
type fakeAwsCertAPI struct {
	mu          sync.Mutex
	uploadCalls []string // "product:name" 逐次
	uploadARNs  []string // 逐次返回 ARN；耗尽沿用末值（缺省 us-east-1 ARN）
	uploadErrFn func(call int) error
	lastAcct    *sharedomain.CloudAccount
	binds       []string // "product:resource:cert"
	bindErrFn   func(call int) error
	bindErr     error
	listCalls   []string
	listRefs    map[string][]aws.CloudCertRef
	listErr     error
	getCalls    []string
	getInfo     map[string]aws.CloudCertInfo
	getErr      error
	getErrs     map[string]error // 按证书 ID 注入错误（未命中回退 getErr/空 info）
	cleanups    []string
	cleanupErr  error
}

func (f *fakeAwsCertAPI) UploadCert(_ context.Context, acct *sharedomain.CloudAccount, product, name, _, _ string) (string, error) {
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
	arn := awsTestARN("us-east-1")
	if len(f.uploadARNs) >= n && f.uploadARNs[n-1] != "" {
		arn = f.uploadARNs[n-1]
	} else if len(f.uploadARNs) > 0 {
		arn = f.uploadARNs[len(f.uploadARNs)-1]
	}
	return arn, nil
}

func (f *fakeAwsCertAPI) BindResource(_ context.Context, _ *sharedomain.CloudAccount, product, resourceID, cloudCertID string) error {
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

func (f *fakeAwsCertAPI) ListReferences(_ context.Context, _ *sharedomain.CloudAccount, product string) ([]aws.CloudCertRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls = append(f.listCalls, product)
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listRefs[product], nil
}

func (f *fakeAwsCertAPI) GetCert(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) (aws.CloudCertInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls = append(f.getCalls, cloudCertID)
	if err, ok := f.getErrs[cloudCertID]; ok {
		return aws.CloudCertInfo{}, err
	}
	if f.getErr != nil {
		return aws.CloudCertInfo{}, f.getErr
	}
	if info, ok := f.getInfo[cloudCertID]; ok {
		return info, nil
	}
	return aws.CloudCertInfo{}, nil
}

func (f *fakeAwsCertAPI) CleanupOrphan(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleanups = append(f.cleanups, cloudCertID)
	return f.cleanupErr
}

func (f *fakeAwsCertAPI) uploadsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.uploadCalls...)
}

func (f *fakeAwsCertAPI) bindsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.binds...)
}

func (f *fakeAwsCertAPI) getSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.getCalls...)
}

func (f *fakeAwsCertAPI) cleanupsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cleanups...)
}

// newTestAwsDeployer 装配被测部署器：fake 适配 + 确定性时间/随机后缀
// （名称唯一性可精确断言）+ 即时睡眠记录器。
func newTestAwsDeployer(fake *fakeAwsCertAPI, mappings domain.CloudCertMappingRepository) (*AwsDeployer, *sleepRecorder) {
	d := NewAwsDeployer(fake, mappings)
	rec := &sleepRecorder{}
	d.sleep = rec.sleep
	d.now = func() time.Time { return time.Unix(1765432100, 0) }
	counter := 0
	d.randHex = func(int) string { counter++; return fmt.Sprintf("%04x", counter) }
	return d, rec
}

func testAwsCreds() Credential {
	return Credential{
		Kind: CredentialKindCloudAK, Cloud: "aws", AccountKey: "acc-main",
		AccessKey: "AWS-test-ak", Secret: []byte("test-sk-plaintext"), KeyVersion: 1,
	}
}

func testAwsTarget() DeployTarget {
	return DeployTarget{
		Channel: "cloud_api", Cloud: "aws", Product: "cdn",
		AccountKey: "acc-main", ResourceID: "E1ABCDEF",
	}
}

// ---------------------------------------------------------------------
// 编译期断言：完整 CertAdapter 满足窄接口（部署器生产装配形态）
// ---------------------------------------------------------------------

var _ awsCertAPI = (*aws.CertAdapter)(nil)

// ---------------------------------------------------------------------
// AC-2：三产品路由 + 监听证书 ID 归一化接缝
// ---------------------------------------------------------------------

// 三产品按 DeployTarget.product 路由至适配方法（AC-2）；监听证书 ID 归一化：
// AWS 引用与上传产物均为自包含 ARN 形态，归一化为幂等透传（仅去空白）。
func TestAwsDeployerBindRoutesThreeProducts(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAwsCertAPI{}
	d, _ := newTestAwsDeployer(fake, nil)

	certArn := awsTestARN("us-east-1")
	targets := map[string]string{
		"cdn": "E1ABCDEF", // CloudFront 分发 ID
		"alb": "arn:aws:elasticloadbalancing:us-east-1:123456789012:listener/app/my-alb/abc/l1",
		"nlb": "arn:aws:elasticloadbalancing:us-east-1:123456789012:listener/net/my-nlb/xyz/l9",
	}
	for _, product := range []string{"cdn", "alb", "nlb"} {
		assert.NoError(t, d.BindResource(ctx, testAwsCreds(), product, targets[product], certArn), product)
	}

	binds := fake.bindsSnapshot()
	require.Len(t, binds, 3, "三产品各一次绑定")
	assert.Equal(t, "cdn:E1ABCDEF:"+certArn, binds[0], "CloudFront 分发级绑定，ARN 直传")
	assert.Equal(t, "alb:"+targets["alb"]+":"+certArn, binds[1], "ALB 监听级绑定（AddListenerCertificates 形态归适配层）")
	assert.Equal(t, "nlb:"+targets["nlb"]+":"+certArn, binds[2], "NLB 监听级绑定")
}

// 监听证书 ID 形态归一化幂等：ARN 与任意既有形态引用均原样透传（发现引用/
// 回滚旧 ID 不被改写），仅去首尾空白。
func TestAwsDeployerBindNormalizesListenerCertID(t *testing.T) {
	certArn := awsTestARN("us-east-1")
	assert.Equal(t, certArn, normalizeAwsListenerCertID("alb", certArn))
	assert.Equal(t, certArn, normalizeAwsListenerCertID("nlb", certArn))
	assert.Equal(t, certArn, normalizeAwsListenerCertID("cdn", certArn))
	assert.Equal(t, certArn, normalizeAwsListenerCertID("alb", " "+certArn+" "))
}

// 未支持产品（waf/clb 等）：适配哨兵错误透传。
func TestAwsDeployerBindUnsupportedProduct(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAwsCertAPI{bindErr: aws.ErrCertProductNotSupported}
	d, _ := newTestAwsDeployer(fake, nil)

	err := d.BindResource(ctx, testAwsCreds(), "waf", "E1ABCDEF", awsTestARN("us-east-1"))
	assert.ErrorIs(t, err, aws.ErrCertProductNotSupported)
}

// 凭证归属云不符/凭证非法：显式拒绝且不触达适配层。
func TestAwsDeployerRejectsForeignCredential(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAwsCertAPI{}
	d, _ := newTestAwsDeployer(fake, nil)

	foreign := testAwsCreds()
	foreign.Cloud = "aliyun"
	_, err := d.UploadCert(ctx, foreign, testCertPEM, []byte(testKeyPEM))
	assert.ErrorContains(t, err, "not aws")

	invalid := testAwsCreds()
	invalid.AccessKey = ""
	_, err = d.UploadCert(ctx, invalid, testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, ErrInvalidCredential)

	assert.Empty(t, fake.uploadsSnapshot(), "校验失败不产生云侧调用")
}

// ---------------------------------------------------------------------
// AC-1：上传名唯一生成（ecam-{指纹前8}-{unix秒}-{随机}）+ 统一 CDN 口径
// ---------------------------------------------------------------------

// 名称规则与逐次唯一（C7：重试不复用可能已成功的名称）；第一段统一以 CDN
// 口径上传（CloudFront us-east-1 硬约束使上传产物恒为 CloudFront 可绑形态）。
func TestAwsUploadNameGeneration(t *testing.T) {
	bundle := certtest.NewBundle(t, "www.example.com", nil, nil)
	fake := &fakeAwsCertAPI{}
	d, _ := newTestAwsDeployer(fake, nil)

	ctx := context.Background()
	arn, err := d.UploadCert(ctx, testAwsCreds(), string(bundle.CertPEM), bundle.KeyPEM)
	require.NoError(t, err)
	assert.Equal(t, awsTestARN("us-east-1"), arn, "上传产物为 ACM ARN 形态")

	uploads := fake.uploadsSnapshot()
	require.Len(t, uploads, 1)
	product, name, ok := strings.Cut(uploads[0], ":")
	require.True(t, ok)
	assert.Equal(t, "cdn", product, "第一段统一以 CDN 口径上传（us-east-1 硬约束口径）")
	assert.Regexp(t, uploadNamePattern, name)
	assert.LessOrEqual(t, len(name), 63, "上传名沿用 63 字符上限（ACM 经 Name 标签承载）")
	assert.Contains(t, name, bundle.Fingerprint[:8], "指纹前 8 位来源=证书叶 DER SHA256")
	assert.Contains(t, name, "1765432100", "unix 秒时间戳分量")

	// 同材料重复生成：随机后缀保证唯一（C7）。
	names := []string{name}
	for i := 0; i < 5; i++ {
		_, err := d.UploadCert(ctx, testAwsCreds(), string(bundle.CertPEM), bundle.KeyPEM)
		require.NoError(t, err)
		names = append(names, strings.SplitN(fake.uploadsSnapshot()[len(names)], ":", 2)[1])
	}
	assert.Len(t, uniqueStrings(names), len(names), "逐次生成名互不相同")
}

// 超 63 字符防御性截断（随机后缀异常超长场景）。
func TestAwsUploadNameTruncation(t *testing.T) {
	d, _ := newTestAwsDeployer(&fakeAwsCertAPI{}, nil)
	d.randHex = func(int) string { return strings.Repeat("a", 80) }

	name := d.generateUploadName(testCertPEM)
	assert.Len(t, name, 63)
	assert.True(t, strings.HasPrefix(name, "ecam-"), "截断保留前缀")
}

// 材料缺失：显式拒绝。
func TestAwsDeployerUploadRejectsEmptyMaterial(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAwsCertAPI{}
	d, _ := newTestAwsDeployer(fake, nil)

	_, err := d.UploadCert(ctx, testAwsCreds(), "", []byte(testKeyPEM))
	assert.ErrorContains(t, err, "requires cert PEM and key PEM")
	_, err = d.UploadCert(ctx, testAwsCreds(), testCertPEM, nil)
	assert.ErrorContains(t, err, "requires cert PEM and key PEM")
	assert.Empty(t, fake.uploadsSnapshot())
}

// ---------------------------------------------------------------------
// AC-6：限流退避（固定序列，有界上限；AWS SDK 自带重试与本层退避分层）
// ---------------------------------------------------------------------

// 限流后退避恢复：按固定序列睡眠，逐次换名重试（C7），最终成功。
func TestAwsDeployerRateLimitBackoffRecovers(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAwsCertAPI{uploadErrFn: func(call int) error {
		if call <= 2 {
			return fmt.Errorf("aws acm api throttled: %w", cloudx.ErrCloudRateLimited)
		}
		return nil
	}}
	d, rec := newTestAwsDeployer(fake, nil)

	arn, err := d.UploadCert(ctx, testAwsCreds(), testCertPEM, []byte(testKeyPEM))
	require.NoError(t, err)
	assert.Equal(t, awsTestARN("us-east-1"), arn)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, rec.snapshot(),
		"固定退避序列 1s/2s（默认策略前两档）")

	uploads := fake.uploadsSnapshot()
	assert.Len(t, uploads, 3)
	assert.Len(t, uniqueStrings(uploads), 3, "重试逐次换名（不得复用可能已成功的名称）")
}

// 限流持续：次数上限耗尽即失败，绝不无限重试（Hard Rule）；哨兵语义保留。
func TestAwsDeployerRateLimitExhaustsByAttempts(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAwsCertAPI{uploadErrFn: func(int) error {
		return fmt.Errorf("aws acm api throttled: %w", cloudx.ErrCloudRateLimited)
	}}
	d, rec := newTestAwsDeployer(fake, nil)

	_, err := d.UploadCert(ctx, testAwsCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited, "耗尽后哨兵仍可判定（5.7 映射 rate_limited/failed）")
	assert.ErrorContains(t, err, "retries exhausted after 5 attempts")
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second},
		rec.snapshot(), "默认固定序列全量消费后停止")
	assert.Len(t, fake.uploadsSnapshot(), 5, "默认 MaxAttempts=5")
}

// 退避总时长上限：下一档退避将超总时长即停止（Hard Rule：上限次数+总时长双闸）。
func TestAwsDeployerRateLimitExhaustsByTotalWait(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAwsCertAPI{uploadErrFn: func(int) error {
		return fmt.Errorf("throttled: %w", cloudx.ErrCloudRateLimited)
	}}
	d, rec := newTestAwsDeployer(fake, nil)
	d.retry = RetryPolicy{
		MaxAttempts:  10,
		Backoffs:     []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second},
		MaxTotalWait: 3 * time.Second,
	}

	_, err := d.UploadCert(ctx, testAwsCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited)
	assert.ErrorContains(t, err, "total backoff cap")
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, rec.snapshot(),
		"累计 3s 后，下一档 4s 超总时长上限即止（尝试 3 次）")
	assert.Len(t, fake.uploadsSnapshot(), 3)
}

// 退避睡眠被取消：返回 ctx 错误，不再继续尝试。
func TestAwsDeployerBackoffCanceled(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAwsCertAPI{uploadErrFn: func(int) error {
		return fmt.Errorf("throttled: %w", cloudx.ErrCloudRateLimited)
	}}
	d, rec := newTestAwsDeployer(fake, nil)
	rec.err = context.DeadlineExceeded

	_, err := d.UploadCert(ctx, testAwsCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Len(t, fake.uploadsSnapshot(), 1, "首试失败进入退避，退避中断即止（重试未发生）")
}

// 非限流错误：立即返回，不退避不重试。
func TestAwsDeployerNonRetryableNoRetry(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAwsCertAPI{uploadErrFn: func(int) error {
		return errors.New("certificate and key do not match")
	}}
	d, rec := newTestAwsDeployer(fake, nil)

	_, err := d.UploadCert(ctx, testAwsCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorContains(t, err, "certificate and key do not match")
	assert.Empty(t, rec.snapshot(), "一般失败不退避")
	assert.Len(t, fake.uploadsSnapshot(), 1)
}

// 退避策略归一化：零值/非法回退缺省保守值。
func TestAwsRetryPolicyOptionNormalization(t *testing.T) {
	def := DefaultRetryPolicy()
	assert.Equal(t, def, RetryPolicy{}.normalized())

	custom := RetryPolicy{
		MaxAttempts:  2,
		Backoffs:     []time.Duration{time.Second},
		MaxTotalWait: time.Second,
	}
	d := NewAwsDeployer(&fakeAwsCertAPI{}, nil, WithAwsRetryPolicy(custom))
	assert.Equal(t, custom, d.retry)
	assert.Equal(t, def, NewAwsDeployer(&fakeAwsCertAPI{}, nil, WithAwsRetryPolicy(RetryPolicy{})).retry,
		"零值配置回退缺省保守值")
}

// ---------------------------------------------------------------------
// AC-5：ListReferences（复用发现适配引用形态 + 指纹三级解析）
// ---------------------------------------------------------------------

// 指纹解析三级口径：映射反查 → GetCert（SHA256 对齐）→ 确定性占位指纹；
// 同云证书多引用只查一次 GetCert。AWS 特有：IAM 托管证书 ID（非 ARN 形态）
// 引用经 GetCert 返回结构化降级标记 → 占位指纹（不中断发现）。
func TestAwsDeployerListReferencesFingerprints(t *testing.T) {
	ctx := context.Background()
	fpMapped := strings.Repeat("a", 64)
	fpFromCloud := strings.Repeat("b", 64)

	mappings := certtest.NewFakeCloudCertMappingRepo()
	require.NoError(t, mappings.Upsert(ctx, &domain.CloudCertMapping{
		CertFingerprint: fpMapped, Cloud: "aws", AccountKey: "acc-main",
		CloudCertID: awsTestARN("us-east-1"), Status: domain.MappingStatusActive,
	}))

	fake := &fakeAwsCertAPI{
		listRefs: map[string][]aws.CloudCertRef{
			"cdn": {
				{Cloud: "aws", Product: "cdn", ResourceID: "E1A", ReferencedCloudCertID: awsTestARN("us-east-1"), AccountKey: "acc-main"},
				{Cloud: "aws", Product: "cdn", ResourceID: "E2B", ReferencedCloudCertID: awsTestARN("us-west-2"), AccountKey: "acc-main"},
				{Cloud: "aws", Product: "cdn", ResourceID: "E3C", ReferencedCloudCertID: "AS1IAMLEGACY", AccountKey: "acc-main"},
				{Cloud: "aws", Product: "cdn", ResourceID: "E4D", ReferencedCloudCertID: awsTestARN("eu-west-1"), AccountKey: "acc-main"},
			},
		},
		getInfo: map[string]aws.CloudCertInfo{
			awsTestARN("us-west-2"): {Exists: true, Fingerprint: fpFromCloud, NotAfter: time.Now().Add(90 * 24 * time.Hour)},
		},
		// IAM 托管形态（非 ARN）→ GetCert 返回结构化降级标记（镜像适配层口径）
		getErrs: map[string]error{
			"AS1IAMLEGACY": fmt.Errorf("%w: aws IAM-hosted certificate id has no acm pem export", aws.ErrCertPEMUnsupported),
		},
	}
	d, _ := newTestAwsDeployer(fake, mappings)

	refs, err := d.ListReferences(ctx, testAwsCreds(), "cdn")
	require.NoError(t, err)
	require.Len(t, refs, 4)

	byResource := make(map[string]domain.CertReference, len(refs))
	for _, r := range refs {
		byResource[r.ResourceID] = r
	}
	assert.Equal(t, fpMapped, byResource["E1A"].CertFingerprint, "映射反查命中")
	assert.Equal(t, fpFromCloud, byResource["E2B"].CertFingerprint, "GetCert SHA256 对齐口径")
	assert.Equal(t, fpPlaceholder("aws", "acc-main", "AS1IAMLEGACY"), byResource["E3C"].CertFingerprint,
		"IAM 形态 GetCert 降级 → 确定性占位指纹")
	assert.Equal(t, fpPlaceholder("aws", "acc-main", awsTestARN("eu-west-1")), byResource["E4D"].CertFingerprint,
		"GetCert 未命中 → 确定性占位指纹")

	assert.Equal(t, []string{awsTestARN("us-west-2"), "AS1IAMLEGACY", awsTestARN("eu-west-1")},
		fake.getSnapshot(), "映射命中不查 GetCert；同证书多引用去重仅查一次")

	for _, r := range refs {
		assert.Equal(t, domain.CloudAWS, r.Cloud)
		assert.Equal(t, domain.ProductCDN, r.Product)
		assert.Equal(t, "acc-main", r.AccountKey)
	}
}

// mappings 缺省（nil）：跳过映射反查，直接 GetCert fallback → 占位指纹。
func TestAwsDeployerListReferencesWithoutMappings(t *testing.T) {
	ctx := context.Background()
	certArn := awsTestARN("us-east-1")
	fake := &fakeAwsCertAPI{
		listRefs: map[string][]aws.CloudCertRef{
			"alb": {{Cloud: "aws", Product: "alb", ResourceID: "arn:aws:elasticloadbalancing:us-east-1:123456789012:listener/app/my-alb/abc/l1", ReferencedCloudCertID: certArn, AccountKey: "acc-main"}},
		},
	}
	d, _ := newTestAwsDeployer(fake, nil)

	refs, err := d.ListReferences(ctx, testAwsCreds(), "alb")
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, fpPlaceholder("aws", "acc-main", certArn), refs[0].CertFingerprint)
	assert.Equal(t, []string{certArn}, fake.getSnapshot(), "无映射仍尝试 GetCert")
}

// ListReferences 限流：退避后整体重试（整页重取）。
func TestAwsDeployerListReferencesRateLimited(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAwsCertAPI{
		listErr:  fmt.Errorf("list throttled: %w", cloudx.ErrCloudRateLimited),
		listRefs: map[string][]aws.CloudCertRef{},
	}
	d, _ := newTestAwsDeployer(fake, nil)

	_, err := d.ListReferences(ctx, testAwsCreds(), "cdn")
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited)
	assert.Len(t, fake.listCalls, 5)
}

// ---------------------------------------------------------------------
// AC-3 / AC-4：GetCert / CleanupOrphan
// ---------------------------------------------------------------------

// GetCert 字段转换（Exists/NotAfter/Fingerprint）+ 云侧已删除=Exists=false 非错误；
// IAM 托管形态降级标记显式透传（回滚判定 fail-safe 阻断）。
func TestAwsDeployerGetCert(t *testing.T) {
	ctx := context.Background()
	notAfter := time.Now().Add(30 * 24 * time.Hour)
	fp64 := strings.Repeat("d", 64)
	fake := &fakeAwsCertAPI{
		getInfo: map[string]aws.CloudCertInfo{
			awsTestARN("us-east-1"): {Exists: true, Fingerprint: fp64, NotAfter: notAfter},
		},
	}
	d, _ := newTestAwsDeployer(fake, nil)

	info, err := d.GetCert(ctx, testAwsCreds(), awsTestARN("us-east-1"))
	require.NoError(t, err)
	assert.True(t, info.Exists, "回滚目标有效性校验：在库存在性")
	assert.Equal(t, fp64, info.Fingerprint, "SHA256 对齐口径指纹（回滚指纹等值比对可用）")
	assert.WithinDuration(t, notAfter, info.NotAfter, time.Second)

	info, err = d.GetCert(ctx, testAwsCreds(), awsTestARN("eu-west-1"))
	require.NoError(t, err)
	assert.False(t, info.Exists, "云侧已删除=Exists=false 非错误")
}

// CleanupOrphan 透传 + 限流退避恢复。
func TestAwsDeployerCleanupOrphanRateLimited(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAwsCertAPI{cleanupErr: fmt.Errorf("acm delete throttled: %w", cloudx.ErrCloudRateLimited)}
	d, rec := newTestAwsDeployer(fake, nil)

	err := d.CleanupOrphan(ctx, testAwsCreds(), awsTestARN("us-east-1"))
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited, "补偿清理同样有界重试，耗尽透传哨兵")
	assert.Len(t, fake.cleanupsSnapshot(), 5)
	assert.Len(t, rec.snapshot(), 4)
}

// ---------------------------------------------------------------------
// AC-2 / AC-4：经 5.3 CloudAPIChannel 端到端（AwsDeployer 注入实例）
// ---------------------------------------------------------------------

// 两段式成功端到端：DeployResult 三字段 + 映射 active（ACM ARN 写入映射）。
func TestAwsDeployerChannelDeployTwoStageSuccess(t *testing.T) {
	fake := &fakeAwsCertAPI{uploadARNs: []string{awsTestARN("us-east-1")}}
	mat := &fakeMaterial{certPEM: testCertPEM, keyPEM: []byte(testKeyPEM), keyVersion: 1}
	old := &fakeOldRefs{
		found: true,
		ref: domain.CertReference{
			Cloud: domain.CloudAWS, Product: domain.ProductCDN,
			ResourceID: "E1ABCDEF", ReferencedCloudCertID: awsTestARN("us-east-1"), AccountKey: "acc-main",
		},
	}
	dep, _ := newTestAwsDeployer(fake, nil)
	mappings := certtest.NewFakeCloudCertMappingRepo()
	ch := NewCloudAPIChannel(mappings, mat, old)
	require.NoError(t, ch.RegisterDeployer("aws", dep, "cdn", "alb", "nlb"))

	res, err := ch.Deploy(context.Background(), testAwsCreds(), testAwsTarget(), testFingerprint)
	require.NoError(t, err)

	assert.Equal(t, awsTestARN("us-east-1"), res.NewCloudCertID)
	assert.Equal(t, awsTestARN("us-east-1"), res.OldCloudCertID, "执行前从引用快照读取（回滚依据）")
	assert.True(t, res.OrphanCandidate, "旧云证书被替换 → 孤儿候选")

	uploads := fake.uploadsSnapshot()
	binds := fake.bindsSnapshot()
	require.Len(t, uploads, 1)
	require.Len(t, binds, 1)
	assert.Equal(t, "cdn:E1ABCDEF:"+awsTestARN("us-east-1"), binds[0], "绑定用第一段产物（ACM ARN）")
	assert.Regexp(t, uploadNamePattern, strings.SplitN(uploads[0], ":", 2)[1], "上传名唯一规则")
	assert.Empty(t, fake.cleanupsSnapshot(), "成功路径不做补偿清理")

	got, err := mappings.FindByCloudCertID(t.Context(), "aws", "acc-main", awsTestARN("us-east-1"))
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusActive, got.Status)

	// 凭证转换：AccountKey→Name、AK/SK 明文仅内存透传。
	fake.mu.Lock()
	acct := fake.lastAcct
	fake.mu.Unlock()
	assert.Equal(t, "acc-main", acct.Name)
	assert.Equal(t, sharedomain.CloudProviderAWS, acct.Provider)
	assert.Equal(t, "AWS-test-ak", acct.AccessKeyID)
}

// 第二段绑定失败端到端：CleanupOrphan 补偿清理 + 映射 active→orphan + OrphanCandidate=true。
func TestAwsDeployerChannelBindFailureCompensates(t *testing.T) {
	fake := &fakeAwsCertAPI{
		uploadARNs: []string{awsTestARN("us-east-1")},
		bindErr:    errors.New("listener not found"),
	}
	mat := &fakeMaterial{certPEM: testCertPEM, keyPEM: []byte(testKeyPEM), keyVersion: 1}
	dep, _ := newTestAwsDeployer(fake, nil)
	mappings := certtest.NewFakeCloudCertMappingRepo()
	ch := NewCloudAPIChannel(mappings, mat, &fakeOldRefs{})
	require.NoError(t, ch.RegisterDeployer("aws", dep, "cdn", "alb", "nlb"))

	res, err := ch.Deploy(context.Background(), testAwsCreds(), testAwsTarget(), testFingerprint)
	require.Error(t, err)
	assert.ErrorContains(t, err, "listener not found")

	assert.True(t, res.OrphanCandidate)
	assert.Equal(t, awsTestARN("us-east-1"), res.NewCloudCertID)

	// 补偿清理：未绑定云侧证书经 CleanupOrphan 删除（幂等重放安全）。
	assert.Equal(t, []string{awsTestARN("us-east-1")}, fake.cleanupsSnapshot())

	got, err := mappings.FindByCloudCertID(t.Context(), "aws", "acc-main", awsTestARN("us-east-1"))
	require.NoError(t, err)
	assert.Equal(t, domain.MappingStatusOrphan, got.Status)
}
