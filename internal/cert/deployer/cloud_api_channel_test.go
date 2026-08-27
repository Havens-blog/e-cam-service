package deployer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------
// 测试替身：mock CloudDeployer（AC-6）与来源端口 fake
// ---------------------------------------------------------------------

// fakeCloudDeployer mock 云部署器：按注入错误/返回值执行五方法并记录调用序列。
type fakeCloudDeployer struct {
	mu         sync.Mutex
	calls      []string // 调用序列："upload" | "bind:<product>:<resource>:<cert>" | "cleanup:<cert>" | "list:<product>" | "get:<cert>"
	uploadID   string   // UploadCert 返回的云证书 ID
	uploadErr  error
	bindErr    error
	cleanupErr error
	listErr    error
	listRefs   map[string][]domain.CertReference
}

func (f *fakeCloudDeployer) record(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

func (f *fakeCloudDeployer) UploadCert(_ context.Context, _ Credential, _ string, _ []byte) (string, error) {
	f.record("upload")
	if f.uploadErr != nil {
		return "", f.uploadErr
	}
	return f.uploadID, nil
}

func (f *fakeCloudDeployer) BindResource(_ context.Context, _ Credential, product, resourceID, cloudCertID string) error {
	f.record("bind:%s:%s:%s", product, resourceID, cloudCertID)
	return f.bindErr
}

func (f *fakeCloudDeployer) ListReferences(_ context.Context, _ Credential, product string) ([]domain.CertReference, error) {
	f.record("list:%s", product)
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listRefs[product], nil
}

func (f *fakeCloudDeployer) GetCert(_ context.Context, _ Credential, cloudCertID string) (CloudCertInfo, error) {
	f.record("get:%s", cloudCertID)
	return CloudCertInfo{}, nil
}

func (f *fakeCloudDeployer) CleanupOrphan(_ context.Context, _ Credential, cloudCertID string) error {
	f.record("cleanup:%s", cloudCertID)
	return f.cleanupErr
}

func (f *fakeCloudDeployer) callsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeCloudDeployer) callIndex(prefix string) int {
	for i, c := range f.callsSnapshot() {
		if strings.HasPrefix(c, prefix) {
			return i
		}
	}
	return -1
}

// fakeMaterial mock 证书材料来源。
type fakeMaterial struct {
	certPEM    string
	keyPEM     []byte
	keyVersion int
	err        error
}

func (f *fakeMaterial) Material(_ context.Context, _ string) (string, []byte, int, error) {
	return f.certPEM, f.keyPEM, f.keyVersion, f.err
}

// fakeOldRefs mock 引用快照来源。
type fakeOldRefs struct {
	ref   domain.CertReference
	found bool
	err   error
}

func (f *fakeOldRefs) CurrentRef(_ context.Context, _, _, _ string) (domain.CertReference, bool, error) {
	return f.ref, f.found, f.err
}

// failingStatusMappingRepo 包装 certtest fake，注入 UpdateStatus 失败
// （补偿路径"映射转 orphan 失败"场景）。
type failingStatusMappingRepo struct {
	*certtest.FakeCloudCertMappingRepo
	failStatus bool
}

func (f *failingStatusMappingRepo) UpdateStatus(ctx context.Context, id string, status domain.MappingStatus) error {
	if f.failStatus {
		return errors.New("injected update-status failure")
	}
	return f.FakeCloudCertMappingRepo.UpdateStatus(ctx, id, status)
}

// ---------------------------------------------------------------------
// 测试装配
// ---------------------------------------------------------------------

const (
	testFingerprint = "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2"
	testCertPEM     = "-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----\n"
	testKeyPEM      = "-----BEGIN PRIVATE KEY-----\nfake-key-material\n-----END PRIVATE KEY-----\n"
)

func testCloudCreds() Credential {
	return Credential{
		Kind: CredentialKindCloudAK, Cloud: "aliyun", AccountKey: "acc-main",
		AccessKey: "LTAI-test-ak", Secret: []byte("test-sk-plaintext"), KeyVersion: 1,
	}
}

