package deployer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/tencent"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
)

// ---------------------------------------------------------------------
// 测试替身：mock 3.2 腾讯云证书适配（tencentCertAPI 窄接口）
// ---------------------------------------------------------------------

// fakeTencentCertAPI mock 3.2 CertAdapter：记录调用序列与上传别名，支持按调用
// 次数注入错误（限流/一般失败）与逐次返回 ID。
type fakeTencentCertAPI struct {
	uploadCalls []string // "product:alias" 逐次
	uploadIDs   []string // 逐次返回 ID；耗尽沿用末值（缺省 ssl-9001）
	uploadErrFn func(call int) error
	lastAcct    *sharedomain.CloudAccount
	binds       []string // "product:resource:cert"
	bindErrFn   func(call int) error
	bindErr     error
	listCalls   []string
	listRefs    map[string][]tencent.CloudCertRef
	listErr     error
	getCalls    []string
	getInfo     map[string]tencent.CloudCertInfo
	getErr      error
	cleanups    []string
	cleanupErr  error
}

func (f *fakeTencentCertAPI) UploadCert(_ context.Context, acct *sharedomain.CloudAccount, product, name, _, _ string) (string, error) {
	n := len(f.uploadCalls) + 1
	f.uploadCalls = append(f.uploadCalls, product+":"+name)
	f.lastAcct = acct
	if f.uploadErrFn != nil {
		if err := f.uploadErrFn(n); err != nil {
			return "", err
		}
	}
	id := "ssl-9001"
	if len(f.uploadIDs) >= n && f.uploadIDs[n-1] != "" {
		id = f.uploadIDs[n-1]
	} else if len(f.uploadIDs) > 0 {
		id = f.uploadIDs[len(f.uploadIDs)-1]
	}
	return id, nil
}

func (f *fakeTencentCertAPI) BindResource(_ context.Context, _ *sharedomain.CloudAccount, product, resourceID, cloudCertID string) error {
	n := len(f.binds) + 1
	f.binds = append(f.binds, product+":"+resourceID+":"+cloudCertID)
	if f.bindErrFn != nil {
		if err := f.bindErrFn(n); err != nil {
			return err
		}
	}
	return f.bindErr
}

func (f *fakeTencentCertAPI) ListReferences(_ context.Context, _ *sharedomain.CloudAccount, product string) ([]tencent.CloudCertRef, error) {
	f.listCalls = append(f.listCalls, product)
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listRefs[product], nil
}

func (f *fakeTencentCertAPI) GetCert(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) (tencent.CloudCertInfo, error) {
	f.getCalls = append(f.getCalls, cloudCertID)
	if f.getErr != nil {
		return tencent.CloudCertInfo{}, f.getErr
	}
	if info, ok := f.getInfo[cloudCertID]; ok {
		return info, nil
	}
	return tencent.CloudCertInfo{}, nil
}

func (f *fakeTencentCertAPI) CleanupOrphan(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) error {
	f.cleanups = append(f.cleanups, cloudCertID)
	return f.cleanupErr
}

func (f *fakeTencentCertAPI) uploadsSnapshot() []string {
	return append([]string(nil), f.uploadCalls...)
}

func (f *fakeTencentCertAPI) bindsSnapshot() []string {
	return append([]string(nil), f.binds...)
}

func (f *fakeTencentCertAPI) getSnapshot() []string {
	return append([]string(nil), f.getCalls...)
}

func (f *fakeTencentCertAPI) cleanupsSnapshot() []string {
	return append([]string(nil), f.cleanups...)
}

// newTestTencentDeployer 装配被测部署器：fake 适配 + 确定性时间/随机后缀
// （别名唯一性可精确断言）+ 即时睡眠记录器（复用 aliyun 测试的 sleepRecorder）。
func newTestTencentDeployer(fake *fakeTencentCertAPI, mappings domain.CloudCertMappingRepository) (*TencentDeployer, *sleepRecorder) {
	d := NewTencentDeployer(fake, mappings)
	rec := &sleepRecorder{}
	d.sleep = rec.sleep
	d.now = func() time.Time { return time.Unix(1765432100, 0) }
	counter := 0
	d.randHex = func(int) string { counter++; return fmt.Sprintf("%04x", counter) }
	return d, rec
}

