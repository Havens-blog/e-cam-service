package deployer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	sdkerrors "github.com/aliyun/alibaba-cloud-sdk-go/sdk/errors"
	"github.com/stretchr/testify/assert"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/aliyun"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// ---------------------------------------------------------------------
// 测试替身：mock 3.1 阿里云证书适配（aliyunCertAPI 窄接口）
// ---------------------------------------------------------------------

// fakeAliyunCertAPI mock 3.1 CertAdapter：记录调用序列与上传名，支持按调用
// 次数注入错误（限流/命名冲突/一般失败）与逐次返回 ID。
type fakeAliyunCertAPI struct {
	mu          sync.Mutex
	uploadCalls []string // "product:name" 逐次
	uploadIDs   []string // 逐次返回 ID；耗尽沿用末值（缺省 cas-8001）
	uploadErrFn func(call int) error
	lastAcct    *sharedomain.CloudAccount
	binds       []string // "product:resource:cert"
	bindErrFn   func(call int) error
	bindErr     error
	listCalls   []string
	listRefs    map[string][]aliyun.CloudCertRef
	listErr     error
	getCalls    []string
	getInfo     map[string]aliyun.CloudCertInfo
	getErr      error
	cleanups    []string
	cleanupErr  error
}

func (f *fakeAliyunCertAPI) UploadCert(_ context.Context, acct *sharedomain.CloudAccount, product, name, _, _ string) (string, error) {
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
	id := "cas-8001"
	if len(f.uploadIDs) >= n && f.uploadIDs[n-1] != "" {
		id = f.uploadIDs[n-1]
	} else if len(f.uploadIDs) > 0 {
		id = f.uploadIDs[len(f.uploadIDs)-1]
	}
	return id, nil
}

func (f *fakeAliyunCertAPI) BindResource(_ context.Context, _ *sharedomain.CloudAccount, product, resourceID, cloudCertID string) error {
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

func (f *fakeAliyunCertAPI) ListReferences(_ context.Context, _ *sharedomain.CloudAccount, product string) ([]aliyun.CloudCertRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls = append(f.listCalls, product)
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listRefs[product], nil
}

func (f *fakeAliyunCertAPI) GetCert(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) (aliyun.CloudCertInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls = append(f.getCalls, cloudCertID)
	if f.getErr != nil {
		return aliyun.CloudCertInfo{}, f.getErr
	}
	if info, ok := f.getInfo[cloudCertID]; ok {
		return info, nil
	}
	return aliyun.CloudCertInfo{}, nil
}

func (f *fakeAliyunCertAPI) CleanupOrphan(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleanups = append(f.cleanups, cloudCertID)
	return f.cleanupErr
}

func (f *fakeAliyunCertAPI) uploadsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.uploadCalls...)
}

func (f *fakeAliyunCertAPI) bindsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.binds...)
}

func (f *fakeAliyunCertAPI) getSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.getCalls...)
}

func (f *fakeAliyunCertAPI) cleanupsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cleanups...)
}

// sleepRecorder 退避睡眠记录器（测试即时返回，记录序列验证固定退避）。
type sleepRecorder struct {
	mu     sync.Mutex
	delays []time.Duration
	err    error
}

func (s *sleepRecorder) sleep(_ context.Context, d time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delays = append(s.delays, d)
	return s.err
}

func (s *sleepRecorder) snapshot() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.delays...)
}

// newTestAliyunDeployer 装配被测部署器：fake 适配 + 确定性时间/随机后缀
// （名称唯一性可精确断言）+ 即时睡眠记录器。
func newTestAliyunDeployer(fake *fakeAliyunCertAPI, mappings domain.CloudCertMappingRepository) (*AliyunDeployer, *sleepRecorder) {
	d := NewAliyunDeployer(fake, mappings)
	rec := &sleepRecorder{}
	d.sleep = rec.sleep
	d.now = func() time.Time { return time.Unix(1765432100, 0) }
	counter := 0
	d.randHex = func(int) string { counter++; return fmt.Sprintf("%04x", counter) }
	return d, rec
}

