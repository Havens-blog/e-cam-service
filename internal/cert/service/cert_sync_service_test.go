package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	volcanocert "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/volcano"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------
// 测试基建（cert-volcano-import-sync 任务 3）
// ---------------------------------------------------------------------

// syncListerStub 单云证书库列举桩（同步服务只读端口替身）：实例清单/错误可注入、
// 调用计数与门控（CAS 防重与整体限时测试用）。命名区别于 discovery 侧桩（坑 5 防撞）。
type syncListerStub struct {
	cloud     domain.Cloud
	instances []CertLibraryInstance
	err       error
	gate      chan struct{} // 非空时 ListInstances 阻塞至关闭或 ctx 到期（模拟慢云）
	entered   chan struct{} // 容量 1：首次进入 ListInstances 即发信号（CAS 对齐）

	mu    sync.Mutex
	lists int
}

func newSyncListerStub(cloud domain.Cloud) *syncListerStub {
	return &syncListerStub{cloud: cloud, entered: make(chan struct{}, 1)}
}

func (l *syncListerStub) Cloud() domain.Cloud { return l.cloud }

func (l *syncListerStub) ListInstances(ctx context.Context, _ *sharedomain.CloudAccount) ([]CertLibraryInstance, error) {
	l.mu.Lock()
	l.lists++
	gate := l.gate
	l.mu.Unlock()
	select {
	case l.entered <- struct{}{}:
	default:
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if l.err != nil {
		return nil, l.err
	}
	return append([]CertLibraryInstance(nil), l.instances...), nil
}

// listCount 列举调用次数（只读纪律断言：云侧访问仅 ListInstances）。
func (l *syncListerStub) listCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lists
}

// syncFullCloudAdapter 全量适配器形态（含"写方法"）：同步服务仅经只读窄端口
// 持有（CertLibraryLister 构造性保证），写方法一旦被调用即计数暴露
// （Hard Rule 只读纪律的可执行断言面）。
type syncFullCloudAdapter struct {
	*syncListerStub
	writes atomic.Int32
}

// UploadCert 模拟云写方法（不在只读端口面内；被调用即违规）。
func (a *syncFullCloudAdapter) UploadCert(context.Context, *sharedomain.CloudAccount, string, string, string, string) (string, error) {
	a.writes.Add(1)
	return "cert-written", nil
}

// syncBarrierAdapter 导入材料通道栅栏桩（AC5 并发竞态测试）：双方会话的
// GetCertChain 调用在栅栏处对齐后同时放行，制造台账 Create 竞态窗口。
type syncBarrierAdapter struct {
	cloud    domain.Cloud
	chainPEM string
	wg       *sync.WaitGroup
	release  chan struct{}
	calls    atomic.Int32
}

func (a *syncBarrierAdapter) Cloud() domain.Cloud { return a.cloud }

func (a *syncBarrierAdapter) GetCertChain(_ context.Context, _ *sharedomain.CloudAccount, _ string) (DiscoveryCertMaterial, error) {
	a.calls.Add(1)
	a.wg.Done()
	<-a.release
	return DiscoveryCertMaterial{Exists: true, CertChainPEM: a.chainPEM}, nil
}

// syncDeps 同步服务测试依赖（内存假实现 + 桩句柄；导入管线为真实服务 +
// discovery 侧材料桩，同一实例同时充当同步服务的 importer）。
type syncDeps struct {
	sessions *certtest.FakeDiscoveryImportSessionRepo
	certs    *certtest.FakeCertificateRepo
	mappings *certtest.FakeCloudCertMappingRepo
	refs     *certtest.FakeCertReferenceRepo
	accounts *discoveryAccountSourceStub

	listers map[domain.Cloud]*syncListerStub
	aliyun  *discoveryCertAdapterStub
	tencent *discoveryCertAdapterStub
	volcano *discoveryCertAdapterStub
}