// testTencentCreds 腾讯云口径凭证（区别于 aliyun 口径的 testCloudCreds）。
func testTencentCreds() Credential {
	return Credential{
		Kind: CredentialKindCloudAK, Cloud: "tencent", AccountKey: "acc-tx",
		AccessKey: "AKIDtestak", Secret: []byte("test-sk-plaintext"), KeyVersion: 1,
	}
}

// testTencentTarget 腾讯云口径部署目标（CDN 域名级 resourceID）。
func testTencentTarget() DeployTarget {
	return DeployTarget{
		Channel: "cloud_api", Cloud: "tencent", Product: "cdn",
		AccountKey: "acc-tx", ResourceID: "www.example.com",
	}
}

// ---------------------------------------------------------------------
// AC-1：三产品路由 + CloudDeployer 端口实现
// ---------------------------------------------------------------------

// 三产品按 DeployTarget.product 路由至 3.2 适配方法（AC-1）；
// resourceId 粒度约定原样透传（CDN=域名 / EdgeOne="{ZoneId}/{Host}" / CLB="{LoadBalancerId}/{ListenerId}"）。
func TestTencentDeployerBindRoutesThreeProducts(t *testing.T) {
	ctx := context.Background()
	fake := &fakeTencentCertAPI{}
	d, _ := newTestTencentDeployer(fake, nil)

	assert.NoError(t, d.BindResource(ctx, testTencentCreds(), "cdn", "www.example.com", "ssl-9001"))
	assert.NoError(t, d.BindResource(ctx, testTencentCreds(), "waf", "zone-123/www.example.com", "ssl-9001"))
	assert.NoError(t, d.BindResource(ctx, testTencentCreds(), "clb", "lb-abc/lbl-def", "ssl-9001"))

	binds := fake.bindsSnapshot()
	assert.Len(t, binds, 3, "三产品各一次绑定")
	assert.Equal(t, "cdn:www.example.com:ssl-9001", binds[0])
	assert.Equal(t, "waf:zone-123/www.example.com:ssl-9001", binds[1], "EdgeOne 复合 resourceID 原样透传")
	assert.Equal(t, "clb:lb-abc/lbl-def:ssl-9001", binds[2], "CLB 监听器级复合 resourceID 原样透传")
}

// 未支持产品（alb 等腾讯云首期未实现���：3.1 适配哨兵错误透传。
func TestTencentDeployerBindUnsupportedProduct(t *testing.T) {
	ctx := context.Background()
	fake := &fakeTencentCertAPI{bindErr: tencent.ErrCertProductNotSupported}
	d, _ := newTestTencentDeployer(fake, nil)

	err := d.BindResource(ctx, testTencentCreds(), "alb", "lb-1", "ssl-9001")
	assert.ErrorIs(t, err, tencent.ErrCertProductNotSupported)
}

// 凭证归属云不符/凭证非法：显式拒绝且不触达适配层。
func TestTencentDeployerRejectsForeignCredential(t *testing.T) {
	ctx := context.Background()
	fake := &fakeTencentCertAPI{}
	d, _ := newTestTencentDeployer(fake, nil)

	foreign := testTencentCreds()
	foreign.Cloud = "aliyun"
	_, err := d.UploadCert(ctx, foreign, testCertPEM, []byte(testKeyPEM))
	assert.ErrorContains(t, err, "not tencent")

	invalid := testTencentCreds()
	invalid.AccessKey = ""
	_, err = d.UploadCert(ctx, invalid, testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, ErrInvalidCredential)

	assert.Empty(t, fake.uploadsSnapshot(), "校验失败不产生云侧调用")
}

// ---------------------------------------------------------------------
// C7：上传别名唯一性（腾讯云 Alias 可重复，Repeatable=true 独立副本；
// 重试不复用可能已成功的别名）
// ---------------------------------------------------------------------

