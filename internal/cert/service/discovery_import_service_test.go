package service

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	aliyuncert "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/aliyun"
	awsdiscover "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/aws"
	azurediscover "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/azure"
	huaweidiscover "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/huawei"
	tencentcert "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/tencent"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// ---------------------------------------------------------------------
// 测试基建（cert-cloud-discovery-import 任务 4）
// ---------------------------------------------------------------------

// discoveryAccountSourceStub 发现导入测试账号源：per-cloud 账号表 + 可注入错误
// （命名区别于 reference_scan_service_test 的 fakeAccountRepo，坑 5 防撞）。
type discoveryAccountSourceStub struct {
	accounts map[domain.Cloud][]*sharedomain.CloudAccount
	errCloud map[domain.Cloud]error
}

func (f *discoveryAccountSourceStub) ActiveByCloud(_ context.Context, cloud domain.Cloud) ([]*sharedomain.CloudAccount, error) {
	if err, ok := f.errCloud[cloud]; ok {
		return nil, err
	}
	return f.accounts[cloud], nil
}

// discoveryCertAdapterStub 单云材料端口桩：certID→材料/错误映射、调用计数
// （支持性预检"不调云 API"断言）与 panic 注入。
type discoveryCertAdapterStub struct {
	cloud    domain.Cloud
	mu       sync.Mutex
	material map[string]DiscoveryCertMaterial
	errs     map[string]error
	panicOn  string
	calls    map[string]int
}

func newDiscoveryCertAdapterStub(cloud domain.Cloud) *discoveryCertAdapterStub {
	return &discoveryCertAdapterStub{
		cloud:    cloud,
		material: map[string]DiscoveryCertMaterial{},
		errs:     map[string]error{},
		calls:    map[string]int{},
	}
}

func (a *discoveryCertAdapterStub) Cloud() domain.Cloud { return a.cloud }

func (a *discoveryCertAdapterStub) GetCertChain(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) (DiscoveryCertMaterial, error) {
	a.mu.Lock()
	a.calls[cloudCertID]++
	a.mu.Unlock()
	if a.panicOn == cloudCertID {
		panic("cloud adapter exploded: SECRET-PANIC-VALUE")
	}
	if err, ok := a.errs[cloudCertID]; ok {
		return DiscoveryCertMaterial{}, err
	}
	if m, ok := a.material[cloudCertID]; ok {
		return m, nil
	}
	return DiscoveryCertMaterial{Exists: false}, nil
}

// callCount 指定 certID 的调用次数（"不调云 API"断言）。
func (a *discoveryCertAdapterStub) callCount(cloudCertID string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls[cloudCertID]
}

// discoveryImportDeps 发现导入服务测试依赖（内存假实现 + 桩句柄）。
type discoveryImportDeps struct {
	sessions *certtest.FakeDiscoveryImportSessionRepo
	certs    *certtest.FakeCertificateRepo
	mappings *certtest.FakeCloudCertMappingRepo
	refs     *certtest.FakeCertReferenceRepo
	accounts *discoveryAccountSourceStub
	aliyun   *discoveryCertAdapterStub
	tencent  *discoveryCertAdapterStub
	aws      *discoveryCertAdapterStub
}

func newDiscoveryImportDeps() *discoveryImportDeps {
	d := &discoveryImportDeps{
		sessions: certtest.NewFakeDiscoveryImportSessionRepo(),
		certs:    certtest.NewFakeCertificateRepo(),
		mappings: certtest.NewFakeCloudCertMappingRepo(),
		refs:     certtest.NewFakeCertReferenceRepo(),
		accounts: &discoveryAccountSourceStub{
			accounts: map[domain.Cloud][]*sharedomain.CloudAccount{},
			errCloud: map[domain.Cloud]error{},
		},
		aliyun:  newDiscoveryCertAdapterStub(domain.CloudAliyun),
		tencent: newDiscoveryCertAdapterStub(domain.CloudTencent),
		aws:     newDiscoveryCertAdapterStub(domain.CloudAWS),
	}
	d.accounts.accounts[domain.CloudAliyun] = []*sharedomain.CloudAccount{
		{Name: "acct-a", Provider: sharedomain.CloudProvider(domain.CloudAliyun), Status: sharedomain.CloudAccountStatusActive},
		{Name: "acct-b", Provider: sharedomain.CloudProvider(domain.CloudAliyun), Status: sharedomain.CloudAccountStatusActive},
	}
	d.accounts.accounts[domain.CloudTencent] = []*sharedomain.CloudAccount{
		{Name: "acct-tx", Provider: sharedomain.CloudProvider(domain.CloudTencent), Status: sharedomain.CloudAccountStatusActive},
	}
	d.accounts.accounts[domain.CloudAWS] = []*sharedomain.CloudAccount{
		{Name: "acct-aws", Provider: sharedomain.CloudProvider(domain.CloudAWS), Status: sharedomain.CloudAccountStatusActive},
	}
	return d
}