func newSyncDeps() *syncDeps {
	d := &syncDeps{
		sessions: certtest.NewFakeDiscoveryImportSessionRepo(),
		certs:    certtest.NewFakeCertificateRepo(),
		mappings: certtest.NewFakeCloudCertMappingRepo(),
		refs:     certtest.NewFakeCertReferenceRepo(),
		accounts: &discoveryAccountSourceStub{
			accounts: map[domain.Cloud][]*sharedomain.CloudAccount{},
			errCloud: map[domain.Cloud]error{},
		},
		listers: map[domain.Cloud]*syncListerStub{},
		aliyun:  newDiscoveryCertAdapterStub(domain.CloudAliyun),
		tencent: newDiscoveryCertAdapterStub(domain.CloudTencent),
		volcano: newDiscoveryCertAdapterStub(discoveryCloudVolcano),
	}
	d.accounts.accounts[domain.CloudAliyun] = []*sharedomain.CloudAccount{
		{Name: "acct-a", Provider: sharedomain.CloudProvider(domain.CloudAliyun), Status: sharedomain.CloudAccountStatusActive},
	}
	d.accounts.accounts[domain.CloudTencent] = []*sharedomain.CloudAccount{
		{Name: "acct-tx", Provider: sharedomain.CloudProvider(domain.CloudTencent), Status: sharedomain.CloudAccountStatusActive},
	}
	d.accounts.accounts[discoveryCloudVolcano] = []*sharedomain.CloudAccount{
		{Name: "acct-v", Provider: sharedomain.CloudProviderVolcano, Status: sharedomain.CloudAccountStatusActive},
	}
	return d
}

// lister 按云取（懒建）列举桩。
func (d *syncDeps) lister(cloud domain.Cloud) *syncListerStub {
	l, ok := d.listers[cloud]
	if !ok {
		l = newSyncListerStub(cloud)
		d.listers[cloud] = l
	}
	return l
}

// importer 构建导入管线（真实服务；材料桩注册 aliyun/tencent/volcano 三云）。
func (d *syncDeps) importer() DiscoveryImportService {
	return NewDiscoveryImportService(d.sessions, d.certs, d.mappings, d.refs,
		[]DiscoveryCertAdapter{d.aliyun, d.tencent, d.volcano}, d.accounts)
}

// svc 构建同步服务（listers 全量注入）。
func (d *syncDeps) svc() CertSyncService {
	listers := make([]CertLibraryLister, 0, len(d.listers))
	for _, l := range d.listers {
		listers = append(listers, l)
	}
	return NewCertSyncService(listers, d.accounts, d.importer(), d.certs, d.mappings)
}

// seedLedgerCert 预置台账证书（fingerprint_only 形态，仅指纹判定所需字段）。
func (d *syncDeps) seedLedgerCert(t *testing.T, fp, cn string) {
	t.Helper()
	require.NoError(t, d.certs.Create(context.Background(), &domain.Certificate{
		Fingerprint: fp, CommonName: cn, HostingStatus: domain.HostingStatusFingerprintOnly,
	}))
}

// seedMapping 预置映射行（uploadedAt 显式给定，支撑漂移留痕的时序断言）。
func (d *syncDeps) seedMapping(t *testing.T, fp, cloud, accountKey, cloudCertID string, uploadedAt time.Time) {
	t.Helper()
	require.NoError(t, d.mappings.Upsert(context.Background(), &domain.CloudCertMapping{
		CertFingerprint: fp, Cloud: cloud, AccountKey: accountKey,
		CloudCertID: cloudCertID, UploadedAt: uploadedAt, Status: domain.MappingStatusActive,
	}))
}

// hasFailureReason 失败清单是否含指定静态错误码（可按云/账号过滤）。
func hasFailureReason(run SyncRun, reason, cloud, accountKey string) bool {
	for _, f := range run.Failures {
		if f.Reason != reason {
			continue
		}
		if cloud != "" && f.Cloud != cloud {
			continue
		}
		if accountKey != "" && f.AccountKey != accountKey {
			continue
		}
		return true
	}
	return false
}

// syncfp 互异 64 位 hex 指纹（与 difp/dfp/lfp 同构，本文件独立）。
func syncfp(i int) string { return fmt.Sprintf("sy%04x%058x", i, i) }

// ---------------------------------------------------------------------
// AC6 + AC2：空台账首轮同步 = 全量回填；未入账指纹经既有幂等管线入账，
// 会话 operator="scheduler"；幂等重放（第二轮同清单全量跳过）
// ---------------------------------------------------------------------