// 别名规则：ecam-{指纹前8}-{unix秒}-{随机后缀}，逐次唯一（C7）；
// 真实证书束时指纹前缀 = 台账指纹前 8 位。
func TestTencentUploadAliasGeneration(t *testing.T) {
	bundle := certtest.NewBundle(t, "www.example.com", nil, nil)
	fake := &fakeTencentCertAPI{}
	d, _ := newTestTencentDeployer(fake, nil)

	ctx := context.Background()
	id, err := d.UploadCert(ctx, testTencentCreds(), string(bundle.CertPEM), bundle.KeyPEM)
	assert.NoError(t, err)
	assert.Equal(t, "ssl-9001", id)

	uploads := fake.uploadsSnapshot()
	assert.Len(t, uploads, 1)
	product, alias, ok := strings.Cut(uploads[0], ":")
	assert.True(t, ok)
	assert.Equal(t, "cdn", product, "第一段统一经 ssl 库上传（产品仅作校验口径）")
	assert.Regexp(t, uploadNamePattern, alias)
	assert.Contains(t, alias, bundle.Fingerprint[:8], "指纹前 8 位来源=证书叶 DER SHA256")
	assert.Contains(t, alias, "1765432100", "unix 秒时间戳分量")

	// 同材料重复生成：随机后缀保证唯一（C7：重试不复用别名）。
	names := []string{alias}
	for i := 0; i < 5; i++ {
		_, err := d.UploadCert(ctx, testTencentCreds(), string(bundle.CertPEM), bundle.KeyPEM)
		assert.NoError(t, err)
		names = append(names, strings.SplitN(fake.uploadsSnapshot()[len(names)], ":", 2)[1])
	}
	assert.Len(t, uniqueStrings(names), len(names), "逐次生成别名互不相同")
}

// 材料缺失：显式拒绝。
func TestTencentDeployerUploadRejectsEmptyMaterial(t *testing.T) {
	ctx := context.Background()
	fake := &fakeTencentCertAPI{}
	d, _ := newTestTencentDeployer(fake, nil)

	_, err := d.UploadCert(ctx, testTencentCreds(), "", []byte(testKeyPEM))
	assert.ErrorContains(t, err, "requires cert PEM and key PEM")
	_, err = d.UploadCert(ctx, testTencentCreds(), testCertPEM, nil)
	assert.ErrorContains(t, err, "requires cert PEM and key PEM")
	assert.Empty(t, fake.uploadsSnapshot())
}

// ---------------------------------------------------------------------
// AC-3：限流退避（固定序列，有界上限）
// ---------------------------------------------------------------------

// 限流后退避恢复：按固定序列睡眠，逐次换别名重试（C7），最终成功。
func TestTencentDeployerRateLimitBackoffRecovers(t *testing.T) {
	ctx := context.Background()
	fake := &fakeTencentCertAPI{uploadErrFn: func(call int) error {
		if call <= 2 {
			return fmt.Errorf("tencent ssl api throttled: %w", cloudx.ErrCloudRateLimited)
		}
		return nil
	}}
	d, rec := newTestTencentDeployer(fake, nil)

	id, err := d.UploadCert(ctx, testTencentCreds(), testCertPEM, []byte(testKeyPEM))
	assert.NoError(t, err)
	assert.Equal(t, "ssl-9001", id)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, rec.snapshot(),
		"固定退避序列 1s/2s（默认策略前两档）")

	uploads := fake.uploadsSnapshot()
	assert.Len(t, uploads, 3)
	assert.Len(t, uniqueStrings(uploads), 3, "重试逐次换别名（不得复用可能已成功的别名）")
}

// 限流持续：次数上限耗尽即失败，绝不无限重试（Hard Rule）；哨兵语义保留。
func TestTencentDeployerRateLimitExhaustsByAttempts(t *testing.T) {
	ctx := context.Background()
	fake := &fakeTencentCertAPI{uploadErrFn: func(int) error {
		return fmt.Errorf("tencent ssl api throttled: %w", cloudx.ErrCloudRateLimited)
	}}
	d, rec := newTestTencentDeployer(fake, nil)

	_, err := d.UploadCert(ctx, testTencentCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited, "耗尽后哨兵仍可判定（5.7 映射 rate_limited/failed）")
	assert.ErrorContains(t, err, "retries exhausted after 5 attempts")
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second},
		rec.snapshot(), "默认固定序列全量消费后停止")
	assert.Len(t, fake.uploadsSnapshot(), 5, "默认 MaxAttempts=5")
}

// 退避总时长上限：下一档退避将超总时长即停止（Hard Rule：次数+总时长双闸）。
func TestTencentDeployerRateLimitExhaustsByTotalWait(t *testing.T) {
	ctx := context.Background()
	fake := &fakeTencentCertAPI{uploadErrFn: func(int) error {
		return fmt.Errorf("throttled: %w", cloudx.ErrCloudRateLimited)
	}}
	d, rec := newTestTencentDeployer(fake, nil)
	d.retry = RetryPolicy{
		MaxAttempts:  10,
		Backoffs:     []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second},
		MaxTotalWait: 3 * time.Second,
	}

	_, err := d.UploadCert(ctx, testTencentCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited)
	assert.ErrorContains(t, err, "total backoff cap")
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, rec.snapshot(),
		"累计 3s 后，下一档 4s 超总时长上限即止（尝试 3 次）")
	assert.Len(t, fake.uploadsSnapshot(), 3)
}