func (d *discoveryImportDeps) svc() DiscoveryImportService {
	return NewDiscoveryImportService(d.sessions, d.certs, d.mappings, d.refs,
		[]DiscoveryCertAdapter{d.aliyun, d.tencent, d.aws}, d.accounts)
}

// waitForDiscoveryTerminal 轮询会话直至终态（completed/partial_failed），超时失败。
func waitForDiscoveryTerminal(t *testing.T, sessions *certtest.FakeDiscoveryImportSessionRepo, sessionID string) domain.DiscoveryImportSession {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		sess, err := sessions.GetByID(context.Background(), sessionID)
		require.NoError(t, err)
		if sess.Status != domain.DiscoveryImportRunning {
			return sess
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("discovery import session did not reach terminal state within deadline")
	return domain.DiscoveryImportSession{}
}

// difp 互异 64 位 hex 指纹（与 dfp/lfp 同构，本文件独立）。
func difp(i int) string { return fmt.Sprintf("cc%04x%058x", i, i) }

// pemBlockTypes 逐块解码 PEM 序列，返回 (类型列表, 首块 DER 的 SHA256 hex)。
func pemBlockTypes(t *testing.T, s string) ([]string, string) {
	t.Helper()
	var types []string
	var first string
	rest := []byte(s)
	for {
		block, remaining := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = remaining
		types = append(types, block.Type)
		if first == "" {
			sum := sha256.Sum256(block.Bytes)
			first = hex.EncodeToString(sum[:])
		}
	}
	return types, first
}

// seedPlaceholderRef 播种占位指纹引用（快照内引用形态）。
func (d *discoveryImportDeps) seedPlaceholderRef(t *testing.T, cloud, accountKey, cloudCertID, snapshotID string) {
	t.Helper()
	_, err := d.refs.CreateMulti(context.Background(), []domain.CertReference{{
		CertFingerprint:       placeholderFingerprintFor(cloud, accountKey, cloudCertID),
		Cloud:                 domain.Cloud(cloud),
		Product:               domain.ProductCDN,
		ResourceID:            "res-" + cloudCertID,
		ReferencedCloudCertID: cloudCertID,
		AccountKey:            accountKey,
		SnapshotID:            snapshotID,
	}})
	require.NoError(t, err)
}

// ---------------------------------------------------------------------
// SC-4：逐条处理链 + 单条失败不中断 + 静态文案 + 会话先持久化再异步
// ---------------------------------------------------------------------

func TestDiscoveryImport_SC4_MixedResultsAndIsolation(t *testing.T) {
	d := newDiscoveryImportDeps()
	okA := certtest.NewBundle(t, "www.a-example.com", []string{"www.a-example.com"}, nil)
	okB := certtest.NewBundle(t, "www.b-example.com", []string{"www.b-example.com"}, nil)
	d.aliyun.material["cert-ok-a"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(okA.CertPEM)}
	d.aliyun.material["cert-ok-b"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(okB.CertPEM)}
	// 云侧错误注入（错误文本含"云响应片段"标记，断言不外泄）
	d.aliyun.errs["cert-err"] = errors.New("aliyun sdk 500: SECRET-RESPONSE-FRAGMENT")
	// 云侧已删除 + 在库但无 PEM
	d.aliyun.material["cert-gone"] = DiscoveryCertMaterial{Exists: false}
	d.aliyun.material["cert-nopem"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: ""}

	sessionID, err := d.svc().ImportFromDiscovery(context.Background(), []DiscoveryImportItemInput{
		{Cloud: "aliyun", AccountKey: "acct-a", CloudCertID: "cert-ok-a"},
		{Cloud: "aliyun", AccountKey: "acct-a", CloudCertID: "cert-err"},
		{Cloud: "aliyun", AccountKey: "acct-a", CloudCertID: "cert-gone"},
		{Cloud: "aliyun", AccountKey: "acct-a", CloudCertID: "cert-nopem"},
		{Cloud: "aliyun", AccountKey: "acct-a", CloudCertID: "cert-ok-b"},
	}, "op-1")
	require.NoError(t, err)
	require.NotEmpty(t, sessionID, "会话先持久化：Create 返回即有 ID")
	// 持久化可见（异步执行前/后均存在）
	_, err = d.sessions.GetByID(context.Background(), sessionID)
	require.NoError(t, err)

	sess := waitForDiscoveryTerminal(t, d.sessions, sessionID)
	assert.Equal(t, "op-1", sess.Operator)
	assert.Equal(t, domain.DiscoveryImportPartialFailed, sess.Status, "存在失败条目 → partial_failed")
	require.NotNil(t, sess.FinishedAt)
	assert.Equal(t, domain.DiscoveryImportProgress{Total: 5, Succeeded: 2, Failed: 3}, sess.Progress)

	// 条目序与请求一致
	wantCloudIDs := []string{"cert-ok-a", "cert-err", "cert-gone", "cert-nopem", "cert-ok-b"}
	for i, w := range wantCloudIDs {
		assert.Equal(t, w, sess.Items[i].CloudCertID, "item %d", i)
	}

	// 成功条目：台账登记 + 映射建档
	assert.Equal(t, domain.DiscoveryItemSuccess, sess.Items[0].Result)
	assert.NotEmpty(t, sess.Items[0].MappedCertID)
	assert.Empty(t, sess.Items[0].ErrorReason, "首次导入成功条目无说明文案")
	assert.Equal(t, domain.DiscoveryItemSuccess, sess.Items[4].Result)

	// 失败条目：静态文案（不携带云响应片段），互不中断
	assert.Equal(t, domain.DiscoveryItemFailed, sess.Items[1].Result)
	assert.Equal(t, reasonDiscoveryGetCertFailed, sess.Items[1].ErrorReason)
	assert.NotContains(t, sess.Items[1].ErrorReason, "SECRET-RESPONSE-FRAGMENT")
	assert.Equal(t, domain.DiscoveryItemFailed, sess.Items[2].Result)
	assert.Equal(t, reasonDiscoveryCertGone, sess.Items[2].ErrorReason)
	assert.Equal(t, domain.DiscoveryItemFailed, sess.Items[3].Result)
	assert.Equal(t, reasonDiscoveryNoPEM, sess.Items[3].ErrorReason)

	// 成功条目台账内容（SC-9 详见独立用例）
	stored, err := d.certs.GetByFingerprint(context.Background(), okA.Fingerprint)
	require.NoError(t, err)
	assert.Equal(t, domain.HostingStatusFingerprintOnly, stored.HostingStatus)
	assert.Equal(t, "www.a-example.com", stored.CommonName)
	assert.Equal(t, okA.Fingerprint, stored.Fingerprint)
	assert.Nil(t, stored.EncryptedPrivateKey, "fingerprint_only：无私钥密文")

	m, err := d.mappings.FindByCloudCertID(context.Background(), "aliyun", "acct-a", "cert-ok-a")
	require.NoError(t, err)
	assert.Equal(t, okA.Fingerprint, m.CertFingerprint)
	assert.Equal(t, domain.MappingStatusActive, m.Status)
}

// ---------------------------------------------------------------------
// SC-9：内容级断言——净化材料入台账，私钥不落库
// ---------------------------------------------------------------------

func TestDiscoveryImport_SC9_PrivateKeyNeverPersisted(t *testing.T) {
	d := newDiscoveryImportDeps()
	b := certtest.NewBundle(t, "www.sc9-example.com", []string{"www.sc9-example.com"}, nil)
	// 云侧返回混入非 CERTIFICATE 块（合法 base64 的伪 PRIVATE KEY 块 +
	// 证书束）——服务边界二次净化后仅 CERTIFICATE 块入台账
	garbage := "-----BEGIN PRIVATE KEY-----\nZ2FyYmFnZQ==\n-----END PRIVATE KEY-----\n"
	d.aliyun.material["cert-sc9"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: garbage + string(b.CertPEM)}

	sessionID, err := d.svc().ImportFromDiscovery(context.Background(), []DiscoveryImportItemInput{
		{Cloud: "aliyun", AccountKey: "acct-a", CloudCertID: "cert-sc9"},
	}, "op-1")
	require.NoError(t, err)
	sess := waitForDiscoveryTerminal(t, d.sessions, sessionID)
	require.Equal(t, domain.DiscoveryImportCompleted, sess.Status)

	stored, err := d.certs.GetByFingerprint(context.Background(), b.Fingerprint)
	require.NoError(t, err, "叶证书在前，解析成功")
	assert.NotContains(t, stored.CertPEM, "PRIVATE KEY", "私钥不落库（内容级断言）")
	types, firstLeaf := pemBlockTypes(t, stored.CertPEM)
	assert.Equal(t, []string{"CERTIFICATE", "CERTIFICATE", "CERTIFICATE"}, types, "含且仅含 CERTIFICATE 块")
	assert.Equal(t, b.Fingerprint, firstLeaf, "叶在前（首块即 leaf，fullchain 口径）")
	assert.Equal(t, domain.HostingStatusFingerprintOnly, stored.HostingStatus)
	assert.Nil(t, stored.EncryptedPrivateKey)
}

// ---------------------------------------------------------------------
// SC-5：同指纹重放幂等（不重复台账 + 补建映射 + success 说明 + 回填）
// ---------------------------------------------------------------------

func TestDiscoveryImport_SC5_DuplicateFingerprintReplay(t *testing.T) {
	d := newDiscoveryImportDeps()
	b := certtest.NewBundle(t, "www.replay-example.com", []string{"www.replay-example.com"}, nil)
	// 台账已有同指纹证书 + 扫描侧留有占位指纹引用（腾讯 SHA-1 口径回退样本）
	existing := &domain.Certificate{
		Fingerprint:   b.Fingerprint,
		CommonName:    "www.replay-example.com",
		NotAfter:      time.Now().Add(8760 * time.Hour),
		CertPEM:       string(b.CertPEM),
		HostingStatus: domain.HostingStatusFingerprintOnly,
	}
	require.NoError(t, d.certs.Create(context.Background(), existing))
	d.seedPlaceholderRef(t, "tencent", "acct-tx", "ssl-9", "snap-old")
	d.tencent.material["ssl-9"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)}

	sessionID, err := d.svc().ImportFromDiscovery(context.Background(), []DiscoveryImportItemInput{
		{Cloud: "tencent", AccountKey: "acct-tx", CloudCertID: "ssl-9"},
	}, "op-1")
	require.NoError(t, err)
	sess := waitForDiscoveryTerminal(t, d.sessions, sessionID)

	// 条目记 success + 说明 + 既有证书 ID；不产生重复台账记录
	assert.Equal(t, domain.DiscoveryImportCompleted, sess.Status, "重放补建映射不构成失败")
	require.Equal(t, domain.DiscoveryItemSuccess, sess.Items[0].Result)
	assert.Equal(t, existing.ID.Hex(), sess.Items[0].MappedCertID)
	assert.Equal(t, reasonDiscoveryAlreadyInLedger, sess.Items[0].ErrorReason)
	ledger, err := d.certs.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, ledger, 1, "uk_fingerprint：同指纹重放仅 1 条台账记录")

	// 本云本账号映射补建
	m, err := d.mappings.FindByCloudCertID(context.Background(), "tencent", "acct-tx", "ssl-9")
	require.NoError(t, err)
	assert.Equal(t, b.Fingerprint, m.CertFingerprint)

	// 幂等重放路径同样触发占位引用回填（可恢复性）
	refs, err := d.refs.ListByFingerprint(context.Background(), b.Fingerprint)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, "ssl-9", refs[0].ReferencedCloudCertID)
}