func TestCertSync_FirstRunFullBackfill(t *testing.T) {
	d := newSyncDeps()
	b1 := certtest.NewBundle(t, "www.sync1-example.com", []string{"www.sync1-example.com"}, nil)
	b2 := certtest.NewBundle(t, "www.sync2-example.com", []string{"www.sync2-example.com"}, nil)
	bv := certtest.NewBundle(t, "www.syncv-example.com", []string{"www.syncv-example.com"}, nil)
	d.aliyun.material["cert-a1"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b1.CertPEM)}
	d.aliyun.material["cert-a2"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b2.CertPEM)}
	d.volcano.material["cert-v1"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(bv.CertPEM)}
	d.lister(domain.CloudAliyun).instances = []CertLibraryInstance{
		{CloudCertID: "cert-a1", Fingerprint: b1.Fingerprint},
		{CloudCertID: "cert-a2", Fingerprint: b2.Fingerprint},
	}
	d.lister(discoveryCloudVolcano).instances = []CertLibraryInstance{
		{CloudCertID: "cert-v1", Fingerprint: bv.Fingerprint},
	}

	run, err := d.svc().SyncCertificates(context.Background())
	require.NoError(t, err)
	assert.Equal(t, domain.DiscoveryImportCompleted, run.Status)
	assert.Equal(t, 2, run.CloudsScanned, "有列举端口的云才参与枚举")
	assert.Equal(t, 2, run.AccountsScanned, "两云各 1 个 active 账号")
	assert.Equal(t, 3, run.Listed)
	assert.Equal(t, 3, run.Imported)
	assert.Equal(t, 3, run.ImportSucceeded)
	assert.Equal(t, 0, run.ImportFailed)
	assert.Empty(t, run.Failures)
	require.NotEmpty(t, run.SessionID)

	// 全量入账 + 映射完整（台账 + 映射 active）
	for _, b := range []*certtest.CertBundle{b1, b2, bv} {
		stored, err := d.certs.GetByFingerprint(context.Background(), b.Fingerprint)
		require.NoError(t, err, "fp %s", b.Fingerprint)
		assert.Equal(t, domain.HostingStatusFingerprintOnly, stored.HostingStatus)
	}
	m, err := d.mappings.FindByCloudCertID(context.Background(), "aliyun", "acct-a", "cert-a1")
	require.NoError(t, err)
	assert.Equal(t, b1.Fingerprint, m.CertFingerprint)
	assert.Equal(t, domain.MappingStatusActive, m.Status)
	mv, err := d.mappings.FindByCloudCertID(context.Background(), "volcano", "acct-v", "cert-v1")
	require.NoError(t, err)
	assert.Equal(t, bv.Fingerprint, mv.CertFingerprint, "火山实例同轮入账（六云枚举含火山）")

	// 会话 operator=scheduler（AC2 标识来源）
	sess, err := d.sessions.GetByID(context.Background(), run.SessionID)
	require.NoError(t, err)
	assert.Equal(t, "scheduler", sess.Operator)
	assert.Equal(t, domain.DiscoveryImportCompleted, sess.Status)

	// 幂等重放：第二轮同清单全量跳过、不再产生导入会话（收敛语义）
	run2, err := d.svc().SyncCertificates(context.Background())
	require.NoError(t, err)
	assert.Equal(t, domain.DiscoveryImportCompleted, run2.Status)
	assert.Equal(t, 3, run2.Skipped)
	assert.Equal(t, 0, run2.Imported)
	assert.Empty(t, run2.SessionID, "无未入账指纹则不创建导入会话")
}

// ---------------------------------------------------------------------
// AC3：指纹已在台账且映射完整 → 跳过且不调 Get（材料通道调用计数=0）
// ---------------------------------------------------------------------