// ---------------------------------------------------------------------
// AC-1：五产品路由 + CloudDeployer 端口实现
// ---------------------------------------------------------------------

// 五产品按 DeployTarget.product 路由至 3.1 适配方法（AC-1）。
func TestAliyunDeployerBindRoutesFiveProducts(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAliyunCertAPI{}
	d, _ := newTestAliyunDeployer(fake, nil)

	products := []string{"cdn", "dcdn", "waf", "alb", "nlb"}
	for _, product := range products {
		target := testCloudTarget()
		target.Product = product
		assert.NoError(t, d.BindResource(ctx, testCloudCreds(), product, target.ResourceID, "8001"))
	}

	binds := fake.bindsSnapshot()
	assert.Len(t, binds, 5, "五产品各一次绑定")
	assert.Equal(t, "cdn:www.example.com:8001", binds[0], "CDN 纯数字 ID 直传")
	assert.Equal(t, "dcdn:www.example.com:8001", binds[1])
	assert.Equal(t, "waf:www.example.com:8001", binds[2])
	assert.Equal(t, "alb:www.example.com:8001-cn-hangzhou", binds[3],
		"ALB 裸数字 ID 归一化为 {certId}-{region} 监听引用形态")
	assert.Equal(t, "nlb:www.example.com:8001-cn-hangzhou", binds[4])
}

// 已带地域后缀的 ID 幂等保持（回滚/发现引用回传场景）。
func TestAliyunDeployerBindKeepsSuffixedCertID(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAliyunCertAPI{}
	d, _ := newTestAliyunDeployer(fake, nil)

	assert.NoError(t, d.BindResource(ctx, testCloudCreds(), "alb", "lsn-1", "8001-ap-southeast-1"))
	assert.NoError(t, d.BindResource(ctx, testCloudCreds(), "cdn", "www.example.com", "8001-cn-hangzhou"))
	binds := fake.bindsSnapshot()
	assert.Equal(t, "alb:lsn-1:8001-ap-southeast-1", binds[0], "已带后缀不重复追加")
	assert.Equal(t, "cdn:www.example.com:8001-cn-hangzhou", binds[1], "CDN 透传不裁剪")
}

// 未支持产品（clb 等）：3.1 适配哨兵错误透传。
func TestAliyunDeployerBindUnsupportedProduct(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAliyunCertAPI{bindErr: aliyun.ErrCertProductNotSupported}
	d, _ := newTestAliyunDeployer(fake, nil)

	err := d.BindResource(ctx, testCloudCreds(), "clb", "lb-1", "8001")
	assert.ErrorIs(t, err, aliyun.ErrCertProductNotSupported)
}

// 凭证归属云不符/凭证非法：显式拒绝且不触达适配层。
func TestAliyunDeployerRejectsForeignCredential(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAliyunCertAPI{}
	d, _ := newTestAliyunDeployer(fake, nil)

	foreign := testCloudCreds()
	foreign.Cloud = "tencent"
	_, err := d.UploadCert(ctx, foreign, testCertPEM, []byte(testKeyPEM))
	assert.ErrorContains(t, err, "not aliyun")

	invalid := testCloudCreds()
	invalid.AccessKey = ""
	_, err = d.UploadCert(ctx, invalid, testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, ErrInvalidCredential)

	assert.Empty(t, fake.uploadsSnapshot(), "校验失败不产生云侧调用")
}

// ---------------------------------------------------------------------
// B2/C2/C7：CAS 唯一上传名
// ---------------------------------------------------------------------

var uploadNamePattern = regexp.MustCompile(`^ecam-[0-9a-f]{8}-\d+-[0-9a-f]{4,}$`)

