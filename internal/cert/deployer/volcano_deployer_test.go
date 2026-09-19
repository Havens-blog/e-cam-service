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

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	volcanosdkalb "github.com/volcengine/volcengine-go-sdk/service/alb"
	volcanosdkcdn "github.com/volcengine/volcengine-go-sdk/service/cdn"
	volcanosdkcsv "github.com/volcengine/volcengine-go-sdk/service/certificateservice"
	volcanosdkwaf "github.com/volcengine/volcengine-go-sdk/service/waf"
	"github.com/volcengine/volcengine-go-sdk/volcengine/request"
)

// ---------------------------------------------------------------------
// 测试替身：fake 火山证书库 SDK（volcanoCertLibraryAPI 窄接口）
// ---------------------------------------------------------------------

// fakeVolcanoLib mock 火山四证书库 SDK 客户端束：记录逐库调用序列与上传产物
// ID，支持按调用次数注入错误（限流/一般失败）与 per-ID 查询/删除结果。
// 证书库语义对齐各库真实 API 缺省：Get/Describe 未命中=空结果（Exists=false）；
// Delete 未命中=nil（已删除=成功）；显式注入错误经 err/errFn 覆盖。
type fakeVolcanoLib struct {
	mu sync.Mutex

	// 上传（csv ImportCertificate / cdn AddCertificate / waf UploadWafServiceCertificate / alb UploadCertificate）
	importIDs    []string // csv 逐次 InstanceId；耗尽沿用末值（缺省 inst-9001）
	importErrFn  func(call int) error
	cdnCertIDs   []string // cdn 逐次 CertId（缺省 cert-cdn-9001）
	cdnAddErrFn  func(call int) error
	wafNextID    int32 // waf 逐次 Id 自增（缺省起 31）
	wafUpErrFn   func(call int) error
	albCertIDs   []string // alb/nlb 逐次 CertificateId（缺省 cert-alb-9001）
	albUpErrFn   func(call int) error
	csvReqs      []*volcanosdkcsv.ImportCertificateInput
	cdnAddReqs   []*volcanosdkcdn.AddCertificateInput
	wafUpReqs    []*volcanosdkwaf.UploadWafServiceCertificateInput
	albUpReqs    []*volcanosdkalb.UploadCertificateInput
	lastChainPEM string // csv 最近一次上传的证书链（私钥不记录——Hard Rule）
	lastKeySeen  string // 最近一次上传收到的私钥明文（仅内存，断言传递透明性后由 GC 回收）

	// 查询（csv CertificateGetInstance / cdn ListCertInfo / waf ListWafServiceCertificate / alb DescribeCertificates）
	csvGetReqs      []*volcanosdkcsv.CertificateGetInstanceInput
	csvGet          map[string]fakeVolcanoCSVInstance // instanceID → 查询结果（未命中=not found 错误）
	getErr          map[string]error                  // instanceID → 显式注入查询错误（非 not-found 语义）
	cdnListReqs     []*volcanosdkcdn.ListCertInfoInput
	cdnList         map[string][]*volcanosdkcdn.CertInfoForListCertInfoOutput // certID → 命中条目
	wafList         []*volcanosdkwaf.DataForListWafServiceCertificateOutput
	albDescribeReqs []*volcanosdkalb.DescribeCertificatesInput
	albDescribe     map[string][]*volcanosdkalb.CertificateForDescribeCertificatesOutput // certID → 命中条目

	// 删除（csv CertificateDeleteInstance / cdn DeleteCdnCertificate / waf DeleteWafServiceCertificate / alb DeleteCertificate）
	deletes []string // "product:rawID" 逐次
	delErr  map[string]error
}

type fakeVolcanoCSVInstance struct {
	status   string
	revoked  bool
	chain    []string // 证书链 PEM 列表（叶在前）
	chainErr bool     // chain 缺失/不可解析
}

func (f *fakeVolcanoLib) ImportCertificateWithContext(_ context.Context, input *volcanosdkcsv.ImportCertificateInput, _ ...request.Option) (*volcanosdkcsv.ImportCertificateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.csvReqs) + 1
	f.csvReqs = append(f.csvReqs, input)
	if input.CertificateInfo != nil {
		f.lastChainPEM = *input.CertificateInfo.CertificateChain
		f.lastKeySeen = *input.CertificateInfo.PrivateKey
	}
	if f.importErrFn != nil {
		if err := f.importErrFn(n); err != nil {
			return nil, err
		}
	}
	id := "inst-9001"
	if len(f.importIDs) >= n && f.importIDs[n-1] != "" {
		id = f.importIDs[n-1]
	} else if len(f.importIDs) > 0 {
		id = f.importIDs[len(f.importIDs)-1]
	}
	return &volcanosdkcsv.ImportCertificateOutput{InstanceId: strPtr(id)}, nil
}