func TestCertSync_SkipMappedNoGet(t *testing.T) {
	d := newSyncDeps()
	known := certtest.NewBundle(t, "www.known-example.com", []string{"www.known-example.com"}, nil)
	fresh := certtest.NewBundle(t, "www.fresh-example.com", []string{"www.fresh-example.com"}, nil)
	d.seedLedgerCert(t, known.Fingerprint, known.CN)
	d.seedMapping(t, known.Fingerprint, "aliyun", "acct-a", "cert-known", time.Now().Add(-time.Hour))
	d.aliyun.material["cert-fresh"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(fresh.CertPEM)}
	d.lister(domain.CloudAliyun).instances = []CertLibraryInstance{
		{CloudCertID: "cert-known", Fingerprint: known.Fingerprint},
		{CloudCertID: "cert-fresh", Fingerprint: fresh.Fingerprint},
	}

	run, err := d.svc().SyncCertificates(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, run.Skipped)
	assert.Equal(t, 1, run.Imported)

	// AC3（修正口径）：五云口径 fake 的 List 即返回元数据——已映射实例跳过 =
	// 无额外 Get：导入材料通道（GetCertChain）对该 certID 调用计数为 0。
	// 火山适配器 List 内部逐实例 Get 为适配器层固有成本（SDK List 无指纹字段），
	// 同步层的跳过=不产生导入动作与台账写（偏差注记见任务提交记录与 proposal）。
	assert.Equal(t, 0, d.aliyun.callCount("cert-known"), "已映射实例不得进入导入材料通道")
	assert.GreaterOrEqual(t, d.aliyun.callCount("cert-fresh"), 1, "未入账指纹经管线拉取材料")

	// 台账无重复行；映射无新增写
	got, err := d.certs.GetByFingerprint(context.Background(), known.Fingerprint)
	require.NoError(t, err)
	assert.Equal(t, known.CN, got.CommonName)
	maps, err := d.mappings.ListByFingerprint(context.Background(), known.Fingerprint)
	require.NoError(t, err)
	assert.Len(t, maps, 1, "已映射实例跳过不重写映射")
}

// ---------------------------------------------------------------------
// AC4：映射缺失 → 补建；同 cloudCertID 新指纹（漂移，新指纹已在台账）→
// 新映射刷新 + 旧映射留痕，FindByCloudCertID 取最新指纹
// ---------------------------------------------------------------------

func TestCertSync_MappingBackfillAndDrift(t *testing.T) {
	d := newSyncDeps()
	fpBF, fpOld, fpNew := syncfp(3), syncfp(4), syncfp(5)
	d.seedLedgerCert(t, fpBF, "cn-backfill")
	d.seedLedgerCert(t, fpOld, "cn-old")
	d.seedLedgerCert(t, fpNew, "cn-new")
	d.seedMapping(t, fpOld, "aliyun", "acct-a", "cert-drift", time.Now().Add(-time.Hour))
	d.lister(domain.CloudAliyun).instances = []CertLibraryInstance{
		{CloudCertID: "cert-bf", Fingerprint: fpBF},     // 指纹在台账、映射缺失 → 补建
		{CloudCertID: "cert-drift", Fingerprint: fpNew}, // 同 cloudCertID 新指纹 → 漂移刷新
	}

	run, err := d.svc().SyncCertificates(context.Background())
	require.NoError(t, err)
	assert.Equal(t, domain.DiscoveryImportCompleted, run.Status)
	assert.Equal(t, 1, run.Backfilled)
	assert.Equal(t, 1, run.Drifted)
	assert.Equal(t, 0, run.Imported, "两实例指纹均在台账，无需导入（不调云 Get）")
	assert.Equal(t, 0, d.aliyun.callCount("cert-bf")+d.aliyun.callCount("cert-drift"))

	// 补建：映射 active 落库
	mBF, err := d.mappings.FindByCloudCertID(context.Background(), "aliyun", "acct-a", "cert-bf")
	require.NoError(t, err)
	assert.Equal(t, fpBF, mBF.CertFingerprint)
	assert.Equal(t, domain.MappingStatusActive, mBF.Status)

	// 漂移留痕：旧指纹映射行不删；新指纹映射新行；反查取最新
	oldRows, err := d.mappings.ListByFingerprint(context.Background(), fpOld)
	require.NoError(t, err)
	assert.Len(t, oldRows, 1, "旧指纹映射留痕不删")
	newRows, err := d.mappings.ListByFingerprint(context.Background(), fpNew)
	require.NoError(t, err)
	assert.Len(t, newRows, 1, "新指纹写入新映射行")
	mDrift, err := d.mappings.FindByCloudCertID(context.Background(), "aliyun", "acct-a", "cert-drift")
	require.NoError(t, err)
	assert.Equal(t, fpNew, mDrift.CertFingerprint, "FindByCloudCertID 按 uploadedAt 取最新指纹")
}