func testCloudTarget() DeployTarget {
	return DeployTarget{
		Channel: "cloud_api", Cloud: "aliyun", Product: "cdn",
		AccountKey: "acc-main", ResourceID: "www.example.com",
	}
}

// newTestChannel 装配被测通道（aliyun 假部署器 + 三类依赖 fake）。
func newTestChannel(dep *fakeCloudDeployer, mat *fakeMaterial, old *fakeOldRefs) (*CloudAPIChannel, *certtest.FakeCloudCertMappingRepo) {
	mappings := certtest.NewFakeCloudCertMappingRepo()
	ch := NewCloudAPIChannel(mappings, mat, old)
	if err := ch.RegisterDeployer("aliyun", dep, "cdn", "dcdn", "waf", "alb", "nlb"); err != nil {
		panic(err)
	}
	return ch, mappings
}

// ---------------------------------------------------------------------
// Type / 注册
// ---------------------------------------------------------------------

func TestCloudAPIChannelType(t *testing.T) {
	ch, _ := newTestChannel(&fakeCloudDeployer{uploadID: "x"}, &fakeMaterial{}, &fakeOldRefs{})
	assert.Equal(t, ChannelTypeCloudAPI, ch.Type())
}

func TestRegisterDeployerValidation(t *testing.T) {
	ch := NewCloudAPIChannel(nil, nil, nil)
	assert.ErrorIs(t, ch.RegisterDeployer("", &fakeCloudDeployer{}, "cdn"), ErrDeployerNotRegistered)
	assert.ErrorIs(t, ch.RegisterDeployer("aliyun", nil, "cdn"), ErrDeployerNotRegistered)
	assert.ErrorIs(t, ch.RegisterDeployer("aliyun", &fakeCloudDeployer{}), ErrDeployerNotRegistered, "产品集至少一项（Discover 默认范围）")
	assert.NoError(t, ch.RegisterDeployer("aliyun", &fakeCloudDeployer{}, "cdn"))
}

// ---------------------------------------------------------------------
// AC-2：两段式成功
// ---------------------------------------------------------------------

func TestCloudAPIChannelDeployTwoStageSuccess(t *testing.T) {
	dep := &fakeCloudDeployer{uploadID: "cloud-cert-777"}
	mat := &fakeMaterial{certPEM: testCertPEM, keyPEM: []byte(testKeyPEM), keyVersion: 2}
	old := &fakeOldRefs{
		found: true,
		ref: domain.CertReference{
			CertFingerprint:       strings.Repeat("9", 64),
			Cloud:                 domain.CloudAliyun,
			Product:               domain.ProductCDN,
			ResourceID:            "www.example.com",
			ReferencedCloudCertID: "old-cert-1",
			AccountKey:            "acc-main",
		},
	}
	ch, mappings := newTestChannel(dep, mat, old)

	res, err := ch.Deploy(context.Background(), testCloudCreds(), testCloudTarget(), testFingerprint)
	assert.NoError(t, err)

	// DeployResult 三字段（AC-2）。
	assert.Equal(t, "cloud-cert-777", res.NewCloudCertID)
	assert.Equal(t, "old-cert-1", res.OldCloudCertID, "执行前从引用快照读取")
	assert.True(t, res.OrphanCandidate, "旧云证书被替换 → 孤儿候选")

	// 两段式顺序：先 UploadCert 后 BindResource，且绑定用第一段产物。
	assert.Greater(t, dep.callIndex("bind:"), dep.callIndex("upload"),
		"第二段 BindResource 必须在第一段 UploadCert 之后")
	assert.Equal(t, []string{"upload", "bind:cdn:www.example.com:cloud-cert-777"}, dep.callsSnapshot())
	assert.Equal(t, -1, dep.callIndex("cleanup:"), "成功路径不做补偿清理")

	// 映射写入：{certFingerprint,cloud,accountKey,cloudCertId,status=active}。
	got, err := mappings.FindByCloudCertID(t.Context(), "aliyun", "acc-main", "cloud-cert-777")
	assert.NoError(t, err)
	assert.Equal(t, testFingerprint, got.CertFingerprint)
	assert.Equal(t, "aliyun", got.Cloud)
	assert.Equal(t, "acc-main", got.AccountKey)
	assert.Equal(t, "cloud-cert-777", got.CloudCertID)
	assert.Equal(t, domain.MappingStatusActive, got.Status)
}