// 名称规则：ecam-{指纹前8}-{unix秒}-{随机后缀}，≤63 字符，逐次唯一（B2/C2）；
// 真实证书束时指纹前缀 = 台账指纹前 8 位。
func TestAliyunUploadNameGeneration(t *testing.T) {
	bundle := certtest.NewBundle(t, "www.example.com", nil, nil)
	fake := &fakeAliyunCertAPI{}
	d, _ := newTestAliyunDeployer(fake, nil)

	ctx := context.Background()
	id, err := d.UploadCert(ctx, testCloudCreds(), string(bundle.CertPEM), bundle.KeyPEM)
	assert.NoError(t, err)
	assert.Equal(t, "cas-8001", id)

	uploads := fake.uploadsSnapshot()
	assert.Len(t, uploads, 1)
	product, name, ok := strings.Cut(uploads[0], ":")
	assert.True(t, ok)
	assert.Equal(t, "cdn", product, "第一段统一以 CDN 口径上传（纯数字 ID 形态）")
	assert.Regexp(t, uploadNamePattern, name)
	assert.LessOrEqual(t, len(name), 63, "CAS Name ≤63 字符（poc-notes L8）")
	assert.Contains(t, name, bundle.Fingerprint[:8], "指纹前 8 位来源=证书叶 DER SHA256")
	assert.Contains(t, name, "1765432100", "unix 秒时间戳分量")

	// 同材料重复生成：随机后缀保证唯一（C7：重试不复用名称）。
	names := []string{name}
	for i := 0; i < 5; i++ {
		_, err := d.UploadCert(ctx, testCloudCreds(), string(bundle.CertPEM), bundle.KeyPEM)
		assert.NoError(t, err)
		names = append(names, strings.SplitN(fake.uploadsSnapshot()[len(names)], ":", 2)[1])
	}
	assert.Len(t, uniqueStrings(names), len(names), "逐次生成名互不相同")
}

// 超 63 字符防御性截断（随机后缀异常超长场景）。
func TestAliyunUploadNameTruncation(t *testing.T) {
	d, _ := newTestAliyunDeployer(&fakeAliyunCertAPI{}, nil)
	d.randHex = func(int) string { return strings.Repeat("a", 80) }

	name := d.generateUploadName(testCertPEM)
	assert.Len(t, name, 63)
	assert.True(t, strings.HasPrefix(name, "ecam-"), "截断保留前缀")
}

// 材料缺失：显式拒绝。
func TestAliyunDeployerUploadRejectsEmptyMaterial(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAliyunCertAPI{}
	d, _ := newTestAliyunDeployer(fake, nil)

	_, err := d.UploadCert(ctx, testCloudCreds(), "", []byte(testKeyPEM))
	assert.ErrorContains(t, err, "requires cert PEM and key PEM")
	_, err = d.UploadCert(ctx, testCloudCreds(), testCertPEM, nil)
	assert.ErrorContains(t, err, "requires cert PEM and key PEM")
	assert.Empty(t, fake.uploadsSnapshot())
}

// ---------------------------------------------------------------------
// AC-3：限流退避（固定序列，有界上限）
// ---------------------------------------------------------------------

// 限流后退避恢复：按固定序列睡眠，逐次换名重试（C7），最终成功。
func TestAliyunDeployerRateLimitBackoffRecovers(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAliyunCertAPI{uploadErrFn: func(call int) error {
		if call <= 2 {
			return fmt.Errorf("aliyun cas api throttled: %w", cloudx.ErrCloudRateLimited)
		}
		return nil
	}}
	d, rec := newTestAliyunDeployer(fake, nil)

	id, err := d.UploadCert(ctx, testCloudCreds(), testCertPEM, []byte(testKeyPEM))
	assert.NoError(t, err)
	assert.Equal(t, "cas-8001", id)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, rec.snapshot(),
		"固定退避序列 1s/2s（默认策略前两档）")

	uploads := fake.uploadsSnapshot()
	assert.Len(t, uploads, 3)
	assert.Len(t, uniqueStrings(uploads), 3, "重试逐次换名（不得复用可能已成功的名称）")
}