// ---------------------------------------------------------------------
// AC4（导入路径漂移）：同 cloudCertID 新指纹未入账 → 转导入管线，导入后
// 新映射刷新、旧映射留痕、反查取新指纹
// ---------------------------------------------------------------------

func TestCertSync_DriftViaImportPath(t *testing.T) {
	d := newSyncDeps()
	fpOld := syncfp(6)
	b := certtest.NewBundle(t, "www.drift2-example.com", []string{"www.drift2-example.com"}, nil)
	d.seedLedgerCert(t, fpOld, "cn-old")
	d.seedMapping(t, fpOld, "aliyun", "acct-a", "cert-drift2", time.Now().Add(-time.Hour))
	d.aliyun.material["cert-drift2"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)}
	d.lister(domain.CloudAliyun).instances = []CertLibraryInstance{
		{CloudCertID: "cert-drift2", Fingerprint: b.Fingerprint},
	}

	run, err := d.svc().SyncCertificates(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, run.Imported)
	assert.Equal(t, 1, run.ImportSucceeded)

	oldRows, err := d.mappings.ListByFingerprint(context.Background(), fpOld)
	require.NoError(t, err)
	assert.Len(t, oldRows, 1, "旧指纹映射留痕不删")
	newRows, err := d.mappings.ListByFingerprint(context.Background(), b.Fingerprint)
	require.NoError(t, err)
	assert.Len(t, newRows, 1)
	m, err := d.mappings.FindByCloudCertID(context.Background(), "aliyun", "acct-a", "cert-drift2")
	require.NoError(t, err)
	assert.Equal(t, b.Fingerprint, m.CertFingerprint, "反查取最新指纹")
}

// ---------------------------------------------------------------------
// AC1：单云单账号失败经 errorReason 隔离、不中断其余（partial_failed 语义）；
// 云侧错误细节不进结果摘要（仅静态错误码）
// ---------------------------------------------------------------------

func TestCertSync_CloudFailureIsolation(t *testing.T) {
	d := newSyncDeps()
	ok := certtest.NewBundle(t, "www.iso-ok-example.com", []string{"www.iso-ok-example.com"}, nil)
	d.aliyun.material["cert-ok"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(ok.CertPEM)}
	d.aliyun.material["cert-bad"] = DiscoveryCertMaterial{Exists: false}
	d.lister(domain.CloudAliyun).instances = []CertLibraryInstance{
		{CloudCertID: "cert-ok", Fingerprint: ok.Fingerprint},
		{CloudCertID: "cert-bad", Fingerprint: syncfp(7)},
	}
	d.lister(domain.CloudTencent).err = errors.New("tencent list down: SECRET-CLOUD-DETAIL")
	d.accounts.errCloud[domain.CloudAzure] = errors.New("azure account repo down")
	d.lister(domain.CloudAzure) // 注册列举桩（账号加载在其前失败）

	run, err := d.svc().SyncCertificates(context.Background())
	require.NoError(t, err)
	assert.Equal(t, domain.DiscoveryImportPartialFailed, run.Status)

	// 云级失败隔离（静态错误码；云侧错误细节只进日志）
	require.True(t, hasFailureReason(run, reasonSyncListFailed, "tencent", "acct-tx"), "列举失败逐云记因: %+v", run.Failures)
	require.True(t, hasFailureReason(run, reasonSyncAccountFailed, "azure", ""), "账号加载失败逐云记因: %+v", run.Failures)
	for _, f := range run.Failures {
		assert.NotContains(t, f.Reason, "SECRET-CLOUD-DETAIL", "失败摘要不携带云侧错误细节")
		assert.NotContains(t, f.Reason, "azure account repo down")
	}

	// 其余云不中断：aliyun 一导入一失败（材料通道 GetCert 失败 → 会话 failed 条目）
	assert.Equal(t, 2, run.Imported)
	assert.Equal(t, 1, run.ImportSucceeded)
	assert.Equal(t, 1, run.ImportFailed)
	got, err := d.certs.GetByFingerprint(context.Background(), ok.Fingerprint)
	require.NoError(t, err, "健康云实例正常入账")
	assert.Equal(t, ok.Fingerprint, got.Fingerprint)
	assert.NotEmpty(t, run.SessionID)
	sess, err := d.sessions.GetByID(context.Background(), run.SessionID)
	require.NoError(t, err)
	assert.Equal(t, domain.DiscoveryImportPartialFailed, sess.Status, "导入会话终态 partial_failed")
}