// ---------------------------------------------------------------------
// SC-6：多账号同证书——1 台账记录 + 映射按账号各 1 条
// ---------------------------------------------------------------------

func TestDiscoveryImport_SC6_MultiAccountOneLedgerPerAccountMappings(t *testing.T) {
	d := newDiscoveryImportDeps()
	b := certtest.NewBundle(t, "www.shared-example.com", []string{"www.shared-example.com"}, nil)
	d.aliyun.material["cert-acc-a"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)}
	d.aliyun.material["cert-acc-b"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)}

	sessionID, err := d.svc().ImportFromDiscovery(context.Background(), []DiscoveryImportItemInput{
		{Cloud: "aliyun", AccountKey: "acct-a", CloudCertID: "cert-acc-a"},
		{Cloud: "aliyun", AccountKey: "acct-b", CloudCertID: "cert-acc-b"},
	}, "op-1")
	require.NoError(t, err)
	sess := waitForDiscoveryTerminal(t, d.sessions, sessionID)
	assert.Equal(t, domain.DiscoveryImportCompleted, sess.Status)
	require.Len(t, sess.Items, 2)

	ledger, err := d.certs.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, ledger, 1, "同证书多账号仅 1 条台账记录")

	mappings, err := d.mappings.ListByFingerprint(context.Background(), b.Fingerprint)
	require.NoError(t, err)
	require.Len(t, mappings, 2, "映射按账号各 1 条")
	assert.ElementsMatch(t, []string{"acct-a", "acct-b"}, []string{mappings[0].AccountKey, mappings[1].AccountKey})
	assert.ElementsMatch(t, []string{"cert-acc-a", "cert-acc-b"}, []string{mappings[0].CloudCertID, mappings[1].CloudCertID})

	// 第二账号条目为幂等重放语义（说明文案），首条目为首次登记
	assert.Equal(t, domain.DiscoveryItemSuccess, sess.Items[0].Result)
	assert.Equal(t, domain.DiscoveryItemSuccess, sess.Items[1].Result)
	assert.Equal(t, reasonDiscoveryAlreadyInLedger, sess.Items[1].ErrorReason)
}