// 限流持续：次数上限耗尽即失败，绝不无限重试（Hard Rule）；哨兵语义保留。
func TestAliyunDeployerRateLimitExhaustsByAttempts(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAliyunCertAPI{uploadErrFn: func(int) error {
		return fmt.Errorf("aliyun cas api throttled: %w", cloudx.ErrCloudRateLimited)
	}}
	d, rec := newTestAliyunDeployer(fake, nil)

	_, err := d.UploadCert(ctx, testCloudCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited, "耗尽后哨兵仍可判定（5.7 映射 rate_limited/failed）")
	assert.ErrorContains(t, err, "retries exhausted after 5 attempts")
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second},
		rec.snapshot(), "默认固定序列全量消费后停止")
	assert.Len(t, fake.uploadsSnapshot(), 5, "默认 MaxAttempts=5")
}

// 退避总时长上限：下一档退避将超总时长即停止（Hard Rule：上限次数+总时长双闸）。
func TestAliyunDeployerRateLimitExhaustsByTotalWait(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAliyunCertAPI{uploadErrFn: func(int) error {
		return fmt.Errorf("throttled: %w", cloudx.ErrCloudRateLimited)
	}}
	d, rec := newTestAliyunDeployer(fake, nil)
	d.retry = RetryPolicy{
		MaxAttempts:  10,
		Backoffs:     []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second},
		MaxTotalWait: 3 * time.Second,
	}

	_, err := d.UploadCert(ctx, testCloudCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited)
	assert.ErrorContains(t, err, "total backoff cap")
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, rec.snapshot(),
		"累计 3s 后，下一档 4s 超总时长上限即止（尝试 3 次）")
	assert.Len(t, fake.uploadsSnapshot(), 3)
}