// ---------------------------------------------------------------------
// AC5：并发双会话导入同一指纹 → 台账恰 1 条、双会话均无 failed
//（ErrDuplicateFingerprint 归 success，幂等语义）
// ---------------------------------------------------------------------

func TestCertSync_ConcurrentDuplicateFingerprint(t *testing.T) {
	d := newSyncDeps()
	b := certtest.NewBundle(t, "www.race-example.com", []string{"www.race-example.com"}, nil)
	var wg sync.WaitGroup
	wg.Add(2)
	release := make(chan struct{})
	barrier := &syncBarrierAdapter{cloud: domain.CloudAliyun, chainPEM: string(b.CertPEM), wg: &wg, release: release}
	// 同步服务与手动会话共享同一 importer 实例；材料通道换为栅栏桩制造竞态窗口
	imp := NewDiscoveryImportService(d.sessions, d.certs, d.mappings, d.refs,
		[]DiscoveryCertAdapter{barrier, d.tencent, d.volcano}, d.accounts)
	svc := NewCertSyncService([]CertLibraryLister{d.lister(domain.CloudAliyun)}, d.accounts, imp, d.certs, d.mappings)
	d.lister(domain.CloudAliyun).instances = []CertLibraryInstance{
		{CloudCertID: "cert-race", Fingerprint: b.Fingerprint},
	}
	go func() {
		wg.Wait()
		close(release)
	}()

	type sessionResult struct {
		id  string
		err error
	}
	bDone := make(chan sessionResult, 1)
	go func() {
		id, err := imp.ImportFromDiscovery(context.Background(), []DiscoveryImportItemInput{
			{Cloud: "aliyun", AccountKey: "acct-a", CloudCertID: "cert-race"},
		}, "manual-op")
		bDone <- sessionResult{id: id, err: err}
	}()

	run, err := svc.SyncCertificates(context.Background())
	require.NoError(t, err)
	bRes := <-bDone
	require.NoError(t, bRes.err)
	require.NotEmpty(t, run.SessionID)
	require.NotEmpty(t, bRes.id)
	// 手动会话为异步执行：终态收敛后再断言（同步轮返回即自身会话终态）
	waitForDiscoveryTerminal(t, d.sessions, bRes.id)

	// 台账恰 1 条（双 Create 竞态 → 一方 ErrDuplicateFingerprint）
	leds, err := d.certs.List(context.Background())
	require.NoError(t, err)
	n := 0
	for _, c := range leds {
		if c.Fingerprint == b.Fingerprint {
			n++
		}
	}
	assert.Equal(t, 1, n, "台账恰 1 条（指纹唯一键幂等）")

	// 双会话均无 failed（重复指纹条目记 success）
	sessA, err := d.sessions.GetByID(context.Background(), run.SessionID)
	require.NoError(t, err)
	assert.Equal(t, 0, sessA.Progress.Failed, "同步会话无 failed 条目")
	assert.Equal(t, 1, sessA.Progress.Succeeded)
	sessB, err := d.sessions.GetByID(context.Background(), bRes.id)
	require.NoError(t, err)
	assert.Equal(t, 0, sessB.Progress.Failed, "手动会话无 failed 条目")
	assert.Equal(t, 1, sessB.Progress.Succeeded)
}

// ---------------------------------------------------------------------
// CAS 防重守卫：running 中再次触发返回 ErrSyncRunning，不启动第二轮
// ---------------------------------------------------------------------