// ---------------------------------------------------------------------
// SC-6：占位指纹引用批量回填（腾讯 SHA-1 回退样本；真实指纹永不被覆盖）
// ---------------------------------------------------------------------

func TestDiscoveryImport_SC6_PlaceholderBackfill(t *testing.T) {
	d := newDiscoveryImportDeps()
	b := certtest.NewBundle(t, "www.tx-example.com", []string{"www.tx-example.com"}, nil)
	// 腾讯 SHA-1 口径回退样本：扫描时无法解析 → 占位指纹引用
	d.seedPlaceholderRef(t, "tencent", "acct-tx", "ssl-9", "snap-1")
	// 真实指纹引用（其他证书扫描解析成功）——永不被回填覆盖
	_, err := d.refs.CreateMulti(context.Background(), []domain.CertReference{{
		CertFingerprint: difp(1), Cloud: domain.CloudTencent, Product: domain.ProductCDN,
		ResourceID: "res-real", ReferencedCloudCertID: "ssl-9", AccountKey: "acct-tx", SnapshotID: "snap-1",
	}})
	require.NoError(t, err)
	// 相邻占位引用：不同 cloudCertId / 不同 accountKey——不在回填范围
	d.seedPlaceholderRef(t, "tencent", "acct-tx", "ssl-other", "snap-1")
	d.seedPlaceholderRef(t, "tencent", "acct-tx2", "ssl-9", "snap-1")

	d.tencent.material["ssl-9"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)}
	sessionID, err := d.svc().ImportFromDiscovery(context.Background(), []DiscoveryImportItemInput{
		{Cloud: "tencent", AccountKey: "acct-tx", CloudCertID: "ssl-9"},
	}, "op-1")
	require.NoError(t, err)
	sess := waitForDiscoveryTerminal(t, d.sessions, sessionID)
	require.Equal(t, domain.DiscoveryImportCompleted, sess.Status)

	// 占位引用回填为真实指纹（台账详情引用列表非空）
	refs, err := d.refs.ListByFingerprint(context.Background(), b.Fingerprint)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, "res-ssl-9", refs[0].ResourceID, "回填的是占位引用（seedPlaceholderRef 派生 ResourceID）")
	refs, err = d.refs.ListByFingerprint(context.Background(), placeholderFingerprintFor("tencent", "acct-tx", "ssl-9"))
	require.NoError(t, err)
	assert.Empty(t, refs, "ssl-9@acct-tx 占位指纹引用已全部回填")

	// 真实指纹引用不被覆盖
	refs, err = d.refs.ListByFingerprint(context.Background(), difp(1))
	require.NoError(t, err)
	require.Len(t, refs, 1, "真实指纹引用保持原指纹（永不被回填覆盖）")
	// 相邻占位引用不受影响
	refs, err = d.refs.ListByFingerprint(context.Background(), placeholderFingerprintFor("tencent", "acct-tx", "ssl-other"))
	require.NoError(t, err)
	assert.Len(t, refs, 1, "不同 cloudCertId 的占位引用不回填")
	refs, err = d.refs.ListByFingerprint(context.Background(), placeholderFingerprintFor("tencent", "acct-tx2", "ssl-9"))
	require.NoError(t, err)
	assert.Len(t, refs, 1, "不同 accountKey 的占位引用不回填")
}