// 无已知旧引用（首次部署）：OldCloudCertID 空、不构成孤儿候选。
func TestCloudAPIChannelDeployWithoutOldRef(t *testing.T) {
	dep := &fakeCloudDeployer{uploadID: "cloud-cert-1"}
	ch, _ := newTestChannel(dep, &fakeMaterial{certPEM: testCertPEM, keyPEM: []byte(testKeyPEM), keyVersion: 1}, &fakeOldRefs{})

	res, err := ch.Deploy(context.Background(), testCloudCreds(), testCloudTarget(), testFingerprint)
	assert.NoError(t, err)
	assert.Equal(t, "cloud-cert-1", res.NewCloudCertID)
	assert.Empty(t, res.OldCloudCertID)
	assert.False(t, res.OrphanCandidate)
}

// ---------------------------------------------------------------------
// AC-3：第二段失败 → 补偿 + 孤儿标记
// ---------------------------------------------------------------------

func TestCloudAPIChannelDeployBindFailureCompensates(t *testing.T) {
	dep := &fakeCloudDeployer{uploadID: "cloud-cert-9", bindErr: errors.New("listener not found")}
	ch, mappings := newTestChannel(dep, &fakeMaterial{certPEM: testCertPEM, keyPEM: []byte(testKeyPEM), keyVersion: 1}, &fakeOldRefs{})

	res, err := ch.Deploy(context.Background(), testCloudCreds(), testCloudTarget(), testFingerprint)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "listener not found")

	// OrphanCandidate=true 供 5.9 队列消费；第一段产物 ID 回传。
	assert.True(t, res.OrphanCandidate)
	assert.Equal(t, "cloud-cert-9", res.NewCloudCertID)

	// 补偿：CleanupOrphan 清理未绑定云侧孤儿证书。
	assert.Equal(t, []string{"upload", "bind:cdn:www.example.com:cloud-cert-9", "cleanup:cloud-cert-9"}, dep.callsSnapshot())

	// 映射状态流转 active→orphan。
	got, err := mappings.FindByCloudCertID(t.Context(), "aliyun", "acc-main", "cloud-cert-9")
	assert.NoError(t, err)
	assert.Equal(t, domain.MappingStatusOrphan, got.Status, "映射表状态流转 active→orphan")
}

// 补偿清理自身失败：主错误附加补偿未竟提示，映射保持 orphan 供 5.9 重试。
func TestCloudAPIChannelDeployBindFailureCleanupFails(t *testing.T) {
	dep := &fakeCloudDeployer{
		uploadID:   "cloud-cert-10",
		bindErr:    errors.New("bind down"),
		cleanupErr: errors.New("cas delete throttled"),
	}
	ch, mappings := newTestChannel(dep, &fakeMaterial{certPEM: testCertPEM, keyPEM: []byte(testKeyPEM), keyVersion: 1}, &fakeOldRefs{})

	res, err := ch.Deploy(context.Background(), testCloudCreds(), testCloudTarget(), testFingerprint)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "bind down")
	assert.ErrorContains(t, err, "compensation incomplete", "补偿未竟不得吞错")
	assert.True(t, res.OrphanCandidate)

	got, findErr := mappings.FindByCloudCertID(t.Context(), "aliyun", "acc-main", "cloud-cert-10")
	assert.NoError(t, findErr)
	assert.Equal(t, domain.MappingStatusOrphan, got.Status, "映射保持 orphan，5.9 幂等重试兜底")
}

