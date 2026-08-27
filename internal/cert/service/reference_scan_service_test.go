package service

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"

	accountrepo "github.com/Havens-blog/e-cam-service/internal/account/repository"
	assetdomain "github.com/Havens-blog/e-cam-service/internal/asset/domain"
	assetrepo "github.com/Havens-blog/e-cam-service/internal/asset/repository"
	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// =====================================================================
// 测试夹具（mock 五云 + fake K8s，AC"集成测试"）
// =====================================================================

// fakeAssetSource 覆盖率分母假数据源（asset 可用/不可用/计数三种形态）。
type fakeAssetSource struct {
	counts map[CloudProductKey]int
	err    error
}

func (f *fakeAssetSource) Counts(context.Context) (map[CloudProductKey]int, error) {
	return f.counts, f.err
}

// fakeScanAdapter 单云适配 fake：按产品返回发现引用/列举错误，
// GetCert 按 cloudCertID 返回在库状态（记录调用次数供"映射命中不回调"断言）。
type fakeScanAdapter struct {
	cloud       domain.Cloud
	products    []domain.Product
	refs        map[domain.Product][]DiscoveredRef
	listErr     map[domain.Product]error
	certs       map[string]CloudCertStatus
	getCertErr  map[string]error
	getCertHits map[string]int
}

func (f *fakeScanAdapter) Cloud() domain.Cloud        { return f.cloud }
func (f *fakeScanAdapter) Products() []domain.Product { return f.products }

func (f *fakeScanAdapter) ListReferences(_ context.Context, _ *sharedomain.CloudAccount, product domain.Product) ([]DiscoveredRef, error) {
	if err := f.listErr[product]; err != nil {
		return nil, err
	}
	return f.refs[product], nil
}

func (f *fakeScanAdapter) GetCert(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) (CloudCertStatus, error) {
	if f.getCertHits == nil {
		f.getCertHits = map[string]int{}
	}
	f.getCertHits[cloudCertID]++
	if err := f.getCertErr[cloudCertID]; err != nil {
		return CloudCertStatus{}, err
	}
	return f.certs[cloudCertID], nil
}

// fakeAccountSource 扫描账号源 fake。
type fakeAccountSource struct {
	byCloud map[domain.Cloud][]*sharedomain.CloudAccount
	err     error
}

func (f *fakeAccountSource) ActiveByCloud(_ context.Context, cloud domain.Cloud) ([]*sharedomain.CloudAccount, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byCloud[cloud], nil
}

// fakeK8sGateway K8s 引用发现 fake：按 cluster|apiGroup|kind 返回实例/错误。
type fakeK8sGateway struct {
	objects map[string][]K8sObject
	errs    map[string]error
}

func k8sGatewayKey(cluster, apiGroup, kind string) string {
	return cluster + "|" + apiGroup + "|" + kind
}

func (f *fakeK8sGateway) ListObjects(_ context.Context, cluster, apiGroup, kind string) ([]K8sObject, error) {
	k := k8sGatewayKey(cluster, apiGroup, kind)
	if err := f.errs[k]; err != nil {
		return nil, err
	}
	return f.objects[k], nil
}

// fakeNotifier 告警 fake（记录 NotifyScanTimedOut 调用）。
type fakeNotifier struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeNotifier) NotifyScanTimedOut(_ context.Context, snapshotID string, _, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, snapshotID)
	return nil
}

// fakeMappingRepo 映射仓储 fake（FindByCloudCertID 语义与 Mongo 实现一致：
// 空参通配、uploadedAt 最新优先）。
type fakeMappingRepo struct {
	mu       sync.Mutex
	mappings []domain.CloudCertMapping
}

func (f *fakeMappingRepo) Upsert(_ context.Context, m *domain.CloudCertMapping) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.mappings {
		if f.mappings[i].CertFingerprint == m.CertFingerprint &&
			f.mappings[i].Cloud == m.Cloud && f.mappings[i].AccountKey == m.AccountKey {
			f.mappings[i] = *m
			return nil
		}
	}
	stored := *m
	if stored.UploadedAt.IsZero() {
		stored.UploadedAt = time.Now()
	}
	if stored.Status == "" {
		stored.Status = domain.MappingStatusActive
	}
	f.mappings = append(f.mappings, stored)
	return nil
}