// 退避睡眠被取消：返回 ctx 错误，不再继续尝试。
func TestAliyunDeployerBackoffCanceled(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAliyunCertAPI{uploadErrFn: func(int) error {
		return fmt.Errorf("throttled: %w", cloudx.ErrCloudRateLimited)
	}}
	d, rec := newTestAliyunDeployer(fake, nil)
	rec.err = context.DeadlineExceeded

	_, err := d.UploadCert(ctx, testCloudCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Len(t, fake.uploadsSnapshot(), 1, "首试失败进入退避，退避中断即止（重试未发生）")
}

// 非限流错误：立即返回，不退避不重试。
func TestAliyunDeployerNonRetryableNoRetry(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAliyunCertAPI{uploadErrFn: func(int) error {
		return errors.New("cert and key mismatch")
	}}
	d, rec := newTestAliyunDeployer(fake, nil)

	_, err := d.UploadCert(ctx, testCloudCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorContains(t, err, "cert and key mismatch")
	assert.Empty(t, rec.snapshot(), "一般失败不退避")
	assert.Len(t, fake.uploadsSnapshot(), 1)
}

// CAS 命名冲突（B2）：立即换名重试（不睡眠），同一重试预算内恢复。
func TestAliyunDeployerUploadNameConflictRetries(t *testing.T) {
	ctx := context.Background()
	conflict := sdkerrors.NewServerError(400,
		`{"Code":"CertNameExists","Message":"the certificate name already exists"}`, "")
	fake := &fakeAliyunCertAPI{uploadErrFn: func(call int) error {
		if call == 1 {
			return conflict
		}
		return nil
	}}
	d, rec := newTestAliyunDeployer(fake, nil)

	_, err := d.UploadCert(ctx, testCloudCreds(), testCertPEM, []byte(testKeyPEM))
	assert.NoError(t, err, "命名冲突可重试：重新生成名后成功（poc-notes C2）")
	assert.Empty(t, rec.snapshot(), "冲突重试不占退避睡眠")

	uploads := fake.uploadsSnapshot()
	assert.Len(t, uploads, 2)
	assert.NotEqual(t, uploads[0], uploads[1], "重试使用新名称")
}

// 冲突误判防御：与名称无关的错误不走冲突重试。
func TestAliyunDeployerNameConflictHeuristicNegative(t *testing.T) {
	assert.False(t, isCertNameConflictErr(nil))
	assert.False(t, isCertNameConflictErr(errors.New("listener not found")))
	assert.False(t, isCertNameConflictErr(fmt.Errorf("api error: %w", cloudx.ErrCloudRateLimited)))
	assert.True(t, isCertNameConflictErr(errors.New("certificate name duplicated")))
}

// 退避策略归一化：零值/非法回退缺省保守值。
func TestAliyunRetryPolicyNormalization(t *testing.T) {
	def := DefaultRetryPolicy()
	assert.Equal(t, def, RetryPolicy{}.normalized())
	assert.Equal(t, def, RetryPolicy{MaxAttempts: 0}.normalized())
	assert.Equal(t, def, RetryPolicy{MaxAttempts: 3}.normalized(), "退避序列缺失回退缺省")

	custom := RetryPolicy{
		MaxAttempts:  2,
		Backoffs:     []time.Duration{time.Second},
		MaxTotalWait: time.Second,
	}
	assert.Equal(t, custom, custom.normalized())
}

// ---------------------------------------------------------------------
// 只读面：ListReferences（指纹解析口径同 3.5）/ GetCert / CleanupOrphan
// ---------------------------------------------------------------------

// 指纹解析三级口径：映射反查 → GetCert（SHA256 对齐）→ 确定性占位指纹；
// 同云证书多引用只查一次 GetCert。
func TestAliyunDeployerListReferencesFingerprints(t *testing.T) {
	ctx := context.Background()
	fpMapped := strings.Repeat("a", 64)
	fpFromCloud := strings.Repeat("b", 64)

	mappings := certtest.NewFakeCloudCertMappingRepo()
	assert.NoError(t, mappings.Upsert(ctx, &domain.CloudCertMapping{
		CertFingerprint: fpMapped, Cloud: "aliyun", AccountKey: "acc-main",
		CloudCertID: "100-cn-hangzhou", Status: domain.MappingStatusActive,
	}))

	fake := &fakeAliyunCertAPI{
		listRefs: map[string][]aliyun.CloudCertRef{
			"cdn": {
				{Cloud: "aliyun", Product: "cdn", ResourceID: "www.example.com", ReferencedCloudCertID: "100-cn-hangzhou", AccountKey: "acc-main"},
				{Cloud: "aliyun", Product: "cdn", ResourceID: "api.example.com", ReferencedCloudCertID: "100-cn-hangzhou", AccountKey: "acc-main"},
				{Cloud: "aliyun", Product: "cdn", ResourceID: "m.example.com", ReferencedCloudCertID: "200-cn-hangzhou", AccountKey: "acc-main"},
				{Cloud: "aliyun", Product: "cdn", ResourceID: "s.example.com", ReferencedCloudCertID: "300-cn-hangzhou", AccountKey: "acc-main"},
			},
		},
		getInfo: map[string]aliyun.CloudCertInfo{
			"200-cn-hangzhou": {Exists: true, Fingerprint: fpFromCloud, NotAfter: time.Now().Add(90 * 24 * time.Hour)},
		},
	}
	d, _ := newTestAliyunDeployer(fake, mappings)

	refs, err := d.ListReferences(ctx, testCloudCreds(), "cdn")
	assert.NoError(t, err)
	assert.Len(t, refs, 4)

	byResource := make(map[string]domain.CertReference, len(refs))
	for _, r := range refs {
		byResource[r.ResourceID] = r
	}
	assert.Equal(t, fpMapped, byResource["www.example.com"].CertFingerprint, "映射反查命中")
	assert.Equal(t, fpMapped, byResource["api.example.com"].CertFingerprint)
	assert.Equal(t, fpFromCloud, byResource["m.example.com"].CertFingerprint, "GetCert SHA256 对齐口径")

	// 占位指纹与 3.5 同公式（certscan-unresolved:{cloud}|{accountKey}|{certId}）。
	sum := sha256.Sum256([]byte("certscan-unresolved:aliyun|acc-main|300-cn-hangzhou"))
	assert.Equal(t, hex.EncodeToString(sum[:]), byResource["s.example.com"].CertFingerprint,
		"无法解析 → 确定性占位指纹（与 3.5 扫描路径可对账）")

	assert.Equal(t, []string{"200-cn-hangzhou", "300-cn-hangzhou"}, fake.getSnapshot(),
		"映射命中不查 GetCert；同证书多引用去重仅查一次")

	for _, r := range refs {
		assert.Equal(t, domain.CloudAliyun, r.Cloud)
		assert.Equal(t, domain.ProductCDN, r.Product)
		assert.Equal(t, "acc-main", r.AccountKey)
	}
}

// mappings 缺省（nil）：跳过映射反查，直接 GetCert fallback → 占位指纹。
func TestAliyunDeployerListReferencesWithoutMappings(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAliyunCertAPI{
		listRefs: map[string][]aliyun.CloudCertRef{
			"waf": {{Cloud: "aliyun", Product: "waf", ResourceID: "waf-1", ReferencedCloudCertID: "900", AccountKey: "acc-main"}},
		},
	}
	d, _ := newTestAliyunDeployer(fake, nil)

	refs, err := d.ListReferences(ctx, testCloudCreds(), "waf")
	assert.NoError(t, err)
	sum := sha256.Sum256([]byte("certscan-unresolved:aliyun|acc-main|900"))
	assert.Equal(t, hex.EncodeToString(sum[:]), refs[0].CertFingerprint)
	assert.Equal(t, []string{"900"}, fake.getSnapshot(), "无映射仍尝试 GetCert")
}

// GetCert 字段转换（Exists/NotAfter/Fingerprint）+ 云侧已删除=Exists=false 非错误。
func TestAliyunDeployerGetCert(t *testing.T) {
	ctx := context.Background()
	notAfter := time.Now().Add(30 * 24 * time.Hour)
	fake := &fakeAliyunCertAPI{
		getInfo: map[string]aliyun.CloudCertInfo{
			"8001": {Exists: true, Fingerprint: strings.Repeat("c", 64), NotAfter: notAfter},
		},
	}
	d, _ := newTestAliyunDeployer(fake, nil)

	info, err := d.GetCert(ctx, testCloudCreds(), "8001")
	assert.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Equal(t, strings.Repeat("c", 64), info.Fingerprint)
	assert.WithinDuration(t, notAfter, info.NotAfter, time.Second)

	info, err = d.GetCert(ctx, testCloudCreds(), "404-cert")
	assert.NoError(t, err)
	assert.False(t, info.Exists, "云侧已删除=Exists=false 非错误")
}

// CleanupOrphan 透传 + 限流退避恢复。
func TestAliyunDeployerCleanupOrphanRateLimited(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAliyunCertAPI{cleanupErr: fmt.Errorf("cas delete throttled: %w", cloudx.ErrCloudRateLimited)}
	d, rec := newTestAliyunDeployer(fake, nil)

	err := d.CleanupOrphan(ctx, testCloudCreds(), "8001-cn-hangzhou")
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited, "补偿清理同样有界重试，耗尽透传哨兵")
	assert.Len(t, fake.cleanupsSnapshot(), 5)
	assert.Len(t, rec.snapshot(), 4)
}

// ListReferences 限流：退避后整体重试（整页重取）。
func TestAliyunDeployerListReferencesRateLimited(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAliyunCertAPI{
		listErr:  fmt.Errorf("list throttled: %w", cloudx.ErrCloudRateLimited),
		listRefs: map[string][]aliyun.CloudCertRef{},
	}
	d, _ := newTestAliyunDeployer(fake, nil)

	_, err := d.ListReferences(ctx, testCloudCreds(), "cdn")
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited)
	assert.Len(t, fake.listCalls, 5)
}