// 映射转 orphan 失败：补偿未竟可见，映射保留 active。
func TestCloudAPIChannelDeployBindFailureMarkOrphanFails(t *testing.T) {
	dep := &fakeCloudDeployer{uploadID: "cloud-cert-11", bindErr: errors.New("bind down")}
	mat := &fakeMaterial{certPEM: testCertPEM, keyPEM: []byte(testKeyPEM), keyVersion: 1}
	wrapped := &failingStatusMappingRepo{FakeCloudCertMappingRepo: certtest.NewFakeCloudCertMappingRepo(), failStatus: true}
	ch := NewCloudAPIChannel(wrapped, mat, &fakeOldRefs{})
	assert.NoError(t, ch.RegisterDeployer("aliyun", dep, "cdn"))

	_, err := ch.Deploy(context.Background(), testCloudCreds(), testCloudTarget(), testFingerprint)
	assert.ErrorContains(t, err, "compensation incomplete")

	got, findErr := wrapped.FindByCloudCertID(t.Context(), "aliyun", "acc-main", "cloud-cert-11")
	assert.NoError(t, findErr)
	assert.Equal(t, domain.MappingStatusActive, got.Status, "转 orphan 失败：映射保留 active（未入队）")
	assert.Equal(t, "cleanup:cloud-cert-11", dep.callsSnapshot()[len(dep.callsSnapshot())-1],
		"云侧补偿清理仍尽力执行")
}

// 第一段失败：不写映射、不触第二段。
func TestCloudAPIChannelDeployUploadFailure(t *testing.T) {
	dep := &fakeCloudDeployer{uploadErr: fmt.Errorf("upload: %w", cloudx.ErrCloudRateLimited)}
	ch, mappings := newTestChannel(dep, &fakeMaterial{certPEM: testCertPEM, keyPEM: []byte(testKeyPEM), keyVersion: 1}, &fakeOldRefs{})

	res, err := ch.Deploy(context.Background(), testCloudCreds(), testCloudTarget(), testFingerprint)
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited, "限流哨兵透传（退避重试属 5.4/5.5 部署器）")
	assert.Empty(t, res.NewCloudCertID)
	assert.Equal(t, []string{"upload"}, dep.callsSnapshot(), "第一段失败即止")

	mappingsList, err := mappings.ListByFingerprint(t.Context(), testFingerprint)
	assert.NoError(t, err)
	assert.Empty(t, mappingsList, "第一段失败不写映射")
}

// ---------------------------------------------------------------------
// AC-4：明文零生命周期 + 输入校验
// ---------------------------------------------------------------------

func TestCloudAPIChannelDeployZeroizesPlaintext(t *testing.T) {
	dep := &fakeCloudDeployer{uploadID: "cloud-cert-1", bindErr: errors.New("fail after upload")}
	keyPEM := []byte(testKeyPEM)
	mat := &fakeMaterial{certPEM: testCertPEM, keyPEM: keyPEM, keyVersion: 1}
	ch, _ := newTestChannel(dep, mat, &fakeOldRefs{})

	creds := testCloudCreds()
	secretView := creds.Secret // 调用方视角：与通道值副本共享底层数组
	_, err := ch.Deploy(context.Background(), creds, testCloudTarget(), testFingerprint)
	assert.Error(t, err) // 取失败路径：确保错误链也无泄露

	for i, b := range secretView {
		assert.Zero(t, b, "Credential.Secret byte %d 未清零", i)
	}
	for i, b := range keyPEM {
		assert.Zero(t, b, "材料私钥 keyPEM byte %d 未清零", i)
	}
	assert.NotContains(t, err.Error(), "test-sk-plaintext", "错误不含凭证明文")
	assert.NotContains(t, err.Error(), "fake-key-material", "错误不含私钥明文")
}