func (f *fakeVolcanoLib) CertificateGetInstanceWithContext(_ context.Context, input *volcanosdkcsv.CertificateGetInstanceInput, _ ...request.Option) (*volcanosdkcsv.CertificateGetInstanceOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.csvGetReqs = append(f.csvGetReqs, input)
	id := *input.InstanceId
	if err := f.getErr[id]; err != nil {
		return nil, err
	}
	inst, ok := f.csvGet[id]
	if !ok {
		return nil, errors.New("Volcengine: instance " + id + " not found")
	}
	out := &volcanosdkcsv.CertificateGetInstanceOutput{
		Status:               strPtr(inst.status),
		IsCertificateRevoked: boolPtr(inst.revoked),
	}
	if !inst.chainErr {
		detail := &volcanosdkcsv.CertificateDetailForCertificateGetInstanceOutput{}
		for _, pemStr := range inst.chain {
			s := pemStr
			detail.Chain = append(detail.Chain, &s)
		}
		out.CertificateDetail = detail
	}
	return out, nil
}

func (f *fakeVolcanoLib) CertificateDeleteInstanceWithContext(_ context.Context, input *volcanosdkcsv.CertificateDeleteInstanceInput, _ ...request.Option) (*volcanosdkcsv.CertificateDeleteInstanceOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, volcanoProductCSV+":"+*input.InstanceId)
	if err := f.delErr[volcanoProductCSV+":"+*input.InstanceId]; err != nil {
		return nil, err
	}
	return &volcanosdkcsv.CertificateDeleteInstanceOutput{}, nil
}

func (f *fakeVolcanoLib) AddCertificateWithContext(_ context.Context, input *volcanosdkcdn.AddCertificateInput, _ ...request.Option) (*volcanosdkcdn.AddCertificateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.cdnAddReqs) + 1
	f.cdnAddReqs = append(f.cdnAddReqs, input)
	f.lastChainPEM = *input.Certificate
	f.lastKeySeen = *input.PrivateKey
	if f.cdnAddErrFn != nil {
		if err := f.cdnAddErrFn(n); err != nil {
			return nil, err
		}
	}
	id := "cert-cdn-9001"
	if len(f.cdnCertIDs) >= n && f.cdnCertIDs[n-1] != "" {
		id = f.cdnCertIDs[n-1]
	} else if len(f.cdnCertIDs) > 0 {
		id = f.cdnCertIDs[len(f.cdnCertIDs)-1]
	}
	return &volcanosdkcdn.AddCertificateOutput{CertId: strPtr(id)}, nil
}

func (f *fakeVolcanoLib) ListCertInfoWithContext(_ context.Context, input *volcanosdkcdn.ListCertInfoInput, _ ...request.Option) (*volcanosdkcdn.ListCertInfoOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cdnListReqs = append(f.cdnListReqs, input)
	items := f.cdnList[*input.CertId]
	return &volcanosdkcdn.ListCertInfoOutput{CertInfo: items}, nil
}

func (f *fakeVolcanoLib) DeleteCdnCertificateWithContext(_ context.Context, input *volcanosdkcdn.DeleteCdnCertificateInput, _ ...request.Option) (*volcanosdkcdn.DeleteCdnCertificateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, volcanoProductCDN+":"+*input.CertId)
	if err := f.delErr[volcanoProductCDN+":"+*input.CertId]; err != nil {
		return nil, err
	}
	return &volcanosdkcdn.DeleteCdnCertificateOutput{}, nil
}

func (f *fakeVolcanoLib) UploadWafServiceCertificateWithContext(_ context.Context, input *volcanosdkwaf.UploadWafServiceCertificateInput, _ ...request.Option) (*volcanosdkwaf.UploadWafServiceCertificateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.wafUpReqs) + 1
	f.wafUpReqs = append(f.wafUpReqs, input)
	f.lastChainPEM = *input.PublicKey
	f.lastKeySeen = *input.PrivateKey
	if f.wafUpErrFn != nil {
		if err := f.wafUpErrFn(n); err != nil {
			return nil, err
		}
	}
	if f.wafNextID == 0 {
		f.wafNextID = 31 // 缺省起始 Id（自增语义与云侧一致）
	}
	f.wafNextID++
	return &volcanosdkwaf.UploadWafServiceCertificateOutput{Id: int32Ptr(f.wafNextID - 1)}, nil
}