// ---------------------------------------------------------------------
// 华为云/IAM-hosted 记因跳过（不调云 API）+ 降级哨兵兜底
// ---------------------------------------------------------------------

func TestDiscoveryImport_SkipUnsupportedWithoutCloudAPI(t *testing.T) {
	d := newDiscoveryImportDeps()
	arn := "arn:aws:acm:us-east-1:123456789012:certificate/abc-def"
	d.aws.errs[arn] = cloudx.ErrCertPEMUnsupported // 预检外形态的防御性兜底

	sessionID, err := d.svc().ImportFromDiscovery(context.Background(), []DiscoveryImportItemInput{
		{Cloud: "huawei", AccountKey: "acct-hw", CloudCertID: "cert-H"},
		{Cloud: "aws", AccountKey: "acct-aws", CloudCertID: "iam-cert-123"},
		{Cloud: "aws", AccountKey: "acct-aws", CloudCertID: arn},
	}, "op-1")
	require.NoError(t, err)
	sess := waitForDiscoveryTerminal(t, d.sessions, sessionID)

	assert.Equal(t, domain.DiscoveryImportPartialFailed, sess.Status)
	assert.Equal(t, domain.DiscoveryItemFailed, sess.Items[0].Result)
	assert.Equal(t, reasonDiscoveryUnsupportedCloud, sess.Items[0].ErrorReason)
	assert.Equal(t, domain.DiscoveryItemFailed, sess.Items[1].Result)
	assert.Equal(t, reasonDiscoveryIAMHosted, sess.Items[1].ErrorReason)
	assert.Equal(t, domain.DiscoveryItemFailed, sess.Items[2].Result)
	assert.Equal(t, reasonDiscoveryUnsupportedCloud, sess.Items[2].ErrorReason)

	// 不调云 API：华为云未注册适配（结构上不可达）；AWS 仅对 ARN 形态发起一次
	//（该次返回降级哨兵后记因，不再重试）
	assert.Equal(t, 0, d.aws.callCount("iam-cert-123"), "IAM-hosted 预检跳过：不调云 API")
	assert.Equal(t, 1, d.aws.callCount(arn))
}