// ---------------------------------------------------------------------
// AC-2 / AC-4：经 5.3 CloudAPIChannel 端到端（AliyunDeployer 注入实例）
// ---------------------------------------------------------------------

// 两段式成功端到端：DeployResult 三字段（OldCloudCertID 执行前快照读取）+ 映射 active。
func TestAliyunDeployerChannelDeployTwoStageSuccess(t *testing.T) {
	fake := &fakeAliyunCertAPI{uploadIDs: []string{"9001"}}
	mat := &fakeMaterial{certPEM: testCertPEM, keyPEM: []byte(testKeyPEM), keyVersion: 1}
	old := &fakeOldRefs{
		found: true,
		ref: domain.CertReference{
			Cloud: domain.CloudAliyun, Product: domain.ProductCDN,
			ResourceID: "www.example.com", ReferencedCloudCertID: "100-cn-hangzhou", AccountKey: "acc-main",
		},
	}
	dep, _ := newTestAliyunDeployer(fake, nil)
	mappings := certtest.NewFakeCloudCertMappingRepo()
	ch := NewCloudAPIChannel(mappings, mat, old)
	assert.NoError(t, ch.RegisterDeployer("aliyun", dep, "cdn", "dcdn", "waf", "alb", "nlb"))

	res, err := ch.Deploy(context.Background(), testCloudCreds(), testCloudTarget(), testFingerprint)
	assert.NoError(t, err)

	// AC-2：DeployResult{NewCloudCertID, OldCloudCertID, OrphanCandidate}。
	assert.Equal(t, "9001", res.NewCloudCertID)
	assert.Equal(t, "100-cn-hangzhou", res.OldCloudCertID, "执行前从引用快照读取（回滚依据）")
	assert.True(t, res.OrphanCandidate, "旧云证书被替换 → 孤儿候选")

	// 顺序：upload → bind；绑定用第一段产物。
	uploads := fake.uploadsSnapshot()
	binds := fake.bindsSnapshot()
	assert.Len(t, uploads, 1)
	assert.Len(t, binds, 1)
	assert.Equal(t, "cdn:www.example.com:9001", binds[0])
	assert.Regexp(t, uploadNamePattern, strings.SplitN(uploads[0], ":", 2)[1], "B2 唯一名规则")
	assert.Empty(t, fake.cleanupsSnapshot(), "成功路径不做补偿清理")

	// 映射 active 写入（5.9 崩溃恢复锚点）。
	got, err := mappings.FindByCloudCertID(t.Context(), "aliyun", "acc-main", "9001")
	assert.NoError(t, err)
	assert.Equal(t, domain.MappingStatusActive, got.Status)

	// 凭证转换：AccountKey→Name、AK/SK 明文仅内存透传。
	fake.mu.Lock()
	acct := fake.lastAcct
	fake.mu.Unlock()
	assert.Equal(t, "acc-main", acct.Name)
	assert.Equal(t, sharedomain.CloudProviderAliyun, acct.Provider)
	assert.Equal(t, "LTAI-test-ak", acct.AccessKeyID)
}