func (f *fakeVolcanoLib) ListWafServiceCertificateWithContext(_ context.Context, _ *volcanosdkwaf.ListWafServiceCertificateInput, _ ...request.Option) (*volcanosdkwaf.ListWafServiceCertificateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &volcanosdkwaf.ListWafServiceCertificateOutput{Data: f.wafList}, nil
}

func (f *fakeVolcanoLib) DeleteWafServiceCertificateWithContext(_ context.Context, input *volcanosdkwaf.DeleteWafServiceCertificateInput, _ ...request.Option) (*volcanosdkwaf.DeleteWafServiceCertificateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, volcanoProductWAF+":"+*input.Id)
	if err := f.delErr[volcanoProductWAF+":"+*input.Id]; err != nil {
		return nil, err
	}
	return &volcanosdkwaf.DeleteWafServiceCertificateOutput{}, nil
}

func (f *fakeVolcanoLib) UploadCertificateWithContext(_ context.Context, input *volcanosdkalb.UploadCertificateInput, _ ...request.Option) (*volcanosdkalb.UploadCertificateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.albUpReqs) + 1
	f.albUpReqs = append(f.albUpReqs, input)
	f.lastChainPEM = *input.PublicKey
	f.lastKeySeen = *input.PrivateKey
	if f.albUpErrFn != nil {
		if err := f.albUpErrFn(n); err != nil {
			return nil, err
		}
	}
	id := "cert-alb-9001"
	if len(f.albCertIDs) >= n && f.albCertIDs[n-1] != "" {
		id = f.albCertIDs[n-1]
	} else if len(f.albCertIDs) > 0 {
		id = f.albCertIDs[len(f.albCertIDs)-1]
	}
	return &volcanosdkalb.UploadCertificateOutput{CertificateId: strPtr(id)}, nil
}

func (f *fakeVolcanoLib) DescribeCertificatesWithContext(_ context.Context, input *volcanosdkalb.DescribeCertificatesInput, _ ...request.Option) (*volcanosdkalb.DescribeCertificatesOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.albDescribeReqs = append(f.albDescribeReqs, input)
	var items []*volcanosdkalb.CertificateForDescribeCertificatesOutput
	for _, idPtr := range input.CertificateIds {
		items = append(items, f.albDescribe[*idPtr]...)
	}
	return &volcanosdkalb.DescribeCertificatesOutput{Certificates: items}, nil
}

func (f *fakeVolcanoLib) DeleteCertificateWithContext(_ context.Context, input *volcanosdkalb.DeleteCertificateInput, _ ...request.Option) (*volcanosdkalb.DeleteCertificateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// alb 与 nlb 共用 alb 证书库删除 API（调用方经归一前缀路由至此）——记录
	// alb 口径前缀，错误注入键同时认 alb:/nlb: 两形态。
	f.deletes = append(f.deletes, volcanoProductALB+":"+*input.CertificateId)
	if err := f.delErr[volcanoProductALB+":"+*input.CertificateId]; err != nil {
		return nil, err
	}
	if err := f.delErr[volcanoProductNLB+":"+*input.CertificateId]; err != nil {
		return nil, err
	}
	return &volcanosdkalb.DeleteCertificateOutput{}, nil
}

// ---------------------------------------------------------------------
// 测试装配
// ---------------------------------------------------------------------

// newTestVolcanoDeployer 装配被测部署器：fake 证书库 SDK + 确定性时间/随机后缀
// + 即时睡眠记录器。
func newTestVolcanoDeployer(fake *fakeVolcanoLib) (*VolcanoDeployer, *sleepRecorder) {
	d := NewVolcanoDeployer(nil)
	rec := &sleepRecorder{}
	d.sleep = rec.sleep
	d.now = func() time.Time { return time.Unix(1765432100, 0) }
	counter := 0
	d.randHex = func(int) string { counter++; return fmt.Sprintf("%04x", counter) }
	d.newClients = func(*sharedomain.CloudAccount) (volcanoCertLibraryAPI, error) {
		return fake, nil
	}
	return d, rec
}