func (f *fakeMappingRepo) ListByFingerprint(_ context.Context, fingerprint string) ([]domain.CloudCertMapping, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.CloudCertMapping
	for _, m := range f.mappings {
		if m.CertFingerprint == fingerprint {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeMappingRepo) FindByCloudCertID(_ context.Context, cloud, accountKey, cloudCertID string) (domain.CloudCertMapping, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best *domain.CloudCertMapping
	for i := range f.mappings {
		m := &f.mappings[i]
		if m.CloudCertID != cloudCertID {
			continue
		}
		if cloud != "" && m.Cloud != cloud {
			continue
		}
		if accountKey != "" && m.AccountKey != accountKey {
			continue
		}
		if best == nil || m.UploadedAt.After(best.UploadedAt) {
			best = m
		}
	}
	if best == nil {
		return domain.CloudCertMapping{}, mongo.ErrNoDocuments
	}
	return *best, nil
}

func (f *fakeMappingRepo) UpdateStatus(_ context.Context, _ string, _ domain.MappingStatus) error {
	return nil
}

func (f *fakeMappingRepo) ListByStatus(_ context.Context, status domain.MappingStatus) ([]domain.CloudCertMapping, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.CloudCertMapping
	for _, m := range f.mappings {
		if m.Status == status {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeMappingRepo) DeleteByID(_ context.Context, _ string) error {
	return nil
}

// fakeAlertCfgRepo 告警配置 fake（返回固定配置）。
type fakeAlertCfgRepo struct {
	cfg domain.AlertConfig
}

func (f *fakeAlertCfgRepo) Get(context.Context) (domain.AlertConfig, error) { return f.cfg, nil }
func (f *fakeAlertCfgRepo) Save(_ context.Context, _ *domain.AlertConfig) error {
	return nil
}

// scanHarness 扫描服务测试装置。
type scanHarness struct {
	svc       ReferenceScanService
	snapshots *certtest.FakeScanSnapshotRepo
	refs      *certtest.FakeCertReferenceRepo
	mappings  *fakeMappingRepo
	crdRegs   *certtest.FakeCrdRegistrationRepo
	assets    AssetCountSource
	accounts  *fakeAccountSource
	k8s       *fakeK8sGateway
	notifier  *fakeNotifier
	alertCfg  *fakeAlertCfgRepo
}

// newScanHarness 构造测试装置：默认逐适配云配 acc-1 账号。
func newScanHarness(t *testing.T, adapters []CloudScanAdapter, assets AssetCountSource) *scanHarness {
	t.Helper()
	h := &scanHarness{
		snapshots: certtest.NewFakeScanSnapshotRepo(),
		refs:      certtest.NewFakeCertReferenceRepo(),
		mappings:  &fakeMappingRepo{},
		crdRegs:   certtest.NewFakeCrdRegistrationRepo(),
		assets:    assets,
		accounts: &fakeAccountSource{
			byCloud: map[domain.Cloud][]*sharedomain.CloudAccount{},
		},
		k8s:      &fakeK8sGateway{objects: map[string][]K8sObject{}, errs: map[string]error{}},
		notifier: &fakeNotifier{},
		alertCfg: &fakeAlertCfgRepo{cfg: domain.DefaultAlertConfig()},
	}
	for _, a := range adapters {
		if _, ok := h.accounts.byCloud[a.Cloud()]; !ok {
			h.accounts.byCloud[a.Cloud()] = []*sharedomain.CloudAccount{
				{Name: "acc-1", Provider: sharedomain.CloudProvider(a.Cloud()),
					AccessKeyID: "ak-test", AccessKeySecret: "sk-test"},
			}
		}
	}
	h.svc = NewReferenceScanService(
		h.snapshots, h.refs, h.mappings, h.crdRegs, h.alertCfg,
		assets, adapters, h.accounts, h.k8s, h.notifier)
	return h
}

// aliyunAdapter 便捷构造：默认 cdn 产品 fake 适配。
func aliyunAdapter(products ...domain.Product) *fakeScanAdapter {
	if len(products) == 0 {
		products = []domain.Product{domain.ProductCDN}
	}
	return &fakeScanAdapter{
		cloud:    domain.CloudAliyun,
		products: products,
		refs:     map[domain.Product][]DiscoveredRef{},
		listErr:  map[domain.Product]error{},
		certs:    map[string]CloudCertStatus{},
	}
}

// validFingerprintRe 台账指纹对齐口径。
var validFingerprintRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// coverageEntry 取指定键条目。
func coverageEntry(t *testing.T, meta []domain.CoverageMeta, cloud, product string) domain.CoverageMeta {
	t.Helper()
	for _, m := range meta {
		if m.Cloud == cloud && m.Product == product {
			return m
		}
	}
	t.Fatalf("coverage entry %s/%s not found in %+v", cloud, product, meta)
	return domain.CoverageMeta{}
}

// seedRunningSnapshot 播种指定起始时间的 running 快照。
func seedRunningSnapshot(t *testing.T, repo *certtest.FakeScanSnapshotRepo, startedAt time.Time) string {
	t.Helper()
	id, err := repo.Create(context.Background(), &domain.ScanSnapshot{
		StartedAt: startedAt,
		Status:    domain.ScanStatusRunning,
	})
	require.NoError(t, err)
	return id
}

// seedBuiltinCrdRegistration 播种内置 AlbConfig 登记项（enabled）。
func seedBuiltinCrdRegistration(t *testing.T, repo *certtest.FakeCrdRegistrationRepo, cluster string) {
	t.Helper()
	err := repo.Create(context.Background(), &domain.CrdRegistration{
		ClusterID:     cluster,
		APIGroup:      "alb.alibabacloud.com",
		Kind:          "AlbConfig",
		CertFieldPath: "spec.listeners[].certificates[].certificateId",
		Operator:      "system",
	})
	require.NoError(t, err)
}

// =====================================================================
// AC1：扫描启动与防重（409 SCAN_IN_PROGRESS）
// =====================================================================

// TestStartScanLifecycle 快照生命周期：创建 running → 完成转 done + finishedAt。
func TestStartScanLifecycle(t *testing.T) {
	adapter := aliyunAdapter()
	adapter.refs[domain.ProductCDN] = []DiscoveredRef{
		{Cloud: "aliyun", Product: "cdn", ResourceID: "example.com", ReferencedCloudCertID: "cert-1", AccountKey: "acc-1"},
	}
	h := newScanHarness(t, []CloudScanAdapter{adapter}, &fakeAssetSource{
		counts: map[CloudProductKey]int{{Cloud: domain.CloudAliyun, Product: domain.ProductCDN}: 5},
	})

	res, err := h.svc.StartScan(context.Background())
	require.NoError(t, err)
	assert.Equal(t, domain.ScanStatusDone, res.Status)
	assert.Empty(t, res.FailReason)
	assert.NotEmpty(t, res.SnapshotID)

	snap, err := h.snapshots.GetByID(context.Background(), res.SnapshotID)
	require.NoError(t, err)
	assert.Equal(t, domain.ScanStatusDone, snap.Status)
	require.NotNil(t, snap.FinishedAt, "完成快照必须固化 finishedAt")
	assert.False(t, snap.StartedAt.IsZero())
	assert.Empty(t, snap.FailReason)

	// 引用写通：snapshotId/scannedAt 与归属字段
	refs, err := h.refs.ListBySnapshotID(context.Background(), res.SnapshotID)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, res.SnapshotID, refs[0].SnapshotID)
	assert.False(t, refs[0].ScannedAt.IsZero(), "scannedAt 由写路径固化")
	assert.Equal(t, domain.CloudAliyun, refs[0].Cloud)
	assert.Equal(t, domain.ProductCDN, refs[0].Product)
	assert.Equal(t, "example.com", refs[0].ResourceID)
	assert.Equal(t, "cert-1", refs[0].ReferencedCloudCertID)
	assert.Equal(t, "acc-1", refs[0].AccountKey)
}

// TestStartScanDuplicateBlocked 防重：已有 running 快照 → SCAN_IN_PROGRESS，
// 不创建新快照、不写引用。
func TestStartScanDuplicateBlocked(t *testing.T) {
	adapter := aliyunAdapter()
	h := newScanHarness(t, []CloudScanAdapter{adapter}, &fakeAssetSource{counts: map[CloudProductKey]int{}})
	runningID := seedRunningSnapshot(t, h.snapshots, time.Now())

	_, err := h.svc.StartScan(context.Background())
	require.Error(t, err)
	ce, ok := domain.AsCertError(err)
	require.True(t, ok, "防重错误应为 CertError（web 层映射 409）")
	assert.Equal(t, domain.CodeScanInProgress, ce.Code())
	assert.True(t, errors.Is(err, domain.ErrScanInProgress))

	// 未新建快照（LatestRunning 仍为原快照）、未写引用
	latest, err := h.snapshots.LatestRunning(context.Background())
	require.NoError(t, err)
	assert.Equal(t, runningID, latest.ID.Hex())
	refs, err := h.refs.ListBySnapshotID(context.Background(), runningID)
	require.NoError(t, err)
	assert.Empty(t, refs)
}

// =====================================================================
// AC2：五云 + K8s 发现（指纹解析 / namespace+kind 落库）
// =====================================================================

// TestScanCloudFingerprintResolution 指纹解析三径：映射反查（不回调 GetCert）、
// GetCert 云侧要素（SHA256 对齐）、无法解析占位指纹（保留引用以云证书 ID 关联）。
func TestScanCloudFingerprintResolution(t *testing.T) {
	adapter := aliyunAdapter()
	adapter.refs[domain.ProductCDN] = []DiscoveredRef{
		{Cloud: "aliyun", Product: "cdn", ResourceID: "mapped.example.com", ReferencedCloudCertID: "cert-mapped", AccountKey: "acc-1"},
		{Cloud: "aliyun", Product: "cdn", ResourceID: "elements.example.com", ReferencedCloudCertID: "cert-elements", AccountKey: "acc-1"},
		{Cloud: "aliyun", Product: "cdn", ResourceID: "unknown.example.com", ReferencedCloudCertID: "cert-unknown", AccountKey: "acc-1"},
		{Cloud: "aliyun", Product: "cdn", ResourceID: "sha1.example.com", ReferencedCloudCertID: "cert-sha1", AccountKey: "acc-1"},
	}
	// 映射反查命中（harness 持有 mappings fake，下方播种）
	adapter.certs["cert-elements"] = CloudCertStatus{
		Exists:      true,
		Fingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	// cert-unknown：GetCert 无指纹；cert-sha1：SHA-1 口径（40 hex，不对齐）
	adapter.certs["cert-sha1"] = CloudCertStatus{Exists: true, Fingerprint: "0123456789abcdef0123456789abcdef01234567"}

	h := newScanHarness(t, []CloudScanAdapter{adapter}, &fakeAssetSource{counts: map[CloudProductKey]int{}})
	require.NoError(t, h.mappings.Upsert(context.Background(), &domain.CloudCertMapping{
		CertFingerprint: "fp-a", Cloud: "aliyun", AccountKey: "acc-1", CloudCertID: "cert-mapped",
	}))

	res, err := h.svc.StartScan(context.Background())
	require.NoError(t, err)
	require.Equal(t, domain.ScanStatusDone, res.Status)

	refs, err := h.refs.ListBySnapshotID(context.Background(), res.SnapshotID)
	require.NoError(t, err)
	require.Len(t, refs, 4)
	byResource := make(map[string]domain.CertReference, len(refs))
	for _, r := range refs {
		byResource[r.ResourceID] = r
	}
	// 1. 映射反查（精确指纹；不回调 GetCert）
	assert.Equal(t, "fp-a", byResource["mapped.example.com"].CertFingerprint)
	assert.Equal(t, 0, adapter.getCertHits["cert-mapped"], "映射命中的云证书不应回调 GetCert")
	// 2. GetCert 云侧要素（SHA256 对齐口径）
	assert.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		byResource["elements.example.com"].CertFingerprint)
	// 3. 无法解析：占位指纹（64 hex 确定性）+ 云证书 ID 关联保留
	fpUnknown := byResource["unknown.example.com"].CertFingerprint
	assert.Regexp(t, validFingerprintRe, fpUnknown)
	assert.NotEqual(t, "fp-a", fpUnknown)
	assert.Equal(t, "cert-unknown", byResource["unknown.example.com"].ReferencedCloudCertID)
	// 4. SHA-1 口径不对齐 → 无法解析（占位）
	fpSHA1 := byResource["sha1.example.com"].CertFingerprint
	assert.Regexp(t, validFingerprintRe, fpSHA1)
	assert.NotEqual(t, "0123456789abcdef0123456789abcdef01234567", fpSHA1)
	assert.NotEqual(t, fpUnknown, fpSHA1, "不同云证书 ID 占位指纹不同")
}

// TestScanUnresolvedFingerprintStable 占位指纹确定性：同云证书 ID 多处引用
// 指纹一致（跨资源稳定，供后续登记关联去重）。
func TestScanUnresolvedFingerprintStable(t *testing.T) {
	adapter := aliyunAdapter()
	adapter.refs[domain.ProductCDN] = []DiscoveredRef{
		{Cloud: "aliyun", Product: "cdn", ResourceID: "a.example.com", ReferencedCloudCertID: "cert-x", AccountKey: "acc-1"},
		{Cloud: "aliyun", Product: "cdn", ResourceID: "b.example.com", ReferencedCloudCertID: "cert-x", AccountKey: "acc-1"},
	}
	h := newScanHarness(t, []CloudScanAdapter{adapter}, &fakeAssetSource{counts: map[CloudProductKey]int{}})

	res, err := h.svc.StartScan(context.Background())
	require.NoError(t, err)
	refs, err := h.refs.ListBySnapshotID(context.Background(), res.SnapshotID)
	require.NoError(t, err)
	require.Len(t, refs, 2)
	assert.Equal(t, refs[0].CertFingerprint, refs[1].CertFingerprint)
	assert.Regexp(t, validFingerprintRe, refs[0].CertFingerprint)
}

// TestScanK8sReferenceFields K8s 引用字段落库：clusterId/namespace/kind/resourceId
// + certFieldPath 值抽取（多证书监听展开）+ 跨云映射反查解析指纹。
func TestScanK8sReferenceFields(t *testing.T) {
	adapter := aliyunAdapter()
	h := newScanHarness(t, []CloudScanAdapter{adapter}, &fakeAssetSource{counts: map[CloudProductKey]int{}})
	seedBuiltinCrdRegistration(t, h.crdRegs, "cluster-a")
	h.k8s.objects[k8sGatewayKey("cluster-a", "alb.alibabacloud.com", "AlbConfig")] = []K8sObject{
		{
			Namespace: "ns-web",
			Name:      "alb-1",
			Content: map[string]interface{}{
				"spec": map[string]interface{}{
					"listeners": []interface{}{
						map[string]interface{}{
							"certificates": []interface{}{
								map[string]interface{}{"certificateId": "cert-k8s-1"},
								map[string]interface{}{"certificateId": "cert-k8s-2"},
							},
						},
					},
				},
			},
		},
		{Namespace: "ns-api", Name: "alb-2", Content: map[string]interface{}{
			"spec": map[string]interface{}{"listeners": []interface{}{}},
		}},
	}
	// K8s 证书引用经通配映射反查（cloud/accountKey 未知）
	require.NoError(t, h.mappings.Upsert(context.Background(), &domain.CloudCertMapping{
		CertFingerprint: "fp-k8s", Cloud: "aliyun", AccountKey: "acc-9", CloudCertID: "cert-k8s-1",
	}))

	res, err := h.svc.StartScan(context.Background())
	require.NoError(t, err)
	require.Equal(t, domain.ScanStatusDone, res.Status)

	refs, err := h.refs.ListBySnapshotID(context.Background(), res.SnapshotID)
	require.NoError(t, err)
	require.Len(t, refs, 2, "alb-1 双证书两引用；alb-2 无证书引用零产出")

	first := refs[0]
	assert.Equal(t, domain.ProductCRD, first.Product)
	assert.Equal(t, "cluster-a", first.ClusterID)
	assert.Equal(t, "ns-web", first.Namespace)
	assert.Equal(t, "AlbConfig", first.Kind)
	assert.Equal(t, "alb-1", first.ResourceID)
	assert.Equal(t, "cert-k8s-1", first.ReferencedCloudCertID)
	assert.Equal(t, "fp-k8s", first.CertFingerprint, "K8s 引用经通配映射反查解析")

	second := refs[1]
	assert.Equal(t, "cert-k8s-2", second.ReferencedCloudCertID)
	assert.Regexp(t, validFingerprintRe, second.CertFingerprint)
	assert.Equal(t, "alb-1", second.ResourceID)

	// covered 去重：alb-1 双证书计 1 资源；crd 条目 total=-1（asset 不盘点 K8s）
	crd := coverageEntry(t, res.CoverageMeta, "", "crd")
	assert.Equal(t, 1, crd.Covered)
	assert.Equal(t, -1, crd.Total)
}

// =====================================================================
// AC3：coverageMeta 分母口径（asset 聚合 / -1 分支 / 滞后标记）
// =====================================================================

// TestCoverageMetaTotalsFromAsset 分母=asset 聚合计数（非扫描自身发现数）；
// covered=本轮去重资源数；无账号云不入范围。
func TestCoverageMetaTotalsFromAsset(t *testing.T) {
	aliyunAd := aliyunAdapter()
	aliyunAd.refs[domain.ProductCDN] = []DiscoveredRef{
		{Cloud: "aliyun", Product: "cdn", ResourceID: "a.example.com", ReferencedCloudCertID: "c1", AccountKey: "acc-1"},
		{Cloud: "aliyun", Product: "cdn", ResourceID: "a.example.com", ReferencedCloudCertID: "c2", AccountKey: "acc-1"},
		{Cloud: "aliyun", Product: "cdn", ResourceID: "b.example.com", ReferencedCloudCertID: "c1", AccountKey: "acc-1"},
	}
	huaweiAd := &fakeScanAdapter{ // 有适配但无 active 账号 → 不入范围
		cloud: domain.CloudHuawei, products: []domain.Product{domain.ProductCDN},
		refs: map[domain.Product][]DiscoveredRef{}, listErr: map[domain.Product]error{},
		certs: map[string]CloudCertStatus{},
	}
	h := newScanHarness(t, []CloudScanAdapter{aliyunAd, huaweiAd}, &fakeAssetSource{
		counts: map[CloudProductKey]int{
			{Cloud: domain.CloudAliyun, Product: domain.ProductCDN}: 5,
			{Cloud: domain.CloudHuawei, Product: domain.ProductCDN}: 3,
		},
	})
	h.accounts.byCloud[domain.CloudHuawei] = nil // 华为云显式无账号

	res, err := h.svc.StartScan(context.Background())
	require.NoError(t, err)
	require.Equal(t, domain.ScanStatusDone, res.Status)

	cdn := coverageEntry(t, res.CoverageMeta, "aliyun", "cdn")
	assert.Equal(t, 5, cdn.Total, "分母来自 asset 聚合（非本轮发现数 2）")
	assert.Equal(t, 2, cdn.Covered, "covered=去重资源数（同资源双证书计 1）")
	assert.False(t, cdn.Lagging)

	// 华为云无账号：不入扫描范围（coverageMeta 不出现，该云保持盲区声明）
	for _, m := range res.CoverageMeta {
		assert.NotEqual(t, "huawei", m.Cloud, "无账号云不得进入扫描范围声明")
	}
}

// TestCoverageMetaAssetUnavailable asset 不可用 → total=-1（盲区声明），
// 扫描不中断（发现照常、终态 done）。
func TestCoverageMetaAssetUnavailable(t *testing.T) {
	adapter := aliyunAdapter()
	adapter.refs[domain.ProductCDN] = []DiscoveredRef{
		{Cloud: "aliyun", Product: "cdn", ResourceID: "a.example.com", ReferencedCloudCertID: "c1", AccountKey: "acc-1"},
	}
	h := newScanHarness(t, []CloudScanAdapter{adapter}, &fakeAssetSource{
		err: errors.New("asset inventory unavailable"),
	})

	res, err := h.svc.StartScan(context.Background())
	require.NoError(t, err, "asset 不可用不中断扫描")
	require.Equal(t, domain.ScanStatusDone, res.Status)

	cdn := coverageEntry(t, res.CoverageMeta, "aliyun", "cdn")
	assert.Equal(t, -1, cdn.Total)
	assert.Equal(t, 1, cdn.Covered)
}

// TestCoverageMetaZeroButHistoricNonZero 计数异常（0 但历史非 0）→ total=-1；
// 历史同为 0 的键保持 0（合法空集）。
func TestCoverageMetaZeroButHistoricNonZero(t *testing.T) {
	adapter := aliyunAdapter(domain.ProductCDN, domain.ProductWAF)
	adapter.refs[domain.ProductCDN] = []DiscoveredRef{
		{Cloud: "aliyun", Product: "cdn", ResourceID: "a.example.com", ReferencedCloudCertID: "c1", AccountKey: "acc-1"},
	}
	h := newScanHarness(t, []CloudScanAdapter{adapter}, &fakeAssetSource{
		counts: map[CloudProductKey]int{}, // 全部计 0
	})
	// 历史成功快照：cdn total=5（非 0）；waf 无历史条目
	prevID, err := h.snapshots.Create(context.Background(), &domain.ScanSnapshot{
		Status: domain.ScanStatusDone,
		CoverageMeta: []domain.CoverageMeta{
			{Cloud: "aliyun", Product: "cdn", Covered: 1, Total: 5},
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, prevID)

	res, err := h.svc.StartScan(context.Background())
	require.NoError(t, err)

	cdn := coverageEntry(t, res.CoverageMeta, "aliyun", "cdn")
	assert.Equal(t, -1, cdn.Total, "0 但历史非 0 → 分母不可用")
	waf := coverageEntry(t, res.CoverageMeta, "aliyun", "waf")
	assert.Equal(t, 0, waf.Total, "无历史非 0 的 0 计数为合法空集")
}

// TestCoverageMetaCoveredExceedsTotal covered>total：以 covered 为准
// （EffectiveTotal）并标记滞后。
func TestCoverageMetaCoveredExceedsTotal(t *testing.T) {
	adapter := aliyunAdapter()
	adapter.refs[domain.ProductCDN] = []DiscoveredRef{
		{Cloud: "aliyun", Product: "cdn", ResourceID: "a.example.com", ReferencedCloudCertID: "c1", AccountKey: "acc-1"},
		{Cloud: "aliyun", Product: "cdn", ResourceID: "b.example.com", ReferencedCloudCertID: "c2", AccountKey: "acc-1"},
		{Cloud: "aliyun", Product: "cdn", ResourceID: "c.example.com", ReferencedCloudCertID: "c3", AccountKey: "acc-1"},
	}
	h := newScanHarness(t, []CloudScanAdapter{adapter}, &fakeAssetSource{
		counts: map[CloudProductKey]int{{Cloud: domain.CloudAliyun, Product: domain.ProductCDN}: 1},
	})

	res, err := h.svc.StartScan(context.Background())
	require.NoError(t, err)

	cdn := coverageEntry(t, res.CoverageMeta, "aliyun", "cdn")
	assert.Equal(t, 3, cdn.Covered)
	assert.Equal(t, 1, cdn.Total, "原始 asset 分母保持不动（防自指改写）")
	assert.True(t, cdn.Lagging, "covered>total 标记 asset 盘点滞后")
	assert.Equal(t, 3, cdn.EffectiveTotal(), "有效分母以 covered 为准")
}

// =====================================================================
// AC4：终态（部分失败不阻塞 / 整体失败 / 空范围）
// =====================================================================

// TestScanPartialFailureStillDone 单产品通道失败：其余照常发现，终态 done，
// 失败通道记入快照元数据 partialFailures。
func TestScanPartialFailureStillDone(t *testing.T) {
	adapter := aliyunAdapter(domain.ProductCDN, domain.ProductWAF)
	adapter.refs[domain.ProductCDN] = []DiscoveredRef{
		{Cloud: "aliyun", Product: "cdn", ResourceID: "a.example.com", ReferencedCloudCertID: "c1", AccountKey: "acc-1"},
	}
	adapter.listErr[domain.ProductWAF] = errors.New("aliyun waf api throttled")
	h := newScanHarness(t, []CloudScanAdapter{adapter}, &fakeAssetSource{counts: map[CloudProductKey]int{}})

	res, err := h.svc.StartScan(context.Background())
	require.NoError(t, err)
	assert.Equal(t, domain.ScanStatusDone, res.Status, "部分失败不阻塞其他云/产品")
	assert.Equal(t, 1, res.ChannelsFailed)
	assert.Equal(t, 2, res.ChannelsAttempted)
	require.Len(t, res.PartialFailures, 1)
	assert.Equal(t, "aliyun", res.PartialFailures[0].Cloud)
	assert.Equal(t, "waf", res.PartialFailures[0].Product)
	assert.Equal(t, "acc-1", res.PartialFailures[0].Account)

	// 快照元数据同步固化
	snap, err := h.snapshots.GetByID(context.Background(), res.SnapshotID)
	require.NoError(t, err)
	require.Len(t, snap.PartialFailures, 1)
	assert.Equal(t, "waf", snap.PartialFailures[0].Product)

	refs, err := h.refs.ListBySnapshotID(context.Background(), res.SnapshotID)
	require.NoError(t, err)
	assert.Len(t, refs, 1, "失败通道不产出引用，成功通道照常写入")
}

// TestScanAllChannelsFailed 全部通道失败 → 整体 failed + failReason。
func TestScanAllChannelsFailed(t *testing.T) {
	adapter := aliyunAdapter()
	adapter.listErr[domain.ProductCDN] = errors.New("aliyun cdn api error")
	h := newScanHarness(t, []CloudScanAdapter{adapter}, &fakeAssetSource{counts: map[CloudProductKey]int{}})

	res, err := h.svc.StartScan(context.Background())
	require.NoError(t, err)
	assert.Equal(t, domain.ScanStatusFailed, res.Status)
	assert.Equal(t, domain.FailReasonScanDiscoveryFailed, res.FailReason)

	snap, err := h.snapshots.GetByID(context.Background(), res.SnapshotID)
	require.NoError(t, err)
	assert.Equal(t, domain.ScanStatusFailed, snap.Status)
	assert.Equal(t, domain.FailReasonScanDiscoveryFailed, snap.FailReason)
	assert.NotNil(t, snap.FinishedAt)
}

// TestScanNoChannels 空范围（无账号且无登记）→ 显式失败（防"空扫=已扫描"
// 声明失真），不写引用。
func TestScanNoChannels(t *testing.T) {
	adapter := aliyunAdapter()
	h := newScanHarness(t, []CloudScanAdapter{adapter}, &fakeAssetSource{counts: map[CloudProductKey]int{}})
	h.accounts.byCloud[domain.CloudAliyun] = nil // 无 active 账号

	res, err := h.svc.StartScan(context.Background())
	require.NoError(t, err)
	assert.Equal(t, domain.ScanStatusFailed, res.Status)
	assert.Equal(t, domain.FailReasonScanNoChannels, res.FailReason)

	refs, err := h.refs.ListBySnapshotID(context.Background(), res.SnapshotID)
	require.NoError(t, err)
	assert.Empty(t, refs)
}

// =====================================================================
// AC5：scan-timeout 恢复
// =====================================================================

// TestRecoverTimedOutScans 超时恢复：running 且超 scanTimeoutHours →
// failed（SCAN_TIMED_OUT）+ 告警事件 + 释放防重锁（可重新触发）。
func TestRecoverTimedOutScans(t *testing.T) {
	adapter := aliyunAdapter()
	h := newScanHarness(t, []CloudScanAdapter{adapter}, &fakeAssetSource{counts: map[CloudProductKey]int{}})
	h.alertCfg.cfg.Thresholds.ScanTimeoutHours = 2

	timedOutID := seedRunningSnapshot(t, h.snapshots, time.Now().Add(-3*time.Hour))

	recovered, err := h.svc.RecoverTimedOutScans(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, recovered)

	// 超时快照转 failed + SCAN_TIMED_OUT
	timedOut, err := h.snapshots.GetByID(context.Background(), timedOutID)
	require.NoError(t, err)
	assert.Equal(t, domain.ScanStatusFailed, timedOut.Status)
	assert.Equal(t, domain.FailReasonScanTimedOut, timedOut.FailReason)
	require.NotNil(t, timedOut.FinishedAt)

	// 告警事件已发（仅超时快照）
	assert.Equal(t, []string{timedOutID}, h.notifier.calls)

	// 防重锁已释放：可重新触发扫描（不再 SCAN_IN_PROGRESS）
	res, err := h.svc.StartScan(context.Background())
	require.NoError(t, err)
	assert.Equal(t, domain.ScanStatusDone, res.Status)
	assert.NotEqual(t, timedOutID, res.SnapshotID)
}

// TestRecoverTimedOutScansFreshUntouched 未超时快照不受恢复影响；
// notifier 缺省（nil）不 panic。
func TestRecoverTimedOutScansFreshUntouched(t *testing.T) {
	adapter := aliyunAdapter()
	h := newScanHarness(t, []CloudScanAdapter{adapter}, &fakeAssetSource{counts: map[CloudProductKey]int{}})
	h.alertCfg.cfg.Thresholds.ScanTimeoutHours = 2

	timedOutID := seedRunningSnapshot(t, h.snapshots, time.Now().Add(-5*time.Hour))
	freshID := seedRunningSnapshot(t, h.snapshots, time.Now().Add(-10*time.Minute))

	recovered, err := h.svc.RecoverTimedOutScans(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, recovered)

	timedOut, err := h.snapshots.GetByID(context.Background(), timedOutID)
	require.NoError(t, err)
	assert.Equal(t, domain.ScanStatusFailed, timedOut.Status)

	fresh, err := h.snapshots.GetByID(context.Background(), freshID)
	require.NoError(t, err)
	assert.Equal(t, domain.ScanStatusRunning, fresh.Status, "未超时快照不受恢复影响")
	assert.Equal(t, []string{timedOutID}, h.notifier.calls)
}

// =====================================================================
// 单元：certFieldPath 抽取 / GVR 候选
// =====================================================================

// TestExtractCertFieldValues certFieldPath 值抽取（嵌套数组下钻/多值/数值/
// 缺失路径/单值容错）。
func TestExtractCertFieldValues(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"listeners": []interface{}{
				map[string]interface{}{
					"port":         float64(443),
					"certificates": []interface{}{map[string]interface{}{"certificateId": "cert-1"}},
				},
				map[string]interface{}{
					"port":         float64(8443),
					"certificates": []interface{}{map[string]interface{}{"certificateId": float64(8089870)}},
				},
			},
			"tls": []interface{}{
				map[string]interface{}{"secretName": "tls-secret"},
			},
			"gateway": map[string]interface{}{"certificateRef": map[string]interface{}{"name": "gw-cert"}},
		},
	}

	assert.Equal(t, []string{"cert-1", "8089870"},
		extractCertFieldValues(obj, "spec.listeners[].certificates[].certificateId"))
	assert.Equal(t, []string{"tls-secret"}, extractCertFieldValues(obj, "spec.tls[].secretName"))
	assert.Equal(t, []string{"gw-cert"}, extractCertFieldValues(obj, "spec.gateway.certificateRef.name"))
	assert.Nil(t, extractCertFieldValues(obj, "spec.missing[].field"))
	assert.Nil(t, extractCertFieldValues(obj, "spec.listeners"))
	assert.Nil(t, extractCertFieldValues(nil, "spec.tls[].secretName"))

	// 数组段遇单值：容错保留（部分 CRD 单元素免数组）
	single := map[string]interface{}{"spec": map[string]interface{}{
		"listeners": map[string]interface{}{"certificates": []interface{}{
			map[string]interface{}{"certificateId": "cert-2"},
		}},
	}}
	assert.Equal(t, []string{"cert-2"}, extractCertFieldValues(single, "spec.listeners[].certificates[].certificateId"))
}

// TestCandidateGVRs 内置固定枚举精确 GVR；自定义登记 version 候选探测。
func TestCandidateGVRs(t *testing.T) {
	ingress := candidateGVRs("networking.k8s.io", "Ingress")
	require.Len(t, ingress, 1)
	assert.Equal(t, "ingresses", ingress[0].Resource)
	assert.Equal(t, "v1", ingress[0].Version)
	assert.Equal(t, "networking.k8s.io", ingress[0].Group)

	custom := candidateGVRs("gateway.example.com", "MyGateway")
	require.Len(t, custom, 3)
	assert.Equal(t, "mygateways", custom[0].Resource)
	assert.Equal(t, "gateway.example.com", custom[0].Group)
	assert.Equal(t, []string{"v1", "v1beta1", "v1alpha1"},
		[]string{custom[0].Version, custom[1].Version, custom[2].Version})
}

// TestScanAccountSourceError 账号源不���用：启动失败（不创建快照）。
func TestScanAccountSourceError(t *testing.T) {
	adapter := aliyunAdapter()
	h := newScanHarness(t, []CloudScanAdapter{adapter}, &fakeAssetSource{counts: map[CloudProductKey]int{}})
	h.accounts.err = errors.New("account store unavailable")

	_, err := h.svc.StartScan(context.Background())
	require.Error(t, err)

	_, lerr := h.snapshots.LatestRunning(context.Background())
	assert.ErrorIs(t, lerr, mongo.ErrNoDocuments, "启动失败不得残留 running 快照")
}

// =====================================================================
// 生产 shim：云适配元数据 / asset 计数 / 账号源 / K8s 候选探测
// =====================================================================

// TestCloudShimMetadata 五云 shim 元数据：归属云与产品枚举（扫描范围声明依据）。
func TestCloudShimMetadata(t *testing.T) {
	cases := []struct {
		name     string
		adapter  CloudScanAdapter
		cloud    domain.Cloud
		products []domain.Product
	}{
		{"aliyun", NewAliyunScanAdapter(nil), domain.CloudAliyun,
			[]domain.Product{domain.ProductCDN, domain.ProductDCDN, domain.ProductWAF, domain.ProductALB, domain.ProductNLB}},
		{"tencent", NewTencentScanAdapter(nil), domain.CloudTencent,
			[]domain.Product{domain.ProductCDN, domain.ProductWAF, domain.ProductCLB}},
		{"huawei", NewHuaweiScanAdapter(nil), domain.CloudHuawei,
			[]domain.Product{domain.ProductCDN, domain.ProductWAF, domain.ProductALB, domain.ProductNLB}},
		{"aws", NewAwsScanAdapter(nil), domain.CloudAWS,
			[]domain.Product{domain.ProductCDN, domain.ProductALB, domain.ProductNLB}},
		{"azure", NewAzureScanAdapter(nil), domain.CloudAzure,
			[]domain.Product{domain.ProductCDN, domain.ProductALB}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.cloud, tc.adapter.Cloud())
			assert.Equal(t, tc.products, tc.adapter.Products())
		})
	}
}

// TestToDiscoveredRefs 五元组闭包 → 统一引用形态转换。
func TestToDiscoveredRefs(t *testing.T) {
	out := toDiscoveredRefs(2, func(i int) (string, string, string, string, string) {
		return "aliyun", "cdn", "res-" + string(rune('a'+i)), "cert-1", "acc-1"
	})
	require.Len(t, out, 2)
	assert.Equal(t, DiscoveredRef{Cloud: "aliyun", Product: "cdn", ResourceID: "res-a", ReferencedCloudCertID: "cert-1", AccountKey: "acc-1"}, out[0])
	assert.Equal(t, "res-b", out[1].ResourceID)
	assert.Empty(t, toDiscoveredRefs(0, nil))
}

// fakeAssetInstances asset 实例仓储 fake（嵌入接口仅覆写 Count，记录候选过滤）。
type fakeAssetInstances struct {
	assetrepo.InstanceRepository
	counts  map[string]int64 // model_uid → count
	err     error
	filters []assetdomain.InstanceFilter
}

func (f *fakeAssetInstances) Count(_ context.Context, filter assetdomain.InstanceFilter) (int64, error) {
	f.filters = append(f.filters, filter)
	if f.err != nil {
		return 0, f.err
	}
	return f.counts[filter.ModelUID], nil
}

// TestAssetRepositoryCounts asset 盘点计数：model_uid 候选聚合（多后缀并和）
// 与 provider 过滤形状；任一查询失败整体不可用。
func TestAssetRepositoryCounts(t *testing.T) {
	instances := &fakeAssetInstances{counts: map[string]int64{
		"aliyun_cdn":  2,
		"aliyun_lb":   1, // 通用 lb 归 clb
		"tencent_clb": 3,
	}}
	src := NewAssetRepositoryCounts(instances)

	counts, err := src.Counts(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, counts[CloudProductKey{Cloud: domain.CloudAliyun, Product: domain.ProductCDN}])
	assert.Equal(t, 1, counts[CloudProductKey{Cloud: domain.CloudAliyun, Product: domain.ProductCLB}])
	assert.Equal(t, 3, counts[CloudProductKey{Cloud: domain.CloudTencent, Product: domain.ProductCLB}])
	assert.Equal(t, 0, counts[CloudProductKey{Cloud: domain.CloudAzure, Product: domain.ProductCDN}])
	// 候选过滤按 "{provider}_{type}" model_uid 约定
	assert.Contains(t, instances.filters, assetdomain.InstanceFilter{ModelUID: "aliyun_cdn"})

	// 任一候选查询失败 → 整体不可用（-1 固化依据）
	instances.err = errors.New("asset store down")
	_, err = src.Counts(context.Background())
	assert.Error(t, err)
}

// fakeAccountRepo account 仓储 fake（嵌入接口仅覆写 List，记录过滤形状）。
type fakeAccountRepo struct {
	accountrepo.CloudAccountRepository
	accounts []sharedomain.CloudAccount
	err      error
	filters  []sharedomain.CloudAccountFilter
}

func (f *fakeAccountRepo) List(_ context.Context, filter sharedomain.CloudAccountFilter) ([]sharedomain.CloudAccount, int64, error) {
	f.filters = append(f.filters, filter)
	if f.err != nil {
		return nil, 0, f.err
	}
	return f.accounts, int64(len(f.accounts)), nil
}

// TestAccountScanSource 扫描账号源：active+provider 过滤形状与指针化返回。
func TestAccountScanSource(t *testing.T) {
	repo := &fakeAccountRepo{accounts: []sharedomain.CloudAccount{
		{Name: "acc-1", Provider: sharedomain.CloudProviderAliyun, Status: sharedomain.CloudAccountStatusActive},
	}}
	src := NewAccountScanSource(repo)

	accounts, err := src.ActiveByCloud(context.Background(), domain.CloudAliyun)
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	assert.Equal(t, "acc-1", accounts[0].Name)
	require.Len(t, repo.filters, 1)
	assert.Equal(t, sharedomain.CloudProviderAliyun, repo.filters[0].Provider)
	assert.Equal(t, sharedomain.CloudAccountStatusActive, repo.filters[0].Status)

	repo.err = errors.New("account store down")
	_, err = src.ActiveByCloud(context.Background(), domain.CloudAliyun)
	assert.Error(t, err)
}

// fakeCRDLister dynamic client fake：按 GVR 返回列表/未命中错误。
type fakeCRDLister struct {
	lists  map[schema.GroupVersionResource][]unstructured.Unstructured
	errs   map[schema.GroupVersionResource]error
	called []schema.GroupVersionResource
}

func (f *fakeCRDLister) List(_ context.Context, gvr schema.GroupVersionResource, _ string) (*unstructured.UnstructuredList, error) {
	f.called = append(f.called, gvr)
	if err := f.errs[gvr]; err != nil {
		return nil, err
	}
	items := f.lists[gvr]
	return &unstructured.UnstructuredList{Items: items}, nil
}

// TestListCRDObjects 候选 GVR 探测：内置精确命中、自定义 version 候选逐试、
// 全未命中报错、非未命中错误透传。
func TestListCRDObjects(t *testing.T) {
	ingressGVR := schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}
	obj := unstructured.Unstructured{}
	obj.SetNamespace("ns-1")
	obj.SetName("ing-1")

	// 内置枚举：一次命中，namespace/name 透传
	lister := &fakeCRDLister{lists: map[schema.GroupVersionResource][]unstructured.Unstructured{
		ingressGVR: {obj},
	}}
	out, err := listCRDObjects(context.Background(), "c1", "networking.k8s.io", "Ingress", lister)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "ns-1", out[0].Namespace)
	assert.Equal(t, "ing-1", out[0].Name)
	assert.Equal(t, []schema.GroupVersionResource{ingressGVR}, lister.called)

	// 自定义登记：v1 未命中（NoMatch）→ v1beta1 命中
	customV1 := schema.GroupVersionResource{Group: "gateway.example.com", Version: "v1", Resource: "mygateways"}
	customV1Beta1 := customV1
	customV1Beta1.Version = "v1beta1"
	lister = &fakeCRDLister{
		lists: map[schema.GroupVersionResource][]unstructured.Unstructured{customV1Beta1: {obj}},
		errs: map[schema.GroupVersionResource]error{
			customV1: &k8smeta.NoResourceMatchError{PartialResource: customV1},
		},
	}
	out, err = listCRDObjects(context.Background(), "c1", "gateway.example.com", "MyGateway", lister)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Len(t, lister.called, 2)

	// 全部候选未命中（NotFound）→ 明确报错（通道失败语义）
	notFound := apierrors.NewNotFound(schema.GroupResource{Resource: "mygateways"}, "")
	lister = &fakeCRDLister{errs: map[schema.GroupVersionResource]error{
		customV1:      notFound,
		customV1Beta1: notFound,
		{Group: "gateway.example.com", Version: "v1alpha1", Resource: "mygateways"}: notFound,
	}}
	_, err = listCRDObjects(context.Background(), "c1", "gateway.example.com", "MyGateway", lister)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in cluster")

	// 非未命中错误（如连接失败）→ 透传不继续探测
	connErr := errors.New("connection refused")
	lister = &fakeCRDLister{errs: map[schema.GroupVersionResource]error{ingressGVR: connErr}}
	_, err = listCRDObjects(context.Background(), "c1", "networking.k8s.io", "Ingress", lister)
	assert.ErrorIs(t, err, connErr)
	assert.True(t, isNoMatchOrNotFound(apierrors.NewNotFound(schema.GroupResource{Resource: "x"}, "")))
}