func TestCloudAPIDeployInputValidation(t *testing.T) {
	dep := &fakeCloudDeployer{uploadID: "x"}
	mat := &fakeMaterial{certPEM: testCertPEM, keyPEM: []byte(testKeyPEM), keyVersion: 1}
	ch, _ := newTestChannel(dep, mat, &fakeOldRefs{})
	ctx := context.Background()

	// 凭证分支校验失败。
	badCreds := testCloudCreds()
	badCreds.AccessKey = ""
	_, err := ch.Deploy(ctx, badCreds, testCloudTarget(), testFingerprint)
	assert.ErrorIs(t, err, ErrInvalidCredential)

	// 目标分支校验失败（cloud_api 缺 product）。
	_, err = ch.Deploy(ctx, testCloudCreds(), DeployTarget{Channel: "cloud_api", Cloud: "aliyun", AccountKey: "a"}, testFingerprint)
	assert.ErrorIs(t, err, ErrInvalidTarget)

	// 空指纹。
	_, err = ch.Deploy(ctx, testCloudCreds(), testCloudTarget(), "")
	assert.ErrorIs(t, err, ErrInvalidTarget)

	// 未注册云。
	_, err = ch.Deploy(ctx, testCloudCreds(), func() DeployTarget {
		tg := testCloudTarget()
		tg.Cloud = "huawei"
		return tg
	}(), testFingerprint)
	assert.ErrorIs(t, err, ErrDeployerNotRegistered, "discovery-only 云无部署器（5.4/5.5 仅组装 aliyun/tencent）")

	// 材料不可用。
	chBadMat, _ := newTestChannel(dep, &fakeMaterial{err: errors.New("fingerprint_only")}, &fakeOldRefs{})
	_, err = chBadMat.Deploy(ctx, testCloudCreds(), testCloudTarget(), testFingerprint)
	assert.Error(t, err)

	// 装配缺失：mappings/material 为 nil。
	bare := NewCloudAPIChannel(nil, mat, &fakeOldRefs{})
	assert.NoError(t, bare.RegisterDeployer("aliyun", dep, "cdn"))
	_, err = bare.Deploy(ctx, testCloudCreds(), testCloudTarget(), testFingerprint)
	assert.ErrorContains(t, err, "without mapping repository")

	// 全部校验失败路径均不触达部署器。
	assert.Empty(t, dep.callsSnapshot(), "输入校验失败不产生云侧调用")
}

// 引用快照读取失败：部署中止（OldCloudCertID 不可得则不盲跑）。
func TestCloudAPIDeployOldRefSourceFails(t *testing.T) {
	dep := &fakeCloudDeployer{uploadID: "x"}
	ch, _ := newTestChannel(dep, &fakeMaterial{certPEM: testCertPEM, keyPEM: []byte(testKeyPEM), keyVersion: 1},
		&fakeOldRefs{err: errors.New("mongo down")})
	_, err := ch.Deploy(context.Background(), testCloudCreds(), testCloudTarget(), testFingerprint)
	assert.ErrorContains(t, err, "read current reference")
	assert.Empty(t, dep.callsSnapshot())
}

// ---------------------------------------------------------------------
// Rollback
// ---------------------------------------------------------------------

func TestCloudAPIChannelRollbackSuccess(t *testing.T) {
	dep := &fakeCloudDeployer{}
	ch, _ := newTestChannel(dep, &fakeMaterial{}, &fakeOldRefs{})
	oldRef := domain.CertReference{
		CertFingerprint:       strings.Repeat("9", 64),
		Cloud:                 domain.CloudAliyun,
		Product:               domain.ProductCDN,
		ResourceID:            "www.example.com",
		ReferencedCloudCertID: "old-cert-1",
	}

	res, err := ch.Rollback(context.Background(), testCloudCreds(), testCloudTarget(), oldRef)
	assert.NoError(t, err)
	assert.True(t, res.Success)
	assert.Equal(t, oldRef, res.RestoredRef, "恢复的引用形态含恢复的 cloudCertId")
	assert.Empty(t, res.OrphanCleaned)
	assert.Equal(t, []string{"bind:cdn:www.example.com:old-cert-1"}, dep.callsSnapshot(),
		"回滚=重新绑定旧云证书 ID；目标有效性三判定属 5.8 前置校验")
}