func testVolcanoCreds() Credential {
	return Credential{
		Kind: CredentialKindCloudAK, Cloud: "volcano", AccountKey: "acc-main",
		AccessKey: "VOLC-test-ak", Secret: []byte("test-sk-plaintext"), KeyVersion: 1,
	}
}

// uploadCallsSnapshot 各库上传调用总数（csv+cdn+waf+alb）。
func (f *fakeVolcanoLib) uploadCallTotal() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.csvReqs) + len(f.cdnAddReqs) + len(f.wafUpReqs) + len(f.albUpReqs)
}

func (f *fakeVolcanoLib) deletesSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deletes...)
}

// ---------------------------------------------------------------------
// AC-1：UploadCert（certificateservice 统一证书库第一段 + {product}:{id} 归一）
// ---------------------------------------------------------------------

// 第一段统一以 csv（certificateservice）口径上传：ImportCertificate 可重复导入
// （C7 重试即新副本），返回 {csv}:{instanceId} 归一云证书 ID；上传名符合 C7 公式。
func TestVolcanoDeployerUploadCSVDefault(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{}
	d, _ := newTestVolcanoDeployer(fake)

	id, err := d.UploadCert(ctx, testVolcanoCreds(), testCertPEM, []byte(testKeyPEM))
	assert.NoError(t, err)
	assert.Equal(t, "csv:inst-9001", id, "第一段统一落 certificateservice，ID 归一 csv: 前缀")

	assert.Len(t, fake.csvReqs, 1)
	req := fake.csvReqs[0]
	assert.True(t, req.Repeatable != nil && *req.Repeatable, "可重复导入（重试即新副本）")
	assert.Equal(t, testCertPEM, *req.CertificateInfo.CertificateChain)
	assert.Equal(t, testKeyPEM, *req.CertificateInfo.PrivateKey, "私钥明文仅内存传递")
}

// 同材料重试/重复上传逐次新副本：ID 逐次不同、上传名互不相同（C7）。
func TestVolcanoDeployerUploadProducesFreshInstances(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{importIDs: []string{"inst-1", "inst-2"}}
	d, _ := newTestVolcanoDeployer(fake)

	id1, err := d.UploadCert(ctx, testVolcanoCreds(), testCertPEM, []byte(testKeyPEM))
	assert.NoError(t, err)
	id2, err := d.UploadCert(ctx, testVolcanoCreds(), testCertPEM, []byte(testKeyPEM))
	assert.NoError(t, err)
	assert.Equal(t, "csv:inst-1", id1)
	assert.Equal(t, "csv:inst-2", id2)
	assert.Len(t, fake.csvReqs, 2)
}

// 材料缺失/凭证归属云不符：显式拒绝且不触达云侧。
func TestVolcanoDeployerUploadRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{}
	d, _ := newTestVolcanoDeployer(fake)

	_, err := d.UploadCert(ctx, testVolcanoCreds(), "", []byte(testKeyPEM))
	assert.ErrorContains(t, err, "requires cert PEM and key PEM")
	_, err = d.UploadCert(ctx, testVolcanoCreds(), testCertPEM, nil)
	assert.ErrorContains(t, err, "requires cert PEM and key PEM")

	foreign := testVolcanoCreds()
	foreign.Cloud = "aliyun"
	_, err = d.UploadCert(ctx, foreign, testCertPEM, []byte(testKeyPEM))
	assert.ErrorContains(t, err, "not volcano")

	invalid := testVolcanoCreds()
	invalid.AccessKey = ""
	_, err = d.UploadCert(ctx, invalid, testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, ErrInvalidCredential)

	assert.Zero(t, fake.uploadCallTotal(), "校验失败不产生云侧调用")
}

// 私钥卫生：云侧失败错误信息不携带私钥明文（Hard Rule：私钥不进日志/错误）。
func TestVolcanoDeployerUploadErrorLeaksNoKey(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{importErrFn: func(int) error { return errors.New("import denied") }}
	d, _ := newTestVolcanoDeployer(fake)

	_, err := d.UploadCert(ctx, testVolcanoCreds(), testCertPEM, []byte(testKeyPEM))
	assert.Error(t, err)
	assert.NotContains(t, err.Error(), testKeyPEM)
	assert.NotContains(t, err.Error(), "test-sk-plaintext", "凭证明文同样不入错误")
}