// ---------------------------------------------------------------------
// 单条 panic 由 recover 兜底（静态文案，不中断会话）
// ---------------------------------------------------------------------

func TestDiscoveryImport_PanicIsolation(t *testing.T) {
	d := newDiscoveryImportDeps()
	okA := certtest.NewBundle(t, "www.panic-a-example.com", []string{"www.panic-a-example.com"}, nil)
	okB := certtest.NewBundle(t, "www.panic-b-example.com", []string{"www.panic-b-example.com"}, nil)
	d.aliyun.material["cert-ok-a"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(okA.CertPEM)}
	d.aliyun.material["cert-boom"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(okB.CertPEM)}
	d.aliyun.material["cert-ok-b"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(okB.CertPEM)}
	d.aliyun.panicOn = "cert-boom"

	sessionID, err := d.svc().ImportFromDiscovery(context.Background(), []DiscoveryImportItemInput{
		{Cloud: "aliyun", AccountKey: "acct-a", CloudCertID: "cert-ok-a"},
		{Cloud: "aliyun", AccountKey: "acct-a", CloudCertID: "cert-boom"},
		{Cloud: "aliyun", AccountKey: "acct-b", CloudCertID: "cert-ok-b"},
	}, "op-1")
	require.NoError(t, err)
	sess := waitForDiscoveryTerminal(t, d.sessions, sessionID)

	assert.Equal(t, domain.DiscoveryImportPartialFailed, sess.Status)
	assert.Equal(t, domain.DiscoveryImportProgress{Total: 3, Succeeded: 2, Failed: 1}, sess.Progress)
	assert.Equal(t, domain.DiscoveryItemFailed, sess.Items[1].Result)
	assert.Equal(t, reasonDiscoveryPanic, sess.Items[1].ErrorReason)
	assert.NotContains(t, sess.Items[1].ErrorReason, "SECRET-PANIC-VALUE", "panic 值不外泄")
	// 邻近条目不受影响（panic 不中断会话）
	assert.Equal(t, domain.DiscoveryItemSuccess, sess.Items[0].Result)
	assert.Equal(t, domain.DiscoveryItemSuccess, sess.Items[2].Result)
}