func TestCloudAPIChannelRollbackFailures(t *testing.T) {
	oldRef := domain.CertReference{ReferencedCloudCertID: "old-cert-1"}
	ctx := context.Background()

	// 限流：ErrCode 映射 CLOUD_API_RATELIMITED。
	dep := &fakeCloudDeployer{bindErr: fmt.Errorf("clb bind: %w", cloudx.ErrCloudRateLimited)}
	ch, _ := newTestChannel(dep, &fakeMaterial{}, &fakeOldRefs{})
	res, err := ch.Rollback(ctx, testCloudCreds(), testCloudTarget(), oldRef)
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited)
	assert.False(t, res.Success)
	assert.Equal(t, ErrCodeCloudRateLimited, res.ErrCode)
	assert.NotContains(t, res.Reason, "test-sk-plaintext")

	// 一般失败：ErrCode 空、Reason 为安全详情。
	dep2 := &fakeCloudDeployer{bindErr: errors.New("listener gone")}
	ch2, _ := newTestChannel(dep2, &fakeMaterial{}, &fakeOldRefs{})
	res, err = ch2.Rollback(ctx, testCloudCreds(), testCloudTarget(), oldRef)
	assert.Error(t, err)
	assert.False(t, res.Success)
	assert.Empty(t, res.ErrCode)
	assert.Contains(t, res.Reason, "listener gone")

	// 空 oldRef 云证书 ID：调用方误用显式报错。
	dep3 := &fakeCloudDeployer{}
	ch3, _ := newTestChannel(dep3, &fakeMaterial{}, &fakeOldRefs{})
	_, err = ch3.Rollback(ctx, testCloudCreds(), testCloudTarget(), domain.CertReference{})
	assert.ErrorIs(t, err, ErrInvalidTarget)
	assert.Empty(t, dep3.callsSnapshot())
}

// ---------------------------------------------------------------------
// Discover
// ---------------------------------------------------------------------

func TestCloudAPIChannelDiscover(t *testing.T) {
	dep := &fakeCloudDeployer{listRefs: map[string][]domain.CertReference{
		"cdn": {
			{ResourceID: "www.example.com", ReferencedCloudCertID: "c-1", CertFingerprint: strings.Repeat("a", 64)},
			{ResourceID: "", ReferencedCloudCertID: "c-2"},             // 无法定位资源：过滤
			{ResourceID: "api.example.com", ReferencedCloudCertID: ""}, // 无证书关联：过滤
		},
		"dcdn": {
			{ResourceID: "m.example.com", ReferencedCloudCertID: "c-3", CertFingerprint: strings.Repeat("b", 64)},
		},
	}}
	ch, _ := newTestChannel(dep, &fakeMaterial{}, &fakeOldRefs{})
	ctx := context.Background()

	// 空产品范围 = 注册的全部已支持产品；snapshotId/cloud/product 回写。
	refs, err := ch.Discover(ctx, testCloudCreds(), DiscoverScope{SnapshotID: "snap-9"})
	assert.NoError(t, err)
	assert.Len(t, refs, 2, "过滤无法定位/无证书关联项")
	for _, r := range refs {
		assert.Equal(t, "snap-9", r.SnapshotID)
		assert.Equal(t, domain.CloudAliyun, r.Cloud)
		assert.Contains(t, []domain.Product{domain.ProductCDN, domain.ProductDCDN}, r.Product)
	}

	// 显式产品范围。
	refs, err = ch.Discover(ctx, testCloudCreds(), DiscoverScope{SnapshotID: "snap-9", Products: []string{"dcdn"}})
	assert.NoError(t, err)
	assert.Len(t, refs, 1)
	assert.Equal(t, "m.example.com", refs[0].ResourceID)

	// scope.Clouds 不含凭证归属云：空结果（该账号不在本轮范围）。
	refs, err = ch.Discover(ctx, testCloudCreds(), DiscoverScope{SnapshotID: "snap-9", Clouds: []string{"tencent"}})
	assert.NoError(t, err)
	assert.Empty(t, refs)

	// scope.Clouds 含归属云：正常发现。
	refs, err = ch.Discover(ctx, testCloudCreds(), DiscoverScope{SnapshotID: "snap-9", Clouds: []string{"tencent", "aliyun"}})
	assert.NoError(t, err)
	assert.Len(t, refs, 2)

	// 缺 SnapshotID：显式拒绝。
	_, err = ch.Discover(ctx, testCloudCreds(), DiscoverScope{})
	assert.ErrorIs(t, err, ErrInvalidScope)

	// 发现失败：整体失败（部分失败聚合属 3.5 扫描编排）。
	depFail := &fakeCloudDeployer{listErr: fmt.Errorf("list: %w", cloudx.ErrCloudRateLimited)}
	chFail, _ := newTestChannel(depFail, &fakeMaterial{}, &fakeOldRefs{})
	_, err = chFail.Discover(ctx, testCloudCreds(), DiscoverScope{SnapshotID: "snap-9"})
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited)

	// 未注册云。
	chNone := NewCloudAPIChannel(nil, nil, nil)
	_, err = chNone.Discover(ctx, testCloudCreds(), DiscoverScope{SnapshotID: "snap-9"})
	assert.ErrorIs(t, err, ErrDeployerNotRegistered)
}