// 第二段绑定失败端到端：CleanupOrphan 补偿清理 + 映射 active→orphan + OrphanCandidate=true。
func TestAliyunDeployerChannelBindFailureCompensates(t *testing.T) {
	fake := &fakeAliyunCertAPI{uploadIDs: []string{"9002"}, bindErr: errors.New("listener not found")}
	mat := &fakeMaterial{certPEM: testCertPEM, keyPEM: []byte(testKeyPEM), keyVersion: 1}
	dep, _ := newTestAliyunDeployer(fake, nil)
	mappings := certtest.NewFakeCloudCertMappingRepo()
	ch := NewCloudAPIChannel(mappings, mat, &fakeOldRefs{})
	assert.NoError(t, ch.RegisterDeployer("aliyun", dep, "cdn", "dcdn", "waf", "alb", "nlb"))

	res, err := ch.Deploy(context.Background(), testCloudCreds(), testCloudTarget(), testFingerprint)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "listener not found")

	// AC-4：OrphanCandidate=true 供 5.9 消费；第一段产物回传。
	assert.True(t, res.OrphanCandidate)
	assert.Equal(t, "9002", res.NewCloudCertID)

	// 补偿清理：未绑定云侧证书经 CleanupOrphan 删除。
	assert.Equal(t, []string{"9002"}, fake.cleanupsSnapshot())

	// 映射状态流转 active→orphan（5.9 孤儿队列入口）。
	got, err := mappings.FindByCloudCertID(t.Context(), "aliyun", "acc-main", "9002")
	assert.NoError(t, err)
	assert.Equal(t, domain.MappingStatusOrphan, got.Status)
}

// uniqueStrings 去重计数小工具。
func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