// 退避睡眠被取消：返回 ctx 错误，不再继续尝试。
func TestTencentDeployerBackoffCanceled(t *testing.T) {
	ctx := context.Background()
	fake := &fakeTencentCertAPI{uploadErrFn: func(int) error {
		return fmt.Errorf("throttled: %w", cloudx.ErrCloudRateLimited)
	}}
	d, rec := newTestTencentDeployer(fake, nil)
	rec.err = context.DeadlineExceeded

	_, err := d.UploadCert(ctx, testTencentCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Len(t, fake.uploadsSnapshot(), 1, "首试失败进入退避，退避中断即止（重试未发生）")
}

// 非限流错误：立即返回，不退避不重试。
func TestTencentDeployerNonRetryableNoRetry(t *testing.T) {
	ctx := context.Background()
	fake := &fakeTencentCertAPI{uploadErrFn: func(int) error {
		return errors.New("FailedOperation.CertificateParseError: 证书解析失败")
	}}
	d, rec := newTestTencentDeployer(fake, nil)

	_, err := d.UploadCert(ctx, testTencentCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorContains(t, err, "CertificateParseError")
	assert.Empty(t, rec.snapshot(), "一般失败不退避")
	assert.Len(t, fake.uploadsSnapshot(), 1)
}

// WithTencentRetryPolicy 注入即归一化（零值/非法回退缺省保守值）。
func TestTencentRetryPolicyOptionNormalization(t *testing.T) {
	def := DefaultRetryPolicy()
	d := NewTencentDeployer(&fakeTencentCertAPI{}, nil,
		WithTencentRetryPolicy(RetryPolicy{MaxAttempts: 0}))
	assert.Equal(t, def, d.retry)

	custom := RetryPolicy{MaxAttempts: 2, Backoffs: []time.Duration{time.Second}, MaxTotalWait: time.Second}
	d = NewTencentDeployer(&fakeTencentCertAPI{}, nil, WithTencentRetryPolicy(custom))
	assert.Equal(t, custom.normalized(), d.retry)
}

// ---------------------------------------------------------------------
// 只读面：ListReferences（指纹解析口径同 3.5/5.4）/ GetCert / CleanupOrphan
// ---------------------------------------------------------------------

// 指纹解析三级口径：映射反查 → GetCert（SHA256 对齐）→ 确定性占位指纹；
// 同云证书多引用只查一次 GetCert。
func TestTencentDeployerListReferencesFingerprints(t *testing.T) {
	ctx := context.Background()
	fpMapped := strings.Repeat("a", 64)
	fpFromCloud := strings.Repeat("b", 64)

	mappings := certtest.NewFakeCloudCertMappingRepo()
	assert.NoError(t, mappings.Upsert(ctx, &domain.CloudCertMapping{
		CertFingerprint: fpMapped, Cloud: "tencent", AccountKey: "acc-tx",
		CloudCertID: "ssl-100", Status: domain.MappingStatusActive,
	}))

	fake := &fakeTencentCertAPI{
		listRefs: map[string][]tencent.CloudCertRef{
			"cdn": {
				{Cloud: "tencent", Product: "cdn", ResourceID: "www.example.com", ReferencedCloudCertID: "ssl-100", AccountKey: "acc-tx"},
				{Cloud: "tencent", Product: "cdn", ResourceID: "api.example.com", ReferencedCloudCertID: "ssl-100", AccountKey: "acc-tx"},
				{Cloud: "tencent", Product: "cdn", ResourceID: "m.example.com", ReferencedCloudCertID: "ssl-200", AccountKey: "acc-tx"},
				{Cloud: "tencent", Product: "cdn", ResourceID: "s.example.com", ReferencedCloudCertID: "ssl-300", AccountKey: "acc-tx"},
			},
		},
		getInfo: map[string]tencent.CloudCertInfo{
			"ssl-200": {Exists: true, Fingerprint: fpFromCloud, NotAfter: time.Now().Add(90 * 24 * time.Hour)},
		},
	}
	d, _ := newTestTencentDeployer(fake, mappings)

	refs, err := d.ListReferences(ctx, testTencentCreds(), "cdn")
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
	sum := sha256.Sum256([]byte("certscan-unresolved:tencent|acc-tx|ssl-300"))
	assert.Equal(t, hex.EncodeToString(sum[:]), byResource["s.example.com"].CertFingerprint,
		"无法解析（含 SHA1 回退形态 40hex 不对齐）→ 确定性占位指纹（与 3.5 扫描路径可对账）")

	assert.Equal(t, []string{"ssl-200", "ssl-300"}, fake.getSnapshot(),
		"映射命中不查 GetCert；同证书多引用去重仅查一次")

	for _, r := range refs {
		assert.Equal(t, domain.CloudTencent, r.Cloud)
		assert.Equal(t, domain.ProductCDN, r.Product)
		assert.Equal(t, "acc-tx", r.AccountKey)
	}
}

// mappings 缺省（nil）：跳过映射反查，直接 GetCert fallback → 占位指纹。
func TestTencentDeployerListReferencesWithoutMappings(t *testing.T) {
	ctx := context.Background()
	fake := &fakeTencentCertAPI{
		listRefs: map[string][]tencent.CloudCertRef{
			"waf": {{Cloud: "tencent", Product: "waf", ResourceID: "zone-1/s.example.com", ReferencedCloudCertID: "ssl-900", AccountKey: "acc-tx"}},
		},
	}
	d, _ := newTestTencentDeployer(fake, nil)

	refs, err := d.ListReferences(ctx, testTencentCreds(), "waf")
	assert.NoError(t, err)
	sum := sha256.Sum256([]byte("certscan-unresolved:tencent|acc-tx|ssl-900"))
	assert.Equal(t, hex.EncodeToString(sum[:]), refs[0].CertFingerprint)
	assert.Equal(t, []string{"ssl-900"}, fake.getSnapshot(), "无映射仍尝试 GetCert")
}

// GetCert 字段转换（Exists/NotAfter/Fingerprint）+ 云侧已删除=Exists=false 非错误。
func TestTencentDeployerGetCert(t *testing.T) {
	ctx := context.Background()
	notAfter := time.Now().Add(30 * 24 * time.Hour)
	fake := &fakeTencentCertAPI{
		getInfo: map[string]tencent.CloudCertInfo{
			"ssl-9001": {Exists: true, Fingerprint: strings.Repeat("c", 64), NotAfter: notAfter},
		},
	}
	d, _ := newTestTencentDeployer(fake, nil)

	info, err := d.GetCert(ctx, testTencentCreds(), "ssl-9001")
	assert.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Equal(t, strings.Repeat("c", 64), info.Fingerprint)
	assert.WithinDuration(t, notAfter, info.NotAfter, time.Second)

	info, err = d.GetCert(ctx, testTencentCreds(), "404-cert")
	assert.NoError(t, err)
	assert.False(t, info.Exists, "云侧已删除=Exists=false 非错误")
}

// CleanupOrphan 透传 + 限流退避有界（B1 异步轮询在 3.2 适配层内部消化）。
func TestTencentDeployerCleanupOrphanRateLimited(t *testing.T) {
	ctx := context.Background()
	fake := &fakeTencentCertAPI{cleanupErr: fmt.Errorf("ssl delete throttled: %w", cloudx.ErrCloudRateLimited)}
	d, rec := newTestTencentDeployer(fake, nil)

	err := d.CleanupOrphan(ctx, testTencentCreds(), "ssl-9001")
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited, "清理同样有界重试，耗尽透传哨兵")
	assert.Len(t, fake.cleanupsSnapshot(), 5)
	assert.Len(t, rec.snapshot(), 4)
}

// ListReferences 限流：退避后整体重试（整页重取）。
func TestTencentDeployerListReferencesRateLimited(t *testing.T) {
	ctx := context.Background()
	fake := &fakeTencentCertAPI{
		listErr:  fmt.Errorf("list throttled: %w", cloudx.ErrCloudRateLimited),
		listRefs: map[string][]tencent.CloudCertRef{},
	}
	d, _ := newTestTencentDeployer(fake, nil)

	_, err := d.ListReferences(ctx, testTencentCreds(), "cdn")
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited)
	assert.Len(t, fake.listCalls, 5)
}