// =====================================================================
// 启动孤儿回收 / 异步 panic 兜底
// =====================================================================

// TestRecoverOrphanedScans 启动孤儿回收：全部 running 快照（无论新旧）立即转
// failed（SCAN_INTERRUPTED）释放防重锁；不发告警事件（重启属预期运维动作）。
func TestRecoverOrphanedScans(t *testing.T) {
	adapter := aliyunAdapter()
	h := newScanHarness(t, []CloudScanAdapter{adapter}, &fakeAssetSource{counts: map[CloudProductKey]int{}})

	oldID := seedRunningSnapshot(t, h.snapshots, time.Now().Add(-30*time.Minute))
	freshID := seedRunningSnapshot(t, h.snapshots, time.Now().Add(-1*time.Second))

	recovered, err := h.svc.RecoverOrphanedScans(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, recovered)

	for _, id := range []string{oldID, freshID} {
		snap, err := h.snapshots.GetByID(context.Background(), id)
		require.NoError(t, err)
		assert.Equal(t, domain.ScanStatusFailed, snap.Status)
		assert.Equal(t, domain.FailReasonScanInterrupted, snap.FailReason)
		require.NotNil(t, snap.FinishedAt)
	}
	// 孤儿回收不发告警（区别于超时恢复）
	assert.Empty(t, h.notifier.calls)

	// 防重锁已释放：可重新触发扫描
	res, err := h.svc.StartScan(context.Background())
	require.NoError(t, err)
	assert.Equal(t, domain.ScanStatusDone, res.Status)
}