// 限流退避（boundedRetry 共享口径）：固定序列退避后恢复，耗尽透传哨兵。
func TestVolcanoDeployerUploadRateLimitedBackoff(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{importErrFn: func(call int) error {
		if call <= 2 {
			return fmt.Errorf("volcano api throttled: %w", cloudx.ErrCloudRateLimited)
		}
		return nil
	}}
	d, rec := newTestVolcanoDeployer(fake)

	id, err := d.UploadCert(ctx, testVolcanoCreds(), testCertPEM, []byte(testKeyPEM))
	assert.NoError(t, err)
	assert.Equal(t, "csv:inst-9001", id)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, rec.snapshot())

	exhaust := &fakeVolcanoLib{importErrFn: func(int) error {
		return fmt.Errorf("throttled: %w", cloudx.ErrCloudRateLimited)
	}}
	d2, _ := newTestVolcanoDeployer(exhaust)
	_, err = d2.UploadCert(ctx, testVolcanoCreds(), testCertPEM, []byte(testKeyPEM))
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited, "耗尽后哨兵语义保留")
	assert.Len(t, exhaust.csvReqs, 5, "默认 MaxAttempts=5，绝不无限重试")
}

// ---------------------------------------------------------------------
// AC-1：uploadForProduct 五分支 + {product}:{id} 多产品 ID 归一断言
// ---------------------------------------------------------------------

// 五分支上传产物 ID 逐产品归一：cdn/waf/alb/nlb/csv 各落对应证书库。
func TestVolcanoDeployerUploadForProductIDNormalization(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{
		importIDs:  []string{"inst-x"},
		cdnCertIDs: []string{"cert-cdn-x"},
		albCertIDs: []string{"cert-alb-x", "cert-nlb-x"},
	}
	d, _ := newTestVolcanoDeployer(fake)
	acct := &sharedomain.CloudAccount{Name: "acc-main", Provider: sharedomain.CloudProviderVolcano}

	cases := []struct {
		product string
		want    string
	}{
		{volcanoProductCSV, "csv:inst-x"},
		{volcanoProductCDN, "cdn:cert-cdn-x"},
		{volcanoProductWAF, "waf:31"},
		{volcanoProductALB, "alb:cert-alb-x"},
		{volcanoProductNLB, "nlb:cert-nlb-x"},
	}
	for _, tc := range cases {
		got, err := d.uploadForProduct(ctx, acct, tc.product, "ecam-name", testCertPEM, testKeyPEM)
		assert.NoError(t, err, tc.product)
		assert.Equal(t, tc.want, got, "产品 %s 云证书 ID 归一 {product}:{id}", tc.product)
	}
	assert.Len(t, fake.csvReqs, 1)
	assert.Len(t, fake.cdnAddReqs, 1)
	assert.Len(t, fake.wafUpReqs, 1)
	assert.Len(t, fake.albUpReqs, 2, "alb/nlb 共用 ALB 监听证书库上传 API")
}

// 未支持产品：哨兵错误。
func TestVolcanoDeployerUploadForProductUnsupported(t *testing.T) {
	d, _ := newTestVolcanoDeployer(&fakeVolcanoLib{})
	_, err := d.uploadForProduct(context.Background(), &sharedomain.CloudAccount{},
		"clb", "ecam-name", testCertPEM, testKeyPEM)
	assert.ErrorIs(t, err, ErrVolcanoProductNotSupported)
}

// ---------------------------------------------------------------------
// AC-2：GetCert（csv 链解析三要素 + 产品库在库状态路由）
// ---------------------------------------------------------------------