func TestCertSync_CASGuard(t *testing.T) {
	d := newSyncDeps()
	b := certtest.NewBundle(t, "www.cas-example.com", []string{"www.cas-example.com"}, nil)
	d.aliyun.material["cert-cas"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)}
	d.lister(domain.CloudAliyun).instances = []CertLibraryInstance{
		{CloudCertID: "cert-cas", Fingerprint: b.Fingerprint},
	}
	gate := make(chan struct{})
	d.lister(domain.CloudAliyun).gate = gate
	svc := d.svc()

	type runResult struct {
		run SyncRun
		err error
	}
	done := make(chan runResult, 1)
	go func() {
		run, err := svc.SyncCertificates(context.Background())
		done <- runResult{run: run, err: err}
	}()
	<-d.lister(domain.CloudAliyun).entered // 第一轮已进入运行态

	// running 中二次触发（定时轮/手工两侧同口径）→ ErrSyncRunning
	_, err := svc.SyncCertificates(context.Background())
	assert.ErrorIs(t, err, ErrSyncRunning)
	_, err = svc.SyncCertificatesManual(context.Background())
	assert.ErrorIs(t, err, ErrSyncRunning)

	close(gate)
	res := <-done
	require.NoError(t, res.err)
	assert.Equal(t, domain.DiscoveryImportCompleted, res.run.Status)

	// 释放后可再次触发（守卫随轮结束释放）
	run3, err := svc.SyncCertificates(context.Background())
	require.NoError(t, err)
	assert.Equal(t, domain.DiscoveryImportCompleted, run3.Status)
}

// ---------------------------------------------------------------------
// Hard Rule：整体限时复用 discoveryImportTimeout 语义——到期剩余条目记超时
// 失败因，不悬挂（overallTimeout 测试注入缩短时限）
// ---------------------------------------------------------------------

func TestCertSync_OverallTimeout(t *testing.T) {
	d := newSyncDeps()
	d.lister(domain.CloudAliyun).instances = []CertLibraryInstance{
		{CloudCertID: "cert-slow", Fingerprint: syncfp(8)},
	}
	gate := make(chan struct{})
	defer close(gate) // 兜底释放（运行结束前 ctx 已到期，此闸不阻塞返回）
	d.lister(domain.CloudAliyun).gate = gate
	svc := d.svc()
	svc.(*certSyncService).overallTimeout = 50 * time.Millisecond

	start := time.Now()
	run, err := svc.SyncCertificates(context.Background())
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 5*time.Second, "整体超时后不悬挂")
	assert.Equal(t, domain.DiscoveryImportPartialFailed, run.Status)
	assert.True(t, hasFailureReason(run, reasonSyncTimeout, "aliyun", "acct-a"),
		"超时条目记超时失败因: %+v", run.Failures)
	assert.Equal(t, 0, run.Imported, "超时前未完成判定的实例不进导入")
}

// ---------------------------------------------------------------------
// Hard Rule：只读纪律——同步执行路径不调用任何云写方法（窄端口构造性保证 +
// 写方法计数断言）
// ---------------------------------------------------------------------

func TestCertSync_ReadOnlyDiscipline(t *testing.T) {
	d := newSyncDeps()
	b := certtest.NewBundle(t, "www.ro-example.com", []string{"www.ro-example.com"}, nil)
	d.aliyun.material["cert-ro"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)}
	full := &syncFullCloudAdapter{syncListerStub: d.lister(domain.CloudAliyun)}
	full.instances = []CertLibraryInstance{{CloudCertID: "cert-ro", Fingerprint: b.Fingerprint}}
	svc := NewCertSyncService([]CertLibraryLister{full}, d.accounts, d.importer(), d.certs, d.mappings)

	run, err := svc.SyncCertificates(context.Background())
	require.NoError(t, err)
	assert.Equal(t, domain.DiscoveryImportCompleted, run.Status)
	assert.Equal(t, int32(0), full.writes.Load(), "同步路径绝不调用云写方法")
	assert.Equal(t, 1, full.listCount(), "云侧访问仅只读列举")
	assert.Equal(t, 1, d.aliyun.callCount("cert-ro"), "材料通道仅 GetCertChain 读")
}

// ---------------------------------------------------------------------
// 手工触发 operator 标识（任务 5 端点消费）
// ---------------------------------------------------------------------

func TestCertSync_ManualOperator(t *testing.T) {
	d := newSyncDeps()
	b := certtest.NewBundle(t, "www.manual-example.com", []string{"www.manual-example.com"}, nil)
	d.aliyun.material["cert-manual"] = DiscoveryCertMaterial{Exists: true, CertChainPEM: string(b.CertPEM)}
	d.lister(domain.CloudAliyun).instances = []CertLibraryInstance{
		{CloudCertID: "cert-manual", Fingerprint: b.Fingerprint},
	}

	run, err := d.svc().SyncCertificatesManual(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, run.SessionID)
	sess, err := d.sessions.GetByID(context.Background(), run.SessionID)
	require.NoError(t, err)
	assert.Equal(t, "manual", sess.Operator)
}