// ---------------------------------------------------------------------
// 生产来源实现
// ---------------------------------------------------------------------

func TestLedgerMaterialSource(t *testing.T) {
	certs := certtest.NewFakeCertificateRepo()
	crypto := certtest.NewTestCrypto(t)
	ctx := context.Background()

	bundle := certtest.NewBundle(t, "mat.example.com", nil, nil)
	ciphertext, keyVersion, err := crypto.Encrypt(bundle.KeyPEM)
	assert.NoError(t, err)
	assert.NoError(t, certs.Create(ctx, &domain.Certificate{
		Fingerprint:         bundle.Fingerprint,
		HostingStatus:       domain.HostingStatusComplete,
		CertPEM:             string(bundle.CertPEM),
		EncryptedPrivateKey: &domain.EncryptedSecret{Ciphertext: ciphertext, KeyVersion: keyVersion, Algo: domain.AlgoAES256GCM},
	}))

	src := NewLedgerMaterialSource(certs, crypto)
	certPEM, keyPEM, kv, err := src.Material(ctx, bundle.Fingerprint)
	assert.NoError(t, err)
	assert.Equal(t, string(bundle.CertPEM), certPEM)
	assert.Equal(t, string(bundle.KeyPEM), string(keyPEM))
	assert.Equal(t, keyVersion, kv)

	// 仅指纹登记（无私钥）：不可执行（5.2 清单已阻断，此处防御）。
	fpOnly := strings.Repeat("c", 64)
	assert.NoError(t, certs.Create(ctx, &domain.Certificate{Fingerprint: fpOnly, HostingStatus: domain.HostingStatusFingerprintOnly}))
	_, _, _, err = src.Material(ctx, fpOnly)
	assert.ErrorIs(t, err, ErrCertMaterialUnavailable)

	// 未登记指纹。
	_, _, _, err = src.Material(ctx, strings.Repeat("d", 64))
	assert.ErrorIs(t, err, ErrCertMaterialUnavailable)
}