// csv 分支：在库=链解析指纹（SHA256 对齐台账口径）+ 有效期；已删除=Exists=false
// 非错误；revoked=Exists=false（不可作回滚目标）；链缺失=存在但指纹无法复核。
func TestVolcanoDeployerGetCertCSVBranch(t *testing.T) {
	bundle := certtest.NewBundle(t, "www.example.com", nil, nil)
	notAfter := time.Now().Add(90 * 24 * time.Hour).Truncate(time.Second)
	fake := &fakeVolcanoLib{
		csvGet: map[string]fakeVolcanoCSVInstance{
			"inst-live":    {status: "Issued", chain: []string{string(bundle.CertPEM)}},
			"inst-revoked": {status: "Revoked", revoked: true, chain: []string{string(bundle.CertPEM)}},
			"inst-nochain": {status: "Issued", chainErr: true},
		},
	}
	d, _ := newTestVolcanoDeployer(fake)
	ctx := context.Background()

	info, err := d.GetCert(ctx, testVolcanoCreds(), "csv:inst-live")
	assert.NoError(t, err)
	assert.True(t, info.Exists, "回滚目标有效性校验：在库存在性")
	assert.Equal(t, bundle.Fingerprint, info.Fingerprint, "SHA256(leaf.Raw) 对齐台账口径")
	assert.WithinDuration(t, notAfter.Add(-90*24*time.Hour).Add(8760*time.Hour), info.NotAfter, time.Minute,
		"有效期取叶证书 NotAfter")

	info, err = d.GetCert(ctx, testVolcanoCreds(), "csv:inst-ghost")
	assert.NoError(t, err, "云侧已删除=Exists=false 非错误（幂等口径）")
	assert.False(t, info.Exists)

	info, err = d.GetCert(ctx, testVolcanoCreds(), "csv:inst-revoked")
	assert.NoError(t, err)
	assert.False(t, info.Exists, "revoked 实例不构成有效回滚目标")

	info, err = d.GetCert(ctx, testVolcanoCreds(), "csv:inst-nochain")
	assert.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Empty(t, info.Fingerprint, "链缺失 → 指纹无法复核（上层 fail-safe）")
}

// 产品库分支：cdn（指纹 Sha256 直读 + 有效期 unix 秒）/ waf（服务证书列表反查）/
// alb、nlb（DescribeCertificates 反查）；未命中=Exists=false 非错误。
func TestVolcanoDeployerGetCertProductBranches(t *testing.T) {
	notAfter := time.Unix(1790000000, 0)
	fake := &fakeVolcanoLib{
		cdnList: map[string][]*volcanosdkcdn.CertInfoForListCertInfoOutput{
			"cert-cdn-1": {{CertId: strPtr("cert-cdn-1"), ExpireTime: int64Ptr(1790000000),
				CertFingerprint: &volcanosdkcdn.CertFingerprintForListCertInfoOutput{Sha256: strPtr(strings.Repeat("a", 64))}},
			},
			"cert-cdn-sha1": {{CertId: strPtr("cert-cdn-sha1"), ExpireTime: int64Ptr(1790000000),
				CertFingerprint: &volcanosdkcdn.CertFingerprintForListCertInfoOutput{Sha1: strPtr(strings.Repeat("b", 40))}},
			},
		},
		wafList: []*volcanosdkwaf.DataForListWafServiceCertificateOutput{
			{Id: int32Ptr(31), ExpireTime: strPtr("2026-09-01 00:00:00")},
		},
		albDescribe: map[string][]*volcanosdkalb.CertificateForDescribeCertificatesOutput{
			"cert-alb-1": {{CertificateId: strPtr("cert-alb-1"), ExpiredAt: strPtr("2026-09-01T00:00:00Z")}},
			"cert-nlb-1": {{CertificateId: strPtr("cert-nlb-1"), ExpiredAt: strPtr("2026-09-01T00:00:00Z")}},
		},
	}
	d, _ := newTestVolcanoDeployer(fake)
	ctx := context.Background()

	info, err := d.GetCert(ctx, testVolcanoCreds(), "cdn:cert-cdn-1")
	assert.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Equal(t, strings.Repeat("a", 64), info.Fingerprint, "CDN 库原生 Sha256 指纹对齐口径直读")
	assert.Equal(t, notAfter, info.NotAfter)

	info, err = d.GetCert(ctx, testVolcanoCreds(), "cdn:cert-cdn-sha1")
	assert.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Empty(t, info.Fingerprint, "Sha1 形态指纹不通过对齐校验 → 无法复核")

	info, err = d.GetCert(ctx, testVolcanoCreds(), "cdn:cert-cdn-ghost")
	assert.NoError(t, err)
	assert.False(t, info.Exists)

	info, err = d.GetCert(ctx, testVolcanoCreds(), "waf:31")
	assert.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Equal(t, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), info.NotAfter, "WAF 服务证书有效期解析")

	info, err = d.GetCert(ctx, testVolcanoCreds(), "waf:99")
	assert.NoError(t, err)
	assert.False(t, info.Exists)

	info, err = d.GetCert(ctx, testVolcanoCreds(), "alb:cert-alb-1")
	assert.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Equal(t, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), info.NotAfter)

	info, err = d.GetCert(ctx, testVolcanoCreds(), "nlb:cert-nlb-1")
	assert.NoError(t, err)
	assert.True(t, info.Exists, "nlb 与 alb 共用监听证书库查询路由")

	info, err = d.GetCert(ctx, testVolcanoCreds(), "alb:cert-alb-ghost")
	assert.NoError(t, err)
	assert.False(t, info.Exists)
}