// ---------------------------------------------------------------------
// 火山证书库列举端口 shim：cloud 标识 + 实例映射（空 ID/空指纹防御性剔除）
// + ListInstances 装配路径（fake volcanoCertLister 注入）
// ---------------------------------------------------------------------

func TestVolcanoCertLibraryLister_Mapping(t *testing.T) {
	fp := syncfp(9)
	instances := []volcanocert.CloudCertInstance{
		{CloudCertID: "i-1", Fingerprint: fp},
		{CloudCertID: "", Fingerprint: syncfp(10)},
		{CloudCertID: "i-3", Fingerprint: ""},
	}
	out, err := volcanoListInstances(instances, nil)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "i-1", out[0].CloudCertID)
	assert.Equal(t, fp, out[0].Fingerprint)

	outErr, err2 := volcanoListInstances(nil, errors.New("list failed"))
	assert.Nil(t, outErr)
	assert.Error(t, err2)

	l := NewVolcanoCertLibraryLister(nil)
	assert.Equal(t, discoveryCloudVolcano, l.Cloud())
}

// volcanoListerFake volcanoCertLister 窄接口替身（shim 装配路径断言）。
type volcanoListerFake struct {
	instances []volcanocert.CloudCertInstance
	err       error
}

func (f *volcanoListerFake) ListCertificates(context.Context, *sharedomain.CloudAccount) ([]volcanocert.CloudCertInstance, error) {
	return f.instances, f.err
}

func TestVolcanoCertLibraryLister_ListInstances(t *testing.T) {
	fp := syncfp(13)
	l := volcanoCertLibraryLister{adapter: &volcanoListerFake{
		instances: []volcanocert.CloudCertInstance{{CloudCertID: "i-9", Fingerprint: fp}},
	}}
	out, err := l.ListInstances(context.Background(), &sharedomain.CloudAccount{Name: "acct-v"})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "i-9", out[0].CloudCertID)
	assert.Equal(t, fp, out[0].Fingerprint)

	// 云侧错误透传（由调用方逐账号记因隔离）
	lErr := volcanoCertLibraryLister{adapter: &volcanoListerFake{err: errors.New("volcano down")}}
	_, err = lErr.ListInstances(context.Background(), nil)
	assert.Error(t, err)
}

// ---------------------------------------------------------------------
// 判定期 panic 隔离：单实例 panic 落因后继续其余实例（不中断）
// ---------------------------------------------------------------------

// panicPillCertRepo 判定期 panic 注入（judgeInstance recover 隔离断言载体）。
type panicPillCertRepo struct {
	*certtest.FakeCertificateRepo
}

func (r *panicPillCertRepo) GetByFingerprint(context.Context, string) (domain.Certificate, error) {
	panic("ledger exploded: SECRET-PANIC-VALUE")
}

func TestCertSync_JudgePanicIsolation(t *testing.T) {
	d := newSyncDeps()
	d.lister(domain.CloudAliyun).instances = []CertLibraryInstance{
		{CloudCertID: "cert-p1", Fingerprint: syncfp(11)},
		{CloudCertID: "cert-p2", Fingerprint: syncfp(12)},
	}
	svc := NewCertSyncService([]CertLibraryLister{d.lister(domain.CloudAliyun)},
		d.accounts, d.importer(), &panicPillCertRepo{d.certs}, d.mappings)

	run, err := svc.SyncCertificates(context.Background())
	require.NoError(t, err, "panic 被逐条 recover，不中断同步轮")
	assert.Equal(t, domain.DiscoveryImportPartialFailed, run.Status)
	require.Len(t, run.Failures, 2)
	for i, f := range run.Failures {
		assert.Equal(t, reasonSyncJudgeFailed, f.Reason, "failure %d", i)
		assert.Equal(t, "aliyun", f.Cloud)
	}
	assert.Equal(t, 0, run.Imported, "判定失败的实例不进导入")
}