// ---------------------------------------------------------------------
// AC-2 / AC-4：经 5.3 CloudAPIChannel 端到端（TencentDeployer 注入实例）
// ---------------------------------------------------------------------

// 两段式成功端到端：DeployResult 三字段（OldCloudCertID 执行前快照读取）+ 映射 active。
func TestTencentDeployerChannelDeployTwoStageSuccess(t *testing.T) {
	fake := &fakeTencentCertAPI{uploadIDs: []string{"ssl-9101"}}
	mat := &fakeMaterial{certPEM: testCertPEM, keyPEM: []byte(testKeyPEM), keyVersion: 1}
	old := &fakeOldRefs{
		found: true,
		ref: domain.CertReference{
			Cloud: domain.CloudTencent, Product: domain.ProductCDN,
			ResourceID: "www.example.com", ReferencedCloudCertID: "ssl-100", AccountKey: "acc-tx",
		},
	}
	dep, _ := newTestTencentDeployer(fake, nil)
	mappings := certtest.NewFakeCloudCertMappingRepo()
	ch := NewCloudAPIChannel(mappings, mat, old)
	assert.NoError(t, ch.RegisterDeployer("tencent", dep, "cdn", "waf", "clb"))

	res, err := ch.Deploy(context.Background(), testTencentCreds(), testTencentTarget(), testFingerprint)
	assert.NoError(t, err)

	// AC-2：DeployResult{NewCloudCertID, OldCloudCertID, OrphanCandidate}。
	assert.Equal(t, "ssl-9101", res.NewCloudCertID)
	assert.Equal(t, "ssl-100", res.OldCloudCertID, "执行前从引用快照读取（回滚依据）")
	assert.True(t, res.OrphanCandidate, "旧云证书被替换 → 孤儿候选")

	// 顺序：upload → bind；绑定用第一段产物。
	uploads := fake.uploadsSnapshot()
	binds := fake.bindsSnapshot()
	assert.Len(t, uploads, 1)
	assert.Len(t, binds, 1)
	assert.Equal(t, "cdn:www.example.com:ssl-9101", binds[0])
	assert.Regexp(t, uploadNamePattern, strings.SplitN(uploads[0], ":", 2)[1], "C7 唯一别名规则")
	assert.Empty(t, fake.cleanupsSnapshot(), "成功路径不做补偿清理")

	// 映射 active 写入（5.9 崩溃恢复锚点）。
	got, err := mappings.FindByCloudCertID(t.Context(), "tencent", "acc-tx", "ssl-9101")
	assert.NoError(t, err)
	assert.Equal(t, domain.MappingStatusActive, got.Status)

	// 凭证转换：AccountKey→Name、AK/SK 明文仅内存透传。
	acct := fake.lastAcct
	assert.Equal(t, "acc-tx", acct.Name)
	assert.Equal(t, sharedomain.CloudProviderTencent, acct.Provider)
	assert.Equal(t, "AKIDtestak", acct.AccessKeyID)
}