// 非归一 ID（无前缀/未知前缀）：显式拒绝（任务 2 绑定引用与回滚旧 ID 均为
// {product}:{id} 归一形态，解析失败 fail-fast 不猜测）。
// 元数据残缺容忍：云侧条目缺有效期/指纹字段 → Exists=true 但要素留空
// （上层按「无法复核」fail-safe 处理，不因元数据缺失误判不存在）。
func TestVolcanoDeployerGetCertToleratesPartialMetadata(t *testing.T) {
	fake := &fakeVolcanoLib{
		cdnList: map[string][]*volcanosdkcdn.CertInfoForListCertInfoOutput{
			"cert-partial": {{CertId: strPtr("cert-partial")}},
		},
		wafList: []*volcanosdkwaf.DataForListWafServiceCertificateOutput{
			{Id: int32Ptr(77), ExpireTime: strPtr("not-a-time")},
		},
		albDescribe: map[string][]*volcanosdkalb.CertificateForDescribeCertificatesOutput{
			"cert-alb-p": {{CertificateId: strPtr("cert-alb-p")}},
		},
	}
	d, _ := newTestVolcanoDeployer(fake)
	ctx := context.Background()

	info, err := d.GetCert(ctx, testVolcanoCreds(), "cdn:cert-partial")
	assert.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Zero(t, info.NotAfter, "缺 ExpireTime → 有效期零值")
	assert.Empty(t, info.Fingerprint)

	info, err = d.GetCert(ctx, testVolcanoCreds(), "waf:77")
	assert.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Zero(t, info.NotAfter, "不可解析时间形态 → 有效期零值")

	info, err = d.GetCert(ctx, testVolcanoCreds(), "alb:cert-alb-p")
	assert.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Zero(t, info.NotAfter, "缺 ExpiredAt → 有效期零值")

	d.Stop() // 无限流器接缝 no-op（进程退出安全）
}

func TestVolcanoDeployerGetCertRejectsNonNormalizedID(t *testing.T) {
	d, _ := newTestVolcanoDeployer(&fakeVolcanoLib{})
	for _, id := range []string{"inst-9001", "clb:cert-1", "csv:", "csv: "} {
		_, err := d.GetCert(context.Background(), testVolcanoCreds(), id)
		assert.ErrorContains(t, err, "not normalized", id)
	}
}

// 云侧查询失败（非 not-found）：错误透传（回滚判定 fail-safe 阻断，不误判有效）。
func TestVolcanoDeployerGetCertCloudErrorPropagates(t *testing.T) {
	fake := &fakeVolcanoLib{
		getErr: map[string]error{"inst-1": errors.New("internal server error")},
	}
	d, _ := newTestVolcanoDeployer(fake)

	_, err := d.GetCert(context.Background(), testVolcanoCreds(), "csv:inst-1")
	assert.ErrorContains(t, err, "internal server error", "not-found 归一外的查询失败按错误处理")
}

// ---------------------------------------------------------------------
// AC-3：CleanupOrphan（幂等成功 + 逐库路由）
// ---------------------------------------------------------------------

// 幂等：已删除证书双调用同结果（云侧 not-found 归一为成功，清理队列重放安全）。
func TestVolcanoDeployerCleanupOrphanIdempotent(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{
		delErr: map[string]error{"csv:inst-9001": errors.New("Volcengine: instance inst-9001 not found")},
	}
	d, _ := newTestVolcanoDeployer(fake)

	assert.NoError(t, d.CleanupOrphan(ctx, testVolcanoCreds(), "csv:inst-9001"))
	assert.NoError(t, d.CleanupOrphan(ctx, testVolcanoCreds(), "csv:inst-9001"), "双调用同结果（幂等）")
	assert.Equal(t, []string{"csv:inst-9001", "csv:inst-9001"}, fake.deletesSnapshot())
}