// ---------------------------------------------------------------------
// 云账号凭证解析：账号未命中 / 账号源读取失败
// ---------------------------------------------------------------------

func TestDiscoveryImport_AccountSourcing(t *testing.T) {
	d := newDiscoveryImportDeps()
	b := certtest.NewBundle(t, "www.acct-example.com", []string{"www.acct-example.com"}, nil)
	d.aliyun.material["cert-x"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)}

	// 账号未命中（active 账号表中无该 accountKey）
	sessionID, err := d.svc().ImportFromDiscovery(context.Background(), []DiscoveryImportItemInput{
		{Cloud: "aliyun", AccountKey: "acct-unknown", CloudCertID: "cert-x"},
	}, "op-1")
	require.NoError(t, err)
	sess := waitForDiscoveryTerminal(t, d.sessions, sessionID)
	assert.Equal(t, domain.DiscoveryItemFailed, sess.Items[0].Result)
	assert.Equal(t, reasonDiscoveryAccountMissing, sess.Items[0].ErrorReason)

	// 账号源读取失败（仓储故障）
	d.accounts.errCloud[domain.CloudAliyun] = errors.New("account repo down")
	sessionID, err = d.svc().ImportFromDiscovery(context.Background(), []DiscoveryImportItemInput{
		{Cloud: "aliyun", AccountKey: "acct-a", CloudCertID: "cert-x"},
	}, "op-1")
	require.NoError(t, err)
	sess = waitForDiscoveryTerminal(t, d.sessions, sessionID)
	assert.Equal(t, domain.DiscoveryItemFailed, sess.Items[0].Result)
	assert.Equal(t, reasonDiscoveryAccountLoadFail, sess.Items[0].ErrorReason)
	assert.NotContains(t, sess.Items[0].ErrorReason, "account repo down", "仓储错误细节不外泄")
	assert.Zero(t, d.aliyun.callCount("cert-x"), "凭证解析失败不调云 API")
}

// ---------------------------------------------------------------------
// 空清单 / 会话查询哨兵
// ---------------------------------------------------------------------

func TestDiscoveryImport_EmptyItemsAndSessionLookup(t *testing.T) {
	d := newDiscoveryImportDeps()
	_, err := d.svc().ImportFromDiscovery(context.Background(), nil, "op-1")
	assert.ErrorIs(t, err, ErrEmptyDiscoveryImport)

	_, err = d.svc().GetSession(context.Background(), primitive.NewObjectID().Hex())
	assert.ErrorIs(t, err, mongo.ErrNoDocuments)
}

// ---------------------------------------------------------------------
// 解析失败记因（域内 CertError 静态文案：已过期样本）
// ---------------------------------------------------------------------