// panicScanAdapter ListReferences panic 的适配（模拟云 SDK 崩溃）。
type panicScanAdapter struct {
	fakeScanAdapter
}

func (p *panicScanAdapter) ListReferences(context.Context, *sharedomain.CloudAccount, domain.Product) ([]DiscoveredRef, error) {
	panic("sdk exploded")
}

// TestStartScanAsyncPanicGuard 异步扫描 panic 兜底：后台 goroutine panic 时
// 快照转 failed（SCAN_INTERRUPTED）释放防重锁，进程不崩。
func TestStartScanAsyncPanicGuard(t *testing.T) {
	h := newScanHarness(t, []CloudScanAdapter{&panicScanAdapter{fakeScanAdapter{
		cloud: domain.CloudAliyun, products: []domain.Product{domain.ProductCDN},
	}}}, &fakeAssetSource{counts: map[CloudProductKey]int{}})

	res, err := h.svc.StartScanAsync(context.Background())
	require.NoError(t, err)
	assert.Equal(t, domain.ScanStatusRunning, res.Status)

	// 轮询等待后台 goroutine 兜底落终态（上限 5s）
	deadline := time.Now().Add(5 * time.Second)
	for {
		snap, err := h.snapshots.GetByID(context.Background(), res.SnapshotID)
		require.NoError(t, err)
		if snap.Status != domain.ScanStatusRunning {
			assert.Equal(t, domain.ScanStatusFailed, snap.Status)
			assert.Equal(t, domain.FailReasonScanInterrupted, snap.FailReason)
			require.NotNil(t, snap.FinishedAt)
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("panic 兜底未在 5s 内落终态，快照仍 running")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