// 逐库路由：csv/cdn/waf/alb/nlb 删除落对应证书库 API（alb/nlb 共用 alb 服务
// 删除 API，nlb 路由经调用落在共享客户端验证）。
func TestVolcanoDeployerCleanupOrphanRoutesPerProduct(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{}
	d, _ := newTestVolcanoDeployer(fake)

	for _, id := range []string{"csv:inst-1", "cdn:cert-1", "waf:31", "alb:cert-2", "nlb:cert-3"} {
		assert.NoError(t, d.CleanupOrphan(ctx, testVolcanoCreds(), id))
	}
	assert.Equal(t, []string{"csv:inst-1", "cdn:cert-1", "waf:31", "alb:cert-2", "alb:cert-3"},
		fake.deletesSnapshot(), "五次删除全部路由成功，alb/nlb 共享删除 API")
}

// 删除持续失败（非 not-found）：错误透传 + 限流退避哨兵保留。
func TestVolcanoDeployerCleanupOrphanRateLimited(t *testing.T) {
	ctx := context.Background()
	fake := &fakeVolcanoLib{delErr: map[string]error{
		"csv:inst-9001": fmt.Errorf("delete throttled: %w", cloudx.ErrCloudRateLimited),
	}}
	d, rec := newTestVolcanoDeployer(fake)

	err := d.CleanupOrphan(ctx, testVolcanoCreds(), "csv:inst-9001")
	assert.ErrorIs(t, err, cloudx.ErrCloudRateLimited, "补偿清理同样有界重试")
	assert.Len(t, fake.deletesSnapshot(), 5)
	assert.Len(t, rec.snapshot(), 4)
}

// 非归一 ID 清理：显式拒绝。
func TestVolcanoDeployerCleanupOrphanRejectsNonNormalizedID(t *testing.T) {
	d, _ := newTestVolcanoDeployer(&fakeVolcanoLib{})
	err := d.CleanupOrphan(context.Background(), testVolcanoCreds(), "inst-9001")
	assert.ErrorContains(t, err, "not normalized")
}

// ---------------------------------------------------------------------
// ID 归一小件 + 重试策略装配
// ---------------------------------------------------------------------

func TestVolcanoCloudCertIDNormalization(t *testing.T) {
	assert.Equal(t, "csv:inst-1", normalizeVolcanoCloudCertID(volcanoProductCSV, " inst-1 "))
	product, raw, ok := splitVolcanoCloudCertID("cdn:cert-1")
	assert.True(t, ok)
	assert.Equal(t, volcanoProductCDN, product)
	assert.Equal(t, "cert-1", raw)
	_, _, ok = splitVolcanoCloudCertID("clb:cert-1")
	assert.False(t, ok, "非火山证书库产品前缀拒绝")
	_, _, ok = splitVolcanoCloudCertID("cert-1")
	assert.False(t, ok, "无前缀拒绝")
	_, _, ok = splitVolcanoCloudCertID("cdn:")
	assert.False(t, ok, "空裸 ID 拒绝")
}

func TestVolcanoRetryPolicyOptionNormalization(t *testing.T) {
	def := DefaultRetryPolicy()
	assert.Equal(t, def, RetryPolicy{}.normalized())

	custom := RetryPolicy{
		MaxAttempts:  2,
		Backoffs:     []time.Duration{time.Second},
		MaxTotalWait: time.Second,
	}
	d := NewVolcanoDeployer(nil, WithVolcanoRetryPolicy(custom))
	assert.Equal(t, custom, d.retry)
	assert.Equal(t, def, NewVolcanoDeployer(nil, WithVolcanoRetryPolicy(RetryPolicy{})).retry,
		"零值配置回退缺省保守值")
}

// not-found 启发式：火山各库"已不存在"错误归一为幂等成功语义。
func TestVolcanoCertNotFoundHeuristic(t *testing.T) {
	assert.False(t, isVolcanoCertNotFoundErr(nil))
	assert.True(t, isVolcanoCertNotFoundErr(errors.New("instance not found")))
	assert.True(t, isVolcanoCertNotFoundErr(errors.New("The certificate cert-1 does not exist")))
	assert.True(t, isVolcanoCertNotFoundErr(errors.New("InvalidInstanceId: no such instance")))
	assert.True(t, isVolcanoCertNotFoundErr(errors.New("http 404 Not Found")))
	assert.False(t, isVolcanoCertNotFoundErr(errors.New("delete denied by policy")))
	assert.False(t, isVolcanoCertNotFoundErr(errors.New("rate throttled, retry later")))
}

// ---------------------------------------------------------------------
// 小件
// ---------------------------------------------------------------------

func strPtr(v string) *string { return &v }
func boolPtr(v bool) *bool    { return &v }
func int32Ptr(v int32) *int32 { return &v }
func int64Ptr(v int64) *int64 { return &v }