func TestDiscoveryImport_ParseFailReason(t *testing.T) {
	d := newDiscoveryImportDeps()
	expired := certtest.NewBundle(t, "www.expired-example.com",
		[]string{"www.expired-example.com"},
		func(tmpl *x509.Certificate) { // 有效期校验样本：已过期
			tmpl.NotAfter = time.Now().Add(-24 * time.Hour)
			tmpl.NotBefore = time.Now().Add(-48 * time.Hour)
		})
	d.aliyun.material["cert-expired"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(expired.CertPEM)}

	sessionID, err := d.svc().ImportFromDiscovery(context.Background(), []DiscoveryImportItemInput{
		{Cloud: "aliyun", AccountKey: "acct-a", CloudCertID: "cert-expired"},
	}, "op-1")
	require.NoError(t, err)
	sess := waitForDiscoveryTerminal(t, d.sessions, sessionID)
	assert.Equal(t, domain.DiscoveryItemFailed, sess.Items[0].Result)
	assert.Contains(t, sess.Items[0].ErrorReason, domain.CodeCertParseFail, "解析失败承载域内静态码")
	assert.Equal(t, domain.DiscoveryImportPartialFailed, sess.Status)
}

// ---------------------------------------------------------------------
// 生产适配 shim 冒烟：五云 Cloud 归属 + 免网络降级哨兵透传
// ---------------------------------------------------------------------

func TestDiscoveryImport_AdapterShims(t *testing.T) {
	logger := elog.DefaultLogger
	adapters := []DiscoveryCertAdapter{
		NewAliyunDiscoveryCertAdapter(aliyuncert.NewCertAdapter(logger)),
		NewTencentDiscoveryCertAdapter(tencentcert.NewCertAdapter(logger)),
		NewHuaweiDiscoveryCertAdapter(huaweidiscover.NewCertDiscoveryAdapter(logger)),
		NewAwsDiscoveryCertAdapter(awsdiscover.NewCertDiscoveryAdapter(logger)),
		NewAzureDiscoveryCertAdapter(azurediscover.NewCertDiscoveryAdapter(logger)),
	}
	wantClouds := []domain.Cloud{domain.CloudAliyun, domain.CloudTencent, domain.CloudHuawei, domain.CloudAWS, domain.CloudAzure}
	for i, a := range adapters {
		assert.Equal(t, wantClouds[i], a.Cloud())
	}

	creds := &sharedomain.CloudAccount{Name: "acct", Provider: sharedomain.CloudProvider(domain.CloudHuawei)}
	// 华为云：SCM 无 PEM 通道，恒降级哨兵（不发起云 API 调用）
	_, err := adapters[2].GetCertChain(context.Background(), creds, "cert-hw")
	assert.ErrorIs(t, err, cloudx.ErrCertPEMUnsupported)
	// AWS IAM-hosted（非 ARN）：前置校验直接降级哨兵（不发起云 API 调用）
	_, err = adapters[3].GetCertChain(context.Background(), creds, "iam-cert-123")
	assert.ErrorIs(t, err, cloudx.ErrCertPEMUnsupported)
}

func TestDiscoveryParseReasonPreservesWrappedDetail(t *testing.T) {
	t.Run("过期细节保留", func(t *testing.T) {
		err := fmt.Errorf("%w: certificate expired at 2025-01-01T00:00:00Z", domain.ErrParseFail)
		got := discoveryParseReason(err)
		assert.Contains(t, got, "CERT_PARSE_FAIL")
		assert.Contains(t, got, "certificate expired at 2025-01-01T00:00:00Z",
			"wrapped 静态细节（日期/算法名）应保留，运营需据此区分过期/结构异常")
	})
	t.Run("缺自签根细节保留", func(t *testing.T) {
		err := fmt.Errorf("%w: 2 certificate(s) provided without self-signed root anchor", domain.ErrChainIncomplete)
		got := discoveryParseReason(err)
		assert.Contains(t, got, "CERT_CHAIN_INCOMPLETE")
		assert.Contains(t, got, "without self-signed root anchor")
	})
	t.Run("非域错误保持INTERNAL_ERROR静态码", func(t *testing.T) {
		got := discoveryParseReason(errors.New("boom"))
		assert.Equal(t, "INTERNAL_ERROR: 证书解析失败", got)
	})
}