func TestSnapshotOldRefSource(t *testing.T) {
	snaps := certtest.NewFakeScanSnapshotRepo()
	refsRepo := certtest.NewFakeCertReferenceRepo()
	ctx := context.Background()

	// 无成功快照：无已知引用（首次部署语义）。
	src := NewSnapshotOldRefSource(snaps, refsRepo)
	_, found, err := src.CurrentRef(ctx, "aliyun", "cdn", "www.example.com")
	assert.NoError(t, err)
	assert.False(t, found)

	// 写入快照（done）与引用，跨账号同资源以 scannedAt 最新为准。
	snapID, err := snaps.Create(ctx, &domain.ScanSnapshot{Status: domain.ScanStatusRunning})
	assert.NoError(t, err)
	assert.NoError(t, snaps.FinishScan(ctx, snapID, domain.ScanStatusDone, "", nil, nil))
	older := domain.CertReference{
		Cloud: domain.CloudAliyun, Product: domain.ProductCDN,
		ResourceID: "www.example.com", ReferencedCloudCertID: "old-a", AccountKey: "acc-a",
		SnapshotID: snapID, ScannedAt: time.Now().Add(-time.Hour),
	}
	newer := older
	newer.ReferencedCloudCertID = "old-b"
	newer.AccountKey = "acc-b"
	newer.ScannedAt = time.Now()
	_, err = refsRepo.CreateMulti(ctx, []domain.CertReference{older, newer,
		{Cloud: domain.CloudTencent, Product: domain.ProductCDN, ResourceID: "www.example.com", ReferencedCloudCertID: "x", SnapshotID: snapID}})
	assert.NoError(t, err)

	got, found, err := src.CurrentRef(ctx, "aliyun", "cdn", "www.example.com")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "old-b", got.ReferencedCloudCertID, "cloud+product+resourceId 精确匹配，取最新扫描")

	// 快照内无该资源引用。
	_, found, err = src.CurrentRef(ctx, "aliyun", "waf", "www.example.com")
	assert.NoError(t, err)
	assert.False(t, found)
}

func TestSnapshotOldRefSourceScopedIDCompat(t *testing.T) {
	snaps := certtest.NewFakeScanSnapshotRepo()
	refsRepo := certtest.NewFakeCertReferenceRepo()
	ctx := context.Background()

	snapID, err := snaps.Create(ctx, &domain.ScanSnapshot{Status: domain.ScanStatusRunning})
	assert.NoError(t, err)
	assert.NoError(t, snaps.FinishScan(ctx, snapID, domain.ScanStatusDone, "", nil, nil))
	// 新快照为复合形态（升级后扫描产物）
	_, err = refsRepo.CreateMulti(ctx, []domain.CertReference{{
		Cloud: domain.CloudAliyun, Product: domain.ProductALB,
		ResourceID: "alb-9/lsn-target", ReferencedCloudCertID: "old-comp", SnapshotID: snapID,
	}})
	assert.NoError(t, err)

	src := NewSnapshotOldRefSource(snaps, refsRepo)

	// 精确匹配仍优先
	got, found, err := src.CurrentRef(ctx, "aliyun", "alb", "alb-9/lsn-target")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "old-comp", got.ReferencedCloudCertID)

	// 升级窗口期：存量变更单持纯监听 ID -> 尾段回退命中复合形态引用
	got, found, err = src.CurrentRef(ctx, "aliyun", "alb", "lsn-target")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "old-comp", got.ReferencedCloudCertID)

	// 反向：目标复合、快照纯监听（旧快照未重扫）
	refsRepo2 := certtest.NewFakeCertReferenceRepo()
	_, err = refsRepo2.CreateMulti(ctx, []domain.CertReference{{
		Cloud: domain.CloudAliyun, Product: domain.ProductNLB,
		ResourceID: "lsn-n-1", ReferencedCloudCertID: "old-plain", SnapshotID: snapID,
	}})
	assert.NoError(t, err)
	src2 := NewSnapshotOldRefSource(snaps, refsRepo2)
	got, found, err = src2.CurrentRef(ctx, "aliyun", "nlb", "nlb-1/lsn-n-1")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "old-plain", got.ReferencedCloudCertID)

	// 尾段不同 -> 不命中
	_, found, err = src.CurrentRef(ctx, "aliyun", "alb", "lsn-other")
	assert.NoError(t, err)
	assert.False(t, found)
}