// 第二段绑定失败端到端：CleanupOrphan 补偿清理 + 映射 active→orphan + OrphanCandidate=true。
func TestTencentDeployerChannelBindFailureCompensates(t *testing.T) {
	fake := &fakeTencentCertAPI{uploadIDs: []string{"ssl-9102"}, bindErr: errors.New("listener not found")}
	mat := &fakeMaterial{certPEM: testCertPEM, keyPEM: []byte(testKeyPEM), keyVersion: 1}
	dep, _ := newTestTencentDeployer(fake, nil)
	mappings := certtest.NewFakeCloudCertMappingRepo()
	ch := NewCloudAPIChannel(mappings, mat, &fakeOldRefs{})
	assert.NoError(t, ch.RegisterDeployer("tencent", dep, "cdn", "waf", "clb"))

	res, err := ch.Deploy(context.Background(), testTencentCreds(), testTencentTarget(), testFingerprint)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "listener not found")

	// AC-4：OrphanCandidate=true 供 5.9 消费；第一段产物回传。
	assert.True(t, res.OrphanCandidate)
	assert.Equal(t, "ssl-9102", res.NewCloudCertID)

	// 补偿清理：未绑定云侧证书经 CleanupOrphan 删除。
	assert.Equal(t, []string{"ssl-9102"}, fake.cleanupsSnapshot())

	// 映射状态流转 active→orphan（5.9 孤儿队列入口）。
	got, err := mappings.FindByCloudCertID(t.Context(), "tencent", "acc-tx", "ssl-9102")
	assert.NoError(t, err)
	assert.Equal(t, domain.MappingStatusOrphan, got.Status)
}
